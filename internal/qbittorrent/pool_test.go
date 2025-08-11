// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package qbittorrent

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// setupTestPool creates a new ClientPool for testing
func setupTestPool(t *testing.T) *ClientPool {
	instanceStore := &models.InstanceStore{}
	pool, err := NewClientPool(instanceStore)
	require.NoError(t, err, "Failed to create client pool")
	return pool
}

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
			pool.resetFailureTracking(instanceID)

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
	pool.resetFailureTracking(instanceID)

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

func TestClientPool_GetBackoffStatus(t *testing.T) {
	pool := setupTestPool(t)
	defer pool.Close()

	instanceID := 1
	
	// Initially no backoff
	inBackoff, nextRetry, attempts := pool.GetBackoffStatus(instanceID)
	assert.False(t, inBackoff, "Initially should not be in backoff")
	assert.True(t, nextRetry.IsZero(), "Initially nextRetry should be zero time")
	assert.Equal(t, 0, attempts, "Initially should have zero attempts")
	
	// Track a ban error
	banError := errors.New("User's IP is banned for too many failed login attempts")
	pool.trackFailure(instanceID, banError)
	
	// Should now have backoff status
	inBackoff, nextRetry, attempts = pool.GetBackoffStatus(instanceID)
	assert.True(t, inBackoff, "After ban error should be in backoff")
	assert.False(t, nextRetry.IsZero(), "After ban error nextRetry should not be zero")
	assert.Equal(t, 1, attempts, "After ban error should have 1 attempt")
	
	// Reset tracking
	pool.resetFailureTracking(instanceID)
	
	// Should be back to no backoff
	inBackoff, nextRetry, attempts = pool.GetBackoffStatus(instanceID)
	assert.False(t, inBackoff, "After reset should not be in backoff")
	assert.True(t, nextRetry.IsZero(), "After reset nextRetry should be zero time")
	assert.Equal(t, 0, attempts, "After reset should have zero attempts")
}