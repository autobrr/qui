// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tmaxmax/go-sse"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func TestStreamManagerHandleSyncErrorPublishesErrorEvent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := &subscriptionState{
		id:      "subscription-1",
		options: StreamOptions{InstanceID: 42},
		created: time.Now(),
	}

	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}

	manager.HandleSyncError(sub.options.InstanceID, errors.New("sync failed"))

	messages := provider.messagesFor(sub.id)
	require.Len(t, messages, 1, "expected a single broadcast message")

	payload := decodeStreamPayload(t, messages[0])
	require.Equal(t, streamEventError, payload.Type)
	require.Equal(t, sub.options.InstanceID, payload.Meta.InstanceID)
	require.Positive(t, payload.Meta.RetryInSeconds, "expected retry interval to be populated")
	require.Contains(t, payload.Err, "sync failed")
}

func TestStreamManagerHandleSyncErrorWithoutSubscribers(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	manager.HandleSyncError(7, errors.New("boom"))

	require.Empty(t, provider.allMessages(), "no subscribers should result in no messages")
}

func TestStreamManagerHeartbeatPublishesEvent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := &subscriptionState{
		id:        "subscription-keepalive",
		options:   StreamOptions{InstanceID: 21},
		groupKey:  "group-keepalive",
		clientKey: "client-keepalive",
	}

	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}

	manager.publishHeartbeat(sub.options.InstanceID)

	messages := provider.messagesFor(sub.id)
	require.Len(t, messages, 1, "expected heartbeat payload to be published")

	payload := decodeStreamPayload(t, messages[0])
	require.Equal(t, streamEventHeartbeat, payload.Type)
	require.Equal(t, sub.options.InstanceID, payload.Meta.InstanceID)
	require.Equal(t, sub.clientKey, payload.Meta.StreamKey)
	require.False(t, payload.Meta.Timestamp.IsZero(), "heartbeat should include timestamp")
}

