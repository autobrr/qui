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
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
)

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

type taskOutcome struct {
	results []Result
	err     error
}

func TestSearchScheduler_BasicFunctionality(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(nil, 10)
		defer s.Stop()

		var executed atomic.Bool
		done := make(chan struct{})

		exec := func(_ context.Context, indexers []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
			executed.Store(true)
			return []Result{{Title: "test"}}, []int{indexers[0].ID}, nil
		}

		indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer", Backend: models.TorznabBackendNative}

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
	})
}

func TestSearchScheduler_PriorityOrdering(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := newRequestSlots(1*time.Millisecond, nil, nil)
		s := newSearchScheduler(rl, 1)
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

		indexer1 := &models.TorznabIndexer{ID: 1, Name: "indexer1"}
		indexer2 := &models.TorznabIndexer{ID: 2, Name: "indexer2"}

		callback := func(jobID uint64) {
			if completed.Add(1) == 2 {
				close(done)
			}
		}

		_, err1 := s.Submit(context.Background(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{indexer1},
			Meta:     &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityBackground}},
			ExecFn:   exec,
			Callbacks: JobCallbacks{
				OnJobDone: callback,
			},
		})

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

		require.Len(t, executedTasks, 2)
		assert.Equal(t, RateLimitPriorityInteractive, executedTasks[0])
		assert.Equal(t, RateLimitPriorityBackground, executedTasks[1])
	})
}

func TestSearchScheduler_WorkerPoolLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := newRequestSlots(1*time.Millisecond, nil, nil)
		s := newSearchScheduler(rl, 2)
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

		assert.LessOrEqual(t, maxConcurrent.Load(), int32(2))
	})
}

func TestSearchScheduler_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(nil, 10)
		defer s.Stop()

		started := make(chan struct{})
		exec := func(ctx context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
			close(started)
			<-ctx.Done()
			return nil, nil, ctx.Err()
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

		<-started
		cancel()

		<-done
		require.ErrorIs(t, gotErr, context.Canceled)
	})
}

func TestSearchScheduler_WorkerPanicRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(nil, 10)
		defer s.Stop()

		var completed atomic.Int32
		done := make(chan struct{})

		panicked := make(chan taskOutcome, 1)
		recovered := make(chan taskOutcome, 1)

		exec := func(ctx context.Context, indexers []*models.TorznabIndexer, params url.Values, meta *searchContext) ([]Result, []int, error) {
			if len(indexers) > 0 && indexers[0].ID == 1 {
				panic("test panic")
			}
			return []Result{{Title: "test"}}, []int{1}, nil
		}

		indexer1 := &models.TorznabIndexer{ID: 1, Name: "test-indexer-1"}
		indexer2 := &models.TorznabIndexer{ID: 2, Name: "test-indexer-2"}

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
	})
}

func TestSearchScheduler_TaskTimeoutCompletesHungExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(nil, 10)
		defer s.Stop()

		parentCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		started := make(chan struct{})
		release := make(chan struct{})
		defer close(release)
		completeCh := make(chan error, 1)
		indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer"}

		exec := func(_ context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
			close(started)
			<-release
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
		require.Equal(t, 30*time.Millisecond, time.Since(start))
	})
}

