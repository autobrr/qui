// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

var (
	setTagsMinVersion          = semver.MustParse("2.11.4")
	torrentCreationMinVersion  = semver.MustParse("2.11.2")
	exportTorrentMinVersion    = semver.MustParse("2.8.11")
	trackerEditingMinVersion   = semver.MustParse("2.2.0")
	trackerIncludeMinVersion   = semver.MustParse("2.11.4")
	filePriorityMinVersion     = semver.MustParse("2.2.0")
	renameTorrentMinVersion    = semver.MustParse("2.0.0")
	renameFileMinVersion       = semver.MustParse("2.4.0")
	renameFolderMinVersion     = semver.MustParse("2.7.0")
	subcategoriesMinVersion    = semver.MustParse("2.9.0")
	torrentTmpPathMinVersion   = semver.MustParse("2.8.4")
	pathAutocompleteMinVersion = semver.MustParse("2.11.2")
	rssSetFeedURLMinVersion    = semver.MustParse("2.9.1")
)

type Client struct {
	*qbt.Client
	instanceID               int
	host                     string
	basicUsername            string
	basicPassword            string
	webAPIVersion            string
	supportsSetTags          bool
	supportsTorrentCreation  bool
	supportsTorrentExport    bool
	supportsTrackerEditing   bool
	supportsRenameTorrent    bool
	supportsRenameFile       bool
	supportsRenameFolder     bool
	supportsFilePriority     bool
	supportsSubcategories    bool
	supportsTorrentTmpPath   bool
	supportsPathAutocomplete bool
	trackerIncludeSupported  bool
	supportsSetRSSFeedURL    bool
	lastHealthCheck          time.Time
	lastRecoveryTime         time.Time // When client transitioned from unhealthy→healthy (or was created)
	isHealthy                bool
	syncManager              *qbt.SyncManager
	peerSyncManager          map[string]*qbt.PeerSyncManager // Map of torrent hash to PeerSyncManager
	// optimisticUpdates stores temporary optimistic state changes for this instance
	optimisticUpdates *ttlcache.Cache[string, *OptimisticTorrentUpdate]
	trackerExclusions map[string]map[string]struct{} // Domains to hide hashes from until fresh sync arrives
	lastServerState   *qbt.ServerState
	mu                sync.RWMutex
	serverStateMu     sync.RWMutex
	healthMu          sync.RWMutex
	completionMu      sync.Mutex
	completionState   map[string]bool
	completionHandler TorrentCompletionHandler
	completionInit    bool
	addedMu           sync.Mutex
	addedState        map[string]struct{}
	addedHandler      TorrentAddedHandler
	addedInit         bool
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

	var basicUser string
	var basicPass string
	if basicUsername != nil && *basicUsername != "" {
		cfg.BasicUser = *basicUsername
		basicUser = *basicUsername
		if basicPassword != nil {
			cfg.BasicPass = *basicPassword
			basicPass = *basicPassword
		}
	}

	qbtClient := qbt.NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := qbtClient.LoginCtx(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent instance: %w", err)
	}

	client := &Client{
		Client:           qbtClient,
		instanceID:       instanceID,
		host:             strings.TrimRight(instanceHost, "/"),
		basicUsername:    basicUser,
		basicPassword:    basicPass,
		lastHealthCheck:  time.Now(),
		lastRecoveryTime: time.Now(), // Treat fresh client as "just recovered"
		isHealthy:        true,
		optimisticUpdates: ttlcache.New(ttlcache.Options[string, *OptimisticTorrentUpdate]{}.
			SetDefaultTTL(30 * time.Second)), // Updates expire after 30 seconds
		trackerExclusions: make(map[string]map[string]struct{}),
		peerSyncManager:   make(map[string]*qbt.PeerSyncManager),
		completionState:   make(map[string]bool),
		addedState:        make(map[string]struct{}),
	}

	if err := client.RefreshCapabilities(ctx); err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("host", instanceHost).
			Msg("Failed to refresh qBittorrent capabilities during client creation")
		client.updateHealthStatus(false)
	} else {
		client.updateHealthStatus(true)
	}

	// Initialize sync manager with default options
	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true

	// Set up health check callbacks
	syncOpts.OnUpdate = func(data *qbt.MainData) {
		client.updateHealthStatus(true)
		client.updateServerState(data)
		client.handleCompletionUpdates(data)
		client.handleAddedUpdates(data)
		log.Trace().Int("instanceID", instanceID).Int("torrentCount", len(data.Torrents)).Msg("Sync manager update received, marking client as healthy")
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
		Str("webAPIVersion", client.GetWebAPIVersion()).
		Bool("supportsSetTags", client.SupportsSetTags()).
		Bool("supportsTorrentCreation", client.SupportsTorrentCreation()).
		Bool("supportsTorrentExport", client.SupportsTorrentExport()).
		Bool("supportsTrackerEditing", client.SupportsTrackerEditing()).
		Bool("supportsFilePriority", client.SupportsFilePriority()).
		Bool("supportsSubcategories", client.SupportsSubcategories()).
		Bool("tlsSkipVerify", tlsSkipVerify).
		Msg("qBittorrent client created successfully")

	return client, nil
}

