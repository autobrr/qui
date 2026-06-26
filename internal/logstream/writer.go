// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logstream

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
)

// SwitchableWriter is an io.Writer that allows atomic swapping of the underlying writer.
// It also captures complete log lines and broadcasts them to a Hub.
type SwitchableWriter struct {
	target atomic.Pointer[writerWithCloser]
	hub    *Hub
	mu     sync.Mutex // protects partial line buffer during writes
	buf    bytes.Buffer
}

type writerWithCloser struct {
	w      io.Writer
	closer io.Closer // optional, may be nil
}

// NewSwitchableWriter creates a new SwitchableWriter with the given initial writer and hub.
func NewSwitchableWriter(initial io.Writer, hub *Hub) *SwitchableWriter {
	sw := &SwitchableWriter{
		hub: hub,
	}
	sw.target.Store(&writerWithCloser{w: initial})
	return sw
}

// Write writes data to the underlying writer and captures complete lines for the hub.
// Note: We don't wrap the error here since SwitchableWriter implements io.Writer
// and callers expect standard Write semantics.
func (sw *SwitchableWriter) Write(p []byte) (n int, err error) {
	target := sw.target.Load()
	if target == nil || target.w == nil {
		return 0, nil
	}

	// Write to the actual target - errors are not wrapped since this is
	// an io.Writer implementation and callers expect standard semantics.
	n, err = target.w.Write(p)
	if err != nil {
		return n, err //nolint:wrapcheck // io.Writer interface compliance
	}

	// Capture lines for the hub if we have one
	if sw.hub != nil {
		sw.captureLine(p[:n])
	}

	return n, nil
}

// captureLine accumulates bytes and broadcasts complete lines to the hub.
func (sw *SwitchableWriter) captureLine(p []byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.buf.Write(p)

	// Process complete lines
	for {
		line, err := sw.buf.ReadString('\n')
		if err != nil {
			// No complete line yet, put partial back
			sw.buf.WriteString(line)
			break
		}
		// Trim the newline and broadcast
		if line != "" && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if line != "" {
			sw.hub.Write(line)
		}
	}
}

// Swap atomically replaces the underlying writer and returns the old closer (if any).
// The caller is responsible for closing the returned closer after the swap.
func (sw *SwitchableWriter) Swap(newWriter io.Writer, newCloser io.Closer) io.Closer {
	old := sw.target.Swap(&writerWithCloser{w: newWriter, closer: newCloser})
	if old != nil {
		return old.closer
	}
	return nil
}

// GetHub returns the Hub associated with this writer.
func (sw *SwitchableWriter) GetHub() *Hub {
	return sw.hub
}
