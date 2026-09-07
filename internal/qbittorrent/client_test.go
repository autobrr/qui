// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/avast/retry-go"
	"github.com/stretchr/testify/require"
)

type peerSyncTestTransport func(*http.Request) (*http.Response, error)

func (f peerSyncTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPeerSyncManagerIdleExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const activeHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		const idleHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		qbtClient := qbt.NewClient(qbt.Config{Host: "http://example.invalid"}).WithHTTPClient(&http.Client{
			Transport: peerSyncTestTransport(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/v2/sync/torrentPeers", r.URL.Path)
				body := `{"rid":1,"full_update":true,"peers":{"192.0.2.1:1":{"ip":"192.0.2.1","dl_speed":10}}}`
				if r.URL.Query().Get("rid") == "1" {
					body = `{"rid":2,"peers":{"192.0.2.1:1":{"dl_speed":20}}}`
				} else {
					require.Equal(t, "0", r.URL.Query().Get("rid"))
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			}),
		})
		client := &Client{Client: qbtClient, peerSyncManager: make(map[string]*peerSyncEntry)}
		active := client.GetOrCreatePeerSyncManager(activeHash)
		idle := client.GetOrCreatePeerSyncManager(idleHash)
		require.NoError(t, active.Sync(t.Context()))
		require.NoError(t, idle.Sync(t.Context()))

		time.Sleep(4 * time.Minute)
		require.Same(t, active, client.GetOrCreatePeerSyncManager(strings.ToUpper(activeHash)))
		require.NoError(t, active.Sync(t.Context()))
		require.Equal(t, int64(2), active.GetPeers().Rid)
		require.Equal(t, "192.0.2.1", active.GetPeers().Peers["192.0.2.1:1"].IP)
		require.EqualValues(t, 20, active.GetPeers().Peers["192.0.2.1:1"].DownSpeed)

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.NotContains(t, client.peerSyncManager, idleHash, "idle entries expire without another lookup")
		require.Contains(t, client.peerSyncManager, activeHash, "reads renew the idle timeout")
		require.Equal(t, int64(1), idle.GetPeers().Rid, "eviction preserves data held by an existing reader")
		replacement := client.GetOrCreatePeerSyncManager(idleHash)
		require.NotSame(t, idle, replacement)
		require.NoError(t, replacement.Sync(t.Context()))
		require.Equal(t, int64(1), replacement.GetPeers().Rid, "a reopened torrent starts with a full sync")

		time.Sleep(6 * time.Minute)
		synctest.Wait()
		require.Empty(t, client.peerSyncManager)
	})
}

func TestPeerSyncManagerTorrentRemoval(t *testing.T) {
	const activeHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const removedHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var phase atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.16.0")
		case "/api/v2/sync/maindata":
			switch phase.Load() {
			case 0:
				_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{},%q:{}}}`, activeHash, removedHash)
			case 1:
				_, _ = fmt.Fprintf(w, `{"rid":2,"torrents_removed":[%q]}`, removedHash)
			default:
				_, _ = io.WriteString(w, `{"rid":3,"full_update":true,"torrents":{}}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	defer client.optimisticUpdates.Close()
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	active := client.GetOrCreatePeerSyncManager(activeHash)
	removed := client.GetOrCreatePeerSyncManager(strings.ToUpper(removedHash))
	t.Cleanup(func() { client.prunePeerSyncManagers(&qbt.MainData{}) })

	phase.Store(1)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	require.NotContains(t, client.peerSyncManager, removedHash)
	require.Same(t, active, client.GetOrCreatePeerSyncManager(activeHash), "an unchanged torrent survives a delta")
	require.NotSame(t, removed, client.GetOrCreatePeerSyncManager(removedHash))

	phase.Store(2)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	require.Empty(t, client.peerSyncManager, "a full refresh removes absent torrents")
}

// mockSyncEventSink is a test helper that records calls to HandleMainData and HandleSyncError.
type mockSyncEventSink struct {
	mu                   sync.Mutex
	mainData             []*mainDataCall
	trackerHealthUpdates []int
	syncErrors           []*syncErrorCall
}

type mainDataCall struct {
	instanceID int
	data       *qbt.MainData
}

