// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"path/filepath"
	"sort"
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

// String serializes the releaseKey into a stable string for caching purposes.
func (k releaseKey) String() string {
	return fmt.Sprintf("%d|%d|%d|%d|%d", k.series, k.episode, k.year, k.month, k.day)
}

// releasesMatch checks if two releases are related using fuzzy matching.
// This allows matching similar content that isn't exactly the same.
func (s *Service) releasesMatch(source, candidate rls.Release, findIndividualEpisodes bool) bool {
	// Title should match closely but not necessarily exactly.
	sourceTitleLower := strings.ToLower(strings.TrimSpace(source.Title))
	candidateTitleLower := strings.ToLower(strings.TrimSpace(candidate.Title))

	if sourceTitleLower == "" || candidateTitleLower == "" {
		return false
	}

	isTV := source.Series > 0 || candidate.Series > 0

	if isTV {
		// For TV, allow a bit of fuzziness in the title (e.g. different punctuation)
		// while still requiring the titles to be closely related.
		if sourceTitleLower != candidateTitleLower &&
			!strings.Contains(sourceTitleLower, candidateTitleLower) &&
			!strings.Contains(candidateTitleLower, sourceTitleLower) {
			return false
		}
	} else {
		// For non-TV content (movies, music, audiobooks, etc.), require exact title
		// match after normalization. This avoids very loose substring matches across
		// unrelated content types.
		if sourceTitleLower != candidateTitleLower {
			return false
		}
	}

	// Year should match if both are present.
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false
	}

	// For non-TV content where rls has inferred a concrete content type (movie, music,
	// audiobook, etc.), require the types to match. This prevents, for example,
	// music releases from matching audiobooks with similar titles.
	if !isTV && source.Type != 0 && candidate.Type != 0 && source.Type != candidate.Type {
		return false
	}

	// For TV shows, season and episode structure must match based on settings.
	if source.Series > 0 || candidate.Series > 0 {
		// Both must have series info if either does
		if source.Series > 0 && candidate.Series == 0 {
			return false
		}
		if candidate.Series > 0 && source.Series == 0 {
			return false
		}

		// Series numbers must match
		if source.Series > 0 && candidate.Series > 0 && source.Series != candidate.Series {
			return false
		}

		// Episode structure matching depends on user setting
		sourceIsPack := source.Series > 0 && source.Episode == 0
		candidateIsPack := candidate.Series > 0 && candidate.Episode == 0

		if !findIndividualEpisodes {
			// Strict matching: season packs only match season packs, episodes only match episodes
			if sourceIsPack != candidateIsPack {
				return false // Don't match season packs with individual episodes
			}

			// If both are individual episodes, episodes must match
			if !sourceIsPack && !candidateIsPack && source.Episode != candidate.Episode {
				return false
			}
		} else {
			// Flexible matching: allow season packs to match individual episodes
			// But individual episodes still need exact episode matching
			if !sourceIsPack && !candidateIsPack && source.Episode != candidate.Episode {
				return false
			}
		}
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

	// Source must match if both are present (WEB-DL vs BluRay produce different files)
	sourceSource := strings.ToUpper(strings.TrimSpace(source.Source))
	candidateSource := strings.ToUpper(strings.TrimSpace(candidate.Source))
	if sourceSource != "" && candidateSource != "" && sourceSource != candidateSource {
		return false
	}

	// Resolution must match if both are present (1080p vs 2160p are different files)
	sourceRes := strings.ToUpper(strings.TrimSpace(source.Resolution))
	candidateRes := strings.ToUpper(strings.TrimSpace(candidate.Resolution))
	if sourceRes != "" && candidateRes != "" && sourceRes != candidateRes {
		return false
	}

	// Collection must match if both are present (NF vs AMZN vs Criterion are different sources)
	sourceCollection := strings.ToUpper(strings.TrimSpace(source.Collection))
	candidateCollection := strings.ToUpper(strings.TrimSpace(candidate.Collection))
	if sourceCollection != "" && candidateCollection != "" && sourceCollection != candidateCollection {
		return false
	}

	// Codec must match if both are present (H.264 vs HEVC produce different files)
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 {
		sourceCodec := joinNormalizedSlice(source.Codec)
		candidateCodec := joinNormalizedSlice(candidate.Codec)
		if sourceCodec != candidateCodec {
			return false
		}
	}

	// HDR must match if both are present (HDR vs SDR are different encodes)
	if len(source.HDR) > 0 && len(candidate.HDR) > 0 {
		sourceHDR := joinNormalizedSlice(source.HDR)
		candidateHDR := joinNormalizedSlice(candidate.HDR)
		if sourceHDR != candidateHDR {
			return false
		}
	}

	// Audio must match if both are present (different audio codecs mean different files)
	if len(source.Audio) > 0 && len(candidate.Audio) > 0 {
		sourceAudio := joinNormalizedSlice(source.Audio)
		candidateAudio := joinNormalizedSlice(candidate.Audio)
		if sourceAudio != candidateAudio {
			return false
		}
	}

	// Channels must match if both are present (5.1 vs 7.1 are different audio tracks)
	sourceChannels := strings.ToUpper(strings.TrimSpace(source.Channels))
	candidateChannels := strings.ToUpper(strings.TrimSpace(candidate.Channels))
	if sourceChannels != "" && candidateChannels != "" && sourceChannels != candidateChannels {
		return false
	}

	// Cut must match if both are present (Theatrical vs Extended are different versions)
	if len(source.Cut) > 0 && len(candidate.Cut) > 0 {
		sourceCut := joinNormalizedSlice(source.Cut)
		candidateCut := joinNormalizedSlice(candidate.Cut)
		if sourceCut != candidateCut {
			return false
		}
	}

	// Edition must match if both are present (Remastered vs Original are different)
	if len(source.Edition) > 0 && len(candidate.Edition) > 0 {
		sourceEdition := joinNormalizedSlice(source.Edition)
		candidateEdition := joinNormalizedSlice(candidate.Edition)
		if sourceEdition != candidateEdition {
			return false
		}
	}

	// Certain variant tags (IMAX, HYBRID, etc.) must match even if RLS places
	// them in different fields. This ensures we only cross-seed truly identical
	// video masters.
	if !strictVariantOverrides.variantsCompatible(source, candidate) ||
		!strictVariantOverrides.variantsCompatible(candidate, source) {
		return false
	}

	return true
}

