// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	trackerCacheTTL            = 30 * time.Minute
	trackerFetchChunkDefault   = 300
	trackerFetchChunkForCounts = 300
	trackerWarmupDelay         = 2 * time.Second
	trackerWarmupTimeout       = 45 * time.Second
	trackerWarmupBatchSize     = 1000
)

type Client struct {
	*qbt.Client
	instanceID      int
	webAPIVersion   string
	supportsSetTags bool
	includeTrackers bool
	lastHealthCheck time.Time
	isHealthy       bool
	syncManager     *qbt.SyncManager
	peerSyncManager map[string]*qbt.PeerSyncManager // Map of torrent hash to PeerSyncManager
	// optimisticUpdates stores temporary optimistic state changes for this instance
	optimisticUpdates    *ttlcache.Cache[string, *OptimisticTorrentUpdate]
	trackerExclusions    map[string]map[string]struct{} // Domains to hide hashes from until fresh sync arrives
	trackerCache         *ttlcache.Cache[string, []qbt.TorrentTracker]
	trackerFetcher       *qbt.TrackerFetcher
	trackerWarmupMu      sync.Mutex
	trackerWarmupPending map[string]struct{}
	mu                   sync.RWMutex
	healthMu             sync.RWMutex
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
	includeTrackers := false
	if webAPIVersion != "" {
		if v, err := semver.NewVersion(webAPIVersion); err == nil {
			required := semver.MustParse("2.11.4")
			supportsSetTags = !v.LessThan(required)
			includeTrackers = supportsSetTags
		}
	}

	client := &Client{
		Client:          qbtClient,
		instanceID:      instanceID,
		webAPIVersion:   webAPIVersion,
		supportsSetTags: supportsSetTags,
		includeTrackers: includeTrackers,
		lastHealthCheck: time.Now(),
		isHealthy:       true,
		optimisticUpdates: ttlcache.New(ttlcache.Options[string, *OptimisticTorrentUpdate]{}.
			SetDefaultTTL(30 * time.Second)), // Updates expire after 30 seconds
		trackerExclusions: make(map[string]map[string]struct{}),
		peerSyncManager:   make(map[string]*qbt.PeerSyncManager),
		trackerCache: ttlcache.New(ttlcache.Options[string, []qbt.TorrentTracker]{}.
			SetDefaultTTL(trackerCacheTTL)),
		trackerWarmupPending: make(map[string]struct{}),
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
		Bool("includeTrackers", includeTrackers).
		Bool("tlsSkipVerify", tlsSkipVerify).
		Msg("qBittorrent client created successfully")

	if !includeTrackers {
		log.Debug().
			Int("instanceID", instanceID).
			Str("host", instanceHost).
			Str("webAPIVersion", webAPIVersion).
			Msg("qBittorrent instance does not support includeTrackers; using fallback tracker queries for status detection")
	}

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

func (c *Client) GetSyncManager() *qbt.SyncManager {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.syncManager
}

func (c *Client) getTrackersForHashes(ctx context.Context, hashes []string, allowFetch bool, fetchLimit int) (map[string][]qbt.TorrentTracker, []string, error) {
	c.mu.RLock()
	cache := c.trackerCache
	c.mu.RUnlock()

	if cache == nil || len(hashes) == 0 {
		return map[string][]qbt.TorrentTracker{}, nil, nil
	}

	deduped := deduplicateHashes(hashes)
	results := make(map[string][]qbt.TorrentTracker, len(deduped))
	missing := make([]string, 0, len(deduped))

	for _, hash := range deduped {
		if trackers, ok := cache.Get(hash); ok {
			results[hash] = trackers
			continue
		}
		missing = append(missing, hash)
	}

	if !allowFetch || len(missing) == 0 {
		return results, missing, nil
	}

	toFetch := missing
	remaining := make([]string, 0)
	if fetchLimit > 0 && len(toFetch) > fetchLimit {
		remaining = append(remaining, toFetch[fetchLimit:]...)
		toFetch = toFetch[:fetchLimit]
	}

	fetched, err := c.fetchAndCacheTrackers(ctx, toFetch)
	for hash, trackers := range fetched {
		results[hash] = trackers
	}

	for _, hash := range toFetch {
		if _, ok := fetched[hash]; !ok {
			remaining = append(remaining, hash)
		}
	}

	return results, remaining, err
}

func (c *Client) fetchAndCacheTrackers(ctx context.Context, hashes []string) (map[string][]qbt.TorrentTracker, error) {
	hashes = deduplicateHashes(hashes)
	if len(hashes) == 0 {
		return map[string][]qbt.TorrentTracker{}, nil
	}

	var (
		fetched map[string][]qbt.TorrentTracker
		err     error
	)

	if c.includeTrackers {
		fetched, err = c.fetchTrackersViaInclude(ctx, hashes)
	} else {
		fetcher := c.ensureTrackerFetcher()
		fetched, err = fetcher.Fetch(ctx, hashes)
	}

	if len(fetched) > 0 {
		c.mu.RLock()
		cache := c.trackerCache
		c.mu.RUnlock()
		if cache != nil {
			for hash, trackers := range fetched {
				cache.Set(hash, trackers, ttlcache.DefaultTTL)
			}
		}
	}

	return fetched, err
}

func (c *Client) fetchTrackersViaInclude(ctx context.Context, hashes []string) (map[string][]qbt.TorrentTracker, error) {
	if len(hashes) == 0 {
		return map[string][]qbt.TorrentTracker{}, nil
	}

	result := make(map[string][]qbt.TorrentTracker, len(hashes))
	const chunkSize = 50
	var firstErr error

	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}

		opts := qbt.TorrentFilterOptions{Hashes: hashes[start:end], IncludeTrackers: true}
		torrents, err := c.Client.GetTorrentsCtx(ctx, opts)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		for _, torrent := range torrents {
			result[torrent.Hash] = torrent.Trackers
		}
		for _, hash := range opts.Hashes {
			if _, ok := result[hash]; !ok {
				result[hash] = nil
			}
		}
	}

	return result, firstErr
}

