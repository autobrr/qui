// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"
)

// matching.go groups all heuristics and helpers that decide whether two torrents
// describe the same underlying content.

// releaseKey is a comparable struct for matching releases across different torrents.
// It uses parsed metadata from rls.Release to avoid brittle filename string compares.
type releaseKey struct {
	// TV shows: series and episode.
	series  int
	episode int

	// Date-based releases: year/month/day.
	year  int
	month int
	day   int
}

// makeReleaseKey creates a releaseKey from a parsed release.
// Returns the zero value if the release doesn't have identifiable metadata.
func makeReleaseKey(r rls.Release) releaseKey {
	// TV episode.
	if r.Series > 0 && r.Episode > 0 {
		return releaseKey{
			series:  r.Series,
			episode: r.Episode,
		}
	}

	// TV season (no specific episode).
	if r.Series > 0 {
		return releaseKey{
			series: r.Series,
		}
	}

	// Date-based release.
	if r.Year > 0 && r.Month > 0 && r.Day > 0 {
		return releaseKey{
			year:  r.Year,
			month: r.Month,
			day:   r.Day,
		}
	}

	// Year-based release (movies, software, etc.).
	if r.Year > 0 {
		return releaseKey{
			year: r.Year,
		}
	}

	// Content without clear identifying metadata - use zero value.
	return releaseKey{}
}

// releasesMatch checks if two releases are related using fuzzy matching.
// This allows matching similar content that isn't exactly the same.
func (s *Service) releasesMatch(source, candidate rls.Release) bool {
	// Title should match closely but not necessarily exactly.
	sourceTitleLower := strings.ToLower(strings.TrimSpace(source.Title))
	candidateTitleLower := strings.ToLower(strings.TrimSpace(candidate.Title))

	if sourceTitleLower == "" || candidateTitleLower == "" {
		return false
	}

	// Check if titles are similar (exact match or one contains the other).
	if sourceTitleLower != candidateTitleLower &&
		!strings.Contains(sourceTitleLower, candidateTitleLower) &&
		!strings.Contains(candidateTitleLower, sourceTitleLower) {
		return false
	}

	// Year should match if both are present.
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false
	}

	// For TV shows, season must match but episodes can differ.
	if source.Series > 0 || candidate.Series > 0 {
		// If one has a season but the other doesn't, skip season check.
		if source.Series > 0 && candidate.Series > 0 && source.Series != candidate.Series {
			return false
		}
		// Don't enforce episode matching here - handled later in file matching.
	}

	// Group tags should match for proper cross-seeding compatibility.
	// Different release groups often have different encoding settings and file structures.
	sourceGroup := strings.ToUpper(strings.TrimSpace(source.Group))
	candidateGroup := strings.ToUpper(strings.TrimSpace(candidate.Group))

	// Only enforce group matching if the source has a group tag
	if sourceGroup != "" {
		// If source has a group, candidate must have the same group
		if candidateGroup == "" || sourceGroup != candidateGroup {
			return false
		}
	}
	// If source has no group, we don't care about candidate's group

	// Resolution matching is optional - different qualities can cross-seed if files match.
	return true
}

