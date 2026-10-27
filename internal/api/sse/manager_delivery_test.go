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
	"github.com/tmaxmax/go-sse"

	"github.com/autobrr/qui/internal/models"
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
	builds                chan struct{}
	torrentsSkipFresh     bool
	torrentsSkipTracker   bool
	torrentsCachedCounts  bool
	torrentsSummary       bool
	crossInstanceResponse *qbittorrent.TorrentResponse
	crossInstanceErr      error
	crossInstanceCalls    int
	crossSkipFresh        bool
	crossSkipTracker      bool
	crossCachedCounts     bool
	connectionStatus      string
	taskCountDeadline     bool
}

func (f *fakeSyncProvider) GetTorrentsWithFilters(ctx context.Context, _ int, _, _ int, _, _, _ string, _ qbittorrent.FilterOptions) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	f.torrentsCalls++
	f.torrentsSkipFresh = qbittorrent.SkipFreshDataRequested(ctx)
	f.torrentsSkipTracker = qbittorrent.SkipTrackerHydrationRequested(ctx)
	f.torrentsCachedCounts = qbittorrent.CachedCountsWhenSkippingTrackerHydrationRequested(ctx)
	f.torrentsSummary = qbittorrent.SummaryRequested(ctx)
	err := f.torrentsErr
	gate := f.torrentsGate
	builds := f.builds
	var resp *qbittorrent.TorrentResponse
	if err == nil {
		resp = cloneTorrentResponse(f.torrentsResponse)
	}
	f.mu.Unlock()

	if builds != nil {
		builds <- struct{}{}
	}

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
	f.crossInstanceCalls++
	f.crossSkipFresh = qbittorrent.SkipFreshDataRequested(ctx)
	f.crossSkipTracker = qbittorrent.SkipTrackerHydrationRequested(ctx)
	f.crossCachedCounts = qbittorrent.CachedCountsWhenSkippingTrackerHydrationRequested(ctx)
	err := f.crossInstanceErr
	builds := f.builds
	var resp *qbittorrent.TorrentResponse
	if err == nil {
		resp = cloneTorrentResponse(f.crossInstanceResponse)
	}
	f.mu.Unlock()

	if builds != nil {
		builds <- struct{}{}
	}
	return resp, err
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

