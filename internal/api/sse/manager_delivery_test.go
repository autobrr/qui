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
	torrentsSkipFresh     bool
	torrentsSkipTracker   bool
	torrentsCachedCounts  bool
	crossInstanceResponse *qbittorrent.TorrentResponse
	crossInstanceErr      error
	crossInstanceCalls    int
	crossSkipFresh        bool
	crossSkipTracker      bool
	crossCachedCounts     bool
	connectionStatus      string
}

func (f *fakeSyncProvider) GetTorrentsWithFilters(ctx context.Context, _ int, _, _ int, _, _, _ string, _ qbittorrent.FilterOptions) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	f.torrentsCalls++
	f.torrentsSkipFresh = qbittorrent.SkipFreshDataRequested(ctx)
	f.torrentsSkipTracker = qbittorrent.SkipTrackerHydrationRequested(ctx)
	f.torrentsCachedCounts = qbittorrent.CachedCountsWhenSkippingTrackerHydrationRequested(ctx)
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

func (f *fakeSyncProvider) GetCrossInstanceTorrentsWithFilters(ctx context.Context, _, _ int, _, _, _ string, _ qbittorrent.FilterOptions, _ []int) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.crossInstanceCalls++
	f.crossSkipFresh = qbittorrent.SkipFreshDataRequested(ctx)
	f.crossSkipTracker = qbittorrent.SkipTrackerHydrationRequested(ctx)
	f.crossCachedCounts = qbittorrent.CachedCountsWhenSkippingTrackerHydrationRequested(ctx)
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

func (f *fakeSyncProvider) ReadCachedConnectionStatus(_ context.Context, _ int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connectionStatus
}

func (f *fakeSyncProvider) torrentsCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.torrentsCalls
}

func TestMaterializeGroupResponseSkipsFreshDataAndTrackerHydration(t *testing.T) {
	canned := cannedResponse()
	canned.TrackerHealthSupported = true
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	response, errPayload := manager.materializeGroupResponse(
		StreamOptions{InstanceID: 1, Limit: 100},
		&StreamMeta{Timestamp: time.Now()},
		"skip-hydration",
	)

	require.NotNil(t, response)
	require.Nil(t, errPayload)
	require.True(t, response.TrackerHealthSupported)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.True(t, provider.torrentsSkipFresh)
	require.True(t, provider.torrentsSkipTracker)
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

// TestInitBuildUsesBaselinePublishedWhileMaterializing pins the initialization
// race: if a tick advances the group while an init candidate is being built, the
// joiner must receive the tick's exact retained snapshot and version rather than
// the stale candidate it materialized independently.
func TestInitBuildUsesBaselinePublishedWhileMaterializing(t *testing.T) {
	before := singleResp(qbittorrent.TorrentView{Torrent: &qbt.Torrent{
		Hash:     "a",
		Progress: 0.99,
		State:    qbt.TorrentStateDownloading,
	}})
	provider := &fakeSyncProvider{torrentsResponse: before}
	manager := NewStreamManager(nil, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	opts := StreamOptions{InstanceID: 1, Limit: 300}
	group := &subscriptionGroup{
		key:     streamOptionsKey(opts),
		options: opts,
		version: StreamVersion{Major: 11},
	}
	release := provider.gateTorrentBuilds()
	result := make(chan *StreamPayload, 1)
	go func() {
		result <- manager.buildGroupInitPayload(group, opts, &StreamMeta{InstanceID: 1})
	}()

	require.Eventually(t, func() bool {
		return provider.torrentsCallCount() == 1
	}, time.Second, time.Millisecond)

	completed := singleResp(qbittorrent.TorrentView{Torrent: &qbt.Torrent{
		Hash:     "a",
		Progress: 1,
		State:    qbt.TorrentStateUploading,
	}})
	published := group.buildUpdatePayload(opts, completed, &StreamMeta{InstanceID: 1})
	require.Equal(t, &StreamVersion{Major: 11, Minor: 1}, published.Version)
	release()

	init := <-result
	require.NotNil(t, init)
	require.Equal(t, streamEventInit, init.Type)
	require.Equal(t, published.Version, init.Version)
	require.InDelta(t, 1, init.Data.Torrents[0].Progress, 0)
	require.Equal(t, qbt.TorrentStateUploading, init.Data.Torrents[0].State)
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

// drain discards any events already buffered on the reader without blocking, so a
// subsequent waitForEvent observes only events produced after the drain.
func (r *sseReader) drain() {
	for {
		select {
		case <-r.events:
		default:
			return
		}
	}
}

// isTickEvent reports whether an event is a per-tick torrent frame. Steady-state
// ticks are incremental "delta" frames; a full "update" frame is only sent for the
// seed tick and the periodic keyframe. Delivery/coalescing/isolation tests assert a
// tick frame was delivered regardless of which encoding it took.
func isTickEvent(event string) bool {
	return event == streamEventUpdate || event == streamEventDelta
}

// waitForTick blocks until any per-tick frame (update or delta) arrives.
func (r *sseReader) waitForTick(t *testing.T, timeout time.Duration) sseEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-r.events:
			if isTickEvent(ev.event) {
				return ev
			}
		case err := <-r.errc:
			t.Fatalf("stream closed before receiving a tick frame: %v", err)
		case <-deadline:
			t.Fatalf("timed out waiting for a tick (update/delta) frame")
		}
	}
}