func TestSearchScheduler_ExecutionTimeoutStartsAfterNativePacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := newRequestSlots(40*time.Millisecond, nil, nil)
		s := newSearchScheduler(limiter, 1)
		defer s.Stop()

		indexer := &models.TorznabIndexer{ID: 1, Name: "native", Backend: models.TorznabBackendNative}
		firstStarted := make(chan struct{})
		releaseFirst := make(chan struct{})
		firstDone := make(chan error, 1)

		_, err := s.Submit(context.Background(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{indexer},
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				close(firstStarted)
				<-releaseFirst
				return nil, []int{indexer.ID}, nil
			},
			Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				firstDone <- err
			}},
		})
		require.NoError(t, err)
		<-firstStarted

		remainingBudget := make(chan time.Duration, 1)
		secondDone := make(chan error, 1)
		_, err = s.Submit(context.Background(), SubmitRequest{
			Indexers:         []*models.TorznabIndexer{indexer},
			ExecutionTimeout: 30 * time.Millisecond,
			ExecFn: func(ctx context.Context, _ []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					remainingBudget <- 0
					return nil, nil, errors.New("execution context has no deadline")
				}
				remainingBudget <- time.Until(deadline)
				return nil, []int{indexer.ID}, nil
			},
			Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				secondDone <- err
			}},
		})
		require.NoError(t, err)

		time.Sleep(15 * time.Millisecond)
		close(releaseFirst)
		require.NoError(t, <-firstDone)

		select {
		case remaining := <-remainingBudget:
			require.Equal(t, 30*time.Millisecond, remaining)
		case <-time.After(5 * time.Second):
			t.Fatal("second task did not start after native pacing")
		}
		require.NoError(t, <-secondDone)
	})
}

func TestSearchScheduler_QueryCooldownSkipsBeforeNativePacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(newRequestSlots(200*time.Millisecond, nil, nil), 1)
		defer s.Stop()
		indexer := &models.TorznabIndexer{ID: 1, Name: "native", Backend: models.TorznabBackendNative}

		var executed atomic.Int32
		submit := func(answer error) (time.Duration, error) {
			t.Helper()
			complete := make(chan error, 1)
			start := time.Now()
			_, err := s.Submit(context.Background(), SubmitRequest{
				Indexers: []*models.TorznabIndexer{indexer},
				ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
					executed.Add(1)
					return nil, []int{indexer.ID}, answer
				},
				Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
					complete <- err
				}},
			})
			require.NoError(t, err)
			err = recvWithin(t, complete)
			took := time.Since(start)
			synctest.Wait()
			return took, err
		}

		_, err := submit(&responseError{StatusCode: 429, RetryAfter: "1"})
		var rateLimitErr *RateLimitError
		require.ErrorAs(t, err, &rateLimitErr)

		took, err := submit(nil)
		require.ErrorAs(t, err, &rateLimitErr)
		assert.Equal(t, dispatchCoalesceDelay, took, "known query cooldown waited for the Request gap")
		assert.Equal(t, int32(1), executed.Load())

		time.Sleep(time.Second)
		took, err = submit(nil)
		require.NoError(t, err)
		assert.Equal(t, dispatchCoalesceDelay, took, "a refused search must not take the Request slot")
	})
}

func TestSearchScheduler_CooldownRefusalDoesNotWaitForOpenSearch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slots := newRequestSlots(time.Millisecond, nil, nil)
		s := newSearchScheduler(slots, 1)
		defer s.Stop()
		indexer := &models.TorznabIndexer{ID: 1, Name: "native", Backend: models.TorznabBackendNative}
		started := make(chan struct{})
		release := make(chan struct{})
		defer close(release)
		_, err := s.Submit(t.Context(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{indexer},
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				close(started)
				<-release
				return nil, nil, nil
			},
		})
		require.NoError(t, err)
		<-started
		answer429(t, slots, indexer, requestCaps, "60")

		refused := make(chan error, 1)
		start := time.Now()
		_, err = s.Submit(t.Context(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{indexer},
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				return nil, nil, nil
			},
			Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) { refused <- err }},
		})
		require.NoError(t, err)
		var rateLimitErr *RateLimitError
		require.ErrorAs(t, recvWithin(t, refused), &rateLimitErr)
		assert.Equal(t, dispatchCoalesceDelay, time.Since(start))
	})
}

