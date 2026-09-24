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

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	t.Cleanup(cancel)

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

	mu.Lock()
	defer mu.Unlock()
	// One idle keep-alive connection is reuse, not a leak.
	require.LessOrEqual(t, len(open), 1)
}