type syncErrorCall struct {
	instanceID int
	err        error
}

func (m *mockSyncEventSink) HandleMainData(instanceID int, data *qbt.MainData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mainData = append(m.mainData, &mainDataCall{instanceID: instanceID, data: data})
}

func (m *mockSyncEventSink) HandleSyncError(instanceID int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncErrors = append(m.syncErrors, &syncErrorCall{instanceID: instanceID, err: err})
}

func (m *mockSyncEventSink) HandleTrackerHealthUpdated(instanceID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackerHealthUpdates = append(m.trackerHealthUpdates, instanceID)
}

func (m *mockSyncEventSink) getMainDataCalls() []*mainDataCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mainData
}

func (m *mockSyncEventSink) getSyncErrorCalls() []*syncErrorCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.syncErrors
}

func (m *mockSyncEventSink) getTrackerHealthUpdates() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.trackerHealthUpdates...)
}

func TestGetTorrentPeersSyncsAndMergesWithoutProxyWarmup(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/sync/torrentPeers" || r.URL.Query().Get("hash") != "abc123" {
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("rid") {
		case "0":
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"peers":{"keep":{"client":"test","dl_speed":10},"remove":{"client":"test"}}}`))
		case "1":
			_, _ = w.Write([]byte(`{"rid":2,"full_update":false,"peers":{"keep":{"dl_speed":20},"add":{"client":"new"}},"peers_removed":["remove"]}`))
		default:
			t.Errorf("unexpected rid: %s", r.URL.Query().Get("rid"))
			http.Error(w, "unexpected rid", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	client := &Client{
		Client:          qbt.NewClient(qbt.Config{Host: srv.URL, APIKey: "test-key"}),
		isHealthy:       true,
		peerSyncManager: make(map[string]*peerSyncEntry),
	}
	sm := &SyncManager{clientPool: &ClientPool{clients: map[int]*Client{1: client}}}

	full, err := sm.GetTorrentPeers(t.Context(), 1, "abc123")
	require.NoError(t, err)
	require.Equal(t, int64(1), full.Rid)
	require.Len(t, full.Peers, 2)
	require.Contains(t, full.Peers, "remove")

	merged, err := sm.GetTorrentPeers(t.Context(), 1, "abc123")
	require.NoError(t, err)
	require.Equal(t, int64(2), merged.Rid)
	require.Len(t, merged.Peers, 2)
	require.NotContains(t, merged.Peers, "remove")
	require.Equal(t, "new", merged.Peers["add"].Client)
	require.Equal(t, "test", merged.Peers["keep"].Client)
	require.Equal(t, int64(20), merged.Peers["keep"].DownSpeed)
	require.Equal(t, int64(2), requests.Load())
}

func TestClientUpdateServerStateDoesNotBlockOnClientMutex(t *testing.T) {
	t.Parallel()

	client := &Client{}
	client.mu.RLock()
	defer client.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.updateServerState(&qbt.MainData{
			ServerState: qbt.ServerState{
				ConnectionStatus: "connected",
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("updateServerState blocked waiting for Client.mu write lock")
	}
}

func TestClientSubcategoriesAlwaysEnabledCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "legacy optional setting",
			version:  "2.14.1",
			expected: false,
		},
		{
			name:     "subcategories unconditional",
			version:  "2.15.0",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{}
			client.applyCapabilitiesLocked(tc.version)

			require.Equal(t, tc.expected, client.SubcategoriesAlwaysEnabled())
		})
	}
}

func TestClientCheckedGetterDoesNotConvoyCapabilityReaders(t *testing.T) {
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	var closeSyncStarted sync.Once
	var releaseSyncOnce sync.Once
	release := func() {
		releaseSyncOnce.Do(func() { close(releaseSync) })
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/sync/maindata":
			closeSyncStarted.Do(func() { close(syncStarted) })
			<-releaseSync
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{}}`))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.16.0"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer release()

	qbtClient := qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60})
	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true
	client := &Client{
		Client:          qbtClient,
		syncManager:     qbtClient.NewSyncManager(syncOpts),
		supportsSetTags: true,
	}

	getterDone := make(chan struct{})
	go func() {
		defer close(getterDone)
		_ = client.getTorrentsByHashes([]string{"abc"})
	}()

	select {
	case <-syncStarted:
	case <-time.After(time.Second):
		t.Fatal("checked getter did not start a sync")
	}

	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- client.RefreshCapabilities(context.Background())
	}()

	writerQueued := make(chan struct{})
	stopWriterProbe := make(chan struct{})
	var stopWriterProbeOnce sync.Once
	stopProbe := func() {
		stopWriterProbeOnce.Do(func() { close(stopWriterProbe) })
	}
	defer stopProbe()
	go func() {
		for {
			select {
			case <-stopWriterProbe:
				return
			default:
			}

			probeDone := make(chan struct{})
			go func() {
				client.mu.RLock()
				_ = client.supportsSetTags
				client.mu.RUnlock()
				close(probeDone)
			}()

			select {
			case <-probeDone:
			case <-stopWriterProbe:
				return
			case <-time.After(10 * time.Millisecond):
				close(writerQueued)
				return
			}
		}
	}()

	refreshComplete := false
	select {
	case err := <-refreshDone:
		require.NoError(t, err)
		refreshComplete = true
	case <-writerQueued:
	case <-time.After(time.Second):
		t.Fatal("capability refresh neither completed nor queued for the client lock")
	}
	stopProbe()

	readDone := make(chan bool, 1)
	go func() {
		readDone <- client.SupportsSetTags()
	}()

	select {
	case supported := <-readDone:
		require.True(t, supported)
	case <-time.After(time.Second):
		t.Fatal("capability reader convoyed behind checked getter and pending writer")
	}

	release()

	if !refreshComplete {
		select {
		case err := <-refreshDone:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("capability refresh did not finish")
		}
	}

	select {
	case <-getterDone:
	case <-time.After(time.Second):
		t.Fatal("checked getter did not finish")
	}
}

