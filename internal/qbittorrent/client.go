// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type Client struct {
	*qbt.Client
	instanceID              int
	webAPIVersion           string
	supportsSetTags         bool
	supportsTorrentCreation bool
	supportsTorrentExport   bool
	supportsTrackerEditing  bool
	supportsRenameTorrent   bool
	supportsRenameFile      bool
	supportsRenameFolder    bool
	supportsSubcategories   bool
	lastHealthCheck         time.Time
	isHealthy               bool
	syncManager             *qbt.SyncManager
	peerSyncManager         map[string]*qbt.PeerSyncManager // Map of torrent hash to PeerSyncManager
	// optimisticUpdates stores temporary optimistic state changes for this instance
	optimisticUpdates *ttlcache.Cache[string, *OptimisticTorrentUpdate]
	trackerExclusions map[string]map[string]struct{} // Domains to hide hashes from until fresh sync arrives
	lastServerState   *qbt.ServerState
	mu                sync.RWMutex
	serverStateMu     sync.RWMutex
	healthMu          sync.RWMutex
}

func NewClient(instanceID int, instanceHost, username, password string, basicUsername, basicPassword *string, tlsSkipVerify bool) (*Client, error) {
	return NewClientWithTimeout(instanceID, instanceHost, username, password, basicUsername, basicPassword, tlsSkipVerify, 60*time.Second)
}

func NewClientWithTimeout(instanceID int, instanceHost, username, password string, basicUsername, basicPassword *string, tlsSkipVerify bool, timeout time.Duration) (*Client, error) {
	cfg := qbt.Config{
		Host:          instanceHost,
		Username:      username,
		Password:      password,
		Timeout:       int(timeout.Seconds()),
		TLSSkipVerify: tlsSkipVerify,
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
	supportsTorrentCreation := false
	supportsTorrentExport := false
	supportsTrackerEditing := false
	supportsRenameTorrent := false
	supportsRenameFile := false
	supportsRenameFolder := false
	supportsSubcategories := false
	if webAPIVersion != "" {
		if v, err := semver.NewVersion(webAPIVersion); err == nil {
			setTagsMinVersion := semver.MustParse("2.11.4")
			supportsSetTags = !v.LessThan(setTagsMinVersion)

			torrentCreationMinVersion := semver.MustParse("2.11.2")
			supportsTorrentCreation = !v.LessThan(torrentCreationMinVersion)

			// Torrent export endpoint: WebAPI 2.8.11+ (qBittorrent 4.5.0+, introduced in 4.5.0beta1)
			exportTorrentMinVersion := semver.MustParse("2.8.11")
			supportsTorrentExport = !v.LessThan(exportTorrentMinVersion)

			trackerEditingMinVersion := semver.MustParse("2.2.0")
			supportsTrackerEditing = !v.LessThan(trackerEditingMinVersion)

			// Rename torrent: qBittorrent 4.1.0+ (WebAPI 2.0.0+)
			renameTorrentMinVersion := semver.MustParse("2.0.0")
			supportsRenameTorrent = !v.LessThan(renameTorrentMinVersion)

			// Rename file: qBittorrent 4.2.1+ (WebAPI 2.4.0+)
			renameFileMinVersion := semver.MustParse("2.4.0")
			supportsRenameFile = !v.LessThan(renameFileMinVersion)

			// Rename folder: qBittorrent 4.3.3+ (WebAPI 2.7.0+)
			renameFolderMinVersion := semver.MustParse("2.7.0")
			supportsRenameFolder = !v.LessThan(renameFolderMinVersion)

			// Subcategories: qBittorrent 4.6+ (WebAPI 2.9.0+)
			subcategoriesMinVersion := semver.MustParse("2.9.0")
			supportsSubcategories = !v.LessThan(subcategoriesMinVersion)
		}
	}

	client := &Client{
		Client:                  qbtClient,
		instanceID:              instanceID,
		webAPIVersion:           webAPIVersion,
		supportsSetTags:         supportsSetTags,
		supportsTorrentCreation: supportsTorrentCreation,
		supportsTorrentExport:   supportsTorrentExport,
		supportsTrackerEditing:  supportsTrackerEditing,
		supportsRenameTorrent:   supportsRenameTorrent,
		supportsRenameFile:      supportsRenameFile,
		supportsRenameFolder:    supportsRenameFolder,
		supportsSubcategories:   supportsSubcategories,
		lastHealthCheck:         time.Now(),
		isHealthy:               true,
		optimisticUpdates: ttlcache.New(ttlcache.Options[string, *OptimisticTorrentUpdate]{}.
			SetDefaultTTL(30 * time.Second)), // Updates expire after 30 seconds
		trackerExclusions: make(map[string]map[string]struct{}),
		peerSyncManager:   make(map[string]*qbt.PeerSyncManager),
	}

	// Initialize sync manager with default options
	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true

	// Set up health check callbacks
	syncOpts.OnUpdate = func(data *qbt.MainData) {
		client.updateHealthStatus(true)
		client.updateServerState(data)
		log.Debug().Int("instanceID", instanceID).Int("torrentCount", len(data.Torrents)).Msg("Sync manager update received, marking client as healthy")
	}

	syncOpts.OnError = func(err error) {
		client.updateHealthStatus(false)
		client.clearServerState()
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Sync manager error received, marking client as unhealthy")
	}

	client.syncManager = qbtClient.NewSyncManager(syncOpts)

	log.Debug().
		Int("instanceID", instanceID).
		Str("host", instanceHost).
		Str("webAPIVersion", webAPIVersion).
		Bool("supportsSetTags", supportsSetTags).
		Bool("supportsTorrentCreation", supportsTorrentCreation).
		Bool("supportsTorrentExport", supportsTorrentExport).
		Bool("supportsTrackerEditing", supportsTrackerEditing).
		Bool("supportsSubcategories", supportsSubcategories).
		Bool("tlsSkipVerify", tlsSkipVerify).
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

func (c *Client) SupportsTorrentCreation() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsTorrentCreation
}

func (c *Client) SupportsTrackerEditing() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsTrackerEditing
}

