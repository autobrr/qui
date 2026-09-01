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

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

// Rate limiting constants and types

const (
	defaultMinRequestInterval = 60 * time.Second
)

const (
	// Maximum interval for retry timer to prevent starvation when all tasks have long waits.
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

// errRSSDeduplicated marks an indexer skipped because an identical RSS fetch is
// already pending. It is delivered via OnComplete so callers waiting on a
// per-indexer completion (e.g. searchIndexersWithScheduler's WaitGroup) still
// see the indexer finish instead of blocking forever.
var errRSSDeduplicated = errors.New("rss search deduplicated: identical fetch already pending")

type indexerRateState struct {
	lastCompleted time.Duration
	cooldowns     map[string]time.Duration
}

type taskExecResult struct {
	results  []Result
	coverage []int
	err      error
}

// RateLimiter tracks per-indexer request timing and computes wait times.
// It does not block; the scheduler queries it to make dispatch decisions.
type RateLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	states      map[int]*indexerRateState
	startTime   time.Time
}

func NewRateLimiter(minInterval time.Duration) *RateLimiter {
	if minInterval <= 0 {
		minInterval = defaultMinRequestInterval
	}
	return &RateLimiter{
		minInterval: minInterval,
		states:      make(map[int]*indexerRateState),
		startTime:   time.Now(),
	}
}

func (r *RateLimiter) setMinInterval(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.minInterval = d
}

func (r *RateLimiter) RecordRequestComplete(indexerID int, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var dur time.Duration
	if ts.IsZero() {
		dur = time.Since(r.startTime)
	} else {
		dur = ts.Sub(r.startTime)
	}
	state := r.getStateLocked(indexerID)
	state.lastCompleted = dur
}

// WaitForMinInterval blocks until a native Torznab request slot is available,
// then reserves it for callers that do not report completion separately.
//
// Note: this intentionally does NOT wait for cooldown windows. Callers that care about cooldowns should
// check IsInCooldown separately (downloads typically return immediately when in cooldown).
func (r *RateLimiter) WaitForMinInterval(ctx context.Context, indexer *models.TorznabIndexer) error {
	if r == nil || indexer == nil || indexer.Backend != models.TorznabBackendNative {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	for {
		r.mu.Lock()
		state := r.getStateLocked(indexer.ID)
		now := time.Since(r.startTime)

		wait := time.Duration(0)
		if r.minInterval > 0 && state.lastCompleted >= 0 {
			next := state.lastCompleted + r.minInterval
			if next > now {
				wait = next - now
			}
		}

		if wait <= 0 {
			if ctx.Err() != nil {
				r.mu.Unlock()
				return ctx.Err()
			}
			state.lastCompleted = now
			r.mu.Unlock()
			return nil
		}
		r.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *RateLimiter) SetCooldown(indexerID int, scope string, until time.Time) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	cooldownDur := until.Sub(r.startTime)
	if cooldownDur > state.cooldowns[scope] {
		state.cooldowns[scope] = cooldownDur
	}
	return r.startTime.Add(state.cooldowns[scope])
}

// IsInCooldown checks if an indexer is currently in cooldown without blocking
func (r *RateLimiter) IsInCooldown(indexerID int, scope string) (bool, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	now := time.Since(r.startTime)
	until := state.cooldowns[scope]
	if until > now {
		return true, r.startTime.Add(until)
	}
	return false, time.Time{}
}

// GetCooldownIndexers returns a list of indexer IDs that are currently in cooldown
func (r *RateLimiter) GetCooldownIndexers(scope string) map[int]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	cooldowns := make(map[int]time.Time)
	now := time.Since(r.startTime)
	for indexerID, state := range r.states {
		until := state.cooldowns[scope]
		if until > now {
			cooldowns[indexerID] = r.startTime.Add(until)
		}
	}
	return cooldowns
}