func TestNewClientWithTimeoutRejectsLoginCookiesWithoutVerifiedSessionMarker(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{
				Name:     "SID",
				Value:    "unverified",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("<html><title>Login</title></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, time.Second, 60*time.Second)

	require.Error(t, err)
	require.Nil(t, client)
	require.Contains(t, err.Error(), "failed to verify qBittorrent session")
	require.Contains(t, err.Error(), "invalid qBittorrent WebAPI version")
}

func TestNewClientWithTimeoutToleratesSlowCapabilitiesFetch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{
				Name:     "SID",
				Value:    "slow-instance",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			// A saturated-but-alive WebUI: the request parks until the caller's
			// deadline expires without ever answering.
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, time.Second, 60*time.Second)

	require.NoError(t, err, "transient capability fetch failures must not block client creation")
	require.NotNil(t, client)
	require.False(t, client.IsHealthy(), "client should start unhealthy until a capability refresh succeeds")
}

// TestNewClientWithTimeoutTransportIndependentOfLoginBudget verifies that a short
// creation budget (e.g. the 3s login warm) is not baked into the HTTP transport
// timeout for the life of the client.
func TestNewClientWithTimeoutTransportIndependentOfLoginBudget(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{
				Name:     "SID",
				Value:    "quick-login",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.16.0"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, 3*time.Second, 60*time.Second)

	require.NoError(t, err)
	require.Equal(t, 60*time.Second, client.GetHTTPClient().Timeout, "transport timeout must come from the pool, not the creation budget")
}

func TestTruncateWebAPIVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, "2.11.2", truncateWebAPIVersion("2.11.2"))
	require.Equal(t, "<html> <body> login</body> </html>", truncateWebAPIVersion("<html>\n  <body> login</body>\n</html>"))

	got := truncateWebAPIVersion(strings.Repeat(`<div class="login">`, 100))
	require.LessOrEqual(t, len(got), 67)
	require.True(t, strings.HasSuffix(got, "..."))
}

func TestRefreshCapabilitiesBoundsOversizedBodyInError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("<div>proxy login page</div>", 1024)))
	}))
	defer srv.Close()

	client := &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 1})}

	err := client.RefreshCapabilities(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, errInvalidWebAPIVersion)
	require.Less(t, len(err.Error()), 256, "proxy pages must not flow verbatim into the error chain")
}

