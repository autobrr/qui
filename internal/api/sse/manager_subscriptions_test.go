// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// changeStream is an open stream that also takes subscription changes.
type changeStream struct {
	*sseReader
	connectionID string
	transport    *http.Transport
	close        func()
}

// openChangeStream serves the manager inside the bubble, opens one stream, and
// reads its connection event. go-sse can still be writing the inits of the
// stream and has not subscribed the session yet.
func openChangeStream(t *testing.T, manager *StreamManager, payload []map[string]any) *changeStream {
	t.Helper()

	listener := &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stream", manager.Serve)
	mux.HandleFunc("POST /stream/subscriptions", manager.ChangeSubscriptions)
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	transport := &http.Transport{DialContext: listener.dial}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		_ = srv.Close()
	})

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://qui.test/stream?streams="+streamsQuery(t, payload), nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	closeStream := func() {
		cancel()
		_ = resp.Body.Close()
	}
	t.Cleanup(closeStream)

	reader := newSSEReader(resp.Body)
	first := <-reader.events
	require.Equal(t, streamEventConnection, first.event, "the connection event comes first")
	id := decodeStreamPayloadData(t, first.data).ConnectionID
	require.NotEmpty(t, id)

	return &changeStream{sseReader: reader, connectionID: id, transport: transport, close: closeStream}
}

// change sends a subscription change and returns the status code.
func (s *changeStream) change(t *testing.T, body map[string]any) int {
	t.Helper()
	status, err := s.post(t.Context(), body)
	require.NoError(t, err)
	return status
}

// post sends a subscription change. It makes no assertions, so a goroutine can call it.
func (s *changeStream) post(ctx context.Context, body map[string]any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://qui.test/stream/subscriptions", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// dataFrames reads the stream for d and returns the event types of the data
// frames for each stream key, in the order they came.
func (s *changeStream) dataFrames(t *testing.T, d time.Duration) map[string][]string {
	t.Helper()
	frames := make(map[string][]string)
	deadline := time.After(d)
	for {
		select {
		case ev := <-s.events:
			if ev.event != streamEventInit && !isTickEvent(ev.event) {
				continue
			}
			key := decodeStreamPayloadData(t, ev.data).Meta.StreamKey
			frames[key] = append(frames[key], ev.event)
		case err := <-s.errc:
			t.Fatalf("stream closed: %v", err)
		case <-deadline:
			return frames
		}
	}
}

func detailsStream(instanceID int, key, hash string) map[string]any {
	payload := streamPayload(instanceID, key)[0]
	payload["limit"] = 1
	payload["filters"] = map[string]any{"hashes": []string{hash}}
	return payload
}

func TestSubscriptionChangeOnOpenStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		instanceID := 1

		stream := openChangeStream(t, manager, streamPayload(instanceID, "main"))
		stream.waitForEvent(t, streamEventInit, time.Second)

		// A tick that races the change must not reach the new subscription first.
		require.Equal(t, http.StatusNoContent, stream.change(t, map[string]any{
			"connectionId": stream.connectionID,
			"add":          []map[string]any{detailsStream(instanceID, "details-a", "aaa")},
		}))
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})
		frames := stream.dataFrames(t, time.Second)
		require.Equal(t, streamEventInit, frames["details-a"][0], "the init comes first")
		require.NotContains(t, frames["main"], streamEventInit)

		// A switch from one torrent to another is one change.
		require.Equal(t, http.StatusNoContent, stream.change(t, map[string]any{
			"connectionId": stream.connectionID,
			"remove":       []string{"details-a"},
			"add":          []map[string]any{detailsStream(instanceID, "details-b", "bbb")},
		}))
		frames = stream.dataFrames(t, time.Second)
		require.Equal(t, map[string][]string{"details-b": {streamEventInit}}, frames)

		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 2})
		frames = stream.dataFrames(t, time.Second)
		require.NotEmpty(t, frames["main"])
		require.NotEmpty(t, frames["details-b"])
		require.NotContains(t, frames, "details-a")
		require.NotContains(t, frames["main"], streamEventInit)
		require.NotContains(t, frames["details-b"], streamEventInit)
	})
}

func TestSubscriptionChangeCleanupOnDisconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := newIdleTestManager(t, &fakeSyncProvider{torrentsResponse: cannedResponse()})

		stream := openChangeStream(t, manager, streamPayload(1, "main"))
		require.Equal(t, http.StatusNoContent, stream.change(t, map[string]any{
			"connectionId": stream.connectionID,
			"add":          []map[string]any{detailsStream(2, "details", "aaa")},
		}))
		stream.waitForEvent(t, streamEventInit, time.Second)
		require.Equal(t, 2, manager.Stats().ActiveSubscriptions)

		stream.close()
		synctest.Wait()

		stats := manager.Stats()
		require.Zero(t, stats.ActiveSubscriptions)
		require.Zero(t, stats.ActiveSyncLoops, "the loop of the instance that joined later stops too")
		require.Equal(t, http.StatusNotFound, stream.change(t, map[string]any{
			"connectionId": stream.connectionID,
			"remove":       []string{"main"},
		}))
	})
}

func TestSubscriptionChangeUnknownConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := newIdleTestManager(t, &fakeSyncProvider{torrentsResponse: cannedResponse()})

		stream := openChangeStream(t, manager, streamPayload(1, "main"))
		require.Equal(t, http.StatusNotFound, stream.change(t, map[string]any{
			"connectionId": "qui-conn-404",
			"add":          []map[string]any{detailsStream(1, "details", "aaa")},
		}))
		require.Equal(t, 1, manager.Stats().ActiveSubscriptions)
	})
}

func TestRemovedSubscriptionGetsNoFrameFromBuildInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		instanceID := 1

		stream := openChangeStream(t, manager, streamPayload(instanceID, "main"))
		require.Equal(t, http.StatusNoContent, stream.change(t, map[string]any{
			"connectionId": stream.connectionID,
			"add":          []map[string]any{detailsStream(instanceID, "details", "aaa")},
		}))
		stream.dataFrames(t, time.Second)

		// The tick of the details view is in its build when the view leaves.
		release := provider.gateTorrentBuilds()
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})
		synctest.Wait()
		require.Equal(t, http.StatusNoContent, stream.change(t, map[string]any{
			"connectionId": stream.connectionID,
			"remove":       []string{"details"},
		}))
		release()

		frames := stream.dataFrames(t, time.Second)
		require.NotEmpty(t, frames["main"])
		require.NotContains(t, frames, "details")
	})
}

func TestSubscriptionChangeBeforeSessionSubscribes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
		manager := newIdleTestManager(t, provider)
		instanceID := 1

		// Hold the init build of the main view, so go-sse does not subscribe the
		// session while the change runs. Later builds do not wait.
		builds := provider.signalBuilds()
		gate := make(chan struct{})
		provider.mu.Lock()
		provider.torrentsGate = gate
		provider.mu.Unlock()
		stream := openChangeStream(t, manager, streamPayload(instanceID, "main"))
		receive(t, builds, "the init build of the main view")
		provider.mu.Lock()
		provider.torrentsGate = nil
		provider.mu.Unlock()

		type result struct {
			status int
			err    error
		}
		done := make(chan result, 1)
		go func() {
			status, err := stream.post(t.Context(), map[string]any{
				"connectionId": stream.connectionID,
				"add":          []map[string]any{detailsStream(instanceID, "details", "aaa")},
			})
			done <- result{status, err}
		}()
		synctest.Wait()
		close(gate)

		res := <-done
		require.NoError(t, res.err)
		require.Equal(t, http.StatusNoContent, res.status)
		frames := stream.dataFrames(t, time.Second)
		require.Equal(t, []string{streamEventInit}, frames["details"])
	})
}
