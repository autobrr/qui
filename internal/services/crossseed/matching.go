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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/stringutils"
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
func makeReleaseKey(r *rls.Release) releaseKey {
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

// parseReleaseName safely parses release metadata when the release cache is available.
func (s *Service) parseReleaseName(name string) *rls.Release {
	if s == nil || s.releaseCache == nil {
		return &rls.Release{}
	}
	return s.releaseCache.Parse(name)
}

// String serializes the releaseKey into a stable string for caching purposes.
func (k releaseKey) String() string {
	return fmt.Sprintf("%d|%d|%d|%d|%d", k.series, k.episode, k.year, k.month, k.day)
}

// releasesMatch checks if two releases are related using fuzzy matching.
// This allows matching similar content that isn't exactly the same.
func (s *Service) releasesMatch(source, candidate *rls.Release, findIndividualEpisodes bool) bool {
	if source == candidate {
		return true
	}

	// Title should match closely but not necessarily exactly.
	// Use punctuation-stripping normalization to handle differences like
	// "Bob's Burgers" vs "Bobs.Burgers" (apostrophes lost in dot notation).
	sourceTitleNorm := stringutils.NormalizeForMatching(source.Title)
	candidateTitleNorm := stringutils.NormalizeForMatching(candidate.Title)

	if sourceTitleNorm == "" || candidateTitleNorm == "" {
		return false
	}

	isTV := source.Series > 0 || candidate.Series > 0

	if isTV {
		// For TV, allow a bit of fuzziness in the title (e.g. different punctuation)
		// while still requiring the titles to be closely related.
		if sourceTitleNorm != candidateTitleNorm &&
			!strings.Contains(sourceTitleNorm, candidateTitleNorm) &&
			!strings.Contains(candidateTitleNorm, sourceTitleNorm) {
			// Title mismatches are expected for most candidates - don't log to avoid noise
			return false
		}
	} else {
		// For non-TV content (movies, music, audiobooks, etc.), require exact title
		// match after normalization. This avoids very loose substring matches across
		// unrelated content types.
		if sourceTitleNorm != candidateTitleNorm {
			// Title mismatches are expected for most candidates - don't log to avoid noise
			return false
		}
	}

	// Artist must match for content with artist metadata (music, 0day scene radio shows, etc.)
	// This prevents matching different artists with the same show/album title.
	if source.Artist != "" && candidate.Artist != "" {
		sourceArtist := s.stringNormalizer.Normalize(source.Artist)
		candidateArtist := s.stringNormalizer.Normalize(candidate.Artist)
		if sourceArtist != candidateArtist {
			return false
		}
	}

	// Year should match if both are present.
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false
	}

	// For date-based releases (0day scene), require exact date match including month and day.
	// This prevents matching releases from different dates within the same year.
	if source.Year > 0 && source.Month > 0 && source.Day > 0 &&
		candidate.Year > 0 && candidate.Month > 0 && candidate.Day > 0 {
		if source.Month != candidate.Month || source.Day != candidate.Day {
			return false
		}
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
				return false
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
	sourceGroup := s.stringNormalizer.Normalize((source.Group))
	candidateGroup := s.stringNormalizer.Normalize((candidate.Group))

	// Only enforce group matching if the source has a group tag
	if sourceGroup != "" {
		// If source has a group, candidate must have the same group
		if candidateGroup == "" || sourceGroup != candidateGroup {
			return false
		}
	}
	// If source has no group, we don't care about candidate's group

	// Site field is used by anime releases where group is in brackets like [SubsPlease].
	// rls parses these as Site rather than Group. Different fansub groups can never
	// cross-seed, so enforce strict matching like Group.
	sourceSite := s.stringNormalizer.Normalize(source.Site)
	candidateSite := s.stringNormalizer.Normalize(candidate.Site)
	if sourceSite != "" {
		if candidateSite == "" || sourceSite != candidateSite {
			return false
		}
	}

	// Sum field contains the CRC32 checksum for anime releases like [32ECE75A].
	// Different checksums mean different files with 100% certainty.
	sourceSum := s.stringNormalizer.Normalize(source.Sum)
	candidateSum := s.stringNormalizer.Normalize(candidate.Sum)
	if sourceSum != "" {
		if candidateSum == "" || sourceSum != candidateSum {
			return false
		}
	}

	// Source must match if both are present (WEB-DL vs BluRay produce different files)
	sourceSource := s.stringNormalizer.Normalize((source.Source))
	candidateSource := s.stringNormalizer.Normalize((candidate.Source))
	if sourceSource != "" && candidateSource != "" && sourceSource != candidateSource {
		return false
	}

	// Resolution must match if both are present (1080p vs 2160p are different files)
	sourceRes := s.stringNormalizer.Normalize((source.Resolution))
	candidateRes := s.stringNormalizer.Normalize((candidate.Resolution))
	if sourceRes != "" && candidateRes != "" && sourceRes != candidateRes {
		return false
	}

	// Collection must match if either is present (NF vs AMZN vs Criterion are different sources)
	// If one release has a collection/service tag and the other doesn't, they cannot match
	sourceCollection := s.stringNormalizer.Normalize((source.Collection))
	candidateCollection := s.stringNormalizer.Normalize((candidate.Collection))
	if sourceCollection != candidateCollection {
		return false
	}

	// Codec must match if both are present (AVC vs HEVC produce different files).
	// Uses codec aliasing so x264/H.264/H264/AVC are treated as equivalent.
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 {
		sourceCodec := joinNormalizedCodecSlice(source.Codec)
		candidateCodec := joinNormalizedCodecSlice(candidate.Codec)
		if sourceCodec != candidateCodec {
			return false
		}
	}

	// HDR must match if either is present (HDR vs SDR are different encodes)
	// If one release has HDR metadata and the other doesn't, they cannot match
	sourceHDR := joinNormalizedSlice(source.HDR)
	candidateHDR := joinNormalizedSlice(candidate.HDR)
	if sourceHDR != candidateHDR {
		return false
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
	sourceChannels := s.stringNormalizer.Normalize((source.Channels))
	candidateChannels := s.stringNormalizer.Normalize((candidate.Channels))
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

	// Language must match if both are present (FRENCH vs ENGLISH are different audio/subs)
	if len(source.Language) > 0 && len(candidate.Language) > 0 {
		sourceLanguage := joinNormalizedSlice(source.Language)
		candidateLanguage := joinNormalizedSlice(candidate.Language)
		if sourceLanguage != candidateLanguage {
			return false
		}
	}

	// Version must match if both are present (v2 often has different files than v1)
	sourceVersion := s.stringNormalizer.Normalize(source.Version)
	candidateVersion := s.stringNormalizer.Normalize(candidate.Version)
	if sourceVersion != "" && candidateVersion != "" && sourceVersion != candidateVersion {
		return false
	}

	// Disc must match if both are present (Disc1 vs Disc2 are different content)
	sourceDisc := s.stringNormalizer.Normalize(source.Disc)
	candidateDisc := s.stringNormalizer.Normalize(candidate.Disc)
	if sourceDisc != "" && candidateDisc != "" && sourceDisc != candidateDisc {
		return false
	}

	// Platform must match if both are present (Windows vs macOS are different binaries)
	sourcePlatform := s.stringNormalizer.Normalize(source.Platform)
	candidatePlatform := s.stringNormalizer.Normalize(candidate.Platform)
	if sourcePlatform != "" && candidatePlatform != "" && sourcePlatform != candidatePlatform {
		return false
	}

	// Architecture must match if both are present (x64 vs x86 are different binaries)
	sourceArch := s.stringNormalizer.Normalize(source.Arch)
	candidateArch := s.stringNormalizer.Normalize(candidate.Arch)
	if sourceArch != "" && candidateArch != "" && sourceArch != candidateArch {
		return false
	}

	// Certain variant tags must match for safe cross-seeding.
	// IMAX/HYBRID always require exact match (different video masters).
	// REPACK/PROPER require exact match for non-pack content, but season packs
	// are exempt since a pack might contain a REPACK of just one episode.
	if compatible, _ := checkVariantsCompatible(source, candidate); !compatible {
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

// videoCodecAliases maps equivalent video codec names to a canonical form.
// x264, H.264, H264, and AVC all refer to the same underlying codec (AVC/H.264).
// x265, H.265, H265, and HEVC all refer to the same underlying codec (HEVC/H.265).
var videoCodecAliases = map[string]string{
	"X264":  "AVC",
	"H.264": "AVC",
	"H264":  "AVC",
	"AVC":   "AVC",
	"X265":  "HEVC",
	"H.265": "HEVC",
	"H265":  "HEVC",
	"HEVC":  "HEVC",
}

// normalizeVideoCodec converts a video codec string to its canonical form.
// Returns the original (uppercased) string if no alias mapping exists.
func normalizeVideoCodec(codec string) string {
	upper := strings.ToUpper(strings.TrimSpace(codec))
	if canonical, ok := videoCodecAliases[upper]; ok {
		return canonical
	}
	return upper
}

// joinNormalizedCodecSlice converts a codec slice to a normalized string for comparison.
// Applies codec aliasing so that x264, H.264, H264, and AVC are treated as equivalent.
func joinNormalizedCodecSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	normalized := make([]string, len(slice))
	for i, s := range slice {
		normalized[i] = normalizeVideoCodec(s)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, " ")
}

// getMatchTypeFromTitle checks if a candidate torrent has files matching what we want based on parsed title.
func (s *Service) getMatchTypeFromTitle(targetName, candidateName string, targetRelease, candidateRelease *rls.Release, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	// Build candidate release keys from actual files with enrichment.
	candidateReleases := make(map[releaseKey]int64)
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns, s.stringNormalizer) {
			fileRelease := s.parseReleaseName(cf.Name)
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
		targetKey := makeReleaseKey(targetRelease)

		if len(candidateReleases) > 0 {
			if _, exists := candidateReleases[targetKey]; exists {
				return "partial-in-pack"
			}
		}

	}

	// Fallback: rls couldn't derive usable release keys from the files, but the titles match and
	// the episode number encoded in the raw torrent names also matches (e.g. anime releases where
	// rls fails to parse " - 1150 " as an episode).
	if len(candidateReleases) == 0 {
		targetTitle := stringutils.NormalizeForMatching(targetRelease.Title)
		candidateTitle := stringutils.NormalizeForMatching(candidateRelease.Title)
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

			// If episode numbers match (anime fallback), accept the match
			if targetEp != "" && candidateEp != "" && targetEp == candidateEp {
				log.Debug().
					Str("title", targetRelease.Title).
					Str("episode", targetEp).
					Msg("Falling back to title+episode candidate match")
				return "partial-in-pack"
			}

			// For content without usable file-level metadata (games, apps, scene releases
			// with RAR files), trust the torrent-level releasesMatch check that got us here
			// when no episode pattern exists in the names.
			targetKey := makeReleaseKey(targetRelease)
			if targetKey == (releaseKey{}) && targetEp == "" && candidateEp == "" {
				return "release-match"
			}
		}
	}

	return ""
}

// MatchResult holds both the match type and a human-readable reason when there's no match.
type MatchResult struct {
	MatchType string // "exact", "partial-in-pack", "partial-contains", "size", or ""
	Reason    string // Human-readable reason when MatchType is "" (no match)
}

// getMatchTypeWithReason determines if files match for cross-seeding and provides
// a detailed reason when they don't match.
// tolerancePercent specifies the maximum size difference percentage for size matching (default 5%).
func (s *Service) getMatchTypeWithReason(sourceRelease, candidateRelease *rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string, tolerancePercent float64) MatchResult {
	var timer *prometheus.Timer
	if s.metrics != nil {
		timer = prometheus.NewTimer(s.metrics.GetMatchTypeDuration)
		defer timer.ObserveDuration()
		s.metrics.GetMatchTypeCalls.Inc()
	}

	// Check layout compatibility first (RAR vs extracted files)
	sourceLayout := classifyTorrentLayout(sourceFiles, ignorePatterns, s.stringNormalizer)
	candidateLayout := classifyTorrentLayout(candidateFiles, ignorePatterns, s.stringNormalizer)
	if sourceLayout != LayoutUnknown && candidateLayout != LayoutUnknown && sourceLayout != candidateLayout {
		if s.metrics != nil {
			s.metrics.GetMatchTypeNoMatch.Inc()
		}
		reason := fmt.Sprintf("Layout mismatch: source is %s, candidate is %s", layoutDescription(sourceLayout), layoutDescription(candidateLayout))
		return MatchResult{MatchType: "", Reason: reason}
	}

	// Stream through files to build filtered lists and accumulate sizes
	var (
		filteredSourceFiles    []TorrentFile
		filteredCandidateFiles []TorrentFile
		totalSourceSize        int64
		totalCandidateSize     int64
		sourceReleaseKeys      = make(map[releaseKey]int64)
		candidateReleaseKeys   = make(map[releaseKey]int64)
	)

	// Process source files
	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, ignorePatterns, s.stringNormalizer) {
			filteredSourceFiles = append(filteredSourceFiles, TorrentFile{
				Name: sf.Name,
				Size: sf.Size,
			})
			totalSourceSize += sf.Size

			fileRelease := s.parseReleaseName(sf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, sourceRelease)
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				if existingSize, exists := sourceReleaseKeys[key]; !exists || sf.Size > existingSize {
					sourceReleaseKeys[key] = sf.Size
				}
			}
		}
	}

	// Process candidate files
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns, s.stringNormalizer) {
			filteredCandidateFiles = append(filteredCandidateFiles, TorrentFile{
				Name: cf.Name,
				Size: cf.Size,
			})
			totalCandidateSize += cf.Size

			fileRelease := s.parseReleaseName(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				if existingSize, exists := candidateReleaseKeys[key]; !exists || cf.Size > existingSize {
					candidateReleaseKeys[key] = cf.Size
				}
			}
		}
	}

	// Check for exact file match
	if s.streamingExactMatch(filteredSourceFiles, filteredCandidateFiles) {
		if s.metrics != nil {
			s.metrics.GetMatchTypeExactMatch.Inc()
		}
		return MatchResult{MatchType: "exact", Reason: ""}
	}

	// Check for partial match
	if len(sourceReleaseKeys) > 0 && len(candidateReleaseKeys) > 0 {
		if s.checkPartialMatch(sourceReleaseKeys, candidateReleaseKeys) {
			if s.metrics != nil {
				s.metrics.GetMatchTypePartialMatch.Inc()
			}
			return MatchResult{MatchType: "partial-in-pack", Reason: ""}
		}

		if s.checkPartialMatch(candidateReleaseKeys, sourceReleaseKeys) {
			if s.metrics != nil {
				s.metrics.GetMatchTypePartialMatch.Inc()
			}
			return MatchResult{MatchType: "partial-contains", Reason: ""}
		}
	}

	// Size match with tolerance
	if totalSourceSize > 0 && len(filteredSourceFiles) > 0 {
		if s.isSizeWithinTolerance(totalSourceSize, totalCandidateSize, tolerancePercent) {
			if s.metrics != nil {
				s.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: "size", Reason: ""}
		}
	}

	// Fallback to largest file match
	if len(sourceReleaseKeys) == 0 && len(candidateReleaseKeys) == 0 &&
		len(filteredSourceFiles) > 0 && len(filteredCandidateFiles) > 0 {
		if s.streamingLargestFileMatch(filteredSourceFiles, filteredCandidateFiles) {
			if s.metrics != nil {
				s.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: "size", Reason: ""}
		}
	}

	// Build detailed reason for no match
	if s.metrics != nil {
		s.metrics.GetMatchTypeNoMatch.Inc()
	}

	reason := buildNoMatchReason(
		filteredSourceFiles, filteredCandidateFiles,
		totalSourceSize, totalCandidateSize,
		sourceReleaseKeys, candidateReleaseKeys,
		tolerancePercent,
	)
	return MatchResult{MatchType: "", Reason: reason}
}