func (c *Client) ensureTrackerFetcher() *qbt.TrackerFetcher {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.trackerFetcher == nil {
		c.trackerFetcher = qbt.NewTrackerFetcher(c.Client, qbt.WithTrackerFetcherConcurrency(4))
	}
	return c.trackerFetcher
}

func (c *Client) scheduleTrackerWarmup(hashes []string, batchSize int) {
	hashes = deduplicateHashes(hashes)
	if len(hashes) == 0 {
		return
	}

	c.mu.RLock()
	cache := c.trackerCache
	c.mu.RUnlock()

	pending := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if cache != nil {
			if _, ok := cache.Get(hash); ok {
				continue
			}
		}
		pending = append(pending, hash)
	}

	if len(pending) == 0 {
		return
	}

	c.trackerWarmupMu.Lock()
	filtered := make([]string, 0, len(pending))
	for _, hash := range pending {
		if _, exists := c.trackerWarmupPending[hash]; exists {
			continue
		}
		c.trackerWarmupPending[hash] = struct{}{}
		filtered = append(filtered, hash)
	}
	c.trackerWarmupMu.Unlock()

	if len(filtered) == 0 {
		return
	}

	if batchSize <= 0 {
		batchSize = trackerWarmupBatchSize
	}

	c.orderHashesByAddedOnDesc(filtered)

	go c.runTrackerWarmup(filtered, batchSize)
}

func (c *Client) runTrackerWarmup(hashes []string, batchSize int) {
	defer func() {
		c.trackerWarmupMu.Lock()
		for _, hash := range hashes {
			delete(c.trackerWarmupPending, hash)
		}
		c.trackerWarmupMu.Unlock()
	}()

	for start := 0; start < len(hashes); start += batchSize {
		end := start + batchSize
		if end > len(hashes) {
			end = len(hashes)
		}

		batch := hashes[start:end]
		ctx, cancel := context.WithTimeout(context.Background(), trackerWarmupTimeout)
		if _, err := c.fetchAndCacheTrackers(ctx, batch); err != nil {
			log.Debug().Err(err).Int("batch", len(batch)).Msg("tracker warmup batch failed")
		}
		cancel()

		if end < len(hashes) {
			time.Sleep(trackerWarmupDelay)
		}
	}
}

func (c *Client) orderHashesByAddedOnDesc(hashes []string) {
	if len(hashes) <= 1 {
		return
	}

	torrents := c.getTorrentsByHashes(hashes)
	if len(torrents) == 0 {
		return
	}

	addedOnByHash := make(map[string]int64, len(torrents))
	for _, torrent := range torrents {
		addedOnByHash[torrent.Hash] = torrent.AddedOn
	}

	sort.SliceStable(hashes, func(i, j int) bool {
		left := addedOnByHash[hashes[i]]
		right := addedOnByHash[hashes[j]]
		if left == right {
			return hashes[i] < hashes[j]
		}
		return left > right
	})
}

func deduplicateHashes(hashes []string) []string {
	if len(hashes) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(hashes))
	unique := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		unique = append(unique, hash)
	}

	return unique
}

func (c *Client) invalidateTrackerCache(hashes ...string) {
	c.mu.RLock()
	cache := c.trackerCache
	c.mu.RUnlock()

	if cache == nil {
		return
	}

	if len(hashes) == 0 {
		for _, key := range cache.GetKeys() {
			if key == "" {
				continue
			}
			cache.Delete(key)
		}
		return
	}

	for _, hash := range hashes {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			continue
		}
		cache.Delete(hash)
	}
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