// triggerRetryInterval paces re-invocation of an update/error trigger while a
// freshly connected session finishes subscribing.
const triggerRetryInterval = 100 * time.Millisecond

// waitForTickTriggered is waitForEventTriggered for per-tick frames: it re-invokes
// trigger until any update/delta frame arrives, tolerating the post-init subscribe
// window.
func (r *sseReader) waitForTickTriggered(t *testing.T, timeout time.Duration, trigger func()) sseEvent {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(triggerRetryInterval)
	defer tick.Stop()
	trigger()
	for {
		select {
		case ev := <-r.events:
			if isTickEvent(ev.event) {
				return ev
			}
		case err := <-r.errc:
			t.Fatalf("stream closed before receiving a tick frame: %v", err)
		case <-tick.C:
			trigger()
		case <-deadline:
			t.Fatalf("timed out waiting for a tick (update/delta) frame")
		}
	}
}

// waitForEventTriggered invokes trigger, then re-invokes it on a fixed interval
// until an event of eventType arrives. A new session receives its init snapshot
// (written synchronously in onSession) before go-sse subscribes it to its topics,
// so an update published in that brief window reaches no subscriber. Re-triggering
// tolerates that window without coupling the test to go-sse's subscribe timing.
func (r *sseReader) waitForEventTriggered(t *testing.T, eventType string, timeout time.Duration, trigger func()) sseEvent {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(triggerRetryInterval)
	defer tick.Stop()
	trigger()
	for {
		select {
		case ev := <-r.events:
			if ev.event == eventType {
				return ev
			}
		case err := <-r.errc:
			t.Fatalf("stream closed before receiving %q event: %v", eventType, err)
		case <-tick.C:
			trigger()
		case <-deadline:
			t.Fatalf("timed out waiting for %q event", eventType)
		}
	}
}

// waitForErrorTriggered is waitForEventTriggered specialised for stream-error
// events, matching on the error message so the per-instance sync loop's unrelated
// "sync manager unavailable" stream-error (tests wire a nil pool) is ignored.
func (r *sseReader) waitForErrorTriggered(t *testing.T, wantMsg string, timeout time.Duration, trigger func()) *StreamPayload {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(triggerRetryInterval)
	defer tick.Stop()
	trigger()
	for {
		select {
		case ev := <-r.events:
			if ev.event != streamEventError {
				continue
			}
			if payload := decodeStreamPayloadData(t, ev.data); payload.Err == wantMsg {
				return payload
			}
		case err := <-r.errc:
			t.Fatalf("stream closed before receiving stream-error %q: %v", wantMsg, err)
		case <-tick.C:
			trigger()
		case <-deadline:
			t.Fatalf("timed out waiting for stream-error %q", wantMsg)
		}
	}
}

