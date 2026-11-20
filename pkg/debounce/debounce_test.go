// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package debounce

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestDebouncer_Basic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(50 * time.Millisecond)
		defer d.Stop()

		done := make(chan bool, 1)
		d.Do(func() {
			done <- true
		})

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Function did not execute within timeout")
		}
	})
}

func TestDebouncer_DebouncesMultipleCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(100 * time.Millisecond)
		defer d.Stop()

		var executed, lastValue atomic.Int32

		for i := range int32(5) {
			d.Do(func() {
				executed.Add(1)
				lastValue.Store(i)
			})
			time.Sleep(5 * time.Millisecond)
		}

		time.Sleep(time.Second)
		synctest.Wait()

		if got := executed.Load(); got != 1 {
			t.Errorf("Expected exactly one execution, got %d", got)
		}
		if got := lastValue.Load(); got != 4 {
			t.Errorf("Expected last value to be 4, got %d", got)
		}
	})
}

func TestDebouncer_Queued(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(100 * time.Millisecond)
		defer d.Stop()

		if d.Queued() {
			t.Error("Expected debouncer to not be queued initially")
		}

		executed := make(chan bool, 1)
		d.Do(func() {
			executed <- true
		})

		synctest.Wait()

		if !d.Queued() {
			t.Error("Expected debouncer to be queued after submission")
		}

		<-executed

		if d.Queued() {
			t.Error("Expected debouncer to not be queued after execution")
		}
	})
}

func TestDebouncer_Stop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(100 * time.Millisecond)

		executions := make(chan bool, 2)
		d.Do(func() {
			executions <- true
		})

		d.Stop()

		d.Do(func() {
			executions <- true
		})

		<-executions
		<-executions
	})
}

func TestDebouncer_StopBeforeExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(100 * time.Millisecond)

		done := make(chan bool, 1)
		d.Do(func() {
			done <- true
		})

		d.Stop()

		select {
		case <-done:
		default:
			t.Error("Expected function to execute immediately on stop")
		}
	})
}

func TestDebouncer_ZeroDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(0)
		defer d.Stop()

		done := make(chan bool, 1)
		d.Do(func() {
			done <- true
		})

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Function did not execute within timeout")
		}
	})
}

func TestDebouncer_MultipleSequences(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := New(50 * time.Millisecond)
		defer d.Stop()

		var executed atomic.Int64

		for range 3 {
			d.Do(func() {
				executed.Add(1)
			})
			time.Sleep(10 * time.Millisecond)
		}

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		if got := executed.Load(); got != 1 {
			t.Errorf("Expected 1 execution in first sequence, got %d", got)
		}

		for range 2 {
			d.Do(func() {
				executed.Add(1)
			})
			time.Sleep(10 * time.Millisecond)
		}

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		if got := executed.Load(); got != 2 {
			t.Errorf("Expected 2 total executions, got %d", got)
		}
	})
}

func TestDebouncer_StopNoPanicWhileDoInFlight(t *testing.T) {
	const attempts = 2000
	const workers = 64

	for attempt := range attempts {
		d := New(0)
		panicCh := make(chan any, workers)
		var wg sync.WaitGroup
		wg.Add(workers)
		start := make(chan struct{})

		for range workers {
			go func() {
				defer wg.Done()
				<-start
				defer func() {
					if r := recover(); r != nil {
						select {
						case panicCh <- r:
						default:
						}
					}
				}()
				d.Do(func() {})
			}()
		}

		close(start)
		runtime.Gosched()
		d.Stop()
		wg.Wait()

		if len(panicCh) > 0 {
			t.Fatalf("send on closed channel panic observed when stopping debouncer (attempt %d)", attempt)
		}
	}
}
