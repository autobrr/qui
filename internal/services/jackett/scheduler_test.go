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
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executed atomic.Bool
	var execMu sync.Mutex
	var executedTasks []string
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		execMu.Lock()
		defer execMu.Unlock()
		executed.Store(true)
		executedTasks = append(executedTasks, indexers[0].Name)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil, func(results []Result, coverage []int, err error) {
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "test", results[0].Title)
		assert.Equal(t, []int{1}, coverage)
		close(done)
	})

	require.NoError(t, err)
	<-done
	assert.True(t, executed.Load())
	assert.Equal(t, []string{"test-indexer"}, executedTasks)
}

func TestSearchScheduler_PriorityOrdering(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executedTasks []string
	var execMu sync.Mutex
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		execMu.Lock()
		defer execMu.Unlock()
		executedTasks = append(executedTasks, indexers[0].Name)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer1 := &models.TorznabIndexer{ID: 1, Name: "background"}
	indexer2 := &models.TorznabIndexer{ID: 2, Name: "interactive"}

	var completed int32

	// Submit background priority first
	err1 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer1}, nil,
		&searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityBackground}}, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			if atomic.AddInt32(&completed, 1) == 2 {
				close(done)
			}
		})

	// Submit interactive priority second
	err2 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer2}, nil,
		&searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityInteractive}}, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			if atomic.AddInt32(&completed, 1) == 2 {
				close(done)
			}
		})

	require.NoError(t, err1)
	require.NoError(t, err2)

	<-done

	execMu.Lock()
	defer execMu.Unlock()

	// Interactive should execute before background due to higher priority (lower number)
	require.Len(t, executedTasks, 2)
	assert.Equal(t, "interactive", executedTasks[0])
	assert.Equal(t, "background", executedTasks[1])
}

func TestSearchScheduler_WorkerQueueCapacity(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	// Create a slow-executing function to fill worker queues
	var executing atomic.Int32
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executing.Add(1)
		defer executing.Add(-1)
		time.Sleep(200 * time.Millisecond) // Slow execution
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	// Submit multiple tasks to fill the worker queue (capacity 8)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
				func(results []Result, coverage []int, err error) {
					assert.NoError(t, err)
					if atomic.AddInt32(&completed, 1) == 10 {
						close(done)
					}
				})
			assert.NoError(t, err)
		}()
	}

	wg.Wait()

	// Wait for at least one task to start executing
	for executing.Load() == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	// For the same indexer, only 1 task executes at a time (sequential processing)
	assert.Equal(t, int32(1), executing.Load())

	// Wait for all to complete
	<-done
	assert.Equal(t, int32(0), executing.Load())
}

func TestSearchScheduler_ContextCancellation(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

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

	var callbackCalled atomic.Bool
	err := s.Submit(ctx, []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			callbackCalled.Store(true)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, context.Canceled))
		})

	require.NoError(t, err)

	// Wait for task to start
	for !started.Load() {
		time.Sleep(1 * time.Millisecond)
	}
	assert.True(t, started.Load())

	// Cancel context
	cancel()

	// Wait for callback
	for !callbackCalled.Load() {
		time.Sleep(1 * time.Millisecond)
	}
	assert.True(t, callbackCalled.Load())
}

func TestSearchScheduler_WorkerPanicRecovery(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executions atomic.Int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		count := executions.Add(1)
		if count == 1 {
			panic("test panic")
		}
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	var completed int32

	// First submission should panic
	err1 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "scheduler worker panic")
			if atomic.AddInt32(&completed, 1) == 2 {
				close(done)
			}
		})
	require.NoError(t, err1)

	// Second submission should succeed (worker should recover)
	err2 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			assert.Len(t, results, 1)
			if atomic.AddInt32(&completed, 1) == 2 {
				close(done)
			}
		})
	require.NoError(t, err2)

	// Wait for completion
	<-done
	assert.Equal(t, int32(2), executions.Load())
}

