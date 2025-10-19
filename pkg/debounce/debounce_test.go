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

	done := make(chan bool, 1)
	d.Do(func() {
		done <- true
	})

	// Wait for execution
	select {
	case <-done:
		// Function executed
	case <-time.After(200 * time.Millisecond):
		t.Error("Function did not execute within timeout")
	}
}

func TestDebouncer_DebouncesMultipleCalls(t *testing.T) {
	d := New(100 * time.Millisecond)
	defer d.Stop()

	var executed []int
	var mu sync.Mutex
	done := make(chan bool, 1)

	// Submit multiple functions quickly
	for i := 0; i < 5; i++ {
		val := i
		d.Do(func() {
			mu.Lock()
			executed = append(executed, val)
			mu.Unlock()
			if val == 4 { // Only signal when the last function executes
				done <- true
			}
		})
		time.Sleep(10 * time.Millisecond) // Less than debounce delay
	}

	// Wait for execution
	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		if len(executed) != 1 {
			t.Errorf("Expected only one execution, got %d: %v", len(executed), executed)
		}
		if len(executed) > 0 && executed[0] != 4 {
			t.Errorf("Expected last value to be 4, got %d", executed[0])
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("Function did not execute within timeout")
	}
}

func TestDebouncer_Queued(t *testing.T) {
	d := New(100 * time.Millisecond)
	defer d.Stop()

	// Initially not queued
	if d.Queued() {
		t.Error("Expected debouncer to not be queued initially")
	}

	executed := make(chan bool, 1)
	d.Do(func() {
		executed <- true
	})

	// Wait for submission to be processed
	time.Sleep(10 * time.Millisecond)

	// Should be queued after submission
	if !d.Queued() {
		t.Error("Expected debouncer to be queued after submission")
	}

	// Wait for execution
	<-executed

	// Should not be queued after execution
	if d.Queued() {
		t.Error("Expected debouncer to not be queued after execution")
	}
}

func TestDebouncer_Stop(t *testing.T) {
	d := New(100 * time.Millisecond)

	executions := make(chan bool, 2)
	d.Do(func() {
		executions <- true
	})

	d.Stop()

	// After stop, functions should execute immediately
	d.Do(func() {
		executions <- true
	})

	// Wait for both executions
	<-executions
	<-executions
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

	done := make(chan bool, 1)
	d.Do(func() {
		done <- true
	})

	select {
	case <-done:
		// Function executed
	case <-time.After(100 * time.Millisecond):
		t.Error("Function did not execute within timeout")
	}
}

func TestDebouncer_MultipleSequences(t *testing.T) {
	d := New(50 * time.Millisecond)
	defer d.Stop()

	var executed int64

	// First sequence
	firstDone := make(chan bool, 1)
	for i := 0; i < 3; i++ {
		d.Do(func() {
			atomic.AddInt64(&executed, 1)
			firstDone <- true
		})
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for first sequence execution
	select {
	case <-firstDone:
		firstCount := atomic.LoadInt64(&executed)
		if firstCount != 1 {
			t.Errorf("Expected 1 execution in first sequence, got %d", firstCount)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("First sequence did not execute within timeout")
	}

	// Second sequence
	secondDone := make(chan bool, 1)
	for i := 0; i < 2; i++ {
		d.Do(func() {
			atomic.AddInt64(&executed, 1)
			secondDone <- true
		})
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for second sequence execution
	select {
	case <-secondDone:
		totalCount := atomic.LoadInt64(&executed)
		if totalCount != 2 {
			t.Errorf("Expected 2 total executions, got %d", totalCount)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Second sequence did not execute within timeout")
	}
}
