// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package activity provides a small in-process pub/sub hub for qui-owned server
// events ("server activity"). Background services publish lightweight signals
// when their state changes; the SSE layer subscribes and forwards them to
// connected browsers, which invalidate the matching cached query and refetch on
// demand instead of polling on a timer.
//
// Events are deliberately signals, not data: they carry identifiers only (kind,
// instance id, resource id), never payloads. A dropped event is therefore safe -
// the next event, a stream reconnect, or a low-rate safety refetch reconciles
// the client - so the hub favours never blocking a publisher over guaranteed
// delivery.
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
	KindCrossSeedStatus    Kind = "crossseed.status"
	KindCrossSeedSearch    Kind = "crossseed.search"
	KindReannounceActivity Kind = "reannounce.activity"
	KindAutomationActivity Kind = "automation.activity"
	KindIndexerActivity    Kind = "indexer.activity"
	KindSearchHistory      Kind = "search.history"
	KindTrackerIcons       Kind = "tracker.icons"
)

// Event is a small, JSON-serializable signal that some qui-owned state changed.
// InstanceID and ResourceID are optional and scope the invalidation when set.
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

// NopPublisher is a no-op Publisher used as the default so services run normally
// when no hub is wired (e.g. in tests).
type NopPublisher struct{}

// Publish discards the event.
func (NopPublisher) Publish(Event) {}

// subscriberBuffer bounds each subscriber channel. A full channel drops rather
// than blocks the publisher.
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

// NewHub returns a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{subscribers: make(map[int]chan Event)}
}

// Publish delivers ev to every subscriber without blocking. A subscriber whose
// buffer is full has the event dropped (counted via Dropped).
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

// Subscribe registers a new subscriber and returns its receive channel plus an
// idempotent unsubscribe func. The channel is closed on unsubscribe or Close.
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

// Close shuts the hub down, closing all subscriber channels. Subsequent
// Publish/Subscribe calls are no-ops.
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

// Dropped reports the lifetime count of events dropped due to full subscriber
// buffers.
func (h *Hub) Dropped() uint64 {
	return h.dropped.Load()
}
