// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package publictrackers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	fetchTimeout    = 30 * time.Second
	maxResponseSize = 1 << 20 // 1 MiB
)

// PruneMode defines how existing trackers should be handled
type PruneMode string

const (
	PruneModeAll  PruneMode = "all"  // Remove all existing trackers
	PruneModeDead PruneMode = "dead" // Remove only dead/erroring trackers
	PruneModeNone PruneMode = "none" // Keep all existing trackers
)

// ActionResult contains the result of a public tracker action
type ActionResult struct {
	TotalTorrents   int      `json:"totalTorrents"`
	ProcessedCount  int      `json:"processedCount"`
	SkippedPrivate  int      `json:"skippedPrivate"`
	TrackersAdded   int      `json:"trackersAdded"`
	TrackersRemoved int      `json:"trackersRemoved"`
	Errors          []string `json:"errors,omitempty"`
}

// Service handles public tracker management
type Service struct {
	store       *models.PublicTrackerSettingsStore
	syncManager *qbittorrent.SyncManager
	httpClient  *http.Client
}

// NewService creates a new public tracker service
func NewService(store *models.PublicTrackerSettingsStore, syncManager *qbittorrent.SyncManager) *Service {
	return &Service{
		store:       store,
		syncManager: syncManager,
		httpClient:  &http.Client{Timeout: fetchTimeout},
	}
}

// GetSettings returns the current public tracker settings
func (s *Service) GetSettings(ctx context.Context) (*models.PublicTrackerSettings, error) {
	settings, err := s.store.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get public tracker settings: %w", err)
	}
	return settings, nil
}

// UpdateSettings updates the public tracker settings
func (s *Service) UpdateSettings(ctx context.Context, input *models.PublicTrackerSettingsInput) (*models.PublicTrackerSettings, error) {
	settings, err := s.store.Update(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("update public tracker settings: %w", err)
	}
	return settings, nil
}

// RefreshTrackerList fetches the tracker list from the configured URL and caches it
func (s *Service) RefreshTrackerList(ctx context.Context) (*models.PublicTrackerSettings, error) {
	settings, err := s.store.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}

	if settings.TrackerListURL == "" {
		return nil, errors.New("no tracker list URL configured")
	}

	trackers, err := s.fetchTrackersFromURL(ctx, settings.TrackerListURL)
	if err != nil {
		return nil, fmt.Errorf("fetch trackers: %w", err)
	}

	err = s.store.UpdateCachedTrackers(ctx, trackers)
	if err != nil {
		return nil, fmt.Errorf("update cached trackers: %w", err)
	}

	log.Info().Int("count", len(trackers)).Str("url", settings.TrackerListURL).Msg("Refreshed public tracker list")

	updatedSettings, err := s.store.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get updated settings: %w", err)
	}
	return updatedSettings, nil
}

// fetchTrackersFromURL fetches and parses a tracker list from a URL
func (s *Service) fetchTrackersFromURL(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "qui/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()
	defer httphelpers.DrainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Limit response size
	limitedReader := io.LimitReader(resp.Body, maxResponseSize)

	var trackers []string
	scanner := bufio.NewScanner(limitedReader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Basic validation - tracker URLs should start with a scheme
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") ||
			strings.HasPrefix(line, "udp://") || strings.HasPrefix(line, "wss://") {
			trackers = append(trackers, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return trackers, nil
}

// ExecuteAction performs a public tracker action on the specified torrents
func (s *Service) ExecuteAction(ctx context.Context, instanceID int, hashes []string, pruneMode PruneMode) (*ActionResult, error) {
	settings, err := s.store.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}

	// Ensure we have cached trackers
	if len(settings.CachedTrackers) == 0 {
		// Try to refresh
		settings, err = s.RefreshTrackerList(ctx)
		if err != nil {
			return nil, fmt.Errorf("no cached trackers and refresh failed: %w", err)
		}
		if len(settings.CachedTrackers) == 0 {
			return nil, errors.New("no trackers available from URL")
		}
	}

	result := &ActionResult{
		TotalTorrents: len(hashes),
	}

	// Filter out private torrents
	publicHashes, skippedCount := s.filterPublicTorrents(ctx, instanceID, hashes)
	result.SkippedPrivate = skippedCount

	if len(publicHashes) == 0 {
		return result, nil
	}

	// Handle pruning based on mode
	if pruneMode == PruneModeAll || pruneMode == PruneModeDead {
		removed, errs := s.pruneTrackers(ctx, instanceID, publicHashes, pruneMode)
		result.TrackersRemoved = removed
		result.Errors = append(result.Errors, errs...)
	}

	// Add new trackers
	trackersToAdd := strings.Join(settings.CachedTrackers, "\n")
	if err := s.syncManager.BulkAddTrackers(ctx, instanceID, publicHashes, trackersToAdd); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("add trackers: %v", err))
	} else {
		result.TrackersAdded = len(settings.CachedTrackers)
		result.ProcessedCount = len(publicHashes)
	}

	return result, nil
}

