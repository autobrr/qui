// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
	"github.com/tmaxmax/go-sse"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestStreamManagerHandleSyncErrorPublishesErrorEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		sub := withConn(&subscriptionState{
			id:      "subscription-1",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 42}},
			created: time.Now(),
		})

		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		manager.HandleSyncError(sub.options.View.InstanceID, errors.New("sync failed"))

		synctest.Wait()

		messages := provider.messagesFor(sub.id)
		require.Len(t, messages, 1, "expected a single broadcast message")

		payload := decodeStreamPayload(t, messages[0])
		require.Equal(t, streamEventError, payload.Type)
		require.Equal(t, sub.options.View.InstanceID, payload.Meta.InstanceID)
		require.Positive(t, payload.Meta.RetryInSeconds, "expected retry interval to be populated")
		require.Contains(t, payload.Err, "sync failed")
	})
}

func TestStreamManagerHandleSyncErrorPreservesHealthBlockerStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		sub := withConn(&subscriptionState{
			id:      "subscription-blocked",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 42}},
			created: time.Now(),
		})

		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		manager.HandleSyncError(sub.options.View.InstanceID, &qbittorrent.InstanceHealthBlockerError{
			Kind:       qbittorrent.InstanceHealthBlockerHealthCheckInProgress,
			InstanceID: sub.options.View.InstanceID,
		})

		synctest.Wait()
		require.Len(t, provider.messagesFor(sub.id), 1, "expected a single broadcast message")

		payload := decodeStreamPayload(t, provider.messagesFor(sub.id)[0])
		require.Equal(t, streamEventError, payload.Type)
		require.Positive(t, payload.Meta.RetryInSeconds, "expected retry interval to be populated")
		require.Contains(t, payload.Err, "paused")
		require.Contains(t, payload.Err, "already running a health check")
		require.Contains(t, payload.Err, "retry shortly")
		require.NotContains(t, payload.Err, "Sync with qBittorrent failed")
	})
}

func TestStreamManagerHandleSyncErrorPreservesBackoffStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		sub := withConn(&subscriptionState{
			id:      "subscription-backoff",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 42}},
			created: time.Now(),
		})

		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		manager.HandleSyncError(sub.options.View.InstanceID, fmt.Errorf("failed to get client: %w", &qbittorrent.InstanceHealthBlockerError{
			Kind:       qbittorrent.InstanceHealthBlockerBackoff,
			InstanceID: sub.options.View.InstanceID,
			RetryAfter: 12 * time.Second,
		}))

		synctest.Wait()
		require.Len(t, provider.messagesFor(sub.id), 1, "expected a single broadcast message")

		payload := decodeStreamPayload(t, provider.messagesFor(sub.id)[0])
		require.Equal(t, streamEventError, payload.Type)
		require.Equal(t, 12, payload.Meta.RetryInSeconds, "retry hint must tick on the blocker's clock, not the sync loop's")
		require.Contains(t, payload.Err, "paused")
		require.Contains(t, payload.Err, "health-check backoff")
		require.Contains(t, payload.Err, "retrying in 12s")
		require.NotContains(t, payload.Err, "Sync with qBittorrent failed")
	})
}

func TestStreamManagerHandleSyncErrorMapsAuthFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		sub := withConn(&subscriptionState{
			id:      "subscription-auth",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 42}},
			created: time.Now(),
		})

		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		manager.HandleSyncError(sub.options.View.InstanceID, qbt.ErrBadCredentials)

		synctest.Wait()
		require.Len(t, provider.messagesFor(sub.id), 1, "expected a single broadcast message")

		payload := decodeStreamPayload(t, provider.messagesFor(sub.id)[0])
		require.Equal(t, streamEventError, payload.Type)
		require.Contains(t, payload.Err, "paused")
		require.Contains(t, payload.Err, "2FA")
		require.Contains(t, payload.Err, "unattended")
		require.Contains(t, payload.Err, "Update the instance login details")
		require.NotContains(t, payload.Err, "Sync with qBittorrent failed")
	})
}

