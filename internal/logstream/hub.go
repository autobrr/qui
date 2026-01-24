// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package logstream provides a thread-safe log broadcasting system with a ring buffer
// for log history and SSE-based streaming to subscribers.
package logstream

import (
	"context"
	"sync"
)

const (
	// DefaultBufferSize is the default number of log lines to keep in the ring buffer.
	DefaultBufferSize = 1000
	// DefaultSubscriberBuffer is the buffer size for each subscriber's channel.
	DefaultSubscriberBuffer = 100
)

// Hub manages log broadcasting to subscribers with a ring buffer for history.
type Hub struct {
	mu          sync.RWMutex
	buffer      []string
	bufferSize  int
	writePos    int
	count       int
	subscribers map[*Subscriber]struct{}
}

// Subscriber represents a log stream subscriber with a buffered channel.
type Subscriber struct {
	ch     chan string
	ctx    context.Context
	cancel context.CancelFunc
}

// NewHub creates a new Hub with the specified buffer size.
// If size <= 0, DefaultBufferSize is used.
func NewHub(size int) *Hub {
	if size <= 0 {
		size = DefaultBufferSize
	}
	return &Hub{
		buffer:      make([]string, size),
		bufferSize:  size,
		subscribers: make(map[*Subscriber]struct{}),
	}
}

// Write appends a log line to the ring buffer and broadcasts to subscribers.
// Lines that exceed subscriber buffer capacity are dropped (slow consumer protection).
func (h *Hub) Write(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Write to ring buffer
	h.buffer[h.writePos] = line
	h.writePos = (h.writePos + 1) % h.bufferSize
	if h.count < h.bufferSize {
		h.count++
	}

	// Broadcast to subscribers (non-blocking, drop on full)
	// Must be inside lock to prevent send-on-closed-channel panic
	// when Unsubscribe closes a channel concurrently.
	for sub := range h.subscribers {
		select {
		case sub.ch <- line:
		default:
			// Drop line for slow consumer
		}
	}
}

// History returns the last n lines from the ring buffer.
// If n <= 0 or n > count, returns all available lines.
func (h *Hub) History(n int) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n <= 0 || n > h.count {
		n = h.count
	}
	if n == 0 {
		return nil
	}

	result := make([]string, n)
	// Calculate start position in ring buffer
	start := (h.writePos - n + h.bufferSize) % h.bufferSize
	for i := range n {
		result[i] = h.buffer[(start+i)%h.bufferSize]
	}
	return result
}

// Subscribe creates a new subscriber that receives log lines.
// The returned Subscriber should be unsubscribed when done.
func (h *Hub) Subscribe(ctx context.Context) *Subscriber {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &Subscriber{
		ch:     make(chan string, DefaultSubscriberBuffer),
		ctx:    subCtx,
		cancel: cancel,
	}

	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()

	// Auto-unsubscribe when context is done
	go func() {
		<-subCtx.Done()
		h.Unsubscribe(sub)
	}()

	return sub
}

// Unsubscribe removes a subscriber and closes its channel.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	if _, ok := h.subscribers[sub]; ok {
		delete(h.subscribers, sub)
		sub.cancel()
		close(sub.ch)
	}
	h.mu.Unlock()
}

// Channel returns the subscriber's log line channel.
func (s *Subscriber) Channel() <-chan string {
	return s.ch
}

// Done returns the subscriber's context done channel.
func (s *Subscriber) Done() <-chan struct{} {
	return s.ctx.Done()
}

// SubscriberCount returns the current number of subscribers.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// Count returns the number of lines currently in the buffer.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}
