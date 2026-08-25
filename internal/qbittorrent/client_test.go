// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

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

	client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, time.Second)

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

	client, err := NewClientWithTimeout(1, srv.URL, "user", "pass", "", nil, nil, true, time.Second)

	require.NoError(t, err, "transient capability fetch failures must not block client creation")
	require.NotNil(t, client)
	require.False(t, client.IsHealthy(), "client should start unhealthy until a capability refresh succeeds")
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

func TestClientHandleSyncManagerErrorMarksUnhealthyForDeadline(t *testing.T) {
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

			require.False(t, client.IsHealthy())
			require.Nil(t, client.GetCachedServerState())
			require.Len(t, sink.getSyncErrorCalls(), 1)
		})
	}
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