// layoutDescription returns a human-readable description of a torrent layout.
func layoutDescription(layout TorrentLayout) string {
	switch layout {
	case LayoutFiles:
		return "extracted files"
	case LayoutArchives:
		return "RAR/archive"
	default:
		return "unknown"
	}
}

// buildNoMatchReason constructs a human-readable reason why files didn't match.
func buildNoMatchReason(
	sourceFiles, candidateFiles []TorrentFile,
	sourceSize, candidateSize int64,
	sourceKeys, candidateKeys map[releaseKey]int64,
	tolerancePercent float64,
) string {
	if len(sourceFiles) == 0 {
		return "No usable files in source torrent after filtering"
	}
	if len(candidateFiles) == 0 {
		return "No usable files in existing torrent after filtering"
	}

	// Size mismatch - calculate actual difference percentage
	if sourceSize != candidateSize {
		var diffPercent float64
		if sourceSize > 0 {
			diff := sourceSize - candidateSize
			if diff < 0 {
				diff = -diff
			}
			diffPercent = (float64(diff) / float64(sourceSize)) * 100
		}
		return fmt.Sprintf("Size mismatch: source %.2f GB vs existing %.2f GB (%.2f%% difference, tolerance %.1f%%)",
			float64(sourceSize)/(1024*1024*1024),
			float64(candidateSize)/(1024*1024*1024),
			diffPercent,
			tolerancePercent)
	}

	// File count mismatch with same size (rare but possible)
	if len(sourceFiles) != len(candidateFiles) {
		return fmt.Sprintf("File count mismatch: source has %d files, existing has %d files",
			len(sourceFiles), len(candidateFiles))
	}

	// Release keys couldn't be parsed
	if len(sourceKeys) == 0 && len(candidateKeys) == 0 {
		return "Unable to parse release metadata from filenames"
	}

	// Keys don't overlap
	if len(sourceKeys) > 0 && len(candidateKeys) > 0 {
		return "Release metadata doesn't match between source and existing files"
	}

	return "Files don't match (structure or naming differs)"
}