func TestRefreshCapabilitiesRejectsInvalidWebAPIVersionMarker(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><title>Login</title></html>"))
	}))
	defer srv.Close()

	client := &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 1})}

	err := client.RefreshCapabilities(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid qBittorrent WebAPI version")
	require.False(t, client.SupportsSetTags())
	require.Empty(t, client.GetWebAPIVersion())
}

func TestClientDispatchMainDataCallsSink(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	client := &Client{
		instanceID:    42,
		syncEventSink: sink,
	}

	testData := &qbt.MainData{
		Rid: 123,
		Torrents: map[string]qbt.Torrent{
			"abc123": {Name: "Test Torrent"},
		},
	}

	client.dispatchMainData(testData)

	calls := sink.getMainDataCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call to HandleMainData, got %d", len(calls))
	}

	if calls[0].instanceID != 42 {
		t.Errorf("expected instanceID 42, got %d", calls[0].instanceID)
	}

	if calls[0].data.Rid != 123 {
		t.Errorf("expected Rid 123, got %d", calls[0].data.Rid)
	}
}

func TestClientDispatchMainDataNilSinkDoesNotPanic(t *testing.T) {
	t.Parallel()

	client := &Client{
		instanceID:    42,
		syncEventSink: nil,
	}

	testData := &qbt.MainData{Rid: 123}

	// Should not panic
	client.dispatchMainData(testData)
}

func TestClientDispatchMainDataNilDataDoesNotCallSink(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	client := &Client{
		instanceID:    42,
		syncEventSink: sink,
	}

	client.dispatchMainData(nil)

	calls := sink.getMainDataCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls to HandleMainData for nil data, got %d", len(calls))
	}
}

func TestClientDispatchSyncErrorCallsSink(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	client := &Client{
		instanceID:    42,
		syncEventSink: sink,
	}

	testErr := errors.New("connection refused")

	client.dispatchSyncError(testErr)

	calls := sink.getSyncErrorCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call to HandleSyncError, got %d", len(calls))
	}

	if calls[0].instanceID != 42 {
		t.Errorf("expected instanceID 42, got %d", calls[0].instanceID)
	}

	if calls[0].err.Error() != "connection refused" {
		t.Errorf("expected error 'connection refused', got '%s'", calls[0].err.Error())
	}
}

func TestClientDispatchSyncErrorNilSinkDoesNotPanic(t *testing.T) {
	t.Parallel()

	client := &Client{
		instanceID:    42,
		syncEventSink: nil,
	}

	testErr := errors.New("connection refused")

	// Should not panic
	client.dispatchSyncError(testErr)
}

func TestClientDispatchSyncErrorNilErrorDoesNotCallSink(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	client := &Client{
		instanceID:    42,
		syncEventSink: sink,
	}

	client.dispatchSyncError(nil)

	calls := sink.getSyncErrorCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls to HandleSyncError for nil error, got %d", len(calls))
	}
}

func TestClientHandleSyncManagerErrorIgnoresContextStopped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "wrapped sentinel",
			err:  fmt.Errorf("could not get main data: %w", context.Canceled),
		},
		{
			name: "retry wrapper text",
			err:  errors.New("could not get main data: error making request: All attempts fail: context canceled"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &mockSyncEventSink{}
			client := &Client{
				instanceID:      42,
				isHealthy:       true,
				syncEventSink:   sink,
				lastServerState: &qbt.ServerState{ConnectionStatus: "connected"},
			}

			client.handleSyncManagerError(tc.err)

			require.True(t, client.IsHealthy())
			require.NotNil(t, client.GetCachedServerState())
			require.Empty(t, sink.getSyncErrorCalls())
		})
	}
}

func TestClientHandleSyncManagerErrorKeepsHealthyForDeadline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "deadline sentinel",
			err:  fmt.Errorf("could not get main data: %w", context.DeadlineExceeded),
		},
		{
			name: "retry deadline text",
			err:  errors.New("could not get main data: error making request: All attempts fail: context deadline exceeded"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &mockSyncEventSink{}
			client := &Client{
				instanceID:      42,
				isHealthy:       true,
				syncEventSink:   sink,
				lastServerState: &qbt.ServerState{ConnectionStatus: "connected"},
			}

			client.handleSyncManagerError(tc.err)

			// Slow, not down: health and cached state survive, but the error
			// still dispatches so the SSE loop backs off and escalates its
			// sync budget.
			require.True(t, client.IsHealthy())
			require.NotNil(t, client.GetCachedServerState())
			require.Len(t, sink.getSyncErrorCalls(), 1)
		})
	}
}

func TestIsDeadlineExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadline sentinel", err: fmt.Errorf("could not get main data: %w", context.DeadlineExceeded), want: true},
		{name: "flattened retry text", err: errors.New("All attempts fail: #1: Get \"http://gluetun:8080/api/v2/app/webapiVersion\": context deadline exceeded"), want: true},
		{name: "mixed retry ending in hard failure", err: retry.Error{context.DeadlineExceeded, errors.New("connection refused")}, want: false},
		{name: "mixed retry ending in deadline", err: retry.Error{errors.New("connection refused"), context.DeadlineExceeded}, want: true},
		{name: "net/http client timeout", err: errors.New("Get \"http://x\": net/http: request canceled (Client.Timeout exceeded while awaiting headers)"), want: true},
		{name: "cancellation", err: context.Canceled, want: false},
		{name: "connection refused", err: errors.New("dial tcp 127.0.0.1:1: connect: connection refused"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isDeadlineExpired(tc.err))
		})
	}
}

// TestClientHealthCheckKeepsHealthOnDeadline verifies that a probe timing out against a
// responding-but-saturated instance does not demote health.
func TestClientHealthCheckKeepsHealthOnDeadline(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: 42, isHealthy: true}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.HealthCheck(ctx)
	require.Error(t, err)
	require.True(t, client.IsHealthy(), "a timed-out probe must not demote a healthy client")
}

// TestClientHealthCheckMarksUnhealthyOnHardFailure pins that connection-level failures
// still demote health immediately (#2096: dead instances must go red fast).
func TestClientHealthCheckMarksUnhealthyOnHardFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	client := &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: 42, isHealthy: true}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.HealthCheck(ctx)
	require.Error(t, err)
	require.False(t, client.IsHealthy(), "a hard connection failure must demote the client")
}

func TestClientHandleSyncManagerErrorMarksUnhealthyForRealError(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	client := &Client{
		instanceID:      42,
		isHealthy:       true,
		syncEventSink:   sink,
		lastServerState: &qbt.ServerState{ConnectionStatus: "connected"},
	}

	client.handleSyncManagerError(errors.New("connection refused"))

	require.False(t, client.IsHealthy())
	require.Nil(t, client.GetCachedServerState())
	require.Len(t, sink.getSyncErrorCalls(), 1)
}

func TestSyncManagerNotifyTrackerHealthUpdatedCallsSink(t *testing.T) {
	t.Parallel()

	sink := &mockSyncEventSink{}
	sm := NewSyncManager(nil, nil)
	sm.SetSyncEventSink(sink)

	sm.notifyTrackerHealthUpdated(42)

	require.Equal(t, []int{42}, sink.getTrackerHealthUpdates())
}

func TestClientSetSyncEventSinkUpdatesDispatch(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 42}

	// Initially no sink
	client.dispatchMainData(&qbt.MainData{Rid: 1})

	// Set sink
	sink := &mockSyncEventSink{}
	client.SetSyncEventSink(sink)

	// Now dispatches should reach sink
	client.dispatchMainData(&qbt.MainData{Rid: 2})
	client.dispatchSyncError(errors.New("test error"))

	mainCalls := sink.getMainDataCalls()
	if len(mainCalls) != 1 {
		t.Errorf("expected 1 main data call after setting sink, got %d", len(mainCalls))
	}

	errorCalls := sink.getSyncErrorCalls()
	if len(errorCalls) != 1 {
		t.Errorf("expected 1 error call after setting sink, got %d", len(errorCalls))
	}
}