func (r *RateLimiter) computeWaitLocked(indexer *models.TorznabIndexer, now time.Duration, minInterval time.Duration) time.Duration {
	if indexer.Backend != models.TorznabBackendNative {
		return 0
	}
	if minInterval <= 0 {
		minInterval = r.minInterval
	}
	state := r.getStateLocked(indexer.ID)

	var wait time.Duration

	if minInterval > 0 && state.lastCompleted >= 0 {
		next := state.lastCompleted + minInterval
		if next > now {
			delay := next - now
			if delay > wait {
				wait = delay
			}
		}
	}

	return wait
}

func (r *RateLimiter) getStateLocked(indexerID int) *indexerRateState {
	state, ok := r.states[indexerID]
	if !ok {
		state = &indexerRateState{lastCompleted: -1, cooldowns: make(map[string]time.Duration)}
		r.states[indexerID] = state
	}
	return state
}

// NextWait returns the amount of time the caller would need to wait before a request could be made
// against the provided indexer using the supplied options. This is a non-blocking helper used by
// the job scheduler to decide if a request can run immediately.
func (r *RateLimiter) NextWait(indexer *models.TorznabIndexer) time.Duration {
	if indexer == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Since(r.startTime)
	return r.computeWaitLocked(indexer, now, r.minInterval)
}

// Scheduler constants and types

// searchJobPriority defines execution ordering for queued searches.
const (
	searchJobPriorityInteractive = 0 // runs on interactive searches
	searchJobPriorityRSS         = 1 // runs on RSS feeds (CrossSeedPage.tsx)
	searchJobPriorityCompletion  = 2 // runs on torrent completion unless the tag cross-seed is present
	searchJobPriorityBackground  = 3 // runs on seeded searches (CrossSeedPage.tsx)
)

const (
	dispatchCoalesceDelay = 5 * time.Millisecond
	defaultMaxWorkers     = 10
)

// JobCallbacks defines the callbacks for async job completion.
type JobCallbacks struct {
	// OnComplete fires for each indexer when it finishes (success or error).
	// Called in a goroutine - safe to do blocking work.
	OnComplete func(jobID uint64, indexer *models.TorznabIndexer, results []Result, coverage []int, err error)
	// OnJobDone fires once after ALL indexers complete (optional, can be nil).
	// Called in a goroutine - safe to do blocking work.
	OnJobDone func(jobID uint64)
}

// SubmitRequest contains all parameters for submitting a job to the scheduler.
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
	started  time.Time // When execution began (for duration tracking)
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

// compareTasks orders tasks the way the scheduler dequeues them: highest
// priority first, then oldest, with the task ID to break ties.
func compareTasks(a, b *taskItem) int {
	if c := cmp.Compare(a.priority, b.priority); c != 0 {
		return c
	}
	if c := a.created.Compare(b.created); c != 0 {
		return c
	}
	return cmp.Compare(a.task.taskID, b.task.taskID)
}

// jobState tracks completion status for a multi-indexer job.
type jobState struct {
	totalTasks     int
	completedTasks int
	callbacks      JobCallbacks
}

// searchScheduler coordinates Torznab searches with dispatch-time rate limiting.
// Jobs are submitted asynchronously and results delivered via callbacks.
type searchScheduler struct {
	mu          sync.Mutex
	rateLimiter *RateLimiter

	taskQueue  taskHeap
	pendingRSS map[int]struct{}

	// Worker pool semaphore - limits concurrent executions
	workerPool chan struct{}
	// Track in-flight tasks per indexer (with full task details for status)
	inFlight map[int]*taskItem
	// Track job completion state
	jobs map[uint64]*jobState

	submitCh   chan []workerTask
	completeCh chan struct{}
	stopCh     chan struct{}
	stopOnce   sync.Once

	jobSeq  uint64
	taskSeq uint64

	// retryTimer tracks the current scheduled retry
	retryTimer *time.Timer

	// historyRecorder records completed searches for history tracking
	historyRecorder HistoryRecorder

	// activityPublisher signals connected clients when the scheduler's visible
	// activity changes (task enqueue/completion, search-history append). Stored
	// atomically so it can be wired after construction without locking the hot path.
	activityPublisher atomic.Pointer[activity.Publisher]
}

