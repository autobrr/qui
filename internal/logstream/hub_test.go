// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logstream

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestHub_RingBuffer(t *testing.T) {
	hub := NewHub(5)

	for i := range 10 {
		hub.Write("line " + string(rune('0'+i)))
	}

	history := hub.History(10)
	if len(history) != 5 {
		t.Errorf("expected 5 lines, got %d", len(history))
	}

	for i, line := range history {
		expected := "line " + string(rune('5'+i))
		if line != expected {
			t.Errorf("expected %q, got %q", expected, line)
		}
	}
}

func TestHub_HistoryPartial(t *testing.T) {
	hub := NewHub(100)

	for i := range 10 {
		hub.Write("line " + string(rune('0'+i)))
	}

	history := hub.History(3)
	if len(history) != 3 {
		t.Errorf("expected 3 lines, got %d", len(history))
	}

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

	hub.Write("test line")

	select {
	case line := <-sub.Channel():
		if line != "test line" {
			t.Errorf("expected 'test line', got %q", line)
		}
	default:
		t.Error("expected the line on the subscriber channel")
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	hub := NewHub(100)
	ctx := context.Background()

	sub := hub.Subscribe(ctx)

	hub.Unsubscribe(sub)

	_, ok := <-sub.Channel()
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestHub_ContextCancel(t *testing.T) {
	hub := NewHub(100)
	ctx, cancel := context.WithCancel(context.Background())

	sub := hub.Subscribe(ctx)

	cancel()

	select {
	case _, ok := <-sub.Channel():
		if ok {
			t.Error("expected channel to be closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the auto-unsubscribe")
	}

	select {
	case <-sub.Done():
	default:
		t.Error("expected Done channel to be closed")
	}
}

func TestHub_SlowConsumer(t *testing.T) {
	hub := NewHub(100)

	sub := hub.Subscribe(t.Context())

	for range 200 {
		hub.Write("line")
	}

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

	if got := len(hub.History(0)); got != 1000 {
		t.Errorf("expected 1000 lines, got %d", got)
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

	for range DefaultBufferSize + 100 {
		hub.Write("line")
	}

	if got := len(hub.History(0)); got != DefaultBufferSize {
		t.Errorf("expected %d lines, got %d", DefaultBufferSize, got)
	}
}

func TestHub_HistoryFrom(t *testing.T) {
	lines := []string{
		`{"level":"debug","message":"d1"}`,
		`{"level":"warn","message":"w1"}`,
		`{"level":"info","message":"i1"}`,
		`{"level":"error","message":"e1"}`,
		"2026-11-17T10:00:00Z \x1b[33mWRN\x1b[0m w2",
		"2026-11-17T10:00:01Z \x1b[32mINF\x1b[0m i2",
		"2026-11-17T10:00:02Z ERR e2",
		"2026-11-17T10:00:03Z DBG d2",
		`not a log line`,
		`{"message":"no level"}`,
	}

	hub := NewHub(100)
	for _, line := range lines {
		hub.Write(line)
	}

	got := hub.HistoryFrom(zerolog.WarnLevel, 0)
	want := []string{lines[1], lines[3], lines[4], lines[6], lines[8], lines[9]}
	if !slices.Equal(got, want) {
		t.Fatalf("HistoryFrom(warn, 0) = %q, want %q", got, want)
	}
}

func TestHub_HistoryFromFiltersBeforeLastN(t *testing.T) {
	hub := NewHub(10)
	hub.Write(`{"level":"error","message":"e1"}`)
	hub.Write(`{"level":"warn","message":"w1"}`)
	for range 5 {
		hub.Write(`{"level":"info","message":"noise"}`)
	}
	hub.Write(`{"level":"warn","message":"w2"}`)

	got := hub.HistoryFrom(zerolog.WarnLevel, 2)
	want := []string{`{"level":"warn","message":"w1"}`, `{"level":"warn","message":"w2"}`}
	if !slices.Equal(got, want) {
		t.Fatalf("HistoryFrom(warn, 2) = %q, want %q", got, want)
	}
}