func TestBlockerRetrySecondsFloorsSubSecondBackoff(t *testing.T) {
	seconds, ok := blockerRetrySeconds(fmt.Errorf("failed to get client: %w", &qbittorrent.InstanceHealthBlockerError{
		Kind:       qbittorrent.InstanceHealthBlockerBackoff,
		InstanceID: 1,
		RetryAfter: 300 * time.Millisecond,
	}))
	require.True(t, ok)
	require.Equal(t, 1, seconds, "sub-second backoff must not round to 0 (omitempty would drop the hint)")

	_, ok = blockerRetrySeconds(errors.New("plain failure"))
	require.False(t, ok)
}

func TestSyncErrorMessageRetryHints(t *testing.T) {
	blocker := &qbittorrent.InstanceHealthBlockerError{
		Kind:       qbittorrent.InstanceHealthBlockerBackoff,
		InstanceID: 42,
		RetryAfter: 12 * time.Second,
	}
	blockerMessage := syncErrorMessage(blocker, 30)
	require.Equal(t, "Sync with qBittorrent paused: qBittorrent instance 42 is in health-check backoff after a failed connection; retrying in 12s", blockerMessage)
	require.NotContains(t, blockerMessage, "retrying in 12s; retrying in 30s")

	authMessage := syncErrorMessage(qbt.ErrBadCredentials, 30)
	require.Contains(t, authMessage, "Sync with qBittorrent paused:")
	require.Contains(t, authMessage, "retrying in 30s")

	timeoutMessage := syncErrorMessage(fmt.Errorf("could not get main data: %w", context.DeadlineExceeded), 30)
	require.Contains(t, timeoutMessage, "qBittorrent did not answer in time")
	require.Contains(t, timeoutMessage, "retrying in 30s")

	fallbackMessage := syncErrorMessage(errors.New("temporary sync failure"), 30)
	require.Equal(t, "Sync with qBittorrent failed (temporary sync failure); retrying in 30s", fallbackMessage)
}

func TestStreamManagerHandleSyncErrorStampsLastSuccessfulSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		lastGood := time.Now().Add(-90 * time.Second)
		manager.lastSuccessfulSyncFn = func(_ context.Context, _ int) time.Time {
			return lastGood
		}

		sub := withConn(&subscriptionState{
			id:      "subscription-stale",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 42}},
			created: time.Now(),
		})
		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		manager.HandleSyncError(sub.options.View.InstanceID, errors.New("sync failed"))

		synctest.Wait()
		require.Len(t, provider.messagesFor(sub.id), 1, "expected a single broadcast message")

		payload := decodeStreamPayload(t, provider.messagesFor(sub.id)[0])
		require.Equal(t, streamEventError, payload.Type)
		require.NotNil(t, payload.Meta.LastSuccessfulSync, "error meta must carry the last successful sync time")
		require.WithinDuration(t, lastGood, *payload.Meta.LastSuccessfulSync, time.Second)
		require.True(t, payload.Meta.LastSuccessfulSync.Before(time.Now().Add(-30*time.Second)),
			"last successful sync must not advance on a failed attempt")
	})
}

func TestStreamManagerHandleSyncErrorResolvesStampOffCallbackGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		stampStarted := make(chan struct{})
		releaseStamp := make(chan struct{})
		manager.lastSuccessfulSyncFn = func(_ context.Context, _ int) time.Time {
			close(stampStarted)
			<-releaseStamp
			return time.Time{}
		}

		sub := withConn(&subscriptionState{
			id:      "subscription-async-stamp",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 42}},
			created: time.Now(),
		})
		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		returned := make(chan struct{})
		go func() {
			manager.HandleSyncError(sub.options.View.InstanceID, errors.New("sync failed"))
			close(returned)
		}()

		select {
		case <-stampStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("lastSuccessfulSyncFn was never invoked")
		}

		select {
		case <-returned:
		case <-time.After(2 * time.Second):
			t.Fatal("HandleSyncError blocked on the staleness stamp; it must resolve it off the sync-loop callback goroutine")
		}

		close(releaseStamp)

		synctest.Wait()
		require.Len(t, provider.messagesFor(sub.id), 1, "error frame should still be published once the stamp resolves")
	})
}

