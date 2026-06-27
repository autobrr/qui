// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
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

			require.True(t, client.IsHealthy())
			require.NotNil(t, client.GetCachedServerState())
			require.Empty(t, sink.getSyncErrorCalls())
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
