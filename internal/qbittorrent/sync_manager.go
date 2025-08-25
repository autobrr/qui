// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/dgraph-io/ristretto"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rs/zerolog/log"
)

// CacheMetadata provides information about cache state
type CacheMetadata struct {
	Source      string    `json:"source"`      // "cache" or "fresh"
	Age         int       `json:"age"`         // Age in seconds
	IsStale     bool      `json:"isStale"`     // Whether data is stale
	NextRefresh time.Time `json:"nextRefresh"` // When next refresh will occur
}

// TorrentResponse represents a response containing torrents with stats and cache metadata
type TorrentResponse struct {
	Torrents      []qbt.Torrent           `json:"torrents"`
	Total         int                     `json:"total"`
	Stats         *TorrentStats           `json:"stats,omitempty"`
	Counts        *TorrentCounts          `json:"counts,omitempty"`        // Include counts for sidebar
	Categories    map[string]qbt.Category `json:"categories,omitempty"`    // Include categories for sidebar
	Tags          []string                `json:"tags,omitempty"`          // Include tags for sidebar
	CacheMetadata *CacheMetadata          `json:"cacheMetadata,omitempty"` // Cache state information
	HasMore       bool                    `json:"hasMore"`                 // Whether more pages are available
	SessionID     string                  `json:"sessionId,omitempty"`     // Optional session tracking
}

// TorrentStats represents aggregated torrent statistics
type TorrentStats struct {
	Total              int `json:"total"`
	Downloading        int `json:"downloading"`
	Seeding            int `json:"seeding"`
	Paused             int `json:"paused"`
	Error              int `json:"error"`
	Checking           int `json:"checking"`
	TotalDownloadSpeed int `json:"totalDownloadSpeed"`
	TotalUploadSpeed   int `json:"totalUploadSpeed"`
}

// SyncManager manages torrent operations and caching
type SyncManager struct {
	clientPool *ClientPool
	cache      *ristretto.Cache
}

// NewSyncManager creates a new sync manager
func NewSyncManager(clientPool *ClientPool) *SyncManager {
	return &SyncManager{
		clientPool: clientPool,
		cache:      clientPool.GetCache(),
	}
}

// GetTorrentsWithFilters gets torrents with filters, search, sorting, and pagination
// Implements stale-while-revalidate pattern for responsive UI
func (sm *SyncManager) GetTorrentsWithFilters(ctx context.Context, instanceID int, limit, offset int, sort, order, search string, filters FilterOptions) (*TorrentResponse, error) {
	// No longer caching filtered results - always compute from all_torrents cache
	// This ensures optimistic updates are always reflected
	var filteredTorrents []qbt.Torrent
	var err error

	// Check if data is in cache first to determine source
	// Must match the exact key format used in getAllTorrentsForStats
	cacheKey := fmt.Sprintf("all_torrents:%d:%s", instanceID, "")
	_, isFromCache := sm.cache.Get(cacheKey)

	// Always try to use getAllTorrentsForStats as the single source of truth
	// It has smart dynamic caching based on response time
	allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("totalCount", len(allTorrents)).
		Bool("fromCache", isFromCache).
		Msg("Using getAllTorrentsForStats as single source")

	// Now we always have all torrents from getAllTorrentsForStats
	// Just apply filters in-memory (this is fast and uses the smart cached data)
	filteredTorrents = sm.applyFilters(allTorrents, filters)

	// Apply search filter if provided
	if search != "" {
		filteredTorrents = sm.filterTorrentsBySearch(filteredTorrents, search)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("filtered", len(filteredTorrents)).
		Msg("Applied in-memory filtering")

	// Calculate stats from filtered torrents
	stats := sm.calculateStats(filteredTorrents)

	// Sort torrents before pagination
	sm.sortTorrents(filteredTorrents, sort, order)

	// Apply pagination to filtered results
	var paginatedTorrents []qbt.Torrent
	start := offset
	end := offset + limit
	if start < len(filteredTorrents) {
		if end > len(filteredTorrents) {
			end = len(filteredTorrents)
		}
		paginatedTorrents = filteredTorrents[start:end]
	}

	// Check if there are more pages
	hasMore := end < len(filteredTorrents)

	// Calculate counts from ALL torrents (not filtered) for sidebar
	// This uses the same cached data, so it's very fast
	counts := sm.calculateCountsFromTorrents(allTorrents)

	// Fetch categories and tags (cached separately for 60s)
	categories, err := sm.GetCategories(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get categories")
		categories = make(map[string]qbt.Category)
	}

	tags, err := sm.GetTags(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get tags")
		tags = []string{}
	}

	// Set cache metadata based on whether data came from cache
	cacheSource := "fresh"
	if isFromCache {
		cacheSource = "cache"
	}

	response := &TorrentResponse{
		Torrents:   paginatedTorrents,
		Total:      len(filteredTorrents),
		Stats:      stats,
		Counts:     counts,     // Include counts for sidebar
		Categories: categories, // Include categories for sidebar
		Tags:       tags,       // Include tags for sidebar
		HasMore:    hasMore,
		CacheMetadata: &CacheMetadata{
			Source:  cacheSource,
			Age:     0,     // TODO: Track actual cache age if needed
			IsStale: false, // With our current design, cached data is never stale
		},
	}

	// Don't cache filtered results - always compute from all_torrents cache
	// This ensures optimistic updates are always reflected
	// The all_torrents cache is the single source of truth

	log.Debug().
		Int("instanceID", instanceID).
		Int("count", len(paginatedTorrents)).
		Int("total", len(filteredTorrents)).
		Str("search", search).
		Interface("filters", filters).
		Bool("hasMore", hasMore).
		Msg("Fresh torrent data fetched and cached")

	return response, nil
}

// GetServerStats gets server statistics using SyncMainData (for Dashboard)
func (sm *SyncManager) GetServerStats(ctx context.Context, instanceID int) (*qbt.MainData, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync data with rid=0 to just fetch server state
	// We only need server_state for Dashboard statistics
	mainData, err := client.SyncMainDataCtx(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get server stats: %w", err)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Msg("Server stats fetched")

	return mainData, nil
}

// BulkAction performs bulk operations on torrents
func (sm *SyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Perform action based on type
	switch action {
	case "pause":
		err = client.PauseCtx(ctx, hashes)
		if err == nil {
			sm.applyOptimisticCacheUpdate(instanceID, hashes, action, nil)
		}
	case "resume":
		err = client.ResumeCtx(ctx, hashes)
		if err == nil {
			sm.applyOptimisticCacheUpdate(instanceID, hashes, action, nil)
		}
	case "delete":
		err = client.DeleteTorrentsCtx(ctx, hashes, false)
		if err == nil {
			sm.applyOptimisticCacheUpdate(instanceID, hashes, action, nil)
		}
	case "deleteWithFiles":
		err = client.DeleteTorrentsCtx(ctx, hashes, true)
		if err == nil {
			sm.applyOptimisticCacheUpdate(instanceID, hashes, "delete", nil)
		}
	case "recheck":
		err = client.RecheckCtx(ctx, hashes)
		if err == nil {
			sm.applyOptimisticCacheUpdate(instanceID, hashes, action, nil)
		}
	case "reannounce":
		// No cache update needed - no visible state change
		err = client.ReAnnounceTorrentsCtx(ctx, hashes)
	case "increasePriority", "decreasePriority", "topPriority", "bottomPriority":
		// Priority changes - no cache update needed as priority not shown
		switch action {
		case "increasePriority":
			err = client.IncreasePriorityCtx(ctx, hashes)
		case "decreasePriority":
			err = client.DecreasePriorityCtx(ctx, hashes)
		case "topPriority":
			err = client.SetMaxPriorityCtx(ctx, hashes)
		case "bottomPriority":
			err = client.SetMinPriorityCtx(ctx, hashes)
		}
	default:
		return fmt.Errorf("unknown bulk action: %s", action)
	}

	return err
}

// AddTorrent adds a new torrent from file content
func (sm *SyncManager) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Use AddTorrentFromMemoryCtx which accepts byte array
	return client.AddTorrentFromMemoryCtx(ctx, fileContent, options)
}

// AddTorrentFromURLs adds new torrents from URLs or magnet links
func (sm *SyncManager) AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Add each URL/magnet link
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}

		if err := client.AddTorrentFromUrlCtx(ctx, url, options); err != nil {
			return fmt.Errorf("failed to add torrent from URL %s: %w", url, err)
		}
	}

	return nil
}