// getMatchType determines if files match for cross-seeding.
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match.
// Uses streaming file comparison to reduce memory usage.
func (s *Service) getMatchType(sourceRelease, candidateRelease *rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, ignorePatterns []string) string {
	var timer *prometheus.Timer
	if s.metrics != nil {
		timer = prometheus.NewTimer(s.metrics.GetMatchTypeDuration)
		defer timer.ObserveDuration()
		s.metrics.GetMatchTypeCalls.Inc()
	}

	sourceLayout := classifyTorrentLayout(sourceFiles, ignorePatterns, s.stringNormalizer)
	candidateLayout := classifyTorrentLayout(candidateFiles, ignorePatterns, s.stringNormalizer)
	if sourceLayout != LayoutUnknown && candidateLayout != LayoutUnknown && sourceLayout != candidateLayout {
		if s.metrics != nil {
			s.metrics.GetMatchTypeNoMatch.Inc()
		}
		return ""
	}

	// Stream through files to build filtered lists and accumulate sizes
	var (
		filteredSourceFiles    []TorrentFile
		filteredCandidateFiles []TorrentFile
		totalSourceSize        int64
		totalCandidateSize     int64
		sourceReleaseKeys      = make(map[releaseKey]int64)
		candidateReleaseKeys   = make(map[releaseKey]int64)
	)

	// Process source files
	for _, sf := range sourceFiles {
		if !shouldIgnoreFile(sf.Name, ignorePatterns, s.stringNormalizer) {
			filteredSourceFiles = append(filteredSourceFiles, TorrentFile{
				Name: sf.Name,
				Size: sf.Size,
			})
			totalSourceSize += sf.Size

			fileRelease := s.parseReleaseName(sf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, sourceRelease)
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				// Keep max size when multiple files map to same key (e.g., mkv vs nfo for movies)
				if existingSize, exists := sourceReleaseKeys[key]; !exists || sf.Size > existingSize {
					sourceReleaseKeys[key] = sf.Size
				}
			}
		}
	}

	// Process candidate files
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, ignorePatterns, s.stringNormalizer) {
			filteredCandidateFiles = append(filteredCandidateFiles, TorrentFile{
				Name: cf.Name,
				Size: cf.Size,
			})
			totalCandidateSize += cf.Size

			fileRelease := s.parseReleaseName(cf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, candidateRelease)
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				// Keep max size when multiple files map to same key (e.g., mkv vs nfo for movies)
				if existingSize, exists := candidateReleaseKeys[key]; !exists || cf.Size > existingSize {
					candidateReleaseKeys[key] = cf.Size
				}
			}
		}
	}

	// Check for exact file match using streaming comparison
	if s.streamingExactMatch(filteredSourceFiles, filteredCandidateFiles) {
		if s.metrics != nil {
			s.metrics.GetMatchTypeExactMatch.Inc()
		}
		return "exact"
	}

	// Check for partial match (season pack scenario, date-based releases, etc.).
	if len(sourceReleaseKeys) > 0 && len(candidateReleaseKeys) > 0 {
		// Check if source files are contained in candidate (source episode in candidate pack).
		if s.checkPartialMatch(sourceReleaseKeys, candidateReleaseKeys) {
			if s.metrics != nil {
				s.metrics.GetMatchTypePartialMatch.Inc()
			}
			return "partial-in-pack"
		}

		// Check if candidate files are contained in source (candidate episode in source pack).
		if s.checkPartialMatch(candidateReleaseKeys, sourceReleaseKeys) {
			if s.metrics != nil {
				s.metrics.GetMatchTypePartialMatch.Inc()
			}
			return "partial-contains"
		}
	}

	// Size match for same content with different structure.
	if totalSourceSize > 0 && totalSourceSize == totalCandidateSize && len(filteredSourceFiles) > 0 {
		if s.metrics != nil {
			s.metrics.GetMatchTypeSizeMatch.Inc()
		}
		return "size"
	}

	// If rls couldn't derive usable release keys but both torrents have at least one non-ignored
	// file, fall back to comparing the largest file by base name and size.
	if len(sourceReleaseKeys) == 0 && len(candidateReleaseKeys) == 0 &&
		len(filteredSourceFiles) > 0 && len(filteredCandidateFiles) > 0 {
		if s.streamingLargestFileMatch(filteredSourceFiles, filteredCandidateFiles) {
			if s.metrics != nil {
				s.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return "size"
		}
	}

	if s.metrics != nil {
		s.metrics.GetMatchTypeNoMatch.Inc()
	}
	return ""
}