// ReannounceTrackersCtx forces a reannounce for a torrent and optionally limits the request
// to specific tracker URLs.
//
// qBittorrent accepts a pipe-separated list of hashes and a pipe-separated list of "urls".
// When urls are provided, only those tracker URLs are reannounced. When omitted, it
// reannounces all trackers and also triggers a DHT announce.
//
// We implement this locally (instead of in go-qbittorrent) so we can reannounce specific
// trackers when available, but still work against older versions (extra params are ignored).
func (c *Client) ReannounceTrackersCtx(ctx context.Context, hashes []string, trackerURLs []string) error {
	if c == nil || len(hashes) == 0 {
		return nil
	}

	endpoint := c.host + "/api/v2/torrents/reannounce"
	hv := strings.Join(hashes, "|")

	var body strings.Builder
	body.WriteString("hashes=")
	body.WriteString(url.QueryEscape(hv))

	if len(trackerURLs) > 0 {
		escaped := make([]string, 0, len(trackerURLs))
		seen := make(map[string]struct{}, len(trackerURLs))
		for _, u := range trackerURLs {
			trimmed := strings.TrimSpace(u)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			escaped = append(escaped, url.QueryEscape(trimmed))
		}
		if len(escaped) > 0 {
			body.WriteString("&urls=")
			// Do not escape the pipe separator; the API splits on '|'.
			body.WriteString(strings.Join(escaped, "|"))
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body.String()))
	if err != nil {
		return fmt.Errorf("reannounce: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.basicUsername != "" {
		req.SetBasicAuth(c.basicUsername, c.basicPassword)
	}

	resp, err := c.GetHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("reannounce: request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("reannounce: unexpected status code: %d", resp.StatusCode)
	}
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

	// Track recovery time when transitioning to healthy
	if healthy && !c.isHealthy {
		c.lastRecoveryTime = time.Now()
	}

	c.isHealthy = healthy
	c.lastHealthCheck = time.Now()
}

func (c *Client) IsHealthy() bool {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.isHealthy
}

func (c *Client) GetLastRecoveryTime() time.Time {
	c.healthMu.RLock()
	defer c.healthMu.RUnlock()
	return c.lastRecoveryTime
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

// RefreshCapabilities fetches the latest WebAPI version information and recalculates feature support flags.
func (c *Client) RefreshCapabilities(ctx context.Context) error {
	version, err := c.Client.GetWebAPIVersionCtx(ctx)
	if err != nil {
		return err
	}

	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("web API version is empty")
	}

	c.mu.Lock()
	previousVersion := c.webAPIVersion
	c.applyCapabilitiesLocked(version)
	supportsInclude := c.trackerIncludeSupported
	c.mu.Unlock()

	// Update TrackerManager's include capability
	if tm := c.trackerManager(); tm != nil {
		tm.SetUseIncludeTrackers(supportsInclude)
	}

	if previousVersion != "" && previousVersion != version {
		log.Info().
			Int("instanceID", c.instanceID).
			Str("previousVersion", previousVersion).
			Str("newVersion", version).
			Msg("qBittorrent version changed, refreshed capabilities")
	}

	return nil
}

func (c *Client) applyCapabilitiesLocked(version string) {
	c.webAPIVersion = version

	v, err := semver.NewVersion(version)
	if err != nil {
		log.Warn().
			Int("instanceID", c.instanceID).
			Str("webAPIVersion", version).
			Err(err).
			Msg("Failed to parse qBittorrent WebAPI version; leaving capability flags unchanged")
		return
	}

	c.supportsSetTags = !v.LessThan(setTagsMinVersion)
	c.supportsTorrentCreation = !v.LessThan(torrentCreationMinVersion)
	c.supportsTorrentExport = !v.LessThan(exportTorrentMinVersion)
	c.supportsTrackerEditing = !v.LessThan(trackerEditingMinVersion)
	c.trackerIncludeSupported = !v.LessThan(trackerIncludeMinVersion)
	c.supportsFilePriority = !v.LessThan(filePriorityMinVersion)
	c.supportsRenameTorrent = !v.LessThan(renameTorrentMinVersion)
	c.supportsRenameFile = !v.LessThan(renameFileMinVersion)
	c.supportsRenameFolder = !v.LessThan(renameFolderMinVersion)
	c.supportsSubcategories = !v.LessThan(subcategoriesMinVersion)
	c.supportsTorrentTmpPath = !v.LessThan(torrentTmpPathMinVersion)
	c.supportsPathAutocomplete = !v.LessThan(pathAutocompleteMinVersion)
	c.supportsSetRSSFeedURL = !v.LessThan(rssSetFeedURLMinVersion)
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

func (c *Client) SupportsFilePriority() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsFilePriority
}

func (c *Client) SupportsSubcategories() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsSubcategories
}

