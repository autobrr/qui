// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// RacingTorrent represents a torrent with racing metrics
type RacingTorrent struct {
	Hash           string     `json:"hash"`
	Name           string     `json:"name"`
	Size           int64      `json:"size"`
	Tracker        string     `json:"tracker"`
	TrackerDomain  string     `json:"trackerDomain"`
	Ratio          float64    `json:"ratio"`
	CompletionTime *int64     `json:"completionTime,omitempty"` // Time to complete in seconds
	AddedOn        time.Time  `json:"addedOn"`
	CompletedOn    *time.Time `json:"completedOn,omitempty"`
	State          string     `json:"state"`
	Category       string     `json:"category"`
	Tags           string     `json:"tags"`
	InstanceID     int        `json:"instanceId"`
	InstanceName   string     `json:"instanceName"`
}

// RacingDashboard represents the complete racing dashboard data
type RacingDashboard struct {
	TopFastest   []RacingTorrent `json:"topFastest"`   // Torrents that completed quickest
	TopRatios    []RacingTorrent `json:"topRatios"`    // Torrents with highest ratio
	BottomRatios []RacingTorrent `json:"bottomRatios"` // Torrents with lowest ratio
	TrackerStats TrackerStats    `json:"trackerStats"` // Statistics per tracker
	LastUpdated  time.Time       `json:"lastUpdated"`
}

// TrackerStats represents statistics for each tracker
type TrackerStats struct {
	TotalTorrents         int                    `json:"totalTorrents"`
	CompletedTorrents     int                    `json:"completedTorrents"`
	AverageRatio          float64                `json:"averageRatio"`
	AverageCompletionTime *int64                 `json:"averageCompletionTime,omitempty"`
	ByTracker             map[string]TrackerData `json:"byTracker"`
}

// TrackerData represents data for a specific tracker
type TrackerData struct {
	TotalTorrents         int     `json:"totalTorrents"`
	CompletedTorrents     int     `json:"completedTorrents"`
	AverageRatio          float64 `json:"averageRatio"`
	AverageCompletionTime *int64  `json:"averageCompletionTime,omitempty"`
	InstanceID            int     `json:"instanceId"`
	InstanceName          string  `json:"instanceName"`
}

// RacingDashboardOptions represents options for the racing dashboard
type RacingDashboardOptions struct {
	Limit          int      `json:"limit"`          // Number of torrents to show in each category (default: 5)
	InstanceIDs    []int    `json:"instanceIds"`    // Instance IDs to include (empty = all configured instances)
	TrackerFilter  []string `json:"trackerFilter"`  // Filter by specific trackers (empty = all)
	MinRatio       float64  `json:"minRatio"`       // Minimum ratio to include (default: 0)
	MinSize        int64    `json:"minSize"`        // Minimum size in bytes (default: 0)
	MaxSize        int64    `json:"maxSize"`        // Maximum size in bytes (default: 0 = no limit)
	CategoryFilter []string `json:"categoryFilter"` // Filter by categories (empty = all)
	StartDate      string   `json:"startDate"`      // Start date for filtering (ISO format)
	EndDate        string   `json:"endDate"`        // End date for filtering (ISO format)
	TimeRange      string   `json:"timeRange"`      // Preset time range (e.g., "24h", "7d", "30d")
}

// RacingManager manages racing dashboard functionality
type RacingManager struct {
	syncManager *SyncManager
}

// NewRacingManager creates a new racing manager
func NewRacingManager(syncManager *SyncManager) *RacingManager {
	return &RacingManager{
		syncManager: syncManager,
	}
}