// getMatchTypeFromTitle checks if a candidate torrent has files matching what we want based on parsed title.
func (s *Service) getMatchTypeFromTitle(targetRelease, candidateRelease rls.Release, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build candidate release keys from actual files with enrichment.
	candidateReleases := make(map[releaseKey]int64)
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				candidateReleases[key] = cf.Size
			}
		}
	}

	// Check if candidate has what we need.
	if targetRelease.Series > 0 && targetRelease.Episode > 0 {
		// Looking for specific episode.
		targetKey := releaseKey{
			series:  targetRelease.Series,
			episode: targetRelease.Episode,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else if targetRelease.Series > 0 {
		// Looking for season pack - check if any episodes from this season exist in candidate files.
		for key := range candidateReleases {
			if key.series == targetRelease.Series && key.episode > 0 {
				return "partial-contains"
			}
		}
	} else if targetRelease.Year > 0 && targetRelease.Month > 0 && targetRelease.Day > 0 {
		// Date-based release - check for exact date match.
		targetKey := releaseKey{
			year:  targetRelease.Year,
			month: targetRelease.Month,
			day:   targetRelease.Day,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else {
		// Non-episodic content - check if any candidate files match.
		if len(candidateReleases) > 0 {
			return "partial-in-pack"
		}
	}

	return ""
}

// getMatchType determines if files match for cross-seeding.
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match.
func (s *Service) getMatchType(sourceRelease, candidateRelease rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build map of source files (name -> size) and (releaseKey -> size).
	sourceMap := make(map[string]int64)
	sourceReleaseKeys := make(map[releaseKey]int64)
	totalSourceSize := int64(0)

	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, ignorePatterns) {
			sourceMap[sf.Name] = sf.Size

			fileRelease := s.releaseCache.Parse(sf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, sourceRelease)

			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				sourceReleaseKeys[key] = sf.Size

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

	// Build candidate maps with enrichment.
	candidateMap := make(map[string]int64)
	candidateReleaseKeys := make(map[releaseKey]int64)
	totalCandidateSize := int64(0)

	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns) {
			candidateMap[cf.Name] = cf.Size

			fileRelease := s.releaseCache.Parse(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)

			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				candidateReleaseKeys[key] = cf.Size

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

	// Check for exact file match (same paths and sizes).
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

	// Check for partial match (season pack scenario, date-based releases, etc.).
	if len(sourceReleaseKeys) > 0 && len(candidateReleaseKeys) > 0 {
		// Check if source files are contained in candidate (source episode in candidate pack).
		if s.checkPartialMatch(sourceReleaseKeys, candidateReleaseKeys) {
			return "partial-in-pack"
		}

		// Check if candidate files are contained in source (candidate episode in source pack).
		if s.checkPartialMatch(candidateReleaseKeys, sourceReleaseKeys) {
			return "partial-contains"
		}
	}

	// Size match for same content with different structure.
	if totalSourceSize > 0 && totalSourceSize == totalCandidateSize && len(sourceMap) > 0 {
		return "size"
	}

	return ""
}

// checkPartialMatch checks if subset files are contained in superset files.
// Returns true if all subset files have matching release keys and sizes in superset.
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

	// Consider it a match if at least 80% of subset files are found.
	threshold := float64(len(subset)) * 0.8
	return float64(matchCount) >= threshold
}

// enrichReleaseFromTorrent enriches file release info with metadata from torrent name.
// This fills in missing group, resolution, codec, and other metadata from the season pack.
func enrichReleaseFromTorrent(fileRelease rls.Release, torrentRelease rls.Release) rls.Release {
	enriched := fileRelease

	// Fill in missing group from torrent.
	if enriched.Group == "" && torrentRelease.Group != "" {
		enriched.Group = torrentRelease.Group
	}

	// Fill in missing resolution from torrent.
	if enriched.Resolution == "" && torrentRelease.Resolution != "" {
		enriched.Resolution = torrentRelease.Resolution
	}

	// Fill in missing codec from torrent.
	if len(enriched.Codec) == 0 && len(torrentRelease.Codec) > 0 {
		enriched.Codec = torrentRelease.Codec
	}

	// Fill in missing audio from torrent.
	if len(enriched.Audio) == 0 && len(torrentRelease.Audio) > 0 {
		enriched.Audio = torrentRelease.Audio
	}

	// Fill in missing source from torrent.
	if enriched.Source == "" && torrentRelease.Source != "" {
		enriched.Source = torrentRelease.Source
	}

	// Fill in missing HDR info from torrent.
	if len(enriched.HDR) == 0 && len(torrentRelease.HDR) > 0 {
		enriched.HDR = torrentRelease.HDR
	}

	// Fill in missing season from torrent (for season packs).
	if enriched.Series == 0 && torrentRelease.Series > 0 {
		enriched.Series = torrentRelease.Series
	}

	// Fill in missing year from torrent.
	if enriched.Year == 0 && torrentRelease.Year > 0 {
		enriched.Year = torrentRelease.Year
	}

	return enriched
}

// shouldIgnoreFile checks if a file should be ignored based on patterns.
func shouldIgnoreFile(filename string, patterns []string) bool {
	lower := strings.ToLower(filename)

	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		// Simple glob matching.
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