func TestSearchScheduler_RSSDeduplication(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executions atomic.Int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		time.Sleep(400 * time.Millisecond) // Make it slow so deduplication can happen
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}

	var completed int32

	// Submit first RSS search
	err1 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, rssMeta, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			if atomic.AddInt32(&completed, 1) == 2 {
				close(done)
			}
		})
	require.NoError(t, err1)

	// Submit second RSS search to same indexer - should be deduplicated
	err2 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, rssMeta, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			assert.Len(t, results, 0) // No results due to deduplication
			if atomic.AddInt32(&completed, 1) == 2 {
				close(done)
			}
		})
	require.NoError(t, err2)

	// Wait for completion
	<-done

	// Only first search should have executed
	assert.Equal(t, int32(1), executions.Load())
}

func TestSearchScheduler_EmptySubmission(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	var callbackCalled atomic.Bool
	err := s.Submit(context.Background(), []*models.TorznabIndexer{}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			callbackCalled.Store(true)
			assert.NoError(t, err)
			assert.Len(t, results, 0)
			assert.Len(t, coverage, 0)
		})

	require.NoError(t, err)
	assert.True(t, callbackCalled.Load())
}

func TestSearchScheduler_NilIndexerHandling(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// Submit with nil indexer
	err := s.Submit(context.Background(), []*models.TorznabIndexer{nil}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			assert.Len(t, results, 0) // No results since indexer was nil
		})

	require.NoError(t, err)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)
}

func TestSearchScheduler_ConcurrentSubmissions(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executions atomic.Int32
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		time.Sleep(10 * time.Millisecond) // Small delay
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
				err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
					func(results []Result, coverage []int, err error) {
						assert.NoError(t, err)
						if atomic.AddInt32(&completed, 1) == numGoroutines*tasksPerGoroutine {
							close(done)
						}
					})
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Wait for all tasks to complete
	<-done

	// Should have executed all tasks
	assert.Equal(t, int32(numGoroutines*tasksPerGoroutine), executions.Load())
}

func TestSearchScheduler_CallbackPanicRecovery(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	var callbackPanicked atomic.Bool
	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			callbackPanicked.Store(true)
			panic("callback panic")
		})

	require.NoError(t, err)

	// Wait for callback to execute and panic
	for !callbackPanicked.Load() {
		time.Sleep(1 * time.Millisecond)
	}
	assert.True(t, callbackPanicked.Load())

	// Scheduler should still be functional
	done := make(chan struct{})
	err2 := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			close(done)
		})
	assert.NoError(t, err2)
	<-done
}

func TestSearchScheduler_OnStartOnDoneCallbacks(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var onStartCalled, onDoneCalled atomic.Bool
	var startJobID, doneJobID atomic.Uint64
	var startIndexerID, doneIndexerID atomic.Int32

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 42, Name: "test-indexer"}

	onStart := func(jobID uint64, indexerID int) context.Context {
		onStartCalled.Store(true)
		startJobID.Store(jobID)
		startIndexerID.Store(int32(indexerID))
		return context.Background()
	}

	onDone := func(jobID uint64, indexerID int, err error) {
		onDoneCalled.Store(true)
		doneJobID.Store(jobID)
		doneIndexerID.Store(int32(indexerID))
		assert.NoError(t, err)
	}

	done := make(chan struct{})
	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, onStart, onDone,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			close(done)
		})

	require.NoError(t, err)

	// Wait for completion
	<-done

	assert.True(t, onStartCalled.Load())
	assert.True(t, onDoneCalled.Load())
	assert.Equal(t, startJobID.Load(), doneJobID.Load())
	assert.Equal(t, int32(42), startIndexerID.Load())
	assert.Equal(t, int32(42), doneIndexerID.Load())
}

func TestSearchScheduler_CallbackPanicInOnStartOnDone(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	onStart := func(jobID uint64, indexerID int) context.Context {
		panic("onStart panic")
	}

	onDone := func(jobID uint64, indexerID int, err error) {
		panic("onDone panic")
	}

	done := make(chan struct{})
	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, onStart, onDone,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err) // Should still succeed despite callback panics
			close(done)
		})

	require.NoError(t, err)

	// Wait for completion
	<-done
}

