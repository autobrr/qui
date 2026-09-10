// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net/http"
	"net/http/httptest"
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