// filterPublicTorrents returns only public (non-private) torrent hashes
func (s *Service) filterPublicTorrents(ctx context.Context, instanceID int, hashes []string) (publicHashes []string, skipped int) {
	publicHashes = make([]string, 0, len(hashes))

	for _, hash := range hashes {
		props, err := s.syncManager.GetTorrentProperties(ctx, instanceID, hash)
		if err != nil {
			// If we can't get the torrent properties, skip it
			skipped++
			continue
		}

		// Check if torrent is private
		if props.IsPrivate {
			skipped++
			continue
		}

		publicHashes = append(publicHashes, hash)
	}

	return publicHashes, skipped
}

// shouldRemoveTracker determines if a tracker should be removed based on prune mode
func shouldRemoveTracker(tracker qbt.TorrentTracker, mode PruneMode) bool {
	// Skip DHT, PeX, LSD (status 0 = disabled for these)
	if tracker.Status == 0 || tracker.Url == "" {
		return false
	}

	switch mode {
	case PruneModeAll:
		return true
	case PruneModeDead:
		// Check if tracker is dead/erroring (status 4 = NotWorking)
		if tracker.Status == 4 {
			return true
		}
		return qbittorrent.TrackerMessageMatchesDown(tracker.Message) ||
			qbittorrent.TrackerMessageMatchesUnregistered(tracker.Message)
	case PruneModeNone:
		return false
	default:
		return false
	}
}

// pruneTrackers removes trackers based on the prune mode
func (s *Service) pruneTrackers(ctx context.Context, instanceID int, hashes []string, mode PruneMode) (totalRemoved int, errs []string) {
	for _, hash := range hashes {
		removed, err := s.pruneTrackersForHash(ctx, instanceID, hash, mode)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		totalRemoved += removed
	}
	return totalRemoved, errs
}

// pruneTrackersForHash removes trackers for a single torrent
func (s *Service) pruneTrackersForHash(ctx context.Context, instanceID int, hash string, mode PruneMode) (int, error) {
	trackers, err := s.syncManager.GetTorrentTrackers(ctx, instanceID, hash)
	if err != nil {
		return 0, fmt.Errorf("get trackers for %s: %w", hash[:8], err)
	}

	var urlsToRemove []string
	for _, tracker := range trackers {
		if shouldRemoveTracker(tracker, mode) {
			urlsToRemove = append(urlsToRemove, tracker.Url)
		}
	}

	if len(urlsToRemove) == 0 {
		return 0, nil
	}

	urlsStr := strings.Join(urlsToRemove, "\n")
	if err := s.syncManager.BulkRemoveTrackers(ctx, instanceID, []string{hash}, urlsStr); err != nil {
		return 0, fmt.Errorf("remove trackers from %s: %w", hash[:8], err)
	}

	return len(urlsToRemove), nil
}
