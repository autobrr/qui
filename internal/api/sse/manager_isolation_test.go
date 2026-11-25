// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type blockingResponseWriter struct {
	gate    chan struct{}
	entered chan struct{}
	enter   func()
	header  http.Header
	flushes atomic.Int32
}

func newBlockingResponseWriter() *blockingResponseWriter {
	entered := make(chan struct{})
	return &blockingResponseWriter{
		gate:    make(chan struct{}),
		entered: entered,
		enter:   sync.OnceFunc(func() { close(entered) }),
		header:  make(http.Header),
	}
}

func (w *blockingResponseWriter) Header() http.Header { return w.header }

func (w *blockingResponseWriter) Write(p []byte) (int, error) {
	w.enter()
	<-w.gate
	return len(p), nil
}

func (w *blockingResponseWriter) WriteHeader(int) {}

func (w *blockingResponseWriter) Flush() { w.flushes.Add(1) }

func (w *blockingResponseWriter) release() { close(w.gate) }

func TestBufferedWriterPreservesGoSSECapabilities(t *testing.T) {
	var _ interface {
		Flush()
		Unwrap() http.ResponseWriter
		http.ResponseWriter
	} = (*bufferedSessionWriter)(nil)

	bw := newBufferedSessionWriter(newBlockingResponseWriter(), nil, streamWriteTimeout, func() {})
	t.Cleanup(bw.Close)

	_, isFlusher := any(bw).(http.Flusher)
	require.True(t, isFlusher, "buffered writer must implement http.Flusher")

	_, hasUnwrap := any(bw).(interface{ Unwrap() http.ResponseWriter })
	require.True(t, hasUnwrap, "buffered writer must implement Unwrap")

	_, hasFlushError := any(bw).(interface{ FlushError() error })
	require.False(t, hasFlushError, "buffered writer must NOT implement FlushError (would change go-sse writer detection)")
}

func TestOverflowingClientIsDropped(t *testing.T) {
	rw := newBlockingResponseWriter()

	var cancelCalls atomic.Int32
	cancel := func() { cancelCalls.Add(1) }

	rc := http.NewResponseController(rw)
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, cancel)
	bw.enableBuffering()

	flushMessage := func(i int) {
		_, err := bw.Write([]byte{byte(i)})
		if err != nil {
			return
		}
		bw.Flush()
	}
	for i := range maxQueuedMessages + 2 {
		flushMessage(i)
	}

	require.Equal(t, int32(1), cancelCalls.Load(), "cancel must be invoked exactly once on overflow")
	require.True(t, bw.failed.Load(), "writer must be marked failed after overflow")

	_, err := bw.Write([]byte("x"))
	require.ErrorIs(t, err, errSlowClient, "Write after drop must return errSlowClient")

	rw.release()
	done := make(chan struct{})
	go func() {
		bw.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return; drain goroutine leaked")
	}

	require.Equal(t, int32(1), cancelCalls.Load(), "cancel must be called exactly once across the lifecycle")
}

func TestFlushReturnsImmediatelyWhileSocketBlocks(t *testing.T) {
	rw := newBlockingResponseWriter()
	rc := http.NewResponseController(rw)
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, func() {})
	bw.enableBuffering()
	t.Cleanup(func() {
		rw.release()
		bw.Close()
	})

	_, err := bw.Write([]byte("first"))
	require.NoError(t, err)
	bw.Flush()
	receive(t, rw.entered, "the drain goroutine to park on the blocked socket write")

	_, err = bw.Write([]byte("second"))
	require.NoError(t, err)
	flushReturned := make(chan struct{})
	go func() {
		bw.Flush()
		close(flushReturned)
	}()
	select {
	case <-flushReturned:
	case <-time.After(time.Second):
		t.Fatal("Flush blocked while the underlying socket was stuck")
	}
}

func TestSlowClientDoesNotBlockOthers(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)

	readerA, _ := connectStream(t, srv, streamPayload(instanceID, "slow-A"))
	readerA.waitForEvent(t, streamEventInit, 5*time.Second)
	receive(t, subscribed, "connection A to subscribe")

	readerB, _ := connectStream(t, srv, streamPayload(instanceID, "fast-B"))
	readerB.waitForEvent(t, streamEventInit, 5*time.Second)
	receive(t, subscribed, "connection B to subscribe")

	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{})
	updateB := readerB.waitForTick(t, 5*time.Second)
	require.True(t, isTickEvent(updateB.event), "expected a tick frame, got %q", updateB.event)
}

func TestFlushAfterCloseDoesNotPanic(t *testing.T) {
	rw := newBlockingResponseWriter()
	rw.release()
	rc := http.NewResponseController(rw)
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, func() {})
	bw.enableBuffering()

	bw.Close()

	require.NotPanics(t, func() {
		for range maxQueuedMessages + 5 {
			if _, err := bw.Write([]byte("late")); err != nil {
				continue
			}
			bw.Flush()
		}
	})
}