// GetCategories gets all categories
func (sm *SyncManager) GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error) {
	// Check cache
	cacheKey := fmt.Sprintf("categories:%d", instanceID)
	if cached, found := sm.cache.Get(cacheKey); found {
		if categories, ok := cached.(map[string]qbt.Category); ok {
			return categories, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get categories
	categories, err := client.GetCategoriesCtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get categories: %w", err)
	}

	// Cache for 1 minute
	sm.cache.SetWithTTL(cacheKey, categories, 1, 60*time.Second)

	return categories, nil
}

// GetTags gets all tags
func (sm *SyncManager) GetTags(ctx context.Context, instanceID int) ([]string, error) {
	// Check cache
	cacheKey := fmt.Sprintf("tags:%d", instanceID)
	if cached, found := sm.cache.Get(cacheKey); found {
		if tags, ok := cached.([]string); ok {
			return tags, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get tags
	tags, err := client.GetTagsCtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags: %w", err)
	}

	slices.SortFunc(tags, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	// Cache for 1 minute
	sm.cache.SetWithTTL(cacheKey, tags, 1, 60*time.Second)

	return tags, nil
}

// GetTorrentProperties gets detailed properties for a specific torrent
func (sm *SyncManager) GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	// Check cache
	cacheKey := fmt.Sprintf("torrent:properties:%d:%s", instanceID, hash)
	if cached, found := sm.cache.Get(cacheKey); found {
		if props, ok := cached.(*qbt.TorrentProperties); ok {
			return props, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get properties
	props, err := client.GetTorrentPropertiesCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent properties: %w", err)
	}

	// Cache for 30 seconds
	sm.cache.SetWithTTL(cacheKey, &props, 1, 30*time.Second)

	return &props, nil
}

// GetTorrentTrackers gets trackers for a specific torrent
func (sm *SyncManager) GetTorrentTrackers(ctx context.Context, instanceID int, hash string) ([]qbt.TorrentTracker, error) {
	// Check cache
	cacheKey := fmt.Sprintf("torrent:trackers:%d:%s", instanceID, hash)
	if cached, found := sm.cache.Get(cacheKey); found {
		if trackers, ok := cached.([]qbt.TorrentTracker); ok {
			return trackers, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get trackers
	trackers, err := client.GetTorrentTrackersCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent trackers: %w", err)
	}

	// Cache for 30 seconds
	sm.cache.SetWithTTL(cacheKey, trackers, 1, 30*time.Second)

	return trackers, nil
}

// GetTorrentFiles gets files information for a specific torrent
func (sm *SyncManager) GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
	// Check cache
	cacheKey := fmt.Sprintf("torrent:files:%d:%s", instanceID, hash)
	if cached, found := sm.cache.Get(cacheKey); found {
		if files, ok := cached.(*qbt.TorrentFiles); ok {
			return files, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get files
	files, err := client.GetFilesInformationCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

	// Cache for 30 seconds
	sm.cache.SetWithTTL(cacheKey, files, 1, 30*time.Second)

	return files, nil
}

// TorrentCounts represents counts for filtering sidebar
type TorrentCounts struct {
	Status     map[string]int `json:"status"`
	Categories map[string]int `json:"categories"`
	Tags       map[string]int `json:"tags"`
	Trackers   map[string]int `json:"trackers"`
	Total      int            `json:"total"`
}

// InstanceSpeeds represents download/upload speeds for an instance
type InstanceSpeeds struct {
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
}

// calculateCountsFromTorrents calculates counts from a list of torrents
// This is used internally to generate counts without additional API calls
func (sm *SyncManager) calculateCountsFromTorrents(allTorrents []qbt.Torrent) *TorrentCounts {
	// Initialize counts
	counts := &TorrentCounts{
		Status:     make(map[string]int),
		Categories: make(map[string]int),
		Tags:       make(map[string]int),
		Trackers:   make(map[string]int),
		Total:      len(allTorrents),
	}

	// Status counts
	statusFilters := []string{
		"all", "downloading", "seeding", "completed", "paused",
		"active", "inactive", "resumed", "stalled",
		"stalled_uploading", "stalled_downloading", "errored",
		"checking", "moving",
	}

	for _, status := range statusFilters {
		count := 0
		for _, torrent := range allTorrents {
			if sm.matchTorrentStatus(torrent, status) {
				count++
			}
		}
		counts.Status[status] = count
	}

	// Count torrents by category, tag, and tracker
	for _, torrent := range allTorrents {
		// Category count
		category := torrent.Category
		if category == "" {
			counts.Categories[""]++
		} else {
			counts.Categories[category]++
		}

		// Tag counts
		if torrent.Tags == "" {
			counts.Tags[""]++
		} else {
			torrentTags := strings.SplitSeq(torrent.Tags, ", ")
			for tag := range torrentTags {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					counts.Tags[tag]++
				}
			}
		}

		// Tracker count (use first tracker URL)
		if torrent.Tracker != "" {
			// Extract domain from tracker URL
			domain := torrent.Tracker
			if strings.Contains(domain, "://") {
				parts := strings.Split(domain, "://")
				if len(parts) > 1 {
					domain = parts[1]
					if idx := strings.IndexAny(domain, ":/"); idx != -1 {
						domain = domain[:idx]
					}
				}
			}
			counts.Trackers[domain]++
		} else {
			counts.Trackers[""]++
		}
	}

	return counts
}

// GetTorrentCounts gets all torrent counts for the filter sidebar
func (sm *SyncManager) GetTorrentCounts(ctx context.Context, instanceID int) (*TorrentCounts, error) {
	// IMPORTANT: We don't cache counts separately anymore
	// Instead, we ALWAYS derive counts from the same cached torrent data that the table uses
	// This ensures the sidebar and table are always in sync

	log.Debug().Int("instanceID", instanceID).Msg("GetTorrentCounts: fetching from getAllTorrentsForStats")

	// Get all torrents from the same cache the table uses
	// This will use the dynamic TTL (2-60s based on instance speed)
	allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get all torrents for counts: %w", err)
	}

	log.Debug().Int("instanceID", instanceID).Int("torrents", len(allTorrents)).Msg("GetTorrentCounts: got torrents from cache/API")

	// Get categories and tags (cached for 60s)
	categories, err := sm.GetCategories(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get categories for counts")
		categories = make(map[string]qbt.Category)
	}

	tags, err := sm.GetTags(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get tags for counts")
		tags = []string{}
	}

	// Initialize counts
	counts := &TorrentCounts{
		Status:     make(map[string]int),
		Categories: make(map[string]int),
		Tags:       make(map[string]int),
		Trackers:   make(map[string]int),
		Total:      len(allTorrents),
	}

	// Status counts
	statusFilters := []string{
		"all", "downloading", "seeding", "completed", "paused",
		"active", "inactive", "resumed", "stalled",
		"stalled_uploading", "stalled_downloading", "errored",
		"checking", "moving",
	}

	for _, status := range statusFilters {
		count := 0
		for _, torrent := range allTorrents {
			if sm.matchTorrentStatus(torrent, status) {
				count++
			}
		}
		counts.Status[status] = count
	}

	// Initialize all known categories with 0
	for category := range categories {
		counts.Categories[category] = 0
	}
	// Always include uncategorized
	counts.Categories[""] = 0

	// Initialize all known tags with 0
	for _, tag := range tags {
		counts.Tags[tag] = 0
	}
	// Always include untagged
	counts.Tags[""] = 0

	// Count torrents by category, tag, and tracker
	for _, torrent := range allTorrents {
		// Category count
		category := torrent.Category
		if category == "" {
			counts.Categories[""]++
		} else {
			counts.Categories[category]++
		}

		// Tag counts
		if torrent.Tags == "" {
			counts.Tags[""]++
		} else {
			// Handle tags as comma-separated string
			torrentTags := strings.SplitSeq(torrent.Tags, ",")
			for tag := range torrentTags {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					counts.Tags[tag]++
				}
			}
		}

		// Tracker counts
		if torrent.Tracker == "" {
			counts.Trackers[""]++
		} else {
			// Extract hostname from tracker URL
			if trackerURL, err := url.Parse(torrent.Tracker); err == nil {
				hostname := trackerURL.Hostname()
				counts.Trackers[hostname]++
			} else {
				counts.Trackers["Unknown"]++
			}
		}
	}

	// Don't cache counts separately - they're always derived from the cached torrent data
	// This ensures sidebar and table are always in sync

	log.Debug().
		Int("instanceID", instanceID).
		Int("total", counts.Total).
		Int("statusCount", len(counts.Status)).
		Int("categoryCount", len(counts.Categories)).
		Int("tagCount", len(counts.Tags)).
		Int("trackerCount", len(counts.Trackers)).
		Msg("Calculated torrent counts")

	return counts, nil
}

// GetInstanceSpeeds gets total download/upload speeds efficiently using GetTransferInfo
// This is MUCH faster than fetching all torrents for large instances
func (sm *SyncManager) GetInstanceSpeeds(ctx context.Context, instanceID int) (*InstanceSpeeds, error) {
	// Check cache for speeds
	cacheKey := fmt.Sprintf("instance:speeds:%d", instanceID)
	if cached, found := sm.cache.Get(cacheKey); found {
		if speeds, ok := cached.(*InstanceSpeeds); ok {
			log.Debug().Int("instanceID", instanceID).Msg("GetInstanceSpeeds: returning cached speeds")
			return speeds, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Use GetTransferInfo - a lightweight API that returns just global speeds
	// This doesn't fetch any torrents, making it perfect for dashboard stats
	transferInfo, err := client.GetTransferInfoCtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer info: %w", err)
	}

	// Extract speeds from TransferInfo
	speeds := &InstanceSpeeds{
		Download: transferInfo.DlInfoSpeed,
		Upload:   transferInfo.UpInfoSpeed,
	}

	// Cache for 2 seconds (matches torrent data cache)
	sm.cache.SetWithTTL(cacheKey, speeds, 1, 2*time.Second)

	log.Debug().Int("instanceID", instanceID).Int64("download", speeds.Download).Int64("upload", speeds.Upload).Msg("GetInstanceSpeeds: got from GetTransferInfo API")

	return speeds, nil
}

// Helper methods

// InvalidateCache clears all cached data for a specific instance
// NOTE: With optimistic updates, this is rarely needed now
func (sm *SyncManager) InvalidateCache(instanceID int) {
	log.Debug().Int("instanceID", instanceID).Msg("Invalidating cache for instance")

	// Delete specific cache keys for this instance only
	// This prevents affecting other instances' caches
	keysToDelete := []string{
		// All torrents variations
		fmt.Sprintf("all_torrents:%d:", instanceID),  // Empty search
		fmt.Sprintf("all_torrents:%d:*", instanceID), // With search

		// Paginated torrents
		fmt.Sprintf("torrents:%d:", instanceID),

		// Filtered torrents
		fmt.Sprintf("torrents:filtered:%d:", instanceID),
		fmt.Sprintf("torrents:search:%d:", instanceID),
		fmt.Sprintf("native_filtered:%d:", instanceID),

		// Metadata
		fmt.Sprintf("categories:%d", instanceID),
		fmt.Sprintf("tags:%d", instanceID),

		// Individual torrent data
		fmt.Sprintf("torrent:properties:%d:", instanceID),
		fmt.Sprintf("torrent:trackers:%d:", instanceID),
		fmt.Sprintf("torrent:files:%d:", instanceID),
		fmt.Sprintf("torrent:webseeds:%d:", instanceID),
	}

	// Since Ristretto doesn't support wildcard/pattern deletion,
	// we need to be more explicit about what we delete
	for _, keyPrefix := range keysToDelete {
		// Try to delete with common suffixes
		sm.cache.Del(keyPrefix)

		// For paginated results, try to clear first few pages
		if strings.Contains(keyPrefix, "torrents:") {
			for page := range 10 {
				for _, limit := range []int{100, 200, 500, 1000} {
					paginatedKey := fmt.Sprintf("%s%d:%d", keyPrefix, page*limit, limit)
					sm.cache.Del(paginatedKey)
				}
			}
		}

		// For search results, we can't predict all search terms
		// but we can clear common empty searches
		if keyPrefix == fmt.Sprintf("all_torrents:%d:", instanceID) {
			sm.cache.Del(keyPrefix)                                    // Empty search
			sm.cache.Del(fmt.Sprintf("all_torrents:%d: ", instanceID)) // Space
		}
	}

	log.Debug().Int("instanceID", instanceID).Msg("Instance-specific cache invalidation completed")
}

// applyOptimisticCacheUpdate applies optimistic updates to cached torrents
func (sm *SyncManager) applyOptimisticCacheUpdate(instanceID int, hashes []string, action string, payload map[string]any) {
	// Get the cache key for all torrents
	// Since we no longer cache filtered results, updating this cache affects all queries
	cacheKey := fmt.Sprintf("all_torrents:%d:", instanceID)

	// Try to get cached torrents
	if cached, found := sm.cache.Get(cacheKey); found {
		if torrents, ok := cached.([]qbt.Torrent); ok {
			// Create a map for quick hash lookup
			hashMap := make(map[string]bool)
			for _, hash := range hashes {
				hashMap[strings.ToLower(hash)] = true
			}

			var updatedTorrents []qbt.Torrent

			// Handle delete action - filter out deleted torrents
			if action == "delete" {
				for _, torrent := range torrents {
					if !hashMap[strings.ToLower(torrent.Hash)] {
						updatedTorrents = append(updatedTorrents, torrent)
					}
				}
				log.Debug().Int("instanceID", instanceID).Int("deletedCount", len(torrents)-len(updatedTorrents)).Msg("Removed deleted torrents from cache")
			} else {
				// For other actions, update torrent properties
				for _, torrent := range torrents {
					if hashMap[strings.ToLower(torrent.Hash)] {
						// Apply optimistic state change
						switch action {
						case "pause":
							// Set to paused state based on progress
							if torrent.Progress < 1 || strings.Contains(string(torrent.State), "DL") {
								torrent.State = qbt.TorrentStateStoppedDl
							} else {
								torrent.State = qbt.TorrentStateStoppedUp
							}
							// Clear speeds
							torrent.DlSpeed = 0
							torrent.UpSpeed = 0

						case "resume":
							// Set to stalled state initially (will be corrected when real data comes)
							if torrent.Progress < 1 {
								torrent.State = qbt.TorrentStateStalledDl
							} else {
								torrent.State = qbt.TorrentStateStalledUp
							}

						case "recheck":
							// Set to checking state
							if torrent.Progress < 1 {
								torrent.State = qbt.TorrentStateCheckingDl
							} else {
								torrent.State = qbt.TorrentStateCheckingUp
							}
							// Clear speeds during checking
							torrent.DlSpeed = 0
							torrent.UpSpeed = 0

						case "setCategory":
							if category, ok := payload["category"].(string); ok {
								torrent.Category = category
							}

						case "addTags":
							if tags, ok := payload["tags"].(string); ok {
								// Parse current tags
								currentTags := []string{}
								if torrent.Tags != "" {
									currentTags = strings.Split(torrent.Tags, ", ")
								}
								// Add new tags
								newTags := strings.SplitSeq(tags, ",")
								for tag := range newTags {
									tag = strings.TrimSpace(tag)
									if tag != "" && !stringSliceContains(currentTags, tag) {
										currentTags = append(currentTags, tag)
									}
								}
								torrent.Tags = strings.Join(currentTags, ", ")
							}

						case "removeTags":
							if tags, ok := payload["tags"].(string); ok {
								// Parse current tags
								currentTags := []string{}
								if torrent.Tags != "" {
									currentTags = strings.Split(torrent.Tags, ", ")
								}
								// Remove specified tags
								tagsToRemove := strings.Split(tags, ",")
								var remainingTags []string
								for _, tag := range currentTags {
									tag = strings.TrimSpace(tag)
									if !stringSliceContains(tagsToRemove, tag) {
										remainingTags = append(remainingTags, tag)
									}
								}
								torrent.Tags = strings.Join(remainingTags, ", ")
							}

						case "setTags":
							if tags, ok := payload["tags"].(string); ok {
								torrent.Tags = tags
							}

						case "toggleAutoTMM":
							// AutoTMM field might not be available in the Torrent struct
							// This would be updated when fetching fresh data
							// Check if enable parameter exists but don't process it
							_, _ = payload["enable"].(bool)
						}
					}
					updatedTorrents = append(updatedTorrents, torrent)
				}
				log.Debug().Int("instanceID", instanceID).Int("updatedCount", len(hashes)).Str("action", action).Msg("Applied optimistic cache update")
			}

			// Update cache with modified torrents
			// Use a conservative 5 second TTL since we don't know the original TTL
			sm.cache.SetWithTTL(cacheKey, updatedTorrents, 1, 5*time.Second)
		}
	}
}

// stringSliceContains checks if a string slice contains a value
func stringSliceContains(slice []string, value string) bool {
	value = strings.TrimSpace(value)
	for _, item := range slice {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

// getAllTorrentsForStats gets all torrents for stats calculation (cached)
// getNativeFilteredTorrents uses qBittorrent's native filtering for single filter scenarios
// This is much more efficient for large instances as it avoids fetching all torrents

func (sm *SyncManager) getAllTorrentsForStats(ctx context.Context, instanceID int, search string) ([]qbt.Torrent, error) {
	// Use different cache key for search vs no search
	cacheKey := fmt.Sprintf("all_torrents:%d:%s", instanceID, search)
	if cached, found := sm.cache.Get(cacheKey); found {
		if torrents, ok := cached.([]qbt.Torrent); ok {
			log.Debug().Int("instanceID", instanceID).Int("torrents", len(torrents)).Str("cacheKey", cacheKey).Msg("getAllTorrentsForStats: CACHE HIT")
			return torrents, nil
		}
	}

	log.Debug().Int("instanceID", instanceID).Str("cacheKey", cacheKey).Msg("getAllTorrentsForStats: CACHE MISS")

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Measure response time to dynamically adjust cache TTL
	startTime := time.Now()

	log.Debug().Int("instanceID", instanceID).Msg("getAllTorrentsForStats: Fetching from qBittorrent API")

	// Get all torrents
	torrents, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get all torrents: %w", err)
	}

	// Calculate response time
	responseTime := time.Since(startTime)

	log.Debug().Int("instanceID", instanceID).Int("torrents", len(torrents)).Dur("responseTime", responseTime).Msg("getAllTorrentsForStats: Fetched from qBittorrent")

	// Dynamic cache TTL based on actual response time
	// Fast instances: shorter cache for responsiveness
	// Slow instances: longer cache to avoid repeated slow fetches
	var cacheTTL time.Duration
	switch {
	case responseTime > 5*time.Second:
		cacheTTL = 60 * time.Second // Very slow instance, cache for 1 minute
		log.Debug().Dur("responseTime", responseTime).Int("torrents", len(torrents)).Msg("Very slow instance detected, using 60s cache")
	case responseTime > 2*time.Second:
		cacheTTL = 30 * time.Second // Slow instance, cache for 30 seconds
		log.Debug().Dur("responseTime", responseTime).Int("torrents", len(torrents)).Msg("Slow instance detected, using 30s cache")
	case responseTime > 1*time.Second:
		cacheTTL = 15 * time.Second // Moderate speed, cache for 15 seconds
		log.Debug().Dur("responseTime", responseTime).Int("torrents", len(torrents)).Msg("Moderate speed instance, using 15s cache")
	case responseTime > 500*time.Millisecond:
		cacheTTL = 5 * time.Second // Fast instance, cache for 5 seconds
		log.Debug().Dur("responseTime", responseTime).Int("torrents", len(torrents)).Msg("Fast instance, using 5s cache")
	case responseTime > 200*time.Millisecond:
		cacheTTL = 3 * time.Second // Very fast instance, cache for 3 seconds
		log.Debug().Dur("responseTime", responseTime).Int("torrents", len(torrents)).Msg("Very fast instance, using 3s cache")
	default:
		cacheTTL = 2 * time.Second // Ultra fast instance (likely local), cache for 2 seconds
		log.Debug().Dur("responseTime", responseTime).Int("torrents", len(torrents)).Msg("Ultra fast instance, using 2s cache")
	}

	sm.cache.SetWithTTL(cacheKey, torrents, 1, cacheTTL)

	return torrents, nil
}

// normalizeForSearch normalizes text for searching by replacing common separators
func normalizeForSearch(text string) string {
	// Replace common torrent separators with spaces
	replacers := []string{".", "_", "-", "[", "]", "(", ")", "{", "}"}
	normalized := strings.ToLower(text)
	for _, r := range replacers {
		normalized = strings.ReplaceAll(normalized, r, " ")
	}
	// Collapse multiple spaces
	return strings.Join(strings.Fields(normalized), " ")
}

// filterTorrentsBySearch filters torrents by search string with smart matching
func (sm *SyncManager) filterTorrentsBySearch(torrents []qbt.Torrent, search string) []qbt.Torrent {
	if search == "" {
		return torrents
	}

	// Check if search contains glob patterns
	if strings.ContainsAny(search, "*?[") {
		return sm.filterTorrentsByGlob(torrents, search)
	}

	type torrentMatch struct {
		torrent qbt.Torrent
		score   int
		method  string // for debugging
	}

	var matches []torrentMatch
	searchLower := strings.ToLower(search)
	searchNormalized := normalizeForSearch(search)
	searchWords := strings.Fields(searchNormalized)

	for _, torrent := range torrents {
		// Method 1: Exact substring match (highest priority)
		nameLower := strings.ToLower(torrent.Name)
		categoryLower := strings.ToLower(torrent.Category)
		tagsLower := strings.ToLower(torrent.Tags)

		if strings.Contains(nameLower, searchLower) ||
			strings.Contains(categoryLower, searchLower) ||
			strings.Contains(tagsLower, searchLower) {
			matches = append(matches, torrentMatch{
				torrent: torrent,
				score:   0, // Best score
				method:  "exact",
			})
			continue
		}

		// Method 2: Normalized match (handles dots, underscores, etc)
		nameNormalized := normalizeForSearch(torrent.Name)
		categoryNormalized := normalizeForSearch(torrent.Category)
		tagsNormalized := normalizeForSearch(torrent.Tags)

		if strings.Contains(nameNormalized, searchNormalized) ||
			strings.Contains(categoryNormalized, searchNormalized) ||
			strings.Contains(tagsNormalized, searchNormalized) {
			matches = append(matches, torrentMatch{
				torrent: torrent,
				score:   1,
				method:  "normalized",
			})
			continue
		}

		// Method 3: All words present (for multi-word searches)
		if len(searchWords) > 1 {
			allFieldsNormalized := fmt.Sprintf("%s %s %s", nameNormalized, categoryNormalized, tagsNormalized)
			allWordsFound := true
			for _, word := range searchWords {
				if !strings.Contains(allFieldsNormalized, word) {
					allWordsFound = false
					break
				}
			}
			if allWordsFound {
				matches = append(matches, torrentMatch{
					torrent: torrent,
					score:   2,
					method:  "all-words",
				})
				continue
			}
		}

		// Method 4: Fuzzy match only on the normalized name (not the full text)
		// This prevents matching random letter combinations across the entire text
		if fuzzy.MatchNormalizedFold(searchNormalized, nameNormalized) {
			score := fuzzy.RankMatchNormalizedFold(searchNormalized, nameNormalized)
			// Only accept good fuzzy matches (score < 10 is quite good)
			if score < 10 {
				matches = append(matches, torrentMatch{
					torrent: torrent,
					score:   3 + score, // Fuzzy matches start at score 3
					method:  "fuzzy",
				})
			}
		}
	}

	// Sort by score (lower is better)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score < matches[j].score
	})

	// Extract just the torrents
	filtered := make([]qbt.Torrent, len(matches))
	for i, match := range matches {
		filtered[i] = match.torrent
		if i < 5 { // Log first 5 matches for debugging
			log.Debug().
				Str("name", match.torrent.Name).
				Int("score", match.score).
				Str("method", match.method).
				Msg("Search match")
		}
	}

	log.Debug().
		Str("search", search).
		Int("totalTorrents", len(torrents)).
		Int("matchedTorrents", len(filtered)).
		Msg("Search completed")

	return filtered
}

// filterTorrentsByGlob filters torrents using glob pattern matching
func (sm *SyncManager) filterTorrentsByGlob(torrents []qbt.Torrent, pattern string) []qbt.Torrent {
	var filtered []qbt.Torrent

	// Convert to lowercase for case-insensitive matching
	patternLower := strings.ToLower(pattern)

	for _, torrent := range torrents {
		nameLower := strings.ToLower(torrent.Name)

		// Try to match the pattern against the torrent name
		matched, err := filepath.Match(patternLower, nameLower)
		if err != nil {
			// Invalid pattern, log and skip
			log.Debug().
				Str("pattern", pattern).
				Err(err).
				Msg("Invalid glob pattern")
			continue
		}

		if matched {
			filtered = append(filtered, torrent)
			continue
		}

		// Also try matching against category and tags
		if torrent.Category != "" {
			categoryLower := strings.ToLower(torrent.Category)
			if matched, _ := filepath.Match(patternLower, categoryLower); matched {
				filtered = append(filtered, torrent)
				continue
			}
		}

		if torrent.Tags != "" {
			tagsLower := strings.ToLower(torrent.Tags)
			// For tags, try matching against individual tags
			tags := strings.SplitSeq(tagsLower, ", ")
			for tag := range tags {
				if matched, _ := filepath.Match(patternLower, strings.TrimSpace(tag)); matched {
					filtered = append(filtered, torrent)
					break
				}
			}
		}
	}

	log.Debug().
		Str("pattern", pattern).
		Int("totalTorrents", len(torrents)).
		Int("matchedTorrents", len(filtered)).
		Msg("Glob pattern search completed")

	return filtered
}

// applyFilters applies multiple filters to torrents
func (sm *SyncManager) applyFilters(torrents []qbt.Torrent, filters FilterOptions) []qbt.Torrent {
	// If no filters are applied, return all torrents
	if len(filters.Status) == 0 && len(filters.Categories) == 0 && len(filters.Tags) == 0 && len(filters.Trackers) == 0 {
		return torrents
	}

	// Log filter application
	log.Debug().
		Interface("filters", filters).
		Int("totalTorrents", len(torrents)).
		Msg("Applying filters to torrents")

	var filtered []qbt.Torrent
	for _, torrent := range torrents {
		// Check status filter
		if len(filters.Status) > 0 {
			statusMatch := false
			for _, status := range filters.Status {
				if sm.matchTorrentStatus(torrent, status) {
					statusMatch = true
					// Log what matched for debugging
					if status == "checking" {
						log.Debug().
							Str("torrentName", torrent.Name).
							Str("torrentState", string(torrent.State)).
							Str("matchedFilter", status).
							Msg("Torrent matched checking filter")
					}
					break
				}
			}
			if !statusMatch {
				continue
			}
		}

		// Check category filter
		if len(filters.Categories) > 0 {
			categoryMatch := slices.Contains(filters.Categories, torrent.Category)
			if !categoryMatch {
				continue
			}
		}

		// Check tags filter
		if len(filters.Tags) > 0 {
			tagMatch := false
			torrentTags := strings.Split(torrent.Tags, ", ")
			for _, filterTag := range filters.Tags {
				if filterTag == "" && torrent.Tags == "" {
					// Handle "Untagged" filter
					tagMatch = true
					break
				}
				if slices.Contains(torrentTags, filterTag) {
					tagMatch = true
				}
				if tagMatch {
					break
				}
			}
			if !tagMatch {
				continue
			}
		}

		// Check tracker filter
		if len(filters.Trackers) > 0 {
			trackerMatch := false
			for _, filterTracker := range filters.Trackers {
				if filterTracker == "" && torrent.Tracker == "" {
					// Handle "No tracker" filter
					trackerMatch = true
					break
				}
				if torrent.Tracker != "" {
					// Extract hostname from tracker URL for comparison
					if trackerURL, err := url.Parse(torrent.Tracker); err == nil {
						if trackerURL.Hostname() == filterTracker {
							trackerMatch = true
							break
						}
					}
				}
			}
			if !trackerMatch {
				continue
			}
		}

		filtered = append(filtered, torrent)
	}

	log.Debug().
		Int("filteredCount", len(filtered)).
		Interface("appliedFilters", filters).
		Msg("Filters applied, returning filtered torrents")

	return filtered
}

// matchTorrentStatus checks if a torrent matches a specific status filter
func (sm *SyncManager) matchTorrentStatus(torrent qbt.Torrent, status string) bool {
	state := string(torrent.State)
	switch status {
	case "all":
		return true
	case "downloading":
		return state == "downloading" || state == "stalledDL" ||
			state == "metaDL" || state == "queuedDL" ||
			state == "allocating" || state == "checkingDL" ||
			state == "forcedDL"
	case "seeding":
		return state == "uploading" || state == "stalledUP" ||
			state == "queuedUP" || state == "checkingUP" ||
			state == "forcedUP"
	case "completed":
		return torrent.Progress == 1
	case "paused":
		return state == "pausedDL" || state == "pausedUP" ||
			state == "stoppedDL" || state == "stoppedUP"
	case "active":
		return state == "downloading" || state == "uploading" ||
			state == "forcedDL" || state == "forcedUP"
	case "inactive":
		return state != "downloading" && state != "uploading" &&
			state != "forcedDL" && state != "forcedUP"
	case "resumed":
		return state != "pausedDL" && state != "pausedUP" &&
			state != "stoppedDL" && state != "stoppedUP"
	case "stalled":
		return state == "stalledDL" || state == "stalledUP"
	case "stalled_uploading":
		return state == "stalledUP"
	case "stalled_downloading":
		return state == "stalledDL"
	case "errored":
		return state == "error" || state == "missingFiles"
	case "checking":
		matches := state == "checkingDL" || state == "checkingUP" ||
			state == "checkingResumeData"
		return matches
	case "moving":
		return state == "moving"
	default:
		// For specific states, match exactly
		return state == status
	}
}

// calculateStats calculates torrent statistics from a list of torrents
func (sm *SyncManager) calculateStats(torrents []qbt.Torrent) *TorrentStats {
	stats := &TorrentStats{
		Total: len(torrents),
	}

	for _, torrent := range torrents {
		// Add speeds
		stats.TotalDownloadSpeed += int(torrent.DlSpeed)
		stats.TotalUploadSpeed += int(torrent.UpSpeed)

		// Count states
		switch torrent.State {
		case qbt.TorrentStateDownloading, qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateForcedDl:
			stats.Downloading++
		case qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp, qbt.TorrentStateForcedUp:
			stats.Seeding++
		case qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp:
			stats.Paused++
		case qbt.TorrentStateError, qbt.TorrentStateMissingFiles:
			stats.Error++
		case qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData:
			stats.Checking++
		}
	}

	return stats
}

// sortTorrents sorts torrents in-place based on the given field and order
func (sm *SyncManager) sortTorrents(torrents []qbt.Torrent, sortField, order string) {
	// Default to descending order if not specified
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	log.Trace().
		Str("sortField", sortField).
		Str("order", order).
		Int("torrentCount", len(torrents)).
		Msg("Sorting torrents")

	sort.Slice(torrents, func(i, j int) bool {
		var less bool

		switch sortField {
		case "name":
			less = strings.ToLower(torrents[i].Name) < strings.ToLower(torrents[j].Name)
		case "size":
			less = torrents[i].Size < torrents[j].Size
		case "progress":
			less = torrents[i].Progress < torrents[j].Progress
		case "dlspeed":
			less = torrents[i].DlSpeed < torrents[j].DlSpeed
		case "upspeed":
			less = torrents[i].UpSpeed < torrents[j].UpSpeed
		case "eta":
			less = torrents[i].ETA < torrents[j].ETA
		case "ratio":
			less = torrents[i].Ratio < torrents[j].Ratio
		case "category":
			less = strings.ToLower(torrents[i].Category) < strings.ToLower(torrents[j].Category)
		case "tags":
			less = strings.ToLower(torrents[i].Tags) < strings.ToLower(torrents[j].Tags)
		case "added_on", "addedOn":
			less = torrents[i].AddedOn < torrents[j].AddedOn
		case "state":
			less = torrents[i].State < torrents[j].State
		default:
			// Default to sorting by added date (newest first)
			less = torrents[i].AddedOn < torrents[j].AddedOn
		}

		// Reverse for descending order
		if order == "desc" {
			return !less
		}
		return less
	})
}

// AddTags adds tags to the specified torrents (keeps existing tags)
func (sm *SyncManager) AddTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.AddTagsCtx(ctx, hashes, tags); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "addTags", map[string]any{"tags": tags})
	return nil
}