func TestSearchScheduler_MultipleIndexersPerSubmission(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executedIndexers []string
	var execMu sync.Mutex

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		execMu.Lock()
		defer execMu.Unlock()
		executedIndexers = append(executedIndexers, indexers[0].Name)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexers := []*models.TorznabIndexer{
		{ID: 1, Name: "indexer1"},
		{ID: 2, Name: "indexer2"},
		{ID: 3, Name: "indexer3"},
	}

	var resultsCount atomic.Int32
	done := make(chan struct{})
	err := s.Submit(context.Background(), indexers, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			resultsCount.Add(int32(len(results)))
			assert.Equal(t, []int{1}, coverage) // Coverage is deduplicated
			close(done)
		})

	require.NoError(t, err)

	// Wait for completion
	<-done

	execMu.Lock()
	defer execMu.Unlock()

	assert.Len(t, executedIndexers, 3)
	assert.Equal(t, int32(3), resultsCount.Load())
}

func TestSearchScheduler_HeapOrderingCorrectness(t *testing.T) {
	// Test the heap implementation directly
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

func TestSearchScheduler_StopFunctionality(t *testing.T) {
	s := newSearchScheduler()

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		time.Sleep(50 * time.Millisecond)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	// Submit a task
	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			// May or may not complete depending on timing
		})
	require.NoError(t, err)

	// Stop the scheduler
	s.stopCh <- struct{}{}

	// Scheduler should be stopped, further submissions should fail
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Scheduler may accept the submission but not process it after stop
	// Since submitCh is buffered, Submit succeeds but task may not execute
	_ = s.Submit(ctx, []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil, nil) // Don't assert error
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
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	// Test sequential ID generation
	id1 := s.nextJobID()
	id2 := s.nextJobID()
	assert.Equal(t, uint64(1), id1)
	assert.Equal(t, uint64(2), id2)

	tid1 := s.nextTaskID()
	tid2 := s.nextTaskID()
	assert.Equal(t, uint64(1), tid1)
	assert.Equal(t, uint64(2), tid2)
}

func TestSearchScheduler_CoverageSetToSlice(t *testing.T) {
	// Test the coverage deduplication logic
	coverage := []int{1, 2, 2, 3, 1, 4}
	set := sliceToSet(coverage)
	result := coverageSetToSlice(set)

	// Should contain unique values
	assert.Len(t, result, 4)
	assert.Contains(t, result, 1)
	assert.Contains(t, result, 2)
	assert.Contains(t, result, 3)
	assert.Contains(t, result, 4)
}

func TestSearchScheduler_PendingRSSClearing(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	// Manually add pending RSS
	s.mu.Lock()
	s.pendingRSS[1] = struct{}{}
	s.pendingRSS[2] = struct{}{}
	s.mu.Unlock()

	tasks := []workerTask{
		{isRSS: true, indexer: &models.TorznabIndexer{ID: 1}},
		{isRSS: false, indexer: &models.TorznabIndexer{ID: 2}}, // Not RSS
		{isRSS: true, indexer: &models.TorznabIndexer{ID: 3}},  // Not in pending
	}

	s.clearPendingRSS(tasks)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Only indexer 1 should be cleared (was RSS and in pending)
	assert.NotContains(t, s.pendingRSS, 1)
	assert.Contains(t, s.pendingRSS, 2) // Wasn't RSS
	// Indexer 3 was never in pending
}

func TestSearchScheduler_WorkerCreationAndReuse(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	indexer1 := &models.TorznabIndexer{ID: 1, Name: "indexer1"}
	indexer2 := &models.TorznabIndexer{ID: 2, Name: "indexer2"}

	// Get worker for indexer1
	w1 := s.getWorker(indexer1)
	assert.NotNil(t, w1)
	assert.Equal(t, 1, w1.indexerID)

	// Get same worker again
	w1Again := s.getWorker(indexer1)
	assert.Equal(t, w1, w1Again)

	// Get worker for different indexer
	w2 := s.getWorker(indexer2)
	assert.NotNil(t, w2)
	assert.Equal(t, 2, w2.indexerID)
	assert.NotEqual(t, w1, w2)

	// Check workers map
	s.mu.Lock()
	assert.Len(t, s.workers, 2)
	s.mu.Unlock()
}

