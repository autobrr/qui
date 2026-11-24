// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package syncowner

import (
	"context"
	"time"
)

const addSyncDelay = 200 * time.Millisecond

type syncRun struct {
	done     chan struct{}
	err      error
	panicked any
}

type dueRun struct {
	*syncRun
	at    time.Time
	timer *time.Timer
}

func newRun() *syncRun {
	return &syncRun{done: make(chan struct{})}
}

func (o *Owner) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	run := o.startLocked(context.WithoutCancel(ctx), false)
	o.mu.Unlock()
	select {
	case <-run.done:
		if run.panicked != nil {
			panic(run.panicked)
		}
		return run.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *Owner) SyncAfterAdd() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.addShownAt = time.Now().Add(addSyncDelay)
	o.scheduleLocked()
}

func (o *Owner) PendingSync(dueWithin time.Duration) <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.due != nil && time.Until(o.due.at) <= dueWithin {
		return o.due.done
	}
	if o.running != nil {
		return o.running.done
	}
	return nil
}

func (o *Owner) RefreshStale() {
	if !o.stale() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.due == nil && o.stale() {
		o.startLocked(o.background, true)
	}
}

func (o *Owner) refreshForRead() {
	neverSynced := o.sync.LastSyncTime().IsZero()
	o.RefreshStale()
	if !neverSynced || !o.readsSync {
		return
	}
	if next := o.nextSync(); next != nil {
		select {
		case <-next:
		case <-time.After(o.firstSyncWait):
		}
	}
}

func (o *Owner) nextSync() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case o.running != nil:
		return o.running.done
	case o.due != nil:
		return o.due.done
	}
	return nil
}

func (o *Owner) stale() bool {
	return o.readsSync && time.Since(o.sync.LastSyncTime()) > o.interval
}

func (o *Owner) startLocked(ctx context.Context, background bool) *syncRun {
	if o.running != nil {
		return o.running
	}
	run := newRun()
	if o.due != nil {
		o.due.timer.Stop()
		run, o.due = o.due.syncRun, nil
	}
	o.lastStart = time.Now()
	if !o.lastStart.Before(o.addShownAt) {
		o.addShownAt = time.Time{}
	}
	o.running = run
	o.scheduleLocked()
	go o.execute(ctx, run, background)
	return run
}

func (o *Owner) execute(ctx context.Context, run *syncRun, background bool) {
	defer func() {
		run.panicked = recover()
		o.mu.Lock()
		o.running = nil
		if o.due != nil {
			o.due.timer.Reset(time.Until(o.due.at))
		}
		o.mu.Unlock()
		close(run.done)
		if background && run.panicked != nil {
			panic(run.panicked)
		}
	}()
	run.err = o.sync.Sync(ctx)
}

func (o *Owner) scheduleLocked() {
	if o.addShownAt.IsZero() || o.due != nil {
		return
	}
	at := o.addShownAt
	if earliest := o.lastStart.Add(o.interval); earliest.After(at) {
		at = earliest
	}
	due := &dueRun{syncRun: newRun(), at: at}
	due.timer = time.AfterFunc(time.Until(at), func() { o.startDue(due) })
	o.due = due
}

func (o *Owner) startDue(due *dueRun) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.due == due {
		o.startLocked(o.background, true)
	}
}