// RemoveTags removes specific tags from the specified torrents
func (sm *SyncManager) RemoveTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.RemoveTagsCtx(ctx, hashes, tags); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "removeTags", map[string]any{"tags": tags})
	return nil
}

// SetTags sets tags on the specified torrents (replaces all existing tags)
// This uses the new qBittorrent 5.1+ API if available, otherwise falls back to RemoveTags + AddTags
func (sm *SyncManager) SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Check version support before attempting API call
	if client.SupportsSetTags() {
		if err := client.SetTags(ctx, hashes, tags); err != nil {
			return err
		}
		log.Debug().Str("webAPIVersion", client.GetWebAPIVersion()).Msg("Used SetTags API directly")
	} else {
		log.Debug().
			Str("webAPIVersion", client.GetWebAPIVersion()).
			Msg("SetTags: qBittorrent version < 2.11.4, using fallback RemoveTags + AddTags")

		torrents, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{
			Hashes: hashes,
		})
		if err != nil {
			return fmt.Errorf("failed to get torrents for fallback: %w", err)
		}

		existingTagsSet := make(map[string]bool)
		for _, torrent := range torrents {
			if torrent.Tags != "" {
				torrentTags := strings.SplitSeq(torrent.Tags, ", ")
				for tag := range torrentTags {
					if strings.TrimSpace(tag) != "" {
						existingTagsSet[strings.TrimSpace(tag)] = true
					}
				}
			}
		}

		var existingTags []string
		for tag := range existingTagsSet {
			existingTags = append(existingTags, tag)
		}

		if len(existingTags) > 0 {
			existingTagsStr := strings.Join(existingTags, ",")
			if err := client.RemoveTagsCtx(ctx, hashes, existingTagsStr); err != nil {
				return fmt.Errorf("failed to remove existing tags during fallback: %w", err)
			}
			log.Debug().Strs("removedTags", existingTags).Msg("SetTags fallback: removed existing tags")
		}

		if tags != "" {
			if err := client.AddTagsCtx(ctx, hashes, tags); err != nil {
				return fmt.Errorf("failed to add new tags during fallback: %w", err)
			}
			newTags := strings.Split(tags, ",")
			log.Debug().Strs("addedTags", newTags).Msg("SetTags fallback: added new tags")
		}
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "setTags", map[string]any{"tags": tags})
	return nil
}