func (c *Client) SupportsTorrentExport() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsTorrentExport
}

func (c *Client) updateServerState(data *qbt.MainData) {
	c.serverStateMu.Lock()
	defer c.serverStateMu.Unlock()

	if data == nil || data.ServerState == (qbt.ServerState{}) {
		c.lastServerState = nil
		return
	}

	stateCopy := data.ServerState
	c.lastServerState = &stateCopy
}

func (c *Client) clearServerState() {
	c.serverStateMu.Lock()
	defer c.serverStateMu.Unlock()

	c.lastServerState = nil
}

func (c *Client) GetCachedServerState() *qbt.ServerState {
	c.serverStateMu.RLock()
	defer c.serverStateMu.RUnlock()

	if c.lastServerState == nil {
		return nil
	}

	copy := *c.lastServerState
	return &copy
}

func (c *Client) GetCachedConnectionStatus() string {
	state := c.GetCachedServerState()
	if state == nil {
		return ""
	}

	return state.ConnectionStatus
}

// UpdateWithMainData updates the client's cached state with fresh MainData
// This is used when intercepting sync/maindata responses to keep local state in sync
func (c *Client) UpdateWithMainData(data *qbt.MainData) {
	c.updateServerState(data)
	c.updateHealthStatus(true)
	log.Debug().
		Int("instanceID", c.instanceID).
		Int("torrentCount", len(data.Torrents)).
		Msg("Updated client state with fresh maindata from intercepted request")
}

// UpdateWithPeersData triggers a sync on the peer manager to keep it warm after intercepting peer data
// This ensures our local peer state stays synchronized with the proxy client's view
func (c *Client) UpdateWithPeersData(hash string, data *qbt.TorrentPeersResponse) {
	// Get or create the peer sync manager for this torrent
	peerSync := c.GetOrCreatePeerSyncManager(hash)

	// Trigger a background sync to refresh the peer state
	// We can't directly inject the data, but we can trigger a sync to keep the cache warm
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := peerSync.Sync(ctx); err != nil {
			log.Error().
				Err(err).
				Int("instanceID", c.instanceID).
				Str("hash", hash).
				Msg("Failed to sync peer manager after intercepted peer data")
			return
		}

		log.Debug().
			Int("instanceID", c.instanceID).
			Str("hash", hash).
			Int("peerCount", len(data.Peers)).
			Int64("rid", data.Rid).
			Msg("Updated peer state with fresh data from intercepted request")
	}()
}

func (c *Client) SupportsRenameTorrent() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsRenameTorrent
}

func (c *Client) SupportsRenameFile() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsRenameFile
}

func (c *Client) SupportsRenameFolder() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsRenameFolder
}

func (c *Client) SupportsSubcategories() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsSubcategories
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