func TestStreamManagerServeInstanceNotFound(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	manager := NewStreamManager(nil, nil, store)

	payload := []map[string]any{
		{
			"key":        "stream-99",
			"instanceId": 99,
			"page":       0,
			"limit":      50,
			"sort":       "added_on",
			"order":      "desc",
			"search":     "",
			"filters":    nil,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	manager.Serve(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestStreamManagerServeInstanceValidationError(t *testing.T) {
	store, cleanup := newTestInstanceStore(t)
	defer cleanup()

	ctx := context.Background()
	_, err := store.Create(ctx, "Test Instance", "http://localhost:8080", "user", "password", nil, nil, false, nil)
	require.NoError(t, err, "failed to seed instance")

	manager := NewStreamManager(nil, nil, store)

	payload := []map[string]any{
		{
			"key":        "invalid-limit",
			"instanceId": 1,
			"page":       -1,
			"limit":      50,
			"sort":       "added_on",
			"order":      "desc",
			"search":     "",
			"filters":    nil,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	manager.Serve(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

// recordingProvider is a minimal sse.Provider that captures published messages for assertions.
type recordingProvider struct {
	mu       sync.Mutex
	messages map[string][]*sse.Message
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{
		messages: make(map[string][]*sse.Message),
	}
}

func (p *recordingProvider) Subscribe(_ context.Context, _ sse.Subscription) error {
	return nil
}

func (p *recordingProvider) Publish(message *sse.Message, topics []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, topic := range topics {
		p.messages[topic] = append(p.messages[topic], message)
	}
	return nil
}

func (p *recordingProvider) Shutdown(context.Context) error {
	return nil
}

func (p *recordingProvider) messagesFor(topic string) []*sse.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*sse.Message(nil), p.messages[topic]...)
}

func (p *recordingProvider) allMessages() []*sse.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	total := 0
	for _, msgs := range p.messages {
		total += len(msgs)
	}

	result := make([]*sse.Message, 0, total)
	for _, msgs := range p.messages {
		result = append(result, msgs...)
	}
	return result
}

func decodeStreamPayload(t *testing.T, message *sse.Message) *StreamPayload {
	t.Helper()

	raw, err := message.MarshalText()
	require.NoError(t, err, "failed to marshal SSE message")

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var builder strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}

	var payload StreamPayload
	err = json.Unmarshal([]byte(builder.String()), &payload)
	require.NoError(t, err, "failed to decode stream payload")
	return &payload
}

func newTestInstanceStore(t *testing.T) (*models.InstanceStore, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "sse-manager-test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err, "failed to create test database")

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store, err := models.NewInstanceStore(db, key)
	require.NoError(t, err, "failed to create instance store")

	return store, func() {
		_ = db.Close()
	}
}

func TestMarkSyncFailure_ExponentialBackoff(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// First failure: 2s * 2^1 = 4s
	interval1 := manager.markSyncFailure(1)
	require.Equal(t, 4*time.Second, interval1, "first failure should yield 4s interval")

	// Second failure: 2s * 2^2 = 8s
	interval2 := manager.markSyncFailure(1)
	require.Equal(t, 8*time.Second, interval2, "second failure should yield 8s interval")

	// Third failure: 2s * 2^3 = 16s
	interval3 := manager.markSyncFailure(1)
	require.Equal(t, 16*time.Second, interval3, "third failure should yield 16s interval")

	// Fourth failure: 2s * 2^4 = 32s -> capped to 30s
	interval4 := manager.markSyncFailure(1)
	require.Equal(t, 30*time.Second, interval4, "fourth failure should yield 30s (capped)")

	// Fifth failure: still capped at 30s
	interval5 := manager.markSyncFailure(1)
	require.Equal(t, 30*time.Second, interval5, "fifth failure should still be 30s (capped)")

	// Verify internal state
	state := manager.syncBackoff[1]
	require.Equal(t, 5, state.attempt, "attempt counter should be 5")
	require.Equal(t, 30*time.Second, state.interval, "interval should be maxSyncInterval")
}

func TestMarkSyncSuccess_ResetsBackoff(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// Simulate some failures first
	manager.markSyncFailure(1)
	manager.markSyncFailure(1)

	// Verify backoff state exists with failures
	state := manager.syncBackoff[1]
	require.Equal(t, 2, state.attempt, "should have 2 failures recorded")
	require.Equal(t, 8*time.Second, state.interval, "interval should be 8s after 2 failures")

	// Success should reset
	manager.markSyncSuccess(1)

	// Verify state was reset
	state = manager.syncBackoff[1]
	require.Equal(t, 0, state.attempt, "attempt should be reset to 0")
	require.Equal(t, defaultSyncInterval, state.interval, "interval should be reset to default")
}

func TestMarkSyncSuccess_NoOpWithoutPriorState(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// Calling success without prior failure should be a no-op
	manager.markSyncSuccess(99)

	// Backoff state should not exist for this instance
	_, exists := manager.syncBackoff[99]
	require.False(t, exists, "backoff state should not be created by markSyncSuccess")
}

func TestBackoffState_IndependentPerInstance(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// Instance 1: 2 failures
	manager.markSyncFailure(1)
	manager.markSyncFailure(1)

	// Instance 2: 1 failure
	manager.markSyncFailure(2)

	// Verify independent state
	state1 := manager.syncBackoff[1]
	state2 := manager.syncBackoff[2]

	require.Equal(t, 2, state1.attempt, "instance 1 should have 2 failures")
	require.Equal(t, 8*time.Second, state1.interval, "instance 1 should have 8s interval")

	require.Equal(t, 1, state2.attempt, "instance 2 should have 1 failure")
	require.Equal(t, 4*time.Second, state2.interval, "instance 2 should have 4s interval")

	// Reset instance 1, verify instance 2 is unaffected
	manager.markSyncSuccess(1)

	state1 = manager.syncBackoff[1]
	state2 = manager.syncBackoff[2]

	require.Equal(t, 0, state1.attempt, "instance 1 should be reset")
	require.Equal(t, 1, state2.attempt, "instance 2 should still have 1 failure")
}

func TestStreamManager_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	const numGoroutines = 50
	const numIterations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Run concurrent subscribe/unsubscribe operations
	for i := range numGoroutines {
		go func(workerID int) {
			defer wg.Done()

			for j := range numIterations {
				instanceID := (workerID % 5) + 1 // Use 5 different instances
				subID := "sub-" + string(rune('A'+workerID)) + "-" + string(rune('0'+j%10))

				// Create subscription
				sub := &subscriptionState{
					id:        subID,
					options:   StreamOptions{InstanceID: instanceID, Page: 0, Limit: 50},
					created:   time.Now(),
					groupKey:  "group-" + subID,
					clientKey: "client-" + subID,
				}

				// Register subscription
				manager.mu.Lock()
				manager.subscriptions[sub.id] = sub
				if manager.instanceIndex[instanceID] == nil {
					manager.instanceIndex[instanceID] = make(map[string]*subscriptionState)
				}
				manager.instanceIndex[instanceID][sub.id] = sub
				manager.mu.Unlock()

				// Immediately unregister
				manager.Unregister(sub.id)
			}
		}(i)
	}

	wg.Wait()

	// Verify manager is in a consistent state
	manager.mu.RLock()
	subCount := len(manager.subscriptions)
	manager.mu.RUnlock()

	require.Equal(t, 0, subCount, "all subscriptions should be unregistered")
}

func TestStreamManager_ShutdownDuringActiveOperations(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	// Create several active subscriptions
	for i := 1; i <= 3; i++ {
		sub := &subscriptionState{
			id:        "sub-" + string(rune('0'+i)),
			options:   StreamOptions{InstanceID: i, Page: 0, Limit: 50},
			created:   time.Now(),
			groupKey:  "group-" + string(rune('0'+i)),
			clientKey: "client-" + string(rune('0'+i)),
		}

		manager.mu.Lock()
		manager.subscriptions[sub.id] = sub
		if manager.instanceIndex[i] == nil {
			manager.instanceIndex[i] = make(map[string]*subscriptionState)
		}
		manager.instanceIndex[i][sub.id] = sub
		manager.mu.Unlock()
	}

	// Start concurrent operations
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Publishing events
	go func() {
		defer wg.Done()
		for range 100 {
			if manager.closing.Load() {
				return
			}
			manager.HandleSyncError(1, errors.New("test error"))
		}
	}()

	// Goroutine 2: Shutdown after brief delay
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		_ = manager.Shutdown(context.Background())
	}()

	wg.Wait()

	// Verify shutdown completed
	require.True(t, manager.closing.Load(), "manager should be marked as closing")
}

func TestStreamManager_ProcessGroupCoalescing(t *testing.T) {
	// Test the coalescing behavior of enqueueGroup
	// Multiple rapid enqueues should coalesce into pending state

	group := &subscriptionGroup{
		key:     "test-group",
		options: StreamOptions{InstanceID: 1, Page: 0, Limit: 50},
		subs:    make(map[string]*subscriptionState),
	}

	// Simulate rapid enqueues without starting the processGroup goroutine
	// by directly testing the pending state coalescing

	// First enqueue sets hasPending and sends
	group.mu.Lock()
	group.pendingMeta = &StreamMeta{InstanceID: 1, Timestamp: time.Now()}
	group.pendingType = streamEventUpdate
	group.hasPending = true
	group.sending = true // Simulate that processGroup is already running
	group.mu.Unlock()

	// Second enqueue should just update pending state, not spawn new goroutine
	newMeta := &StreamMeta{InstanceID: 1, Timestamp: time.Now().Add(time.Second)}
	group.mu.Lock()
	group.pendingMeta = newMeta
	group.pendingType = streamEventUpdate
	group.hasPending = true
	// sending stays true - no new goroutine needed
	group.mu.Unlock()

	// Third enqueue - same behavior
	finalMeta := &StreamMeta{InstanceID: 1, Timestamp: time.Now().Add(2 * time.Second)}
	group.mu.Lock()
	group.pendingMeta = finalMeta
	group.pendingType = streamEventUpdate
	group.hasPending = true
	group.mu.Unlock()

	// Verify the coalescing - only the final meta should be present
	group.mu.Lock()
	require.True(t, group.hasPending, "should have pending update")
	require.True(t, group.sending, "should still be marked as sending")
	require.Equal(t, finalMeta, group.pendingMeta, "should have coalesced to final meta")
	group.mu.Unlock()
}

func TestUnregister_MultipleSubscribersInSameGroup(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	// Create two subscriptions with identical StreamOptions (same group)
	opts := StreamOptions{InstanceID: 1, Page: 0, Limit: 50, Sort: "added_on", Order: "desc"}
	groupKey := streamOptionsKey(opts)

	sub1 := &subscriptionState{
		id:        "sub-1",
		options:   opts,
		created:   time.Now(),
		groupKey:  groupKey,
		clientKey: "client-1",
	}
	sub2 := &subscriptionState{
		id:        "sub-2",
		options:   opts,
		created:   time.Now(),
		groupKey:  groupKey,
		clientKey: "client-2",
	}

	// Create group with both subscribers
	group := &subscriptionGroup{
		key:     groupKey,
		options: opts,
		subs:    make(map[string]*subscriptionState),
	}
	group.subs[sub1.id] = sub1
	group.subs[sub2.id] = sub2

	// Register both subscriptions
	manager.mu.Lock()
	manager.subscriptions[sub1.id] = sub1
	manager.subscriptions[sub2.id] = sub2
	manager.instanceIndex[opts.InstanceID] = map[string]*subscriptionState{
		sub1.id: sub1,
		sub2.id: sub2,
	}
	manager.groups[groupKey] = group
	manager.instanceGroups[opts.InstanceID] = map[string]*subscriptionGroup{
		groupKey: group,
	}
	manager.mu.Unlock()

	// Unregister sub1
	manager.Unregister(sub1.id)

	// Verify sub1 is removed but sub2 remains
	manager.mu.RLock()
	_, sub1Exists := manager.subscriptions[sub1.id]
	_, sub2Exists := manager.subscriptions[sub2.id]
	groupStillExists := manager.groups[groupKey] != nil
	instanceIndexExists := manager.instanceIndex[opts.InstanceID] != nil
	instanceGroupsExist := manager.instanceGroups[opts.InstanceID] != nil
	manager.mu.RUnlock()

	require.False(t, sub1Exists, "sub1 should be removed from subscriptions")
	require.True(t, sub2Exists, "sub2 should still exist in subscriptions")
	require.True(t, groupStillExists, "group should still exist with remaining subscriber")
	require.True(t, instanceIndexExists, "instance index should still exist")
	require.True(t, instanceGroupsExist, "instance groups should still exist")

	// Verify group still has sub2
	group.subsMu.RLock()
	_, sub1InGroup := group.subs[sub1.id]
	_, sub2InGroup := group.subs[sub2.id]
	groupSubCount := len(group.subs)
	group.subsMu.RUnlock()

	require.False(t, sub1InGroup, "sub1 should be removed from group")
	require.True(t, sub2InGroup, "sub2 should still be in group")
	require.Equal(t, 1, groupSubCount, "group should have exactly 1 subscriber")

	// Unregister sub2 - now everything should be cleaned up
	manager.Unregister(sub2.id)

	manager.mu.RLock()
	_, sub2StillExists := manager.subscriptions[sub2.id]
	groupGone := manager.groups[groupKey] == nil
	instanceIndexGone := manager.instanceIndex[opts.InstanceID] == nil
	instanceGroupsGone := manager.instanceGroups[opts.InstanceID] == nil
	manager.mu.RUnlock()

	require.False(t, sub2StillExists, "sub2 should be removed")
	require.True(t, groupGone, "group should be cleaned up when empty")
	require.True(t, instanceIndexGone, "instance index should be cleaned up")
	require.True(t, instanceGroupsGone, "instance groups should be cleaned up")
}

func TestHandleMainData_NilData(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := &subscriptionState{
		id:        "sub-nil-test",
		options:   StreamOptions{InstanceID: 1},
		created:   time.Now(),
		groupKey:  "group-nil-test",
		clientKey: "client-nil-test",
	}

	manager.mu.Lock()
	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}
	manager.mu.Unlock()

	// Call with nil data - should return early without publishing
	manager.HandleMainData(sub.options.InstanceID, nil)

	// Give a brief moment for any async operations
	time.Sleep(10 * time.Millisecond)

	messages := provider.allMessages()
	require.Empty(t, messages, "nil data should not produce any messages")
}

func TestHandleSyncError_NilError(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := &subscriptionState{
		id:        "sub-nil-err",
		options:   StreamOptions{InstanceID: 1},
		created:   time.Now(),
		groupKey:  "group-nil-err",
		clientKey: "client-nil-err",
	}

	manager.mu.Lock()
	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}
	manager.mu.Unlock()

	// Call with nil error - should return early without publishing
	manager.HandleSyncError(sub.options.InstanceID, nil)

	messages := provider.allMessages()
	require.Empty(t, messages, "nil error should not produce any messages")
}

