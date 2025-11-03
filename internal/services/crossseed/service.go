// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package crossseed provides intelligent cross-seeding functionality for torrents.
//
// Key features:
// - Uses moistari/rls parser for robust release name parsing on both torrent names and file names
// - TTL-based caching (5 minutes) of rls parsing results for performance (rls parsing is slow)
// - Fuzzy matching for finding related content (single episodes, season packs, etc.)
// - Metadata enrichment: fills missing group, resolution, codec, source, etc. from season pack torrent names
// - Season pack support: matches individual episodes with season packs and vice versa
// - Partial matching: detects when single episode files are contained within season packs
package crossseed

import (
	"context"
	"fmt"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
)

// Service provides cross-seed functionality
type Service struct {
	instanceStore *models.InstanceStore
	syncManager   *qbittorrent.SyncManager
	filesManager  *filesmanager.Service
	releaseCache  *ReleaseCache
}

// NewService creates a new cross-seed service
func NewService(
	instanceStore *models.InstanceStore,
	syncManager *qbittorrent.SyncManager,
	filesManager *filesmanager.Service,
) *Service {
	return &Service{
		instanceStore: instanceStore,
		syncManager:   syncManager,
		filesManager:  filesManager,
		releaseCache:  NewReleaseCache(),
	}
}