func TestStreamManagerHandleSyncErrorOmitsLastSuccessfulSyncWhenUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewStreamManager(nil, nil, nil, nil)
		provider := newRecordingProvider()
		manager.server.Provider = provider

		sub := withConn(&subscriptionState{
			id:      "subscription-unknown",
			options: StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 7}},
			created: time.Now(),
		})
		manager.subscriptions[sub.id] = sub
		manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
			sub.id: sub,
		}

		manager.HandleSyncError(sub.options.View.InstanceID, errors.New("boom"))

		synctest.Wait()
		require.Len(t, provider.messagesFor(sub.id), 1, "expected a single broadcast message")

		payload := decodeStreamPayload(t, provider.messagesFor(sub.id)[0])
		require.Nil(t, payload.Meta.LastSuccessfulSync, "no resolvable client means the field must be omitted")
	})
}

func TestStreamManagerHandleSyncErrorWithoutSubscribers(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	manager.HandleSyncError(7, errors.New("boom"))

	require.Empty(t, provider.allMessages(), "no subscribers should result in no messages")
}

func TestStreamManagerHeartbeatPublishesEvent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := withConn(&subscriptionState{
		id:        "subscription-keepalive",
		options:   StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 21}},
		groupKey:  "group-keepalive",
		clientKey: "client-keepalive",
	})

	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}

	manager.publishHeartbeat(sub.options.View.InstanceID)

	messages := provider.messagesFor(sub.id)
	require.Len(t, messages, 1, "expected heartbeat payload to be published")

	payload := decodeStreamPayload(t, messages[0])
	require.Equal(t, streamEventHeartbeat, payload.Type)
	require.Equal(t, sub.options.View.InstanceID, payload.Meta.InstanceID)
	require.Equal(t, sub.clientKey, payload.Meta.StreamKey)
	require.False(t, payload.Meta.Timestamp.IsZero(), "heartbeat should include timestamp")
}

func TestStreamManagerServeInstanceNotFound(t *testing.T) {
	store := newTestInstanceStore(t)

	manager := NewStreamManager(nil, nil, nil, store)

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
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	manager.Serve(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestStreamManagerServeInstanceValidationError(t *testing.T) {
	store := newTestInstanceStore(t)

	ctx := context.Background()
	_, err := store.Create(ctx, "Test Instance", "http://localhost:8080", "user", "password", nil, nil, false, nil)
	require.NoError(t, err, "failed to seed instance")

	manager := NewStreamManager(nil, nil, nil, store)

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
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	manager.Serve(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestStreamManagerServeMissingInstanceStore(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	payload := []map[string]any{
		{
			"key":        "stream-1",
			"instanceId": 1,
			"page":       0,
			"limit":      50,
			"sort":       "added_on",
			"order":      "desc",
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)

	require.NotPanics(t, func() {
		manager.Serve(recorder, request)
	})
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}

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

func newTestInstanceStore(t *testing.T) *models.InstanceStore {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "sse-manager-test")

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	store, err := models.NewInstanceStore(db, key)
	require.NoError(t, err, "failed to create instance store")

	return store
}

func TestMarkSyncFailure_ExponentialBackoff(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	interval1 := manager.markSyncFailure(1)
	require.Equal(t, 4*time.Second, interval1, "first failure should yield 4s interval")

	interval2 := manager.markSyncFailure(1)
	require.Equal(t, 8*time.Second, interval2, "second failure should yield 8s interval")

	interval3 := manager.markSyncFailure(1)
	require.Equal(t, 16*time.Second, interval3, "third failure should yield 16s interval")

	interval4 := manager.markSyncFailure(1)
	require.Equal(t, 30*time.Second, interval4, "fourth failure should yield 30s (capped)")

	interval5 := manager.markSyncFailure(1)
	require.Equal(t, 30*time.Second, interval5, "fifth failure should still be 30s (capped)")
}

func TestMarkSyncSuccess_ResetsBackoff(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	manager.markSyncFailure(1)
	manager.markSyncFailure(1)
	manager.markSyncSuccess(1)

	require.Equal(t, 4*time.Second, manager.markSyncFailure(1), "the streak should start over after a success")
}

func TestStreamManager_syncTimeout_followsClientTimeout(t *testing.T) {
	tests := []struct {
		name          string
		clientTimeout time.Duration
		want          time.Duration
	}{
		{name: "larger_transport_raises_full_budget", clientTimeout: 180 * time.Second, want: 180 * time.Second},
		{name: "default_transport_keeps_full_budget", clientTimeout: 60 * time.Second, want: syncTimeoutFull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := qbittorrent.NewClientPool(nil, nil, tt.clientTimeout)
			require.NoError(t, err)
			defer pool.Close()

			manager := NewStreamManager(pool, nil, nil, nil)
			require.Equal(t, tt.want, manager.syncTimeout(1), "unprimed instance should get the full budget bounded by the transport")
		})
	}
}

func TestStreamManager_syncTimeout_concurrent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	const goroutines = 8
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				_ = manager.syncTimeout(1)
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				manager.markSyncFailure(1)
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				manager.markSyncSuccess(1)
			}
		}()
	}

	wg.Wait()

	require.Contains(t, []time.Duration{syncTimeoutIncremental, syncTimeoutFull}, manager.syncTimeout(1))
}

