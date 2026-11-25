// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"cmp"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

const (
	maxRetryTimerInterval = 15 * time.Minute
)

type RateLimitPriority string

const (
	RateLimitPriorityInteractive RateLimitPriority = "interactive"
	RateLimitPriorityRSS         RateLimitPriority = "rss"
	RateLimitPriorityCompletion  RateLimitPriority = "completion"
	RateLimitPriorityBackground  RateLimitPriority = "background"
)

type RateLimitOptions struct {
	Priority RateLimitPriority
}

var errRSSDeduplicated = errors.New("rss search deduplicated: identical fetch already pending")

type taskExecResult struct {
	results  []Result
	coverage []int
	err      error
}

const (
	searchJobPriorityInteractive = 0
	searchJobPriorityRSS         = 1
	searchJobPriorityCompletion  = 2
	searchJobPriorityBackground  = 3
)

const (
	dispatchCoalesceDelay = 5 * time.Millisecond
	defaultMaxWorkers     = 10
)

type JobCallbacks struct {
	OnComplete func(jobID uint64, indexer *models.TorznabIndexer, results []Result, coverage []int, err error)
	OnJobDone  func(jobID uint64)
}

type SubmitRequest struct {
	Indexers         []*models.TorznabIndexer
	Params           url.Values
	Meta             *searchContext
	Callbacks        JobCallbacks
	ExecutionTimeout time.Duration
	ExecFn           func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)
}

type workerTask struct {
	jobID     uint64
	taskID    uint64
	indexer   *models.TorznabIndexer
	params    url.Values
	meta      *searchContext
	exec      func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)
	ctx       context.Context
	callbacks JobCallbacks
	timeout   time.Duration
	isRSS     bool
}

type taskItem struct {
	task     workerTask
	priority int
	created  time.Time
	index    int
	started  time.Time
	detached bool
	ticket   *slotTicket
}

type taskHeap []*taskItem

func (h taskHeap) Len() int           { return len(h) }
func (h taskHeap) Less(i, j int) bool { return compareTasks(h[i], h[j]) < 0 }

func (h taskHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *taskHeap) Push(x any) {
	item := x.(*taskItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

func compareTasks(a, b *taskItem) int {
	if c := cmp.Compare(a.priority, b.priority); c != 0 {
		return c
	}
	if c := a.created.Compare(b.created); c != 0 {
		return c
	}
	return cmp.Compare(a.task.taskID, b.task.taskID)
}

type jobState struct {
	totalTasks     int
	completedTasks int
	callbacks      JobCallbacks
}

type searchScheduler struct {
	mu    sync.Mutex
	slots *requestSlots

	taskQueue  taskHeap
	pendingRSS map[int]struct{}

	workerPool chan struct{}
	inFlight   map[int]*taskItem
	jobs       map[uint64]*jobState

	submitCh   chan []workerTask
	completeCh chan struct{}
	stopCh     chan struct{}
	stopOnce   sync.Once

	jobSeq  uint64
	taskSeq uint64

	retryTimer *time.Timer

	historyRecorder HistoryRecorder

	activityPublisher atomic.Pointer[activity.Publisher]
}

func (s *searchScheduler) setActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher.Store(&publisher)
}

func (s *searchScheduler) publishActivity(kind activity.Kind) {
	if s == nil {
		return
	}
	if p := s.activityPublisher.Load(); p != nil && *p != nil {
		(*p).Publish(activity.Event{Kind: kind})
	}
}

func newSearchScheduler(slots *requestSlots, maxWorkers int) *searchScheduler {
	if slots == nil {
		slots = newRequestSlots(defaultRequestGap, nil, nil)
	}
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxWorkers
	}
	s := &searchScheduler{
		pendingRSS: make(map[int]struct{}),
		slots:      slots,
		workerPool: make(chan struct{}, maxWorkers),
		inFlight:   make(map[int]*taskItem),
		jobs:       make(map[uint64]*jobState),
		submitCh:   make(chan []workerTask, 128),
		completeCh: make(chan struct{}, 128),
		stopCh:     make(chan struct{}),
	}
	heap.Init(&s.taskQueue)
	slots.setOnFree(s.wake)
	go s.loop()
	return s
}