// setActivityPublisher wires the server-event hub. Safe to call once at startup.
func (s *searchScheduler) setActivityPublisher(publisher activity.Publisher) {
	if s == nil || publisher == nil {
		return
	}
	s.activityPublisher.Store(&publisher)
}

// publishActivity emits a coarse activity signal if a publisher is wired.
// Must not be called while holding s.mu.
func (s *searchScheduler) publishActivity(kind activity.Kind) {
	if s == nil {
		return
	}
	if p := s.activityPublisher.Load(); p != nil && *p != nil {
		(*p).Publish(activity.Event{Kind: kind})
	}
}

func newSearchScheduler(rl *RateLimiter, maxWorkers int) *searchScheduler {
	if rl == nil {
		rl = NewRateLimiter(defaultMinRequestInterval)
	}
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxWorkers
	}
	s := &searchScheduler{
		pendingRSS:  make(map[int]struct{}),
		rateLimiter: rl,
		workerPool:  make(chan struct{}, maxWorkers),
		inFlight:    make(map[int]*taskItem),
		jobs:        make(map[uint64]*jobState),
		submitCh:    make(chan []workerTask, 128),
		completeCh:  make(chan struct{}, 128),
		stopCh:      make(chan struct{}),
	}
	heap.Init(&s.taskQueue)
	go s.loop()
	return s
}

