// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package debounce

import (
	"sync"
	"sync/atomic"
	"time"
)

type Debouncer struct {
	submissions chan func()
	timer       <-chan time.Time
	latest      func()
	mu          sync.RWMutex
	submitMu    sync.Mutex
	delay       time.Duration
	stopped     atomic.Bool
	done        chan struct{}
}

func New(delay time.Duration) *Debouncer {
	d := &Debouncer{
		submissions: make(chan func(), 100),
		delay:       delay,
		done:        make(chan struct{}),
	}

	go d.run()

	return d
}

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
				runFunc()
				return
			}
			d.mu.Lock()
			d.latest = fn
			if d.timer == nil {
				d.timer = time.After(d.delay)
			}
			d.mu.Unlock()
		}
	}
}

func (d *Debouncer) Do(fn func()) {
	d.submitMu.Lock()
	if d.stopped.Load() {
		d.submitMu.Unlock()
		fn()
		return
	}

	select {
	case d.submissions <- fn:
		d.submitMu.Unlock()
	default:
		d.submitMu.Unlock()
		if d.stopped.Load() {
			fn()
		}
	}
}

func (d *Debouncer) Queued() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.timer != nil
}

func (d *Debouncer) Stop() {
	if !d.stopped.CompareAndSwap(false, true) {
		return
	}

	d.submitMu.Lock()
	close(d.submissions)
	d.submitMu.Unlock()
	<-d.done
}