func TestSplitHostUserinfo(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantHost string
		wantUser string
		wantPass string
	}{
		{
			name:     "plain host unchanged",
			host:     "http://qbit.example.com:8080",
			wantHost: "http://qbit.example.com:8080",
		},
		{
			// Fake credentials exercising userinfo stripping.
			name:     "userinfo stripped and returned",
			host:     "https://proxyuser:proxypass@qbit.example.com",
			wantHost: "https://qbit.example.com",
			wantUser: "proxyuser",
			wantPass: "proxypass",
		},
		{
			name:     "userinfo without password",
			host:     "http://onlyuser@qbit.example.com:8080/base",
			wantHost: "http://qbit.example.com:8080/base",
			wantUser: "onlyuser",
		},
		{
			name:     "password-only userinfo",
			host:     "https://:proxypass@qbit.example.com",
			wantHost: "https://qbit.example.com",
			wantPass: "proxypass",
		},
		{
			name:     "empty host",
			host:     "",
			wantHost: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotUser, gotPass := splitHostUserinfo(tt.host)
			require.Equal(t, tt.wantHost, gotHost)
			require.Equal(t, tt.wantUser, gotUser)
			require.Equal(t, tt.wantPass, gotPass)
		})
	}
}

// TestNewClientWithTimeoutEnablesBulkTrackerFetch pins list hydration to the
// bulk torrents/info request on qBittorrent 5.1+ and to no request at all below
// it. The per-hash torrents/trackers fallback must never fire from qui.
func TestNewClientWithTimeoutEnablesBulkTrackerFetch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		version      string
		bulkRequests int
	}{
		{version: "2.11.3", bulkRequests: 0}, // highest version below the gate
		{version: "2.11.4", bulkRequests: 1}, // lowest version at the gate
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			hits := map[string]int{}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				hits[r.URL.Path]++
				mu.Unlock()
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{
						Name:     "SID",
						Value:    "bulk-trackers",
						Secure:   true,
						HttpOnly: true,
						SameSite: http.SameSiteStrictMode,
					})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/app/webapiVersion":
					_, _ = w.Write([]byte(tc.version))
				case "/api/v2/torrents/info":
					var out []qbt.Torrent
					for hash := range strings.SplitSeq(r.URL.Query().Get("hashes"), "|") {
						out = append(out, qbt.Torrent{Hash: hash, Trackers: []qbt.TorrentTracker{{Url: "https://tracker.example.invalid/announce", Status: 2}}})
					}
					_ = json.MarshalWrite(w, out)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, time.Second, 60*time.Second)
			require.NoError(t, err)

			torrents := []qbt.Torrent{{Hash: "aaa"}, {Hash: "bbb"}, {Hash: "ccc"}}
			enriched, _, _ := (&SyncManager{}).enrichTorrentsWithTrackerData(t.Context(), client, torrents, nil)

			mu.Lock()
			defer mu.Unlock()
			require.Zero(t, hits["/api/v2/torrents/trackers"], "list hydration must never use per-hash torrents/trackers; use torrents/info?includeTrackers on 5.1+ and skip hydration below it")
			require.Equal(t, tc.bulkRequests, hits["/api/v2/torrents/info"], "bulk torrents/info requests")
			for _, torrent := range enriched {
				// One tracker per torrent when hydrated, none when the gate skips hydration.
				require.Len(t, torrent.Trackers, tc.bulkRequests, "hash %s", torrent.Hash)
			}
		})
	}
}