func (c *Client) SupportsTorrentTmpPath() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsTorrentTmpPath
}

func (c *Client) SupportsPathAutocomplete() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsPathAutocomplete
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
	if c.IsHealthy() && time.Now().Add(-minHealthCheckInterval).Before(c.GetLastHealthCheck()) {
		return nil
	}

	if err := c.RefreshCapabilities(ctx); err != nil {
		c.updateHealthStatus(false)
		return errors.Wrap(err, "health check failed")
	}

	c.updateHealthStatus(true)
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

func (c *Client) SupportsSetRSSFeedURL() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsSetRSSFeedURL
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.trackerIncludeSupported
}

func (c *Client) hydrateTorrentsWithTrackers(ctx context.Context, torrents []qbt.Torrent) ([]qbt.Torrent, map[string][]qbt.TorrentTracker, []string, error) {
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

// SetTorrentCompletionHandler registers a callback to be invoked when torrents finish downloading.
func (c *Client) SetTorrentCompletionHandler(handler TorrentCompletionHandler) {
	c.completionMu.Lock()
	c.completionHandler = handler
	if c.completionState == nil {
		c.completionState = make(map[string]bool)
	}
	c.completionMu.Unlock()
}

// SetTorrentAddedHandler registers a callback to be invoked when torrents are first seen as new.
func (c *Client) SetTorrentAddedHandler(handler TorrentAddedHandler) {
	c.addedMu.Lock()
	c.addedHandler = handler
	if c.addedState == nil {
		c.addedState = make(map[string]struct{})
	}
	c.addedMu.Unlock()
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

const torrentAddedGraceWindow = 60 * time.Second

func (c *Client) handleCompletionUpdates(data *qbt.MainData) {
	if data == nil {
		return
	}

	c.completionMu.Lock()
	if c.completionState == nil {
		c.completionState = make(map[string]bool)
	}

	handler := c.completionHandler

	for _, removed := range data.TorrentsRemoved {
		delete(c.completionState, normalizeHashForCompletion(removed))
	}

	if !c.completionInit {
		if len(data.Torrents) == 0 {
			c.completionMu.Unlock()
			return
		}
		for hash, torrent := range data.Torrents {
			normalized := normalizeHashForCompletion(hash)
			c.completionState[normalized] = isTorrentComplete(&torrent)
		}
		c.completionInit = true
		c.completionMu.Unlock()
		return
	}

	ready := make([]qbt.Torrent, 0)
	for hash, torrent := range data.Torrents {
		normalized := normalizeHashForCompletion(hash)
		alreadyHandled := c.completionState[normalized]
		isComplete := isTorrentComplete(&torrent)

		if !alreadyHandled && isComplete {
			c.completionState[normalized] = true
			ready = append(ready, torrent)
			continue
		}

		if !isComplete && !alreadyHandled {
			c.completionState[normalized] = false
		}
	}
	c.completionMu.Unlock()

	if handler == nil || len(ready) == 0 {
		return
	}

	for _, torrent := range ready {
		torrentCopy := torrent
		go handler(context.Background(), c.instanceID, torrentCopy)
	}
}

func normalizeHashForCompletion(hash string) string {
	return strings.ToUpper(strings.TrimSpace(hash))
}

func (c *Client) handleAddedUpdates(data *qbt.MainData) {
	if data == nil {
		return
	}

	c.addedMu.Lock()
	if c.addedState == nil {
		c.addedState = make(map[string]struct{})
	}

	handler := c.addedHandler

	for _, removed := range data.TorrentsRemoved {
		delete(c.addedState, normalizeHashForCompletion(removed))
	}

	if !c.addedInit {
		if len(data.Torrents) == 0 {
			c.addedMu.Unlock()
			return
		}
		for hash := range data.Torrents {
			c.addedState[normalizeHashForCompletion(hash)] = struct{}{}
		}
		c.addedInit = true
		c.addedMu.Unlock()
		return
	}

	ready := make([]qbt.Torrent, 0)
	for hash, torrent := range data.Torrents {
		normalized := normalizeHashForCompletion(hash)
		if _, ok := c.addedState[normalized]; ok {
			continue
		}
		c.addedState[normalized] = struct{}{}
		ready = append(ready, torrent)
	}
	c.addedMu.Unlock()

	if handler == nil || len(ready) == 0 {
		return
	}

	now := time.Now()
	for _, torrent := range ready {
		if torrent.AddedOn > 0 {
			addedAt := time.Unix(torrent.AddedOn, 0)
			if now.Sub(addedAt) > torrentAddedGraceWindow {
				continue
			}
		}
		torrentCopy := torrent
		go handler(context.Background(), c.instanceID, torrentCopy)
	}
}

func isTorrentComplete(t *qbt.Torrent) bool {
	if t == nil {
		return false
	}

	return t.CompletionOn > 0
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
