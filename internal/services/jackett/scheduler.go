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

// searchJobPriority defines execution ordering for queued searches.
const (
	searchJobPriorityInteractive = 0
	searchJobPriorityRSS         = 1
	searchJobPriorityCompletion  = 2
	searchJobPriorityBackground  = 3
)

type workerTask struct {
	jobID   uint64
	taskID  uint64
	indexer *models.TorznabIndexer
	params  url.Values
	meta    *searchContext
	exec    func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)
	ctx     context.Context
	respCh  chan workerResult
	onStart func(jobID uint64, indexerID int) context.Context
	onDone  func(jobID uint64, indexerID int, err error)
	isRSS   bool
}

type workerResult struct {
	jobID    uint64
	indexer  int
	results  []Result
	coverage []int
	err      error
}

type indexerWorker struct {
	indexerID   int
	tasks       chan workerTask
	rateLimiter *RateLimiter
}

type taskItem struct {
	task     workerTask
	priority int
	created  time.Time
	index    int
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
func (h *taskHeap) Push(x interface{}) {
	item := x.(*taskItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *taskHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// searchScheduler coordinates Torznab searches so we can queue thousands of indexer tasks while
// keeping one goroutine per indexer and shared clients.
type searchScheduler struct {
	mu          sync.Mutex
	workers     map[int]*indexerWorker
	rateLimiter *RateLimiter

	taskQueue  taskHeap
	pendingRSS map[int]struct{}

	submitCh   chan []workerTask
	completeCh chan workerResult
	stopCh     chan struct{}

	jobSeq  uint64
	taskSeq uint64
}

func newSearchScheduler() *searchScheduler {
	s := &searchScheduler{
		workers:    make(map[int]*indexerWorker),
		pendingRSS: make(map[int]struct{}),
		submitCh:   make(chan []workerTask, 128),
		completeCh: make(chan workerResult, 128),
		stopCh:     make(chan struct{}),
	}
	heap.Init(&s.taskQueue)
	go s.loop()
	return s
}

// Submit enqueues all indexer tasks for this search and waits for all to finish.
func (s *searchScheduler) Submit(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext, exec func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error), onReady func(jobID uint64, indexerID int) context.Context, onComplete func(jobID uint64, indexerID int, err error), resultCallback func([]Result, []int, error)) error {
	if len(indexers) == 0 {
		resultCallback(nil, nil, nil)
		return nil
	}

	jobID := s.nextJobID()
	respCh := make(chan workerResult, len(indexers))
	tasks := make([]workerTask, 0, len(indexers))
	for _, idx := range indexers {
		if meta != nil && meta.rateLimit != nil && meta.rateLimit.Priority == RateLimitPriorityRSS {
			if idx != nil {
				s.mu.Lock()
				if _, exists := s.pendingRSS[idx.ID]; exists {
					s.mu.Unlock()
					continue
				}
				s.pendingRSS[idx.ID] = struct{}{}
				s.mu.Unlock()
			}
		}
		tasks = append(tasks, workerTask{
			jobID:   jobID,
			taskID:  s.nextTaskID(),
			indexer: idx,
			params:  cloneValues(params),
			meta:    meta,
			exec:    exec,
			ctx:     ctx,
			respCh:  respCh,
			onStart: onReady,
			onDone:  onComplete,
			isRSS:   meta != nil && meta.rateLimit != nil && meta.rateLimit.Priority == RateLimitPriorityRSS,
		})
	}

	if len(tasks) == 0 {
		resultCallback(nil, nil, nil)
		return nil
	}

	select {
	case s.submitCh <- tasks:
	case <-ctx.Done():
		resultCallback(nil, nil, ctx.Err())
		return ctx.Err()
	}

	go func() {
		var (
			results  []Result
			coverage []int
			failures int
			lastErr  error
		)

		for i := 0; i < len(tasks); i++ {
			select {
			case <-ctx.Done():
				safeInvokeResultCallback(resultCallback, results, coverage, ctx.Err())
				s.clearPendingRSS(tasks)
				return
			case res := <-respCh:
				if res.err != nil {
					failures++
					lastErr = res.err
					continue
				}
				if len(res.coverage) > 0 {
					coverage = append(coverage, res.coverage...)
				}
				if len(res.results) > 0 {
					results = append(results, res.results...)
				}
			}
		}

		if failures == len(tasks) && lastErr != nil && len(results) == 0 {
			s.clearPendingRSS(tasks)
			safeInvokeResultCallback(resultCallback, nil, coverage, lastErr)
			return
		}

		s.clearPendingRSS(tasks)
		safeInvokeResultCallback(resultCallback, results, coverageSetToSlice(sliceToSet(coverage)), nil)
	}()

	return nil
}

func (s *searchScheduler) loop() {
	for {
		s.dispatchTasks()

		select {
		case batch := <-s.submitCh:
			s.enqueueTasks(batch)
		case res := <-s.completeCh:
			s.markWorkerFree(res.indexer)
		case <-s.stopCh:
			return
		default:
			select {
			case batch := <-s.submitCh:
				s.enqueueTasks(batch)
			case res := <-s.completeCh:
				s.markWorkerFree(res.indexer)
			case <-s.stopCh:
				return
			}
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
	for {
		s.mu.Lock()
		if len(s.taskQueue) == 0 {
			s.mu.Unlock()
			return
		}
		item := heap.Pop(&s.taskQueue).(*taskItem)
		s.mu.Unlock()
		worker := s.getWorker(item.task.indexer)
		if worker == nil {
			continue
		}
		select {
		case worker.tasks <- item.task:
		default:
			// worker queue full; requeue and stop dispatching
			s.mu.Lock()
			heap.Push(&s.taskQueue, item)
			s.mu.Unlock()
			return
		}
	}
}

func (s *searchScheduler) getWorker(indexer *models.TorznabIndexer) *indexerWorker {
	if indexer == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if w, ok := s.workers[indexer.ID]; ok {
		return w
	}
	w := &indexerWorker{
		indexerID:   indexer.ID,
		tasks:       make(chan workerTask, 8),
		rateLimiter: s.rateLimiter,
	}
	s.workers[indexer.ID] = w
	go w.run(s.completeCh)
	return w
}

func (w *indexerWorker) run(done chan<- workerResult) {
	for task := range w.tasks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("scheduler worker panic: %v", r)
					task.respCh <- workerResult{jobID: task.jobID, indexer: w.indexerID, err: err}
					done <- workerResult{jobID: task.jobID, indexer: w.indexerID}
				}
			}()

			if !task.isRSS && task.ctx.Err() != nil {
				task.respCh <- workerResult{jobID: task.jobID, indexer: w.indexerID, err: task.ctx.Err()}
				done <- workerResult{jobID: task.jobID, indexer: w.indexerID}
				return
			}
			searchCtx := safeInvokeStart(task.onStart, task.jobID, w.indexerID)
			if searchCtx == nil {
				searchCtx = task.ctx
			}
			results, coverage, err := task.exec(searchCtx, []*models.TorznabIndexer{task.indexer}, task.params, task.meta)
			safeInvokeDone(task.onDone, task.jobID, w.indexerID, err)
			task.respCh <- workerResult{
				jobID:    task.jobID,
				indexer:  w.indexerID,
				results:  results,
				coverage: coverage,
				err:      err,
			}
			done <- workerResult{jobID: task.jobID, indexer: w.indexerID}
		}()
	}
}

func (s *searchScheduler) markWorkerFree(indexerID int) {
	// placeholder for future per-indexer cleanup
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

func sliceToSet(ids []int) map[int]struct{} {
	set := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func (s *searchScheduler) clearPendingRSS(tasks []workerTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range tasks {
		if task.isRSS && task.indexer != nil {
			delete(s.pendingRSS, task.indexer.ID)
		}
	}
}

func safeInvokeStart(fn func(jobID uint64, indexerID int) context.Context, jobID uint64, indexerID int) context.Context {
	if fn == nil {
		return nil
	}
	defer func() {
		_ = recover()
	}()
	return fn(jobID, indexerID)
}

func safeInvokeDone(fn func(jobID uint64, indexerID int, err error), jobID uint64, indexerID int, err error) {
	if fn == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	fn(jobID, indexerID, err)
}

func safeInvokeResultCallback(fn func([]Result, []int, error), results []Result, coverage []int, err error) {
	if fn == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	fn(results, coverage, err)
}
