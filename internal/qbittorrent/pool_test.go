// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package qbittorrent

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
)

func TestClientPool_BackoffLogic(t *testing.T) {
	// Create a mock instance store (we only need it to not panic)
	instanceStore := &models.InstanceStore{}
	
	pool, err := NewClientPool(instanceStore)
	if err != nil {
		t.Fatalf("Failed to create client pool: %v", err)
	}
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
			if pool.isInBackoff(instanceID) {
				t.Error("Instance should not be in backoff initially")
			}

			// Track failure
			pool.trackFailure(instanceID, tt.err)

			// Should now be in backoff
			if !pool.isInBackoff(instanceID) {
				t.Error("Instance should be in backoff after failure")
			}

			// Check failure info
			pool.mu.RLock()
			info, exists := pool.failureTracker[instanceID]
			pool.mu.RUnlock()

			if !exists {
				t.Fatal("Failure info should exist")
			}

			// Check if this is a ban error (we can't directly check isBanned field anymore)
			isBanError := pool.isBanError(tt.err)
			if isBanError != tt.expectedBanned {
				t.Errorf("Expected ban error=%v, got %v", tt.expectedBanned, isBanError)
			}

			// Check backoff duration is in expected range
			backoffDuration := time.Until(info.nextRetry)
			if backoffDuration < tt.minBackoff || backoffDuration > tt.maxBackoff {
				t.Errorf("Backoff duration %v not in range [%v, %v]", backoffDuration, tt.minBackoff, tt.maxBackoff)
			}
		})
	}
}

func TestClientPool_BackoffEscalation(t *testing.T) {
	instanceStore := &models.InstanceStore{}
	
	pool, err := NewClientPool(instanceStore)
	if err != nil {
		t.Fatalf("Failed to create client pool: %v", err)
	}
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

			if !exists {
				t.Fatal("Failure info should exist")
			}

			if info.attempts != i+1 {
				t.Errorf("Expected attempts=%d, got %d", i+1, info.attempts)
			}

			backoffDuration := time.Until(info.nextRetry)
			minExpected := time.Duration(expectedMin-1) * time.Minute
			maxExpected := time.Duration(expectedMin+1) * time.Minute

			if backoffDuration < minExpected || backoffDuration > maxExpected {
				t.Errorf("Failure %d: backoff duration %v not in range [%v, %v]", 
					i+1, backoffDuration, minExpected, maxExpected)
			}
		})
	}
}

func TestClientPool_ResetFailureTracking(t *testing.T) {
	instanceStore := &models.InstanceStore{}
	
	pool, err := NewClientPool(instanceStore)
	if err != nil {
		t.Fatalf("Failed to create client pool: %v", err)
	}
	defer pool.Close()

	instanceID := 1
	banError := errors.New("User's IP is banned for too many failed login attempts")

	// Track multiple failures
	pool.trackFailure(instanceID, banError)
	pool.trackFailure(instanceID, banError)

	// Should be in backoff
	if !pool.isInBackoff(instanceID) {
		t.Error("Instance should be in backoff after failures")
	}

	// Reset failure tracking
	pool.resetFailureTracking(instanceID)

	// Should no longer be in backoff
	if pool.isInBackoff(instanceID) {
		t.Error("Instance should not be in backoff after reset")
	}

	// Failure info should be cleared
	pool.mu.RLock()
	_, exists := pool.failureTracker[instanceID]
	pool.mu.RUnlock()

	if exists {
		t.Error("Failure info should be cleared after reset")
	}
}

func TestClientPool_IsBanError(t *testing.T) {
	instanceStore := &models.InstanceStore{}
	
	pool, err := NewClientPool(instanceStore)
	if err != nil {
		t.Fatalf("Failed to create client pool: %v", err)
	}
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
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for error: %v", tt.expected, result, tt.err)
			}
		})
	}
}

func TestClientPool_GetBackoffStatus(t *testing.T) {
	instanceStore := &models.InstanceStore{}
	
	pool, err := NewClientPool(instanceStore)
	if err != nil {
		t.Fatalf("Failed to create client pool: %v", err)
	}
	defer pool.Close()

	instanceID := 1
	
	// Initially no backoff
	inBackoff, nextRetry, attempts := pool.GetBackoffStatus(instanceID)
	if inBackoff || !nextRetry.IsZero() || attempts != 0 {
		t.Error("Initially should have no backoff status")
	}
	
	// Track a ban error
	banError := errors.New("User's IP is banned for too many failed login attempts")
	pool.trackFailure(instanceID, banError)
	
	// Should now have backoff status
	inBackoff, nextRetry, attempts = pool.GetBackoffStatus(instanceID)
	if !inBackoff || nextRetry.IsZero() || attempts != 1 {
		t.Errorf("After ban error: inBackoff=%v, nextRetry=%v, attempts=%d", 
			inBackoff, nextRetry, attempts)
	}
	
	// Reset tracking
	pool.resetFailureTracking(instanceID)
	
	// Should be back to no backoff
	inBackoff, nextRetry, attempts = pool.GetBackoffStatus(instanceID)
	if inBackoff || !nextRetry.IsZero() || attempts != 0 {
		t.Error("After reset should have no backoff status")
	}
}