func TestSearchScheduler_FreshTaskKeepsOriginalContextDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestSearchScheduler_DoesNotRunQueuedTaskAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(newRequestSlots(time.Millisecond, nil, nil), 1)
		defer s.Stop()

		blockerStarted := make(chan struct{})
		releaseBlocker := make(chan struct{})
		blockerDone := make(chan struct{})
		expiredDone := make(chan error, 1)
		var expiredExecuted atomic.Bool

		_, err := s.Submit(context.Background(), SubmitRequest{
			Indexers: []*models.TorznabIndexer{{ID: 1, Name: "blocker"}},
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				close(blockerStarted)
				<-releaseBlocker
				return nil, []int{1}, nil
			},
			Callbacks: JobCallbacks{OnJobDone: func(uint64) { close(blockerDone) }},
		})
		require.NoError(t, err)
		<-blockerStarted

		expiredCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err = s.Submit(expiredCtx, SubmitRequest{
			Indexers: []*models.TorznabIndexer{{ID: 2, Name: "expires-in-queue"}},
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				expiredExecuted.Store(true)
				return nil, []int{2}, nil
			},
			Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
				expiredDone <- err
			}},
		})
		require.NoError(t, err)
		<-expiredCtx.Done()
		close(releaseBlocker)
		<-blockerDone

		require.ErrorIs(t, <-expiredDone, context.DeadlineExceeded)
		synctest.Wait()
		assert.False(t, expiredExecuted.Load(), "expired queued task must not be resurrected")
	})
}

func TestSearchScheduler_RSSDeduplicationInvokesOnComplete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := newRequestSlots(1*time.Millisecond, nil, nil)
		s := newSearchScheduler(rl, 1)
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

		firstDone := make(chan struct{})
		_, err1 := s.Submit(context.Background(), SubmitRequest{
			Indexers:  []*models.TorznabIndexer{indexer},
			Meta:      rssMeta,
			ExecFn:    exec,
			Callbacks: JobCallbacks{OnJobDone: func(uint64) { close(firstDone) }},
		})
		require.NoError(t, err1)
		<-firstExecStarted

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

		close(releaseFirst)
		<-firstDone

		assert.Equal(t, int32(1), executions.Load())
	})
}

func TestSearchScheduler_EmptySubmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		<-done
	})
}

func TestSearchScheduler_NilIndexerHandling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		<-done
	})
}

func TestSearchScheduler_ConcurrentSubmissions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestSearchScheduler_MultipleIndexersPerSubmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := newRequestSlots(1*time.Millisecond, nil, nil)
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
	})
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

	assert.Equal(t, searchJobPriorityBackground, jobPriority(nil))
	assert.Equal(t, searchJobPriorityBackground, jobPriority(&searchContext{}))
	assert.Equal(t, searchJobPriorityBackground, jobPriority(&searchContext{rateLimit: &RateLimitOptions{}}))
}

func TestSearchScheduler_RateLimitIntervalStartsAfterCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := newRequestSlots(80*time.Millisecond, nil, nil)
		s := newSearchScheduler(rl, 10)
		defer s.Stop()

		indexer := &models.TorznabIndexer{ID: 1, Name: "test-indexer", Backend: models.TorznabBackendNative}
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
		assert.Equal(t, dispatchCoalesceDelay+100*time.Millisecond, time.Since(start1))

		done2 := make(chan struct{})
		start2 := time.Now()
		_, err = s.Submit(context.Background(), SubmitRequest{
			Indexers:  []*models.TorznabIndexer{indexer},
			ExecFn:    exec,
			Callbacks: JobCallbacks{OnJobDone: func(jobID uint64) { close(done2) }},
		})
		require.NoError(t, err)
		<-done2
		assert.Equal(t, 80*time.Millisecond, time.Since(start2))
	})
}

