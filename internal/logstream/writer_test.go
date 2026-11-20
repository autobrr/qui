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

	_, err := sw.Write([]byte("partial"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	history := hub.History(10)
	if len(history) != 0 {
		t.Errorf("expected 0 lines, got %d", len(history))
	}

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

	_, err := sw.Write([]byte("first\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}

	sw.Swap(&buf2, nil)

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

	closed := false
	mockCloser := &mockWriter{
		w:       &bytes.Buffer{},
		closeFn: func() error { closed = true; return nil },
	}

	sw.Swap(mockCloser.w, mockCloser)

	oldCloser := sw.Swap(&buf, nil)
	if oldCloser == nil {
		t.Error("expected to get old closer back")
	}

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
	hub := NewHub(1000)
	sw := NewSwitchableWriter(&safeBuffer{}, hub)

	var wg sync.WaitGroup
	numWriters := 10
	linesPerWriter := 100

	for range numWriters {
		wg.Go(func() {
			for range linesPerWriter {
				_, _ = sw.Write([]byte("line\n"))
			}
		})
	}

	wg.Wait()

	if got := len(hub.History(0)); got != 1000 {
		t.Errorf("expected 1000 lines, got %d", got)
	}
}

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