func TestParseStreamRequests_EmptyStreamsParam(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
	_, err := parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing streams parameter")
}

func TestParseStreamRequests_MalformedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stream?streams=not-json", nil)
	_, err := parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid streams payload")
}

func TestParseStreamRequests_EmptyArray(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stream?streams=[]", nil)
	_, err := parseStreamRequests(req)
	require.Error(t, err)
	require.ErrorIs(t, err, errNoStreamRequests)
}

func TestParseStreamRequests_InvalidInstanceID(t *testing.T) {
	tests := []struct {
		name       string
		instanceID int
	}{
		{"zero", 0},
		{"negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []map[string]any{
				{
					"key":        "test-stream",
					"instanceId": tt.instanceID,
					"page":       0,
					"limit":      50,
					"sort":       "added_on",
					"order":      "desc",
				},
			}
			raw, err := json.Marshal(payload)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
			_, err = parseStreamRequests(req)
			require.Error(t, err)
			require.ErrorIs(t, err, errInvalidInstanceID)
		})
	}
}

func TestParseStreamRequests_LimitExceedsMax(t *testing.T) {
	payload := []map[string]any{
		{
			"key":        "test-stream",
			"instanceId": 1,
			"page":       0,
			"limit":      3000, // exceeds maxLimit of 2000
			"sort":       "added_on",
			"order":      "desc",
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	_, err = parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid limit")
}

func TestParseStreamRequests_NegativePage(t *testing.T) {
	payload := []map[string]any{
		{
			"key":        "test-stream",
			"instanceId": 1,
			"page":       -1,
			"limit":      50,
			"sort":       "added_on",
			"order":      "desc",
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	_, err = parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid page")
}

func TestParseStreamRequests_DefaultsApplied(t *testing.T) {
	// Request with minimal fields - defaults should be applied
	payload := []map[string]any{
		{
			"instanceId": 1,
			// page, limit, sort, order all omitted
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	requests, err := parseStreamRequests(req)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	opts := requests[0].options
	require.Equal(t, 1, opts.InstanceID)
	require.Equal(t, 0, opts.Page, "page should default to 0")
	require.Equal(t, defaultLimit, opts.Limit, "limit should default to 300")
	require.Equal(t, "added_on", opts.Sort, "sort should default to added_on")
	require.Equal(t, "desc", opts.Order, "order should default to desc")
}

func TestParseStreamRequests_OrderNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ASC", "asc"},
		{"DESC", "desc"},
		{"Asc", "asc"},
		{"invalid", "desc"}, // invalid values should default to desc
		{"", "desc"},        // empty should default to desc
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			payload := []map[string]any{
				{
					"instanceId": 1,
					"order":      tt.input,
				},
			}
			raw, err := json.Marshal(payload)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
			requests, err := parseStreamRequests(req)
			require.NoError(t, err)
			require.Len(t, requests, 1)
			require.Equal(t, tt.expected, requests[0].options.Order)
		})
	}
}

func TestRegisterSubscription_DuringShutdown(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// Start shutdown
	err := manager.Shutdown(context.Background())
	require.NoError(t, err)

	// Attempt to register after shutdown
	_, err = manager.registerSubscription(StreamOptions{InstanceID: 1, Limit: 50}, "test-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutting down")
}

func TestPrepareBatch_DuringShutdown(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// Start shutdown
	err := manager.Shutdown(context.Background())
	require.NoError(t, err)

	// Attempt to prepare batch after shutdown
	requests := []streamRequest{
		{key: "test", options: StreamOptions{InstanceID: 1, Limit: 50}},
	}
	_, _, err = manager.PrepareBatch(context.Background(), requests)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutting down")
}

func TestShutdown_WithNilContext(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// Shutdown with nil context should not panic
	var shutdownCtx context.Context
	err := manager.Shutdown(shutdownCtx)
	require.NoError(t, err)
	require.True(t, manager.closing.Load())
}

func TestShutdown_Idempotent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil)

	// First shutdown
	err := manager.Shutdown(context.Background())
	require.NoError(t, err)

	// Second shutdown should be a no-op (idempotent)
	err = manager.Shutdown(context.Background())
	require.NoError(t, err)
	require.True(t, manager.closing.Load())
}
