// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
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

/*
	func TestClientPool_BackoffLogic(t *testing.T) {
		pool := setupTestPool(t)
		defer pool.Close()

		instanceID := 1

		tests := []struct {
			name           string
			err            error
			expectedBanned bool
			minBackoff     time.Duration
			maxBackoff     time.Duration
		}{
			{
				name:           "IP ban error triggers long backoff",
				err:            errors.New("User's IP is banned for too many failed login attempts"),
				expectedBanned: true,
				minBackoff:     4 * time.Minute,
				maxBackoff:     6 * time.Minute,
			},
			{
				name:           "Rate limit error triggers long backoff",
				err:            errors.New("Rate limit exceeded"),
				expectedBanned: true,
				minBackoff:     4 * time.Minute,
				maxBackoff:     6 * time.Minute,
			},
			{
				name:           "403 forbidden triggers long backoff",
				err:            errors.New("HTTP 403 Forbidden"),
				expectedBanned: true,
				minBackoff:     4 * time.Minute,
				maxBackoff:     6 * time.Minute,
			},
			{
				name:           "Generic connection error triggers short backoff",
				err:            errors.New("connection refused"),
				expectedBanned: false,
				minBackoff:     25 * time.Second,
				maxBackoff:     35 * time.Second,
			},
			{
				name:           "Timeout error triggers short backoff",
				err:            errors.New("context deadline exceeded"),
				expectedBanned: false,
				minBackoff:     25 * time.Second,
				maxBackoff:     35 * time.Second,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Reset failure tracking
				pool.ResetFailureTracking(instanceID)

				// Should not be in backoff initially
				assert.False(t, pool.isInBackoff(instanceID), "Instance should not be in backoff initially")

				// Track failure
				pool.trackFailure(instanceID, tt.err)

				// Should now be in backoff
				assert.True(t, pool.isInBackoff(instanceID), "Instance should be in backoff after failure")

				// Check failure info
				pool.mu.RLock()
				info, exists := pool.failureTracker[instanceID]
				pool.mu.RUnlock()

				require.True(t, exists, "Failure info should exist")

				// Check if this is a ban error (we can't directly check isBanned field anymore)
				isBanError := pool.isBanError(tt.err)
				assert.Equal(t, tt.expectedBanned, isBanError, "Ban error classification mismatch")

				// Check backoff duration is in expected range
				backoffDuration := time.Until(info.nextRetry)
				assert.Truef(t, backoffDuration >= tt.minBackoff && backoffDuration <= tt.maxBackoff,
					"Backoff duration %v not in range [%v, %v]", backoffDuration, tt.minBackoff, tt.maxBackoff)
			})
		}
	}

	func TestClientPool_BackoffEscalation(t *testing.T) {
		pool := setupTestPool(t)
		defer pool.Close()

		instanceID := 1
		banError := errors.New("User's IP is banned for too many failed login attempts")

		// Test exponential backoff escalation for ban errors
		expectedMinutes := []int{5, 10, 20, 40, 60, 60} // Max at 1 hour

		for i, expectedMin := range expectedMinutes {
			t.Run(fmt.Sprintf("failure_%d", i+1), func(t *testing.T) {
				pool.trackFailure(instanceID, banError)

				pool.mu.RLock()
				info, exists := pool.failureTracker[instanceID]
				pool.mu.RUnlock()

				require.True(t, exists, "Failure info should exist")

				assert.Equal(t, i+1, info.attempts, "Attempt count mismatch")

				backoffDuration := time.Until(info.nextRetry)
				minExpected := time.Duration(expectedMin-1) * time.Minute
				maxExpected := time.Duration(expectedMin+1) * time.Minute

				assert.Truef(t, backoffDuration >= minExpected && backoffDuration <= maxExpected,
					"Failure %d: backoff duration %v not in range [%v, %v]", i+1, backoffDuration, minExpected, maxExpected)
			})
		}
	}
*/
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
	// Closed listener: probes fail fast with a hard connection error.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	pool := setupTestPool(t)
	defer pool.Close()

	const instanceID = 1
	pool.mu.Lock()
	pool.clients[instanceID] = &Client{Client: qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60}), instanceID: instanceID}
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
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
			name:     "Simple banned error",
			err:      errors.New("IP is banned"),
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

