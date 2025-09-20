// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// LogCacheEntry represents cached logs for an instance
type LogCacheEntry struct {
	Logs        []qbt.Log
	LastKnownID int64
	UpdatedAt   time.Time
}

// PeerLogCacheEntry represents cached peer logs for an instance
type PeerLogCacheEntry struct {
	Logs        []qbt.PeerLog
	LastKnownID int64
	UpdatedAt   time.Time
}

// LogCache manages cached logs for all instances
type LogCache struct {
	mainCache *ttlcache.Cache[string, *LogCacheEntry]
	peerCache *ttlcache.Cache[string, *PeerLogCacheEntry]
	pool      *ClientPool
	mu        sync.RWMutex
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// NewLogCache creates a new log cache
func NewLogCache(pool *ClientPool) *LogCache {
	// Create caches with 30 minute TTL
	mainCache := ttlcache.New(ttlcache.Options[string, *LogCacheEntry]{}.
		SetDefaultTTL(30 * time.Minute))

	peerCache := ttlcache.New(ttlcache.Options[string, *PeerLogCacheEntry]{}.
		SetDefaultTTL(30 * time.Minute))

	lc := &LogCache{
		mainCache: mainCache,
		peerCache: peerCache,
		pool:      pool,
		stopChan:  make(chan struct{}),
	}

	// Start background polling
	lc.startPolling()

	return lc
}

// startPolling starts background polling for new logs
func (lc *LogCache) startPolling() {
	lc.wg.Add(1)
	go func() {
		defer lc.wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lc.updateAllLogs()
			case <-lc.stopChan:
				return
			}
		}
	}()
}

// updateAllLogs fetches new logs for all connected instances
func (lc *LogCache) updateAllLogs() {
	clients := lc.pool.GetAllClients()
	for instanceID, client := range clients {
		if client == nil || !client.IsHealthy() {
			continue
		}

		// Update main logs
		lc.updateMainLogs(instanceID, client)

		// Update peer logs
		lc.updatePeerLogs(instanceID, client)
	}
}

