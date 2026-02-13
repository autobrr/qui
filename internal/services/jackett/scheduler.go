// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"container/heap"
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/models"
)

// Rate limiting constants and types

const (
	defaultMinRequestInterval = 60 * time.Second
)

// escalationPeriods defines backoff durations for repeated rate limit failures.
// Matches Prowlarr/Sonarr behavior: escalates with consecutive failures, resets on success.
var escalationPeriods = []time.Duration{
	0,               // Level 0: immediate retry
	1 * time.Minute, // Level 1
	5 * time.Minute, // Level 2
	15 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	3 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour, // Level 9 (max)
}

var priorityMultipliers = map[RateLimitPriority]float64{
	RateLimitPriorityInteractive: 0.1,
	RateLimitPriorityRSS:         0.5,
	RateLimitPriorityCompletion:  0.7,
	RateLimitPriorityBackground:  1.0,
}

const (
	// Keep RSS responsive; we'll skip indexers that need more than this wait.
	rssMaxWait = 15 * time.Second
	// Completion searches should not be blocked indefinitely by a single rate-limited/down indexer.
	// Keep this modest so the overall end-to-end completion automation stays bounded even when some
	// indexers are slow or temporarily unavailable.
	completionMaxWait = 30 * time.Second
	// Background jobs are least urgent; allow more wait before skipping.
	backgroundMaxWait = 60 * time.Second
	// Maximum interval for retry timer to prevent starvation when all tasks have long waits.
	maxRetryTimerInterval = 15 * time.Minute

	// Execution timeouts for tasks whose original context deadline expired while queued.
	// These give the task a fresh chance to execute without being penalized for queue wait time.
	rssExecutionTimeout        = 15 * time.Second
	completionExecutionTimeout = 45 * time.Second
	backgroundExecutionTimeout = 60 * time.Second
	defaultExecutionTimeout    = 30 * time.Second
)

type RateLimitPriority string

const (
	RateLimitPriorityInteractive RateLimitPriority = "interactive"
	RateLimitPriorityRSS         RateLimitPriority = "rss"
	RateLimitPriorityCompletion  RateLimitPriority = "completion"
	RateLimitPriorityBackground  RateLimitPriority = "background"
)

type RateLimitOptions struct {
	Priority    RateLimitPriority
	MinInterval time.Duration
	MaxWait     time.Duration
}

type RateLimitWaitError struct {
	IndexerID   int
	IndexerName string
	Wait        time.Duration
	MaxWait     time.Duration
	Priority    RateLimitPriority
}

func (e *RateLimitWaitError) Error() string {
	indexer := fmt.Sprintf("indexer %d", e.IndexerID)
	if e.IndexerName != "" {
		indexer = fmt.Sprintf("%s (%d)", e.IndexerName, e.IndexerID)
	}
	return fmt.Sprintf("%s blocked by torznab rate limit: requires %s wait but maximum allowed is %s", indexer, e.Wait, e.MaxWait)
}

func (e *RateLimitWaitError) Is(target error) bool {
	_, ok := target.(*RateLimitWaitError)
	return ok
}

