// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package debounce

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncer_Basic(t *testing.T) {
	d := New(50 * time.Millisecond)
	defer d.Stop()

	var executed int64
	d.Do(func() {
		atomic.AddInt64(&executed, 1)
	})

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&executed) != 1 {
		t.Errorf("Expected function to be executed once, got %d", executed)
	}
}

func TestDebouncer_DebouncesMultipleCalls(t *testing.T) {
	d := New(100 * time.Millisecond)
	defer d.Stop()

	var executed []int
	var mu sync.Mutex

	// Submit multiple functions quickly
	for i := 0; i < 5; i++ {
		val := i
		d.Do(func() {
			mu.Lock()
			executed = append(executed, val)
			mu.Unlock()
		})
		time.Sleep(10 * time.Millisecond) // Less than debounce delay
	}

	// Wait for execution
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 1 {
		t.Errorf("Expected only one execution, got %d: %v", len(executed), executed)
	}

	if len(executed) > 0 && executed[0] != 4 {
		t.Errorf("Expected last value to be 4, got %d", executed[0])
	}
}

func TestDebouncer_Queued(t *testing.T) {
	d := New(100 * time.Millisecond)
	defer d.Stop()

	// Initially not queued
	if d.Queued() {
		t.Error("Expected debouncer to not be queued initially")
	}

	d.Do(func() {})

	// Should be queued after submission
	time.Sleep(10 * time.Millisecond)
	if !d.Queued() {
		t.Error("Expected debouncer to be queued after submission")
	}

	// Wait for execution
	time.Sleep(150 * time.Millisecond)
	if d.Queued() {
		t.Error("Expected debouncer to not be queued after execution")
	}
}

func TestDebouncer_Stop(t *testing.T) {
	d := New(100 * time.Millisecond)

	var executed int64
	d.Do(func() {
		atomic.AddInt64(&executed, 1)
	})

	d.Stop()

	// After stop, functions should execute immediately
	done := make(chan bool, 1)
	d.Do(func() {
		atomic.AddInt64(&executed, 1)
		done <- true
	})

	<-done

	if atomic.LoadInt64(&executed) != 2 {
		t.Errorf("Expected both functions to execute, got %d", executed)
	}
}

func TestDebouncer_StopBeforeExecution(t *testing.T) {
	d := New(100 * time.Millisecond)

	done := make(chan bool, 1)
	d.Do(func() {
		done <- true
	})

	// Stop immediately
	d.Stop()

	select {
	case <-done:
		// Function executed immediately
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected function to execute immediately on stop")
	}
}

func TestDebouncer_ZeroDelay(t *testing.T) {
	d := New(0)
	defer d.Stop()

	var executed int64
	d.Do(func() {
		atomic.AddInt64(&executed, 1)
	})

	time.Sleep(10 * time.Millisecond)

	if atomic.LoadInt64(&executed) != 1 {
		t.Errorf("Expected function to execute with zero delay, got %d", executed)
	}
}

func TestDebouncer_MultipleSequences(t *testing.T) {
	d := New(50 * time.Millisecond)
	defer d.Stop()

	var executed int64

	// First sequence
	for i := 0; i < 3; i++ {
		d.Do(func() {
			atomic.AddInt64(&executed, 1)
		})
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	firstCount := atomic.LoadInt64(&executed)
	if firstCount != 1 {
		t.Errorf("Expected 1 execution in first sequence, got %d", firstCount)
	}

	// Second sequence
	for i := 0; i < 2; i++ {
		d.Do(func() {
			atomic.AddInt64(&executed, 1)
		})
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	totalCount := atomic.LoadInt64(&executed)
	if totalCount != 2 {
		t.Errorf("Expected 2 total executions, got %d", totalCount)
	}
}
