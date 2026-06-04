// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// blockingResponseWriter is a fake http.ResponseWriter whose Write parks on a
// gate channel until the test releases it, simulating a stalled client socket.
// SetWriteDeadline/Flush via http.NewResponseController route through these
// methods (it discovers Flush() and Unwrap()), but the buffered writer drives
// the rolling deadline through its own controller over the real socket; here we
// only need Write to block so the drain goroutine parks.
type blockingResponseWriter struct {
	gate    chan struct{}
	header  http.Header
	writes  atomic.Int32
	flushes atomic.Int32
}

func newBlockingResponseWriter() *blockingResponseWriter {
	return &blockingResponseWriter{
		gate:   make(chan struct{}),
		header: make(http.Header),
	}
}

func (w *blockingResponseWriter) Header() http.Header { return w.header }

func (w *blockingResponseWriter) Write(p []byte) (int, error) {
	w.writes.Add(1)
	<-w.gate // park until released
	return len(p), nil
}

func (w *blockingResponseWriter) WriteHeader(int) {}

func (w *blockingResponseWriter) Flush() { w.flushes.Add(1) }

func (w *blockingResponseWriter) release() { close(w.gate) }

// TestBufferedWriterPreservesGoSSECapabilities verifies the buffered writer keeps
// the exact interface shape go-sse's getResponseWriter detects: it must implement
// http.Flusher (a Flush() with no return) and Unwrap() http.ResponseWriter, and
// must NOT implement interface{ FlushError() error } (which would make go-sse pick
// the FlushError path instead of writeFlusher, changing behavior).
func TestBufferedWriterPreservesGoSSECapabilities(t *testing.T) {
	// Compile-time guarantee of the required shape.
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

// TestOverflowingClientIsDropped is the deterministic, race-safe proof that a
// client which cannot keep up is dropped without blocking the caller: Flush never
// blocks, an overflowing queue trips the drop (cancel called once, failed set),
// subsequent Write returns errSlowClient, and Close tears the drain goroutine
// down cleanly once the socket unblocks.
func TestOverflowingClientIsDropped(t *testing.T) {
	rw := newBlockingResponseWriter()

	var cancelCalls atomic.Int32
	cancel := func() { cancelCalls.Add(1) }

	rc := http.NewResponseController(rw)
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, cancel)
	bw.enableBuffering() // exercise the post-subscribe buffered/drain path

	// Enqueue more messages than the queue can hold. The drain goroutine pulls the
	// first message and parks on the blocked socket Write, so the queue (capacity
	// maxQueuedMessages) fills and the next Flush trips the overflow drop.
	flushMessage := func(i int) {
		_, err := bw.Write([]byte{byte(i)})
		if err != nil {
			return // already dropped; staging path short-circuits
		}
		bw.Flush()
	}
	for i := range maxQueuedMessages + 2 {
		flushMessage(i)
	}

	// Overflow must have dropped exactly this client.
	require.Eventually(t, func() bool {
		return cancelCalls.Load() == 1
	}, time.Second, 5*time.Millisecond, "cancel must be invoked exactly once on overflow")
	require.True(t, bw.failed.Load(), "writer must be marked failed after overflow")

	// A subsequent Write reports the drop so go-sse's next Send removes the session.
	_, err := bw.Write([]byte("x"))
	require.ErrorIs(t, err, errSlowClient, "Write after drop must return errSlowClient")

	// Releasing the socket and closing must tear down the drain goroutine without
	// deadlock; -race plus the <-w.done in Close proves no leak.
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

	// cancel must remain idempotent: still exactly one call (Close does not re-drop).
	require.Equal(t, int32(1), cancelCalls.Load(), "cancel must be called exactly once across the lifecycle")
}