// Submit enqueues all indexer tasks for this search and returns immediately.
// Results are delivered via the callbacks in the request.
func (s *searchScheduler) Submit(ctx context.Context, req SubmitRequest) (uint64, error) {
	if len(req.Indexers) == 0 {
		// No indexers - call OnJobDone immediately if provided
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

		// RSS deduplication
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

	// Deliver a completion for every RSS-deduplicated indexer so callers that
	// track per-indexer completion (via OnComplete) do not wait on it forever.
	// The skipped indexer's pendingRSS entry belongs to the earlier in-flight
	// job, so it is intentionally left untouched here.
	notifyDedupSkipped := func() {
		if req.Callbacks.OnComplete == nil {
			return
		}
		for _, idx := range dedupSkipped {
			go req.Callbacks.OnComplete(jobID, idx, nil, nil, errRSSDeduplicated)
		}
	}

	if len(tasks) == 0 {
		// All indexers filtered out - call OnJobDone immediately
		notifyDedupSkipped()
		if req.Callbacks.OnJobDone != nil {
			go req.Callbacks.OnJobDone(jobID)
		}
		return jobID, nil
	}

	// Register job state for tracking completion
	s.mu.Lock()
	s.jobs[jobID] = &jobState{
		totalTasks:     len(tasks),
		completedTasks: 0,
		callbacks:      req.Callbacks,
	}
	s.mu.Unlock()

	// Submit tasks - non-blocking
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

	// A new batch of work entered the visible queue. Published after releasing
	// the lock so the publisher never blocks the scheduler.
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

	// Drain heap to classify tasks
	items := make([]*taskItem, 0, s.taskQueue.Len())
	for s.taskQueue.Len() > 0 {
		items = append(items, heap.Pop(&s.taskQueue).(*taskItem))
	}

	var blocked []*taskItem

	// Track whether any task completed (so we emit a single queue-state signal)
	// and whether any of them recorded a search-history entry, so we can emit the
	// matching activity signals once after releasing s.mu. Both consuming panels
	// rely on these events instead of polling.
	var taskCompleted bool
	var historyRecorded bool

	for _, item := range items {
		if item.task.ctx.Err() != nil {
			item.started = time.Now() // Mark as "started" now for skipped tasks
			taskCompleted = true
			historyRecorded = s.handleTaskCompleteLocked(item, nil, nil, item.task.ctx.Err()) || historyRecorded
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}
		if inCooldown, retryAt := s.rateLimiter.IsInCooldown(item.task.indexer.ID, rateLimitScopeQuery); inCooldown {
			item.started = time.Now()
			taskCompleted = true
			err := &RateLimitError{
				IndexerID:   item.task.indexer.ID,
				IndexerName: item.task.indexer.Name,
				Scope:       rateLimitScopeQuery,
				RetryAt:     retryAt,
			}
			historyRecorded = s.handleTaskCompleteLocked(item, nil, nil, err) || historyRecorded
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}

		// Check if indexer already has in-flight task
		if _, inFlight := s.inFlight[item.task.indexer.ID]; inFlight {
			blocked = append(blocked, item)
			continue
		}

		wait := s.rateLimiter.NextWait(item.task.indexer)

		if wait > 0 {
			// Blocked - re-queue for later
			blocked = append(blocked, item)
			continue
		}

		// Ready - try to acquire worker
		select {
		case s.workerPool <- struct{}{}:
			s.inFlight[item.task.indexer.ID] = item
			go s.executeTask(item)
		default:
			// No worker available - re-queue
			blocked = append(blocked, item)
		}
	}

	// Re-queue blocked items
	for _, item := range blocked {
		heap.Push(&s.taskQueue, item)
	}

	// Schedule retry timer for blocked tasks
	s.scheduleRetryTimerLocked()

	s.mu.Unlock()

	// Tasks completed here (cancelled or deadline-exceeded)
	// without going through executeTask, so their activity signals must be emitted
	// from this path. Published after releasing the lock so the publisher never
	// blocks the scheduler. Mirrors the success path in executeTask.
	if taskCompleted {
		s.publishActivity(activity.KindIndexerActivity)
	}
	if historyRecorded {
		s.publishActivity(activity.KindSearchHistory)
	}
}

func (s *searchScheduler) executeTask(item *taskItem) {
	task := item.task
	var panicked bool
	var requestCompleted bool

	defer func() {
		if !requestCompleted {
			s.rateLimiter.RecordRequestComplete(task.indexer.ID, time.Time{})
		}

		if r := recover(); r != nil {
			panicked = true
			err := fmt.Errorf("scheduler worker panic: %v", r)
			s.mu.Lock()
			historyRecorded := s.handleTaskCompleteLocked(item, nil, nil, err)
			delete(s.inFlight, task.indexer.ID)
			if task.isRSS {
				delete(s.pendingRSS, task.indexer.ID)
			}
			s.mu.Unlock()

			// A task finished (via panic recovery), changing the scheduler's visible
			// activity. Emitted after releasing the lock, mirroring the success path.
			s.publishActivity(activity.KindIndexerActivity)
			if historyRecorded {
				s.publishActivity(activity.KindSearchHistory)
			}
		}

		// Release worker
		<-s.workerPool

		// Notify loop - may unblock other tasks
		select {
		case s.completeCh <- struct{}{}:
		default:
		}
	}()

	// Capture start time for duration tracking
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

	var (
		results  []Result
		coverage []int
		err      error
	)
	select {
	case result := <-done:
		results = result.results
		coverage = result.coverage
		err = result.err
	case <-execCtx.Done():
		err = execCtx.Err()
	}
	s.rateLimiter.RecordRequestComplete(task.indexer.ID, time.Time{})
	requestCompleted = true

	// Handle completion (only if we didn't panic)
	if !panicked {
		s.mu.Lock()
		historyRecorded := s.handleTaskCompleteLocked(item, results, coverage, err)
		delete(s.inFlight, task.indexer.ID)
		if task.isRSS {
			delete(s.pendingRSS, task.indexer.ID)
		}
		s.mu.Unlock()

		// A task finished, changing the scheduler's visible activity. Published
		// after releasing the lock so the publisher never blocks the scheduler.
		s.publishActivity(activity.KindIndexerActivity)
		if historyRecorded {
			s.publishActivity(activity.KindSearchHistory)
		}
	}
}

// handleTaskCompleteLocked handles task completion. Caller must hold s.mu.
// Returns true when a search-history entry was recorded so the caller can emit
// the corresponding activity signal after releasing the lock.
func (s *searchScheduler) handleTaskCompleteLocked(item *taskItem, results []Result, coverage []int, err error) (historyRecorded bool) {
	task := item.task

	// Call OnComplete callback
	if task.callbacks.OnComplete != nil {
		go task.callbacks.OnComplete(task.jobID, task.indexer, results, coverage, err)
	}

	// Record search history (unless explicitly skipped, e.g., for connectivity tests)
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

		// Extract search context
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

		// Extract query and params
		if task.params != nil {
			entry.Query = task.params.Get("q")
			// Capture all params sent to the indexer (excluding apikey)
			if len(task.params) > 0 {
				entry.Params = make(map[string]string, len(task.params))
				for k, v := range task.params {
					if k != "apikey" && len(v) > 0 {
						entry.Params[k] = v[0]
					}
				}
			}
		}

		// Determine status
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

		// Record synchronously so the entry is committed before the caller emits
		// the KindSearchHistory activity signal after releasing s.mu; otherwise a
		// client refetch can race ahead of the write and miss the new row, and the
		// panel no longer polls to self-heal. Record -> buffer.Push is an in-memory,
		// non-blocking append, so this does not stall the worker.
		s.historyRecorder.Record(entry)
		historyRecorded = true
	}

	// Track job completion
	job, exists := s.jobs[task.jobID]
	if !exists {
		return historyRecorded
	}

	job.completedTasks++

	if job.completedTasks >= job.totalTasks {
		// Job complete - call OnJobDone
		if job.callbacks.OnJobDone != nil {
			go job.callbacks.OnJobDone(task.jobID)
		}
		delete(s.jobs, task.jobID)
	}

	return historyRecorded
}

