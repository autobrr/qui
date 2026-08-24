// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"container/heap"
	"context"
	"errors"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

// recordingPublisher captures published activity events for assertions.
type recordingPublisher struct {
	mu     sync.Mutex
	events []activity.Event
}

func (p *recordingPublisher) Publish(ev activity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *recordingPublisher) counts() map[activity.Kind]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := make(map[activity.Kind]int)
	for _, ev := range p.events {
		counts[ev.Kind]++
	}
	return counts
}

// recordingHistoryRecorder captures recorded search-history entries.
type recordingHistoryRecorder struct {
	mu      sync.Mutex
	entries []SearchHistoryEntry
}

func (r *recordingHistoryRecorder) Record(entry SearchHistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}

func (r *recordingHistoryRecorder) statuses() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.Status
	}
	return out
}

// taskOutcome carries what a scheduler callback saw back to the test goroutine.
type taskOutcome struct {
	results []Result
	err     error
}

func TestSearchScheduler_BasicFunctionality(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var executed atomic.Bool
	done := make(chan struct{})

	exec := func(_ context.Context, indexers []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		executed.Store(true)
		return []Result{{Title: "test"}}, []int{indexers[0].ID}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	// Callbacks run on a scheduler worker, so they hand their outcome to the
	// test goroutine: a failed require there would Goexit the worker instead of
	// failing the test, and OnJobDone signals from a different goroutine again.
	completions := make(chan taskOutcome, 1)

	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, results []Result, _ []int, err error) {
				completions <- taskOutcome{results: results, err: err}
			},
			OnJobDone: func(jobID uint64) {
				close(done)
			},
		},
	})

	require.NoError(t, err)
	<-done
	got := <-completions
	require.NoError(t, got.err)
	require.Len(t, got.results, 1)
	assert.Equal(t, "test", got.results[0].Title)
	assert.True(t, executed.Load())
}

func TestSearchScheduler_PriorityOrdering(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 1) // Single worker to force sequential execution
	defer s.Stop()

	var executedTasks []RateLimitPriority
	var execMu sync.Mutex
	var completed atomic.Int32
	done := make(chan struct{})

	exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, meta *searchContext) ([]Result, []int, error) {
		execMu.Lock()
		defer execMu.Unlock()
		if meta != nil && meta.rateLimit != nil {
			executedTasks = append(executedTasks, meta.rateLimit.Priority)
		}
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// Use different indexers
	indexer1 := &models.TorznabIndexer{ID: 1, Name: "indexer1"}
	indexer2 := &models.TorznabIndexer{ID: 2, Name: "indexer2"}

	callback := func(jobID uint64) {
		if completed.Add(1) == 2 {
			close(done)
		}
	}

	// Submit background priority first
	_, err1 := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer1},
		Meta:     &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityBackground}},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnJobDone: callback,
		},
	})

	// Submit interactive priority second
	_, err2 := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer2},
		Meta:     &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityInteractive}},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnJobDone: callback,
		},
	})

	require.NoError(t, err1)
	require.NoError(t, err2)

	<-done

	execMu.Lock()
	defer execMu.Unlock()

	// Interactive should execute before background due to higher priority (lower number)
	require.Len(t, executedTasks, 2)
	assert.Equal(t, RateLimitPriorityInteractive, executedTasks[0])
	assert.Equal(t, RateLimitPriorityBackground, executedTasks[1])
}

func TestSearchScheduler_WorkerPoolLimit(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 2) // Only 2 workers
	defer s.Stop()

	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32
	var completed atomic.Int32
	done := make(chan struct{})

	exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		current := currentConcurrent.Add(1)
		for {
			peak := maxConcurrent.Load()
			if current <= peak {
				break
			}
			if maxConcurrent.CompareAndSwap(peak, current) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		currentConcurrent.Add(-1)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// Submit 5 tasks with different indexers
	for i := range 5 {
		indexer := &models.TorznabIndexer{ID: i, Name: "indexer"}
		_, err := s.Submit(context.Background(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{indexer},
			ExecFn:   exec,
			Callbacks: JobCallbacks{
				OnJobDone: func(jobID uint64) {
					if completed.Add(1) == 5 {
						close(done)
					}
				},
			},
		})
		require.NoError(t, err)
	}

	<-done

	// Max concurrent should be limited to 2 (worker pool size)
	assert.LessOrEqual(t, maxConcurrent.Load(), int32(2))
}

