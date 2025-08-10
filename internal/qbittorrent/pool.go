// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/panjf2000/ants/v2"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

var (
	ErrClientNotFound = errors.New("qBittorrent client not found")
	ErrPoolClosed     = errors.New("client pool is closed")
)

// ClientPool manages multiple qBittorrent client connections
type ClientPool struct {
	clients       map[int]*Client
	instanceStore *models.InstanceStore
	cache         *ristretto.Cache
	pool          *ants.Pool
	mu            sync.RWMutex
	dbMu          sync.Mutex // Serialize database updates
	closed        bool
	healthTicker  *time.Ticker
	stopHealth    chan struct{}
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

	// Create goroutine pool
	pool, err := ants.NewPool(100, ants.WithPreAlloc(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create goroutine pool: %w", err)
	}

	cp := &ClientPool{
		clients:       make(map[int]*Client),
		instanceStore: instanceStore,
		cache:         cache,
		pool:          pool,
		healthTicker:  time.NewTicker(30 * time.Second),
		stopHealth:    make(chan struct{}),
	}

	// Start health check routine
	go cp.healthCheckLoop()

	return cp, nil
}

// GetClient returns a qBittorrent client for the given instance ID
func (cp *ClientPool) GetClient(instanceID int) (*Client, error) {
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
	return cp.createClient(instanceID)
}

// createClient creates a new client connection
func (cp *ClientPool) createClient(instanceID int) (*Client, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := cp.clients[instanceID]; exists && client.IsHealthy() {
		return client, nil
	}

	// Get instance details
	instance, err := cp.instanceStore.Get(instanceID)
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
	client, err := NewClient(instanceID, instance.Host, instance.Port, instance.Username, password, instance.BasicUsername, basicPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Store in pool
	cp.clients[instanceID] = client

	// Update last connected timestamp
	go func() {
		cp.dbMu.Lock()
		defer cp.dbMu.Unlock()
		if err := cp.instanceStore.UpdateLastConnected(instanceID); err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update last connected timestamp")
		}
	}()

	//log.Info().Int("instanceID", instanceID).Str("name", instance.Name).Msg("Created new qBittorrent client")
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
		// Skip if recently checked
		if time.Since(client.GetLastHealthCheck()) < 20*time.Second {
			continue
		}

		// Submit health check to goroutine pool
		instanceID := client.GetInstanceID()
		cp.pool.Submit(func() {
			// Use appropriate timeout for health checks
			// Since we're now using GetWebAPIVersion instead of Login,
			// this should be much faster even for large instances
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := client.HealthCheck(ctx); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Msg("Health check failed")

				// Mark as inactive in database (serialize DB updates)
				cp.dbMu.Lock()
				if err := cp.instanceStore.UpdateActive(instanceID, false); err != nil {
					log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update inactive status")
				}
				cp.dbMu.Unlock()

				// Try to recreate the client
				if _, err := cp.createClient(instanceID); err != nil {
					log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to recreate client")
				}
			} else {
				// Health check succeeded, ensure marked as active (serialize DB updates)
				cp.dbMu.Lock()
				if err := cp.instanceStore.UpdateActive(instanceID, true); err != nil {
					log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update active status")
				}
				cp.dbMu.Unlock()
			}
		})
	}
}

// GetCache returns the cache instance for external use
func (cp *ClientPool) GetCache() *ristretto.Cache {
	return cp.cache
}

// GetPool returns the goroutine pool for external use
func (cp *ClientPool) GetPool() *ants.Pool {
	return cp.pool
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

	// Clear all clients
	for id := range cp.clients {
		delete(cp.clients, id)
	}

	// Release resources
	cp.pool.Release()
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

	return map[string]interface{}{
		"total_clients":   len(cp.clients),
		"healthy_clients": healthyCount,
		"cache_hits":      cp.cache.Metrics.Hits(),
		"cache_misses":    cp.cache.Metrics.Misses(),
		"pool_running":    cp.pool.Running(),
		"pool_free":       cp.pool.Free(),
	}
}