func TestClientPoolSetSyncEventSinkUpdatesExistingClients(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	// Create clients manually and add them to the pool
	// These clients don't have a sink yet
	client1 := &Client{instanceID: 1}
	client2 := &Client{instanceID: 2}

	pool.mu.Lock()
	pool.clients[1] = client1
	pool.clients[2] = client2
	pool.mu.Unlock()

	// Verify clients have no sink initially
	assert.Nil(t, client1.getSyncEventSink(), "client1 should have no sink initially")
	assert.Nil(t, client2.getSyncEventSink(), "client2 should have no sink initially")

	// Create a mock sink
	sink := &mockPoolSyncEventSink{}

	// Set the sink on the pool
	pool.SetSyncEventSink(sink)

	// Verify all existing clients were updated with the sink
	assert.Equal(t, sink, client1.getSyncEventSink(), "client1 should have the sink after SetSyncEventSink")
	assert.Equal(t, sink, client2.getSyncEventSink(), "client2 should have the sink after SetSyncEventSink")

	// Verify the pool itself stored the sink
	pool.mu.RLock()
	poolSink := pool.syncEventSink
	pool.mu.RUnlock()
	assert.Equal(t, sink, poolSink, "pool should have stored the sink")
}

func TestClientPoolSetSyncEventSinkWithNoClients(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	// Verify pool starts with no clients
	pool.mu.RLock()
	clientCount := len(pool.clients)
	pool.mu.RUnlock()
	assert.Equal(t, 0, clientCount, "pool should start with no clients")

	// Setting sink should not panic when there are no clients
	sink := &mockPoolSyncEventSink{}
	pool.SetSyncEventSink(sink)

	// Verify the pool stored the sink
	pool.mu.RLock()
	poolSink := pool.syncEventSink
	pool.mu.RUnlock()
	assert.Equal(t, sink, poolSink, "pool should have stored the sink even with no clients")
}

func TestClientPoolSetSyncEventSinkUpdatesSyncManager(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	sink := &mockPoolSyncEventSink{}
	pool.SetSyncEventSink(sink)

	assert.Equal(t, sink, sm.getSyncEventSink(), "sync manager should receive pool sink")
}

func TestClientPoolSetSyncManagerReceivesExistingSink(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	sink := &mockPoolSyncEventSink{}
	pool.SetSyncEventSink(sink)

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	assert.Equal(t, sink, sm.getSyncEventSink(), "new sync manager should receive existing pool sink")
}

func TestClientPoolSetSyncManagerSkipsStaleSinkReplay(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	oldSink := &mockPoolSyncEventSink{id: 1}
	newSink := &mockPoolSyncEventSink{id: 2}
	pool.SetSyncEventSink(oldSink)

	sm := NewSyncManager(nil, nil)
	pool.SetSyncManager(sm)

	pool.mu.RLock()
	staleSeq := pool.syncEventSinkSeq
	pool.mu.RUnlock()

	pool.SetSyncEventSink(newSink)
	require.Equal(t, newSink, sm.getSyncEventSink(), "sync manager should receive newer pool sink")

	pool.applySyncManagerSinkIfCurrent(sm, oldSink, staleSeq)

	assert.Equal(t, newSink, sm.getSyncEventSink(), "stale captured sink replay should not overwrite newer sink")
}

func TestClientPoolSetSyncEventSinkReplacesExisting(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	// Create a client and add to pool
	client := &Client{instanceID: 1}
	pool.mu.Lock()
	pool.clients[1] = client
	pool.mu.Unlock()

	// Set first sink
	sink1 := &mockPoolSyncEventSink{id: 1}
	pool.SetSyncEventSink(sink1)
	assert.Equal(t, sink1, client.getSyncEventSink(), "client should have sink1")

	// Set second sink (replaces first)
	sink2 := &mockPoolSyncEventSink{id: 2}
	pool.SetSyncEventSink(sink2)
	assert.Equal(t, sink2, client.getSyncEventSink(), "client should have sink2 after replacement")
}

// mockPoolSyncEventSink is a simple mock for testing pool sink propagation.
type mockPoolSyncEventSink struct {
	id int
}

func (m *mockPoolSyncEventSink) HandleMainData(_ int, _ *qbt.MainData) {}
func (m *mockPoolSyncEventSink) HandleTrackerHealthUpdated(_ int)      {}
func (m *mockPoolSyncEventSink) HandleSyncError(_ int, _ error)        {}

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