func TestStreamManager_markSyncSuccess_primes(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	manager.markSyncSuccess(1)

	require.Equal(t, syncTimeoutIncremental, manager.syncTimeout(1), "primed healthy instance uses incremental budget")
}

func TestStreamManager_failureAfterPrimeKeepsFull_thenRecovers(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	manager.markSyncSuccess(1)
	require.Equal(t, syncTimeoutIncremental, manager.syncTimeout(1))

	manager.markSyncFailure(1)
	require.Equal(t, syncTimeoutFull, manager.syncTimeout(1), "failure streak uses full budget")

	manager.markSyncSuccess(1)
	require.Equal(t, syncTimeoutIncremental, manager.syncTimeout(1), "recovered instance returns to incremental budget")
}

func TestStreamManager_unprimedFirstSyncUsesFull(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	manager.markSyncFailure(1)
	require.Equal(t, syncTimeoutFull, manager.syncTimeout(1), "unprimed instance uses full budget")

	manager.markSyncSuccess(1)
	require.Equal(t, syncTimeoutIncremental, manager.syncTimeout(1), "first success drops to incremental budget")
}

func TestBackoffState_IndependentPerInstance(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	manager.markSyncFailure(1)
	manager.markSyncFailure(1)
	require.Equal(t, 4*time.Second, manager.markSyncFailure(2), "instance 2 starts its own streak")

	manager.markSyncSuccess(1)

	require.Equal(t, 8*time.Second, manager.markSyncFailure(2), "a success on instance 1 leaves instance 2's streak")
	require.Equal(t, 4*time.Second, manager.markSyncFailure(1))
}

func TestStreamManager_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	const numGoroutines = 50
	const numIterations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(workerID int) {
			defer wg.Done()

			for j := range numIterations {
				instanceID := (workerID % 5) + 1
				subID := "sub-" + string(rune('A'+workerID)) + "-" + string(rune('0'+j%10))

				sub := withConn(&subscriptionState{
					id:        subID,
					options:   StreamOptions{View: qbittorrent.TorrentListView{InstanceID: instanceID, Limit: 50}},
					created:   time.Now(),
					groupKey:  "group-" + subID,
					clientKey: "client-" + subID,
				})

				manager.mu.Lock()
				manager.subscriptions[sub.id] = sub
				if manager.instanceIndex[instanceID] == nil {
					manager.instanceIndex[instanceID] = make(map[string]*subscriptionState)
				}
				manager.instanceIndex[instanceID][sub.id] = sub
				manager.mu.Unlock()

				manager.Unregister(sub.id)
			}
		}(i)
	}

	wg.Wait()

	manager.mu.RLock()
	subCount := len(manager.subscriptions)
	manager.mu.RUnlock()

	require.Equal(t, 0, subCount, "all subscriptions should be unregistered")
}

