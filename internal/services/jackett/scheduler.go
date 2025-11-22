package jackett

import (
	"context"
	"net/url"
	"sort"
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

type searchJob struct {
	priority  int
	createdAt time.Time
	ctx       context.Context
	indexers  []*models.TorznabIndexer
	params    url.Values
	meta      *searchContext
	respCh    chan searchJobResult
	exec      func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)
}

type searchJobResult struct {
	job      *searchJob
	results  []Result
	coverage []int
	err      error
}

// searchScheduler coordinates Torznab searches so we can skip over jobs blocked by per-indexer cooldowns.
type searchScheduler struct {
	mu          sync.Mutex
	queue       []*searchJob
	busy        map[int]int
	rateLimiter *RateLimiter

	submitCh   chan *searchJob
	completeCh chan searchJobResult
	stopCh     chan struct{}
}

func newSearchScheduler() *searchScheduler {
	s := &searchScheduler{
		queue:       make([]*searchJob, 0),
		busy:        make(map[int]int),
		rateLimiter: nil,
		submitCh:    make(chan *searchJob, 64),
		completeCh:  make(chan searchJobResult, 64),
		stopCh:      make(chan struct{}),
	}
	go s.loop()
	return s
}

// Submit enqueues the search and blocks until it completes or ctx is cancelled.
func (s *searchScheduler) Submit(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext, exec func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error)) ([]Result, []int, error) {
	job := &searchJob{
		priority:  jobPriority(meta),
		createdAt: time.Now(),
		ctx:       ctx,
		indexers:  indexers,
		params:    params,
		meta:      meta,
		respCh:    make(chan searchJobResult, 1),
		exec:      exec,
	}

	select {
	case s.submitCh <- job:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	select {
	case res := <-job.respCh:
		return res.results, res.coverage, res.err
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (s *searchScheduler) loop() {
	var wake <-chan time.Time

	for {
		runnable, wait := s.nextRunnable()
		if runnable != nil {
			s.startJob(runnable)
			continue
		}

		if wait > 0 {
			wake = time.After(wait)
		} else {
			wake = nil
		}

		select {
		case job := <-s.submitCh:
			if job != nil {
				s.enqueue(job)
			}
		case res := <-s.completeCh:
			s.finishJob(res.job)
			res.job.respCh <- res
		case <-wake:
			// timer woke us up to re-evaluate queue
		case <-s.stopCh:
			return
		}
	}
}

func (s *searchScheduler) enqueue(job *searchJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, job)
	sort.SliceStable(s.queue, func(i, j int) bool {
		if s.queue[i].priority == s.queue[j].priority {
			return s.queue[i].createdAt.Before(s.queue[j].createdAt)
		}
		return s.queue[i].priority < s.queue[j].priority
	})
}

func (s *searchScheduler) startJob(job *searchJob) {
	s.reserve(job.indexers)
	go func() {
		results, coverage, err := job.exec(job.ctx, job.indexers, job.params, job.meta)
		s.completeCh <- searchJobResult{
			job:      job,
			results:  results,
			coverage: coverage,
			err:      err,
		}
	}()
}

func (s *searchScheduler) finishJob(job *searchJob) {
	s.release(job.indexers)
}

func (s *searchScheduler) nextRunnable() (*searchJob, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.queue) == 0 {
		return nil, 0
	}

	now := time.Now()
	var nextWait time.Duration

	for i := 0; i < len(s.queue); i++ {
		job := s.queue[i]
		if job.ctx.Err() != nil {
			// Context cancelled while queued
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			job.respCh <- searchJobResult{job: job, err: job.ctx.Err()}
			i--
			continue
		}

		if s.isBusy(job.indexers) {
			continue
		}

		wait := s.maxWait(job, now)
		if wait <= 0 {
			// runnable
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			return job, 0
		}

		if nextWait == 0 || wait < nextWait {
			nextWait = wait
		}
	}

	return nil, nextWait
}

func (s *searchScheduler) maxWait(job *searchJob, now time.Time) time.Duration {
	if s.rateLimiter == nil {
		return 0
	}
	var maxWait time.Duration
	opts := cloneRateLimitOptions(job.meta)
	for _, idx := range job.indexers {
		wait := s.rateLimiter.NextWait(idx, opts)
		if wait > maxWait {
			maxWait = wait
		}
	}
	return maxWait
}

func (s *searchScheduler) isBusy(indexers []*models.TorznabIndexer) bool {
	if len(indexers) == 0 {
		return false
	}
	for _, idx := range indexers {
		if _, ok := s.busy[idx.ID]; ok {
			return true
		}
	}
	return false
}

func (s *searchScheduler) reserve(indexers []*models.TorznabIndexer) {
	if len(indexers) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, idx := range indexers {
		s.busy[idx.ID]++
	}
}

func (s *searchScheduler) release(indexers []*models.TorznabIndexer) {
	if len(indexers) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, idx := range indexers {
		if s.busy[idx.ID] <= 1 {
			delete(s.busy, idx.ID)
			continue
		}
		s.busy[idx.ID]--
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
