// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// setupTestPool creates a new ClientPool for testing
func setupTestPool(t *testing.T) *ClientPool {
	db := testdb.NewMigratedSQLite(t, "qbittorrent-pool")

	// Use test encryption key
	testKey := make([]byte, 32)

	instanceStore, err := models.NewInstanceStore(db, testKey)
	require.NoError(t, err, "Failed to create instance store")

	errorStore := models.NewInstanceErrorStore(db)
	pool, err := NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err, "Failed to create client pool")
	return pool
}

func TestClientPool_ResetFailureTracking(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	instanceID := 1
	banError := errors.New("User's IP is banned for too many failed login attempts")

	// Track multiple failures
	pool.trackFailure(instanceID, banError)
	pool.trackFailure(instanceID, banError)

	// Should be in backoff
	assert.True(t, pool.isInBackoff(instanceID), "Instance should be in backoff after failures")

	// Reset failure tracking
	pool.ResetFailureTracking(instanceID)

	// Should no longer be in backoff
	assert.False(t, pool.isInBackoff(instanceID), "Instance should not be in backoff after reset")

	// Failure info should be cleared
	pool.mu.RLock()
	_, exists := pool.failureTracker[instanceID]
	pool.mu.RUnlock()

	assert.False(t, exists, "Failure info should be cleared after reset")
}

// TestClientPool_GetClientWithTimeout_UnhealthyInBackoffFastFails verifies that an
// existing but unhealthy client already in backoff fast-fails instead of running a
// live HealthCheck that would block on the unreachable host every call (discussion #2096).
func TestClientPool_GetClientWithTimeout_UnhealthyInBackoffFastFails(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1

	// Existing but unhealthy client (zero-value isHealthy == false).
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{instanceID: instanceID}
	pool.mu.Unlock()

	// Drive the instance into failure backoff.
	pool.trackFailure(instanceID, errors.New("connection refused"))
	require.True(t, pool.isInBackoff(instanceID), "instance should be in backoff")

	start := time.Now()
	client, err := pool.GetClientWithTimeout(context.Background(), instanceID, 60*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, client)
	require.ErrorIs(t, err, ErrInstanceInBackoff)
	assert.Contains(t, err.Error(), "backoff period", "backed-off unhealthy client should return the backoff error, not attempt a health check")
	message, ok := InstanceHealthBlockerMessage(err)
	require.True(t, ok, "backoff error should expose an actionable boundary message")
	assert.Contains(t, message, "health-check backoff")
	assert.Contains(t, message, "retrying in")
	assert.Less(t, elapsed, time.Second, "backoff fast-path must not perform a network health check")
}

// TestClientPool_GetClientWithTimeout_ConcurrentUnhealthyProbesBackoffOnce verifies that
// a burst of concurrent callers against one unhealthy, not-yet-backed-off instance records
// a single failure (advances backoff once), not once per caller (adversarial review of #2096).
// The instance is dead (connection refused): a hard failure, so it must back off.
func TestClientPool_GetClientWithTimeout_ConcurrentUnhealthyProbesBackoffOnce(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	// Port 1 is outside the ephemeral range: during the retries, a closed httptest port can go to another test's server.
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: "http://127.0.0.1:1", Timeout: 60}), instanceID: instanceID}
	pool.mu.Unlock()

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = pool.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
		})
	}
	wg.Wait()

	pool.mu.RLock()
	info := pool.failureTracker[instanceID]
	pool.mu.RUnlock()

	require.NotNil(t, info, "one failure should have been recorded")
	assert.Equal(t, 1, info.attempts, "concurrent probes must advance the backoff exactly once, not once per caller")
	assert.True(t, pool.isInBackoff(instanceID), "a dead instance must go into backoff (#2096)")
}

// TestClientPool_GetClientWithTimeout_SlowProbeDoesNotBackoff verifies that a probe that
// times out against a responding-but-saturated instance does NOT record a failure or back
// the instance off: slow is not down, and backing off a working instance turns one slow
// request into an outage for every caller.
func TestClientPool_GetClientWithTimeout_SlowProbeDoesNotBackoff(t *testing.T) {
	// Blocking endpoint: the connection is accepted but the response never comes,
	// so the probe fails with a deadline, the signature of a saturated instance.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: instanceID}
	pool.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client, err := pool.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
	require.Error(t, err)
	require.Nil(t, client)

	assert.False(t, pool.isInBackoff(instanceID), "a timed-out probe must not put the instance in backoff")
	pool.mu.RLock()
	_, tracked := pool.failureTracker[instanceID]
	pool.mu.RUnlock()
	assert.False(t, tracked, "a timed-out probe must not record an instance failure")
}

