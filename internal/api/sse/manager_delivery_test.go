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
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

type fakeSyncProvider struct {
	mu sync.Mutex

	torrentsResponse      *qbittorrent.TorrentResponse
	torrentsErr           error
	torrentsCalls         int
	torrentsGate          chan struct{}
	builds                chan struct{}
	torrentsView          qbittorrent.TorrentListView
	groupedResponse       *qbittorrent.TorrentResponse
	crossInstanceResponse *qbittorrent.TorrentResponse
	crossInstanceErr      error
	crossInstanceCalls    int
	crossView             qbittorrent.TorrentListView
	connectionStatus      string
	taskCountDeadline     bool
	webAPIVersions        map[int]string
}

func (f *fakeSyncProvider) ListTorrents(_ context.Context, view qbittorrent.TorrentListView) (*qbittorrent.TorrentResponse, error) {
	f.mu.Lock()
	var (
		resp *qbittorrent.TorrentResponse
		err  error
		gate chan struct{}
	)
	if view.Unified {
		f.crossInstanceCalls++
		f.crossView = view
		err = f.crossInstanceErr
		if err == nil {
			resp = cloneTorrentResponse(f.crossInstanceResponse)
		}
	} else {
		f.torrentsCalls++
		f.torrentsView = view
		err = f.torrentsErr
		gate = f.torrentsGate
		if err == nil {
			resp = cloneTorrentResponse(f.torrentsResponse)
			if view.CopyGroups && f.groupedResponse != nil {
				resp = cloneTorrentResponse(f.groupedResponse)
			}
		}
	}
	builds := f.builds
	f.mu.Unlock()

	if builds != nil {
		builds <- struct{}{}
	}

	if gate != nil {
		<-gate
	}

	return resp, err
}

func (f *fakeSyncProvider) GetSyncOwner(_ context.Context, _ int) (*syncowner.Owner, error) {
	return nil, errors.New("sync manager unavailable in test")
}

func (f *fakeSyncProvider) ReadCachedConnectionStatus(_ context.Context, _ int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connectionStatus
}

func (f *fakeSyncProvider) WebAPIVersionsChanged(_ context.Context, versions map[int]string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, version := range versions {
		if current, ok := f.webAPIVersions[id]; ok && current != version {
			return true
		}
	}
	return false
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

func TestMaterializeGroupResponseReadsTheCacheWithoutCounts(t *testing.T) {
	canned := cannedResponse()
	canned.TrackerHealthSupported = true
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	response, errPayload := manager.materializeGroupResponse(
		StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: 100}},
		&StreamMeta{Timestamp: time.Now()},
		"cache-only",
	)

	require.NotNil(t, response)
	require.Nil(t, errPayload)
	require.True(t, response.TrackerHealthSupported)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.True(t, provider.torrentsView.CacheOnly)
	require.False(t, provider.torrentsView.SidebarCounts)
}

func TestMaterializeGroupResponsePassesSummary(t *testing.T) {
	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	_, errPayload := manager.materializeGroupResponse(StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: 1}}, &StreamMeta{}, "rows")
	require.Nil(t, errPayload)
	provider.mu.Lock()
	require.False(t, provider.torrentsView.NoRows)
	provider.mu.Unlock()

	_, errPayload = manager.materializeGroupResponse(StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: 1, NoRows: true}}, &StreamMeta{}, "summary")
	require.Nil(t, errPayload)
	provider.mu.Lock()
	require.True(t, provider.torrentsView.NoRows)
	provider.mu.Unlock()
}

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

func (f *fakeSyncProvider) signalBuilds() <-chan struct{} {
	builds := make(chan struct{}, 64)

	f.mu.Lock()
	f.builds = builds
	f.mu.Unlock()

	return builds
}

func receive(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestInitBuildUsesBaselinePublishedWhileMaterializing(t *testing.T) {
	before := singleResp(qbittorrent.TorrentView{Torrent: &qbt.Torrent{
		Hash:     "a",
		Progress: 0.99,
		State:    qbt.TorrentStateDownloading,
	}})
	provider := &fakeSyncProvider{torrentsResponse: before}
	manager := NewStreamManager(nil, provider, provider, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: 300}}
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

func cloneTorrentResponse(resp *qbittorrent.TorrentResponse) *qbittorrent.TorrentResponse {
	if resp == nil {
		return nil
	}
	clone := *resp
	return &clone
}

func startStreamServer(t *testing.T, manager *StreamManager) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(manager.Serve))
	t.Cleanup(srv.Close)
	return srv
}

type subscribeSignal struct {
	sse.Replayer
	subscribed chan<- struct{}
}

func (s subscribeSignal) Replay(sub sse.Subscription) error {
	err := s.Replayer.Replay(sub)
	s.subscribed <- struct{}{}
	return err
}

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

type sseEvent struct {
	event string
	data  string
}

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

func isTickEvent(event string) bool {
	return event == streamEventUpdate || event == streamEventDelta
}

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

func connectStream(t *testing.T, srv *httptest.Server, payload []map[string]any) (*sseReader, context.CancelFunc) {
	t.Helper()

	resp, cleanup := dialStream(t, srv, payload, "")
	return newSSEReader(resp.Body), cleanup
}

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