func (f *fakeSyncProvider) GetActiveTaskCount(ctx context.Context, _ int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, f.taskCountDeadline = ctx.Deadline()
	return f.torrentsResponse.ActiveTaskCount
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

func TestMaterializeGroupResponsePassesSummary(t *testing.T) {
	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	_, errPayload := manager.materializeGroupResponse(StreamOptions{InstanceID: 1, Limit: 1}, &StreamMeta{}, "rows")
	require.Nil(t, errPayload)
	provider.mu.Lock()
	require.False(t, provider.torrentsSummary)
	provider.mu.Unlock()

	_, errPayload = manager.materializeGroupResponse(StreamOptions{InstanceID: 1, Limit: 1, Summary: true}, &StreamMeta{}, "summary")
	require.Nil(t, errPayload)
	provider.mu.Lock()
	require.True(t, provider.torrentsSummary)
	provider.mu.Unlock()
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

// signalBuilds makes every later torrents build, single or cross-instance, send
// on the returned channel when it starts, before it parks on a gate.
func (f *fakeSyncProvider) signalBuilds() <-chan struct{} {
	builds := make(chan struct{}, 64)

	f.mu.Lock()
	f.builds = builds
	f.mu.Unlock()

	return builds
}

// receive waits for one signal on ch, or fails the test after 5 s.
func receive(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
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
	builds := provider.signalBuilds()
	release := provider.gateTorrentBuilds()
	result := make(chan *StreamPayload, 1)
	go func() {
		result <- manager.buildGroupInitPayload(group, opts, &StreamMeta{InstanceID: 1})
	}()

	receive(t, builds, "the init build")

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

// subscribeSignal wraps the replayer of the go-sse provider. Joe calls Replay on
// its loop when it adds a session, and it handles a publish only after that, so a
// publish after the signal reaches the session.
type subscribeSignal struct {
	sse.Replayer
	subscribed chan<- struct{}
}

func (s subscribeSignal) Replay(sub sse.Subscription) error {
	err := s.Replayer.Replay(sub)
	s.subscribed <- struct{}{}
	return err
}

// watchSubscriptions sends on the returned channel each time the manager
// subscribes a session. A new session gets its init before go-sse subscribes it,
// so an update published between the two reaches no one. Call it before the
// manager serves or publishes.
func watchSubscriptions(manager *StreamManager) <-chan struct{} {
	subscribed := make(chan struct{}, 8)
	joe := manager.server.Provider.(*sse.Joe)
	joe.Replayer = subscribeSignal{Replayer: joe.Replayer, subscribed: subscribed}
	return subscribed
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

// isTickEvent reports whether an event is a per-tick torrent frame. Steady-state
// ticks are incremental "delta" frames; a full "update" frame is only sent for the
// seed tick and the periodic keyframe. Delivery/coalescing/isolation tests assert a
// tick frame was delivered regardless of which encoding it took.
func isTickEvent(event string) bool {
	return event == streamEventUpdate || event == streamEventDelta
}

// waitForTick blocks until any per-tick frame (update or delta) arrives. A
// connection gets one init only, so an init before the tick fails the test.
func (r *sseReader) waitForTick(t *testing.T, timeout time.Duration) sseEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-r.events:
			if ev.event == streamEventInit {
				t.Fatalf("got a second init frame before the tick frame")
			}
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

// waitForError blocks until a stream-error event with wantMsg arrives. It matches
// on the message, so the unrelated "sync manager unavailable" stream-error of the
// per-instance sync loop (tests wire a nil pool) is ignored.
func (r *sseReader) waitForError(t *testing.T, wantMsg string, timeout time.Duration) *StreamPayload {
	t.Helper()
	deadline := time.After(timeout)
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
		case <-deadline:
			t.Fatalf("timed out waiting for stream-error %q", wantMsg)
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
	instance, err := manager.instanceDB.(*models.InstanceStore).Create(
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
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
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
	// changes but the full aggregate metadata (total, sessionID).
	receive(t, subscribed, "the session to subscribe")
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 99, FullUpdate: true})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.True(t, isTickEvent(updatePayload.Type), "expected a tick frame, got %q", updatePayload.Type)
	require.NotNil(t, updatePayload.Data, "tick frame should carry aggregate data")
	require.Equal(t, canned.Total, updatePayload.Data.Total)
	require.Equal(t, canned.SessionID, updatePayload.Data.SessionID)
	require.Equal(t, instanceID, updatePayload.Meta.InstanceID)
}

func TestServeEndToEndDeliversTrackerHealthUpdate(t *testing.T) {
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-health"))

	initEvent := reader.waitForEvent(t, streamEventInit, 5*time.Second)
	initPayload := decodeStreamPayloadData(t, initEvent.data)
	require.Equal(t, streamEventInit, initPayload.Type)

	receive(t, subscribed, "the session to subscribe")
	manager.HandleTrackerHealthUpdated(instanceID)
	updateEvent := reader.waitForTick(t, 5*time.Second)
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

func TestServeServerStateOnlyTickSkipsRebuild(t *testing.T) {
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-server-state"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	builds := provider.torrentsCallCount()

	receive(t, subscribed, "the session to subscribe")
	manager.HandleMainData(instanceID, &qbt.MainData{
		Rid:         2,
		ServerState: qbt.ServerState{DlInfoSpeed: 4096},
		Changes:     qbt.MainDataChanges{ServerState: true},
	})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.Equal(t, streamEventDelta, updatePayload.Type)
	require.NotNil(t, updatePayload.Data.ServerState)
	require.Equal(t, int64(4096), updatePayload.Data.ServerState.DlInfoSpeed)
	require.Empty(t, updatePayload.Data.Torrents)
	require.Equal(t, canned.Total, updatePayload.Data.Total)
	require.Equal(t, canned.SessionID, updatePayload.Data.SessionID)
	require.Equal(t, builds, provider.torrentsCallCount(), "a server-state tick must not rebuild the view")
}

func TestServerStateTickKeepsQueuedRebuild(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-queued-rebuild"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	builds := provider.signalBuilds()
	release := provider.gateTorrentBuilds()
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
	receive(t, builds, "the first rebuild")

	// Both ticks queue behind the parked build. The second one must not turn
	// the queued rebuild into a server-state frame.
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true, serverState: &qbt.ServerState{}})
	release()

	receive(t, builds, "the queued rebuild")
}

func TestServerStateTickRetriesFailedRebuild(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-failed-rebuild"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	builds := provider.signalBuilds()
	provider.setTorrentsErr(errors.New("boom"))
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
	receive(t, builds, "the failing rebuild")

	provider.setTorrentsErr(nil)
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true, serverState: &qbt.ServerState{}})
	receive(t, builds, "the rebuild after the failed rebuild")
}

func TestCombinedViewRebuildsOnServerStateTick(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse(), crossInstanceResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	payload := streamPayload(instanceID, "stream-combined-server-state")
	payload[0]["instanceId"] = 0
	payload[0]["instanceIds"] = []int{instanceID}

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, payload)
	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	provider.mu.Lock()
	builds := provider.crossInstanceCalls
	provider.mu.Unlock()

	// A member's subcategory mode changes its category counts in the combined view.
	receive(t, subscribed, "the session to subscribe")
	manager.HandleMainData(instanceID, &qbt.MainData{
		Rid:         2,
		ServerState: qbt.ServerState{UseSubcategories: true},
		Changes:     qbt.MainDataChanges{ServerState: true},
	})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	require.True(t, isTickEvent(decodeStreamPayloadData(t, updateEvent.data).Type))
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Greater(t, provider.crossInstanceCalls, builds, "a combined view must rebuild on a server-state tick")
}