func TestSearchScheduler_ContextCancellation(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var started atomic.Bool
	exec := func(ctx context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		started.Store(true)
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return []Result{{Title: "test"}}, []int{1}, nil
		}
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	var gotErr error
	_, err := s.Submit(ctx, SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				gotErr = err
				close(done)
			},
		},
	})

	require.NoError(t, err)

	// Wait for task to start
	for !started.Load() {
		time.Sleep(1 * time.Millisecond)
	}

	// Cancel context
	cancel()

	<-done
	require.ErrorIs(t, gotErr, context.Canceled)
}

func TestSearchScheduler_WorkerPanicRecovery(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var completed atomic.Int32
	done := make(chan struct{})

	// Both callbacks run on scheduler workers; they report over a channel and
	// leave every assertion to the test goroutine.
	panicked := make(chan taskOutcome, 1)
	recovered := make(chan taskOutcome, 1)

	// Exec that panics for indexer 1, succeeds for indexer 2
	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		if len(indexers) > 0 && indexers[0].ID == 1 {
			panic("test panic")
		}
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer1 := &models.TorznabIndexer{ID: 1, Name: "test-indexer-1"}
	indexer2 := &models.TorznabIndexer{ID: 2, Name: "test-indexer-2"}

	// First submission should panic
	_, err1 := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer1},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				panicked <- taskOutcome{err: err}
				if completed.Add(1) == 2 {
					close(done)
				}
			},
		},
	})
	require.NoError(t, err1)

	// Second submission should succeed (scheduler should recover)
	_, err2 := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer2},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, results []Result, _ []int, err error) {
				recovered <- taskOutcome{results: results, err: err}
				if completed.Add(1) == 2 {
					close(done)
				}
			},
		},
	})
	require.NoError(t, err2)

	<-done
	gotPanic := <-panicked
	require.Error(t, gotPanic.err)
	assert.Contains(t, gotPanic.err.Error(), "scheduler worker panic")

	gotRecovered := <-recovered
	require.NoError(t, gotRecovered.err)
	assert.Len(t, gotRecovered.results, 1)
}

func TestSearchScheduler_TaskTimeoutCompletesHungExecution(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	parentCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	started := make(chan struct{})
	completeCh := make(chan error, 1)
	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		close(started)
		time.Sleep(200 * time.Millisecond)
		return []Result{{Title: "late"}}, []int{1}, nil
	}

	start := time.Now()
	_, err := s.Submit(parentCtx, SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				completeCh <- err
			},
		},
	})
	require.NoError(t, err)

	<-started
	callbackErr := <-completeCh
	require.ErrorIs(t, callbackErr, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 150*time.Millisecond)
}

func TestSearchScheduler_FreshTaskKeepsOriginalContextDeadline(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	deadlineCh := make(chan bool, 1)
	done := make(chan struct{})
	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	exec := func(ctx context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		_, hasDeadline := ctx.Deadline()
		deadlineCh <- hasDeadline
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	_, err := s.Submit(ctx, SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnJobDone: func(uint64) {
				close(done)
			},
		},
	})
	require.NoError(t, err)

	<-done
	require.True(t, <-deadlineCh)
}