func (s *searchScheduler) wake() {
	select {
	case s.completeCh <- struct{}{}:
	default:
	}
}

func (s *searchScheduler) Submit(ctx context.Context, req SubmitRequest) (uint64, error) {
	if len(req.Indexers) == 0 {
		if req.Callbacks.OnJobDone != nil {
			go req.Callbacks.OnJobDone(0)
		}
		return 0, nil
	}

	jobID := s.nextJobID()
	tasks := make([]workerTask, 0, len(req.Indexers))
	var dedupSkipped []*models.TorznabIndexer

	for _, idx := range req.Indexers {
		if idx == nil {
			continue
		}

		if req.Meta != nil && req.Meta.rateLimit != nil && req.Meta.rateLimit.Priority == RateLimitPriorityRSS {
			s.mu.Lock()
			if _, exists := s.pendingRSS[idx.ID]; exists {
				s.mu.Unlock()
				dedupSkipped = append(dedupSkipped, idx)
				continue
			}
			s.pendingRSS[idx.ID] = struct{}{}
			s.mu.Unlock()
		}

		tasks = append(tasks, workerTask{
			jobID:     jobID,
			taskID:    s.nextTaskID(),
			indexer:   idx,
			params:    cloneValues(req.Params),
			meta:      req.Meta,
			exec:      req.ExecFn,
			ctx:       ctx,
			callbacks: req.Callbacks,
			timeout:   req.ExecutionTimeout,
			isRSS:     req.Meta != nil && req.Meta.rateLimit != nil && req.Meta.rateLimit.Priority == RateLimitPriorityRSS,
		})
	}

	notifyDedupSkipped := func() {
		if req.Callbacks.OnComplete == nil {
			return
		}
		for _, idx := range dedupSkipped {
			go req.Callbacks.OnComplete(jobID, idx, nil, nil, errRSSDeduplicated)
		}
	}

	if len(tasks) == 0 {
		notifyDedupSkipped()
		if req.Callbacks.OnJobDone != nil {
			go req.Callbacks.OnJobDone(jobID)
		}
		return jobID, nil
	}

	s.mu.Lock()
	s.jobs[jobID] = &jobState{
		totalTasks:     len(tasks),
		completedTasks: 0,
		callbacks:      req.Callbacks,
	}
	s.mu.Unlock()

	select {
	case s.submitCh <- tasks:
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.jobs, jobID)
		s.clearPendingRSSLocked(tasks)
		s.mu.Unlock()
		return 0, ctx.Err()
	}

	notifyDedupSkipped()

	return jobID, nil
}

func (s *searchScheduler) loop() {
	var coalesce <-chan time.Time

	for {
		if coalesce != nil {
			select {
			case batch := <-s.submitCh:
				s.enqueueTasks(batch)
			case <-s.completeCh:
				s.dispatchTasks()
			case <-s.stopCh:
				return
			case <-coalesce:
				coalesce = nil
				s.dispatchTasks()
			}
			continue
		}

		select {
		case batch := <-s.submitCh:
			s.enqueueTasks(batch)
			coalesce = time.After(dispatchCoalesceDelay)
		case <-s.completeCh:
			s.dispatchTasks()
		case <-s.stopCh:
			return
		}
	}
}

func (s *searchScheduler) enqueueTasks(tasks []workerTask) {
	s.mu.Lock()
	for i := range tasks {
		task := tasks[i]
		heap.Push(&s.taskQueue, &taskItem{
			task:     task,
			priority: jobPriority(task.meta),
			created:  time.Now(),
		})
	}
	s.mu.Unlock()

	if len(tasks) > 0 {
		s.publishActivity(activity.KindIndexerActivity)
	}
}