// waitForUpdateOnBoth re-invokes trigger until both readers receive a per-tick
// frame (update or delta), tolerating the post-init subscribe window for either
// session.
func waitForUpdateOnBoth(t *testing.T, a, b *sseReader, timeout time.Duration, trigger func()) {
	t.Helper()
	deadline := time.After(timeout)
	tick := time.NewTicker(triggerRetryInterval)
	defer tick.Stop()
	trigger()
	gotA, gotB := false, false
	for !gotA || !gotB {
		select {
		case ev := <-a.events:
			if isTickEvent(ev.event) {
				gotA = true
			}
		case ev := <-b.events:
			if isTickEvent(ev.event) {
				gotB = true
			}
		case err := <-a.errc:
			t.Fatalf("subscriber A stream closed before update: %v", err)
		case err := <-b.errc:
			t.Fatalf("subscriber B stream closed before update: %v", err)
		case <-tick.C:
			trigger()
		case <-deadline:
			t.Fatalf("timed out waiting for update on both subscribers (a=%v b=%v)", gotA, gotB)
		}
	}
}

// connectStream opens an SSE connection for the given stream payload and returns
// a reader plus a cancel func that closes the client (ending Serve).
func connectStream(t *testing.T, srv *httptest.Server, payload []map[string]any) (*sseReader, context.CancelFunc) {
	t.Helper()

	resp, cleanup := dialStream(t, srv, payload, "")
	return newSSEReader(resp.Body), cleanup
}

// dialStream opens an SSE connection and returns the live response plus a cancel
// func that closes the client (ending Serve). A non-empty acceptEncoding is sent
// verbatim, which also stops the transport from adding its own and decoding the
// body, so the caller sees the wire bytes.
func dialStream(t *testing.T, srv *httptest.Server, payload []map[string]any, acceptEncoding string) (*http.Response, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	reqURL := srv.URL + "/stream?streams=" + streamsQuery(t, payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}

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

	cleanup := func() {
		cancel()
		resp.Body.Close()
	}
	t.Cleanup(cleanup)
	return resp, cleanup
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

	// 2. An external main-data update is fanned out as a tick frame. Since init seeded
	// the baseline with the same canned page, this tick is a delta carrying no row
	// changes but the full aggregate metadata (total, sessionID). The trigger is
	// retried because the session subscribes shortly after its init is flushed, so the
	// first publish can land before the subscription exists.
	updateEvent := reader.waitForTickTriggered(t, 5*time.Second, func() {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 99, FullUpdate: true})
	})
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.True(t, isTickEvent(updatePayload.Type), "expected a tick frame, got %q", updatePayload.Type)
	require.NotNil(t, updatePayload.Data, "tick frame should carry aggregate data")
	require.Equal(t, canned.Total, updatePayload.Data.Total)
	require.Equal(t, canned.SessionID, updatePayload.Data.SessionID)
	require.Equal(t, instanceID, updatePayload.Meta.InstanceID)
}