// TestServeCoalescesBurstOfUpdates verifies that a rapid burst of HandleMainData
// calls collapses into far fewer torrent builds than events while still
// delivering at least one update to the connected subscriber.
func TestServeCoalescesBurstOfUpdates(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-coalesce"))

	// Drain the initial snapshot first so it does not count toward update builds.
	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	receive(t, subscribed, "the session to subscribe")
	callsAfterInit := provider.torrentsCallCount()

	// Hold the first update build open so the entire burst provably arrives while a
	// build is in flight. This makes coalescing deterministic instead of relying on
	// the producer loop out-pacing the build worker, which only holds on an idle
	// machine and flakes under CI scheduling pressure.
	builds := provider.signalBuilds()
	release := provider.gateTorrentBuilds()

	const burst = 50
	for i := range burst {
		manager.HandleMainData(instanceID, &qbt.MainData{Rid: int64(i)})
	}

	// Wait for the first coalesced build to start (it is now parked on the gate).
	// Every burst event is already enqueued, so they collapse onto the single
	// pending slot behind it instead of each spawning its own build.
	receive(t, builds, "the first update build")

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
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)

	// First connection on the group.
	readerA, _ := connectStream(t, srv, streamPayload(instanceID, "client-A"))
	initA := readerA.waitForEvent(t, streamEventInit, 5*time.Second)
	require.Equal(t, streamEventInit, decodeStreamPayloadData(t, initA.data).Type)
	receive(t, subscribed, "subscriber A to subscribe")

	// Second connection on an identical view (same group). It must get its own
	// init, and A must not receive a second init purely because B joined.
	readerB, _ := connectStream(t, srv, streamPayload(instanceID, "client-B"))
	initB := readerB.waitForEvent(t, streamEventInit, 5*time.Second)
	bPayload := decodeStreamPayloadData(t, initB.data)
	require.Equal(t, streamEventInit, bPayload.Type)
	require.NotNil(t, bPayload.Data)
	require.Equal(t, "canned-session", bPayload.Data.SessionID)
	receive(t, subscribed, "subscriber B to subscribe")

	// A subsequent update fans out to both connections in the group. Each stream
	// is ordered, so a second init that B's join pushed to A arrives before this
	// tick, and waitForTick fails on it.
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1, FullUpdate: true})
	readerA.waitForTick(t, 5*time.Second)
	readerB.waitForTick(t, 5*time.Second)
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
			store := newTestInstanceStore(t)

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

			subscribed := watchSubscriptions(manager)
			srv := startStreamServer(t, manager)
			reader, _ := connectStream(t, srv, payload)

			// Drain the successful init snapshot, then arm the failure for the update build.
			reader.waitForEvent(t, streamEventInit, 5*time.Second)
			tt.armErr(provider)

			// An external update triggers a rebuild, which now fails and must surface a
			// stream-error event rather than silently dropping.
			receive(t, subscribed, "the session to subscribe")
			manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})
			errPayload := reader.waitForError(t, tt.wantMsg, 5*time.Second)
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
	store := newTestInstanceStore(t)

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

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, payload)

	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	provider.setCrossInstanceErr(errors.New("boom"))

	receive(t, subscribed, "the session to subscribe")
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 1})
	errPayload := reader.waitForError(t, "failed to refresh torrent list", 5*time.Second)
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

func TestServerStateTickRefreshesActiveTaskCount(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-task-count"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	// A torrent-creation task ends while only the server state changes.
	provider.mu.Lock()
	provider.torrentsResponse.ActiveTaskCount = 0
	provider.mu.Unlock()
	receive(t, subscribed, "the session to subscribe")
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 2, Changes: qbt.MainDataChanges{ServerState: true}})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.Equal(t, streamEventDelta, updatePayload.Type)
	require.Equal(t, 0, updatePayload.Data.ActiveTaskCount)

	// A stalled qBittorrent must not hold back the next server-state frames.
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.True(t, provider.taskCountDeadline, "the task count request has no deadline")
}

func TestInitRequestsCounts(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	payload := streamPayload(instanceID, "stream-init-counts")
	payload[0]["summary"] = true

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, payload)
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.True(t, provider.torrentsCachedCounts, "the init frame must carry counts")
}

func TestServerStateTickRefreshesCacheMetadata(t *testing.T) {
	store := newTestInstanceStore(t)

	// A tracker-health rebuild more than 2 s after the last sync reports cached data.
	canned := cannedResponse()
	canned.CacheMetadata = &qbittorrent.CacheMetadata{Source: "cache", Age: 5, IsStale: true}
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-cache-metadata"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	receive(t, subscribed, "the session to subscribe")
	manager.HandleMainData(instanceID, &qbt.MainData{Rid: 2, Changes: qbt.MainDataChanges{ServerState: true}})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.Equal(t, streamEventDelta, updatePayload.Type)
	require.NotNil(t, updatePayload.Data.CacheMetadata)
	require.Equal(t, "fresh", updatePayload.Data.CacheMetadata.Source)
	require.False(t, updatePayload.Data.CacheMetadata.IsStale)
}