func (s *searchScheduler) dispatchTasks() {
	s.mu.Lock()

	if s.taskQueue.Len() == 0 {
		s.mu.Unlock()
		return
	}

	items := make([]*taskItem, 0, s.taskQueue.Len())
	for s.taskQueue.Len() > 0 {
		items = append(items, heap.Pop(&s.taskQueue).(*taskItem))
	}

	var blocked []*taskItem
	slotWait := time.Duration(-1)

	var taskCompleted bool
	var historyRecorded bool

	for _, item := range items {
		if item.task.ctx.Err() != nil {
			item.started = time.Now()
			taskCompleted = true
			historyRecorded = s.handleTaskCompleteLocked(item, nil, nil, item.task.ctx.Err()) || historyRecorded
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}
		if err := s.slots.refusal(item.task.indexer, requestSearch); err != nil {
			item.started = time.Now()
			taskCompleted = true
			historyRecorded = s.handleTaskCompleteLocked(item, nil, nil, err) || historyRecorded
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}
		if running, inFlight := s.inFlight[item.task.indexer.ID]; inFlight {
			if !running.detached {
				blocked = append(blocked, item)
				continue
			}
			item.started = time.Now()
			taskCompleted = true
			err := fmt.Errorf("indexer %s is still running a timed-out search", item.task.indexer.Name)
			historyRecorded = s.handleTaskCompleteLocked(item, nil, nil, err) || historyRecorded
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}

		if len(s.workerPool) == cap(s.workerPool) {
			blocked = append(blocked, item)
			continue
		}

		ticket, wait, err := s.slots.tryAcquire(item.task.indexer, requestSearch)
		if err != nil {
			item.started = time.Now()
			taskCompleted = true
			historyRecorded = s.handleTaskCompleteLocked(item, nil, nil, err) || historyRecorded
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}
		if ticket == nil {
			blocked = append(blocked, item)
			if slotWait < 0 || wait < slotWait {
				slotWait = wait
			}
			continue
		}

		item.ticket = ticket
		s.workerPool <- struct{}{}
		s.inFlight[item.task.indexer.ID] = item
		go s.executeTask(item)
	}

	for _, item := range blocked {
		heap.Push(&s.taskQueue, item)
	}

	s.scheduleRetryTimerLocked(slotWait)

	s.mu.Unlock()

	if taskCompleted {
		s.publishActivity(activity.KindIndexerActivity)
	}
	if historyRecorded {
		s.publishActivity(activity.KindSearchHistory)
	}
}

func (s *searchScheduler) executeTask(item *taskItem) {
	task := item.task
	reported := false

	defer func() {
		if !reported {
			err := fmt.Errorf("scheduler worker panic: %v", recover())
			_ = item.ticket.done(task.ctx, err)
			s.reportTaskComplete(item, nil, nil, err)
		}

		s.mu.Lock()
		delete(s.inFlight, task.indexer.ID)
		if task.isRSS && !item.detached {
			delete(s.pendingRSS, task.indexer.ID)
		}
		s.mu.Unlock()
		s.publishActivity(activity.KindIndexerActivity)

		<-s.workerPool

		select {
		case s.completeCh <- struct{}{}:
		default:
		}
	}()

	item.started = time.Now()

	execCtx := task.ctx
	if task.timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(execCtx, task.timeout)
		defer cancel()
	}

	done := make(chan taskExecResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- taskExecResult{err: fmt.Errorf("scheduler worker panic: %v", r)}
			}
		}()

		results, coverage, err := task.exec(execCtx, []*models.TorznabIndexer{task.indexer}, task.params, task.meta)
		done <- taskExecResult{results: results, coverage: coverage, err: err}
	}()

	select {
	case result := <-done:
		err := item.ticket.done(task.ctx, result.err)
		s.reportTaskComplete(item, result.results, result.coverage, err)
		reported = true
	case <-execCtx.Done():
		_ = item.ticket.done(task.ctx, execCtx.Err())
		s.reportTaskComplete(item, nil, nil, execCtx.Err())
		reported = true
		s.publishActivity(activity.KindIndexerActivity)
		s.awaitDetachedExec(item, done)
	}
}

var detachedExecWarnAfter = 30 * time.Second

func (s *searchScheduler) awaitDetachedExec(item *taskItem, done <-chan taskExecResult) {
	warnAfter := detachedExecWarnAfter
	timer := time.NewTimer(warnAfter)
	defer timer.Stop()
	select {
	case result := <-done:
		_ = item.ticket.done(item.task.ctx, result.err)
		return
	case <-timer.C:
	}
	s.mu.Lock()
	item.detached = true
	if item.task.isRSS {
		delete(s.pendingRSS, item.task.indexer.ID)
	}
	s.mu.Unlock()
	select {
	case s.completeCh <- struct{}{}:
	default:
	}
	log.Warn().
		Int("indexerID", item.task.indexer.ID).
		Str("indexer", item.task.indexer.Name).
		Dur("pastDeadline", warnAfter).
		Msg("Torznab search still running after its context ended; indexer stays held and its queued searches fail until it returns")
	result := <-done
	_ = item.ticket.done(item.task.ctx, result.err)
}