// FindCandidates finds potential cross-seed candidates on the SAME instance
// Cross-seeding means finding torrents that share the same files on disk
func (s *Service) FindCandidates(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	// Get all torrents from SyncManager for THIS instance
	torrents, err := s.syncManager.GetAllTorrents(ctx, req.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	// Find the source torrent by hash or name
	var sourceTorrent *qbt.Torrent
	for i := range torrents {
		if torrents[i].Hash == req.TorrentHash || torrents[i].Name == req.TorrentName {
			sourceTorrent = &torrents[i]
			break
		}
	}

	if sourceTorrent == nil {
		return nil, fmt.Errorf("source torrent not found: %s", req.TorrentHash)
	}

	// Get source torrent files from cache
	sourceFiles, err := s.filesManager.GetCachedFiles(ctx, req.SourceInstanceID, sourceTorrent.Hash, sourceTorrent.Progress)
	if err != nil {
		return nil, fmt.Errorf("failed to get source files from cache: %w", err)
	}

	if len(sourceFiles) == 0 {
		return nil, fmt.Errorf("source torrent has no cached files")
	}

	// Build source torrent info
	sourceTorrentInfo := &TorrentInfo{
		Hash:       sourceTorrent.Hash,
		Name:       sourceTorrent.Name,
		Size:       sourceTorrent.Size,
		Progress:   sourceTorrent.Progress,
		TotalFiles: len(sourceFiles),
		FileCount:  len(sourceFiles),
		Files:      make([]TorrentFile, len(sourceFiles)),
	}

	for i, f := range sourceFiles {
		sourceTorrentInfo.Files[i] = TorrentFile{
			Index: f.Index,
			Name:  f.Name,
			Size:  f.Size,
		}
	}

	// Parse source release name for normalized matching (cached)
	sourceRelease := s.releaseCache.Parse(sourceTorrent.Name)

	// Find matching torrents on SAME instance
	var matchedTorrents []qbt.Torrent

	for _, torrent := range torrents {
		// Skip source torrent
		if torrent.Hash == sourceTorrent.Hash {
			continue
		}

		// Only consider complete torrents
		if torrent.Progress < 1.0 {
			continue
		}

		// Parse candidate release name (cached)
		candidateRelease := s.releaseCache.Parse(torrent.Name)

		// Check if releases are related early - use fuzzy matching
		// We're looking for similar content, not exact matches
		if !s.releasesMatch(sourceRelease, candidateRelease) {
			continue
		}

		// Get candidate files from cache
		candidateFiles, err := s.filesManager.GetCachedFiles(ctx, req.SourceInstanceID, torrent.Hash, torrent.Progress)
		if err != nil || len(candidateFiles) == 0 {
			continue
		}

		// Check file matching with ignore patterns
		// This now supports exact matches, size matches, and partial matches for season packs
		matchType := s.getMatchType(sourceRelease, candidateRelease, sourceFiles, candidateFiles, req.IgnorePatterns)
		if matchType != "" {
			matchedTorrents = append(matchedTorrents, torrent)
			log.Debug().
				Str("source", sourceTorrent.Name).
				Str("candidate", torrent.Name).
				Str("matchType", matchType).
				Msg("Found cross-seed candidate")
		}
	}

	// Get instance info
	instance, err := s.instanceStore.Get(ctx, req.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	response := &FindCandidatesResponse{
		SourceTorrent: sourceTorrentInfo,
		Candidates:    make([]CrossSeedCandidate, 0),
	}

	if len(matchedTorrents) > 0 {
		response.Candidates = append(response.Candidates, CrossSeedCandidate{
			InstanceID:   req.SourceInstanceID,
			InstanceName: instance.Name,
			Torrents:     matchedTorrents,
			MatchType:    "exact",
		})
	}

	log.Info().
		Str("sourceTorrent", sourceTorrent.Name).
		Int("candidates", len(matchedTorrents)).
		Msg("Found cross-seed candidates on same instance")

	return response, nil
}

// releasesMatch checks if two releases are related using fuzzy matching
// This allows matching similar content that isn't exactly the same
func (s *Service) releasesMatch(source, candidate rls.Release) bool {
	// Title should match closely but not necessarily exactly
	// This handles variations in title formatting
	sourceTitleLower := strings.ToLower(strings.TrimSpace(source.Title))
	candidateTitleLower := strings.ToLower(strings.TrimSpace(candidate.Title))

	if sourceTitleLower == "" || candidateTitleLower == "" {
		return false
	}

	// Check if titles are similar (exact match or one contains the other)
	if sourceTitleLower != candidateTitleLower &&
		!strings.Contains(sourceTitleLower, candidateTitleLower) &&
		!strings.Contains(candidateTitleLower, sourceTitleLower) {
		return false
	}

	// Year should match if both are present
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false
	}

	// For TV shows, season must match but episodes can differ
	// This allows matching single episodes with season packs
	if source.Series > 0 || candidate.Series > 0 {
		// If one has a season but the other doesn't, skip season check
		if source.Series > 0 && candidate.Series > 0 {
			if source.Series != candidate.Series {
				return false
			}
		}
		// Don't enforce episode matching here - we'll handle that in file matching
		// This allows a single episode (e.g., S01E05) to match a season pack (S01)
	}

	// Resolution matching is optional - different qualities can cross-seed if files match
	// Don't enforce resolution, version, group, etc. - let file matching decide

	return true
}

// getMatchType determines if files match for cross-seeding
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match
func (s *Service) getMatchType(sourceRelease, candidateRelease rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build map of source files (name -> size) and (basename -> size)
	// Parse each file with rls and enrich with torrent metadata
	sourceMap := make(map[string]int64)
	sourceBasenames := make(map[string]int64)
	totalSourceSize := int64(0)

	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, ignorePatterns) {
			sourceMap[sf.Name] = sf.Size

			// Parse file and enrich with torrent metadata (cached)
			fileRelease := s.releaseCache.Parse(sf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, sourceRelease)

			// Extract basename using enriched release for better matching
			basename := s.extractBasename(sf.Name)
			if basename != "" {
				sourceBasenames[basename] = sf.Size

				// Log enriched metadata for debugging
				if fileRelease.Group == "" && enrichedRelease.Group != "" {
					log.Debug().
						Str("file", sf.Name).
						Str("enrichedGroup", enrichedRelease.Group).
						Msg("Enriched file with group from torrent")
				}
			}

			totalSourceSize += sf.Size
		}
	}

	// Build candidate maps with enrichment
	candidateMap := make(map[string]int64)
	candidateBasenames := make(map[string]int64)
	totalCandidateSize := int64(0)

	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			candidateMap[cf.Name] = cf.Size

			// Parse file and enrich with torrent metadata (cached)
			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			basename := s.extractBasename(cf.Name)
			if basename != "" {
				candidateBasenames[basename] = cf.Size

				// Log enriched metadata for debugging
				if fileRelease.Resolution == "" && enrichedRelease.Resolution != "" {
					log.Debug().
						Str("file", cf.Name).
						Str("enrichedResolution", enrichedRelease.Resolution).
						Msg("Enriched file with resolution from torrent")
				}
			}

			totalCandidateSize += cf.Size
		}
	}

	// Check for exact file match (same paths and sizes)
	exactMatch := true
	for path, size := range sourceMap {
		if candidateSize, exists := candidateMap[path]; !exists || candidateSize != size {
			exactMatch = false
			break
		}
	}

	if exactMatch && len(sourceMap) == len(candidateMap) {
		return "exact"
	}

	// Check for partial match (season pack scenario)
	// Scenario 1: Single episode contained in season pack
	// Scenario 2: Season pack contains multiple single episodes
	if sourceRelease.Series > 0 || candidateRelease.Series > 0 {
		// Check if source files are contained in candidate (source episode in candidate pack)
		sourceInCandidate := s.checkPartialMatch(sourceBasenames, candidateBasenames)
		if sourceInCandidate {
			return "partial-in-pack"
		}

		// Check if candidate files are contained in source (candidate episode in source pack)
		candidateInSource := s.checkPartialMatch(candidateBasenames, sourceBasenames)
		if candidateInSource {
			return "partial-contains"
		}
	}

	// Size match for same content with different structure
	if totalSourceSize > 0 && totalSourceSize == totalCandidateSize && len(sourceMap) > 0 {
		return "size"
	}

	return ""
}

