// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Client struct {
	*qbt.Client
	instanceID      int
	webAPIVersion   string
	supportsSetTags bool
	lastHealthCheck time.Time
	isHealthy       bool
	syncManager     *qbt.SyncManager
	// optimisticUpdates stores temporary optimistic state changes for this instance
	optimisticUpdates map[string]*OptimisticTorrentUpdate
	mu                sync.RWMutex
	healthMu          sync.RWMutex
}

func NewClient(instanceID int, instanceHost, username, password string, basicUsername, basicPassword *string) (*Client, error) {
	return NewClientWithTimeout(instanceID, instanceHost, username, password, basicUsername, basicPassword, 60*time.Second)
}

func NewClientWithTimeout(instanceID int, instanceHost, username, password string, basicUsername, basicPassword *string, timeout time.Duration) (*Client, error) {
	cfg := qbt.Config{
		Host:     instanceHost,
		Username: username,
		Password: password,
		Timeout:  int(timeout.Seconds()),
	}

	if basicUsername != nil && *basicUsername != "" {
		cfg.BasicUser = *basicUsername
		if basicPassword != nil {
			cfg.BasicPass = *basicPassword
		}
	}

	qbtClient := qbt.NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := qbtClient.LoginCtx(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent instance: %w", err)
	}

	webAPIVersion, err := qbtClient.GetWebAPIVersionCtx(ctx)
	if err != nil {
		webAPIVersion = ""
	}

	supportsSetTags := false
	if webAPIVersion != "" {
		if v, err := semver.NewVersion(webAPIVersion); err == nil {
			minVersion := semver.MustParse("2.11.4")
			supportsSetTags = !v.LessThan(minVersion)
		}
	}

	client := &Client{
		Client:            qbtClient,
		instanceID:        instanceID,
		webAPIVersion:     webAPIVersion,
		supportsSetTags:   supportsSetTags,
		lastHealthCheck:   time.Now(),
		isHealthy:         true,
		optimisticUpdates: make(map[string]*OptimisticTorrentUpdate),
	}

	// Initialize sync manager with default options
	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true

	// Set up health check callbacks
	syncOpts.OnUpdate = func(data *qbt.MainData) {
		client.updateHealthStatus(true)
		log.Debug().Int("instanceID", instanceID).Int("torrentCount", len(data.Torrents)).Msg("Sync manager update received, marking client as healthy")
	}

	syncOpts.OnError = func(err error) {
		client.updateHealthStatus(false)
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Sync manager error received, marking client as unhealthy")
	}

	client.syncManager = qbtClient.NewSyncManager(syncOpts)

	log.Debug().
		Int("instanceID", instanceID).
		Str("host", instanceHost).
		Str("webAPIVersion", webAPIVersion).
		Bool("supportsSetTags", supportsSetTags).
		Msg("qBittorrent client created successfully")

	return client, nil
}

func (c *Client) GetInstanceID() int {
	return c.instanceID
}

func (c *Client) GetLastHealthCheck() time.Time {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.lastHealthCheck
}

func (c *Client) GetLastSyncUpdate() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.syncManager == nil {
		return time.Time{}
	}
	return c.syncManager.LastSyncTime()
}

func (c *Client) updateHealthStatus(healthy bool) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()
	c.isHealthy = healthy
	c.lastHealthCheck = time.Now()
}

func (c *Client) IsHealthy() bool {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.isHealthy
}

// getTorrentByHash returns a torrent by hash from the sync manager
func (c *Client) getTorrentByHash(hash string) (qbt.Torrent, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.syncManager == nil {
		return qbt.Torrent{}, false
	}

	return c.syncManager.GetTorrent(hash)
}

// getTorrentsByHashes returns multiple torrents by their hashes (O(n) where n is number of requested hashes)
func (c *Client) getTorrentsByHashes(hashes []string) []qbt.Torrent {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.syncManager == nil {
		return nil
	}

	return c.syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: hashes})
}

// validateTorrentHashes returns validation info for a list of hashes
func (c *Client) validateTorrentHashes(hashes []string) (existing []qbt.Torrent, missing []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.syncManager == nil {
		return nil, hashes
	}

	torrentMap := c.syncManager.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: hashes})

	existing = make([]qbt.Torrent, 0, len(torrentMap))
	missing = make([]string, 0, len(hashes)-len(torrentMap))

	for _, hash := range hashes {
		if torrent, exists := torrentMap[hash]; exists {
			existing = append(existing, torrent)
		} else {
			missing = append(missing, hash)
		}
	}
	return existing, missing
}