func TestStreamManager_ShutdownDuringActiveOperations(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	for i := 1; i <= 3; i++ {
		sub := withConn(&subscriptionState{
			id:        "sub-" + string(rune('0'+i)),
			options:   StreamOptions{View: qbittorrent.TorrentListView{InstanceID: i, Limit: 50}},
			created:   time.Now(),
			groupKey:  "group-" + string(rune('0'+i)),
			clientKey: "client-" + string(rune('0'+i)),
		})

		manager.mu.Lock()
		manager.subscriptions[sub.id] = sub
		if manager.instanceIndex[i] == nil {
			manager.instanceIndex[i] = make(map[string]*subscriptionState)
		}
		manager.instanceIndex[i][sub.id] = sub
		manager.mu.Unlock()
	}

	var wg sync.WaitGroup
	published := make(chan struct{})

	wg.Go(func() {
		for i := range 100 {
			if manager.closing.Load() {
				return
			}
			manager.HandleSyncError(1, errors.New("test error"))
			if i == 0 {
				close(published)
			}
		}
	})

	wg.Go(func() {
		<-published
		_ = manager.Shutdown(context.Background())
	})

	wg.Wait()

	require.True(t, manager.closing.Load(), "manager should be marked as closing")
}

func TestUnregister_MultipleSubscribersInSameGroup(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: 50, Sort: "added_on", Order: "desc"}}
	groupKey := streamOptionsKey(opts)

	sub1 := withConn(&subscriptionState{
		id:        "sub-1",
		options:   opts,
		created:   time.Now(),
		groupKey:  groupKey,
		clientKey: "client-1",
	})
	sub2 := withConn(&subscriptionState{
		id:        "sub-2",
		options:   opts,
		created:   time.Now(),
		groupKey:  groupKey,
		clientKey: "client-2",
	})

	group := &subscriptionGroup{
		key:     groupKey,
		options: opts,
		subs:    make(map[string]*subscriptionState),
	}
	group.subs[sub1.id] = sub1
	group.subs[sub2.id] = sub2

	manager.mu.Lock()
	manager.subscriptions[sub1.id] = sub1
	manager.subscriptions[sub2.id] = sub2
	manager.instanceIndex[opts.View.InstanceID] = map[string]*subscriptionState{
		sub1.id: sub1,
		sub2.id: sub2,
	}
	manager.groups[groupKey] = group
	manager.instanceGroups[opts.View.InstanceID] = map[string]*subscriptionGroup{
		groupKey: group,
	}
	manager.mu.Unlock()

	manager.Unregister(sub1.id)

	manager.mu.RLock()
	_, sub1Exists := manager.subscriptions[sub1.id]
	_, sub2Exists := manager.subscriptions[sub2.id]
	groupStillExists := manager.groups[groupKey] != nil
	instanceIndexExists := manager.instanceIndex[opts.View.InstanceID] != nil
	instanceGroupsExist := manager.instanceGroups[opts.View.InstanceID] != nil
	manager.mu.RUnlock()

	require.False(t, sub1Exists, "sub1 should be removed from subscriptions")
	require.True(t, sub2Exists, "sub2 should still exist in subscriptions")
	require.True(t, groupStillExists, "group should still exist with remaining subscriber")
	require.True(t, instanceIndexExists, "instance index should still exist")
	require.True(t, instanceGroupsExist, "instance groups should still exist")

	group.subsMu.RLock()
	_, sub1InGroup := group.subs[sub1.id]
	_, sub2InGroup := group.subs[sub2.id]
	groupSubCount := len(group.subs)
	group.subsMu.RUnlock()

	require.False(t, sub1InGroup, "sub1 should be removed from group")
	require.True(t, sub2InGroup, "sub2 should still be in group")
	require.Equal(t, 1, groupSubCount, "group should have exactly 1 subscriber")

	manager.Unregister(sub2.id)

	manager.mu.RLock()
	_, sub2StillExists := manager.subscriptions[sub2.id]
	groupGone := manager.groups[groupKey] == nil
	instanceIndexGone := manager.instanceIndex[opts.View.InstanceID] == nil
	instanceGroupsGone := manager.instanceGroups[opts.View.InstanceID] == nil
	manager.mu.RUnlock()

	require.False(t, sub2StillExists, "sub2 should be removed")
	require.True(t, groupGone, "group should be cleaned up when empty")
	require.True(t, instanceIndexGone, "instance index should be cleaned up")
	require.True(t, instanceGroupsGone, "instance groups should be cleaned up")
}