// GetRacingDashboard generates the racing dashboard data for multiple instances
func (rm *RacingManager) GetRacingDashboard(ctx context.Context, options RacingDashboardOptions) (*RacingDashboard, error) {
	// Set defaults
	if options.Limit == 0 {
		options.Limit = 5
	}

	// If no instances specified, use all available instances
	instanceIDs := options.InstanceIDs
	if len(instanceIDs) == 0 {
		// Get all configured instance IDs from the pool
		instanceIDs = rm.syncManager.clientPool.GetAllInstanceIDs()
	}

	// Collect torrents from all specified instances
	var allRacingTorrents []RacingTorrent
	for _, instanceID := range instanceIDs {
		// Get instance info
		instanceInfo, err := rm.syncManager.clientPool.GetInstanceInfo(instanceID)
		if err != nil {
			log.Warn().Int("instanceID", instanceID).Err(err).Msg("Failed to get instance info, skipping")
			continue
		}

		// Get all torrents from this instance
		torrents, err := rm.syncManager.getAllTorrentsForStats(ctx, instanceID, "")
		if err != nil {
			log.Warn().Int("instanceID", instanceID).Err(err).Msg("Failed to get torrents for instance, skipping")
			continue
		}

		// Convert to racing torrents with instance info and apply filters
		racingTorrents := rm.convertToRacingTorrentsWithInstance(torrents, options, instanceID, instanceInfo.Name)
		allRacingTorrents = append(allRacingTorrents, racingTorrents...)
	}

	// Calculate racing metrics
	dashboard := &RacingDashboard{
		LastUpdated: time.Now(),
	}

	// Get top fastest completed torrents
	dashboard.TopFastest = rm.getTopFastest(allRacingTorrents, options.Limit)

	// Get top ratios
	dashboard.TopRatios = rm.getTopRatios(allRacingTorrents, options.Limit)

	// Get bottom ratios
	dashboard.BottomRatios = rm.getBottomRatios(allRacingTorrents, options.Limit)

	// Calculate tracker statistics
	dashboard.TrackerStats = rm.calculateTrackerStats(allRacingTorrents)

	log.Debug().
		Ints("instanceIDs", instanceIDs).
		Int("totalTorrents", len(allRacingTorrents)).
		Int("topFastest", len(dashboard.TopFastest)).
		Int("topRatios", len(dashboard.TopRatios)).
		Int("bottomRatios", len(dashboard.BottomRatios)).
		Msg("Generated racing dashboard")

	return dashboard, nil
}

// convertToRacingTorrentsWithInstance converts qbt.Torrent to RacingTorrent with instance info and filtering
func (rm *RacingManager) convertToRacingTorrentsWithInstance(torrents []qbt.Torrent, options RacingDashboardOptions, instanceID int, instanceName string) []RacingTorrent {
	var racingTorrents []RacingTorrent
	filtered := 0

	for _, torrent := range torrents {
		// Apply filters
		if !rm.matchesFilters(torrent, options) {
			filtered++
			continue
		}

		racingTorrent := RacingTorrent{
			Hash:         torrent.Hash,
			Name:         torrent.Name,
			Size:         torrent.Size,
			Tracker:      torrent.Tracker,
			State:        string(torrent.State),
			Category:     torrent.Category,
			Tags:         torrent.Tags,
			Ratio:        torrent.Ratio,
			AddedOn:      time.Unix(torrent.AddedOn, 0),
			InstanceID:   instanceID,
			InstanceName: instanceName,
		}

		// Extract tracker domain
		if torrent.Tracker != "" {
			racingTorrent.TrackerDomain = rm.syncManager.extractDomainFromURL(torrent.Tracker)
		}

		// Calculate completion time if torrent is completed
		if torrent.Progress == 1 && torrent.CompletionOn > 0 {
			racingTorrent.CompletedOn = &time.Time{}
			*racingTorrent.CompletedOn = time.Unix(torrent.CompletionOn, 0)

			// Calculate time to complete
			if torrent.CompletionOn >= torrent.AddedOn {
				completionTime := torrent.CompletionOn - torrent.AddedOn
				racingTorrent.CompletionTime = &completionTime
			}
		}

		racingTorrents = append(racingTorrents, racingTorrent)
	}

	if options.TimeRange != "" {
		log.Debug().
			Int("instanceID", instanceID).
			Int("totalTorrents", len(torrents)).
			Int("filtered", filtered).
			Int("remaining", len(racingTorrents)).
			Str("timeRange", options.TimeRange).
			Msg("RACING TIME FILTER APPLIED")
	}

	return racingTorrents
}