func (c *Client) HealthCheck(ctx context.Context) error {
	if c.isHealthy && time.Now().Add(-minHealthCheckInterval).Before(c.GetLastHealthCheck()) {
		return nil
	}

	_, err := c.GetWebAPIVersionCtx(ctx)
	c.updateHealthStatus(err == nil)

	if err != nil {
		return errors.Wrap(err, "health check failed")
	}

	return nil
}

func (c *Client) SupportsSetTags() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsSetTags
}

func (c *Client) GetWebAPIVersion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.webAPIVersion
}

func (c *Client) GetSyncManager() *qbt.SyncManager {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncManager
}

func (c *Client) StartSyncManager(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.syncManager == nil {
		return fmt.Errorf("sync manager not initialized")
	}
	return c.syncManager.Start(ctx)
}

// applyOptimisticCacheUpdate applies optimistic updates for the given hashes and action
func (c *Client) applyOptimisticCacheUpdate(hashes []string, action string, payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Debug().Int("instanceID", c.instanceID).Str("action", action).Int("hashCount", len(hashes)).Msg("Starting optimistic cache update")

	now := time.Now()

	// Apply optimistic updates based on action using sync manager data
	for _, hash := range hashes {
		var originalState qbt.TorrentState
		var progress float64
		if c.syncManager != nil {
			if torrent, exists := c.syncManager.GetTorrent(hash); exists {
				originalState = torrent.State
				progress = torrent.Progress
			}
		}
		state := getTargetState(action, progress)
		if state != "" && state != originalState {
			c.optimisticUpdates[hash] = &OptimisticTorrentUpdate{
				State:         state,
				OriginalState: originalState,
				UpdatedAt:     now,
				Action:        action,
			}
			log.Debug().Int("instanceID", c.instanceID).Str("hash", hash).Str("action", action).Msg("Created optimistic update for " + action)
		}
	}

	log.Debug().Int("instanceID", c.instanceID).Str("action", action).Int("hashCount", len(hashes)).Int("totalOptimistic", len(c.optimisticUpdates)).Msg("Completed optimistic cache update")
}

// getOptimisticUpdates returns a copy of the current optimistic updates
func (c *Client) getOptimisticUpdates() map[string]*OptimisticTorrentUpdate {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	updates := make(map[string]*OptimisticTorrentUpdate, len(c.optimisticUpdates))
	maps.Copy(updates, c.optimisticUpdates)
	return updates
}

// clearOptimisticUpdate removes an optimistic update for a specific torrent
func (c *Client) clearOptimisticUpdate(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.optimisticUpdates, hash)
	log.Debug().Int("instanceID", c.instanceID).Str("hash", hash).Msg("Cleared optimistic update")
}

// clearStaleOptimisticUpdates removes optimistic updates that are older than the specified duration
func (c *Client) clearStaleOptimisticUpdates(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	for hash, update := range c.optimisticUpdates {
		if now.Sub(update.UpdatedAt) > maxAge {
			delete(c.optimisticUpdates, hash)
			removed++
		}
	}

	if removed > 0 {
		log.Debug().Int("instanceID", c.instanceID).Int("removed", removed).Msg("Cleared stale optimistic updates")
	}
}

// clearAllOptimisticUpdates removes all optimistic updates for this instance
func (c *Client) clearAllOptimisticUpdates() {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := len(c.optimisticUpdates)
	if count > 0 {
		c.optimisticUpdates = make(map[string]*OptimisticTorrentUpdate)
		log.Debug().Int("instanceID", c.instanceID).Int("cleared", count).Msg("Cleared all optimistic updates")
	}
}

// getTargetState returns the target state for the given action and progress
func getTargetState(action string, progress float64) qbt.TorrentState {
	switch action {
	case "resume":
		if progress == 1.0 {
			return qbt.TorrentStateQueuedUp
		}
		return qbt.TorrentStateQueuedDl
	case "force_resume":
		if progress == 1.0 {
			return qbt.TorrentStateForcedUp
		}
		return qbt.TorrentStateForcedDl
	case "pause":
		if progress == 1.0 {
			return qbt.TorrentStatePausedUp
		}
		return qbt.TorrentStatePausedDl
	case "recheck":
		if progress == 1.0 {
			return qbt.TorrentStateCheckingUp
		}
		return qbt.TorrentStateCheckingDl
	default:
		return ""
	}
}
