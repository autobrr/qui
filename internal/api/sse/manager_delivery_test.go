// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// fakeSyncProvider is a configurable implementation of the consumer-side
// syncProvider interface. It returns canned responses and records how many times
// each method is invoked so delivery and coalescing behavior can be asserted
// without a live qBittorrent connection.
type fakeSyncProvider struct {
	mu sync.Mutex

	torrentsResponse      *qbittorrent.TorrentResponse
	torrentsErr           error
	torrentsCalls         int
	torrentsGate          chan struct{}
	crossInstanceResponse *qbittorrent.TorrentResponse
	crossInstanceErr      error
	crossInstanceCalls    int
}

func (f *fakeSyncProvider) GetTorrentsWithFilters(_ context.Context, _ int, _, _ int, _, _, _ string, _ qbittorrent.FilterOptions) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	f.torrentsCalls++
	err := f.torrentsErr
	gate := f.torrentsGate
	var resp *qbittorrent.TorrentResponse
	if err == nil {
		resp = cloneTorrentResponse(f.torrentsResponse)
	}
	f.mu.Unlock()

	// When a gate is armed, park here (without holding the lock, so torrentsCallCount
	// stays observable) until the test releases it. This lets a test hold the first
	// build open while it enqueues a burst, exercising coalescing deterministically.
	if gate != nil {
		<-gate
	}

	return resp, err
}

func (f *fakeSyncProvider) GetCrossInstanceTorrentsWithFilters(_ context.Context, _, _ int, _, _, _ string, _ qbittorrent.FilterOptions, _ []int) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.crossInstanceCalls++
	if f.crossInstanceErr != nil {
		return nil, f.crossInstanceErr
	}
	return cloneTorrentResponse(f.crossInstanceResponse), nil
}

func (f *fakeSyncProvider) GetQBittorrentSyncManager(_ context.Context, _ int) (*qbt.SyncManager, error) {
	// These tests drive delivery through HandleMainData rather than the real sync
	// loop, so the loop's attempt to fetch a sync manager always fails fast.
	return nil, errors.New("sync manager unavailable in test")
}

func (f *fakeSyncProvider) torrentsCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.torrentsCalls
}

// gateTorrentBuilds makes every subsequent torrents build park until the returned
// release func is called. A test uses this to hold the first coalesced build open
// while it enqueues a burst, so coalescing is exercised deterministically rather
// than depending on the producer goroutine out-pacing the build worker. release is
// safe to call exactly once.
func (f *fakeSyncProvider) gateTorrentBuilds() (release func()) {
	gate := make(chan struct{})

	f.mu.Lock()
	f.torrentsGate = gate
	f.mu.Unlock()

	return func() {
		f.mu.Lock()
		f.torrentsGate = nil
		f.mu.Unlock()
		close(gate)
	}
}

// cloneTorrentResponse returns a shallow copy so the build path cannot mutate the
// canned response shared across calls (buildGroupPayload sets InstanceMeta).
func cloneTorrentResponse(resp *qbittorrent.TorrentResponse) *qbittorrent.TorrentResponse {
	if resp == nil {
		return nil
	}
	clone := *resp
	return &clone
}

// startStreamServer wires the manager's Serve handler behind an httptest server.
func startStreamServer(t *testing.T, manager *StreamManager) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(manager.Serve))
	t.Cleanup(srv.Close)
	return srv
}

func streamsQuery(t *testing.T, payload []map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return url.QueryEscape(string(raw))
}

// sseEvent is a parsed Server-Sent Event.
type sseEvent struct {
	event string
	data  string
}

// sseReader consumes an SSE response body line-by-line, emitting fully formed
// events onto a channel. go-sse formats events as one or more "event: <type>"
// and "data: <json>" lines terminated by a blank line.
type sseReader struct {
	events chan sseEvent
	errc   chan error
}