func TestServeEndToEndDeliversInitAndUpdate(t *testing.T) {
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-init"))

	initEvent := reader.waitForEvent(t, streamEventInit, 5*time.Second)
	initPayload := decodeStreamPayloadData(t, initEvent.data)
	require.Equal(t, streamEventInit, initPayload.Type)
	require.NotNil(t, initPayload.Data, "init event should carry data")
	require.Equal(t, canned.Total, initPayload.Data.Total)
	require.Equal(t, canned.ActiveTaskCount, initPayload.Data.ActiveTaskCount)
	require.Equal(t, canned.SessionID, initPayload.Data.SessionID)
	require.Equal(t, canned.HasMore, initPayload.Data.HasMore)

	receive(t, subscribed, "the session to subscribe")
	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{})
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
	manager := NewStreamManager(nil, provider, provider, store)
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
	require.True(t, provider.torrentsView.CacheOnly, "tracker-health tick should use cached qbit data")
	require.True(t, provider.torrentsView.SidebarCounts, "tracker-health tick should still request cached counts")
	provider.mu.Unlock()
}

func TestServeServerStateOnlyTickSkipsRebuild(t *testing.T) {
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-server-state"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	builds := provider.torrentsCallCount()

	receive(t, subscribed, "the session to subscribe")
	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{
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

func TestServerStateTickRebuildsWhenAVersionChanged(t *testing.T) {
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)
	canned.WebAPIVersions = map[int]string{instanceID: ""}

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-version-change"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	receive(t, subscribed, "the session to subscribe")

	recovered := *canned
	recovered.WebAPIVersions = map[int]string{instanceID: "2.15.1"}
	provider.mu.Lock()
	provider.torrentsResponse = &recovered
	provider.webAPIVersions = recovered.WebAPIVersions
	provider.mu.Unlock()

	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{
		ServerState: qbt.ServerState{DlInfoSpeed: 4096},
		Changes:     qbt.MainDataChanges{ServerState: true},
	})
	updatePayload := decodeStreamPayloadData(t, reader.waitForTick(t, 5*time.Second).data)
	require.Equal(t, map[int]string{instanceID: "2.15.1"}, updatePayload.Data.WebAPIVersions)
}

func TestServerStateTickKeepsQueuedRebuild(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-queued-rebuild"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	builds := provider.signalBuilds()
	release := provider.gateTorrentBuilds()
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
	receive(t, builds, "the first rebuild")

	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true})
	manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, IncludeCounts: true, serverState: &qbt.ServerState{}})
	release()

	receive(t, builds, "the queued rebuild")
}

func TestServerStateTickRetriesFailedRebuild(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
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
	manager := NewStreamManager(nil, provider, provider, store)
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

	receive(t, subscribed, "the session to subscribe")
	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{
		ServerState: qbt.ServerState{UseSubcategories: true},
		Changes:     qbt.MainDataChanges{ServerState: true},
	})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	require.True(t, isTickEvent(decodeStreamPayloadData(t, updateEvent.data).Type))
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Greater(t, provider.crossInstanceCalls, builds, "a combined view must rebuild on a server-state tick")
}

func TestServeCoalescesBurstOfUpdates(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-coalesce"))

	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	receive(t, subscribed, "the session to subscribe")
	callsAfterInit := provider.torrentsCallCount()

	builds := provider.signalBuilds()
	release := provider.gateTorrentBuilds()

	const burst = 50
	for i := range burst {
		manager.publishInstance(instanceID, &StreamMeta{InstanceID: instanceID, RID: int64(i), IncludeCounts: true})
	}

	receive(t, builds, "the first update build")

	release()

	reader.waitForTick(t, 5*time.Second)

	updateBuilds := provider.torrentsCallCount() - callsAfterInit
	require.Positive(t, updateBuilds, "burst should trigger at least one build")
	require.Less(t, updateBuilds, burst/2,
		"coalescing should collapse %d events into far fewer builds, got %d", burst, updateBuilds)
}

func TestParseStreamRequestsRejectsTooManyEntries(t *testing.T) {
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

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	_, err = parseStreamRequests(req)
	require.ErrorIs(t, err, errTooManyStreamRequests)

	manager := NewStreamManager(nil, &fakeSyncProvider{}, &fakeSyncProvider{}, nil)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	recorder := httptest.NewRecorder()
	serveReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	manager.Serve(recorder, serveReq)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

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

func TestServeDeliversExactlyOneInitPerConnection(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)

	readerA, _ := connectStream(t, srv, streamPayload(instanceID, "client-A"))
	initA := readerA.waitForEvent(t, streamEventInit, 5*time.Second)
	require.Equal(t, streamEventInit, decodeStreamPayloadData(t, initA.data).Type)
	receive(t, subscribed, "subscriber A to subscribe")

	readerB, _ := connectStream(t, srv, streamPayload(instanceID, "client-B"))
	initB := readerB.waitForEvent(t, streamEventInit, 5*time.Second)
	bPayload := decodeStreamPayloadData(t, initB.data)
	require.Equal(t, streamEventInit, bPayload.Type)
	require.NotNil(t, bPayload.Data)
	require.Equal(t, "canned-session", bPayload.Data.SessionID)
	receive(t, subscribed, "subscriber B to subscribe")

	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{})
	readerA.waitForTick(t, 5*time.Second)
	readerB.waitForTick(t, 5*time.Second)
}