func TestSearchScheduler_ShortSearchUsesOneGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(newRequestSlots(80*time.Millisecond, nil, nil), 10)
		defer s.Stop()
		indexer := &models.TorznabIndexer{ID: 1, Name: "native", Backend: models.TorznabBackendNative}

		var started []time.Time
		var answeredAt time.Time
		for range 2 {
			done := make(chan struct{})
			_, err := s.Submit(context.Background(), SubmitRequest{
				Indexers: []*models.TorznabIndexer{indexer},
				ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
					started = append(started, time.Now())
					time.Sleep(10 * time.Millisecond)
					answeredAt = time.Now()
					return nil, []int{indexer.ID}, nil
				},
				Callbacks: JobCallbacks{OnJobDone: func(uint64) { close(done) }},
			})
			require.NoError(t, err)
			<-done
		}
		require.Len(t, started, 2)
		assert.Equal(t, 80*time.Millisecond, started[1].Sub(started[0].Add(10*time.Millisecond)))
		assert.Equal(t, 10*time.Millisecond, answeredAt.Sub(started[1]))
	})
}

func TestSearchScheduler_ExecPanicStillEmitsActivity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		synctest.Wait()
		assert.Contains(t, rec.statuses(), "error", "panicked task should record an error history entry")
		counts := pub.counts()
		assert.Positive(t, counts[activity.KindIndexerActivity], "panic-recovery path must emit an indexer-activity signal")
		assert.Positive(t, counts[activity.KindSearchHistory], "panic-recovery path must emit a search-history signal")
	})
}

func TestSchedulerGetStatusOrdersTasksDeterministically(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		base := time.Now()
		newItem := func(taskID uint64, indexerID int, priority int, age time.Duration) *taskItem {
			return &taskItem{
				task: workerTask{
					jobID:   1,
					taskID:  taskID,
					indexer: &models.TorznabIndexer{ID: indexerID, Name: "Indexer"},
				},
				priority: priority,
				created:  base.Add(age),
			}
		}

		s := &searchScheduler{
			inFlight:   make(map[int]*taskItem),
			jobs:       make(map[uint64]*jobState),
			workerPool: make(chan struct{}, 4),
		}

		queuedItems := []*taskItem{
			newItem(1, 11, 2, time.Second),
			newItem(2, 12, 2, 2*time.Second),
			newItem(3, 13, 1, 3*time.Second),
			newItem(4, 14, 0, 4*time.Second),
			newItem(5, 15, 1, time.Second),
			newItem(6, 16, 2, time.Second),
		}
		for _, item := range queuedItems {
			heap.Push(&s.taskQueue, item)
		}

		for _, item := range []*taskItem{
			newItem(20, 20, 0, 3*time.Second),
			newItem(22, 22, 0, time.Second),
			newItem(21, 21, 0, time.Second),
		} {
			s.inFlight[item.task.indexer.ID] = item
		}

		s.jobs[7] = &jobState{totalTasks: 1}
		s.jobs[3] = &jobState{totalTasks: 1}
		s.jobs[5] = &jobState{totalTasks: 1}

		wantQueued := []uint64{4, 5, 3, 1, 6, 2}
		wantInFlight := []uint64{21, 22, 20}
		wantJobs := []uint64{3, 5, 7}

		for range 20 {
			status := s.GetStatus()

			gotQueued := make([]uint64, 0, len(status.QueuedTasks))
			for _, task := range status.QueuedTasks {
				gotQueued = append(gotQueued, task.TaskID)
			}
			if !slices.Equal(gotQueued, wantQueued) {
				t.Fatalf("queued order = %v, want %v", gotQueued, wantQueued)
			}

			gotInFlight := make([]uint64, 0, len(status.InFlightTasks))
			for _, task := range status.InFlightTasks {
				gotInFlight = append(gotInFlight, task.TaskID)
			}
			if !slices.Equal(gotInFlight, wantInFlight) {
				t.Fatalf("in-flight order = %v, want %v", gotInFlight, wantInFlight)
			}

			gotJobs := make([]uint64, 0, len(status.ActiveJobs))
			for _, job := range status.ActiveJobs {
				gotJobs = append(gotJobs, job.JobID)
			}
			if !slices.Equal(gotJobs, wantJobs) {
				t.Fatalf("active job order = %v, want %v", gotJobs, wantJobs)
			}
		}

		reversed := &searchScheduler{
			inFlight:   make(map[int]*taskItem),
			jobs:       make(map[uint64]*jobState),
			workerPool: make(chan struct{}, 4),
		}
		for _, item := range slices.Backward(queuedItems) {
			heap.Push(&reversed.taskQueue, item)
		}
		gotQueued := make([]uint64, 0, len(queuedItems))
		for _, task := range reversed.GetStatus().QueuedTasks {
			gotQueued = append(gotQueued, task.TaskID)
		}
		if !slices.Equal(gotQueued, wantQueued) {
			t.Fatalf("queued order after reversed pushes = %v, want %v", gotQueued, wantQueued)
		}
	})
}