func (s *searchScheduler) reportTaskComplete(item *taskItem, results []Result, coverage []int, err error) {
	s.mu.Lock()
	historyRecorded := s.handleTaskCompleteLocked(item, results, coverage, err)
	s.mu.Unlock()

	if historyRecorded {
		s.publishActivity(activity.KindSearchHistory)
	}
}

func (s *searchScheduler) handleTaskCompleteLocked(item *taskItem, results []Result, coverage []int, err error) (historyRecorded bool) {
	task := item.task

	if task.callbacks.OnComplete != nil {
		go task.callbacks.OnComplete(task.jobID, task.indexer, results, coverage, err)
	}

	if s.historyRecorder != nil && (task.meta == nil || !task.meta.skipHistory) {
		completedAt := time.Now()
		entry := SearchHistoryEntry{
			JobID:       task.jobID,
			TaskID:      task.taskID,
			IndexerID:   task.indexer.ID,
			IndexerName: task.indexer.Name,
			StartedAt:   item.started,
			CompletedAt: completedAt,
			DurationMs:  int(completedAt.Sub(item.started).Milliseconds()),
			ResultCount: len(results),
		}

		if task.meta != nil {
			if task.meta.rateLimit != nil {
				entry.Priority = string(task.meta.rateLimit.Priority)
			}
			entry.ContentType = task.meta.contentType.String()
			entry.SearchMode = task.meta.searchMode
			entry.Categories = task.meta.categories
			entry.ReleaseName = task.meta.releaseName
		}
		if entry.Priority == "" {
			entry.Priority = string(RateLimitPriorityBackground)
		}

		if task.params != nil {
			entry.Query = task.params.Get("q")
			if len(task.params) > 0 {
				entry.Params = make(map[string]string, len(task.params))
				for k, v := range task.params {
					if k != "apikey" && len(v) > 0 {
						entry.Params[k] = v[0]
					}
				}
			}
		}

		if err != nil {
			if _, ok := errors.AsType[*RateLimitError](err); ok {
				entry.Status = "rate_limited"
			} else {
				entry.Status = "error"
			}
			entry.ErrorMessage = err.Error()
		} else {
			entry.Status = "success"
		}

		s.historyRecorder.Record(entry)
		historyRecorded = true
	}

	job, exists := s.jobs[task.jobID]
	if !exists {
		return historyRecorded
	}

	job.completedTasks++

	if job.completedTasks >= job.totalTasks {
		if job.callbacks.OnJobDone != nil {
			go job.callbacks.OnJobDone(task.jobID)
		}
		delete(s.jobs, task.jobID)
	}

	return historyRecorded
}

func (s *searchScheduler) scheduleRetryTimerLocked(slotWait time.Duration) {
	if s.taskQueue.Len() == 0 {
		return
	}

	minWait := slotWait
	for i := 0; i < s.taskQueue.Len(); i++ {
		item := s.taskQueue[i]
		if deadline, ok := item.task.ctx.Deadline(); ok {
			untilDeadline := max(time.Until(deadline), 0)
			if minWait < 0 || untilDeadline < minWait {
				minWait = untilDeadline
			}
		}
	}

	if minWait >= 0 {
		if minWait > maxRetryTimerInterval {
			minWait = maxRetryTimerInterval
		}

		if s.retryTimer != nil {
			s.retryTimer.Stop()
		}

		s.retryTimer = time.AfterFunc(minWait, func() {
			select {
			case s.completeCh <- struct{}{}:
			default:
			}
		})
	}
}

func (s *searchScheduler) clearPendingRSSLocked(tasks []workerTask) {
	for _, task := range tasks {
		if task.isRSS && task.indexer != nil {
			delete(s.pendingRSS, task.indexer.ID)
		}
	}
}