// matchesFilters checks if a torrent matches the filter criteria
func (rm *RacingManager) matchesFilters(torrent qbt.Torrent, options RacingDashboardOptions) bool {
	// Size filters
	if options.MinSize > 0 && torrent.Size < options.MinSize {
		return false
	}
	if options.MaxSize > 0 && torrent.Size > options.MaxSize {
		return false
	}

	// Ratio filter
	if torrent.Ratio < options.MinRatio {
		return false
	}

	// Category filter
	if len(options.CategoryFilter) > 0 {
		found := slices.Contains(options.CategoryFilter, torrent.Category)
		if !found {
			return false
		}
	}

	// Tracker filter
	if len(options.TrackerFilter) > 0 {
		trackerDomain := rm.syncManager.extractDomainFromURL(torrent.Tracker)
		found := false
		for _, tracker := range options.TrackerFilter {
			if trackerDomain == tracker || torrent.Tracker == tracker {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Apply time filtering using the shared helper
	if !matchesTimeFilter(torrent, options) {
		return false
	}

	return true
}

// getTopFastest returns the fastest completed torrents
func (rm *RacingManager) getTopFastest(torrents []RacingTorrent, limit int) []RacingTorrent {
	var completed []RacingTorrent

	// Filter to only completed torrents with completion time
	for _, torrent := range torrents {
		if torrent.CompletionTime != nil && *torrent.CompletionTime > 0 {
			completed = append(completed, torrent)
		}
	}

	// Sort by completion time (fastest first)
	sort.Slice(completed, func(i, j int) bool {
		return *completed[i].CompletionTime < *completed[j].CompletionTime
	})

	// Return top N
	if len(completed) > limit {
		return completed[:limit]
	}
	return completed
}

// getTopRatios returns torrents with highest ratio
func (rm *RacingManager) getTopRatios(torrents []RacingTorrent, limit int) []RacingTorrent {
	// Sort by ratio descending
	sorted := make([]RacingTorrent, len(torrents))
	copy(sorted, torrents)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Ratio > sorted[j].Ratio
	})

	// Return top N
	if len(sorted) > limit {
		return sorted[:limit]
	}
	return sorted
}

// getBottomRatios returns torrents with lowest ratio
func (rm *RacingManager) getBottomRatios(torrents []RacingTorrent, limit int) []RacingTorrent {
	// Sort by ratio ascending
	sorted := make([]RacingTorrent, len(torrents))
	copy(sorted, torrents)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Ratio < sorted[j].Ratio
	})

	// Return bottom N
	if len(sorted) > limit {
		return sorted[:limit]
	}
	return sorted
}

// calculateTrackerStats calculates statistics per tracker
func (rm *RacingManager) calculateTrackerStats(torrents []RacingTorrent) TrackerStats {
	stats := TrackerStats{
		ByTracker: make(map[string]TrackerData),
	}

	totalRatio := 0.0
	totalCompletionTime := int64(0)

	for _, torrent := range torrents {
		stats.TotalTorrents++

		// Create composite key: tracker_instanceId
		trackerKey := torrent.TrackerDomain
		if trackerKey == "" {
			trackerKey = "Unknown"
		}

		// Create unique key combining tracker and instance
		compositeKey := fmt.Sprintf("%s_%d", trackerKey, torrent.InstanceID)

		// Initialize tracker data if not exists
		if _, exists := stats.ByTracker[compositeKey]; !exists {
			stats.ByTracker[compositeKey] = TrackerData{
				InstanceID:   torrent.InstanceID,
				InstanceName: torrent.InstanceName,
			}
		}

		trackerData := stats.ByTracker[compositeKey]
		trackerData.TotalTorrents++

		// Track ratio
		totalRatio += torrent.Ratio
		trackerData.AverageRatio += torrent.Ratio

		// Track completion time
		if torrent.CompletionTime != nil {
			stats.CompletedTorrents++
			trackerData.CompletedTorrents++
			totalCompletionTime += *torrent.CompletionTime

			// Initialize if needed, then accumulate
			if trackerData.AverageCompletionTime == nil {
				trackerData.AverageCompletionTime = new(int64)
			}
			*trackerData.AverageCompletionTime += *torrent.CompletionTime
		}

		stats.ByTracker[compositeKey] = trackerData
	}

	// Calculate averages
	if stats.TotalTorrents > 0 {
		stats.AverageRatio = totalRatio / float64(stats.TotalTorrents)
	}

	if stats.CompletedTorrents > 0 {
		avgTime := totalCompletionTime / int64(stats.CompletedTorrents)
		stats.AverageCompletionTime = &avgTime
	}

	// Calculate per-tracker averages
	for tracker, data := range stats.ByTracker {
		if data.TotalTorrents > 0 {
			data.AverageRatio = data.AverageRatio / float64(data.TotalTorrents)
		}
		if data.CompletedTorrents > 0 && data.AverageCompletionTime != nil {
			*data.AverageCompletionTime = *data.AverageCompletionTime / int64(data.CompletedTorrents)
		}
		stats.ByTracker[tracker] = data
	}

	return stats
}