func newSSEReader(body io.Reader) *sseReader {
	r := &sseReader{
		events: make(chan sseEvent, 64),
		errc:   make(chan error, 1),
	}

	go func() {
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var (
			eventType string
			dataParts []string
		)

		flush := func() {
			if eventType == "" && len(dataParts) == 0 {
				return
			}
			r.events <- sseEvent{event: eventType, data: strings.Join(dataParts, "\n")}
			eventType = ""
			dataParts = nil
		}

		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				flush()
			case strings.HasPrefix(line, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataParts = append(dataParts, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			default:
				// Ignore id:, retry:, comments, etc.
			}
		}

		if err := scanner.Err(); err != nil {
			r.errc <- err
			return
		}
		r.errc <- io.EOF
	}()

	return r
}

// waitForEvent blocks until an event of the requested type arrives or the
// deadline elapses. Heartbeat and other interleaved events are skipped.
func (r *sseReader) waitForEvent(t *testing.T, eventType string, timeout time.Duration) sseEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-r.events:
			if ev.event == eventType {
				return ev
			}
		case err := <-r.errc:
			t.Fatalf("stream closed before receiving %q event: %v", eventType, err)
		case <-deadline:
			t.Fatalf("timed out waiting for %q event", eventType)
		}
	}
}

// connectStream opens an SSE connection for the given stream payload and returns
// a reader plus a cancel func that closes the client (ending Serve).
func connectStream(t *testing.T, srv *httptest.Server, payload []map[string]any) (*sseReader, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	reqURL := srv.URL + "/stream?streams=" + streamsQuery(t, payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("failed to connect to stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("unexpected status %d connecting to stream: %s", resp.StatusCode, string(body))
	}

	reader := newSSEReader(resp.Body)
	cleanup := func() {
		cancel()
		resp.Body.Close()
	}
	t.Cleanup(cleanup)
	return reader, cleanup
}

func cannedResponse() *qbittorrent.TorrentResponse {
	return &qbittorrent.TorrentResponse{
		Torrents:        []qbittorrent.TorrentView{},
		Total:           7,
		ActiveTaskCount: 3,
		SessionID:       "canned-session",
		HasMore:         true,
	}
}

func streamPayload(instanceID int, key string) []map[string]any {
	return []map[string]any{
		{
			"key":        key,
			"instanceId": instanceID,
			"page":       0,
			"limit":      50,
			"sort":       "added_on",
			"order":      "desc",
			"search":     "",
			"filters":    nil,
		},
	}
}

// seedActiveInstance creates an instance in the store and returns its ID.
func seedActiveInstance(t *testing.T, manager *StreamManager) int {
	t.Helper()
	instance, err := manager.instanceDB.Create(
		context.Background(),
		"Test Instance",
		"http://localhost:8080",
		"user",
		"password",
		nil, nil, false, nil,
	)
	require.NoError(t, err, "failed to seed instance")
	return instance.ID
}

// TestServeEndToEndDeliversInitAndUpdate covers the happy path: an init snapshot
// on connect, followed by an update event when HandleMainData fires.
func TestServeEndToEndDeliversInitAndUpdate(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-init"))

	// 1. The freshly connected subscriber receives an init snapshot.
	initEvent := reader.waitForEvent(t, streamEventInit, 5*time.Second)
	initPayload := decodeStreamPayloadData(t, initEvent.data)
	require.Equal(t, streamEventInit, initPayload.Type)
	require.NotNil(t, initPayload.Data, "init event should carry data")
	require.Equal(t, canned.Total, initPayload.Data.Total)
	require.Equal(t, canned.ActiveTaskCount, initPayload.Data.ActiveTaskCount)
	require.Equal(t, canned.SessionID, initPayload.Data.SessionID)
	require.Equal(t, canned.HasMore, initPayload.Data.HasMore)

	// 2. An external main-data update is fanned out as an update event.
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 99, FullUpdate: true})

	updateEvent := reader.waitForEvent(t, streamEventUpdate, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.Equal(t, streamEventUpdate, updatePayload.Type)
	require.NotNil(t, updatePayload.Data, "update event should carry data")
	require.Equal(t, canned.Total, updatePayload.Data.Total)
	require.Equal(t, canned.SessionID, updatePayload.Data.SessionID)
	require.Equal(t, instanceID, updatePayload.Meta.InstanceID)
}

