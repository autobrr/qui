// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBufferPool(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool()
	require.NotNil(t, pool)
}

func TestBufferPool_Get(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool()

	buf := pool.Get()
	require.NotNil(t, buf)

	// Should be 32KB
	assert.Len(t, buf, 32*1024)
}

func TestBufferPool_GetPut(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool()

	// Get a buffer
	buf := pool.Get()
	require.NotNil(t, buf)

	// Modify it
	buf[0] = 'x'

	// Put it back
	pool.Put(buf)

	// Get another buffer - may or may not be the same one
	buf2 := pool.Get()
	require.NotNil(t, buf2)
	assert.Len(t, buf2, 32*1024)
}

func TestBufferPool_Concurrent(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool()
	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				buf := pool.Get()
				assert.NotNil(t, buf)
				assert.Len(t, buf, 32*1024)

				// Do some work with the buffer
				buf[0] = byte(j)
				buf[len(buf)-1] = byte(j)

				pool.Put(buf)
			}
		}()
	}

	wg.Wait()
}

func TestBufferPool_Size(t *testing.T) {
	t.Parallel()

	pool := NewBufferPool()

	// Get multiple buffers and verify they're all 32KB
	buffers := make([][]byte, 10)
	for i := range buffers {
		buffers[i] = pool.Get()
		assert.Len(t, buffers[i], 32*1024)
	}

	// Put them all back
	for _, buf := range buffers {
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_GetPut(b *testing.B) {
	pool := NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_Concurrent(b *testing.B) {
	pool := NewBufferPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			pool.Put(buf)
		}
	})
}