// matchesTimeFilter checks if a torrent matches the time filter criteria
func matchesTimeFilter(torrent qbt.Torrent, options RacingDashboardOptions) bool {
	// Time filtering
	now := time.Now()

	// Handle preset time ranges
	if options.TimeRange != "" {
		var startTime time.Time
		switch options.TimeRange {
		case "24h":
			startTime = now.Add(-24 * time.Hour)
		case "7d":
			startTime = now.Add(-7 * 24 * time.Hour)
		case "30d":
			startTime = now.Add(-30 * 24 * time.Hour)
		default:
			// Invalid time range, ignore - pass through
			return true
		}

		if !startTime.IsZero() {
			// For completed torrents, filter by completion date
			// For non-completed torrents, filter by added date
			if torrent.CompletionOn > 0 {
				completedTime := time.Unix(torrent.CompletionOn, 0)
				if completedTime.Before(startTime) {
					return false
				}
			} else {
				addedTime := time.Unix(torrent.AddedOn, 0)
				if addedTime.Before(startTime) {
					return false
				}
			}
		}
	}

	// Handle custom date range
	if options.StartDate != "" || options.EndDate != "" {
		var startDate, endDate time.Time
		var startErr, endErr error

		if options.StartDate != "" {
			startDate, startErr = time.Parse(time.RFC3339, options.StartDate)
		}

		if options.EndDate != "" {
			endDate, endErr = time.Parse(time.RFC3339, options.EndDate)
			if endErr == nil {
				// Add 23:59:59 to end date to include the full day
				endDate = endDate.Add(24*time.Hour - time.Second)
			}
		}

		// Determine which timestamp to check based on completion status
		var checkTime time.Time
		if torrent.CompletionOn > 0 {
			// For completed torrents, use completion date
			checkTime = time.Unix(torrent.CompletionOn, 0)
		} else {
			// For non-completed torrents, use added date
			checkTime = time.Unix(torrent.AddedOn, 0)
		}

		// Check against start date
		if startErr == nil && !startDate.IsZero() {
			if checkTime.Before(startDate) {
				return false
			}
		}

		// Check against end date
		if endErr == nil && !endDate.IsZero() {
			if checkTime.After(endDate) {
				return false
			}
		}
	}

	return true
}

// GetRacingDashboard returns the racing dashboard data for multiple instances
func (sm *SyncManager) GetRacingDashboard(ctx context.Context, instanceID int, options RacingDashboardOptions) (*RacingDashboard, error) {
	// For backward compatibility, if instanceID is provided but options.InstanceIDs is empty,
	// use the instanceID
	if instanceID > 0 && len(options.InstanceIDs) == 0 {
		options.InstanceIDs = []int{instanceID}
	}

	// Use the RacingManager to generate the dashboard
	rm := NewRacingManager(sm)
	return rm.GetRacingDashboard(ctx, options)
}

// GetRacingDashboardMulti returns the racing dashboard data for multiple instances (new method)
func (sm *SyncManager) GetRacingDashboardMulti(ctx context.Context, options RacingDashboardOptions) (*RacingDashboard, error) {
	rm := NewRacingManager(sm)
	return rm.GetRacingDashboard(ctx, options)
}