// TestClientPool_GetClientWithTimeout_CancelledProbeDoesNotBackoff verifies that a probe
// interrupted by caller cancellation (client disconnect / shutdown) does NOT record a
// failure or back off the instance, so a healthy instance is not stranded (review of #2096).
func TestClientPool_GetClientWithTimeout_CancelledProbeDoesNotBackoff(t *testing.T) {
	arrived := make(chan struct{})
	closeArrived := sync.OnceFunc(func() { close(arrived) })
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		closeArrived()
		<-r.Context().Done()
	}))
	defer srv.Close()

	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: instanceID}
	pool.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-arrived
		cancel()
	}()

	_, err := pool.GetClientWithTimeout(ctx, instanceID, 60*time.Second)
	require.Error(t, err)

	assert.False(t, pool.isInBackoff(instanceID), "a cancelled probe must not put the instance in backoff")
	pool.mu.RLock()
	_, tracked := pool.failureTracker[instanceID]
	pool.mu.RUnlock()
	assert.False(t, tracked, "a cancelled probe must not record an instance failure")
}

// TestClientPool_GetClientWithTimeout_ProbeInProgressFastFails verifies that a caller does
// not block behind an in-flight probe (TryLock): with a probe holding the per-instance lock,
// a second caller returns promptly instead of queueing until the probe completes (#2096).
func TestClientPool_GetClientWithTimeout_ProbeInProgressFastFails(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{instanceID: instanceID} // unhealthy; never probed (lock is held)
	pool.mu.Unlock()

	// Simulate an in-flight probe by holding the per-instance lock for the test.
	lock := pool.getInstanceLock(instanceID)
	lock.Lock()
	defer lock.Unlock()

	start := time.Now()
	client, err := pool.GetClientWithTimeout(context.Background(), instanceID, 60*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Nil(t, client)
	require.ErrorIs(t, err, ErrHealthCheckInProgress)
	message, ok := InstanceHealthBlockerMessage(err)
	require.True(t, ok, "in-flight probe error should expose an actionable boundary message")
	assert.Contains(t, message, "already running a health check")
	assert.Contains(t, message, "retry shortly")
	assert.Less(t, elapsed, time.Second, "caller must fast-fail rather than block behind an in-flight probe")
}

func TestClientPool_IsBanError(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "IP banned error",
			err:      errors.New("User's IP is banned for too many failed login attempts"),
			expected: true,
		},
		{
			name:     "Rate limit error",
			err:      errors.New("Rate limit exceeded"),
			expected: true,
		},
		{
			name:     "HTTP 403 error",
			err:      errors.New("HTTP 403 Forbidden"),
			expected: true,
		},
		{
			name:     "Connection refused",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "Timeout error",
			err:      errors.New("context deadline exceeded"),
			expected: false,
		},
		{
			name:     "Mixed case banned error",
			err:      errors.New("IP IS BANNED"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pool.isBanError(tt.err)
			assert.Equal(t, tt.expected, result, "Ban error detection mismatch for error: %v", tt.err)
		})
	}
}