// SetCategory sets the category for the specified torrents
func (sm *SyncManager) SetCategory(ctx context.Context, instanceID int, hashes []string, category string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.SetCategoryCtx(ctx, hashes, category); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "setCategory", map[string]any{"category": category})
	return nil
}

// SetAutoTMM sets the automatic torrent management for torrents
func (sm *SyncManager) SetAutoTMM(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.SetAutoManagementCtx(ctx, hashes, enable); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "toggleAutoTMM", map[string]any{"enable": enable})
	return nil
}

// CreateTags creates new tags
func (sm *SyncManager) CreateTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.CreateTagsCtx(ctx, tags); err != nil {
		return err
	}

	// Invalidate tags cache
	cacheKey := fmt.Sprintf("tags:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated tags cache")
	return nil
}

// DeleteTags deletes tags
func (sm *SyncManager) DeleteTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.DeleteTagsCtx(ctx, tags); err != nil {
		return err
	}

	// Invalidate tags cache
	cacheKey := fmt.Sprintf("tags:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated tags cache")
	return nil
}

// CreateCategory creates a new category
func (sm *SyncManager) CreateCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.CreateCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	// Invalidate categories cache
	cacheKey := fmt.Sprintf("categories:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated categories cache")
	return nil
}

// EditCategory edits an existing category
func (sm *SyncManager) EditCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.EditCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	// Invalidate categories cache
	cacheKey := fmt.Sprintf("categories:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated categories cache")
	return nil
}

// RemoveCategories removes categories
func (sm *SyncManager) RemoveCategories(ctx context.Context, instanceID int, categories []string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.RemoveCategoriesCtx(ctx, categories); err != nil {
		return err
	}

	// Invalidate categories cache
	cacheKey := fmt.Sprintf("categories:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated categories cache")
	return nil
}