type indexerRateState struct {
	lastRequest     time.Duration
	cooldownUntil   time.Duration
	escalationLevel int
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

func (r *RateLimiter) RecordRequest(indexerID int, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var dur time.Duration
	if ts.IsZero() {
		dur = time.Since(r.startTime)
	} else {
		dur = ts.Sub(r.startTime)
	}
	r.recordLocked(indexerID, dur)
}

// WaitForMinInterval blocks until a new request slot is available for the given indexer according to the
// configured min interval and priority multiplier, then reserves the slot by recording the request time.
//
// Note: this intentionally does NOT wait for cooldown windows. Callers that care about cooldowns should
// check IsInCooldown separately (downloads typically return immediately when in cooldown).
func (r *RateLimiter) WaitForMinInterval(ctx context.Context, indexer *models.TorznabIndexer, opts *RateLimitOptions) error {
	if r == nil || indexer == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	cfg := r.resolveOptions(opts)

	for {
		r.mu.Lock()
		state := r.getStateLocked(indexer.ID)
		now := time.Since(r.startTime)

		wait := time.Duration(0)
		if cfg.MinInterval > 0 && state.lastRequest >= 0 {
			next := state.lastRequest + cfg.MinInterval
			if next > now {
				wait = next - now
			}
		}

		if wait <= 0 {
			if ctx.Err() != nil {
				r.mu.Unlock()
				return ctx.Err()
			}
			r.recordLocked(indexer.ID, now)
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

func (r *RateLimiter) SetCooldown(indexerID int, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	cooldownDur := until.Sub(r.startTime)
	if cooldownDur > state.cooldownUntil {
		state.cooldownUntil = cooldownDur
	}
}

// LoadCooldowns seeds the rate limiter with pre-existing cooldown windows.
func (r *RateLimiter) LoadCooldowns(cooldowns map[int]time.Time) {
	if len(cooldowns) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for indexerID, until := range cooldowns {
		if until.IsZero() {
			continue
		}
		state := r.getStateLocked(indexerID)
		cooldownDur := until.Sub(r.startTime)
		if cooldownDur > state.cooldownUntil {
			state.cooldownUntil = cooldownDur
		}
	}
}

func (r *RateLimiter) ClearCooldown(indexerID int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	state.cooldownUntil = 0
}

// RecordFailure increments the escalation level and sets a cooldown based on the new level.
// This implements Prowlarr-style escalating backoff for rate limit failures.
func (r *RateLimiter) RecordFailure(indexerID int) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)

	// Increment escalation level (capped at max)
	if state.escalationLevel < len(escalationPeriods)-1 {
		state.escalationLevel++
	}

	cooldown := escalationPeriods[state.escalationLevel]
	if cooldown > 0 {
		now := time.Since(r.startTime)
		state.cooldownUntil = now + cooldown
	}

	return cooldown
}

// RecordSuccess resets the escalation level to 0 on successful request.
func (r *RateLimiter) RecordSuccess(indexerID int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	state.escalationLevel = 0
}

// IsInCooldown checks if an indexer is currently in cooldown without blocking
func (r *RateLimiter) IsInCooldown(indexerID int) (bool, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	now := time.Since(r.startTime)
	if state.cooldownUntil > 0 && state.cooldownUntil > now {
		return true, r.startTime.Add(state.cooldownUntil)
	}
	return false, time.Time{}
}

// GetCooldownIndexers returns a list of indexer IDs that are currently in cooldown
func (r *RateLimiter) GetCooldownIndexers() map[int]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	cooldowns := make(map[int]time.Time)
	now := time.Since(r.startTime)
	for indexerID, state := range r.states {
		if state.cooldownUntil > 0 && state.cooldownUntil > now {
			cooldowns[indexerID] = r.startTime.Add(state.cooldownUntil)
		}
	}
	return cooldowns
}

func (r *RateLimiter) computeWaitLocked(indexer *models.TorznabIndexer, now time.Duration, minInterval time.Duration) time.Duration {
	if minInterval <= 0 {
		minInterval = r.minInterval
	}
	state := r.getStateLocked(indexer.ID)

	var wait time.Duration

	if state.cooldownUntil > 0 && state.cooldownUntil > now {
		wait = state.cooldownUntil - now
	}

	if minInterval > 0 && state.lastRequest >= 0 {
		next := state.lastRequest + minInterval
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
		state = &indexerRateState{lastRequest: -1}
		r.states[indexerID] = state
	}
	return state
}

func (r *RateLimiter) recordLocked(indexerID int, ts time.Duration) {
	state := r.getStateLocked(indexerID)
	state.lastRequest = ts
}

// NextWait returns the amount of time the caller would need to wait before a request could be made
// against the provided indexer using the supplied options. This is a non-blocking helper used by
// the job scheduler to decide if a request can run immediately.
func (r *RateLimiter) NextWait(indexer *models.TorznabIndexer, opts *RateLimitOptions) time.Duration {
	if indexer == nil {
		return 0
	}
	cfg := r.resolveOptions(opts)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Since(r.startTime)
	return r.computeWaitLocked(indexer, now, cfg.MinInterval)
}

func (r *RateLimiter) resolveOptions(opts *RateLimitOptions) RateLimitOptions {
	cfg := RateLimitOptions{
		Priority:    RateLimitPriorityBackground,
		MinInterval: r.minInterval,
	}

	if opts != nil {
		if opts.Priority != "" {
			cfg.Priority = opts.Priority
		}
		if opts.MinInterval > 0 {
			cfg.MinInterval = opts.MinInterval
		}
		if opts.MaxWait > 0 {
			cfg.MaxWait = opts.MaxWait
		}
	}

	if cfg.MinInterval <= 0 {
		cfg.MinInterval = defaultMinRequestInterval
	}

	if multiplier, ok := priorityMultipliers[cfg.Priority]; ok {
		cfg.MinInterval = time.Duration(float64(cfg.MinInterval) * multiplier)
	}

	return cfg
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
	Indexers  []*models.TorznabIndexer
	Params    url.Values
	Meta      *searchContext
	Callbacks JobCallbacks
	ExecFn    func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)
}