// TestServeCoalescesBurstOfUpdates verifies that a rapid burst of HandleMainData
// calls collapses into far fewer torrent builds than events while still
// delivering at least one update to the connected subscriber.
func TestServeCoalescesBurstOfUpdates(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-coalesce"))

	// Drain the initial snapshot first so it does not count toward update builds.
	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	callsAfterInit := provider.torrentsCallCount()

	// Hold the first update build open so the entire burst provably arrives while a
	// build is in flight. This makes coalescing deterministic instead of relying on
	// the producer loop out-pacing the build worker, which only holds on an idle
	// machine and flakes under CI scheduling pressure.
	release := provider.gateTorrentBuilds()

	const burst = 50
	for i := range burst {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: int64(i)})
	}

	// Wait for the first coalesced build to start (it is now parked on the gate).
	// Every burst event is already enqueued, so they collapse onto the single
	// pending slot behind it instead of each spawning its own build.
	require.Eventually(t, func() bool {
		return provider.torrentsCallCount() >= callsAfterInit+1
	}, 5*time.Second, 10*time.Millisecond, "expected the first update build to start")

	// Release the gate: the in-flight build finishes and at most one further build
	// runs for the coalesced pending update.
	release()

	// At least one coalesced update must reach the subscriber.
	reader.waitForEvent(t, streamEventUpdate, 5*time.Second)

	updateBuilds := provider.torrentsCallCount() - callsAfterInit
	require.Positive(t, updateBuilds, "burst should trigger at least one build")
	require.Less(t, updateBuilds, burst/2,
		"coalescing should collapse %d events into far fewer builds, got %d", burst, updateBuilds)
}

// TestParseStreamRequestsRejectsTooManyEntries verifies the maxStreamRequests cap
// both at the parser level and through the Serve HTTP path (HTTP 400).
func TestParseStreamRequestsRejectsTooManyEntries(t *testing.T) {
	// Build 65 entries (one over maxStreamRequests of 64).
	payload := make([]map[string]any, maxStreamRequests+1)
	for i := range payload {
		payload[i] = map[string]any{
			"key":        fmt.Sprintf("stream-%d", i),
			"instanceId": 1,
			"page":       0,
			"limit":      50,
			"sort":       "added_on",
			"order":      "desc",
		}
	}

	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	// Parser-level: surfaces errTooManyStreamRequests.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	_, err = parseStreamRequests(req)
	require.ErrorIs(t, err, errTooManyStreamRequests)

	// Serve-level: responds 400 Bad Request.
	manager := NewStreamManager(nil, &fakeSyncProvider{}, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	recorder := httptest.NewRecorder()
	serveReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	manager.Serve(recorder, serveReq)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	// A valid small batch (exactly maxStreamRequests entries) parses successfully.
	smallPayload := make([]map[string]any, maxStreamRequests)
	for i := range smallPayload {
		smallPayload[i] = map[string]any{
			"key":        fmt.Sprintf("ok-%d", i),
			"instanceId": i + 1,
			"page":       0,
			"limit":      50,
		}
	}
	smallRaw, err := json.Marshal(smallPayload)
	require.NoError(t, err)

	smallReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(smallRaw)), nil)
	requests, err := parseStreamRequests(smallReq)
	require.NoError(t, err)
	require.Len(t, requests, maxStreamRequests)
}

// TestServeDeliversExactlyOneInitPerConnection asserts that each fresh connection
// receives exactly one init snapshot, written directly to its session before
// go-sse subscribes it. A second connection joining the same group must not push a
// spurious init to the first (init is per-connection, not a group fan-out), while
// a subsequent update still fans out to both.
func TestServeDeliversExactlyOneInitPerConnection(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)

	// First connection on the group.
	readerA, _ := connectStream(t, srv, streamPayload(instanceID, "client-A"))
	initA := readerA.waitForEvent(t, streamEventInit, 5*time.Second)
	require.Equal(t, streamEventInit, decodeStreamPayloadData(t, initA.data).Type)

	// Second connection on an identical view (same group). It must get its own
	// init, and A must not receive a second init purely because B joined.
	readerB, _ := connectStream(t, srv, streamPayload(instanceID, "client-B"))
	initB := readerB.waitForEvent(t, streamEventInit, 5*time.Second)
	bPayload := decodeStreamPayloadData(t, initB.data)
	require.Equal(t, streamEventInit, bPayload.Type)
	require.NotNil(t, bPayload.Data)
	require.Equal(t, "canned-session", bPayload.Data.SessionID)

	// A is already live; B joining must not deliver another init to A. The only
	// event A may legitimately see in this window is a heartbeat, never a second init.
	select {
	case ev := <-readerA.events:
		require.NotEqualf(t, streamEventInit, ev.event, "subscriber A must not receive a second init when B joins (got %q)", ev.event)
	case <-time.After(500 * time.Millisecond):
		// No further init for A, as expected.
	}

	// A subsequent update fans out to both connections in the group.
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1, FullUpdate: true})
	readerA.waitForEvent(t, streamEventUpdate, 5*time.Second)
	readerB.waitForEvent(t, streamEventUpdate, 5*time.Second)
}