func (f *fakeSyncProvider) setTorrentsErr(err error) {
	f.mu.Lock()
	f.torrentsErr = err
	f.mu.Unlock()
}

func (f *fakeSyncProvider) setCrossInstanceErr(err error) {
	f.mu.Lock()
	f.crossInstanceErr = err
	f.mu.Unlock()
}

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
			manager := NewStreamManager(nil, provider, provider, store)
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

			reader.waitForEvent(t, streamEventInit, 5*time.Second)
			tt.armErr(provider)

			receive(t, subscribed, "the session to subscribe")
			manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{})
			errPayload := reader.waitForError(t, tt.wantMsg, 5*time.Second)
			require.Equal(t, streamEventError, errPayload.Type)
			require.NotNil(t, errPayload.Meta, "error event must carry meta for the retry hint")
			require.Positive(t, errPayload.Meta.RetryInSeconds, "error event must advertise a positive retry countdown")
		})
	}
}

func TestServeStampsLastSuccessfulSyncOnSingleMemberBuildFailure(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{
		torrentsResponse:      cannedResponse(),
		crossInstanceResponse: cannedResponse(),
	}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	lastGood := time.Now().Add(-90 * time.Second)
	manager.lastSuccessfulSyncFn = func(_ context.Context, id int) time.Time {
		if id == instanceID {
			return lastGood
		}
		return time.Time{}
	}

	payload := streamPayload(instanceID, "stream-err")
	payload[0]["instanceId"] = 0
	payload[0]["instanceIds"] = []int{instanceID}

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, payload)

	reader.waitForEvent(t, streamEventInit, 5*time.Second)
	provider.setCrossInstanceErr(errors.New("boom"))

	receive(t, subscribed, "the session to subscribe")
	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{})
	errPayload := reader.waitForError(t, "failed to refresh torrent list", 5*time.Second)
	require.Equal(t, streamEventError, errPayload.Type)
	require.NotNil(t, errPayload.Meta, "error event must carry meta")
	require.NotNil(t, errPayload.Meta.LastSuccessfulSync, "group build-failure meta must carry the last successful sync time")
	require.WithinDuration(t, lastGood, *errPayload.Meta.LastSuccessfulSync, time.Second)
}

func decodeStreamPayloadData(t *testing.T, data string) *StreamPayload {
	t.Helper()
	var payload StreamPayload
	require.NoError(t, json.Unmarshal([]byte(data), &payload), "failed to decode stream payload data")
	return &payload
}

func TestServerStateTickRefreshesActiveTaskCount(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-task-count"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	provider.mu.Lock()
	provider.torrentsResponse.ActiveTaskCount = 0
	provider.mu.Unlock()
	receive(t, subscribed, "the session to subscribe")
	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{Changes: qbt.MainDataChanges{ServerState: true}})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.Equal(t, streamEventDelta, updatePayload.Type)
	require.Equal(t, 0, updatePayload.Data.ActiveTaskCount)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.True(t, provider.taskCountDeadline, "the task count request has no deadline")
}

func TestInitRequestsCounts(t *testing.T) {
	store := newTestInstanceStore(t)

	provider := &fakeSyncProvider{torrentsResponse: cannedResponse()}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	payload := streamPayload(instanceID, "stream-init-counts")
	payload[0]["summary"] = true

	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, payload)
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.True(t, provider.torrentsView.SidebarCounts, "the init frame must carry counts")
}

func TestServerStateTickRefreshesCacheMetadata(t *testing.T) {
	store := newTestInstanceStore(t)

	canned := cannedResponse()
	canned.CacheMetadata = &qbittorrent.CacheMetadata{Source: "cache", Age: 5, IsStale: true}
	provider := &fakeSyncProvider{torrentsResponse: canned}
	manager := NewStreamManager(nil, provider, provider, store)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	instanceID := seedActiveInstance(t, manager)

	subscribed := watchSubscriptions(manager)
	srv := startStreamServer(t, manager)
	reader, _ := connectStream(t, srv, streamPayload(instanceID, "stream-cache-metadata"))
	reader.waitForEvent(t, streamEventInit, 5*time.Second)

	receive(t, subscribed, "the session to subscribe")
	manager.HandleSyncUpdate(instanceID, qbt.SyncUpdate{Changes: qbt.MainDataChanges{ServerState: true}})
	updateEvent := reader.waitForTick(t, 5*time.Second)
	updatePayload := decodeStreamPayloadData(t, updateEvent.data)
	require.Equal(t, streamEventDelta, updatePayload.Type)
	require.NotNil(t, updatePayload.Data.CacheMetadata)
	require.Equal(t, "fresh", updatePayload.Data.CacheMetadata.Source)
	require.False(t, updatePayload.Data.CacheMetadata.IsStale)
}
