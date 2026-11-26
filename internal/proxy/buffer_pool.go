// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import "sync"

type BufferPool struct {
	pool sync.Pool
}

func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, 32*1024)
				return &buf
			},
		},
	}
}

func (p *BufferPool) Get() []byte {
	return *p.pool.Get().(*[]byte)
}

func (p *BufferPool) Put(buf []byte) {
	p.pool.Put(&buf)
}