// TestFlushReturnsImmediatelyWhileSocketBlocks proves Flush is non-blocking: the
// drain goroutine is parked on a blocked socket Write, yet a Flush returns
// promptly (the message lands in the bounded queue) instead of waiting on the
// stuck socket. This is the core isolation property at the unit level.
func TestFlushReturnsImmediatelyWhileSocketBlocks(t *testing.T) {
	rw := newBlockingResponseWriter()
	rc := http.NewResponseController(rw)
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, func() {})
	bw.enableBuffering() // exercise the post-subscribe buffered/drain path
	t.Cleanup(func() {
		rw.release()
		bw.Close()
	})

	// First message: the drain goroutine will dequeue it and park on the blocked
	// socket Write. Wait until it has actually entered Write so the next Flush
	// proves non-blocking against a genuinely stuck drain.
	_, err := bw.Write([]byte("first"))
	require.NoError(t, err)
	bw.Flush()
	require.Eventually(t, func() bool {
		return rw.writes.Load() == 1
	}, time.Second, 5*time.Millisecond, "drain goroutine should park on the blocked socket write")

	// A second Flush must return immediately even though the socket is stuck.
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

// TestSlowClientDoesNotBlockOthers is the end-to-end isolation check: with two
// SSE connections on the same group, a connection whose reader stops draining
// (its buffered events channel fills) must not prevent the other connection from
// promptly receiving an update. Each session now has its own buffered writer and
// drain goroutine, so B's delivery is independent of A.
// TestSlowClientDoesNotBlockOthers is an end-to-end smoke check that two SSE
// connections on the same group are served independently: connection B promptly
// receives an update while connection A merely sits idle after its init.
//
// Note this does NOT by itself prove socket-level isolation — over httptest
// loopback A's reader goroutine keeps draining its body and a single update never
// back-pressures A's socket, so the assertion would also pass against the old
// shared serial fan-out. The actual "a stalled socket cannot block other
// sessions" guarantee is proven deterministically at the unit level by
// TestFlushReturnsImmediatelyWhileSocketBlocks and TestOverflowingClientIsDropped,
// which park a real socket write on a gate.
func TestSlowClientDoesNotBlockOthers(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	srv := startStreamServer(t, manager)

	// Connection A: read its init so it subscribes, then leave its reader idle.
	readerA, _ := connectStream(t, srv, streamPayload(instanceID, "slow-A"))
	readerA.waitForEvent(t, streamEventInit, 5*time.Second)

	// Connection B: a healthy fast reader on the same view (same group).
	readerB, _ := connectStream(t, srv, streamPayload(instanceID, "fast-B"))
	readerB.waitForEvent(t, streamEventInit, 5*time.Second)

	// Fire updates; B must receive one promptly while A's reader sits idle.
	updateB := readerB.waitForEventTriggered(t, streamEventUpdate, 5*time.Second, func() {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1, FullUpdate: true})
	})
	require.Equal(t, streamEventUpdate, updateB.event)
}

// TestFlushAfterCloseDoesNotPanic guards the lifecycle race that makes closing
// the queue unsafe: go-sse's Joe dispatch loop can call Send+Flush on this writer
// after Serve's ServeHTTP returns (Joe.Subscribe returns as soon as it queues the
// unsubscription, before the loop drops the subscriber), so a final fan-out can
// run concurrently with Close. Because queue is never closed, those late Flushes
// must not panic with "send on closed channel"; they land in the bounded buffer or
// trip the overflow drop instead.
func TestFlushAfterCloseDoesNotPanic(t *testing.T) {
	rw := newBlockingResponseWriter()
	rw.release() // never block the drain goroutine
	rc := http.NewResponseController(rw)
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, func() {})
	bw.enableBuffering() // exercise the post-subscribe buffered/drain path

	// Close stops the drain (Serve returned); the queue stays open by design.
	bw.Close()

	// Simulate go-sse's loop doing more Send+Flush cycles after Close than the
	// bounded queue can hold. None may panic.
	require.NotPanics(t, func() {
		for range maxQueuedMessages + 5 {
			if _, err := bw.Write([]byte("late")); err != nil {
				continue // dropped after overflow; staging short-circuits
			}
			bw.Flush()
		}
	})
}
