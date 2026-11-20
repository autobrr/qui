// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logstream

import (
	"context"
	"encoding/json/v2"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

const (
	DefaultBufferSize       = 1000
	DefaultSubscriberBuffer = 100
)

type Hub struct {
	mu          sync.RWMutex
	buffer      []string
	bufferSize  int
	writePos    int
	count       int
	subscribers map[*Subscriber]struct{}
}

type Subscriber struct {
	ch     chan string
	ctx    context.Context
	cancel context.CancelFunc
}

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
		}
	}
}

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
	start := (h.writePos - n + h.bufferSize) % h.bufferSize
	for i := range n {
		result[i] = h.buffer[(start+i)%h.bufferSize]
	}
	return result
}

// HistoryFrom returns the last n lines at minLevel or above, oldest first.
// It filters before it counts, so n is the number of matching lines returned
// when the buffer holds that many. If n <= 0, it returns every matching line.
// A line whose level it cannot read is always kept.
func (h *Hub) HistoryFrom(minLevel zerolog.Level, n int) []string {
	lines := h.History(0)
	var result []string
	for _, line := range slices.Backward(lines) {
		if n > 0 && len(result) == n {
			break
		}
		if level, ok := lineLevel(line); ok && level < minLevel {
			continue
		}
		result = append(result, line)
	}
	slices.Reverse(result)
	return result
}

var ansiEscapeRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var consoleLevels = func() map[string]zerolog.Level {
	levels := make(map[string]zerolog.Level, len(zerolog.FormattedLevels))
	for level, word := range zerolog.FormattedLevels {
		levels[word] = level
	}
	return levels
}()

// lineLevel reads the level field of a zerolog JSON line, or the level word
// of a line in zerolog's console format (timestamp, then level word).
func lineLevel(line string) (zerolog.Level, bool) {
	if strings.HasPrefix(line, "{") {
		var entry struct {
			Level string `json:"level"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Level == "" {
			return zerolog.NoLevel, false
		}
		level, err := zerolog.ParseLevel(entry.Level)
		return level, err == nil
	}

	words := 0
	for word := range strings.FieldsSeq(ansiEscapeRegex.ReplaceAllString(line, "")) {
		if level, ok := consoleLevels[word]; ok {
			return level, true
		}
		if words++; words == 2 {
			break
		}
	}
	return zerolog.NoLevel, false
}

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

	go func() {
		<-subCtx.Done()
		h.Unsubscribe(sub)
	}()

	return sub
}

func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	if _, ok := h.subscribers[sub]; ok {
		delete(h.subscribers, sub)
		sub.cancel()
		close(sub.ch)
	}
	h.mu.Unlock()
}

func (s *Subscriber) Channel() <-chan string {
	return s.ch
}

func (s *Subscriber) Done() <-chan struct{} {
	return s.ctx.Done()
}