func TestSearchScheduler_TimedOutTaskHoldsSlotUntilExecReturns(t *testing.T) {
	for _, tc := range []struct {
		name string
		late func() ([]Result, []int, error)
	}{
		{name: "late result", late: func() ([]Result, []int, error) { return []Result{{Title: "late"}}, []int{1}, nil }},
		{name: "late panic", late: func() ([]Result, []int, error) { panic("late panic") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSearchScheduler(nil, 1)
				defer s.Stop()
				rec := &recordingHistoryRecorder{}
				s.historyRecorder = rec
				pub := &recordingPublisher{}
				s.setActivityPublisher(pub)

				first := &models.TorznabIndexer{ID: 1, Name: "stuck"}
				other := &models.TorznabIndexer{ID: 2, Name: "other"}
				release := make(chan struct{})
				firstDone := make(chan error, 2)

				_, err := s.Submit(context.Background(), SubmitRequest{
					Indexers:         []*models.TorznabIndexer{first},
					ExecutionTimeout: 20 * time.Millisecond,
					ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
						<-release
						return tc.late()
					},
					Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) {
						firstDone <- err
					}},
				})
				require.NoError(t, err)

				select {
				case err := <-firstDone:
					require.ErrorIs(t, err, context.DeadlineExceeded)
				case <-time.After(time.Second):
					t.Fatal("timed-out task did not report completion before its exec returned")
				}
				synctest.Wait()
				require.GreaterOrEqual(t, pub.counts()[activity.KindIndexerActivity], 2)

				started := make(chan int, 2)
				laterDone := make(chan struct{}, 2)
				for _, idx := range []*models.TorznabIndexer{first, other} {
					_, err := s.Submit(context.Background(), SubmitRequest{
						Indexers: []*models.TorznabIndexer{idx},
						ExecFn: func(_ context.Context, idxs []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
							started <- idxs[0].ID
							return nil, nil, nil
						},
						Callbacks: JobCallbacks{OnComplete: func(uint64, *models.TorznabIndexer, []Result, []int, error) {
							laterDone <- struct{}{}
						}},
					})
					require.NoError(t, err)
				}

				time.Sleep(100 * time.Millisecond)
				synctest.Wait()
				select {
				case id := <-started:
					t.Fatalf("indexer %d started while the timed-out exec still held the slot", id)
				default:
				}

				close(release)
				got := []int{recvWithin(t, started), recvWithin(t, started)}
				assert.ElementsMatch(t, []int{1, 2}, got)
				recvWithin(t, laterDone)
				recvWithin(t, laterDone)

				synctest.Wait()
				assert.Empty(t, firstDone, "timed-out task reported completion twice")
				require.Len(t, rec.statuses(), 3)
			})
		})
	}
}

func recvWithin[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("nothing received within 2s")
		var zero T
		return zero
	}
}

func submitStuck(t *testing.T, s *searchScheduler, idx *models.TorznabIndexer, release <-chan struct{}, jobDone chan<- struct{}) {
	t.Helper()
	timedOut := make(chan error, 1)
	_, err := s.Submit(t.Context(), SubmitRequest{
		Indexers:         []*models.TorznabIndexer{idx},
		ExecutionTimeout: 20 * time.Millisecond,
		ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
			<-release
			return nil, nil, nil
		},
		Callbacks: JobCallbacks{
			OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) { timedOut <- err },
			OnJobDone:  func(uint64) { jobDone <- struct{}{} },
		},
	})
	require.NoError(t, err)
	require.ErrorIs(t, recvWithin(t, timedOut), context.DeadlineExceeded)
}

