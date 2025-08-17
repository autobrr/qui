// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

var (
	ErrClientNotFound = errors.New("qBittorrent client not found")
	ErrPoolClosed     = errors.New("client pool is closed")
)

// Backoff constants
const (
	healthCheckInterval    = 30 * time.Second
	healthCheckTimeout     = 10 * time.Second
	minHealthCheckInterval = 20 * time.Second

	// Normal failure backoff durations
	initialBackoff = 30 * time.Second
	maxBackoff     = 10 * time.Minute

	// Ban-related backoff durations
	banInitialBackoff = 5 * time.Minute
	banMaxBackoff     = 1 * time.Hour
)

// failureInfo tracks failure state and backoff for an instance
type failureInfo struct {
	nextRetry time.Time
	attempts  int
}

// ClientPool manages multiple qBittorrent client connections
type ClientPool struct {
	clients        map[int]*Client
	instanceStore  *models.InstanceStore
	cache          *ristretto.Cache
	mu             sync.RWMutex
	dbMu           sync.Mutex // Serialize database updates
	closed         bool
	healthTicker   *time.Ticker
	stopHealth     chan struct{}
	failureTracker map[int]*failureInfo // Track failure state per instance
}

// NewClientPool creates a new client pool
func NewClientPool(instanceStore *models.InstanceStore) (*ClientPool, error) {
	// Create high-performance cache
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,     // 10 million
		MaxCost:     1 << 30, // 1GB
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	cp := &ClientPool{
		clients:        make(map[int]*Client),
		instanceStore:  instanceStore,
		cache:          cache,
		healthTicker:   time.NewTicker(healthCheckInterval),
		stopHealth:     make(chan struct{}),
		failureTracker: make(map[int]*failureInfo),
	}

	// Start health check routine
	go cp.healthCheckLoop()

	return cp, nil
}

// GetClient returns a qBittorrent client for the given instance ID
func (cp *ClientPool) GetClient(ctx context.Context, instanceID int) (*Client, error) {
	cp.mu.RLock()
	if cp.closed {
		cp.mu.RUnlock()
		return nil, ErrPoolClosed
	}

	client, exists := cp.clients[instanceID]
	cp.mu.RUnlock()

	if exists && client.IsHealthy() {
		return client, nil
	}

	// Need to create or recreate the client
	return cp.createClient(ctx, instanceID)
}

// createClient creates a new client connection
func (cp *ClientPool) createClient(ctx context.Context, instanceID int) (*Client, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Check if instance is in backoff period
	if cp.isInBackoffLocked(instanceID) {
		return nil, fmt.Errorf("instance %d is in backoff period, will retry later", instanceID)
	}

	// Double-check after acquiring write lock
	if client, exists := cp.clients[instanceID]; exists && client.IsHealthy() {
		return client, nil
	}

	// Get instance details
	instance, err := cp.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	// Decrypt password
	password, err := cp.instanceStore.GetDecryptedPassword(instance)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	// Decrypt basic auth password if present
	var basicPassword *string
	if instance.BasicPasswordEncrypted != nil {
		basicPassword, err = cp.instanceStore.GetDecryptedBasicPassword(instance)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt basic auth password: %w", err)
		}
	}

	// Create new client
	client, err := NewClient(instanceID, instance.Host, instance.Username, password, instance.BasicUsername, basicPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Store in pool
	cp.clients[instanceID] = client

	// Reset failure tracking on successful connection
	cp.resetFailureTrackingLocked(instanceID)

	// Update last connected timestamp
	cp.dbMu.Lock()
	if err := cp.instanceStore.UpdateLastConnected(ctx, instanceID); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update last connected timestamp")
	}
	cp.dbMu.Unlock()

	return client, nil
}

// RemoveClient removes a client from the pool
func (cp *ClientPool) RemoveClient(instanceID int) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	delete(cp.clients, instanceID)
	log.Info().Int("instanceID", instanceID).Msg("Removed client from pool")
}

// healthCheckLoop periodically checks the health of all clients
func (cp *ClientPool) healthCheckLoop() {
	for {
		select {
		case <-cp.healthTicker.C:
			cp.performHealthChecks()
		case <-cp.stopHealth:
			return
		}
	}
}

// performHealthChecks checks the health of all clients
func (cp *ClientPool) performHealthChecks() {
	cp.mu.RLock()
	clients := make([]*Client, 0, len(cp.clients))
	for _, client := range cp.clients {
		clients = append(clients, client)
	}
	cp.mu.RUnlock()

	for _, client := range clients {
		instanceID := client.GetInstanceID()

		// Skip if recently checked
		if time.Since(client.GetLastHealthCheck()) < minHealthCheckInterval {
			continue
		}

		// Skip if instance is in backoff period
		if cp.isInBackoff(instanceID) {
			continue
		}

		// Submit health check in goroutine
		go func(client *Client, instanceID int) {
			// Use appropriate timeout for health checks
			// Since we're now using GetWebAPIVersion instead of Login,
			// this should be much faster even for large instances
			ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
			defer cancel()

			if err := client.HealthCheck(ctx); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Health check failed")

				// Track failure and apply backoff
				cp.trackFailure(instanceID, err)

				// Mark as inactive in database (serialize DB updates)
				cp.dbMu.Lock()
				if err := cp.instanceStore.UpdateActive(ctx, instanceID, false); err != nil {
					log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update inactive status")
				}
				cp.dbMu.Unlock()

				// Don't try to recreate if we're now in backoff
				if !cp.isInBackoff(instanceID) {
					if _, err := cp.createClient(ctx, instanceID); err != nil {
						log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to recreate client")
					}
				}
			} else {
				// Health check succeeded, reset failure tracking and ensure marked as active
				cp.resetFailureTracking(instanceID)

				cp.dbMu.Lock()
				if err := cp.instanceStore.UpdateActive(ctx, instanceID, true); err != nil {
					log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update active status")
				}
				cp.dbMu.Unlock()
			}
		}(client, instanceID)
	}
}

