// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A cancelled caller must end the retry loop, not just the attempt in flight:
// otherwise a search whose deadline passed keeps retrying in the background
// and logs long after its caller has moved on.
func TestRetryDoStopsWhenContextEnds(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		<-arrived
		cancel()
	}()

	client := NewClient(Config{Host: server.URL})
	start := time.Now()
	_, err := client.GetTorrentsCtx(ctx, "tracker", map[string]string{})
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, time.Second, "retry loop kept running after the context ended")
}

// A 5xx response must not leave its connection open until Client.Timeout (#2817).
func TestRetryDoReleasesConnectionOn5xx(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	open := map[net.Conn]bool{}
	var allClosed chan struct{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	server.Config.ConnState = func(c net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch state {
		case http.StateNew:
			open[c] = true
		case http.StateClosed, http.StateHijacked:
			delete(open, c)
			if len(open) == 0 && allClosed != nil {
				close(allClosed)
				allClosed = nil
			}
		case http.StateActive, http.StateIdle:
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	for range 20 {
		_, err := client.GetTorrentsCtx(t.Context(), "tracker", map[string]string{})
		require.Error(t, err)
	}

	// A released connection is closed or idle in the pool, depending on whether
	// the client read the short body before the close. Close the idle ones, so
	// that only a leaked connection stays open.
	client.http.CloseIdleConnections()

	// The server sees each client close a moment after the call returns.
	closed := make(chan struct{})
	mu.Lock()
	if len(open) == 0 {
		close(closed)
	} else {
		allClosed = closed
	}
	mu.Unlock()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("a connection stayed open after its 5xx response")
	}
}

// A 5xx body that never ends must not hold the caller until the client timeout.
func TestRetryDoReturnsOn5xxWithoutReadingBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("partial"))
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{Host: server.URL})
	start := time.Now()
	_, err := client.GetTorrentsCtx(t.Context(), "tracker", map[string]string{})
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second, "drained a 5xx body until the client timeout")
}