func TestClientPoolDecryptFieldReturnsActionableDecryptionError(t *testing.T) {
	pool := &ClientPool{decryptionTracker: make(map[int]*decryptionErrorInfo)}

	_, err := pool.decryptField(1, "test", "password", func() (string, error) {
		return "", errors.New("cipher: message authentication failed")
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt password")
	assert.Contains(t, err.Error(), "instance will be unavailable until password is re-entered via web UI")
	assert.Contains(t, err.Error(), "cipher: message authentication failed")
}

func TestClientPoolDecryptFieldKeepsGenericErrorConcise(t *testing.T) {
	pool := &ClientPool{decryptionTracker: make(map[int]*decryptionErrorInfo)}

	_, err := pool.decryptField(1, "test", "password", func() (string, error) {
		return "", errors.New("storage unavailable")
	})

	require.Error(t, err)
	assert.EqualError(t, err, "failed to decrypt password: storage unavailable")
}

func TestClientPoolSyncEventSinksEachGetEveryUpdate(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	client1 := &Client{instanceID: 1}
	client2 := &Client{instanceID: 2}
	pool.mu.Lock()
	pool.clients[1] = client1
	pool.clients[2] = client2
	pool.mu.Unlock()

	first := &recordingSyncEventSink{}
	second := &recordingSyncEventSink{}
	pool.AddSyncEventSink(first)
	pool.AddSyncEventSink(second)

	data := &qbt.MainData{Rid: 7}
	syncErr := errors.New("sync failed")
	client1.dispatchMainData(data)
	client2.dispatchSyncError(syncErr)
	sm.notifyTrackerHealthUpdated(2)

	want := []string{"maindata:1", "error:2:sync failed", "health:2"}
	assert.Equal(t, want, first.events, "first consumer should get every update")
	assert.Equal(t, want, second.events, "second consumer should get every update")
}

func TestClientPoolSetSyncManagerReceivesExistingSinks(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	first := &recordingSyncEventSink{}
	second := &recordingSyncEventSink{}
	pool.AddSyncEventSink(first)
	pool.AddSyncEventSink(second)

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)
	sm.notifyTrackerHealthUpdated(3)

	assert.Equal(t, []string{"health:3"}, first.events)
	assert.Equal(t, []string{"health:3"}, second.events)
}

func TestClientPoolSetSyncManagerSkipsStaleSinkReplay(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	first := &recordingSyncEventSink{}
	second := &recordingSyncEventSink{}
	pool.AddSyncEventSink(first)

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	pool.mu.RLock()
	staleSinks := pool.syncEventSinks
	staleSeq := pool.syncEventSinkSeq
	pool.mu.RUnlock()

	pool.AddSyncEventSink(second)
	pool.applySyncManagerSinkIfCurrent(sm, staleSinks, staleSeq)
	sm.notifyTrackerHealthUpdated(1)

	assert.Equal(t, []string{"health:1"}, second.events, "stale captured sinks replay should not drop a newer consumer")
}

func TestSyncEventSinksCallConsumersInRegistrationOrder(t *testing.T) {
	var calls []string
	sinks := syncEventSinks{
		&orderSyncEventSink{name: "first", calls: &calls},
		&orderSyncEventSink{name: "second", calls: &calls},
	}

	sinks.HandleMainData(1, &qbt.MainData{})
	sinks.HandleSyncError(1, errors.New("sync failed"))
	sinks.HandleTrackerHealthUpdated(1)

	assert.Equal(t, []string{"first", "second", "first", "second", "first", "second"}, calls)
}

type orderSyncEventSink struct {
	name  string
	calls *[]string
}

func (o *orderSyncEventSink) HandleMainData(int, *qbt.MainData) { *o.calls = append(*o.calls, o.name) }
func (o *orderSyncEventSink) HandleTrackerHealthUpdated(int)    { *o.calls = append(*o.calls, o.name) }
func (o *orderSyncEventSink) HandleSyncError(int, error)        { *o.calls = append(*o.calls, o.name) }

// recordingSyncEventSink records each update in the order it arrives.
type recordingSyncEventSink struct {
	events []string
}

func (r *recordingSyncEventSink) HandleMainData(instanceID int, _ *qbt.MainData) {
	r.events = append(r.events, fmt.Sprintf("maindata:%d", instanceID))
}

func (r *recordingSyncEventSink) HandleTrackerHealthUpdated(instanceID int) {
	r.events = append(r.events, fmt.Sprintf("health:%d", instanceID))
}

func (r *recordingSyncEventSink) HandleSyncError(instanceID int, err error) {
	r.events = append(r.events, fmt.Sprintf("error:%d:%v", instanceID, err))
}

// TestClientPool_CreateDoubleCheckReturnsExistingUnhealthyClient guards the
// create-path double-check against re-creating a client another caller just
// stored: creation can legitimately succeed with a not-yet-verified
// (unhealthy) client, and callers queued on the instance lock must reuse it
// instead of serially re-logging in and overwriting the pool entry.
func TestClientPool_CreateDoubleCheckReturnsExistingUnhealthyClient(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1

	existing := &Client{instanceID: instanceID} // zero-value isHealthy == false
	pool.mu.Lock()
	pool.clients[instanceID] = existing
	pool.mu.Unlock()

	client, err := pool.createClientWithTimeout(context.Background(), instanceID, time.Second)

	require.NoError(t, err, "double-check must return the pooled client, not attempt a re-create")
	require.Same(t, existing, client)
}