func TestSearchScheduler_RSSDeduplication(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 1) // Single worker
	defer s.Stop()

	var executions atomic.Int32
	var completed atomic.Int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		time.Sleep(100 * time.Millisecond) // Make it slow so deduplication can happen
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}
	rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}

	callback := func(jobID uint64) {
		if completed.Add(1) == 2 {
			close(done)
		}
	}

	// Submit first RSS search
	_, err1 := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		Meta:      rssMeta,
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: callback},
	})
	require.NoError(t, err1)

	// Submit second RSS search to same indexer - should be deduplicated
	_, err2 := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		Meta:      rssMeta,
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: callback},
	})
	require.NoError(t, err2)

	<-done

	// Only first search should have executed
	assert.Equal(t, int32(1), executions.Load())
}

func TestSearchScheduler_RSSDeduplicationInvokesOnComplete(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 1) // Single worker
	defer s.Stop()

	var executions atomic.Int32
	var startOnce sync.Once
	firstExecStarted := make(chan struct{})
	releaseFirst := make(chan struct{})

	exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		startOnce.Do(func() { close(firstExecStarted) })
		<-releaseFirst
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}
	rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}

	// First RSS search occupies pendingRSS[1] until released.
	firstDone := make(chan struct{})
	_, err1 := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		Meta:      rssMeta,
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: func(uint64) { close(firstDone) }},
	})
	require.NoError(t, err1)
	<-firstExecStarted

	// Second RSS search to the same indexer is fully deduplicated. The deduped
	// indexer must still report an OnComplete so WaitGroup-based callers finish.
	type completion struct {
		jobID     uint64
		indexerID int
		results   []Result
		coverage  []int
		err       error
	}
	completeCh := make(chan completion, 1)
	secondJobDone := make(chan struct{})
	jobID2, err2 := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		Meta:     rssMeta,
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				id := 0
				if idx != nil {
					id = idx.ID
				}
				completeCh <- completion{jobID: jobID, indexerID: id, results: results, coverage: coverage, err: err}
			},
			OnJobDone: func(uint64) { close(secondJobDone) },
		},
	})
	require.NoError(t, err2)

	select {
	case got := <-completeCh:
		assert.Equal(t, jobID2, got.jobID)
		assert.Equal(t, 1, got.indexerID)
		assert.Nil(t, got.results)
		assert.Nil(t, got.coverage)
		require.ErrorIs(t, got.err, errRSSDeduplicated)
	case <-time.After(2 * time.Second):
		t.Fatal("deduplicated indexer never reported OnComplete")
	}
	<-secondJobDone

	// Release the first search and let it finish.
	close(releaseFirst)
	<-firstDone

	// Only the first search should have executed.
	assert.Equal(t, int32(1), executions.Load())
}

func TestSearchScheduler_EmptySubmission(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	done := make(chan struct{})
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnJobDone: func(jobID uint64) {
				close(done)
			},
		},
	})

	require.NoError(t, err)
	<-done // Should complete immediately
}

func TestSearchScheduler_NilIndexerHandling(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	done := make(chan struct{})
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{nil},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnJobDone: func(jobID uint64) {
				close(done)
			},
		},
	})

	require.NoError(t, err)
	<-done // Should complete immediately since nil indexer is filtered
}

func TestSearchScheduler_ConcurrentSubmissions(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var executions atomic.Int32
	var completed atomic.Int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		time.Sleep(10 * time.Millisecond)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	const numGoroutines = 10
	const tasksPerGoroutine = 5

	var wg sync.WaitGroup
	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range tasksPerGoroutine {
				indexer := &models.TorznabIndexer{ID: id*10 + j, Name: "indexer"}
				_, err := s.Submit(context.Background(), SubmitRequest{
					Indexers: []*models.TorznabIndexer{indexer},
					ExecFn:   exec,
					Callbacks: JobCallbacks{
						OnJobDone: func(jobID uint64) {
							if completed.Add(1) == numGoroutines*tasksPerGoroutine {
								close(done)
							}
						},
					},
				})
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()
	<-done

	assert.Equal(t, int32(numGoroutines*tasksPerGoroutine), executions.Load())
}

func TestSearchScheduler_MultipleIndexersPerSubmission(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 10)
	defer s.Stop()

	var executedIndexers []string
	var execMu sync.Mutex

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		execMu.Lock()
		defer execMu.Unlock()
		executedIndexers = append(executedIndexers, indexers[0].Name)
		return []Result{{Title: "test"}}, []int{indexers[0].ID}, nil
	}

	indexers := []*models.TorznabIndexer{
		{ID: 1, Name: "indexer1"},
		{ID: 2, Name: "indexer2"},
		{ID: 3, Name: "indexer3"},
	}

	// Use WaitGroup to wait for all OnComplete callbacks
	// since OnComplete and OnJobDone both run as goroutines and may race
	var wg sync.WaitGroup
	wg.Add(len(indexers))

	var completedCount atomic.Int32
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: indexers,
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				completedCount.Add(1)
				wg.Done()
			},
		},
	})

	require.NoError(t, err)
	wg.Wait()

	execMu.Lock()
	defer execMu.Unlock()

	assert.Len(t, executedIndexers, 3)
	assert.Equal(t, int32(3), completedCount.Load())
}