// checkPartialMatch checks if subset files are contained in superset files
// Returns true if all subset files have matching basenames and sizes in superset
func (s *Service) checkPartialMatch(subset, superset map[string]int64) bool {
	if len(subset) == 0 || len(superset) == 0 {
		return false
	}

	matchCount := 0
	for basename, size := range subset {
		if superSize, exists := superset[basename]; exists && superSize == size {
			matchCount++
		}
	}

	// Consider it a match if at least 80% of subset files are found
	threshold := float64(len(subset)) * 0.8
	return float64(matchCount) >= threshold
}

// extractBasename extracts the core filename component for matching using rls parser
// Examples:
//
//	"Show.Name.S01E05.1080p.mkv" -> "S01E05"
//	"dir/Show.S01E05.mkv" -> "S01E05"
func (s *Service) extractBasename(fullPath string) string {
	// Get filename without directory
	parts := strings.Split(fullPath, "/")
	filename := parts[len(parts)-1]

	// Parse filename with rls (cached)
	release := s.releaseCache.Parse(filename)

	// Build season/episode identifier
	if release.Series > 0 {
		if release.Episode > 0 {
			return fmt.Sprintf("S%02dE%02d", release.Series, release.Episode)
		}
		return fmt.Sprintf("S%02d", release.Series)
	}

	return ""
}

// enrichReleaseFromTorrent enriches file release info with metadata from torrent name
// This fills in missing group, resolution, codec, and other metadata from the season pack
func enrichReleaseFromTorrent(fileRelease rls.Release, torrentRelease rls.Release) rls.Release {
	enriched := fileRelease

	// Fill in missing group from torrent
	if enriched.Group == "" && torrentRelease.Group != "" {
		enriched.Group = torrentRelease.Group
	}

	// Fill in missing resolution from torrent
	if enriched.Resolution == "" && torrentRelease.Resolution != "" {
		enriched.Resolution = torrentRelease.Resolution
	}

	// Fill in missing codec from torrent
	if len(enriched.Codec) == 0 && len(torrentRelease.Codec) > 0 {
		enriched.Codec = torrentRelease.Codec
	}

	// Fill in missing audio from torrent
	if len(enriched.Audio) == 0 && len(torrentRelease.Audio) > 0 {
		enriched.Audio = torrentRelease.Audio
	}

	// Fill in missing source from torrent
	if enriched.Source == "" && torrentRelease.Source != "" {
		enriched.Source = torrentRelease.Source
	}

	// Fill in missing HDR info from torrent
	if len(enriched.HDR) == 0 && len(torrentRelease.HDR) > 0 {
		enriched.HDR = torrentRelease.HDR
	}

	// Fill in missing season from torrent (for season packs)
	if enriched.Series == 0 && torrentRelease.Series > 0 {
		enriched.Series = torrentRelease.Series
	}

	// Fill in missing year from torrent
	if enriched.Year == 0 && torrentRelease.Year > 0 {
		enriched.Year = torrentRelease.Year
	}

	return enriched
}

// shouldIgnoreFile checks if a file should be ignored based on patterns
func shouldIgnoreFile(filename string, patterns []string) bool {
	lower := strings.ToLower(filename)

	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		// Simple glob matching
		if strings.Contains(pattern, "*") {
			parts := strings.Split(pattern, "*")
			matches := true
			for _, part := range parts {
				if part != "" && !strings.Contains(lower, part) {
					matches = false
					break
				}
			}
			if matches {
				return true
			}
		} else if strings.HasSuffix(lower, pattern) {
			return true
		}
	}

	return false
}

// CrossSeed attempts to add a new torrent for cross-seeding on the same instance
// TODO: Implement proper cross-seeding with same save path pointing to existing files
func (s *Service) CrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	return nil, fmt.Errorf("CrossSeed not yet implemented - need to add torrent with existing save path")
}