// joinNormalizedSlice converts a string slice to a normalized uppercase string for comparison.
// Uppercases and joins elements to ensure consistent comparison regardless of case or order.
func joinNormalizedSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	normalized := make([]string, len(slice))
	for i, s := range slice {
		normalized[i] = strings.ToUpper(strings.TrimSpace(s))
	}
	sort.Strings(normalized)
	return strings.Join(normalized, " ")
}

// getMatchTypeFromTitle checks if a candidate torrent has files matching what we want based on parsed title.
func (s *Service) getMatchTypeFromTitle(targetName, candidateName string, targetRelease, candidateRelease rls.Release, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
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
		// Non-episodic content - require at least one candidate file whose release
		// key matches the target's release key. This prevents unrelated torrents
		// with generic filenames from matching purely because rls could parse
		// something from their names.
		if len(candidateReleases) > 0 {
			targetKey := makeReleaseKey(targetRelease)
			if targetKey == (releaseKey{}) {
				// No usable metadata from the target; be conservative and avoid
				// treating non-episodic candidates as matches in this pre-filter.
				return ""
			}

			if _, exists := candidateReleases[targetKey]; exists {
				return "partial-in-pack"
			}
		}
	}

	// Fallback: rls couldn't derive usable release keys from the files, but the titles match and
	// the episode number encoded in the raw torrent names also matches (e.g. anime releases where
	// rls fails to parse " - 1150 " as an episode).
	if len(candidateReleases) == 0 {
		targetTitle := strings.ToLower(strings.TrimSpace(targetRelease.Title))
		candidateTitle := strings.ToLower(strings.TrimSpace(candidateRelease.Title))
		if targetTitle != "" && targetTitle == candidateTitle {
			// Extract simple episode number from torrent names of the form "... - 1150 (...)".
			extractEpisode := func(name string) string {
				nameLower := strings.ToLower(name)
				// Look for " - <digits> " pattern.
				for i := 0; i+4 < len(nameLower); i++ {
					if nameLower[i] == ' ' && nameLower[i+1] == '-' && nameLower[i+2] == ' ' {
						j := i + 3
						start := j
						for j < len(nameLower) && nameLower[j] >= '0' && nameLower[j] <= '9' {
							j++
						}
						if j > start && j < len(nameLower) && nameLower[j] == ' ' {
							return nameLower[start:j]
						}
						break
					}
				}
				return ""
			}

			targetEp := extractEpisode(targetName)
			candidateEp := extractEpisode(candidateName)

			if targetEp == "" || candidateEp == "" || targetEp != candidateEp {
				return ""
			}

			log.Debug().
				Str("title", targetRelease.Title).
				Str("episode", targetEp).
				Msg("Falling back to title+episode candidate match")
			return "partial-in-pack"
		}
	}

	return ""
}

// getMatchType determines if files match for cross-seeding.
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match.
func (s *Service) getMatchType(sourceRelease, candidateRelease rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	sourceLayout := classifyTorrentLayout(sourceFiles, ignorePatterns)
	candidateLayout := classifyTorrentLayout(candidateFiles, ignorePatterns)
	if sourceLayout != LayoutUnknown && candidateLayout != LayoutUnknown && sourceLayout != candidateLayout {
		return ""
	}

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

	// If rls couldn't derive usable release keys but both torrents have at least one non-ignored
	// file, fall back to comparing the largest file by base name and size. This is designed for
	// single-episode torrents (common in anime) where the main .mkv matches but sidecars differ.
	if len(sourceReleaseKeys) == 0 && len(candidateReleaseKeys) == 0 &&
		len(sourceMap) > 0 && len(candidateMap) > 0 {
		var (
			srcPath  string
			srcSize  int64
			candPath string
			candSize int64
		)

		for path, size := range sourceMap {
			if size > srcSize {
				srcSize = size
				srcPath = path
			}
		}
		for path, size := range candidateMap {
			if size > candSize {
				candSize = size
				candPath = path
			}
		}

		if srcSize > 0 && srcSize == candSize {
			srcBase := strings.ToLower(strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath)))
			candBase := strings.ToLower(strings.TrimSuffix(filepath.Base(candPath), filepath.Ext(candPath)))
			if srcBase != "" && srcBase == candBase {
				log.Debug().
					Str("sourceFile", srcPath).
					Str("candidateFile", candPath).
					Int64("fileSize", srcSize).
					Msg("Falling back to filename+size match for cross-seed")
				return "size"
			}
		}
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
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}

		// Backwards compatibility: treat plain strings as suffix matches (".nfo", "sample", etc.).
		if !strings.ContainsAny(pattern, "*?[") {
			if strings.HasSuffix(lower, pattern) {
				return true
			}
			continue
		}

		matches, err := filepath.Match(pattern, lower)
		if err != nil {
			log.Debug().Err(err).Str("pattern", pattern).Msg("Invalid ignore pattern skipped")
			continue
		}
		if matches {
			return true
		}
	}

	return false
}
