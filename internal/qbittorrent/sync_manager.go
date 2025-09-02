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
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rs/zerolog/log"
)

// CacheMetadata provides information about cache state
type CacheMetadata struct {
	Source      string `json:"source"`      // "cache" or "fresh"
	Age         int    `json:"age"`         // Age in seconds
	IsStale     bool   `json:"isStale"`     // Whether data is stale
	NextRefresh string `json:"nextRefresh"` // When next refresh will occur (ISO 8601 string)
}

// TorrentResponse represents a response containing torrents with stats
type TorrentResponse struct {
	Torrents      []qbt.Torrent           `json:"torrents"`
	Total         int                     `json:"total"`
	Stats         *TorrentStats           `json:"stats,omitempty"`
	Counts        *TorrentCounts          `json:"counts,omitempty"`     // Include counts for sidebar
	Categories    map[string]qbt.Category `json:"categories,omitempty"` // Include categories for sidebar
	Tags          []string                `json:"tags,omitempty"`       // Include tags for sidebar
	HasMore       bool                    `json:"hasMore"`              // Whether more pages are available
	SessionID     string                  `json:"sessionId,omitempty"`  // Optional session tracking
	CacheMetadata *CacheMetadata          `json:"cacheMetadata,omitempty"`
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

// SyncManager manages torrent operations
type SyncManager struct {
	clientPool *ClientPool
}

// OptimisticTorrentUpdate represents a temporary optimistic update to a torrent
type OptimisticTorrentUpdate struct {
	State         qbt.TorrentState `json:"state"`
	OriginalState qbt.TorrentState `json:"originalState"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	Action        string           `json:"action"`
}

// NewSyncManager creates a new sync manager
func NewSyncManager(clientPool *ClientPool) *SyncManager {
	return &SyncManager{
		clientPool: clientPool,
	}
}

// GetTorrentsWithFilters gets torrents with filters, search, sorting, and pagination
// Always fetches fresh data from sync manager for real-time updates
func (sm *SyncManager) GetTorrentsWithFilters(ctx context.Context, instanceID int, limit, offset int, sort, order, search string, filters FilterOptions) (*TorrentResponse, error) {
	// Always get fresh data from sync manager for real-time updates
	var filteredTorrents []qbt.Torrent
	var err error

	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return nil, fmt.Errorf("sync manager not initialized")
	}

	// Determine if we can use library filtering or need manual filtering
	// Use library filtering only if we have single filters that the library supports
	var torrentFilterOptions qbt.TorrentFilterOptions
	var useManualFiltering bool

	// Check if we need manual filtering for any reason
	hasMultipleStatusFilters := len(filters.Status) > 1
	hasMultipleCategoryFilters := len(filters.Categories) > 1
	hasMultipleTagFilters := len(filters.Tags) > 1
	hasTrackerFilters := len(filters.Trackers) > 0 // Library doesn't support tracker filtering

	// Determine if any status filter needs manual filtering
	needsManualStatusFiltering := false
	if len(filters.Status) > 0 {
		for _, status := range filters.Status {
			if status == "active" || status == "inactive" || status == "checking" || status == "moving" {
				needsManualStatusFiltering = true
				break
			}
		}
	}

	useManualFiltering = hasMultipleStatusFilters || hasMultipleCategoryFilters || hasMultipleTagFilters ||
		hasTrackerFilters || needsManualStatusFiltering

	if useManualFiltering {
		// Use manual filtering - get all torrents and filter manually
		log.Debug().
			Int("instanceID", instanceID).
			Bool("multipleStatus", hasMultipleStatusFilters).
			Bool("multipleCategories", hasMultipleCategoryFilters).
			Bool("multipleTags", hasMultipleTagFilters).
			Bool("hasTrackers", hasTrackerFilters).
			Bool("needsManualStatus", needsManualStatusFiltering).
			Msg("Using manual filtering due to multiple selections or unsupported filters")

		// Get all torrents
		torrentFilterOptions.Filter = qbt.TorrentFilterAll
		torrentFilterOptions.Sort = sort
		torrentFilterOptions.Reverse = (order == "desc")

		filteredTorrents = syncManager.GetTorrents(torrentFilterOptions)

		// Apply manual filtering for multiple selections
		filteredTorrents = sm.applyManualFilters(filteredTorrents, filters)
	} else {
		// Use library filtering for single selections
		log.Debug().
			Int("instanceID", instanceID).
			Msg("Using library filtering for single selections")

		// Handle single status filter
		if len(filters.Status) == 1 {
			status := filters.Status[0]
			switch status {
			case "all":
				torrentFilterOptions.Filter = qbt.TorrentFilterAll
			case "completed":
				torrentFilterOptions.Filter = qbt.TorrentFilterCompleted
			case "resumed":
				torrentFilterOptions.Filter = qbt.TorrentFilterResumed
			case "paused":
				torrentFilterOptions.Filter = qbt.TorrentFilterPaused
			case "stopped":
				torrentFilterOptions.Filter = qbt.TorrentFilterStopped
			case "stalled":
				torrentFilterOptions.Filter = qbt.TorrentFilterStalled
			case "uploading", "seeding":
				torrentFilterOptions.Filter = qbt.TorrentFilterUploading
			case "stalled_uploading", "stalled_seeding":
				torrentFilterOptions.Filter = qbt.TorrentFilterStalledUploading
			case "downloading":
				torrentFilterOptions.Filter = qbt.TorrentFilterDownloading
			case "stalled_downloading":
				torrentFilterOptions.Filter = qbt.TorrentFilterStalledDownloading
			case "errored", "error":
				torrentFilterOptions.Filter = qbt.TorrentFilterError
			default:
				// Default to all if unknown status
				torrentFilterOptions.Filter = qbt.TorrentFilterAll
			}
		} else {
			// Default to all when no status filter is provided
			torrentFilterOptions.Filter = qbt.TorrentFilterAll
		}

		// Handle single category filter
		if len(filters.Categories) == 1 {
			torrentFilterOptions.Category = filters.Categories[0]
		}

		// Handle single tag filter
		if len(filters.Tags) == 1 {
			torrentFilterOptions.Tag = filters.Tags[0]
		}

		// Set sorting in the filter options (library handles sorting)
		torrentFilterOptions.Sort = sort
		torrentFilterOptions.Reverse = (order == "desc")

		// Use library filtering and sorting
		filteredTorrents = syncManager.GetTorrents(torrentFilterOptions)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("totalCount", len(filteredTorrents)).
		Bool("useManualFiltering", useManualFiltering).
		Msg("Applied initial filtering")

	// Apply search filter if provided (library doesn't support search)
	if search != "" {
		filteredTorrents = sm.filterTorrentsBySearch(filteredTorrents, search)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("filtered", len(filteredTorrents)).
		Msg("Applied search filtering")

	// Calculate stats from filtered torrents
	stats := sm.calculateStats(filteredTorrents)

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
	allTorrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{})
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

	// Determine cache metadata based on last sync update time
	var cacheMetadata *CacheMetadata
	client, clientErr := sm.clientPool.GetClient(ctx, instanceID)
	if clientErr == nil {
		syncManager := client.GetSyncManager()
		if syncManager != nil {
			lastSyncTime := syncManager.LastSyncTime()
			now := time.Now()
			age := int(now.Sub(lastSyncTime).Seconds())
			isFresh := age <= 1 // Fresh if updated within the last second

			source := "cache"
			if isFresh {
				source = "fresh"
			}

			cacheMetadata = &CacheMetadata{
				Source:      source,
				Age:         age,
				IsStale:     !isFresh,
				NextRefresh: now.Add(time.Second).Format(time.RFC3339),
			}
		}
	}

	// Data is always fresh from sync manager

	response := &TorrentResponse{
		Torrents:      paginatedTorrents,
		Total:         len(filteredTorrents),
		Stats:         stats,
		Counts:        counts,     // Include counts for sidebar
		Categories:    categories, // Include categories for sidebar
		Tags:          tags,       // Include tags for sidebar
		HasMore:       hasMore,
		CacheMetadata: cacheMetadata,
	}

	// Always compute from fresh all_torrents data
	// This ensures real-time updates are always reflected
	// The sync manager is the single source of truth

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

// GetServerStats gets server statistics using sync manager (for Dashboard)
func (sm *SyncManager) GetServerStats(ctx context.Context, instanceID int) (*qbt.MainData, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return nil, fmt.Errorf("sync manager not initialized")
	}

	// Get main data from sync manager
	mainData := syncManager.GetData()

	log.Debug().
		Int("instanceID", instanceID).
		Msg("Server stats fetched from sync manager")

	return mainData, nil
}

// BulkAction performs bulk operations on torrents
func (sm *SyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return fmt.Errorf("sync manager not initialized")
	}

	// Validate that torrents exist before proceeding
	torrentMap := syncManager.GetTorrentMap(qbt.TorrentFilterOptions{Hashes: hashes})
	if len(torrentMap) == 0 {
		return fmt.Errorf("no sync data available")
	}

	existingTorrents := make([]*qbt.Torrent, 0, len(torrentMap))
	missingHashes := make([]string, 0, len(hashes)-len(torrentMap))
	for _, hash := range hashes {
		if torrent, exists := torrentMap[hash]; exists {
			existingTorrents = append(existingTorrents, &torrent)
		} else {
			missingHashes = append(missingHashes, hash)
		}
	}

	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found for bulk action: %s", action)
	}

	// Log warning for any missing torrents
	if len(missingHashes) > 0 {
		log.Warn().
			Int("instanceID", instanceID).
			Int("requested", len(hashes)).
			Int("found", len(existingTorrents)).
			Str("action", action).
			Msg("Some torrents not found for bulk action")
	}

	// Apply optimistic update immediately for instant UI feedback
	sm.applyOptimisticCacheUpdate(instanceID, hashes, action, nil)

	// Perform action based on type
	switch action {
	case "pause":
		err = client.PauseCtx(ctx, hashes)
	case "resume":
		err = client.ResumeCtx(ctx, hashes)
	case "delete":
		err = client.DeleteTorrentsCtx(ctx, hashes, false)
	case "deleteWithFiles":
		err = client.DeleteTorrentsCtx(ctx, hashes, true)
	case "recheck":
		err = client.RecheckCtx(ctx, hashes)
	case "reannounce":
		// No cache update needed - no visible state change
		err = client.ReAnnounceTorrentsCtx(ctx, hashes)
	case "increasePriority":
		err = client.IncreasePriorityCtx(ctx, hashes)
		if err == nil {
			sm.syncAfterModification(instanceID, client, action)
		}
	case "decreasePriority":
		err = client.DecreasePriorityCtx(ctx, hashes)
		if err == nil {
			sm.syncAfterModification(instanceID, client, action)
		}
	case "topPriority":
		err = client.SetMaxPriorityCtx(ctx, hashes)
		if err == nil {
			sm.syncAfterModification(instanceID, client, action)
		}
	case "bottomPriority":
		err = client.SetMinPriorityCtx(ctx, hashes)
		if err == nil {
			sm.syncAfterModification(instanceID, client, action)
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
	if err := client.AddTorrentFromMemoryCtx(ctx, fileContent, options); err != nil {
		return err
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "add_torrent_from_memory")

	return nil
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

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "add_torrent_from_urls")

	return nil
}

// GetCategories gets all categories
func (sm *SyncManager) GetCategories(ctx context.Context, instanceID int) (map[string]qbt.Category, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return nil, fmt.Errorf("sync manager not initialized")
	}

	// Get categories from sync manager (real-time)
	categories := syncManager.GetCategories()

	return categories, nil
}

// GetTags gets all tags
func (sm *SyncManager) GetTags(ctx context.Context, instanceID int) ([]string, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return nil, fmt.Errorf("sync manager not initialized")
	}

	// Get tags from sync manager (real-time)
	tags := syncManager.GetTags()

	slices.SortFunc(tags, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	return tags, nil
}

// GetTorrentProperties gets detailed properties for a specific torrent
func (sm *SyncManager) GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get properties (real-time)
	props, err := client.GetTorrentPropertiesCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent properties: %w", err)
	}

	return &props, nil
}

// GetTorrentTrackers gets trackers for a specific torrent
func (sm *SyncManager) GetTorrentTrackers(ctx context.Context, instanceID int, hash string) ([]qbt.TorrentTracker, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get trackers (real-time)
	trackers, err := client.GetTorrentTrackersCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent trackers: %w", err)
	}

	return trackers, nil
}

// GetTorrentFiles gets files information for a specific torrent
func (sm *SyncManager) GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get files (real-time)
	files, err := client.GetFilesInformationCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent files: %w", err)
	}

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
			// Extract domain from tracker URL using proper URL parsing
			// Handle multiple trackers separated by newlines or commas
			trackerStrings := strings.Split(torrent.Tracker, "\n")
			domainFound := false
			for _, trackerStr := range trackerStrings {
				trackerStr = strings.TrimSpace(trackerStr)
				if trackerStr == "" {
					continue
				}
				// Split by commas
				commaParts := strings.Split(trackerStr, ",")
				for _, part := range commaParts {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					// Extract domain from this tracker URL
					var domain string
					if strings.Contains(part, "://") {
						if u, err := url.Parse(part); err == nil {
							domain = u.Hostname()
						} else {
							// Fallback to string manipulation
							parts := strings.Split(part, "://")
							if len(parts) > 1 {
								domain = parts[1]
								if idx := strings.IndexAny(domain, ":/"); idx != -1 {
									domain = domain[:idx]
								}
							}
						}
					}
					if domain != "" {
						counts.Trackers[domain]++
						domainFound = true
						break // Use first valid domain found
					}
				}
				if domainFound {
					break // Use first tracker with valid domain
				}
			}
			if !domainFound {
				// If no valid domain found, count as unknown
				counts.Trackers["Unknown"]++
			}
		} else {
			counts.Trackers[""]++
		}
	}

	return counts
}

// GetTorrentCounts gets all torrent counts for the filter sidebar
func (sm *SyncManager) GetTorrentCounts(ctx context.Context, instanceID int) (*TorrentCounts, error) {
	// IMPORTANT: We don't cache counts separately anymore
	// We derive counts from the same fresh torrent data that the table uses
	// This ensures the sidebar and table are always in sync

	log.Debug().Int("instanceID", instanceID).Msg("GetTorrentCounts: fetching fresh data from getAllTorrentsForStats")

	// Get all torrents from the same source the table uses (now fresh from sync manager)
	allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get all torrents for counts: %w", err)
	}

	log.Debug().Int("instanceID", instanceID).Int("torrents", len(allTorrents)).Msg("GetTorrentCounts: got fresh torrents from sync manager")

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
			// Extract hostname from tracker URL - handle multiple trackers
			trackerStrings := strings.Split(torrent.Tracker, "\n")
			domainFound := false
			for _, trackerStr := range trackerStrings {
				trackerStr = strings.TrimSpace(trackerStr)
				if trackerStr == "" {
					continue
				}
				// Split by commas
				commaParts := strings.Split(trackerStr, ",")
				for _, part := range commaParts {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					// Extract hostname from this tracker URL
					if trackerURL, err := url.Parse(part); err == nil {
						hostname := trackerURL.Hostname()
						if hostname != "" {
							counts.Trackers[hostname]++
							domainFound = true
							break // Use first valid hostname found
						}
					}
				}
				if domainFound {
					break // Use first tracker with valid hostname
				}
			}
			if !domainFound {
				// Fallback to string manipulation if URL parsing fails
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

	log.Debug().Int("instanceID", instanceID).Int64("download", speeds.Download).Int64("upload", speeds.Upload).Msg("GetInstanceSpeeds: got from GetTransferInfo API")

	return speeds, nil
}

// Helper methods

// applyOptimisticCacheUpdate applies optimistic updates for the given instance and hashes
func (sm *SyncManager) applyOptimisticCacheUpdate(instanceID int, hashes []string, action string, payload map[string]any) {
	// Get client for this instance
	client, err := sm.clientPool.GetClient(context.Background(), instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for optimistic update")
		return
	}

	// Delegate to client's optimistic update method
	client.applyOptimisticCacheUpdate(hashes, action, payload)
}

// syncAfterModification performs a background sync after a modification operation
func (sm *SyncManager) syncAfterModification(instanceID int, client *Client, operation string) {
	go func() {
		ctx := context.Background()

		// If no client provided, get one
		if client == nil {
			if sm.clientPool == nil {
				log.Warn().Int("instanceID", instanceID).Str("operation", operation).Msg("Client pool is nil, skipping sync")
				return
			}
			var err error
			client, err = sm.clientPool.GetClient(ctx, instanceID)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("operation", operation).Msg("Failed to get client for sync")
				return
			}
		}

		if syncManager := client.GetSyncManager(); syncManager != nil {
			// Small delay to let qBittorrent process the command
			time.Sleep(10 * time.Millisecond)
			if err := syncManager.Sync(ctx); err != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Str("operation", operation).Msg("Failed to sync after modification")
			}
		}
	}()
}

// getAllTorrentsForStats gets all torrents for stats calculation (with optimistic updates)
func (sm *SyncManager) getAllTorrentsForStats(ctx context.Context, instanceID int, search string) ([]qbt.Torrent, error) {
	// Get client
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return nil, fmt.Errorf("sync manager not initialized")
	}

	// Get all torrents from sync manager
	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{})

	// Build a map for O(1) lookups during optimistic updates
	torrentMap := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentMap[torrents[i].Hash] = &torrents[i]
	}

	// Apply optimistic updates using the torrent map for O(1) lookups
	if instanceUpdates := client.getOptimisticUpdates(); len(instanceUpdates) > 0 {
		// Get the last sync time to detect if backend has responded since our optimistic update
		// This provides much more accurate clearing than a fixed timeout
		lastSyncTime := syncManager.LastSyncTime()

		optimisticCount := 0
		removedCount := 0

		for hash, optimisticUpdate := range instanceUpdates {
			// Use O(1) map lookup instead of iterating through all torrents
			if torrent, exists := torrentMap[hash]; exists {
				shouldClear := false
				timeSinceUpdate := time.Since(optimisticUpdate.UpdatedAt)

				// Clear if backend state indicates the operation was successful
				if sm.shouldClearOptimisticUpdate(torrent.State, optimisticUpdate.OriginalState, optimisticUpdate.State, optimisticUpdate.Action) {
					shouldClear = true
					log.Debug().
						Str("hash", hash).
						Str("state", string(torrent.State)).
						Str("originalState", string(optimisticUpdate.OriginalState)).
						Str("optimisticState", string(optimisticUpdate.State)).
						Str("action", optimisticUpdate.Action).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Msg("Clearing optimistic update - backend state indicates operation success")
				} else if timeSinceUpdate > 60*time.Second {
					// Safety net: still clear after 60 seconds if something went wrong
					shouldClear = true
					log.Debug().
						Str("hash", hash).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Msg("Clearing stale optimistic update (safety net)")
				} else {
					// Debug: show why we're not clearing yet
					log.Debug().
						Str("hash", hash).
						Time("optimisticAt", optimisticUpdate.UpdatedAt).
						Time("lastSyncAt", lastSyncTime).
						Dur("timeSinceUpdate", timeSinceUpdate).
						Bool("syncAfterUpdate", lastSyncTime.After(optimisticUpdate.UpdatedAt)).
						Str("backendState", string(torrent.State)).
						Str("optimisticState", string(optimisticUpdate.State)).
						Msg("Keeping optimistic update - conditions not met")
				}

				if shouldClear {
					client.clearOptimisticUpdate(hash)
					removedCount++
				} else {
					// Apply the optimistic state change to the torrent in our slice
					log.Debug().
						Str("hash", hash).
						Str("oldState", string(torrent.State)).
						Str("newState", string(optimisticUpdate.State)).
						Str("action", optimisticUpdate.Action).
						Msg("Applying optimistic update")

					torrent.State = optimisticUpdate.State
					optimisticCount++
				}
			} else {
				// Torrent no longer exists - clear the optimistic update
				log.Debug().
					Str("hash", hash).
					Str("action", optimisticUpdate.Action).
					Time("optimisticAt", optimisticUpdate.UpdatedAt).
					Msg("Clearing optimistic update - torrent no longer exists")
				client.clearOptimisticUpdate(hash)
				removedCount++
			}
		}

		if optimisticCount > 0 {
			log.Debug().Int("instanceID", instanceID).Int("optimisticCount", optimisticCount).Msg("Applied optimistic updates to torrent data")
		}

		if removedCount > 0 {
			log.Debug().Int("instanceID", instanceID).Int("removedCount", removedCount).Msg("Cleared optimistic updates")
		}
	}

	log.Debug().Int("instanceID", instanceID).Int("torrents", len(torrents)).Msg("getAllTorrentsForStats: Fetched from sync manager with optimistic updates")

	return torrents, nil
}

// clearOptimisticUpdate removes an optimistic update for a specific torrent
func (sm *SyncManager) clearOptimisticUpdate(instanceID int, hash string) {
	client, err := sm.clientPool.GetClient(context.Background(), instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for clearing optimistic update")
		return
	}
	client.clearOptimisticUpdate(hash)
}

// clearStaleOptimisticUpdates removes optimistic updates that are older than the specified duration
func (sm *SyncManager) clearStaleOptimisticUpdates(maxAge time.Duration) {
	// This method is no longer needed since each client manages its own optimistic updates
	// But we can iterate through all clients if needed
	log.Debug().Dur("maxAge", maxAge).Msg("Clearing stale optimistic updates across all instances")

	// Note: This would require iterating through all clients in the pool
	// For now, we'll rely on the per-client clearing in getAllTorrentsForStats
}

// clearAllOptimisticUpdatesForInstance removes all optimistic updates for a specific instance
func (sm *SyncManager) clearAllOptimisticUpdatesForInstance(instanceID int) {
	client, err := sm.clientPool.GetClient(context.Background(), instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for clearing all optimistic updates")
		return
	}
	client.clearAllOptimisticUpdates()
}
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

// filterTorrentsByTrackers filters torrents by tracker domains
func (sm *SyncManager) filterTorrentsByTrackers(torrents []qbt.Torrent, trackers []string) []qbt.Torrent {
	if len(trackers) == 0 {
		return torrents
	}

	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		// Extract tracker domains - handle multiple trackers separated by newlines or commas
		var trackerDomains []string

		if torrent.Tracker != "" {
			// Split by newlines first, then by commas
			trackerStrings := strings.Split(torrent.Tracker, "\n")
			for _, trackerStr := range trackerStrings {
				trackerStr = strings.TrimSpace(trackerStr)
				if trackerStr == "" {
					continue
				}
				// Split by commas
				commaParts := strings.Split(trackerStr, ",")
				for _, part := range commaParts {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					// Extract domain from this tracker URL
					var domain string
					if strings.Contains(part, "://") {
						if u, err := url.Parse(part); err == nil {
							domain = u.Hostname()
						} else {
							// Fallback to string manipulation
							parts := strings.Split(part, "://")
							if len(parts) > 1 {
								domain = parts[1]
								if idx := strings.IndexAny(domain, ":/"); idx != -1 {
									domain = domain[:idx]
								}
							}
						}
					}
					if domain != "" {
						trackerDomains = append(trackerDomains, domain)
					}
				}
			}
		}

		// If no trackers found, add empty string
		if len(trackerDomains) == 0 {
			trackerDomains = append(trackerDomains, "")
		}

		// Check if any of this torrent's tracker domains match the filter list
		for _, torrentDomain := range trackerDomains {
			for _, filterTracker := range trackers {
				if torrentDomain == filterTracker {
					filtered = append(filtered, torrent)
					goto nextTorrent
				}
			}
		}
	nextTorrent:
	}

	return filtered
}

// applyManualFilters applies all filters manually when library filtering is insufficient
func (sm *SyncManager) applyManualFilters(torrents []qbt.Torrent, filters FilterOptions) []qbt.Torrent {
	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		matches := true

		// Apply status filters (OR logic within status filters)
		if len(filters.Status) > 0 {
			statusMatch := false
			for _, status := range filters.Status {
				if sm.matchTorrentStatus(torrent, status) {
					statusMatch = true
					break
				}
			}
			matches = matches && statusMatch
		}

		// Apply category filters (OR logic within category filters)
		if len(filters.Categories) > 0 {
			categoryMatch := false
			torrentCategory := torrent.Category
			for _, filterCategory := range filters.Categories {
				if torrentCategory == filterCategory {
					categoryMatch = true
					break
				}
			}
			matches = matches && categoryMatch
		}

		// Apply tag filters (OR logic within tag filters)
		if len(filters.Tags) > 0 {
			tagMatch := false
			if torrent.Tags == "" {
				// Check if empty tag is in the filter (for "untagged" option)
				for _, filterTag := range filters.Tags {
					if filterTag == "" {
						tagMatch = true
						break
					}
				}
			} else {
				// Parse torrent tags
				torrentTags := strings.SplitSeq(torrent.Tags, ", ")
				torrentTagsMap := make(map[string]bool)
				for tag := range torrentTags {
					trimmedTag := strings.TrimSpace(tag)
					if trimmedTag != "" {
						torrentTagsMap[trimmedTag] = true
					}
				}

				// Check if any filter tag matches torrent tags
				for _, filterTag := range filters.Tags {
					if filterTag == "" {
						// Empty filter tag means "untagged", but we already handled that case
						continue
					}
					if torrentTagsMap[filterTag] {
						tagMatch = true
						break
					}
				}
			}
			matches = matches && tagMatch
		}

		// Apply tracker filters (OR logic within tracker filters)
		if len(filters.Trackers) > 0 {
			trackerMatch := false
			if torrent.Tracker == "" {
				// Check if empty tracker is in the filter (for "no tracker" option)
				for _, filterTracker := range filters.Trackers {
					if filterTracker == "" {
						trackerMatch = true
						break
					}
				}
			} else {
				// Extract tracker domains from torrent
				var trackerDomains []string
				trackerStrings := strings.Split(torrent.Tracker, "\n")

				for _, trackerStr := range trackerStrings {
					trackerStr = strings.TrimSpace(trackerStr)
					if trackerStr == "" {
						continue
					}

					commaParts := strings.Split(trackerStr, ",")
					for _, part := range commaParts {
						part = strings.TrimSpace(part)
						if part == "" {
							continue
						}

						var domain string
						if strings.Contains(part, "://") {
							if u, err := url.Parse(part); err == nil {
								domain = u.Hostname()
							} else {
								parts := strings.Split(part, "://")
								if len(parts) > 1 {
									domain = parts[1]
									if idx := strings.IndexAny(domain, ":/"); idx != -1 {
										domain = domain[:idx]
									}
								}
							}
						}
						if domain != "" {
							trackerDomains = append(trackerDomains, domain)
							break // Use first valid domain per tracker string
						}
					}
				}

				// If no valid domains found, use "Unknown"
				if len(trackerDomains) == 0 {
					trackerDomains = append(trackerDomains, "Unknown")
				}

				// Check if any tracker domain matches the filter
				for _, trackerDomain := range trackerDomains {
					for _, filterTracker := range filters.Trackers {
						if trackerDomain == filterTracker {
							trackerMatch = true
							break
						}
					}
					if trackerMatch {
						break
					}
				}
			}
			matches = matches && trackerMatch
		}

		if matches {
			filtered = append(filtered, torrent)
		}
	}

	log.Debug().
		Int("inputTorrents", len(torrents)).
		Int("filteredTorrents", len(filtered)).
		Int("statusFilters", len(filters.Status)).
		Int("categoryFilters", len(filters.Categories)).
		Int("tagFilters", len(filters.Tags)).
		Int("trackerFilters", len(filters.Trackers)).
		Msg("Applied manual filtering with multiple selections")

	return filtered
}

// Torrent state categories for fast lookup
var torrentStateCategories = map[string][]qbt.TorrentState{
	"downloading":          {qbt.TorrentStateDownloading, qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateAllocating, qbt.TorrentStateCheckingDl, qbt.TorrentStateForcedDl},
	"seeding":              {qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp},
	"paused":               {qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp},
	"active":               {qbt.TorrentStateDownloading, qbt.TorrentStateUploading, qbt.TorrentStateForcedDl, qbt.TorrentStateForcedUp},
	"stalled":              {qbt.TorrentStateStalledDl, qbt.TorrentStateStalledUp},
	"checking":             {qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData},
	"errored":              {qbt.TorrentStateError, qbt.TorrentStateMissingFiles},
	"moving":               {qbt.TorrentStateMoving},
	"stalled_uploading":    {qbt.TorrentStateStalledUp},
	"stalled_downloading":  {qbt.TorrentStateStalledDl},
}

// Action state categories for optimistic update clearing
var actionSuccessCategories = map[string]string{
	"resume":       "active",
	"force_resume": "active",
	"pause":        "paused",
	"recheck":      "checking",
}

// shouldClearOptimisticUpdate checks if an optimistic update should be cleared based on the action and current state
func (sm *SyncManager) shouldClearOptimisticUpdate(currentState qbt.TorrentState, originalState qbt.TorrentState, optimisticState qbt.TorrentState, action string) bool {
	// Check if originalState is set (not zero value)
	var zeroState qbt.TorrentState
	if originalState != zeroState {
		// Clear the optimistic update if the current state is different from the original state
		// This indicates that the backend has acknowledged and processed the operation
		if currentState != originalState {
			log.Debug().
				Str("currentState", string(currentState)).
				Str("originalState", string(originalState)).
				Str("optimisticState", string(optimisticState)).
				Str("action", action).
				Msg("Clearing optimistic update - backend state changed from original")
			return true
		}
	} else {
		// Fallback to category-based logic if originalState is not set
		if successCategory, exists := actionSuccessCategories[action]; exists {
			if categoryStates, categoryExists := torrentStateCategories[successCategory]; categoryExists {
				if slices.Contains(categoryStates, currentState) {
					log.Debug().
						Str("currentState", string(currentState)).
						Str("originalState", string(originalState)).
						Str("optimisticState", string(optimisticState)).
						Str("action", action).
						Str("successCategory", successCategory).
						Msg("Clearing optimistic update - current state in success category")
					return true
				}
			}
		}
	}

	// Final fallback: use exact state match
	return currentState == optimisticState
}

// matchTorrentStatus checks if a torrent matches a specific status filter
func (sm *SyncManager) matchTorrentStatus(torrent qbt.Torrent, status string) bool {
	// Handle special cases first
	switch status {
	case "all":
		return true
	case "completed":
		return torrent.Progress == 1
	case "inactive":
		// Inactive is the inverse of active
		return !slices.Contains(torrentStateCategories["active"], torrent.State)
	case "resumed":
		// Resumed is the inverse of paused
		return !slices.Contains(torrentStateCategories["paused"], torrent.State)
	}

	// For grouped status categories, check if state is in the category
	if category, exists := torrentStateCategories[status]; exists {
		return slices.Contains(category, torrent.State)
	}

	// For everything else, just do direct equality with the string representation
	return string(torrent.State) == status
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

// AddTags adds tags to the specified torrents (keeps existing tags)
func (sm *SyncManager) AddTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync manager
	syncManager := client.GetSyncManager()
	if syncManager == nil {
		return fmt.Errorf("sync manager not initialized")
	}

	// Validate that torrents exist
	torrentList := syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: hashes})

	torrentMap := make(map[string]qbt.Torrent, len(torrentList))
	for _, torrent := range torrentList {
		torrentMap[torrent.Hash] = torrent
	}

	if len(torrentMap) == 0 {
		return fmt.Errorf("no sync data available")
	}

	existingCount := 0
	for _, hash := range hashes {
		if _, exists := torrentMap[hash]; exists {
			existingCount++
		}
	}

	if existingCount == 0 {
		return fmt.Errorf("no valid torrents found to add tags")
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

	// Validate that torrents exist
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found to remove tags")
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

		// Use sync manager data instead of direct API call for better performance
		// Get torrents directly from the client's torrent map for O(1) lookups
		torrents := client.getTorrentsByHashes(hashes)

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

	// Validate that torrents exist
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found to set category")
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

	// Validate that torrents exist
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found to set auto TMM")
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

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "create_tags")

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

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "delete_tags")

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

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "create_category")

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

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "edit_category")

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

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "remove_categories")

	return nil
}

// GetAppPreferences fetches app preferences for an instance
func (sm *SyncManager) GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
	// Get client and fetch preferences
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return qbt.AppPreferences{}, fmt.Errorf("failed to get client: %w", err)
	}

	prefs, err := client.GetAppPreferencesCtx(ctx)
	if err != nil {
		return qbt.AppPreferences{}, fmt.Errorf("failed to get app preferences: %w", err)
	}

	return prefs, nil
}

// SetAppPreferences updates app preferences
func (sm *SyncManager) SetAppPreferences(ctx context.Context, instanceID int, prefs map[string]any) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.SetPreferencesCtx(ctx, prefs); err != nil {
		return fmt.Errorf("failed to set preferences: %w", err)
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "set_app_preferences")

	return nil
}

// GetAlternativeSpeedLimitsMode gets whether alternative speed limits are currently active
func (sm *SyncManager) GetAlternativeSpeedLimitsMode(ctx context.Context, instanceID int) (bool, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return false, fmt.Errorf("failed to get client: %w", err)
	}

	enabled, err := client.GetAlternativeSpeedLimitsModeCtx(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get alternative speed limits mode: %w", err)
	}

	return enabled, nil
}

// ToggleAlternativeSpeedLimits toggles alternative speed limits on/off
func (sm *SyncManager) ToggleAlternativeSpeedLimits(ctx context.Context, instanceID int) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.ToggleAlternativeSpeedLimitsCtx(ctx); err != nil {
		return fmt.Errorf("failed to toggle alternative speed limits: %w", err)
	}

	// Sync after modification
	sm.syncAfterModification(instanceID, client, "toggle_alternative_speed_limits")

	return nil
}

// SetTorrentShareLimit sets share limits (ratio, seeding time) for torrents
func (sm *SyncManager) SetTorrentShareLimit(ctx context.Context, instanceID int, hashes []string, ratioLimit float64, seedingTimeLimit, inactiveSeedingTimeLimit int64) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Validate that torrents exist
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found to set share limits")
	}

	if err := client.SetTorrentShareLimitCtx(ctx, hashes, ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit); err != nil {
		return fmt.Errorf("failed to set torrent share limit: %w", err)
	}

	return nil
}

// SetTorrentUploadLimit sets upload speed limit for torrents
func (sm *SyncManager) SetTorrentUploadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Validate that torrents exist
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found to set upload limit")
	}

	// Convert KB/s to bytes/s (qBittorrent API expects bytes/s)
	limitBytes := limitKBs * 1024

	if err := client.SetTorrentUploadLimitCtx(ctx, hashes, limitBytes); err != nil {
		return fmt.Errorf("failed to set torrent upload limit: %w", err)
	}

	return nil
}

// SetTorrentDownloadLimit sets download speed limit for torrents
func (sm *SyncManager) SetTorrentDownloadLimit(ctx context.Context, instanceID int, hashes []string, limitKBs int64) error {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Validate that torrents exist
	existingTorrents := client.getTorrentsByHashes(hashes)
	if len(existingTorrents) == 0 {
		return fmt.Errorf("no valid torrents found to set download limit")
	}

	// Convert KB/s to bytes/s (qBittorrent API expects bytes/s)
	limitBytes := limitKBs * 1024

	if err := client.SetTorrentDownloadLimitCtx(ctx, hashes, limitBytes); err != nil {
		return fmt.Errorf("failed to set torrent download limit: %w", err)
	}

	return nil
}
