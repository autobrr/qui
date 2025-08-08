package qbittorrent

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/dgraph-io/ristretto"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/rs/zerolog/log"
)

// CacheMetadata provides information about cache state
type CacheMetadata struct {
	Source  string    `json:"source"`  // "cache" or "fresh"
	Age     int       `json:"age"`     // Age in seconds
	IsStale bool      `json:"isStale"` // Whether data is stale
	NextRefresh time.Time `json:"nextRefresh,omitempty"` // When next refresh will occur
}

// TorrentResponse represents a response containing torrents with stats and cache metadata
type TorrentResponse struct {
	Torrents []qbt.Torrent `json:"torrents"`
	Total    int           `json:"total"`
	Stats    *TorrentStats `json:"stats,omitempty"`
	Counts   *TorrentCounts `json:"counts,omitempty"` // Include counts for sidebar
	Categories map[string]qbt.Category `json:"categories,omitempty"` // Include categories for sidebar
	Tags     []string `json:"tags,omitempty"` // Include tags for sidebar
	CacheMetadata *CacheMetadata `json:"cacheMetadata,omitempty"` // Cache state information
	HasMore  bool          `json:"hasMore"`    // Whether more pages are available
	SessionID string       `json:"sessionId,omitempty"` // Optional session tracking
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

// SyncManager manages SyncMainData for efficient torrent updates
type SyncManager struct {
	clientPool   *ClientPool
	mainData     map[int]*qbt.MainData
	ridTracker   map[int]int64
	mu           sync.RWMutex
	cache        *ristretto.Cache
	cacheCleared time.Time // Track when cache was last cleared to avoid re-caching stale data
}

// NewSyncManager creates a new sync manager
func NewSyncManager(clientPool *ClientPool) *SyncManager {
	return &SyncManager{
		clientPool: clientPool,
		mainData:   make(map[int]*qbt.MainData),
		ridTracker: make(map[int]int64),
		cache:      clientPool.GetCache(),
	}
}

// InitialLoad performs initial paginated load of torrents
func (sm *SyncManager) InitialLoad(ctx context.Context, instanceID int, limit, offset int) (*TorrentResponse, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("torrents:%d:%d:%d", instanceID, offset, limit)
	if cached, found := sm.cache.Get(cacheKey); found {
		if response, ok := cached.(*TorrentResponse); ok {
			return response, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Use GetTorrentsCtx for initial paginated load
	opts := qbt.TorrentFilterOptions{
		Limit:   limit,
		Offset:  offset,
		Sort:    "added_on",
		Reverse: true,
	}

	torrents, err := client.GetTorrentsCtx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	// Get total count
	total := sm.getTotalCount(ctx, instanceID)

	response := &TorrentResponse{
		Torrents: torrents,
		Total:    total,
	}

	// Cache the response with shorter TTL for more responsive updates
	if !sm.shouldSkipCache() {
		sm.cache.SetWithTTL(cacheKey, response, 1, 2*time.Second)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("count", len(torrents)).
		Int("total", total).
		Msg("Initial torrent load completed")

	return response, nil
}

// GetTorrentsWithSearch gets torrents with search, sorting, and pagination with stats
func (sm *SyncManager) GetTorrentsWithSearch(ctx context.Context, instanceID int, limit, offset int, sort, order, search string) (*TorrentResponse, error) {
	// Build cache key
	cacheKey := fmt.Sprintf("torrents:search:%d:%d:%d:%s:%s:%s", instanceID, offset, limit, sort, order, search)
	if cached, found := sm.cache.Get(cacheKey); found {
		if response, ok := cached.(*TorrentResponse); ok {
			return response, nil
		}
	}

	// Get all torrents for stats calculation (cached separately)
	allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, search)
	if err != nil {
		return nil, fmt.Errorf("failed to get all torrents for stats: %w", err)
	}

	// Filter torrents by search if provided
	var filteredTorrents []qbt.Torrent
	if search != "" {
		filteredTorrents = sm.filterTorrentsBySearch(allTorrents, search)
	} else {
		filteredTorrents = allTorrents
	}

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

	response := &TorrentResponse{
		Torrents: paginatedTorrents,
		Total:    len(filteredTorrents),
		Stats:    stats,
	}

	// Cache the response with shorter TTL for more responsive updates
	if !sm.shouldSkipCache() {
		sm.cache.SetWithTTL(cacheKey, response, 1, 2*time.Second)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("count", len(paginatedTorrents)).
		Int("total", len(filteredTorrents)).
		Str("search", search).
		Msg("Torrent search completed")

	return response, nil
}

// GetTorrentsWithFilters gets torrents with filters, search, sorting, and pagination
// Implements stale-while-revalidate pattern for responsive UI
func (sm *SyncManager) GetTorrentsWithFilters(ctx context.Context, instanceID int, limit, offset int, sort, order, search string, filters FilterOptions) (*TorrentResponse, error) {
	// No longer caching filtered results - always compute from all_torrents cache
	// This ensures optimistic updates are always reflected
	cacheKey := fmt.Sprintf("torrents:filtered:%d:%d:%d:%s:%s:%s:%+v", instanceID, offset, limit, sort, order, search, filters)
	
	// Always fetch from all_torrents cache and apply filters
	// This uses the optimistically updated cache as the single source of truth
	return sm.fetchFreshTorrentData(ctx, instanceID, limit, offset, sort, order, search, filters, cacheKey)
}

// fetchFreshTorrentData fetches fresh torrent data from all_torrents cache and applies filters
func (sm *SyncManager) fetchFreshTorrentData(ctx context.Context, instanceID int, limit, offset int, sort, order, search string, filters FilterOptions, _ string) (*TorrentResponse, error) {
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
		Torrents: paginatedTorrents,
		Total:    len(filteredTorrents),
		Stats:    stats,
		Counts:   counts,  // Include counts for sidebar
		Categories: categories, // Include categories for sidebar
		Tags:     tags,     // Include tags for sidebar
		HasMore:  hasMore,
		CacheMetadata: &CacheMetadata{
			Source:  cacheSource,
			Age:     0, // TODO: Track actual cache age if needed
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

// GetUpdates gets real-time updates using SyncMainData
func (sm *SyncManager) GetUpdates(ctx context.Context, instanceID int) (*qbt.MainData, error) {
	sm.mu.Lock()
	rid := sm.ridTracker[instanceID]
	sm.mu.Unlock()

	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get sync data
	mainData, err := client.SyncMainDataCtx(ctx, rid)
	if err != nil {
		return nil, fmt.Errorf("failed to sync main data: %w", err)
	}

	// Update RID for next request
	sm.mu.Lock()
	sm.ridTracker[instanceID] = mainData.Rid

	// Merge updates into existing data
	if existing, ok := sm.mainData[instanceID]; ok && !mainData.FullUpdate {
		sm.mergeMainData(existing, mainData)
		sm.mainData[instanceID] = existing
	} else {
		sm.mainData[instanceID] = mainData
	}
	sm.mu.Unlock()

	log.Debug().
		Int("instanceID", instanceID).
		Int64("rid", mainData.Rid).
		Bool("fullUpdate", mainData.FullUpdate).
		Msg("Sync update completed")

	return mainData, nil
}

// GetFilteredTorrents gets torrents with filtering and pagination
func (sm *SyncManager) GetFilteredTorrents(ctx context.Context, instanceID int, opts qbt.TorrentFilterOptions) (*TorrentResponse, error) {
	// Build cache key based on filter options
	cacheKey := fmt.Sprintf("torrents:filtered:%d:%+v", instanceID, opts)
	if cached, found := sm.cache.Get(cacheKey); found {
		if response, ok := cached.(*TorrentResponse); ok {
			return response, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get filtered torrents
	torrents, err := client.GetTorrentsCtx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get filtered torrents: %w", err)
	}

	// For filtered results, we need to get total count differently
	// This is a limitation of the qBittorrent API
	total := len(torrents)
	if opts.Limit > 0 && len(torrents) == opts.Limit {
		// If we got exactly the limit, there might be more
		total = -1 // Indicate unknown total
	}

	response := &TorrentResponse{
		Torrents: torrents,
		Total:    total,
	}

	// Cache with shorter TTL for filtered results
	if !sm.shouldSkipCache() {
		sm.cache.SetWithTTL(cacheKey, response, 1, 5*time.Second)
	}

	return response, nil
}

// BulkAction performs bulk operations on torrents
func (sm *SyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
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
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Use AddTorrentFromMemoryCtx which accepts byte array
	return client.AddTorrentFromMemoryCtx(ctx, fileContent, options)
}

// AddTorrentFromURLs adds new torrents from URLs or magnet links
func (sm *SyncManager) AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error {
	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
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
	client, err := sm.clientPool.GetClient(instanceID)
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
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get tags
	tags, err := client.GetTagsCtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags: %w", err)
	}

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
	client, err := sm.clientPool.GetClient(instanceID)
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
	client, err := sm.clientPool.GetClient(instanceID)
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
	client, err := sm.clientPool.GetClient(instanceID)
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

// GetTorrentWebSeeds gets web seeds for a specific torrent
func (sm *SyncManager) GetTorrentWebSeeds(ctx context.Context, instanceID int, hash string) ([]qbt.WebSeed, error) {
	// Check cache
	cacheKey := fmt.Sprintf("torrent:webseeds:%d:%s", instanceID, hash)
	if cached, found := sm.cache.Get(cacheKey); found {
		if seeds, ok := cached.([]qbt.WebSeed); ok {
			return seeds, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Get web seeds
	seeds, err := client.GetTorrentsWebSeedsCtx(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrent web seeds: %w", err)
	}

	// Cache for 30 seconds
	sm.cache.SetWithTTL(cacheKey, seeds, 1, 30*time.Second)

	return seeds, nil
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
			torrentTags := strings.Split(torrent.Tags, ", ")
			for _, tag := range torrentTags {
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
			torrentTags := strings.Split(torrent.Tags, ",")
			for _, tag := range torrentTags {
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
	client, err := sm.clientPool.GetClient(instanceID)
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

func (sm *SyncManager) getTotalCount(ctx context.Context, instanceID int) int {
	// Check cache
	cacheKey := fmt.Sprintf("torrent_count:%d", instanceID)
	if cached, found := sm.cache.Get(cacheKey); found {
		if count, ok := cached.(int); ok {
			return count
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return 0
	}

	// Get all torrents to count (this is inefficient but necessary)
	// In the future, we might want to maintain a count in the database
	torrents, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	if err != nil {
		return 0
	}

	count := len(torrents)

	// Cache for shorter time for more responsive updates
	if !sm.shouldSkipCache() {
		sm.cache.SetWithTTL(cacheKey, count, 1, 2*time.Second)
	}

	return count
}

func (sm *SyncManager) mergeMainData(existing, update *qbt.MainData) {
	// Merge torrents
	if existing.Torrents == nil {
		existing.Torrents = make(map[string]qbt.Torrent)
	}
	for hash, torrent := range update.Torrents {
		existing.Torrents[hash] = torrent
	}

	// Remove deleted torrents
	for _, hash := range update.TorrentsRemoved {
		delete(existing.Torrents, hash)
	}

	// Merge categories
	if update.Categories != nil {
		existing.Categories = update.Categories
	}

	// Merge tags
	if update.Tags != nil {
		existing.Tags = update.Tags
	}

	// Update server state (ServerState is a struct, not a pointer)
	existing.ServerState = update.ServerState
}

// ResetRID resets the RID for an instance (useful after reconnection)
func (sm *SyncManager) ResetRID(instanceID int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.ridTracker, instanceID)
	delete(sm.mainData, instanceID)
}

// InvalidateCache clears all cached data for a specific instance
// NOTE: With optimistic updates, this is rarely needed now
func (sm *SyncManager) InvalidateCache(instanceID int) {
	log.Debug().Int("instanceID", instanceID).Msg("Invalidating cache for instance")

	// Delete specific cache keys for this instance only
	// This prevents affecting other instances' caches
	keysToDelete := []string{
		// All torrents variations
		fmt.Sprintf("all_torrents:%d:", instanceID), // Empty search
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
			for page := 0; page < 10; page++ {
				for _, limit := range []int{100, 200, 500, 1000} {
					paginatedKey := fmt.Sprintf("%s%d:%d", keyPrefix, page*limit, limit)
					sm.cache.Del(paginatedKey)
				}
			}
		}
		
		// For search results, we can't predict all search terms
		// but we can clear common empty searches
		if keyPrefix == fmt.Sprintf("all_torrents:%d:", instanceID) {
			sm.cache.Del(keyPrefix) // Empty search
			sm.cache.Del(fmt.Sprintf("all_torrents:%d: ", instanceID)) // Space
		}
	}
	
	log.Debug().Int("instanceID", instanceID).Msg("Instance-specific cache invalidation completed")
}

// invalidateTagsCache invalidates the tags cache for a specific instance
func (sm *SyncManager) invalidateTagsCache(instanceID int) {
	cacheKey := fmt.Sprintf("tags:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated tags cache")
}

// invalidateCategoriesCache invalidates the categories cache for a specific instance
func (sm *SyncManager) invalidateCategoriesCache(instanceID int) {
	cacheKey := fmt.Sprintf("categories:%d", instanceID)
	sm.cache.Del(cacheKey)
	log.Debug().Int("instanceID", instanceID).Msg("Invalidated categories cache")
}

// shouldSkipCache returns true if we should skip caching (cache was recently cleared)
func (sm *SyncManager) shouldSkipCache() bool {
	// With optimistic updates, we no longer need to skip caching after actions
	// The cache is kept updated with expected changes
	return false
}

// applyOptimisticCacheUpdate applies optimistic updates to cached torrents
func (sm *SyncManager) applyOptimisticCacheUpdate(instanceID int, hashes []string, action string, payload map[string]interface{}) {
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
								newTags := strings.Split(tags, ",")
								for _, tag := range newTags {
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
							// Just log that we would update it
							if _, ok := payload["enable"].(bool); ok {
								// Note: AutoTMM field not available in current qbt.Torrent struct
								// This would be updated when fetching fresh data
							}
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
func (sm *SyncManager) getNativeFilteredTorrents(ctx context.Context, instanceID int, filters FilterOptions) ([]qbt.Torrent, error) {
	// Build filter options for qBittorrent API
	var opts qbt.TorrentFilterOptions

	// Map our status filters to qBittorrent's filter values
	if len(filters.Status) == 1 {
		status := filters.Status[0]
		// Map our status values to qBittorrent's expected values
		switch status {
		case "all":
			opts.Filter = "all"
		case "downloading":
			opts.Filter = "downloading"
		case "seeding":
			opts.Filter = "seeding"
		case "completed":
			opts.Filter = "completed"
		case "paused":
			opts.Filter = "paused"
		case "active":
			opts.Filter = "active"
		case "inactive":
			opts.Filter = "inactive"
		case "resumed":
			opts.Filter = "resumed"
		case "stalled":
			opts.Filter = "stalled"
		case "stalled_uploading":
			opts.Filter = "stalled_uploading"
		case "stalled_downloading":
			opts.Filter = "stalled_downloading"
		case "errored":
			opts.Filter = "errored"
		case "checking":
			// "checking" is not a valid filter in the go-qbittorrent library
			// checkingUP, checkingDL, checkingResumeData are states, not filters
			// Need to fetch all and filter in memory
			log.Debug().Str("status", status).Msg("Checking status requires in-memory filtering")
			allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
			if err != nil {
				return nil, err
			}
			// Apply the checking filter
			var filtered []qbt.Torrent
			for _, torrent := range allTorrents {
				if sm.matchTorrentStatus(torrent, "checking") {
					filtered = append(filtered, torrent)
				}
			}
			return filtered, nil
		case "moving":
			// "moving" is not a valid filter in the go-qbittorrent library
			// It's a state, not a filter - need to fetch all and filter in memory
			log.Debug().Str("status", status).Msg("Moving status requires in-memory filtering")
			allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
			if err != nil {
				return nil, err
			}
			// Apply the moving filter
			var filtered []qbt.Torrent
			for _, torrent := range allTorrents {
				if sm.matchTorrentStatus(torrent, "moving") {
					filtered = append(filtered, torrent)
				}
			}
			return filtered, nil
		default:
			// If we don't recognize the status, fall back to fetching all
			log.Debug().Str("status", status).Msg("Unknown status filter, falling back to fetch all")
			return sm.getAllTorrentsForStats(ctx, instanceID, "")
		}
	}

	// Single category filter
	if len(filters.Categories) == 1 {
		opts.Category = filters.Categories[0]
	}

	// Single tag filter
	if len(filters.Tags) == 1 {
		opts.Tag = filters.Tags[0]
	}

	// Build cache key for this specific filter
	cacheKey := fmt.Sprintf("native_filtered:%d:%+v", instanceID, opts)
	if cached, found := sm.cache.Get(cacheKey); found {
		if torrents, ok := cached.([]qbt.Torrent); ok {
			log.Debug().Int("instanceID", instanceID).Interface("opts", opts).Msg("Returning cached native filtered torrents")
			return torrents, nil
		}
	}

	// Get client
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Measure response time for dynamic caching
	startTime := time.Now()

	// Get filtered torrents directly from qBittorrent
	torrents, err := client.GetTorrentsCtx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get filtered torrents: %w", err)
	}

	// Calculate response time
	responseTime := time.Since(startTime)

	// IMPORTANT: Validate that the filter actually worked
	// For large instances, qBittorrent might ignore filters and return all torrents
	// If we're filtering by tag/category and get back > 5000 torrents, something might be wrong
	if (opts.Tag != "" || opts.Category != "") && len(torrents) > 5000 {
		log.Warn().
			Int("instanceID", instanceID).
			Str("tag", opts.Tag).
			Str("category", opts.Category).
			Int("returnedCount", len(torrents)).
			Dur("responseTime", responseTime).
			Msg("Tag/Category filter may have been ignored by qBittorrent - too many results")

		// Don't cache potentially wrong results for tag filters on large instances
		// Fall back to in-memory filtering
		allTorrents, err := sm.getAllTorrentsForStats(ctx, instanceID, "")
		if err != nil {
			return nil, fmt.Errorf("failed to get all torrents for filtering: %w", err)
		}

		// Apply tag or category filter in memory
		var filtered []qbt.Torrent
		for _, torrent := range allTorrents {
			// Check tag filter
			if opts.Tag != "" && !sm.torrentHasTag(torrent, opts.Tag) {
				continue
			}
			// Check category filter
			if opts.Category != "" && torrent.Category != opts.Category {
				continue
			}
			filtered = append(filtered, torrent)
		}

		log.Debug().
			Int("instanceID", instanceID).
			Str("tag", opts.Tag).
			Str("category", opts.Category).
			Int("filteredCount", len(filtered)).
			Msg("Applied tag/category filter in memory")

		// Cache the correctly filtered results
		if !sm.shouldSkipCache() {
			sm.cache.SetWithTTL(cacheKey, filtered, 1, 15*time.Second)
		}

		return filtered, nil
	}

	// Dynamic cache TTL based on response time (same logic as getAllTorrentsForStats)
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

	if !sm.shouldSkipCache() {
		sm.cache.SetWithTTL(cacheKey, torrents, 1, cacheTTL)
	}

	log.Debug().
		Int("instanceID", instanceID).
		Interface("opts", opts).
		Int("count", len(torrents)).
		Dur("responseTime", responseTime).
		Dur("cacheTTL", cacheTTL).
		Msg("Native filtered torrents fetched and cached")

	return torrents, nil
}

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
	client, err := sm.clientPool.GetClient(instanceID)
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

	if !sm.shouldSkipCache() {
		sm.cache.SetWithTTL(cacheKey, torrents, 1, cacheTTL)
	}

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

// isGlobPattern checks if a search string contains glob pattern characters
func isGlobPattern(search string) bool {
	// Check for glob metacharacters: *, ?, [
	// Note: We check for [ but not ] alone, as ] without [ is not a glob pattern
	return strings.ContainsAny(search, "*?[")
}

// filterTorrentsBySearch filters torrents by search string with smart matching
func (sm *SyncManager) filterTorrentsBySearch(torrents []qbt.Torrent, search string) []qbt.Torrent {
	if search == "" {
		return torrents
	}

	// Check if search contains glob patterns
	if isGlobPattern(search) {
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
			tags := strings.Split(tagsLower, ", ")
			for _, tag := range tags {
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
			categoryMatch := false
			for _, category := range filters.Categories {
				if torrent.Category == category {
					categoryMatch = true
					break
				}
			}
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
				for _, torrentTag := range torrentTags {
					if torrentTag == filterTag {
						tagMatch = true
						break
					}
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

// torrentHasTag checks if a torrent has a specific tag
func (sm *SyncManager) torrentHasTag(torrent qbt.Torrent, tag string) bool {
	if tag == "" && torrent.Tags == "" {
		// Handle "Untagged" filter
		return true
	}

	torrentTags := strings.Split(torrent.Tags, ", ")
	for _, torrentTag := range torrentTags {
		if torrentTag == tag {
			return true
		}
	}
	return false
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
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.AddTagsCtx(ctx, hashes, tags); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "addTags", map[string]interface{}{"tags": tags})
	return nil
}

// RemoveTags removes specific tags from the specified torrents
func (sm *SyncManager) RemoveTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.RemoveTagsCtx(ctx, hashes, tags); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "removeTags", map[string]interface{}{"tags": tags})
	return nil
}

// SetTags sets tags on the specified torrents (replaces all existing tags)
// This uses the new qBittorrent 5.1+ API if available, otherwise returns error
func (sm *SyncManager) SetTags(ctx context.Context, instanceID int, hashes []string, tags string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	// Try to use the new SetTags method (qBittorrent 5.1+)
	if err := client.Client.SetTags(ctx, hashes, tags); err != nil {
		// If it fails due to version requirement, return the error
		// The frontend will handle the fallback to addTags
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "setTags", map[string]interface{}{"tags": tags})
	return nil
}

// SetCategory sets the category for the specified torrents
func (sm *SyncManager) SetCategory(ctx context.Context, instanceID int, hashes []string, category string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.SetCategoryCtx(ctx, hashes, category); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "setCategory", map[string]interface{}{"category": category})
	return nil
}

// SetAutoTMM sets the automatic torrent management for torrents
func (sm *SyncManager) SetAutoTMM(ctx context.Context, instanceID int, hashes []string, enable bool) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.SetAutoManagementCtx(ctx, hashes, enable); err != nil {
		return err
	}

	// Apply optimistic update to cache
	sm.applyOptimisticCacheUpdate(instanceID, hashes, "toggleAutoTMM", map[string]interface{}{"enable": enable})
	return nil
}

// CreateTags creates new tags
func (sm *SyncManager) CreateTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.CreateTagsCtx(ctx, tags); err != nil {
		return err
	}

	sm.invalidateTagsCache(instanceID)
	return nil
}

// DeleteTags deletes tags
func (sm *SyncManager) DeleteTags(ctx context.Context, instanceID int, tags []string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.DeleteTagsCtx(ctx, tags); err != nil {
		return err
	}

	sm.invalidateTagsCache(instanceID)
	return nil
}

// CreateCategory creates a new category
func (sm *SyncManager) CreateCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.CreateCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	sm.invalidateCategoriesCache(instanceID)
	return nil
}

// EditCategory edits an existing category
func (sm *SyncManager) EditCategory(ctx context.Context, instanceID int, name string, path string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.EditCategoryCtx(ctx, name, path); err != nil {
		return err
	}

	sm.invalidateCategoriesCache(instanceID)
	return nil
}

// RemoveCategories removes categories
func (sm *SyncManager) RemoveCategories(ctx context.Context, instanceID int, categories []string) error {
	client, err := sm.clientPool.GetClient(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	if err := client.Client.RemoveCategoriesCtx(ctx, categories); err != nil {
		return err
	}

	sm.invalidateCategoriesCache(instanceID)
	return nil
}