func TestSearchScheduler_HeapOrderingCorrectness(t *testing.T) {
	h := &taskHeap{}
	heap.Init(h)

	now := time.Now()

	// Add tasks with different priorities
	heap.Push(h, &taskItem{priority: 3, created: now.Add(1 * time.Hour)}) // Background
	heap.Push(h, &taskItem{priority: 0, created: now.Add(2 * time.Hour)}) // Interactive
	heap.Push(h, &taskItem{priority: 1, created: now.Add(3 * time.Hour)}) // RSS
	heap.Push(h, &taskItem{priority: 0, created: now.Add(4 * time.Hour)}) // Interactive (later)

	// Should pop in priority order, then by creation time
	item1 := heap.Pop(h).(*taskItem)
	assert.Equal(t, 0, item1.priority) // First interactive

	item2 := heap.Pop(h).(*taskItem)
	assert.Equal(t, 0, item2.priority) // Second interactive

	item3 := heap.Pop(h).(*taskItem)
	assert.Equal(t, 1, item3.priority) // RSS

	item4 := heap.Pop(h).(*taskItem)
	assert.Equal(t, 3, item4.priority) // Background

	assert.Equal(t, 0, h.Len())
}

func TestSearchScheduler_RateLimitPriorityMapping(t *testing.T) {
	tests := []struct {
		rateLimitPriority         RateLimitPriority
		expectedSchedulerPriority int
	}{
		{RateLimitPriorityInteractive, searchJobPriorityInteractive},
		{RateLimitPriorityRSS, searchJobPriorityRSS},
		{RateLimitPriorityCompletion, searchJobPriorityCompletion},
		{RateLimitPriorityBackground, searchJobPriorityBackground},
	}

	for _, tt := range tests {
		t.Run(string(tt.rateLimitPriority), func(t *testing.T) {
			meta := &searchContext{rateLimit: &RateLimitOptions{Priority: tt.rateLimitPriority}}
			priority := jobPriority(meta)
			assert.Equal(t, tt.expectedSchedulerPriority, priority)
		})
	}

	// Test nil cases
	assert.Equal(t, searchJobPriorityBackground, jobPriority(nil))
	assert.Equal(t, searchJobPriorityBackground, jobPriority(&searchContext{}))
	assert.Equal(t, searchJobPriorityBackground, jobPriority(&searchContext{rateLimit: &RateLimitOptions{}}))
}

func TestSearchScheduler_JobAndTaskIDGeneration(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	id1 := s.nextJobID()
	id2 := s.nextJobID()
	assert.Equal(t, uint64(1), id1)
	assert.Equal(t, uint64(2), id2)

	tid1 := s.nextTaskID()
	tid2 := s.nextTaskID()
	assert.Equal(t, uint64(1), tid1)
	assert.Equal(t, uint64(2), tid2)
}

func TestSearchScheduler_ErrorPropagation(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	expectedErr := errors.New("test error")
	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return nil, nil, expectedErr
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	// Use channel to wait for OnComplete specifically (not OnJobDone)
	// since both callbacks run as goroutines and may race
	completeCh := make(chan error, 1)
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				completeCh <- err
			},
		},
	})

	require.NoError(t, err)
	callbackErr := <-completeCh
	assert.Equal(t, expectedErr, callbackErr)
}

