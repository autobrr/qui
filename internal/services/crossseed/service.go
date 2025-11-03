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
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
)

// releaseKey is a comparable struct for matching releases across different torrents
// It uses the actual parsed data from rls.Release instead of inventing string formats
type releaseKey struct {
	// TV shows: series and episode
	series  int
	episode int

	// Date-based releases: year/month/day
	year  int
	month int
	day   int
}

// makeReleaseKey creates a releaseKey from a parsed release
// Returns the zero value if the release doesn't have identifiable metadata
func makeReleaseKey(r rls.Release) releaseKey {
	// TV episode
	if r.Series > 0 && r.Episode > 0 {
		return releaseKey{
			series:  r.Series,
			episode: r.Episode,
		}
	}

	// TV season (no specific episode)
	if r.Series > 0 {
		return releaseKey{
			series: r.Series,
		}
	}

	// Date-based release
	if r.Year > 0 && r.Month > 0 && r.Day > 0 {
		return releaseKey{
			year:  r.Year,
			month: r.Month,
			day:   r.Day,
		}
	}

	// Year-based release (movies, software, etc.)
	if r.Year > 0 {
		return releaseKey{
			year: r.Year,
		}
	}

	// Content without clear identifying metadata - use zero value
	return releaseKey{}
}

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

// FindCandidates finds ALL existing torrents across instances that match a title string
// Input: Just a torrent NAME (string) - the torrent doesn't exist yet
// Output: All existing torrents that have related content based on release name parsing
func (s *Service) FindCandidates(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	if req.TorrentName == "" {
		return nil, fmt.Errorf("torrent_name is required")
	}

	// Parse the title string to understand what we're looking for
	targetRelease := s.releaseCache.Parse(req.TorrentName)

	// Build basic info for response
	sourceTorrentInfo := &TorrentInfo{
		Name: req.TorrentName,
	}

	// Determine which instances to search
	var searchInstanceIDs []int
	if len(req.TargetInstanceIDs) > 0 {
		searchInstanceIDs = req.TargetInstanceIDs
	} else {
		// Search all instances
		allInstances, err := s.instanceStore.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list instances: %w", err)
		}
		for _, inst := range allInstances {
			searchInstanceIDs = append(searchInstanceIDs, inst.ID)
		}
	}

	response := &FindCandidatesResponse{
		SourceTorrent: sourceTorrentInfo,
		Candidates:    make([]CrossSeedCandidate, 0),
	}

	totalCandidates := 0

	// Search ALL instances for torrents that match the title
	for _, instanceID := range searchInstanceIDs {
		instance, err := s.instanceStore.Get(ctx, instanceID)
		if err != nil {
			log.Warn().
				Int("instanceID", instanceID).
				Err(err).
				Msg("Failed to get instance info, skipping")
			continue
		}

		// Get all torrents from this instance
		torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
		if err != nil {
			log.Warn().
				Int("instanceID", instanceID).
				Str("instanceName", instance.Name).
				Err(err).
				Msg("Failed to get torrents from instance, skipping")
			continue
		}

		var matchedTorrents []qbt.Torrent

		// Check EVERY torrent to see if it has the files we need
		for _, torrent := range torrents {
			// Only complete torrents can provide data
			if torrent.Progress < 1.0 {
				continue
			}

			candidateRelease := s.releaseCache.Parse(torrent.Name)

			// Check if releases are related (quick filter)
			if !s.releasesMatch(targetRelease, candidateRelease) {
				continue
			}

			// Get the candidate torrent's files to check if it has what we need
			candidateFiles, err := s.filesManager.GetCachedFiles(ctx, instanceID, torrent.Hash, torrent.Progress)
			if err != nil || len(candidateFiles) == 0 {
				continue
			}

			// Now check if this torrent actually has the files we need
			// This handles: single episode in season pack, season pack containing episodes, etc.
			matchType := s.getMatchTypeFromTitle(targetRelease, candidateRelease, candidateFiles, req.IgnorePatterns)
			if matchType != "" {
				matchedTorrents = append(matchedTorrents, torrent)
				log.Debug().
					Str("targetTitle", req.TorrentName).
					Str("existingTorrent", torrent.Name).
					Int("instanceID", instanceID).
					Str("instanceName", instance.Name).
					Str("matchType", matchType).
					Msg("Found matching torrent with required files")
			}
		}

		// Add all matches from this instance
		if len(matchedTorrents) > 0 {
			response.Candidates = append(response.Candidates, CrossSeedCandidate{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Torrents:     matchedTorrents,
				MatchType:    "release-match",
			})
			totalCandidates += len(matchedTorrents)
		}
	}

	log.Info().
		Str("targetTitle", req.TorrentName).
		Int("instancesSearched", len(searchInstanceIDs)).
		Int("totalMatches", totalCandidates).
		Msg("Found existing torrents matching title")

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