func jobPriority(meta *searchContext) int {
	if meta != nil && meta.rateLimit != nil {
		switch meta.rateLimit.Priority {
		case RateLimitPriorityInteractive:
			return searchJobPriorityInteractive
		case RateLimitPriorityRSS:
			return searchJobPriorityRSS
		case RateLimitPriorityCompletion:
			return searchJobPriorityCompletion
		default:
			return searchJobPriorityBackground
		}
	}
	return searchJobPriorityBackground
}

func (s *searchScheduler) nextJobID() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobSeq++
	return s.jobSeq
}

func (s *searchScheduler) nextTaskID() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskSeq++
	return s.taskSeq
}

func (s *searchScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

type SchedulerTaskStatus struct {
	JobID       uint64    `json:"jobId"`
	TaskID      uint64    `json:"taskId"`
	IndexerID   int       `json:"indexerId"`
	IndexerName string    `json:"indexerName"`
	Priority    string    `json:"priority"`
	CreatedAt   time.Time `json:"createdAt"`
	IsRSS       bool      `json:"isRss"`
}

type SchedulerJobStatus struct {
	JobID          uint64 `json:"jobId"`
	TotalTasks     int    `json:"totalTasks"`
	CompletedTasks int    `json:"completedTasks"`
}

type SchedulerStatus struct {
	QueuedTasks   []SchedulerTaskStatus `json:"queuedTasks"`
	InFlightTasks []SchedulerTaskStatus `json:"inFlightTasks"`
	ActiveJobs    []SchedulerJobStatus  `json:"activeJobs"`
	QueueLength   int                   `json:"queueLength"`
	WorkerCount   int                   `json:"workerCount"`
	WorkersInUse  int                   `json:"workersInUse"`
}

func (s *searchScheduler) GetStatus() SchedulerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := SchedulerStatus{
		QueuedTasks:   make([]SchedulerTaskStatus, 0, s.taskQueue.Len()),
		InFlightTasks: make([]SchedulerTaskStatus, 0, len(s.inFlight)),
		ActiveJobs:    make([]SchedulerJobStatus, 0, len(s.jobs)),
		QueueLength:   s.taskQueue.Len(),
		WorkerCount:   cap(s.workerPool),
		WorkersInUse:  len(s.workerPool),
	}

	queued := slices.Clone(s.taskQueue)
	slices.SortFunc(queued, compareTasks)
	for _, item := range queued {
		priority := "background"
		if item.task.meta != nil && item.task.meta.rateLimit != nil {
			priority = string(item.task.meta.rateLimit.Priority)
		}
		status.QueuedTasks = append(status.QueuedTasks, SchedulerTaskStatus{
			JobID:       item.task.jobID,
			TaskID:      item.task.taskID,
			IndexerID:   item.task.indexer.ID,
			IndexerName: item.task.indexer.Name,
			Priority:    priority,
			CreatedAt:   item.created,
			IsRSS:       item.task.isRSS,
		})
	}

	inFlight := slices.Collect(maps.Values(s.inFlight))
	slices.SortFunc(inFlight, func(a, b *taskItem) int {
		if c := a.created.Compare(b.created); c != 0 {
			return c
		}
		return cmp.Compare(a.task.taskID, b.task.taskID)
	})
	for _, item := range inFlight {
		priority := "background"
		if item.task.meta != nil && item.task.meta.rateLimit != nil {
			priority = string(item.task.meta.rateLimit.Priority)
		}
		status.InFlightTasks = append(status.InFlightTasks, SchedulerTaskStatus{
			JobID:       item.task.jobID,
			TaskID:      item.task.taskID,
			IndexerID:   item.task.indexer.ID,
			IndexerName: item.task.indexer.Name,
			Priority:    priority,
			CreatedAt:   item.created,
			IsRSS:       item.task.isRSS,
		})
	}

	for _, jobID := range slices.Sorted(maps.Keys(s.jobs)) {
		job := s.jobs[jobID]
		status.ActiveJobs = append(status.ActiveJobs, SchedulerJobStatus{
			JobID:          jobID,
			TotalTasks:     job.totalTasks,
			CompletedTasks: job.completedTasks,
		})
	}

	return status
}