func TestSearchScheduler_RateLimitIntervalStartsAfterCompletion(t *testing.T) {
	rl := NewRateLimiter(80 * time.Millisecond)
	s := newSearchScheduler(rl, 10)
	defer s.Stop()

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}
	var calls atomic.Int32

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		if calls.Add(1) == 1 {
			time.Sleep(100 * time.Millisecond)
		}
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	done1 := make(chan struct{})
	start1 := time.Now()
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: func(jobID uint64) { close(done1) }},
	})
	require.NoError(t, err)
	<-done1
	elapsed1 := time.Since(start1)
	assert.GreaterOrEqual(t, elapsed1, 100*time.Millisecond)

	done2 := make(chan struct{})
	start2 := time.Now()
	_, err = s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: func(jobID uint64) { close(done2) }},
	})
	require.NoError(t, err)
	<-done2
	elapsed2 := time.Since(start2)
	assert.Greater(t, elapsed2, 70*time.Millisecond)
}

func TestSearchScheduler_MaxWaitSkipsIndexer(t *testing.T) {
	// Use a long interval (5 seconds) with background priority (1.0 multiplier)
	// so we're guaranteed to need to wait
	rl := NewRateLimiter(5 * time.Second)
	s := newSearchScheduler(rl, 10)
	defer s.Stop()

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// First request to set rate limit state
	done1 := make(chan struct{})
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: func(jobID uint64) { close(done1) }},
	})
	require.NoError(t, err)
	<-done1

	// Verify rate limiter recorded the request
	wait := rl.NextWait(indexer, &RateLimitOptions{Priority: RateLimitPriorityBackground})
	t.Logf("After first request, NextWait returns: %v", wait)

	// Second request with short MaxWait should be skipped
	completeCh := make(chan error, 1)
	_, err = s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		Meta: &searchContext{
			rateLimit: &RateLimitOptions{
				Priority: RateLimitPriorityBackground, // 1.0 multiplier
				MaxWait:  10 * time.Millisecond,       // Very short max wait (5s wait > 10ms max)
			},
		},
		ExecFn: exec,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				completeCh <- err
			},
		},
	})
	require.NoError(t, err)

	// Wait for OnComplete callback
	gotError := <-completeCh

	// Should have received a RateLimitWaitError
	require.Error(t, gotError, "expected RateLimitWaitError but got nil")
	var waitErr *RateLimitWaitError
	assert.ErrorAs(t, gotError, &waitErr)
}

func TestSearchScheduler_DefaultMaxWaitByPriority(t *testing.T) {
	// Use a very long interval so we're always blocked (must exceed backgroundMaxWait of 60s)
	rl := NewRateLimiter(90 * time.Second)
	s := newSearchScheduler(rl, 10)
	defer s.Stop()

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// First request to set rate limit state
	done1 := make(chan struct{})
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: func(jobID uint64) { close(done1) }},
	})
	require.NoError(t, err)
	<-done1

	// Test RSS and Background - they should skip immediately
	skipTests := []struct {
		name            string
		priority        RateLimitPriority
		expectedMaxWait time.Duration
	}{
		{
			name:            "RSS uses 15s default, should skip (90s wait > 15s max)",
			priority:        RateLimitPriorityRSS,
			expectedMaxWait: 15 * time.Second,
		},
		{
			name:            "Completion uses 30s default, should skip (90s wait > 30s max)",
			priority:        RateLimitPriorityCompletion,
			expectedMaxWait: 30 * time.Second,
		},
		{
			name:            "Background uses 60s default, should skip (90s wait > 60s max)",
			priority:        RateLimitPriorityBackground,
			expectedMaxWait: 60 * time.Second,
		},
	}

	for _, tc := range skipTests {
		t.Run(tc.name, func(t *testing.T) {
			completeCh := make(chan error, 1)
			_, err := s.Submit(context.Background(), SubmitRequest{
				Indexers: []*models.TorznabIndexer{indexer},
				Meta: &searchContext{
					rateLimit: &RateLimitOptions{
						Priority: tc.priority,
					},
				},
				ExecFn: exec,
				Callbacks: JobCallbacks{
					OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
						completeCh <- err
					},
				},
			})
			require.NoError(t, err)

			gotError := <-completeCh
			require.Error(t, gotError, "expected RateLimitWaitError for priority %s", tc.priority)
			var waitErr *RateLimitWaitError
			require.ErrorAs(t, gotError, &waitErr)
			assert.Equal(t, tc.expectedMaxWait, waitErr.MaxWait, "wrong MaxWait for priority %s", tc.priority)
		})
	}
}