func submitStart(t *testing.T, s *searchScheduler, idx *models.TorznabIndexer, started chan<- int, done chan<- error) {
	t.Helper()
	_, err := s.Submit(t.Context(), SubmitRequest{
		Indexers: []*models.TorznabIndexer{idx},
		ExecFn: func(_ context.Context, idxs []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
			started <- idxs[0].ID
			return nil, nil, nil
		},
		Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) { done <- err }},
	})
	require.NoError(t, err)
}

func TestSearchScheduler_TimedOutTaskEndsJobAndHoldsOnlyItsIndexer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSearchScheduler(nil, 2)
		defer s.Stop()
		stuck := &models.TorznabIndexer{ID: 1, Name: "stuck"}
		other := &models.TorznabIndexer{ID: 2, Name: "other"}
		release := make(chan struct{})
		jobDone := make(chan struct{}, 1)
		submitStuck(t, s, stuck, release, jobDone)
		recvWithin(t, jobDone)

		started := make(chan int, 2)
		done := make(chan error, 2)
		submitStart(t, s, stuck, started, done)
		submitStart(t, s, other, started, done)
		require.Equal(t, 2, recvWithin(t, started), "a free worker must run the other indexer")
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		select {
		case id := <-started:
			t.Fatalf("indexer %d started while its timed-out exec still runs", id)
		default:
		}
		close(release)
		require.Equal(t, 1, recvWithin(t, started))
	})
}

func TestSearchScheduler_TimedOutSearchFreesSlotAtTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slots := newRequestSlots(150*time.Millisecond, nil, nil)
		s := newSearchScheduler(slots, 2)
		defer s.Stop()
		stuck := &models.TorznabIndexer{ID: 1, Name: "stuck", Backend: models.TorznabBackendNative}
		release := make(chan struct{})
		timedOut := make(chan error, 1)
		_, err := s.Submit(t.Context(), SubmitRequest{
			Indexers:         []*models.TorznabIndexer{stuck},
			ExecutionTimeout: 20 * time.Millisecond,
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				<-release
				return nil, nil, &responseError{StatusCode: 429, RetryAfter: "60"}
			},
			Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) { timedOut <- err }},
		})
		require.NoError(t, err)
		require.ErrorIs(t, recvWithin(t, timedOut), context.DeadlineExceeded)
		timedOutAt := time.Now()

		waitCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		ticket, err := slots.acquire(waitCtx, stuck, requestDirScanDownload)
		require.NoError(t, err)
		require.Equal(t, 150*time.Millisecond, time.Since(timedOutAt), "the Request gap must count from the timeout")
		require.NoError(t, ticket.done(t.Context(), nil))

		close(release)
		synctest.Wait()
		_, _, err = slots.tryAcquire(stuck, requestSearch)
		var rateLimitErr *RateLimitError
		require.ErrorAs(t, err, &rateLimitErr, "a late 429 must start the search cooldown")
		_, _, err = slots.tryAcquire(stuck, requestDownload)
		require.NoError(t, err)
	})
}

