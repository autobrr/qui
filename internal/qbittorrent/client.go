// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"sync"
	"time"
	"unsafe"

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
	peerSyncManager map[string]*qbt.PeerSyncManager // Map of torrent hash to PeerSyncManager
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
		peerSyncManager:   make(map[string]*qbt.PeerSyncManager),
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

// getTorrentsByHashes returns multiple torrents by their hashes (O(n) where n is number of requested hashes)
func (c *Client) getTorrentsByHashes(hashes []string) []qbt.Torrent {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.syncManager == nil {
		return nil
	}

	return c.syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: hashes})
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

// GetHTTPClient allows you to receive the implemented *http.Client with cookie jar
// This method uses reflection to access the private http field from the embedded qbt.Client
//
// TODO: Remove this method and update proxy handler when go-qbittorrent merges GetHTTPClient method
// When https://github.com/autobrr/go-qbittorrent is updated with GetHTTPClient method:
// 1. Remove this entire GetHTTPClient method from qui's Client wrapper
// 2. Update proxy handler to call client.Client.GetHTTPClient() directly instead of client.GetHTTPClient()
// 3. Remove "reflect" and "unsafe" imports from this file
// 4. Update go.mod to use the new version of go-qbittorrent
func (c *Client) GetHTTPClient() *http.Client {
	// Use reflection to access the private 'http' field from the embedded qbt.Client
	clientValue := reflect.ValueOf(c.Client).Elem()
	httpField := clientValue.FieldByName("http")

	if !httpField.IsValid() {
		log.Error().Msg("Failed to access http field from qBittorrent client")
		return nil
	}

	// The field is unexported, so we need to make it accessible
	if !httpField.CanInterface() {
		// Make the field accessible using reflection
		httpField = reflect.NewAt(httpField.Type(), unsafe.Pointer(httpField.UnsafeAddr())).Elem()
	}

	if httpClient, ok := httpField.Interface().(*http.Client); ok {
		return httpClient
	}

	log.Error().Msg("Failed to convert http field to *http.Client")
	return nil
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

// GetOrCreatePeerSyncManager gets or creates a PeerSyncManager for a specific torrent
func (c *Client) GetOrCreatePeerSyncManager(hash string) *qbt.PeerSyncManager {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we already have a sync manager for this torrent
	if peerSync, exists := c.peerSyncManager[hash]; exists {
		return peerSync
	}

	// Create a new peer sync manager for this torrent
	peerSyncOpts := qbt.DefaultPeerSyncOptions()
	peerSyncOpts.AutoSync = false // We'll sync manually when requested
	peerSync := c.Client.NewPeerSyncManager(hash, peerSyncOpts)
	c.peerSyncManager[hash] = peerSync

	return peerSync
}

// applyOptimisticCacheUpdate applies optimistic updates for the given hashes and action
func (c *Client) applyOptimisticCacheUpdate(hashes []string, action string, _ map[string]any) {
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
