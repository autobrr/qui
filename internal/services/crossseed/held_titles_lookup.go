// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/pkg/releases"
)

const (
	heldSyncDueWithin = time.Second
	heldSyncWait      = heldSyncDueWithin + 500*time.Millisecond
)

type heldLookup struct {
	titles     *heldTitles
	query      heldTitlesQuery
	targets    []int
	owners     []*syncowner.Owner
	synced     []int
	readErrors map[int]error
	wait       *heldWait
}

type heldWait struct {
	pending []<-chan struct{}
}

func (h *heldTitles) lookup(ctx context.Context, query heldTitlesQuery, instances []*models.Instance) *heldLookup {
	if h == nil {
		return nil
	}
	lookup := &heldLookup{titles: h, query: query, readErrors: map[int]error{}}
	for _, instance := range instances {
		lookup.targets = append(lookup.targets, instance.ID)
		owner, err := h.svc.syncManager.GetSyncOwner(ctx, instance.ID)
		if err != nil && query.everyInstance {
			return nil
		}
		if err != nil {
			lookup.readErrors[instance.ID] = err
			continue
		}
		lookup.owners = append(lookup.owners, owner)
		if owner.SnapshotUnchecked() != nil {
			lookup.synced = append(lookup.synced, instance.ID)
		}
	}
	if !lookup.answers(ctx) {
		return nil
	}
	lookup.wait = newHeldWait(lookup.owners)
	return lookup
}

func (l *heldLookup) answers(ctx context.Context) bool {
	if l.query.everyInstance && len(l.synced) < len(l.targets) {
		return false
	}
	return l.titles.ready(ctx, l.targets)
}

func (l *heldLookup) holdsNothing(ctx context.Context, contentType releases.ContentType) bool {
	caughtUp := l.titles.alternates.caughtUp(string(contentType))
	return caughtUp && l.answers(ctx) && len(l.candidates()) == 0
}

func (l *heldLookup) addTitles(titles []string) {
	if l == nil {
		return
	}
	for _, title := range titles {
		l.query.addedKeys = addNormalizedTitle(l.query.addedKeys, title)
	}
}

func (l *heldLookup) shareWait(wait *heldWait) {
	if l != nil && wait != nil {
		l.wait = wait
	}
}

func (l *heldLookup) awaitDueSyncs(ctx context.Context) (due bool) {
	return l != nil && l.wait.await(ctx)
}

func (l *heldLookup) candidates() map[int]heldCandidates {
	return l.titles.candidates(l.query, l.synced)
}

func (l *heldLookup) refreshStale() {
	if l == nil {
		return
	}
	for _, owner := range l.owners {
		owner.RefreshStale()
	}
}

func newHeldWait(owners []*syncowner.Owner) *heldWait {
	wait := &heldWait{}
	for _, owner := range owners {
		if owner.SnapshotUnchecked() == nil {
			continue
		}
		if done := owner.PendingSync(heldSyncDueWithin); done != nil {
			wait.pending = append(wait.pending, done)
		}
	}
	return wait
}

func (w *heldWait) await(ctx context.Context) (due bool) {
	pending := w.pending
	w.pending = nil
	limit := time.After(heldSyncWait)
	for _, done := range pending {
		select {
		case <-done:
		case <-limit:
			return true
		case <-ctx.Done():
			return true
		}
	}
	return len(pending) > 0
}