// streamingExactMatch checks if two file lists have exactly matching paths and sizes.
// Uses streaming comparison to avoid storing all files in memory.
func (s *Service) streamingExactMatch(sourceFiles, candidateFiles []TorrentFile) bool {
	if len(sourceFiles) != len(candidateFiles) {
		return false
	}

	// Create a map of source files for lookup
	sourceMap := make(map[string]int64, len(sourceFiles))
	for _, sf := range sourceFiles {
		sourceMap[sf.Name] = sf.Size
	}

	// Check all candidate files exist in source with same size
	for _, cf := range candidateFiles {
		if sourceSize, exists := sourceMap[cf.Name]; !exists || sourceSize != cf.Size {
			return false
		}
	}

	return true
}

// streamingLargestFileMatch compares the largest files by size and base filename.
// Returns true if the largest files match in size and normalized base name.
func (s *Service) streamingLargestFileMatch(sourceFiles, candidateFiles []TorrentFile) bool {
	var (
		srcPath  string
		srcSize  int64
		candPath string
		candSize int64
	)

	// Find largest source file
	for _, sf := range sourceFiles {
		if sf.Size > srcSize {
			srcSize = sf.Size
			srcPath = sf.Name
		}
	}

	// Find largest candidate file
	for _, cf := range candidateFiles {
		if cf.Size > candSize {
			candSize = cf.Size
			candPath = cf.Name
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
			return true
		}
	}

	return false
}

// enrichReleaseFromTorrent enriches file release info with metadata from torrent name.
// This fills in missing group, resolution, codec, and other metadata from the season pack.
func enrichReleaseFromTorrent(fileRelease *rls.Release, torrentRelease *rls.Release) *rls.Release {
	enriched := *fileRelease

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

	return &enriched
}

// shouldIgnoreFile checks if a file should be ignored based on patterns.
func shouldIgnoreFile(filename string, patterns []string, normalizer *stringutils.Normalizer[string, string]) bool {
	lower := normalizer.Normalize(filename)

	for _, pattern := range patterns {
		pattern = normalizer.Normalize(pattern)
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