// getMatchTypeFromTitle checks if a candidate torrent has files matching what we want based on parsed title
func (s *Service) getMatchTypeFromTitle(targetRelease, candidateRelease rls.Release, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build candidate release keys from actual files WITH ENRICHMENT
	candidateReleases := make(map[releaseKey]int64)
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			// Parse file and enrich with torrent metadata
			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			// Extract release key from enriched release
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				candidateReleases[key] = cf.Size
			}
		}
	}

	// Check if candidate has what we need
	if targetRelease.Series > 0 && targetRelease.Episode > 0 {
		// Looking for specific episode - check if it exists in candidate files
		targetKey := releaseKey{
			series:  targetRelease.Series,
			episode: targetRelease.Episode,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else if targetRelease.Series > 0 {
		// Looking for season pack - check if ANY episodes from this season exist in candidate files
		for key := range candidateReleases {
			if key.series == targetRelease.Series && key.episode > 0 {
				return "partial-contains"
			}
		}
	} else if targetRelease.Year > 0 && targetRelease.Month > 0 && targetRelease.Day > 0 {
		// Date-based release - check for exact date match
		targetKey := releaseKey{
			year:  targetRelease.Year,
			month: targetRelease.Month,
			day:   targetRelease.Day,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else {
		// Non-episodic content - check if any candidate files match
		if len(candidateReleases) > 0 {
			return "partial-in-pack"
		}
	}

	return ""
}

// getMatchType determines if files match for cross-seeding
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match
func (s *Service) getMatchType(sourceRelease, candidateRelease rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build map of source files (name -> size) and (releaseKey -> size)
	// Parse each file with rls and enrich with torrent metadata
	sourceMap := make(map[string]int64)
	sourceReleaseKeys := make(map[releaseKey]int64)
	totalSourceSize := int64(0)

	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, ignorePatterns) {
			sourceMap[sf.Name] = sf.Size

			// Parse file and enrich with torrent metadata (cached)
			fileRelease := s.releaseCache.Parse(sf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, sourceRelease)

			// Extract release key using parsed data
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				sourceReleaseKeys[key] = sf.Size

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
	candidateReleaseKeys := make(map[releaseKey]int64)
	totalCandidateSize := int64(0)

	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			candidateMap[cf.Name] = cf.Size

			// Parse file and enrich with torrent metadata (cached)
			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				candidateReleaseKeys[key] = cf.Size

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

	// Check for partial match (season pack scenario, date-based releases, etc.)
	// Scenario 1: Single episode/release contained in pack
	// Scenario 2: Pack contains multiple single episodes/releases
	if len(sourceReleaseKeys) > 0 && len(candidateReleaseKeys) > 0 {
		// Check if source files are contained in candidate (source episode in candidate pack)
		sourceInCandidate := s.checkPartialMatch(sourceReleaseKeys, candidateReleaseKeys)
		if sourceInCandidate {
			return "partial-in-pack"
		}

		// Check if candidate files are contained in source (candidate episode in source pack)
		candidateInSource := s.checkPartialMatch(candidateReleaseKeys, sourceReleaseKeys)
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
// Returns true if all subset files have matching release keys and sizes in superset
func (s *Service) checkPartialMatch(subset, superset map[releaseKey]int64) bool {
	if len(subset) == 0 || len(superset) == 0 {
		return false
	}

	matchCount := 0
	for key, size := range subset {
		if superSize, exists := superset[key]; exists && superSize == size {
			matchCount++
		}
	}

	// Consider it a match if at least 80% of subset files are found
	threshold := float64(len(subset)) * 0.8
	return float64(matchCount) >= threshold
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

// CrossSeed attempts to add a new torrent for cross-seeding
// It finds existing 100% complete torrents that match the content and adds the new torrent
// paused to the same location with matching category and ATM state
func (s *Service) CrossSeed(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
	if req.TorrentData == "" {
		return nil, fmt.Errorf("torrent_data is required")
	}

	// Decode base64 torrent data
	torrentBytes, err := s.decodeTorrentData(req.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent data: %w", err)
	}

	// Parse torrent to get name for finding candidates
	torrentName, torrentHash, err := s.parseTorrentName(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse torrent: %w", err)
	}

	// Use FindCandidates to locate matching torrents
	findReq := &FindCandidatesRequest{
		TorrentName:       torrentName,
		TargetInstanceIDs: req.TargetInstanceIDs,
	}

	candidatesResp, err := s.FindCandidates(ctx, findReq)
	if err != nil {
		return nil, fmt.Errorf("failed to find candidates: %w", err)
	}

	response := &CrossSeedResponse{
		Success: false,
		Results: make([]InstanceCrossSeedResult, 0),
		TorrentInfo: &TorrentInfo{
			Name: torrentName,
			Hash: torrentHash,
		},
	}

	// Process each instance with matching candidates
	for _, candidate := range candidatesResp.Candidates {
		result := s.processCrossSeedCandidate(ctx, candidate, torrentBytes, torrentHash, torrentName, req)
		response.Results = append(response.Results, result)
		if result.Success {
			response.Success = true
		}
	}

	// If no candidates found, return appropriate response
	if len(candidatesResp.Candidates) == 0 {
		// Try all target instances or all instances if not specified
		targetInstanceIDs := req.TargetInstanceIDs
		if len(targetInstanceIDs) == 0 {
			allInstances, err := s.instanceStore.List(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list instances: %w", err)
			}
			for _, inst := range allInstances {
				targetInstanceIDs = append(targetInstanceIDs, inst.ID)
			}
		}

		for _, instanceID := range targetInstanceIDs {
			instance, err := s.instanceStore.Get(ctx, instanceID)
			if err != nil {
				log.Warn().
					Int("instanceID", instanceID).
					Err(err).
					Msg("Failed to get instance info")
				continue
			}

			response.Results = append(response.Results, InstanceCrossSeedResult{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Success:      false,
				Status:       "no_match",
				Message:      "No matching torrents found with required files",
			})
		}
	}

	return response, nil
}

// processCrossSeedCandidate processes a single candidate for cross-seeding
func (s *Service) processCrossSeedCandidate(ctx context.Context, candidate CrossSeedCandidate, torrentBytes []byte, torrentHash, torrentName string, req *CrossSeedRequest) InstanceCrossSeedResult {
	result := InstanceCrossSeedResult{
		InstanceID:   candidate.InstanceID,
		InstanceName: candidate.InstanceName,
		Success:      false,
		Status:       "error",
	}

	// Check if torrent already exists
	existingTorrents, err := s.syncManager.GetAllTorrents(ctx, candidate.InstanceID)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to check existing torrents: %v", err)
		return result
	}

	// Check by hash (v1 or v2)
	for _, existing := range existingTorrents {
		if existing.Hash == torrentHash || existing.InfohashV1 == torrentHash || existing.InfohashV2 == torrentHash {
			result.Success = false
			result.Status = "exists"
			result.Message = "Torrent already exists in this instance"
			result.MatchedTorrent = &MatchedTorrent{
				Hash:     existing.Hash,
				Name:     existing.Name,
				Progress: existing.Progress,
				Size:     existing.Size,
			}
			return result
		}
	}

	// Skip if requested
	if req.SkipIfExists {
		result.Status = "skipped"
		result.Message = "Torrent already exists (skipped due to SkipIfExists)"
		return result
	}

	// Find the best matching torrent (100% complete)
	var matchedTorrent *qbt.Torrent
	for _, t := range candidate.Torrents {
		if t.Progress >= 1.0 {
			matchedTorrent = &t
			break
		}
	}

	if matchedTorrent == nil {
		result.Status = "no_match"
		result.Message = "No 100% complete matching torrent found"
		return result
	}

	// Get torrent properties to extract save path
	props, err := s.syncManager.GetTorrentProperties(ctx, candidate.InstanceID, matchedTorrent.Hash)
	if err != nil {
		result.Message = fmt.Sprintf("Failed to get torrent properties: %v", err)
		return result
	}

	// Determine the appropriate save path for cross-seeding
	savePath := s.determineSavePath(torrentName, matchedTorrent, props)

	// Build options for adding the torrent
	options := make(map[string]string)

	// Add paused
	options["paused"] = "true"
	options["stopped"] = "true"

	// Skip hash checking since we're pointing to existing files
	options["skip_checking"] = "true"

	// Use category from request or matched torrent
	category := req.Category
	if category == "" {
		category = matchedTorrent.Category
	}
	if category != "" {
		options["category"] = category
	}

	// Handle AutoTMM and save path (use AutoManaged field from Torrent struct)
	if matchedTorrent.AutoManaged {
		options["autoTMM"] = "true"
	} else {
		options["autoTMM"] = "false"
		// Use the determined save path (handles season packs, custom paths, etc.)
		if savePath != "" {
			options["savepath"] = savePath
		}
	}

	// Add tags
	tags := req.Tags
	if len(tags) == 0 && len(matchedTorrent.Tags) > 0 {
		tags = strings.Split(matchedTorrent.Tags, ", ")
	}
	if len(tags) > 0 {
		// Add a cross-seed tag to identify it
		tags = append(tags, "cross-seed")
		options["tags"] = strings.Join(tags, ",")
	} else {
		options["tags"] = "cross-seed"
	}

	// Add the torrent
	err = s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
	if err != nil {
		// If adding fails, try with recheck enabled (skip_checking=false)
		log.Warn().
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Msg("Failed to add cross-seed torrent, retrying with recheck enabled")

		// Remove skip_checking and add with recheck
		delete(options, "skip_checking")
		err = s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
		if err != nil {
			result.Message = fmt.Sprintf("Failed to add torrent even with recheck: %v", err)
			log.Error().
				Err(err).
				Int("instanceID", candidate.InstanceID).
				Str("torrentHash", torrentHash).
				Msg("Failed to add cross-seed torrent after retry")
			return result
		}

		result.Message = fmt.Sprintf("Added torrent with recheck to %s", props.SavePath)
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Msg("Successfully added cross-seed torrent with recheck")
	} else {
		result.Message = fmt.Sprintf("Added torrent paused to %s", props.SavePath)
	}

	// Wait for the torrent to be added and potentially rechecked
	newTorrent := s.waitForTorrentRecheck(ctx, candidate.InstanceID, torrentHash, &result)
	if newTorrent != nil {
		// If torrent is 100% complete (or very close), auto-resume it
		if newTorrent.Progress >= 0.999 {
			resumeErr := s.syncManager.BulkAction(ctx, candidate.InstanceID, []string{newTorrent.Hash}, "resume")
			if resumeErr != nil {
				log.Warn().
					Err(resumeErr).
					Int("instanceID", candidate.InstanceID).
					Str("hash", newTorrent.Hash).
					Msg("Failed to auto-resume 100% complete cross-seed torrent")
				result.Message += " (100% complete but failed to auto-resume)"
			} else {
				result.Message += " (100% complete, auto-resumed)"
				log.Info().
					Int("instanceID", candidate.InstanceID).
					Str("hash", newTorrent.Hash).
					Float64("progress", newTorrent.Progress).
					Msg("Auto-resumed 100% complete cross-seed torrent")
			}
		} else {
			// Torrent is not 100%, may be missing files (like .srt, sample, etc.)
			// This is expected and controlled by user's file layout
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("hash", newTorrent.Hash).
				Float64("progress", newTorrent.Progress).
				Msg("Cross-seed torrent added but not 100% complete (may be missing optional files)")
			result.Message += fmt.Sprintf(" (%.1f%% complete, check manually)", newTorrent.Progress*100)
		}
	}

	// Success!
	result.Success = true
	result.Status = "added"
	result.MatchedTorrent = &MatchedTorrent{
		Hash:     matchedTorrent.Hash,
		Name:     matchedTorrent.Name,
		Progress: matchedTorrent.Progress,
		Size:     matchedTorrent.Size,
	}

	log.Info().
		Int("instanceID", candidate.InstanceID).
		Str("instanceName", candidate.InstanceName).
		Str("torrentHash", torrentHash).
		Str("matchedHash", matchedTorrent.Hash).
		Str("savePath", props.SavePath).
		Bool("autoTMM", matchedTorrent.AutoManaged).
		Str("category", category).
		Msg("Successfully added cross-seed torrent")

	return result
}

// decodeTorrentData decodes base64-encoded torrent data
func (s *Service) decodeTorrentData(data string) ([]byte, error) {
	// Try standard base64
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(data)
		if err != nil {
			// Try raw base64 (no padding)
			decoded, err = base64.RawStdEncoding.DecodeString(data)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64: %w", err)
			}
		}
	}
	return decoded, nil
}

// parseTorrentName extracts the name and info hash from torrent bytes
func (s *Service) parseTorrentName(torrentBytes []byte) (name string, hash string, err error) {
	// Simple bencode parsing to extract name and calculate info hash
	// For now, we'll use a basic approach - in production you might want a proper bencode parser

	// This is a simplified parser - you may want to use a proper bencode library
	// For the name, we'll look for the "name" field in the torrent
	namePattern := []byte("4:name")
	nameIdx := bytes.Index(torrentBytes, namePattern)
	if nameIdx != -1 {
		// Skip past "4:name" and parse the length
		idx := nameIdx + len(namePattern)
		lengthEnd := bytes.IndexByte(torrentBytes[idx:], ':')
		if lengthEnd != -1 {
			lengthStr := string(torrentBytes[idx : idx+lengthEnd])
			nameLen := 0
			fmt.Sscanf(lengthStr, "%d", &nameLen)
			if nameLen > 0 && nameLen < 1024 {
				nameStart := idx + lengthEnd + 1
				if nameStart+nameLen <= len(torrentBytes) {
					name = string(torrentBytes[nameStart : nameStart+nameLen])
				}
			}
		}
	}

	// Calculate info hash (SHA1 of the info dictionary)
	// For simplicity, we'll leave hash empty for now - it will be populated when torrent is added
	// A proper implementation would parse the bencode and calculate SHA1 of info dict
	hash = ""

	if name == "" {
		return "", "", fmt.Errorf("could not extract torrent name")
	}

	return name, hash, nil
}

// determineSavePath determines the appropriate save path for cross-seeding
// This handles various scenarios:
// - Season pack being added when individual episodes exist
// - Individual episode being added when a season pack exists
// - Custom paths and directory structures
func (s *Service) determineSavePath(newTorrentName string, matchedTorrent *qbt.Torrent, props *qbt.TorrentProperties) string {
	// Default to the matched torrent's save path
	baseSavePath := props.SavePath

	// Parse both torrent names to understand what we're dealing with
	newTorrentRelease := s.releaseCache.Parse(newTorrentName)
	matchedRelease := s.releaseCache.Parse(matchedTorrent.Name)

	// Scenario 1: New torrent is a season pack, matched torrent is a single episode
	// In this case, we want to use the parent directory of the episode
	if newTorrentRelease.Series > 0 && newTorrentRelease.Episode == 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode > 0 {
		// New is season pack (has series but no episode)
		// Matched is single episode (has both series and episode)
		// Use parent directory of the matched torrent's content path
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("baseSavePath", baseSavePath).
			Msg("Cross-seeding season pack from individual episode, using parent directory")

		// If the matched torrent is in a subdirectory, use the parent
		// This handles: /downloads/Show.S01E01/ -> /downloads/
		return baseSavePath
	}

	// Scenario 2: New torrent is a single episode, matched torrent is a season pack
	// Use the matched torrent's save path directly - the files are already there
	if newTorrentRelease.Series > 0 && newTorrentRelease.Episode > 0 &&
		matchedRelease.Series > 0 && matchedRelease.Episode == 0 {
		log.Debug().
			Str("newTorrent", newTorrentName).
			Str("matchedTorrent", matchedTorrent.Name).
			Str("savePath", baseSavePath).
			Msg("Cross-seeding individual episode from season pack")

		// The season pack already has the episode files, use its path directly
		return baseSavePath
	}

	// Scenario 3: Both are the same type (both season packs or both single episodes)
	// Or non-episodic content (movies, etc.)
	// Use the matched torrent's save path as-is
	log.Debug().
		Str("newTorrent", newTorrentName).
		Str("matchedTorrent", matchedTorrent.Name).
		Str("savePath", baseSavePath).
		Msg("Cross-seeding same content type, using matched torrent's path")

	return baseSavePath
}

// waitForTorrentRecheck waits for a torrent to finish rechecking after being added
// This can take several minutes for large torrents, so we poll periodically
func (s *Service) waitForTorrentRecheck(ctx context.Context, instanceID int, torrentHash string, result *InstanceCrossSeedResult) *qbt.Torrent {
	const (
		maxWaitTime  = 5 * time.Minute // Maximum time to wait for recheck
		pollInterval = 2 * time.Second // How often to check status
		initialWait  = 500 * time.Millisecond
	)

	// Initial wait for torrent to appear
	time.Sleep(initialWait)

	startTime := time.Now()
	lastLogTime := startTime

	for {
		// Check if we've exceeded max wait time
		if time.Since(startTime) > maxWaitTime {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Dur("waited", time.Since(startTime)).
				Msg("Timeout waiting for torrent recheck to complete")
			result.Message += " (timeout waiting for recheck)"
			return nil
		}

		// Check context cancellation
		if ctx.Err() != nil {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Err(ctx.Err()).
				Msg("Context cancelled while waiting for recheck")
			return nil
		}

		// Force sync to get latest state from qBittorrent
		qbtSyncManager, err := s.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to get sync manager while waiting for recheck")
			time.Sleep(pollInterval)
			continue
		}

		if syncErr := qbtSyncManager.Sync(ctx); syncErr != nil {
			log.Warn().
				Err(syncErr).
				Int("instanceID", instanceID).
				Msg("Failed to sync while waiting for recheck, will retry")
			time.Sleep(pollInterval)
			continue
		}

		// Get current torrents
		torrents, err := s.syncManager.GetAllTorrents(ctx, instanceID)
		if err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Failed to get torrents while waiting for recheck")
			time.Sleep(pollInterval)
			continue
		}

		// Find the torrent
		var torrent *qbt.Torrent
		for _, t := range torrents {
			if t.Hash == torrentHash || t.InfohashV1 == torrentHash || t.InfohashV2 == torrentHash {
				torrent = &t
				break
			}
		}

		if torrent == nil {
			log.Warn().
				Int("instanceID", instanceID).
				Str("torrentHash", torrentHash).
				Msg("Torrent not found after adding")
			return nil
		}

		// Check if torrent is still checking
		isChecking := torrent.State == qbt.TorrentStateCheckingDl ||
			torrent.State == qbt.TorrentStateCheckingUp ||
			torrent.State == qbt.TorrentStateCheckingResumeData ||
			torrent.State == qbt.TorrentStateAllocating

		if isChecking {
			// Log periodically (every 10 seconds) to show progress
			if time.Since(lastLogTime) > 10*time.Second {
				log.Info().
					Int("instanceID", instanceID).
					Str("hash", torrent.Hash).
					Str("state", string(torrent.State)).
					Float64("progress", torrent.Progress).
					Dur("elapsed", time.Since(startTime)).
					Msg("Waiting for torrent recheck to complete")
				lastLogTime = time.Now()
			}

			time.Sleep(pollInterval)
			continue
		}

		// Torrent is done checking
		log.Info().
			Int("instanceID", instanceID).
			Str("hash", torrent.Hash).
			Str("state", string(torrent.State)).
			Float64("progress", torrent.Progress).
			Dur("elapsed", time.Since(startTime)).
			Msg("Torrent recheck completed")

		return torrent
	}
}