// TestHealthCheckRetriesCapabilitiesUntilLoaded covers a transient capability
// failure at construction. Sync updates stamp the client healthy, which used to
// let HealthCheck skip the probe forever and leave bulk tracker fetching off.
func TestHealthCheckRetriesCapabilitiesUntilLoaded(t *testing.T) {
	t.Parallel()

	var versionFails atomic.Bool
	versionFails.Store(true)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "retry-caps", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			if versionFails.Load() {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("2.11.4"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, time.Second, 60*time.Second)
	require.NoError(t, err, "a transient capability failure must not block client creation")
	require.Empty(t, client.GetWebAPIVersion())
	require.False(t, client.trackerManager().SupportsIncludeTrackers())

	// A successful sync stamps the client healthy and fresh.
	client.updateHealthStatus(true)
	versionFails.Store(false)

	require.NoError(t, client.HealthCheck(t.Context()))
	require.Equal(t, "2.11.4", client.GetWebAPIVersion())
	require.True(t, client.trackerManager().SupportsIncludeTrackers(), "the health check must apply the capability to the tracker manager")
}

// TestSyncPeersSerializesConcurrentReaders pins the ordering guarantee that the
// race detector cannot see: every peer fetch must carry the rid the previous
// merge produced, so an older snapshot can never merge on top of a newer one.
func TestSyncPeersSerializesConcurrentReaders(t *testing.T) {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const stalePeer = "192.0.2.9:9"

	var mu sync.Mutex
	ridsSent := make(map[string]int)
	requests := 0
	newestRid := int64(0)
	secondRequest := make(chan struct{})

	// served records the newest response the server produced. The merged state
	// must end up matching it, whatever order the fetches ran in. Only the rid 1
	// response carries the stale peer, so the rid alone says what to expect.
	served := func(rid int64) {
		mu.Lock()
		defer mu.Unlock()
		newestRid = max(newestRid, rid)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.URL.Query().Get("rid")

		mu.Lock()
		ridsSent[rid]++
		requests++
		first := requests == 1
		if requests == 2 {
			close(secondRequest)
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if first {
			// Hold the oldest snapshot open long enough for an unserialized
			// reader to overtake it. Nothing overtakes it once fetch and merge
			// are serialized, so fall through on the timeout instead.
			select {
			case <-secondRequest:
			case <-time.After(200 * time.Millisecond):
			}
			served(1)
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"peers":{"192.0.2.1:1":{"ip":"192.0.2.1"},"` + stalePeer + `":{"ip":"192.0.2.9"}}}`))
			return
		}

		served(2)
		if rid == "0" {
			// A reader that never saw the first merge asks for a full snapshot.
			_, _ = w.Write([]byte(`{"rid":2,"full_update":true,"peers":{"192.0.2.1:1":{"ip":"192.0.2.1"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"rid":2,"peers_removed":["` + stalePeer + `"]}`))
	}))
	t.Cleanup(srv.Close)

	client := &Client{
		Client:          qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}),
		peerSyncManager: make(map[string]*peerSyncEntry),
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := client.SyncPeers(t.Context(), hash)
			require.NoError(t, err)
		})
	}
	wg.Wait()

	mu.Lock()
	wantRid := newestRid
	wantStalePeer := wantRid == 1
	for rid, count := range ridsSent {
		require.Equalf(t, 1, count, "rid %s was sent by %d fetches; concurrent readers shared a rid", rid, count)
	}
	mu.Unlock()

	// The merged state must match the newest response the server produced, not
	// whichever fetch happened to return last.
	peers := client.GetOrCreatePeerSyncManager(hash).GetPeers()
	require.Equal(t, wantRid, peers.Rid, "rid regressed to an older snapshot")
	if wantStalePeer {
		require.Contains(t, peers.Peers, stalePeer)
	} else {
		require.NotContains(t, peers.Peers, stalePeer, "an older snapshot restored a removed peer")
	}

	// The next reader carries the rid the last merge produced, so the server can
	// answer with a removal and the removal sticks.
	peers, err := client.SyncPeers(t.Context(), hash)
	require.NoError(t, err)
	require.Equal(t, int64(2), peers.Rid)
	require.NotContains(t, peers.Peers, stalePeer, "a removal from an incremental update was lost")
}

// TestSyncPeersLeavesWhenCallerGoesAway keeps a reader whose request was
// cancelled from waiting out a slow qBittorrent.
func TestSyncPeersLeavesWhenCallerGoesAway(t *testing.T) {
	const hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	fetchStarted := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(fetchStarted)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"peers":{}}`))
	}))
	t.Cleanup(srv.Close)
	// Cleanups run last registered first, so the handler is released before the
	// server shuts down and waits for it.
	t.Cleanup(func() { close(release) })

	client := &Client{
		Client:          qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}),
		peerSyncManager: make(map[string]*peerSyncEntry),
	}

	ctx, cancel := context.WithCancel(t.Context())
	errs := make(chan error, 1)
	go func() {
		_, err := client.SyncPeers(ctx, hash)
		errs <- err
	}()

	// Cancel while the fetch is in flight, which is what a closed tab does.
	<-fetchStarted
	cancel()

	select {
	case err := <-errs:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("SyncPeers kept waiting for a fetch after its caller went away")
	}
}