// Activity emission tests
//
// Both consuming panels (SearchHistoryPanel, IndexerActivityPanel) disabled
// polling and rely on the scheduler emitting KindIndexerActivity and
// KindSearchHistory whenever a task completes. These tests lock in emission for
// completion paths that previously stayed silent: the rate-limit skip in
// dispatchTasks, and a panicking exec (recovered per-task) still routing its
// completion through the emit.

func TestSearchScheduler_RateLimitSkipEmitsActivity(t *testing.T) {
	// Long interval with background priority guarantees the second request is
	// skipped for exceeding its MaxWait budget, exercising the dispatchTasks
	// rate-limit-skip completion path.
	rl := NewRateLimiter(5 * time.Second)
	s := newSearchScheduler(rl, 10)
	defer s.Stop()

	pub := &recordingPublisher{}
	rec := &recordingHistoryRecorder{}
	s.setActivityPublisher(pub)
	s.historyRecorder = rec

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}
	exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// First request sets rate-limit state and completes successfully. Its own
	// completion already emits both signals, so the skip path below must be measured
	// as a DELTA on top of this baseline, not as an absolute count.
	done1 := make(chan struct{})
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers:  []*models.TorznabIndexer{indexer},
		ExecFn:    exec,
		Callbacks: JobCallbacks{OnJobDone: func(uint64) { close(done1) }},
	})
	require.NoError(t, err)
	<-done1

	// KindSearchHistory is emitted only by completion paths, never by enqueue, so a
	// settled count of 1 after the first (successful) request is a stable baseline.
	require.Eventually(t, func() bool {
		return pub.counts()[activity.KindSearchHistory] >= 1
	}, time.Second, 5*time.Millisecond, "first completion should emit a search-history signal")
	before := pub.counts()

	// Second request with a tiny MaxWait is skipped as rate_limited.
	completeCh := make(chan error, 1)
	_, err = s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		Meta: &searchContext{
			rateLimit: &RateLimitOptions{
				Priority: RateLimitPriorityBackground,
				MaxWait:  10 * time.Millisecond,
			},
		},
		ExecFn: exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				completeCh <- err
			},
		},
	})
	require.NoError(t, err)

	gotErr := <-completeCh
	var waitErr *RateLimitWaitError
	require.ErrorAs(t, gotErr, &waitErr)

	require.Eventually(t, func() bool {
		return slices.Contains(rec.statuses(), "rate_limited")
	}, time.Second, 5*time.Millisecond, "rate_limited history entry should be recorded")

	// The skip path must emit its OWN completion signals: the search-history count has
	// to rise above the post-first-request baseline. Without the dispatchTasks
	// skip-path emit, KindSearchHistory stays at `before` and this fails
	// (fail-without-fix). The search-history delta is load-bearing here because
	// KindIndexerActivity is also bumped by the second request's enqueue.
	require.Eventually(t, func() bool {
		after := pub.counts()
		return after[activity.KindSearchHistory] > before[activity.KindSearchHistory] &&
			after[activity.KindIndexerActivity] > before[activity.KindIndexerActivity]
	}, time.Second, 5*time.Millisecond, "rate-limit skip must emit its own indexer-activity and search-history signals")
}