func TestServeEndToEndDeliversTrackerHealthUpdate(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-health"))

	initEvent := reader.waitForEvent(t, streamEventInit, 5*time.Second)
	initPayload := decodeStreamPayloadData(t, initEvent.data)
	require.Equal(t, streamEventInit, initPayload.Type)

	updateEvent := reader.waitForTickTriggered(t, 5*time.Second, func() {
		manager.HandleTrackerHealthUpdated(instanceID)
	})
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.True(t, isTickEvent(updatePayload.Type), "expected a tick frame, got %q", updatePayload.Type)
	require.NotNil(t, updatePayload.Data, "tracker-health tick should rematerialize dashboard data")
	require.Equal(t, canned.Total, updatePayload.Data.Total)
	require.Equal(t, instanceID, updatePayload.Meta.InstanceID)
	require.True(t, updatePayload.Meta.IncludeCounts)

	provider.mu.Lock()
	require.True(t, provider.torrentsSkipFresh, "tracker-health tick should use cached qbit data")
	require.True(t, provider.torrentsSkipTracker, "tracker-health tick should not hydrate trackers inline")
	require.True(t, provider.torrentsCachedCounts, "tracker-health tick should still request cached counts")
	provider.mu.Unlock()
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

	// A fresh session is subscribed to its go-sse topics only *after* its init
	// snapshot is written synchronously in onSession, so an update published in that
	// window reaches no subscriber: it lands only in the replay buffer, which a fresh
	// connection (empty Last-Event-ID) never replays. The sibling delivery tests ride
	// out that window by re-firing their trigger; this test fires the coalescing burst
	// exactly once, so it must establish the subscription up front. Re-fire one warm-up
	// update until it lands; once it does the session stays subscribed for the life of
	// the connection, so the single coalesced burst update below is delivered without
	// coupling the coalescing measurement to go-sse's subscribe timing. The gate makes
	// coalescing deterministic but cannot close this subscribe window.
	reader.waitForTickTriggered(t, 5*time.Second, func() {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1000})
	})

	// Let the warm-up builds settle, then drop their buffered events, before
	// snapshotting the build count. This keeps warm-up activity out of both the
	// coalescing measurement and the delivery assertion below.
	var prevCalls int
	require.Eventually(t, func() bool {
		cur := provider.torrentsCallCount()
		settled := cur == prevCalls
		prevCalls = cur
		return settled
	}, 2*time.Second, 50*time.Millisecond, "warm-up builds did not settle")
	reader.drain()
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

	// At least one coalesced tick frame must reach the subscriber.
	reader.waitForTick(t, 5*time.Second)

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

	// A subsequent update fans out to both connections in the group. Retry the
	// trigger until both subscribers (each subscribes just after its init flush)
	// have received it.
	waitForUpdateOnBoth(t, readerA, readerB, 5*time.Second, func() {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1, FullUpdate: true})
	})
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
			name: "single instance health check in progress",
			armErr: func(f *fakeSyncProvider) {
				f.setTorrentsErr(&qbittorrent.InstanceHealthBlockerError{
					Kind:       qbittorrent.InstanceHealthBlockerHealthCheckInProgress,
					InstanceID: 1,
				})
			},
			wantMsg: "qBittorrent instance 1 is already running a health check; retry shortly",
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
			// stream-error event rather than silently dropping. Retry the trigger (the
			// session may subscribe just after its init is flushed) and match by message
			// (the sync loop emits its own unrelated stream-error).
			errPayload := reader.waitForErrorTriggered(t, tt.wantMsg, 5*time.Second, func() {
				manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})
			})
			require.Equal(t, streamEventError, errPayload.Type)
			require.NotNil(t, errPayload.Meta, "error event must carry meta for the retry hint")
			require.Positive(t, errPayload.Meta.RetryInSeconds, "error event must advertise a positive retry countdown")
		})
	}
}

// TestServeStampsLastSuccessfulSyncOnSingleMemberBuildFailure covers the
// materializeGroupResponse error path's staleness stamp (issue #2052): when a
// one-member aggregate build fails, the stream-error frame can carry
// LastSuccessfulSync for that concrete instance. The HandleSyncError stamp path is
// covered separately in manager_test.go; this exercises the second, group-refresh
// callsite.
func TestServeStampsLastSuccessfulSyncOnSingleMemberBuildFailure(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	provider := &fakeSyncProvider{
		torrentsResponse:      cannedResponse(),
		crossInstanceResponse: cannedResponse(),
	}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	// Source the last good sync from a fixed point in the past, keyed on the only
	// instance in the group. Returning the zero time for any other id proves the
	// stamp is keyed on that concrete instance, not stamped blindly.
	lastGood := time.Now().Add(-90 * time.Second)
	manager.lastSuccessfulSyncFn = func(_ context.Context, id int) time.Time {
		if id == instanceID {
			return lastGood
		}
		return time.Time{}
	}

	// One-member aggregate group so retryInstanceID falls back to InstanceIDs[0].
	payload := streamPayload(instanceID, "stream-err")
	payload[0]["instanceId"] = 0
	payload[0]["instanceIds"] = []int{instanceID}

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, payload)

	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	provider.setCrossInstanceErr(errors.New("boom"))

	errPayload := reader.waitForErrorTriggered(t, "failed to refresh torrent list", 5*time.Second, func() {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})
	})
	require.Equal(t, streamEventError, errPayload.Type)
	require.NotNil(t, errPayload.Meta, "error event must carry meta")
	require.NotNil(t, errPayload.Meta.LastSuccessfulSync, "group build-failure meta must carry the last successful sync time")
	require.WithinDuration(t, lastGood, *errPayload.Meta.LastSuccessfulSync, time.Second)
}

// decodeStreamPayloadData unmarshals the JSON data segment of an SSE event.
func decodeStreamPayloadData(t *testing.T, data string) *StreamPayload {
	t.Helper()
	var payload StreamPayload
	require.NoError(t, json.Unmarshal([]byte(data), &payload), "failed to decode stream payload data")
	return &payload
}
