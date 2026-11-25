// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package logstream

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
)

type SwitchableWriter struct {
	target atomic.Pointer[writerWithCloser]
	hub    *Hub
	mu     sync.Mutex
	buf    bytes.Buffer
}

type writerWithCloser struct {
	w      io.Writer
	closer io.Closer
}

func NewSwitchableWriter(initial io.Writer, hub *Hub) *SwitchableWriter {
	sw := &SwitchableWriter{
		hub: hub,
	}
	sw.target.Store(&writerWithCloser{w: initial})
	return sw
}

func (sw *SwitchableWriter) Write(p []byte) (n int, err error) {
	target := sw.target.Load()
	if target == nil || target.w == nil {
		return 0, nil
	}

	n, err = target.w.Write(p)
	if err != nil {
		return n, err
	}

	if sw.hub != nil {
		sw.captureLine(p[:n])
	}

	return n, nil
}

func (sw *SwitchableWriter) captureLine(p []byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.buf.Write(p)

	for {
		line, err := sw.buf.ReadString('\n')
		if err != nil {
			sw.buf.WriteString(line)
			break
		}
		if line != "" && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if line != "" {
			sw.hub.Write(line)
		}
	}
}

func (sw *SwitchableWriter) Swap(newWriter io.Writer, newCloser io.Closer) io.Closer {
	old := sw.target.Swap(&writerWithCloser{w: newWriter, closer: newCloser})
	if old != nil {
		return old.closer
	}
	return nil
}