type workerTask struct {
	jobID     uint64
	taskID    uint64
	indexer   *models.TorznabIndexer
	params    url.Values
	meta      *searchContext
	exec      func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)
	ctx       context.Context
	ctxCancel context.CancelFunc // Set when we create a fresh context for expired-deadline tasks
	callbacks JobCallbacks
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

func (h taskHeap) Len() int { return len(h) }
func (h taskHeap) Less(i, j int) bool {
	if h[i].priority == h[j].priority {
		return h[i].created.Before(h[j].created)
	}
	return h[i].priority < h[j].priority
}
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

	for _, idx := range req.Indexers {
		if idx == nil {
			continue
		}

		// RSS deduplication
		if req.Meta != nil && req.Meta.rateLimit != nil && req.Meta.rateLimit.Priority == RateLimitPriorityRSS {
			s.mu.Lock()
			if _, exists := s.pendingRSS[idx.ID]; exists {
				s.mu.Unlock()
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
			isRSS:     req.Meta != nil && req.Meta.rateLimit != nil && req.Meta.rateLimit.Priority == RateLimitPriorityRSS,
		})
	}

	if len(tasks) == 0 {
		// All indexers filtered out - call OnJobDone immediately
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
}

func (s *searchScheduler) dispatchTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.taskQueue.Len() == 0 {
		return
	}

	// Drain heap to classify tasks
	items := make([]*taskItem, 0, s.taskQueue.Len())
	for s.taskQueue.Len() > 0 {
		items = append(items, heap.Pop(&s.taskQueue).(*taskItem))
	}

	var blocked []*taskItem

	for _, item := range items {
		// Check if context was explicitly cancelled (user/system stopped the search)
		if item.task.ctx.Err() == context.Canceled {
			item.started = time.Now() // Mark as "started" now for skipped tasks
			s.handleTaskCompleteLocked(item, nil, nil, item.task.ctx.Err())
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}

		// If context deadline expired while queued (not explicit cancellation),
		// give the task a fresh context so it can still execute.
		// This prevents tasks from failing just because they waited in queue.
		// Only do this once (check ctxCancel == nil to avoid creating multiple contexts).
		if item.task.ctx.Err() == context.DeadlineExceeded && item.task.ctxCancel == nil {
			timeout := s.getExecutionTimeout(item)
			item.task.ctx, item.task.ctxCancel = context.WithTimeout(context.Background(), timeout)
		}

		// If we already gave this task a fresh context and it expired too, fail it.
		// This prevents infinite retries for tasks that can't complete in time.
		if item.task.ctx.Err() != nil && item.task.ctxCancel != nil {
			item.task.ctxCancel()
			item.started = time.Now()
			s.handleTaskCompleteLocked(item, nil, nil, item.task.ctx.Err())
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

		var rlOpts *RateLimitOptions
		if item.task.meta != nil {
			rlOpts = item.task.meta.rateLimit
		}

		wait := s.rateLimiter.NextWait(item.task.indexer, rlOpts)
		maxWait := s.getMaxWait(item)

		if maxWait > 0 && wait > maxWait {
			// Skip - exceeds budget
			var priority RateLimitPriority
			if rlOpts != nil {
				priority = rlOpts.Priority
			}
			err := &RateLimitWaitError{
				IndexerID:   item.task.indexer.ID,
				IndexerName: item.task.indexer.Name,
				Wait:        wait,
				MaxWait:     maxWait,
				Priority:    priority,
			}
			item.started = time.Now() // Mark as "started" now for skipped tasks
			s.handleTaskCompleteLocked(item, nil, nil, err)
			if item.task.isRSS {
				delete(s.pendingRSS, item.task.indexer.ID)
			}
			continue
		}

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
}

func (s *searchScheduler) executeTask(item *taskItem) {
	task := item.task
	var panicked bool

	defer func() {
		// Cancel fresh context if one was created for this task
		if task.ctxCancel != nil {
			task.ctxCancel()
		}

		if r := recover(); r != nil {
			panicked = true
			err := fmt.Errorf("scheduler worker panic: %v", r)
			s.mu.Lock()
			s.handleTaskCompleteLocked(item, nil, nil, err)
			delete(s.inFlight, task.indexer.ID)
			if task.isRSS {
				delete(s.pendingRSS, task.indexer.ID)
			}
			s.mu.Unlock()
		}

		// Release worker
		<-s.workerPool

		// Notify loop - may unblock other tasks
		select {
		case s.completeCh <- struct{}{}:
		default:
		}
	}()

	// Record request BEFORE execution (reserve slot)
	s.rateLimiter.RecordRequest(task.indexer.ID, time.Time{})

	// Capture start time for duration tracking
	item.started = time.Now()

	// Execute the search
	results, coverage, err := task.exec(task.ctx, []*models.TorznabIndexer{task.indexer}, task.params, task.meta)

	// Handle completion (only if we didn't panic)
	if !panicked {
		s.mu.Lock()
		s.handleTaskCompleteLocked(item, results, coverage, err)
		delete(s.inFlight, task.indexer.ID)
		if task.isRSS {
			delete(s.pendingRSS, task.indexer.ID)
		}
		s.mu.Unlock()
	}
}

// handleTaskCompleteLocked handles task completion. Caller must hold s.mu.
func (s *searchScheduler) handleTaskCompleteLocked(item *taskItem, results []Result, coverage []int, err error) {
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
			if _, isRateLimit := asRateLimitWaitError(err); isRateLimit {
				entry.Status = "rate_limited"
			} else {
				entry.Status = "error"
			}
			entry.ErrorMessage = err.Error()
		} else {
			entry.Status = "success"
		}

		// Record asynchronously to avoid blocking
		go s.historyRecorder.Record(entry)
	}

	// Track job completion
	job, exists := s.jobs[task.jobID]
	if !exists {
		return
	}

	job.completedTasks++

	if job.completedTasks >= job.totalTasks {
		// Job complete - call OnJobDone
		if job.callbacks.OnJobDone != nil {
			go job.callbacks.OnJobDone(task.jobID)
		}
		delete(s.jobs, task.jobID)
	}
}