// updateMainLogs fetches new main logs for an instance
func (lc *LogCache) updateMainLogs(instanceID int, client *Client) {
	cacheKey := fmt.Sprintf("instance-%d-main", instanceID)

	// Get current cache entry
	lc.mu.RLock()
	entry, _ := lc.mainCache.Get(cacheKey)
	lc.mu.RUnlock()

	lastKnownID := int64(-1)
	var existingLogs []qbt.Log

	if entry != nil {
		lastKnownID = entry.LastKnownID
		existingLogs = entry.Logs
	}

	// Fetch new logs
	opts := &qbt.LogOptions{
		Normal:      true,
		Info:        true,
		Warning:     true,
		Critical:    true,
		LastKnownID: lastKnownID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	newLogs, err := client.Client.GetLogsWithOptionsCtx(ctx, opts)
	if err != nil {
		log.Debug().Err(err).Int("instance_id", instanceID).Msg("Failed to fetch main logs")
		return
	}

	if len(newLogs) == 0 {
		return
	}

	// Merge logs and update cache
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Append new logs to existing ones
	allLogs := append(existingLogs, newLogs...)

	// Keep only last 10k logs
	if len(allLogs) > 10000 {
		allLogs = allLogs[len(allLogs)-10000:]
	}

	// Find the max ID
	maxID := lastKnownID
	for _, log := range newLogs {
		if log.ID > maxID {
			maxID = log.ID
		}
	}

	newEntry := &LogCacheEntry{
		Logs:        allLogs,
		LastKnownID: maxID,
		UpdatedAt:   time.Now(),
	}

	lc.mainCache.Set(cacheKey, newEntry, ttlcache.DefaultTTL)
}

// updatePeerLogs fetches new peer logs for an instance
func (lc *LogCache) updatePeerLogs(instanceID int, client *Client) {
	cacheKey := fmt.Sprintf("instance-%d-peer", instanceID)

	// Get current cache entry
	lc.mu.RLock()
	entry, _ := lc.peerCache.Get(cacheKey)
	lc.mu.RUnlock()

	lastKnownID := int64(-1)
	var existingLogs []qbt.PeerLog

	if entry != nil {
		lastKnownID = entry.LastKnownID
		existingLogs = entry.Logs
	}

	// Fetch new logs
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	newLogs, err := client.Client.GetPeerLogsWithOptionsCtx(ctx, lastKnownID)
	if err != nil {
		log.Debug().Err(err).Int("instance_id", instanceID).Msg("Failed to fetch peer logs")
		return
	}

	if len(newLogs) == 0 {
		return
	}

	// Merge logs and update cache
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Append new logs to existing ones
	allLogs := append(existingLogs, newLogs...)

	// Keep only last 10k logs
	if len(allLogs) > 10000 {
		allLogs = allLogs[len(allLogs)-10000:]
	}

	// Find the max ID
	maxID := lastKnownID
	for _, log := range newLogs {
		if log.ID > maxID {
			maxID = log.ID
		}
	}

	newEntry := &PeerLogCacheEntry{
		Logs:        allLogs,
		LastKnownID: maxID,
		UpdatedAt:   time.Now(),
	}

	lc.peerCache.Set(cacheKey, newEntry, ttlcache.DefaultTTL)
}

// GetMainLogs returns cached main logs for an instance with filtering
func (lc *LogCache) GetMainLogs(instanceID int, page, limit int, search string, levels []int) ([]qbt.Log, int, error) {
	cacheKey := fmt.Sprintf("instance-%d-main", instanceID)

	lc.mu.RLock()
	entry, _ := lc.mainCache.Get(cacheKey)
	lc.mu.RUnlock()

	if entry == nil {
		// No cached data, trigger an update
		ctx := context.Background()
		client, _ := lc.pool.GetClient(ctx, instanceID)
		if client != nil && client.IsHealthy() {
			lc.updateMainLogs(instanceID, client)

			// Try to get from cache again
			lc.mu.RLock()
			entry, _ = lc.mainCache.Get(cacheKey)
			lc.mu.RUnlock()
		}

		if entry == nil {
			return []qbt.Log{}, 0, nil
		}
	}

	logs := entry.Logs

	// Apply level filtering
	if len(levels) > 0 {
		var filtered []qbt.Log
		levelMap := make(map[int64]bool)
		for _, l := range levels {
			levelMap[int64(l)] = true
		}
		for _, log := range logs {
			if levelMap[log.Type] {
				filtered = append(filtered, log)
			}
		}
		logs = filtered
	}

	// Apply search filtering
	if search != "" {
		search = strings.ToLower(search)
		var filtered []qbt.Log
		for _, log := range logs {
			if strings.Contains(strings.ToLower(log.Message), search) {
				filtered = append(filtered, log)
			}
		}
		logs = filtered
	}

	totalCount := len(logs)

	// Apply pagination (reverse order - newest first)
	if limit <= 0 {
		limit = 100
	}
	if page < 0 {
		page = 0
	}

	// Reverse the logs array for newest first
	reversed := make([]qbt.Log, len(logs))
	for i, log := range logs {
		reversed[len(logs)-1-i] = log
	}
	logs = reversed

	start := page * limit
	end := start + limit

	if start >= len(logs) {
		return []qbt.Log{}, totalCount, nil
	}

	if end > len(logs) {
		end = len(logs)
	}

	return logs[start:end], totalCount, nil
}

// GetPeerLogs returns cached peer logs for an instance with filtering
func (lc *LogCache) GetPeerLogs(instanceID int, page, limit int, search string) ([]qbt.PeerLog, int, error) {
	cacheKey := fmt.Sprintf("instance-%d-peer", instanceID)

	lc.mu.RLock()
	entry, _ := lc.peerCache.Get(cacheKey)
	lc.mu.RUnlock()

	if entry == nil {
		// No cached data, trigger an update
		ctx := context.Background()
		client, _ := lc.pool.GetClient(ctx, instanceID)
		if client != nil && client.IsHealthy() {
			lc.updatePeerLogs(instanceID, client)

			// Try to get from cache again
			lc.mu.RLock()
			entry, _ = lc.peerCache.Get(cacheKey)
			lc.mu.RUnlock()
		}

		if entry == nil {
			return []qbt.PeerLog{}, 0, nil
		}
	}

	logs := entry.Logs

	// Apply search filtering
	if search != "" {
		search = strings.ToLower(search)
		var filtered []qbt.PeerLog
		for _, log := range logs {
			if strings.Contains(strings.ToLower(log.IP), search) ||
				strings.Contains(strings.ToLower(log.Reason), search) {
				filtered = append(filtered, log)
			}
		}
		logs = filtered
	}

	totalCount := len(logs)

	// Apply pagination (reverse order - newest first)
	if limit <= 0 {
		limit = 100
	}
	if page < 0 {
		page = 0
	}

	// Reverse the logs array for newest first
	reversed := make([]qbt.PeerLog, len(logs))
	for i, log := range logs {
		reversed[len(logs)-1-i] = log
	}
	logs = reversed

	start := page * limit
	end := start + limit

	if start >= len(logs) {
		return []qbt.PeerLog{}, totalCount, nil
	}

	if end > len(logs) {
		end = len(logs)
	}

	return logs[start:end], totalCount, nil
}

// Stop stops the log cache polling
func (lc *LogCache) Stop() {
	close(lc.stopChan)
	lc.wg.Wait()
	lc.mainCache.Close()
	lc.peerCache.Close()
}
