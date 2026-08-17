// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// connectStreamGzip opens an SSE connection that advertises gzip and returns a
// reader over the decompressed body plus the response, so callers can assert on
// the negotiated encoding.
func connectStreamGzip(t *testing.T, srv *httptest.Server, payload []map[string]any) (*sseReader, *http.Response) {
	t.Helper()

	resp, _ := dialStream(t, srv, payload, "gzip")
	zr, err := gzip.NewReader(resp.Body)
	require.NoError(t, err, "response claimed gzip but the body is not a gzip stream")
	return newSSEReader(zr), resp
}

// TestGzipStreamFlushesEveryEvent is the reason this change is risky. A gzip
// writer that is not flushed per event holds bytes in the deflate buffer, so the
// init snapshot may still arrive (it is large enough to spill on its own) while
// every later tick sits in the compressor until the buffer fills. The visible
// failure is "the whole table stopped updating", not a decode error, so assert a
// second event arrives on a still-open compressed stream.
func TestGzipStreamFlushesEveryEvent(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	srv := startStreamServer(t, manager)

	reader, resp := connectStreamGzip(t, srv, streamPayload(instanceID, "stream-gzip-flush"))
	require.Equal(t, "gzip", resp.Header.Get("Content-Encoding"),
		"a client advertising gzip should get a compressed stream")

	initEvent := reader.waitForEvent(t, streamEventInit, 5*time.Second)
	initPayload := decodeStreamPayloadData(t, initEvent.data)
	require.Equal(t, streamEventInit, initPayload.Type)

	updateEvent := reader.waitForTickTriggered(t, 5*time.Second, func() {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 99, FullUpdate: true})
	})
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.True(t, isTickEvent(updatePayload.Type), "expected a tick frame, got %q", updatePayload.Type)
	require.Equal(t, canned.Total, updatePayload.Data.Total)
}

// TestStreamStaysPlainWithoutGzip pins the negotiated half. A reverse proxy that
// strips or refuses gzip must still get a readable stream, byte for byte what it
// got before compression existed.
func TestStreamStaysPlainWithoutGzip(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	srv := startStreamServer(t, manager)

	// DisableCompression keeps the transport from adding its own Accept-Encoding and
	// transparently decoding the reply, which would hide an unwanted Content-Encoding
	// from the assertion below.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

	for _, encoding := range []string{"", "identity", "gzip;q=0"} {
		t.Run("accept-encoding="+encoding, func(t *testing.T) {
			ctx := t.Context()

			reqURL := srv.URL + "/stream?streams=" + streamsQuery(t, streamPayload(instanceID, "stream-plain-"+encoding))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
			require.NoError(t, err)
			req.Header.Set("Accept", "text/event-stream")
			if encoding != "" {
				req.Header.Set("Accept-Encoding", encoding)
			}

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Empty(t, resp.Header.Get("Content-Encoding"),
				"a client that did not ask for gzip must get an uncompressed stream")

			initEvent := newSSEReader(resp.Body).waitForEvent(t, streamEventInit, 5*time.Second)
			require.Equal(t, streamEventInit, decodeStreamPayloadData(t, initEvent.data).Type)
		})
	}
}

// TestGzipNegotiationReadsRepeatedHeaderLines guards the Values-not-Get call site:
// Go keeps repeated Accept-Encoding field lines separate, so Header.Get would read
// only "br" here and the client would silently lose compression.
func TestGzipNegotiationReadsRepeatedHeaderLines(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	srv := startStreamServer(t, manager)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		srv.URL+"/stream?streams="+streamsQuery(t, streamPayload(instanceID, "stream-two-headers")), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Add("Accept-Encoding", "br")
	req.Header.Add("Accept-Encoding", "gzip")

	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "gzip", resp.Header.Get("Content-Encoding"),
		"gzip on a second Accept-Encoding line must still be honoured")
}

func TestAcceptsGzip(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"identity", false},
		{"br, zstd", false},
		{"gzip", true},
		{"gzip, deflate, br, zstd", true},
		{" GZIP ", true},
		{"gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"br, gzip;q=0", false},
		{"gzipped", false},
		{"x-gzip", false},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			require.Equal(t, tt.want, acceptsGzip(tt.header))
		})
	}
}

// TestGzipSessionWriterKeepsUnwrapChain guards the silent failure. Three loops walk
// this chain (flushSession, initFlushError, and the ResponseController Serve builds
// from the raw writer). A wrapper without Unwrap still streams perfectly while
// slow-client eviction and init-flush error detection quietly stop working.
func TestGzipSessionWriterKeepsUnwrapChain(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newBufferedSessionWriter(rec, http.NewResponseController(rec), streamWriteTimeout, func() {})
	t.Cleanup(sw.Close)

	gz := newGzipSessionWriter(sw)
	require.Same(t, sw, gz.Unwrap(), "gzip writer must unwrap to the buffered session writer")

	_, err := gz.Write([]byte("event: init\ndata: {}\n\n"))
	require.NoError(t, err)
	require.NoError(t, flushSession(gz), "flushSession must still find the writer's flush path")
}
