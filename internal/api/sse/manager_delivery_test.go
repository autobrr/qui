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
	crossInstanceResponse *qbittorrent.TorrentResponse
	crossInstanceErr      error
	crossInstanceCalls    int
}

func (f *fakeSyncProvider) GetTorrentsWithFilters(_ context.Context, _ int, _, _ int, _, _, _ string, _ qbittorrent.FilterOptions) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.torrentsCalls++
	if f.torrentsErr != nil {
		return nil, f.torrentsErr
	}
	return cloneTorrentResponse(f.torrentsResponse), nil
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

	const burst = 50
	for i := range burst {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: int64(i)})
	}

	// At least one coalesced update must reach the subscriber.
	reader.waitForEvent(t, streamEventUpdate, 5*time.Second)

	// Give any in-flight coalesced builds a moment to settle, then assert the
	// number of torrent builds triggered by the burst is far below the event
	// count (coalescing collapses bursts into a small number of builds).
	require.Eventually(t, func() bool {
		return provider.torrentsCallCount() >= callsAfterInit+1
	}, 5*time.Second, 10*time.Millisecond, "expected at least one update build")

	time.Sleep(200 * time.Millisecond)
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

// TestPublishInitToSubscriberDeliversExactlyOneInit asserts that publishInitToSubscriber
// (the per-subscriber init path) delivers exactly one init event to a freshly
// connected subscriber, not to its group peers.
func TestPublishInitToSubscriberDeliversExactlyOneInit(t *testing.T) {
	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	recorder := newRecordingProvider()
	manager.server.Provider = recorder

	opts := StreamOptions{InstanceID: 1, Page: 0, Limit: 50, Sort: "added_on", Order: "desc"}
	groupKey := streamOptionsKey(opts)

	subA := &subscriptionState{id: "sub-A", options: opts, created: time.Now(), groupKey: groupKey, clientKey: "client-A"}
	subB := &subscriptionState{id: "sub-B", options: opts, created: time.Now(), groupKey: groupKey, clientKey: "client-B"}

	group := &subscriptionGroup{key: groupKey, options: opts, subs: make(map[string]*subscriptionState)}
	group.subs[subA.id] = subA
	group.subs[subB.id] = subB

	manager.mu.Lock()
	manager.subscriptions[subA.id] = subA
	manager.subscriptions[subB.id] = subB
	manager.instanceIndex[opts.InstanceID] = map[string]*subscriptionState{subA.id: subA, subB.id: subB}
	manager.groups[groupKey] = group
	manager.instanceGroups[opts.InstanceID] = map[string]*subscriptionGroup{groupKey: group}
	manager.mu.Unlock()

	// Connecting subscriber B should receive exactly one init, and A must NOT
	// receive an init purely from B joining (init is per-subscriber, not group fan-out).
	manager.publishInitToSubscriber(subB, group)

	require.Eventually(t, func() bool {
		return len(recorder.messagesFor(subB.id)) == 1
	}, 5*time.Second, 10*time.Millisecond, "subscriber B should receive exactly one init")

	bMessages := recorder.messagesFor(subB.id)
	require.Len(t, bMessages, 1)
	bPayload := decodeStreamPayload(t, bMessages[0])
	require.Equal(t, streamEventInit, bPayload.Type)
	require.NotNil(t, bPayload.Data)
	require.Equal(t, "canned-session", bPayload.Data.SessionID)

	require.Empty(t, recorder.messagesFor(subA.id), "subscriber A should not receive an init from B joining")

	// An update triggered by HandleMainData reaches both subscribers in the group.
	manager.publishInstance(opts.InstanceID, streamEventUpdate, &StreamMeta{InstanceID: opts.InstanceID, Timestamp: time.Now()})

	require.Eventually(t, func() bool {
		return len(recorder.messagesFor(subA.id)) >= 1 && len(recorder.messagesFor(subB.id)) >= 2
	}, 5*time.Second, 10*time.Millisecond, "update should fan out to both subscribers in the group")
}

// decodeStreamPayloadData unmarshals the JSON data segment of an SSE event.
func decodeStreamPayloadData(t *testing.T, data string) *StreamPayload {
	t.Helper()
	var payload StreamPayload
	require.NoError(t, json.Unmarshal([]byte(data), &payload), "failed to decode stream payload data")
	return &payload
}