// TestSearchScheduler_ExecPanicStillEmitsActivity verifies that an exec which
// panics does not silently swallow the activity signals the panels depend on. The
// panic is caught by executeTask's inner per-task recover(), converted to an error
// result, and routed through the normal completion emit, so both signals must
// still fire. (The outer worker-level recover() in executeTask emits the same way
// for the near-impossible case of a panic outside the exec goroutine; that branch
// is covered by inspection since its only triggers are unmockable internals.)
func TestSearchScheduler_ExecPanicStillEmitsActivity(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	pub := &recordingPublisher{}
	rec := &recordingHistoryRecorder{}
	s.setActivityPublisher(pub)
	s.historyRecorder = rec

	indexer := &models.TorznabIndexer{ID: 1, Name: "panic-indexer"}
	exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		panic("boom")
	}

	completeCh := make(chan error, 1)
	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				completeCh <- err
			},
		},
	})
	require.NoError(t, err)

	gotErr := <-completeCh
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "scheduler worker panic")

	require.Eventually(t, func() bool {
		return slices.Contains(rec.statuses(), "error")
	}, time.Second, 5*time.Millisecond, "panicked task should record an error history entry")

	require.Eventually(t, func() bool {
		counts := pub.counts()
		return counts[activity.KindIndexerActivity] > 0 && counts[activity.KindSearchHistory] > 0
	}, time.Second, 5*time.Millisecond, "panic-recovery path must emit both indexer-activity and search-history signals")
}

// Rate limiter tests

func TestRateLimiter_NextWaitRespectsCooldown(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	cooldown := 40 * time.Millisecond
	limiter.SetCooldown(indexer.ID, time.Now().Add(cooldown))

	wait := limiter.NextWait(indexer, nil)
	if wait < 30*time.Millisecond {
		t.Fatalf("expected wait at least 30ms due to cooldown, got %v", wait)
	}
}

func TestRateLimiter_NextWaitRespectsMinInterval(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	limiter.RecordRequestComplete(indexer.ID, time.Now())

	wait := limiter.NextWait(indexer, nil)
	if wait < 40*time.Millisecond {
		t.Fatalf("expected wait at least 40ms due to min interval, got %v", wait)
	}
}

func TestRateLimiter_NextWaitIgnoresStartedUntilCompleted(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	limiter.RecordRequestStart(indexer.ID, time.Now())

	wait := limiter.NextWait(indexer, nil)
	if wait > 0 {
		t.Fatalf("expected zero wait before request completion, got %v", wait)
	}

	limiter.RecordRequestComplete(indexer.ID, time.Now())
	wait = limiter.NextWait(indexer, nil)
	if wait < 40*time.Millisecond {
		t.Fatalf("expected wait after request completion, got %v", wait)
	}
}

func TestRateLimiter_NextWaitReturnsZeroWhenReady(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	// No prior requests - should be ready immediately
	wait := limiter.NextWait(indexer, nil)
	if wait > 0 {
		t.Fatalf("expected zero wait for fresh indexer, got %v", wait)
	}
}

func TestRateLimiter_GetCooldownIndexers(t *testing.T) {
	limiter := NewRateLimiter(time.Millisecond)

	limiter.SetCooldown(1, time.Now().Add(100*time.Millisecond))
	limiter.SetCooldown(2, time.Now().Add(20*time.Millisecond))

	time.Sleep(40 * time.Millisecond)

	cooldowns := limiter.GetCooldownIndexers()

	if _, ok := cooldowns[1]; !ok {
		t.Fatalf("expected indexer 1 to still be in cooldown")
	}
	if _, ok := cooldowns[2]; ok {
		t.Fatalf("expected indexer 2 cooldown to expire")
	}
}

