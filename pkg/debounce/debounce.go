// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package debounce

import (
	"sync"
	"sync/atomic"
	"time"
)

// Debouncer provides debounced execution of functions.
// It ensures that functions are executed at most once per delay period,
// with automatic cleanup after execution.
type Debouncer struct {
	submissions chan func()
	timer       <-chan time.Time
	latest      func()
	mu          sync.RWMutex
	delay       time.Duration
	stopped     atomic.Bool
	done        chan struct{}
}

// New creates a new Debouncer with the specified delay.
func New(delay time.Duration) *Debouncer {
	d := &Debouncer{
		submissions: make(chan func(), 100), // buffered channel to prevent blocking
		delay:       delay,
		done:        make(chan struct{}),
	}

	go d.run()

	return d
}

// run is the main goroutine that processes submissions
func (d *Debouncer) run() {
	defer close(d.done)

	runFunc := func() {
		d.mu.Lock()

		select {
		case <-d.timer:
		default:
		}

		d.timer = nil

		fn := d.latest
		d.latest = nil
		d.mu.Unlock()
		if fn != nil {
			fn()
		}
	}

	for {
		select {
		case <-d.timer:
			runFunc()
		case fn, ok := <-d.submissions:
			if !ok {
				// Channel closed, execute final function and exit
				runFunc()
				return
			}
			d.mu.Lock()
			// Store the latest function
			d.latest = fn
			// Start the timer if not already running
			if d.timer == nil {
				d.timer = time.After(d.delay)
			}
			d.mu.Unlock()
		}
	}
}

// Do executes the function fn after the delay.
// If called multiple times within the delay period, only the last fn will execute after the delay.
func (d *Debouncer) Do(fn func()) {
	// Check if stopped
	if d.stopped.Load() {
		// Execute immediately if stopped
		fn()
		return
	}

	// Try to send to submissions channel
	select {
	case d.submissions <- fn:
		// Successfully submitted
	default:
		// Channel is full or closed, check if stopped
		if d.stopped.Load() {
			fn()
		}
		// Otherwise, drop the submission (buffer is full)
	}
}

func (d *Debouncer) Queued() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.timer != nil
}

// Stop shuts down the debouncer goroutine
func (d *Debouncer) Stop() {
	// Only stop once using atomic compare-and-swap
	if !d.stopped.CompareAndSwap(false, true) {
		// Already stopped or stopping
		return
	}

	// First call to Stop - close submissions and wait
	close(d.submissions)
	<-d.done
}
