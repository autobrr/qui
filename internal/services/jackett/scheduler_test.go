// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"container/heap"
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchScheduler_BasicFunctionality(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var executed atomic.Bool
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executed.Store(true)
		return []Result{{Title: "test"}}, []int{indexers[0].ID}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	_, err := s.Submit(context.Background(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				assert.NoError(t, err)
				assert.Len(t, results, 1)
				assert.Equal(t, "test", results[0].Title)
			},
			OnJobDone: func(jobID uint64) {
				close(done)
			},
		},
	})

	require.NoError(t, err)
	<-done
	assert.True(t, executed.Load())
}

func TestSearchScheduler_PriorityOrdering(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 1) // Single worker to force sequential execution
	defer s.Stop()

	var executedTasks []RateLimitPriority
	var execMu sync.Mutex
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
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
		if atomic.AddInt32(&completed, 1) == 2 {
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

	var maxConcurrent int32
	var currentConcurrent int32
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		current := atomic.AddInt32(&currentConcurrent, 1)
		for {
			max := atomic.LoadInt32(&maxConcurrent)
			if current > max {
				if atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
					break
				}
			} else {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&currentConcurrent, -1)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// Submit 5 tasks with different indexers
	for i := 0; i < 5; i++ {
		indexer := &models.TorznabIndexer{ID: i, Name: "indexer"}
		_, err := s.Submit(context.Background(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{indexer},
			ExecFn:   exec,
			Callbacks: JobCallbacks{
				OnJobDone: func(jobID uint64) {
					if atomic.AddInt32(&completed, 1) == 5 {
						close(done)
					}
				},
			},
		})
		require.NoError(t, err)
	}

	<-done

	// Max concurrent should be limited to 2 (worker pool size)
	assert.LessOrEqual(t, atomic.LoadInt32(&maxConcurrent), int32(2))
}

func TestSearchScheduler_ContextCancellation(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var started atomic.Bool
	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
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
	_, err := s.Submit(ctx, SubmitRequest{
		Indexers: []*models.TorznabIndexer{indexer},
		ExecFn:   exec,
		Callbacks: JobCallbacks{
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, context.Canceled))
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
}

func TestSearchScheduler_WorkerPanicRecovery(t *testing.T) {
	s := newSearchScheduler(nil, 10)
	defer s.Stop()

	var completed int32
	done := make(chan struct{})

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
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "scheduler worker panic")
				if atomic.AddInt32(&completed, 1) == 2 {
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
			OnComplete: func(jobID uint64, idx *models.TorznabIndexer, results []Result, coverage []int, err error) {
				assert.NoError(t, err)
				assert.Len(t, results, 1)
				if atomic.AddInt32(&completed, 1) == 2 {
					close(done)
				}
			},
		},
	})
	require.NoError(t, err2)

	<-done
}

func TestSearchScheduler_RSSDeduplication(t *testing.T) {
	rl := NewRateLimiter(1 * time.Millisecond)
	s := newSearchScheduler(rl, 1) // Single worker
	defer s.Stop()

	var executions atomic.Int32
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		time.Sleep(100 * time.Millisecond) // Make it slow so deduplication can happen
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}
	rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}

	callback := func(jobID uint64) {
		if atomic.AddInt32(&completed, 1) == 2 {
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
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		time.Sleep(10 * time.Millisecond)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	const numGoroutines = 10
	const tasksPerGoroutine = 5

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				indexer := &models.TorznabIndexer{ID: id*10 + j, Name: "indexer"}
				_, err := s.Submit(context.Background(), SubmitRequest{
					Indexers: []*models.TorznabIndexer{indexer},
					ExecFn:   exec,
					Callbacks: JobCallbacks{
						OnJobDone: func(jobID uint64) {
							if atomic.AddInt32(&completed, 1) == numGoroutines*tasksPerGoroutine {
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

func TestSearchScheduler_DispatchTimeRateLimiting(t *testing.T) {
	rl := NewRateLimiter(100 * time.Millisecond)
	s := newSearchScheduler(rl, 10)
	defer s.Stop()

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// First request should execute immediately
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
	assert.Less(t, elapsed1, 50*time.Millisecond)

	// Second request should be delayed due to rate limiting
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
	// Should have waited for rate limit
	assert.Greater(t, elapsed2, 50*time.Millisecond)
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
	assert.NotNil(t, gotError, "expected RateLimitWaitError but got nil")
	var waitErr *RateLimitWaitError
	assert.True(t, errors.As(gotError, &waitErr))
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
			require.NotNil(t, gotError, "expected RateLimitWaitError for priority %s", tc.priority)
			var waitErr *RateLimitWaitError
			require.True(t, errors.As(gotError, &waitErr))
			assert.Equal(t, tc.expectedMaxWait, waitErr.MaxWait, "wrong MaxWait for priority %s", tc.priority)
		})
	}
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

	// Record a request
	limiter.RecordRequest(indexer.ID, time.Now())

	wait := limiter.NextWait(indexer, nil)
	if wait < 40*time.Millisecond {
		t.Fatalf("expected wait at least 40ms due to min interval, got %v", wait)
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

	// Record a request
	limiter.RecordRequest(indexer.ID, time.Now())

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

func TestRateLimiter_RecordRequest(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	// Should be ready before recording
	wait := limiter.NextWait(indexer, nil)
	if wait > 0 {
		t.Fatalf("expected zero wait before recording request")
	}

	// Record request
	limiter.RecordRequest(indexer.ID, time.Time{})

	// Should need to wait now
	wait = limiter.NextWait(indexer, nil)
	if wait < 40*time.Millisecond {
		t.Fatalf("expected wait after recording request, got %v", wait)
	}
}

func TestRateLimiter_WaitForMinInterval_ReservesSlot(t *testing.T) {
	limiter := NewRateLimiter(50 * time.Millisecond)
	indexer := &models.TorznabIndexer{ID: 1}

	limiter.RecordRequest(indexer.ID, time.Time{})

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
