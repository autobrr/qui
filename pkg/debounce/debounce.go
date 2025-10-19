// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package debounce

import (
	"sync"
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
	stop        chan struct{}
}

// New creates a new Debouncer with the specified delay.
func New(delay time.Duration) *Debouncer {
	d := &Debouncer{
		submissions: make(chan func(), 100), // buffered channel to prevent blocking
		delay:       delay,
		stop:        make(chan struct{}),
	}

	go d.run()

	return d
}

// run is the main goroutine that processes submissions
func (d *Debouncer) run() {
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
		case <-d.stop:
			go func(t <-chan time.Time) {
				for range t {
				}
			}(d.timer)

			close(d.submissions)
			d.mu.Lock()
			for fn := range d.submissions {
				d.latest = fn
			}
			d.mu.Unlock()
			runFunc()
			return
		case <-d.timer:
			runFunc()
		case fn := <-d.submissions:
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
	select {
	case <-d.stop:
		fn()
	case d.submissions <- fn:
	}
}

func (d *Debouncer) Queued() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.timer != nil
}

// Stop shuts down the debouncer goroutine
func (d *Debouncer) Stop() {
	close(d.stop)
}