func (s *searchScheduler) scheduleRetryTimerLocked() {
	if s.taskQueue.Len() == 0 {
		return
	}

	// Find minimum wait among queued tasks
	minWait := time.Duration(-1)
	for i := 0; i < s.taskQueue.Len(); i++ {
		item := s.taskQueue[i]
		wait := s.rateLimiter.NextWait(item.task.indexer)
		if wait > 0 && (minWait < 0 || wait < minWait) {
			minWait = wait
		}
		if deadline, ok := item.task.ctx.Deadline(); ok {
			untilDeadline := max(time.Until(deadline), 0)
			if minWait < 0 || untilDeadline < minWait {
				minWait = untilDeadline
			}
		}
	}

	if minWait >= 0 {
		// Clamp to maximum interval to prevent starvation when all tasks have long waits
		if minWait > maxRetryTimerInterval {
			minWait = maxRetryTimerInterval
		}

		// Cancel existing timer if any
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

// Stop gracefully shuts down the scheduler. Safe to call multiple times.
func (s *searchScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// SchedulerTaskStatus represents a single task's status
type SchedulerTaskStatus struct {
	JobID       uint64    `json:"jobId"`
	TaskID      uint64    `json:"taskId"`
	IndexerID   int       `json:"indexerId"`
	IndexerName string    `json:"indexerName"`
	Priority    string    `json:"priority"`
	CreatedAt   time.Time `json:"createdAt"`
	IsRSS       bool      `json:"isRss"`
}

// SchedulerJobStatus represents a job's completion status
type SchedulerJobStatus struct {
	JobID          uint64 `json:"jobId"`
	TotalTasks     int    `json:"totalTasks"`
	CompletedTasks int    `json:"completedTasks"`
}

// SchedulerStatus represents the current state of the scheduler
type SchedulerStatus struct {
	QueuedTasks   []SchedulerTaskStatus `json:"queuedTasks"`
	InFlightTasks []SchedulerTaskStatus `json:"inFlightTasks"`
	ActiveJobs    []SchedulerJobStatus  `json:"activeJobs"`
	QueueLength   int                   `json:"queueLength"`
	WorkerCount   int                   `json:"workerCount"`
	WorkersInUse  int                   `json:"workersInUse"`
}

// GetStatus returns the current state of the scheduler
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

	// Collect queued tasks by priority and age; the heap array is not sorted.
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

	// Collect in-flight tasks with full details, oldest first.
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

	// Collect active jobs, lowest job ID first.
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
