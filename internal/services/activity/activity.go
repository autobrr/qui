// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package activity

import (
	"sync"
	"sync/atomic"
	"time"
)

// Kind identifies the category of a server activity event. The frontend maps
// each kind to the react-query keys it should invalidate.
type Kind string

const (
	KindBackupRun          Kind = "backup.run"
	KindDirScanRun         Kind = "dirscan.run"
	KindOrphanScanRun      Kind = "orphanscan.run"
	KindDiscScanRun        Kind = "discscan.run"
	KindCrossSeedStatus    Kind = "crossseed.status"
	KindCrossSeedSearch    Kind = "crossseed.search"
	KindReannounceActivity Kind = "reannounce.activity"
	KindAutomationActivity Kind = "automation.activity"
	KindIndexerActivity    Kind = "indexer.activity"
	KindSearchHistory      Kind = "search.history"
	KindTrackerIcons       Kind = "tracker.icons"
	KindThemeSettings      Kind = "theme.settings"
	KindClientSettings     Kind = "client.settings"
	KindCustomThemes       Kind = "theme.custom"
)

type Event struct {
	Kind       Kind      `json:"kind"`
	InstanceID int       `json:"instanceId,omitempty"`
	ResourceID string    `json:"resourceId,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Publisher is the write side that background services depend on. Implementations
// must return quickly and never block the caller.
type Publisher interface {
	Publish(ev Event)
}

type NopPublisher struct{}

func (NopPublisher) Publish(Event) {}

const subscriberBuffer = 64

// Hub fans out published events to all current subscribers. The zero value is
// not usable; construct with NewHub.
type Hub struct {
	mu          sync.Mutex
	subscribers map[int]chan Event
	nextID      int
	closed      bool

	dropped atomic.Uint64
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[int]chan Event)}
}

func (h *Hub) Publish(ev Event) {
	if h == nil {
		return
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	for _, ch := range h.subscribers {
		select {
		case ch <- ev:
		default:
			h.dropped.Add(1)
		}
	}
	h.mu.Unlock()
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	id := h.nextID
	h.nextID++
	ch := make(chan Event, subscriberBuffer)
	h.subscribers[id] = ch

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if c, ok := h.subscribers[id]; ok {
				delete(h.subscribers, id)
				close(c)
			}
		})
	}

	return ch, unsubscribe
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subscribers {
		delete(h.subscribers, id)
		close(ch)
	}
}

func (h *Hub) Dropped() uint64 {
	return h.dropped.Load()
}