// setTorrentsErr arms the single-instance build to fail with err on the next call.
func (f *fakeSyncProvider) setTorrentsErr(err error) {
	f.mu.Lock()
	f.torrentsErr = err
	f.mu.Unlock()
}

// setCrossInstanceErr arms the cross-instance build to fail with err on the next call.
func (f *fakeSyncProvider) setCrossInstanceErr(err error) {
	f.mu.Lock()
	f.crossInstanceErr = err
	f.mu.Unlock()
}

// TestServeDeliversStreamErrorOnBuildFailure covers buildGroupPayload's error path:
// when the torrent build fails after a subscriber connects, the failure is surfaced
// as a stream-error event carrying a recovery message and a positive retry hint
// (the countdown the frontend depends on), for both single- and cross-instance views.
func TestServeDeliversStreamErrorOnBuildFailure(t *testing.T) {
	tests := []struct {
		name    string
		multi   bool
		armErr  func(*fakeSyncProvider)
		wantMsg string
	}{
		{
			name:    "single instance generic failure",
			armErr:  func(f *fakeSyncProvider) { f.setTorrentsErr(errors.New("boom")) },
			wantMsg: "failed to refresh torrent list",
		},
		{
			name:    "single instance deadline exceeded",
			armErr:  func(f *fakeSyncProvider) { f.setTorrentsErr(context.DeadlineExceeded) },
			wantMsg: "torrent list refresh timed out",
		},
		{
			name:    "cross instance generic failure",
			multi:   true,
			armErr:  func(f *fakeSyncProvider) { f.setCrossInstanceErr(errors.New("boom")) },
			wantMsg: "failed to refresh torrent list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := newTestInstanceStore(t)
			defer cleanup()

			provider := &fakeSyncProvider{
				torrentsResponse:      cannedResponse(),
				crossInstanceResponse: cannedResponse(),
			}
			manager := NewStreamManager(nil, provider, store)
			t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

			instanceID := seedActiveInstance(t, manager)

			payload := streamPayload(instanceID, "stream-err")
			if tt.multi {
				payload[0]["instanceId"] = 0
				payload[0]["instanceIds"] = []int{instanceID}
			}

			srv := startStreamServer(t, manager)
			reader, _ := connectStream(t, srv, payload)

			// Drain the successful init snapshot, then arm the failure for the update build.
			reader.waitForEvent(t, streamEventInit, 5*time.Second)
			tt.armErr(provider)

			// An external update triggers a rebuild, which now fails and must surface a
			// stream-error event rather than silently dropping.
			manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})

			errEvent := reader.waitForEvent(t, streamEventError, 5*time.Second)
			errPayload := decodeStreamPayloadData(t, errEvent.data)
			require.Equal(t, streamEventError, errPayload.Type)
			require.Equal(t, tt.wantMsg, errPayload.Err)
			require.NotNil(t, errPayload.Meta, "error event must carry meta for the retry hint")
			require.Positive(t, errPayload.Meta.RetryInSeconds, "error event must advertise a positive retry countdown")
		})
	}
}

// decodeStreamPayloadData unmarshals the JSON data segment of an SSE event.
func decodeStreamPayloadData(t *testing.T, data string) *StreamPayload {
	t.Helper()
	var payload StreamPayload
	require.NoError(t, json.Unmarshal([]byte(data), &payload), "failed to decode stream payload data")
	return &payload
}
