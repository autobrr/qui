// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import "sync"

// BufferPool provides a thread-safe pool of byte slices for the reverse proxy
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool with 32KB buffers
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() any {
				// Create 32KB buffers - good balance between memory usage and performance
				return make([]byte, 32*1024)
			},
		},
	}
}

// Get returns a buffer from the pool
func (p *BufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buf []byte) {
	p.pool.Put(buf)
}