func TestSearchScheduler_DetachedIndexerFailsQueuedTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := detachedExecWarnAfter
		detachedExecWarnAfter = 50 * time.Millisecond
		t.Cleanup(func() { detachedExecWarnAfter = old })

		s := newSearchScheduler(nil, 2)
		defer s.Stop()
		stuck := &models.TorznabIndexer{ID: 1, Name: "stuck"}
		release := make(chan struct{})
		submitStuck(t, s, stuck, release, make(chan struct{}, 1))

		started := make(chan int, 1)
		done := make(chan error, 1)
		submitStart(t, s, stuck, started, done)
		require.ErrorContains(t, recvWithin(t, done), "still running a timed-out search")

		submitStart(t, s, stuck, started, done)
		require.ErrorContains(t, recvWithin(t, done), "still running a timed-out search")
		require.Empty(t, started, "a task for the stuck indexer ran while its exec still runs")

		close(release)
		synctest.Wait()
		s.mu.Lock()
		inFlight := len(s.inFlight)
		s.mu.Unlock()
		require.Zero(t, inFlight)
		submitStart(t, s, stuck, started, done)
		require.Equal(t, 1, recvWithin(t, started))
		require.NoError(t, recvWithin(t, done))
	})
}

func TestSearchScheduler_DetachedIndexerFailsRSSTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := detachedExecWarnAfter
		detachedExecWarnAfter = 50 * time.Millisecond
		t.Cleanup(func() { detachedExecWarnAfter = old })

		s := newSearchScheduler(nil, 2)
		defer s.Stop()
		stuck := &models.TorznabIndexer{ID: 1, Name: "stuck"}
		rssMeta := &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}}
		release := make(chan struct{})
		defer close(release)
		submitRSS := func(exec func(), done chan<- error) {
			t.Helper()
			_, err := s.Submit(t.Context(), SubmitRequest{
				Indexers:         []*models.TorznabIndexer{stuck},
				Meta:             rssMeta,
				ExecutionTimeout: 20 * time.Millisecond,
				ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
					exec()
					return nil, nil, nil
				},
				Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) { done <- err }},
			})
			require.NoError(t, err)
		}

		timedOut := make(chan error, 1)
		submitRSS(func() { <-release }, timedOut)
		require.ErrorIs(t, recvWithin(t, timedOut), context.DeadlineExceeded)
		time.Sleep(detachedExecWarnAfter)
		synctest.Wait()
		s.mu.Lock()
		detached := s.inFlight[stuck.ID].detached
		s.mu.Unlock()
		require.True(t, detached)

		done := make(chan error, 1)
		submitRSS(func() {}, done)
		require.ErrorContains(t, recvWithin(t, done), "still running a timed-out search")
	})
}

func TestSearchScheduler_DetachedExecKeepsReplacementRSSMarker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := detachedExecWarnAfter
		detachedExecWarnAfter = 50 * time.Millisecond
		t.Cleanup(func() { detachedExecWarnAfter = old })

		s := newSearchScheduler(nil, 2)
		defer s.Stop()
		stuck := &models.TorznabIndexer{ID: 1, Name: "stuck"}
		release := make(chan struct{})
		timedOut := make(chan error, 1)
		_, err := s.Submit(t.Context(), SubmitRequest{
			Indexers:         []*models.TorznabIndexer{stuck},
			Meta:             &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}},
			ExecutionTimeout: 20 * time.Millisecond,
			ExecFn: func(context.Context, []*models.TorznabIndexer, url.Values, *searchContext) ([]Result, []int, error) {
				<-release
				return nil, nil, nil
			},
			Callbacks: JobCallbacks{OnComplete: func(_ uint64, _ *models.TorznabIndexer, _ []Result, _ []int, err error) { timedOut <- err }},
		})
		require.NoError(t, err)
		require.ErrorIs(t, recvWithin(t, timedOut), context.DeadlineExceeded)
		time.Sleep(detachedExecWarnAfter)
		synctest.Wait()
		s.mu.Lock()
		detached := s.inFlight[stuck.ID].detached
		s.mu.Unlock()
		require.True(t, detached)

		s.mu.Lock()
		s.pendingRSS[stuck.ID] = struct{}{}
		s.mu.Unlock()
		close(release)
		synctest.Wait()

		s.mu.Lock()
		defer s.mu.Unlock()
		require.NotContains(t, s.inFlight, stuck.ID)
		require.Contains(t, s.pendingRSS, stuck.ID)
	})
}