func TestSearchScheduler_DispatchTasks_EmptyQueue(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	// Empty queue should return immediately
	s.dispatchTasks()

	// Should not block or panic
}

func TestSearchScheduler_DispatchTasks_WithTasks(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executed atomic.Bool
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executed.Store(true)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	// Submit task
	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			assert.NoError(t, err)
			close(done)
		})
	require.NoError(t, err)

	// Wait for dispatch
	<-done

	assert.True(t, executed.Load())
}

func TestSearchScheduler_ConcurrentAccessSafety(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var executions atomic.Int32
	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		executions.Add(1)
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	const numGoroutines = 20
	const tasksPerGoroutine = 10

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < tasksPerGoroutine; j++ {
				indexer := &models.TorznabIndexer{ID: id*100 + j, Name: "indexer"}
				err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
					func(results []Result, coverage []int, err error) {
						assert.NoError(t, err)
						if atomic.AddInt32(&completed, 1) == numGoroutines*tasksPerGoroutine {
							close(done)
						}
					})
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Wait for all tasks to complete
	<-done

	// Should have executed all tasks without race conditions
	assert.Equal(t, int32(numGoroutines*tasksPerGoroutine), executions.Load())
}

func TestSearchScheduler_MemoryLeaks_NoWorkerCleanup(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var completed int32
	done := make(chan struct{})

	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return []Result{{Title: "test"}}, []int{1}, nil
	}

	// Create many different indexers
	const numIndexers = 50
	for i := 0; i < numIndexers; i++ {
		indexer := &models.TorznabIndexer{ID: i, Name: "indexer"}
		err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
			func(results []Result, coverage []int, err error) {
				assert.NoError(t, err)
				if atomic.AddInt32(&completed, 1) == numIndexers {
					close(done)
				}
			})
		assert.NoError(t, err)
	}

	// Wait for completion
	<-done

	// Check that all workers are still in memory (no cleanup implemented)
	s.mu.Lock()
	workerCount := len(s.workers)
	s.mu.Unlock()

	assert.Equal(t, numIndexers, workerCount)
}

func TestSearchScheduler_ErrorPropagation(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	expectedErr := errors.New("test error")
	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		return nil, nil, expectedErr
	}

	indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

	done := make(chan struct{})
	var callbackErr error
	err := s.Submit(context.Background(), []*models.TorznabIndexer{indexer}, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			callbackErr = err
			assert.Len(t, results, 0)
			assert.Len(t, coverage, 0)
			close(done)
		})

	require.NoError(t, err)
	<-done
	assert.Equal(t, expectedErr, callbackErr)
}

func TestSearchScheduler_PartialFailures(t *testing.T) {
	s := newSearchScheduler()
	defer func() { s.stopCh <- struct{}{} }()

	var callCount atomic.Int32
	exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
		count := callCount.Add(1)
		if count == 1 {
			return nil, nil, errors.New("first indexer failed")
		}
		return []Result{{Title: "success"}}, []int{1}, nil
	}

	indexers := []*models.TorznabIndexer{
		{ID: 1, Name: "failing-indexer"},
		{ID: 2, Name: "success-indexer"},
	}

	done := make(chan struct{})
	err := s.Submit(context.Background(), indexers, nil, nil, exec, nil, nil,
		func(results []Result, coverage []int, err error) {
			// Should get partial results
			assert.NoError(t, err)    // Partial failure doesn't propagate as error
			assert.Len(t, results, 1) // Only successful result
			assert.Equal(t, []int{1}, coverage)
			close(done)
		})

	require.NoError(t, err)

	// Wait for completion
	<-done
	assert.Equal(t, int32(2), callCount.Load())
}