// GetCache returns the cache instance for external use
func (cp *ClientPool) GetCache() *ristretto.Cache {
	return cp.cache
}

// Close closes all clients and releases resources
func (cp *ClientPool) Close() error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	if cp.closed {
		return nil
	}

	cp.closed = true
	close(cp.stopHealth)
	cp.healthTicker.Stop()

	// Clear all clients and failure tracking
	for id := range cp.clients {
		delete(cp.clients, id)
	}
	cp.failureTracker = make(map[int]*failureInfo)

	// Release resources
	cp.cache.Close()

	log.Info().Msg("Client pool closed")
	return nil
}

// Stats returns statistics about the pool
func (cp *ClientPool) Stats() map[string]interface{} {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	healthyCount := 0
	for _, client := range cp.clients {
		if client.IsHealthy() {
			healthyCount++
		}
	}

	// Count instances in backoff
	backoffCount := 0
	for _, info := range cp.failureTracker {
		if time.Now().Before(info.nextRetry) {
			backoffCount++
		}
	}

	return map[string]interface{}{
		"total_clients":   len(cp.clients),
		"healthy_clients": healthyCount,
		"backoff_clients": backoffCount,
		"cache_hits":      cp.cache.Metrics.Hits(),
		"cache_misses":    cp.cache.Metrics.Misses(),
	}
}

// isInBackoff checks if an instance is in backoff period
func (cp *ClientPool) isInBackoff(instanceID int) bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.isInBackoffLocked(instanceID)
}

// isInBackoffLocked checks if an instance is in backoff period (caller must hold lock)
func (cp *ClientPool) isInBackoffLocked(instanceID int) bool {
	info, exists := cp.failureTracker[instanceID]
	if !exists {
		return false
	}
	return time.Now().Before(info.nextRetry)
}

// trackFailure records a failure and applies exponential backoff
func (cp *ClientPool) trackFailure(instanceID int, err error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	info, exists := cp.failureTracker[instanceID]
	if !exists {
		info = &failureInfo{}
		cp.failureTracker[instanceID] = info
	}

	info.attempts++

	// Calculate backoff duration
	var backoffDuration time.Duration
	if cp.isBanError(err) {
		backoffDuration = cp.calculateBackoff(info.attempts, banInitialBackoff, banMaxBackoff)
		log.Warn().Int("instanceID", instanceID).Int("attempts", info.attempts).Dur("backoffDuration", backoffDuration).Msg("IP ban detected, applying extended backoff")
	} else {
		backoffDuration = cp.calculateBackoff(info.attempts, initialBackoff, maxBackoff)
		log.Debug().Int("instanceID", instanceID).Int("attempts", info.attempts).Dur("backoffDuration", backoffDuration).Msg("Connection failure, applying backoff")
	}

	info.nextRetry = time.Now().Add(backoffDuration)
}

// calculateBackoff returns exponential backoff duration with limits
func (cp *ClientPool) calculateBackoff(attempts int, initialDuration, maxDuration time.Duration) time.Duration {
	backoff := time.Duration(1<<(attempts-1)) * initialDuration
	if backoff > maxDuration {
		backoff = maxDuration
	}
	return backoff
}

// resetFailureTracking clears failure tracking for successful connections
func (cp *ClientPool) resetFailureTracking(instanceID int) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.resetFailureTrackingLocked(instanceID)
}

func (cp *ClientPool) resetFailureTrackingLocked(instanceID int) {
	if _, exists := cp.failureTracker[instanceID]; exists {
		delete(cp.failureTracker, instanceID)
		log.Debug().Int("instanceID", instanceID).Msg("Reset failure tracking after successful connection")
	}
}

// isBanError checks if the error indicates an IP ban
func (cp *ClientPool) isBanError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := strings.ToLower(err.Error())

	// Check for common ban-related error messages
	return strings.Contains(errorStr, "ip is banned") ||
		strings.Contains(errorStr, "too many failed login attempts") ||
		strings.Contains(errorStr, "banned") ||
		strings.Contains(errorStr, "rate limit") ||
		strings.Contains(errorStr, "403") ||
		strings.Contains(errorStr, "forbidden")
}

// GetBackoffStatus returns the backoff status for an instance (useful for debugging)
func (cp *ClientPool) GetBackoffStatus(instanceID int) (inBackoff bool, nextRetry time.Time, attempts int) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	info, exists := cp.failureTracker[instanceID]
	if !exists {
		return false, time.Time{}, 0
	}

	inBackoff = time.Now().Before(info.nextRetry)
	return inBackoff, info.nextRetry, info.attempts
}