func (c *Client) SupportsTrackerHealth() bool {
	return c.supportsTrackerInclude()
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

func (c *Client) trackerManager() *qbt.TrackerManager {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.syncManager == nil {
		return nil
	}
	return c.syncManager.Trackers()
}

func (c *Client) supportsTrackerInclude() bool {
	if c.trackerManager() == nil {
		return false
	}

	// Check if the underlying client supports tracker include
	return c.trackerManager().SupportsIncludeTrackers()
}

func (c *Client) hydrateTorrentsWithTrackers(ctx context.Context, torrents []qbt.Torrent, allowFetch bool) ([]qbt.Torrent, map[string][]qbt.TorrentTracker, []string, error) {
	// Call sync before getting trackers
	if c.syncManager != nil {
		if err := c.syncManager.Sync(ctx); err != nil {
			return torrents, nil, nil, fmt.Errorf("failed to sync: %w", err)
		}
	}

	tm := c.trackerManager()
	if tm == nil {
		return torrents, nil, nil, fmt.Errorf("tracker manager unavailable")
	}

	enriched, trackerData := tm.HydrateTorrents(ctx, torrents)
	return enriched, trackerData, nil, nil
}

func (c *Client) invalidateTrackerCache(hashes ...string) {
	if tm := c.trackerManager(); tm != nil {
		tm.Invalidate(hashes...)
	}
}

func (c *Client) StartSyncManager(ctx context.Context) error {
	c.mu.RLock()
	syncManager := c.syncManager
	c.mu.RUnlock()

	if syncManager == nil {
		return fmt.Errorf("sync manager not initialized")
	}

	return syncManager.Start(ctx)
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
	log.Debug().Int("instanceID", c.instanceID).Str("action", action).Int("hashCount", len(hashes)).Msg("Starting optimistic cache update")

	now := time.Now()

	// Apply optimistic updates based on action using sync manager data
	for _, hash := range hashes {
		var originalState qbt.TorrentState
		var progress float64

		// Need mutex only for syncManager access
		c.mu.RLock()
		if c.syncManager != nil {
			if torrent, exists := c.syncManager.GetTorrent(hash); exists {
				originalState = torrent.State
				progress = torrent.Progress
			}
		}
		c.mu.RUnlock()

		state := getTargetState(action, progress)
		if state != "" && state != originalState {
			c.optimisticUpdates.Set(hash, &OptimisticTorrentUpdate{
				State:         state,
				OriginalState: originalState,
				UpdatedAt:     now,
				Action:        action,
			}, 30*time.Second)
			log.Debug().Int("instanceID", c.instanceID).Str("hash", hash).Str("action", action).Msg("Created optimistic update for " + action)
		}
	}

	log.Debug().Int("instanceID", c.instanceID).Str("action", action).Int("hashCount", len(hashes)).Msg("Completed optimistic cache update")
}

// addTrackerExclusions records hashes that should be temporarily excluded from a tracker domain.
func (c *Client) addTrackerExclusions(domain string, hashes []string) {
	if domain == "" || len(hashes) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	set, ok := c.trackerExclusions[domain]
	if !ok {
		set = make(map[string]struct{})
		c.trackerExclusions[domain] = set
	}

	for _, hash := range hashes {
		if hash == "" {
			continue
		}
		set[hash] = struct{}{}
	}
}

// removeTrackerExclusions removes specific hashes from the exclusion map for a domain.
// If no hashes are provided, the entire domain entry is cleared.
func (c *Client) removeTrackerExclusions(domain string, hashes []string) {
	if domain == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(hashes) == 0 {
		delete(c.trackerExclusions, domain)
		return
	}

	set, ok := c.trackerExclusions[domain]
	if !ok {
		return
	}

	for _, hash := range hashes {
		delete(set, hash)
	}

	if len(set) == 0 {
		delete(c.trackerExclusions, domain)
	}
}

// getTrackerExclusionsCopy returns a deep copy of tracker exclusions for safe iteration.
func (c *Client) getTrackerExclusionsCopy() map[string]map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.trackerExclusions) == 0 {
		return nil
	}

	copyMap := make(map[string]map[string]struct{}, len(c.trackerExclusions))
	for domain, hashes := range c.trackerExclusions {
		inner := make(map[string]struct{}, len(hashes))
		for hash := range hashes {
			inner[hash] = struct{}{}
		}
		copyMap[domain] = inner
	}
	return copyMap
}

// clearTrackerExclusions removes domains from the temporary exclusion map.
func (c *Client) clearTrackerExclusions(domains []string) {
	if len(domains) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, domain := range domains {
		delete(c.trackerExclusions, domain)
	}
}

// getOptimisticUpdates returns all current optimistic updates
func (c *Client) getOptimisticUpdates() map[string]*OptimisticTorrentUpdate {
	updates := make(map[string]*OptimisticTorrentUpdate)
	for _, key := range c.optimisticUpdates.GetKeys() {
		if val, found := c.optimisticUpdates.Get(key); found {
			updates[key] = val
		}
	}
	return updates
}

// clearOptimisticUpdate removes an optimistic update for a specific torrent
func (c *Client) clearOptimisticUpdate(hash string) {
	c.optimisticUpdates.Delete(hash)
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
