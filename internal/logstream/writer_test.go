// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logstream

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

func TestSwitchableWriter_Write(t *testing.T) {
	var buf bytes.Buffer
	hub := NewHub(100)
	sw := NewSwitchableWriter(&buf, hub)

	_, err := sw.Write([]byte("hello world\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if buf.String() != "hello world\n" {
		t.Errorf("expected 'hello world\\n', got %q", buf.String())
	}

	// Check that line was captured to hub
	history := hub.History(10)
	if len(history) != 1 {
		t.Errorf("expected 1 line in hub, got %d", len(history))
	}
	if history[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", history[0])
	}
}

func TestSwitchableWriter_MultipleLines(t *testing.T) {
	var buf bytes.Buffer
	hub := NewHub(100)
	sw := NewSwitchableWriter(&buf, hub)

	_, err := sw.Write([]byte("line1\nline2\nline3\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	history := hub.History(10)
	if len(history) != 3 {
		t.Errorf("expected 3 lines, got %d", len(history))
	}

	expected := []string{"line1", "line2", "line3"}
	for i, line := range history {
		if line != expected[i] {
			t.Errorf("expected %q, got %q", expected[i], line)
		}
	}
}

func TestSwitchableWriter_PartialLines(t *testing.T) {
	var buf bytes.Buffer
	hub := NewHub(100)
	sw := NewSwitchableWriter(&buf, hub)

	// Write partial line
	_, err := sw.Write([]byte("partial"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// No complete line yet
	history := hub.History(10)
	if len(history) != 0 {
		t.Errorf("expected 0 lines, got %d", len(history))
	}

	// Complete the line
	_, err = sw.Write([]byte(" complete\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	history = hub.History(10)
	if len(history) != 1 {
		t.Errorf("expected 1 line, got %d", len(history))
	}
	if history[0] != "partial complete" {
		t.Errorf("expected 'partial complete', got %q", history[0])
	}
}

func TestSwitchableWriter_Swap(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	hub := NewHub(100)
	sw := NewSwitchableWriter(&buf1, hub)

	// Write to first buffer
	_, err := sw.Write([]byte("first\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Swap to second buffer
	sw.Swap(&buf2, nil)

	// Write to second buffer
	_, err = sw.Write([]byte("second\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if buf1.String() != "first\n" {
		t.Errorf("buf1: expected 'first\\n', got %q", buf1.String())
	}
	if buf2.String() != "second\n" {
		t.Errorf("buf2: expected 'second\\n', got %q", buf2.String())
	}
}

func TestSwitchableWriter_SwapCloser(t *testing.T) {
	var buf bytes.Buffer
	hub := NewHub(100)
	sw := NewSwitchableWriter(&buf, hub)

	// Create a mock closer
	closed := false
	mockCloser := &mockWriter{
		w:       &bytes.Buffer{},
		closeFn: func() error { closed = true; return nil },
	}

	// Swap with a closer
	sw.Swap(mockCloser.w, mockCloser)

	// Swap again, should return the old closer
	oldCloser := sw.Swap(&buf, nil)
	if oldCloser == nil {
		t.Error("expected to get old closer back")
	}

	// Close the old closer
	if err := oldCloser.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}

	if !closed {
		t.Error("expected closer to be closed")
	}
}

type mockWriter struct {
	w       io.Writer
	closeFn func() error
}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	return m.w.Write(p)
}

func (m *mockWriter) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func TestSwitchableWriter_ConcurrentWrite(t *testing.T) {
	// Use a thread-safe writer for the test
	hub := NewHub(1000)
	sw := NewSwitchableWriter(&safeBuffer{}, hub)

	var wg sync.WaitGroup
	numWriters := 10
	linesPerWriter := 100

	for range numWriters {
		wg.Go(func() {
			for range linesPerWriter {
				sw.Write([]byte("line\n"))
			}
		})
	}

	wg.Wait()

	// Verify all lines were captured
	if hub.Count() != 1000 {
		t.Errorf("expected 1000 lines, got %d", hub.Count())
	}
}

// safeBuffer is a thread-safe bytes.Buffer for testing.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *safeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func TestSwitchableWriter_NilHub(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSwitchableWriter(&buf, nil)

	_, err := sw.Write([]byte("test\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	if buf.String() != "test\n" {
		t.Errorf("expected 'test\\n', got %q", buf.String())
	}
}

func TestSwitchableWriter_GetHub(t *testing.T) {
	hub := NewHub(100)
	sw := NewSwitchableWriter(&bytes.Buffer{}, hub)

	if sw.GetHub() != hub {
		t.Error("GetHub returned different hub")
	}
}
