// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

// setupTestPool creates a new ClientPool for testing
func setupTestPool(t *testing.T) *ClientPool {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "qui-pool-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize test database
	db, err := database.New(dbPath)
	require.NoError(t, err, "Failed to initialize test database")
	t.Cleanup(func() { db.Close() })

	// Use test encryption key
	testKey := make([]byte, 32)

	instanceStore, err := models.NewInstanceStore(db, testKey)
	require.NoError(t, err, "Failed to create instance store")

	errorStore := models.NewInstanceErrorStore(db)
	pool, err := NewClientPool(instanceStore, errorStore)
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
func (m *mockPoolSyncEventSink) HandleSyncError(_ int, _ error)        {}
