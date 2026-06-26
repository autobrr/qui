// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logstream

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestHub_Write(t *testing.T) {
	hub := NewHub(10)

	// Write some lines
	for i := range 5 {
		hub.Write("line " + string(rune('0'+i)))
	}

	if hub.Count() != 5 {
		t.Errorf("expected count 5, got %d", hub.Count())
	}
}

func TestHub_RingBuffer(t *testing.T) {
	hub := NewHub(5)

	// Write more lines than buffer size
	for i := range 10 {
		hub.Write("line " + string(rune('0'+i)))
	}

	// Should only have last 5 lines
	if hub.Count() != 5 {
		t.Errorf("expected count 5, got %d", hub.Count())
	}

	history := hub.History(10) // Request more than available
	if len(history) != 5 {
		t.Errorf("expected 5 lines, got %d", len(history))
	}

	// Verify the content is the last 5 lines
	for i, line := range history {
		expected := "line " + string(rune('5'+i))
		if line != expected {
			t.Errorf("expected %q, got %q", expected, line)
		}
	}
}

func TestHub_HistoryPartial(t *testing.T) {
	hub := NewHub(100)

	// Write 10 lines
	for i := range 10 {
		hub.Write("line " + string(rune('0'+i)))
	}

	// Request only 3 lines
	history := hub.History(3)
	if len(history) != 3 {
		t.Errorf("expected 3 lines, got %d", len(history))
	}

	// Should be the last 3 lines
	expected := []string{"line 7", "line 8", "line 9"}
	for i, line := range history {
		if line != expected[i] {
			t.Errorf("expected %q, got %q", expected[i], line)
		}
	}
}

func TestHub_Subscribe(t *testing.T) {
	hub := NewHub(100)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sub := hub.Subscribe(ctx)

	// Write a line after subscribing
	hub.Write("test line")

	select {
	case line := <-sub.Channel():
		if line != "test line" {
			t.Errorf("expected 'test line', got %q", line)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for line")
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	hub := NewHub(100)
	ctx := context.Background()

	sub := hub.Subscribe(ctx)

	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", hub.SubscriberCount())
	}

	hub.Unsubscribe(sub)

	if hub.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers, got %d", hub.SubscriberCount())
	}

	// Channel should be closed
	_, ok := <-sub.Channel()
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestHub_ContextCancel(t *testing.T) {
	hub := NewHub(100)
	ctx, cancel := context.WithCancel(context.Background())

	sub := hub.Subscribe(ctx)

	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", hub.SubscriberCount())
	}

	cancel()

	// Give time for the goroutine to process the cancellation
	time.Sleep(50 * time.Millisecond)

	if hub.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers after context cancel, got %d", hub.SubscriberCount())
	}

	// Verify Done channel is closed
	select {
	case <-sub.Done():
		// Expected
	default:
		t.Error("expected Done channel to be closed")
	}
}

func TestHub_SlowConsumer(t *testing.T) {
	hub := NewHub(100)

	sub := hub.Subscribe(t.Context())

	// Fill up the subscriber's buffer (100 messages) plus more
	for range 200 {
		hub.Write("line")
	}

	// Drain the channel
	count := 0
	for {
		select {
		case <-sub.Channel():
			count++
		default:
			goto done
		}
	}
done:

	// Should have received at most the buffer size
	if count > DefaultSubscriberBuffer {
		t.Errorf("expected at most %d messages, got %d", DefaultSubscriberBuffer, count)
	}

	hub.Unsubscribe(sub)
}

func TestHub_ConcurrentWrite(t *testing.T) {
	hub := NewHub(1000)

	sub := hub.Subscribe(t.Context())
	defer hub.Unsubscribe(sub)

	var wg sync.WaitGroup
	numWriters := 10
	linesPerWriter := 100

	for range numWriters {
		wg.Go(func() {
			for range linesPerWriter {
				hub.Write("line")
			}
		})
	}

	wg.Wait()

	// Verify count
	if hub.Count() != 1000 {
		t.Errorf("expected count 1000, got %d", hub.Count())
	}
}

func TestHub_HistoryEmpty(t *testing.T) {
	hub := NewHub(100)

	history := hub.History(10)
	if history != nil {
		t.Errorf("expected nil history for empty hub, got %v", history)
	}
}

func TestHub_DefaultSize(t *testing.T) {
	hub := NewHub(0)

	// Should use default size
	for range DefaultBufferSize + 100 {
		hub.Write("line")
	}

	if hub.Count() != DefaultBufferSize {
		t.Errorf("expected count %d, got %d", DefaultBufferSize, hub.Count())
	}
}