func (s *searchScheduler) getMaxWait(item *taskItem) time.Duration {
	// Explicit MaxWait takes precedence
	if item.task.meta != nil && item.task.meta.rateLimit != nil && item.task.meta.rateLimit.MaxWait > 0 {
		return item.task.meta.rateLimit.MaxWait
	}

	// Apply priority-based defaults
	if item.task.meta != nil && item.task.meta.rateLimit != nil {
		switch item.task.meta.rateLimit.Priority {
		case RateLimitPriorityRSS:
			return rssMaxWait
		case RateLimitPriorityCompletion:
			return completionMaxWait
		case RateLimitPriorityBackground:
			return backgroundMaxWait
		case RateLimitPriorityInteractive:
			return 0 // No limit - interactive waits as long as needed
		}
	}

	return 0
}

// getExecutionTimeout returns the appropriate timeout for task execution based on priority.
// Used when a task's original context deadline expired while queued.
func (s *searchScheduler) getExecutionTimeout(item *taskItem) time.Duration {
	if item.task.meta != nil && item.task.meta.rateLimit != nil {
		switch item.task.meta.rateLimit.Priority {
		case RateLimitPriorityRSS:
			return rssExecutionTimeout
		case RateLimitPriorityCompletion:
			return completionExecutionTimeout
		case RateLimitPriorityBackground:
			return backgroundExecutionTimeout
		case RateLimitPriorityInteractive:
			// Interactive tasks shouldn't be queued, but if they are, use default
			return defaultExecutionTimeout
		}
	}
	return defaultExecutionTimeout
}

func (s *searchScheduler) scheduleRetryTimerLocked() {
	if s.taskQueue.Len() == 0 {
		return
	}

	// Find minimum wait among queued tasks
	var minWait time.Duration
	for i := 0; i < s.taskQueue.Len(); i++ {
		item := s.taskQueue[i]
		var rlOpts *RateLimitOptions
		if item.task.meta != nil {
			rlOpts = item.task.meta.rateLimit
		}
		wait := s.rateLimiter.NextWait(item.task.indexer, rlOpts)
		if wait > 0 && (minWait == 0 || wait < minWait) {
			minWait = wait
		}
	}

	if minWait > 0 {
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

	// Collect queued tasks
	for i := 0; i < s.taskQueue.Len(); i++ {
		item := s.taskQueue[i]
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

	// Collect in-flight tasks with full details
	for _, item := range s.inFlight {
		if item != nil {
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
	}

	// Collect active jobs
	for jobID, job := range s.jobs {
		status.ActiveJobs = append(status.ActiveJobs, SchedulerJobStatus{
			JobID:          jobID,
			TotalTasks:     job.totalTasks,
			CompletedTasks: job.completedTasks,
		})
	}

	return status
}