func TestHandleSyncError_NilError(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)
	provider := newRecordingProvider()
	manager.server.Provider = provider

	sub := withConn(&subscriptionState{
		id:        "sub-nil-err",
		options:   StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}},
		created:   time.Now(),
		groupKey:  "group-nil-err",
		clientKey: "client-nil-err",
	})

	manager.mu.Lock()
	manager.subscriptions[sub.id] = sub
	manager.instanceIndex[sub.options.View.InstanceID] = map[string]*subscriptionState{
		sub.id: sub,
	}
	manager.mu.Unlock()

	manager.HandleSyncError(sub.options.View.InstanceID, nil)

	messages := provider.allMessages()
	require.Empty(t, messages, "nil error should not produce any messages")
}

func TestIsContextStopped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "canceled sentinel", err: context.Canceled, want: true},
		{name: "deadline sentinel", err: context.DeadlineExceeded, want: true},
		{name: "wrapped canceled text", err: errors.New("All attempts fail: context canceled"), want: true},
		{name: "wrapped deadline text", err: errors.New("All attempts fail: context deadline exceeded"), want: true},
		{name: "real error", err: errors.New("connection refused"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isContextStopped(tt.err))
		})
	}
}

func TestParseStreamRequests_EmptyStreamsParam(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream", nil)
	_, err := parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing streams parameter")
}

func TestParseStreamRequests_MalformedJSON(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams=not-json", nil)
	_, err := parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid streams payload")
}

func TestParseStreamRequests_EmptyArray(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams=[]", nil)
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

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
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
			"limit":      3000,
			"sort":       "added_on",
			"order":      "desc",
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
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

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	_, err = parseStreamRequests(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid page")
}

func TestParseStreamRequests_DefaultsApplied(t *testing.T) {
	payload := []map[string]any{
		{
			"instanceId": 1,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
	requests, err := parseStreamRequests(req)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	opts := requests[0].options
	require.Equal(t, 1, opts.View.InstanceID)
	require.Equal(t, 0, opts.View.Offset, "page should default to 0")
	require.Equal(t, defaultLimit, opts.View.Limit, "limit should default to 300")
	require.Equal(t, "added_on", opts.View.Sort, "sort should default to added_on")
	require.Equal(t, "desc", opts.View.Order, "order should default to desc")
}

func TestParseStreamRequests_OrderNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ASC", "asc"},
		{"DESC", "desc"},
		{"invalid", "desc"},
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

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
			requests, err := parseStreamRequests(req)
			require.NoError(t, err)
			require.Len(t, requests, 1)
			require.Equal(t, tt.expected, requests[0].options.View.Order)
		})
	}
}

func TestRegisterSubscription_DuringShutdown(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	err := manager.Shutdown(context.Background())
	require.NoError(t, err)

	_, err = manager.registerSubscription(StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: 50}}, "test-key", manager.openConnection(false, 0), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutting down")
}

func TestShutdown_WithNilContext(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	var shutdownCtx context.Context
	err := manager.Shutdown(shutdownCtx)
	require.NoError(t, err)
	require.True(t, manager.closing.Load())
}

func TestShutdown_Idempotent(t *testing.T) {
	manager := NewStreamManager(nil, nil, nil, nil)

	err := manager.Shutdown(context.Background())
	require.NoError(t, err)

	err = manager.Shutdown(context.Background())
	require.NoError(t, err)
	require.True(t, manager.closing.Load())
}

func TestParseStreamRequests_Summary(t *testing.T) {
	parse := func(entry map[string]any) ([]streamRequest, error) {
		raw, err := json.Marshal([]map[string]any{entry})
		require.NoError(t, err)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
		return parseStreamRequests(req)
	}

	requests, err := parse(map[string]any{"instanceId": 1, "summary": true})
	require.NoError(t, err)
	require.True(t, requests[0].options.View.NoRows)

	rows, err := parse(map[string]any{"instanceId": 1})
	require.NoError(t, err)
	require.NotEqual(t, streamOptionsKey(rows[0].options), streamOptionsKey(requests[0].options),
		"a summary stream must not share a group with a row stream")

	_, err = parse(map[string]any{"instanceIds": []int{1, 2}, "summary": true})
	require.Error(t, err, "a cross-instance view has no summary")
}

func withConn(sub *subscriptionState) *subscriptionState {
	sub.conn = &streamConnection{id: sub.id, subs: map[string]*subscriptionState{sub.id: sub}}
	return sub
}
