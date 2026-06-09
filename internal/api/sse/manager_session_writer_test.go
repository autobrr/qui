// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestInitPhaseWritesDirectlyToSocket pins the session writer's two-phase
// contract: before the session is subscribed (the onSession init phase), Write
// must reach the socket synchronously on the calling goroutine, with no drain
// goroutine involved and nothing queued. onSession relies on this so its
// per-subscription init snapshots never consume the bounded queue (no false
// overflow drop on connections with many streams) and so the request goroutine
// is the sole writer during init (it cannot race the drain on the response
// header map).
func TestInitPhaseWritesDirectlyToSocket(t *testing.T) {
	rec := httptest.NewRecorder()
	rc := http.NewResponseController(rec)
	bw := newBufferedSessionWriter(rec, rc, streamWriteTimeout, func() {})
	t.Cleanup(bw.Close)

	_, err := bw.Write([]byte("init-snapshot"))
	require.NoError(t, err)
	require.Equal(t, "init-snapshot", rec.Body.String(),
		"init-phase Write must reach the socket synchronously, before any Flush")
}

// TestMultiSubscriptionInitIsRaceFree connects a single multiplexed SSE
// connection that requests several distinct stream views — the normal frontend
// case, where the torrents table, title-bar speeds, and per-instance dashboard
// all ride one EventSource. onSession writes one init snapshot per subscription;
// with the single-phase buffered writer those synchronous Header().Set writes on
// the request goroutine raced the drain goroutine's first socket write on the
// response header map. Run under -race: the two-phase writer keeps the init
// writes synchronous (and off the bounded queue), so they precede the drain and
// cannot race or overflow-drop a healthy client.
func TestMultiSubscriptionInitIsRaceFree(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	srv := startStreamServer(t, manager)

	// Exceed the bounded queue so the test also guards the >maxQueuedMessages
	// init-burst false-drop, not just the header-map race.
	subscriptions := maxQueuedMessages + 4

	for c := range 3 {
		payload := make([]map[string]any, 0, subscriptions)
		for i := range subscriptions {
			payload = append(payload, map[string]any{
				"key":        fmt.Sprintf("conn%d-view%d", c, i),
				"instanceId": instanceID,
				"page":       i, // distinct page => distinct subscription, no dedupe
				"limit":      50,
				"sort":       "added_on",
				"order":      "desc",
				"search":     "",
				"filters":    nil,
			})
		}

		reader, _ := connectStream(t, srv, payload)

		inits := 0
		deadline := time.After(5 * time.Second)
		for inits < subscriptions {
			select {
			case ev := <-reader.events:
				if ev.event == streamEventInit {
					inits++
				}
			case err := <-reader.errc:
				t.Fatalf("conn %d: stream closed after %d/%d init snapshots: %v", c, inits, subscriptions, err)
			case <-deadline:
				t.Fatalf("conn %d: received only %d/%d init snapshots before timeout", c, inits, subscriptions)
			}
		}
	}
}

// erroringResponseWriter is a fake socket whose Write always fails, used to drive
// the drain goroutine's write-error drop path — the mechanism that drops a stuck-
// then-erroring client when its rolling streamWriteTimeout deadline fires.
type erroringResponseWriter struct {
	header http.Header
}

func (w *erroringResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *erroringResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("socket write failed")
}

func (w *erroringResponseWriter) WriteHeader(int) {}

func (w *erroringResponseWriter) Flush() {}

// TestDrainDropsOnWriteError covers the drain goroutine's error path (distinct
// from the Flush queue-overflow drop): when a queued socket write fails, the
// drain drops only this session — cancel fires once, the writer is marked failed
// — and the goroutine exits cleanly without leaking.
func TestDrainDropsOnWriteError(t *testing.T) {
	rw := &erroringResponseWriter{}
	rc := http.NewResponseController(rw)
	var cancelCalls atomic.Int32
	bw := newBufferedSessionWriter(rw, rc, streamWriteTimeout, func() { cancelCalls.Add(1) })
	bw.enableBuffering()

	_, err := bw.Write([]byte("x"))
	require.NoError(t, err)
	bw.Flush() // enqueue; drain dequeues, the socket write errors -> drop + return

	require.Eventually(t, func() bool {
		return cancelCalls.Load() == 1 && bw.failed.Load()
	}, time.Second, 5*time.Millisecond, "drain must drop the session when the socket write errors")

	// The drain goroutine must have exited; Close returns without hanging.
	done := make(chan struct{})
	go func() {
		bw.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return; drain goroutine leaked after a write error")
	}
	require.Equal(t, int32(1), cancelCalls.Load(), "cancel must fire exactly once")
}