func TestRateLimiter_IsInCooldown(t *testing.T) {
	limiter := NewRateLimiter(time.Millisecond)

	limiter.SetCooldown(1, time.Now().Add(20*time.Millisecond))

	inCooldown, resumeAt := limiter.IsInCooldown(1)
	if !inCooldown {
		t.Fatalf("expected indexer to be in cooldown immediately after SetCooldown")
	}
	if resumeAt.Before(time.Now()) {
		t.Fatalf("expected resumeAt to be in the future")
	}

	time.Sleep(30 * time.Millisecond)

	inCooldown, _ = limiter.IsInCooldown(1)
	if inCooldown {
		t.Fatalf("expected cooldown to expire")
	}
}

func TestRateLimiter_NextWaitWithPriorityMultiplier(t *testing.T) {
	limiter := NewRateLimiter(100 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	limiter.RecordRequestComplete(indexer.ID, time.Now())

	// Interactive priority has 0.1x multiplier, so min interval = 10ms
	opts := &RateLimitOptions{
		Priority: RateLimitPriorityInteractive,
	}

	wait := limiter.NextWait(indexer, opts)
	// With 0.1x multiplier on 100ms, effective interval is 10ms
	if wait > 15*time.Millisecond {
		t.Fatalf("expected short wait due to interactive priority multiplier, got %v", wait)
	}
}

func TestRateLimiter_RecordRequestComplete(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	// Should be ready before recording
	wait := limiter.NextWait(indexer, nil)
	if wait > 0 {
		t.Fatalf("expected zero wait before recording request")
	}

	limiter.RecordRequestComplete(indexer.ID, time.Time{})

	// Should need to wait now
	wait = limiter.NextWait(indexer, nil)
	if wait < 40*time.Millisecond {
		t.Fatalf("expected wait after recording request, got %v", wait)
	}
}

func TestRateLimiter_WaitForMinInterval_ReservesSlot(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	limiter.RecordRequestComplete(indexer.ID, time.Time{})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := limiter.WaitForMinInterval(ctx, indexer, &RateLimitOptions{Priority: RateLimitPriorityBackground}); err != nil {
		t.Fatalf("WaitForMinInterval returned error: %v", err)
	}

	// We just reserved a slot; immediately after, there should be some wait remaining.
	wait := limiter.NextWait(indexer, &RateLimitOptions{Priority: RateLimitPriorityBackground})
	if wait <= 0 {
		t.Fatalf("expected positive wait after reserving slot, got %v", wait)
	}
}

func TestRateLimiter_WaitForMinInterval_IgnoresCooldown(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	limiter.SetCooldown(indexer.ID, time.Now().Add(1*time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := limiter.WaitForMinInterval(ctx, indexer, &RateLimitOptions{Priority: RateLimitPriorityBackground}); err != nil {
		t.Fatalf("WaitForMinInterval returned error: %v", err)
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Fatalf("WaitForMinInterval waited unexpectedly long (cooldown should be ignored)")
	}
}

func TestRateLimiter_ClearCooldown(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Millisecond)

	limiter.SetCooldown(1, time.Now().Add(1*time.Hour))

	inCooldown, _ := limiter.IsInCooldown(1)
	if !inCooldown {
		t.Fatalf("expected indexer to be in cooldown")
	}

	limiter.ClearCooldown(1)

	inCooldown, _ = limiter.IsInCooldown(1)
	if inCooldown {
		t.Fatalf("expected cooldown to be cleared")
	}
}

func TestRateLimiter_LoadCooldowns(t *testing.T) {
	limiter := NewRateLimiter(5 * time.Millisecond)

	cooldowns := map[int]time.Time{
		1: time.Now().Add(100 * time.Millisecond),
		2: time.Now().Add(50 * time.Millisecond),
	}

	limiter.LoadCooldowns(cooldowns)

	inCooldown, _ := limiter.IsInCooldown(1)
	if !inCooldown {
		t.Fatalf("expected indexer 1 to be in cooldown after LoadCooldowns")
	}

	inCooldown, _ = limiter.IsInCooldown(2)
	if !inCooldown {
		t.Fatalf("expected indexer 2 to be in cooldown after LoadCooldowns")
	}
}
