// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// matching.go groups all heuristics and helpers that decide whether two torrents
// describe the same underlying content.

func isTVRelease(r *rls.Release) bool {
	return r != nil && (r.Type == rls.Series || r.Type == rls.Episode || r.Series > 0 || r.Episode > 0)
}

// isTVEpisode returns true if the release is a TV episode, including anime-style
// absolute-numbered episodes that do not carry a season number.
func isTVEpisode(r *rls.Release) bool {
	return isTVRelease(r) && r.Episode > 0
}

// isTVSeasonPack returns true if the release is a TV season pack, including
// seasonless anime packs where file inspection marked the release as TV.
func isTVSeasonPack(r *rls.Release) bool {
	return isTVRelease(r) && (r.Series > 0 || r.Type == rls.Series) && r.Episode == 0
}

// rejectReasonSeasonPackFromEpisode is the reason returned when rejecting a season pack
// cross-seed attempt against a single-episode torrent.
const rejectReasonSeasonPackFromEpisode = "Season packs cannot be cross-seeded against single-episode torrents"

// rejectSeasonPackFromEpisode checks if a cross-seed should be rejected because it would
// apply a season pack based on a single-episode torrent's files. This is a forbidden pairing
// that leads to incomplete/incorrect cross-seeds.
//
// Parameters:
//   - newR: the incoming/source release (the torrent being added)
//   - existingR: the candidate/matched release (the existing torrent with files)
//   - episodeMatching: whether episode-aware matching mode is enabled
//
// Returns (reject=true, reason) if the pairing should be rejected, (false, "") otherwise.
func rejectSeasonPackFromEpisode(newR, existingR *rls.Release, episodeMatching bool) (reject bool, reason string) {
	if episodeMatching && isTVSeasonPack(newR) && isTVEpisode(existingR) {
		return true, rejectReasonSeasonPackFromEpisode
	}
	return false, ""
}

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
	match, _ := s.releasesMatchWithReason(source, candidate, findIndividualEpisodes)
	return match
}

func (s *Service) releasesMatchWithReason(source, candidate *rls.Release, findIndividualEpisodes bool) (bool, string) {
	return s.releasesMatchWithReasonAndNames(source, candidate, "", "", findIndividualEpisodes)
}

func (s *Service) releasesMatchWithReasonAndNames(source, candidate *rls.Release, sourceName, candidateName string, findIndividualEpisodes bool) (bool, string) {
	return s.releasesMatchWithReasonAndNamesAndTitles(source, candidate, sourceName, candidateName, nil, nil, findIndividualEpisodes)
}

func (s *Service) releasesMatchWithReasonAndNamesAndTitles(source, candidate *rls.Release, sourceName, candidateName string, sourceTitles, candidateTitles []string, findIndividualEpisodes bool) (bool, string) {
	if source == candidate {
		return true, ""
	}

	isTV := isTVRelease(source) || isTVRelease(candidate)
	if ok, reason := s.validateTitleArtistAndDates(source, candidate, sourceName, candidateName, sourceTitles, candidateTitles, isTV); !ok {
		return false, reason
	}
	if ok, reason := validateTVStructure(source, candidate, findIndividualEpisodes, isTV); !ok {
		return false, reason
	}
	if ok, reason := s.validateGroupSiteAndChecksum(source, candidate, false); !ok {
		return false, reason
	}
	if ok, reason := s.validateFormatAndCodec(source, candidate); !ok {
		return false, reason
	}
	if ok, reason := s.validateMetadataFlags(source, candidate); !ok {
		return false, reason
	}
	if ok, reason := validateReleaseVariants(source, candidate); !ok {
		return false, reason
	}

	return true, ""
}

func normalizerForService(s *Service) *stringutils.Normalizer[string, string] {
	if s != nil && s.stringNormalizer != nil {
		return s.stringNormalizer
	}
	// Reuse the process-wide singleton instead of allocating a fresh
	// normalizer here. Every NewDefaultNormalizer() spins up a ttlcache
	// whose startExpirations goroutine never terminates (the throwaway
	// cache is never closed), and this is on the cross-seed matching hot
	// path - one call per release pair - so a fresh allocation leaks a
	// goroutine on every comparison.
	return stringutils.DefaultNormalizer
}

// titleMismatchReason is the rejection reason emitted when two releases differ
// only by title. The title-rescue gates key off this exact value, so it is a
// named constant rather than an inline literal.
const titleMismatchReason = "title mismatch"

func (s *Service) validateTitleArtistAndDates(source, candidate *rls.Release, sourceName, candidateName string, sourceExtraTitles, candidateExtraTitles []string, isTV bool) (bool, string) {
	// Title should match closely but not necessarily exactly.
	// Use punctuation-stripping normalization to handle differences like
	// "Bob's Burgers" vs "Bobs.Burgers" (apostrophes lost in dot notation).
	sourceTitles := s.normalizedReleaseTitles(source, sourceName)
	candidateTitles := s.normalizedReleaseTitles(candidate, candidateName)
	addNormalizedTitles(sourceTitles, sourceExtraTitles)
	addNormalizedTitles(candidateTitles, candidateExtraTitles)
	if len(sourceTitles) == 0 || len(candidateTitles) == 0 {
		return false, "empty normalized title"
	}

	// Accept any overlap between normalized title sets. Each set contains complete
	// normalized title entries from Title, Alt, and parsed AKA parts, so legitimate
	// alternate titles can match without requiring strict equality of one parsed title.
	//
	// This still avoids false positives between related-but-distinct TV franchises
	// (e.g. "FBI" vs "FBI Most Wanted") because overlap is checked on full normalized
	// title entries, not arbitrary substrings.
	if !normalizedTitleSetsOverlap(sourceTitles, candidateTitles) {
		// Title mismatches are expected for most candidates - don't log to avoid noise
		return false, titleMismatchReason
	}

	return s.validateArtistAndDates(source, candidate, isTV)
}

func (s *Service) validateArtistAndDates(source, candidate *rls.Release, isTV bool) (bool, string) {
	normalizer := normalizerForService(s)

	// Artist must match for content with artist metadata (music, 0day scene radio shows, etc.)
	// This prevents matching different artists with the same show/album title.
	if source.Artist != "" && candidate.Artist != "" {
		sourceArtist := normalizer.Normalize(source.Artist)
		candidateArtist := normalizer.Normalize(candidate.Artist)
		if sourceArtist != candidateArtist {
			return false, "artist mismatch"
		}
	}

	// Year should match if both are present.
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false, "year mismatch"
	}

	// For date-based releases (0day scene), require exact date match including month and day.
	// This prevents matching releases from different dates within the same year.
	if source.Year > 0 && source.Month > 0 && source.Day > 0 &&
		candidate.Year > 0 && candidate.Month > 0 && candidate.Day > 0 {
		if source.Month != candidate.Month || source.Day != candidate.Day {
			return false, "date mismatch"
		}
	}

	// For non-TV content where rls has inferred a concrete content type (movie, music,
	// audiobook, etc.), require the types to match. This prevents, for example,
	// music releases from matching audiobooks with similar titles.
	if !isTV && source.Type != 0 && candidate.Type != 0 && source.Type != candidate.Type {
		return false, "content type mismatch"
	}

	return true, ""
}

// releasesMatchExceptTitleWithReason keeps every normal release rule except title.
// Retitled listings are usually bare-file re-uploads that also drop the -GROUP
// and [CRC] tags, so absent candidate tags are tolerated here; conflicting tags
// still reject. Every caller must pair this with an exact-size gate, because
// with the title ignored a sparsely parsed name can leave nothing else to
// reject on. Callers that add data (search rescue) additionally get the
// paused-add full recheck as the final authority; callers that only report
// existing pairings (local match detection) do not, so their false positives
// must stay confined to display.
func (s *Service) releasesMatchExceptTitleWithReason(source, candidate *rls.Release, findIndividualEpisodes bool) (bool, string) {
	isTV := isTVRelease(source) || isTVRelease(candidate)
	if ok, reason := s.validateArtistAndDates(source, candidate, isTV); !ok {
		return false, reason
	}
	if ok, reason := validateTVStructure(source, candidate, findIndividualEpisodes, isTV); !ok {
		return false, reason
	}
	if ok, reason := s.validateGroupSiteAndChecksum(source, candidate, true); !ok {
		return false, reason
	}
	if ok, reason := s.validateFormatAndCodec(source, candidate); !ok {
		return false, reason
	}
	if ok, reason := s.validateMetadataFlags(source, candidate); !ok {
		return false, reason
	}
	return validateReleaseVariants(source, candidate)
}

func (s *Service) normalizedReleaseTitles(release *rls.Release, rawName string) map[string]struct{} {
	titles := make(map[string]struct{})
	addNormalizedTitle(titles, releaseTitle(release))
	addNormalizedTitle(titles, releaseAlt(release))

	// Cached parse: this runs once per library torrent per search.
	for _, rawTitle := range rawAKATitleParts(rawName) {
		parsed := releases.DefaultParser.Parse(rawTitle)
		addNormalizedTitle(titles, parsed.Title)
		addNormalizedTitle(titles, parsed.Alt)
	}

	// rls ends the title at a slash, so an indexer listing like
	// "Fate/strange Fake S01 ..." parses as "Fate" while the dot-separated source
	// name parses as "Fate strange Fake". Read the slash as a separator too and
	// keep both spellings.
	//
	// Keep only the reading that extends the truncated title: dropping the slash
	// also re-splits artist from title ("AC/DC - Back In Black" -> title "Back In
	// Black"), which would reach a different artist's album.
	//
	// TODO: drop this block at the next rls bump if
	// TestReleasesMatch_SlashInTitleReadsAsSeparator still passes without it. rls
	// merely keeping "/" inside Title is not enough: NormalizeForMatching passes
	// "/" through, so "fate/strange fake" still will not equal "fate strange fake".
	if unslashed := strings.ReplaceAll(strings.ReplaceAll(rawName, "/", " "), `\`, " "); unslashed != rawName {
		parsed := s.parseReleaseName(unslashed)
		if strings.HasPrefix(parsed.Title, releaseTitle(release)) {
			addNormalizedTitle(titles, parsed.Title)
			addNormalizedTitle(titles, parsed.Alt)
		}
	}

	return titles
}

func addNormalizedTitles(titles map[string]struct{}, extraTitles []string) {
	for _, title := range extraTitles {
		addNormalizedTitle(titles, title)
	}
}

func releaseTitle(release *rls.Release) string {
	if release == nil {
		return ""
	}
	return release.Title
}

func releaseAlt(release *rls.Release) string {
	if release == nil {
		return ""
	}
	return release.Alt
}

func rawAKATitleParts(rawName string) []string {
	if rawName == "" || !strings.Contains(rawName, " AKA ") {
		return nil
	}

	const minAKATitleLength = 4
	parts := strings.Split(rawName, " AKA ")
	titles := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) >= minAKATitleLength {
			titles = append(titles, part)
		}
	}
	if len(titles) < 2 {
		return nil
	}
	return titles
}

func addNormalizedTitle(titles map[string]struct{}, title string) {
	normalized := stringutils.NormalizeForMatching(title)
	if normalized != "" {
		titles[normalized] = struct{}{}
	}
}

func normalizedTitleSetsOverlap(sourceTitles, candidateTitles map[string]struct{}) bool {
	for title := range sourceTitles {
		if _, exists := candidateTitles[title]; exists {
			return true
		}
	}
	return false
}

func validateTVStructure(source, candidate *rls.Release, findIndividualEpisodes, isTV bool) (bool, string) {
	if !isTV {
		return true, ""
	}

	// For TV shows, season and episode structure must match based on settings.
	sourceIsTV := isTVRelease(source)
	candidateIsTV := isTVRelease(candidate)
	sourceIsPack := isTVSeasonPack(source)
	candidateIsPack := isTVSeasonPack(candidate)

	if sourceIsTV && !candidateIsTV {
		return false, "candidate not recognized as TV"
	}
	if candidateIsTV && !sourceIsTV {
		return false, "source not recognized as TV"
	}

	if source.Series > 0 && candidate.Series > 0 && source.Series != candidate.Series {
		return false, "season mismatch"
	}

	if !findIndividualEpisodes {
		// Strict matching: season packs only match season packs, episodes only match episodes
		if sourceIsPack != candidateIsPack {
			return false, "season pack versus episode mismatch"
		}

		// If both are individual episodes, episodes must match
		if !sourceIsPack && !candidateIsPack && source.Episode != candidate.Episode {
			return false, "episode mismatch"
		}
		return true, ""
	}

	// Flexible matching: allow season packs to match individual episodes.
	// But individual episodes still need exact episode matching.
	if !sourceIsPack && !candidateIsPack && source.Episode != candidate.Episode {
		return false, "episode mismatch"
	}

	return true, ""
}

func (s *Service) validateGroupSiteAndChecksum(source, candidate *rls.Release, tolerateMissingCandidateTags bool) (bool, string) {
	// Group tags should match for proper cross-seeding compatibility.
	// Different release groups often have different encoding settings and file structures.
	normalizer := normalizerForService(s)
	sourceGroup := normalizer.Normalize((source.Group))
	candidateGroup := normalizer.Normalize((candidate.Group))
	sourceSite := normalizer.Normalize(source.Site)
	candidateSite := normalizer.Normalize(candidate.Site)

	// Only enforce group matching if the source has a group tag
	if sourceGroup != "" {
		candidateGroupIdentity := candidateGroup
		if candidateGroupIdentity == "" {
			candidateGroupIdentity = candidateSite
		}
		switch {
		case candidateGroupIdentity == "":
			if !tolerateMissingCandidateTags {
				return false, "group mismatch"
			}
		case sourceGroup != candidateGroupIdentity:
			return false, "group mismatch"
		}
	}
	// If source has no group, we don't care about candidate's group

	// Site field is used by anime releases where group is in brackets like [SubsPlease].
	// rls parses these as Site rather than Group. Different fansub groups can never
	// cross-seed, but many indexer titles omit the site tag entirely. Treat mismatched
	// non-empty site tags as incompatible, but don't reject candidates that simply
	// lack this metadata.
	if sourceSite != "" {
		candidateSiteIdentity := candidateSite
		if candidateSiteIdentity == "" {
			candidateSiteIdentity = candidateGroup
		}
		if candidateSiteIdentity != "" && sourceSite != candidateSiteIdentity {
			return false, "site mismatch"
		}
	}

	// Sum field contains the CRC32 checksum for anime releases like [32ECE75A].
	// Different checksums mean different files with 100% certainty.
	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if sourceSum != "" {
		switch {
		case candidateSum == "":
			if !tolerateMissingCandidateTags {
				return false, "checksum mismatch"
			}
		case sourceSum != candidateSum:
			return false, "checksum mismatch"
		}
	}

	return true, ""
}

// sourceMismatchReason is the rejection reason emitted when two releases differ
// only by an incompatible source (e.g. WEBRip vs WEB-DL). Callers key off this
// exact value to apply cross-tracker relabel tolerance, so it is a named constant
// rather than an inline literal.
const sourceMismatchReason = "source mismatch"

func (s *Service) validateFormatAndCodec(source, candidate *rls.Release) (bool, string) {
	normalizer := normalizerForService(s)

	// Source must be compatible if both are present.
	// WEB is ambiguous and matches both WEB-DL and WEBRip.
	// WEB-DL and WEBRip are explicitly different and do not match.
	// Other sources (BluRay, HDTV, etc.) must match exactly.
	sourceSource := normalizeSource(source.Source)
	candidateSource := normalizeSource(candidate.Source)
	if !sourcesCompatible(sourceSource, candidateSource) {
		return false, sourceMismatchReason
	}

	// Resolution must match (1080p vs 2160p are different files).
	// Exception: empty resolution is allowed to match SD resolutions (480p, 576p, SD).
	sourceRes := normalizer.Normalize((source.Resolution))
	candidateRes := normalizer.Normalize((candidate.Resolution))
	if sourceRes != candidateRes {
		// rls omits resolution for many SD releases (e.g. "WEB" without "480p"), so
		// treat an empty resolution as a match only when the other side is clearly SD.
		isKnownSD := func(res string) bool {
			switch normalizeVariant(res) {
			case "480P", "576P", "SD":
				return true
			default:
				return false
			}
		}

		sdFallbackAllowed := (sourceRes == "" && isKnownSD(candidateRes)) || (candidateRes == "" && isKnownSD(sourceRes))
		if !sdFallbackAllowed {
			return false, "resolution mismatch"
		}
	}

	// Collection must match when both names carry a collection/service tag
	// (NF vs AMZN vs Criterion are different masters). Trackers routinely drop
	// the service tag from otherwise identical names, so a tag on only one
	// side is not evidence of a different master; the per-file size comparison
	// downstream catches real mismatches before anything is injected.
	sourceCollection := normalizer.Normalize(source.Collection)
	candidateCollection := normalizer.Normalize(candidate.Collection)
	if sourceCollection != "" && candidateCollection != "" && sourceCollection != candidateCollection {
		return false, "collection mismatch"
	}

	// Codec must match if both are present (AVC vs HEVC produce different files).
	// Uses codec aliasing so x264/H.264/H264/AVC are treated as equivalent.
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 {
		sourceCodec := joinNormalizedCodecSlice(source.Codec)
		candidateCodec := joinNormalizedCodecSlice(candidate.Codec)
		if sourceCodec != candidateCodec {
			return false, "codec mismatch"
		}
	}

	// HDR must match when both names carry HDR metadata (HDR vs SDR are
	// different encodes). Trackers routinely drop HDR tags from otherwise
	// identical names, so a tag on only one side is not evidence of a
	// different encode; the per-file size comparison downstream catches real
	// mismatches before anything is injected.
	sourceHDR := joinNormalizedHDRSlice(source.HDR)
	candidateHDR := joinNormalizedHDRSlice(candidate.HDR)
	if sourceHDR != "" && candidateHDR != "" && sourceHDR != candidateHDR {
		return false, "hdr mismatch"
	}

	// Bit depth should match when both are present (8-bit vs 10-bit are different encodes).
	// We intentionally don't enforce "either present" here since indexer titles often omit it.
	sourceBitDepth := normalizer.Normalize(source.BitDepth)
	candidateBitDepth := normalizer.Normalize(candidate.BitDepth)
	if sourceBitDepth != "" && candidateBitDepth != "" && sourceBitDepth != candidateBitDepth {
		return false, "bit depth mismatch"
	}

	return true, ""
}

func (s *Service) validateMetadataFlags(source, candidate *rls.Release) (bool, string) {
	normalizer := normalizerForService(s)

	// NOTE: Audio codec and channel checks are intentionally omitted here.
	// Indexer metadata can be inaccurate (e.g., BTN returning DDPA5.1 when the
	// actual file is DDP5.1). The downstream file size matching in
	// hasContentFileSizeMismatch() and alignFilesForCrossSeed() will catch
	// any real mismatches, so we let potential matches through for validation.

	// Cut must match if both are present (Theatrical vs Extended are different versions)
	if len(source.Cut) > 0 && len(candidate.Cut) > 0 {
		sourceCut := joinNormalizedSlice(source.Cut)
		candidateCut := joinNormalizedSlice(candidate.Cut)
		if sourceCut != candidateCut {
			return false, "cut mismatch"
		}
	}

	// Edition must match if both are present (Remastered vs Original are different)
	if len(source.Edition) > 0 && len(candidate.Edition) > 0 {
		sourceEdition := joinNormalizedSlice(source.Edition)
		candidateEdition := joinNormalizedSlice(candidate.Edition)
		if sourceEdition != candidateEdition {
			return false, "edition mismatch"
		}
	}

	// Language must match if both are present (FRENCH vs ENGLISH are different audio/subs).
	// A missing tag means unknown, not English: trackers like Aither label the original
	// audio language (JAPANESE, KOREAN) on releases that other trackers publish untagged,
	// so assuming ENGLISH rejected every anime cross-seed. Genuine dub mismatches are
	// caught downstream by the exact per-file size comparison before anything is injected.
	if len(source.Language) > 0 && len(candidate.Language) > 0 {
		if joinNormalizedSlice(source.Language) != joinNormalizedSlice(candidate.Language) {
			return false, "language mismatch"
		}
	}

	// Version must match if both are present (v2 often has different files than v1)
	sourceVersion := normalizer.Normalize(source.Version)
	candidateVersion := normalizer.Normalize(candidate.Version)
	if sourceVersion != "" && candidateVersion != "" && sourceVersion != candidateVersion {
		return false, "version mismatch"
	}

	// Disc must match if both are present (Disc1 vs Disc2 are different content)
	sourceDisc := normalizer.Normalize(source.Disc)
	candidateDisc := normalizer.Normalize(candidate.Disc)
	if sourceDisc != "" && candidateDisc != "" && sourceDisc != candidateDisc {
		return false, "disc mismatch"
	}

	// Platform must match if both are present (Windows vs macOS are different binaries)
	sourcePlatform := normalizer.Normalize(source.Platform)
	candidatePlatform := normalizer.Normalize(candidate.Platform)
	if sourcePlatform != "" && candidatePlatform != "" && sourcePlatform != candidatePlatform {
		return false, "platform mismatch"
	}

	// Architecture must match if both are present (x64 vs x86 are different binaries)
	sourceArch := normalizer.Normalize(source.Arch)
	candidateArch := normalizer.Normalize(candidate.Arch)
	if sourceArch != "" && candidateArch != "" && sourceArch != candidateArch {
		return false, "architecture mismatch"
	}

	return true, ""
}

func validateReleaseVariants(source, candidate *rls.Release) (bool, string) {
	// Certain variant tags must match for safe cross-seeding.
	// IMAX/HYBRID always require exact match (different video masters).
	// REPACK/PROPER require exact match for non-pack content, but season packs
	// are exempt since a pack might contain a REPACK of just one episode.
	if compatible, reason := checkVariantsCompatible(source, candidate); !compatible {
		if reason == "" {
			reason = "variant mismatch"
		}
		return false, reason
	}

	return true, ""
}

// joinNormalizedSlice converts a string slice to a normalized uppercase string for comparison.
// Uppercases and joins elements to ensure consistent comparison regardless of case or order.
func joinNormalizedSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	normalized := make([]string, len(slice))
	for i, s := range slice {
		normalized[i] = normalizeVariant(s)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, " ")
}

func joinNormalizedHDRSlice(slice []string) string {
	normalized := releases.NormalizeHDRTags(slice)
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
	upper := normalizeVariant(codec)
	if canonical, ok := videoCodecAliases[upper]; ok {
		return canonical
	}
	return upper
}

// sourceAliases maps source names to a canonical form for comparison.
// WEB-DL variants normalize to WEBDL, WEBRip variants to WEBRIP.
// Plain "WEB" stays as "WEB" and is treated as ambiguous (matches both).
var sourceAliases = map[string]string{
	"WEB-DL": "WEBDL",
	"WEBDL":  "WEBDL",
	"WEBRIP": "WEBRIP",
	"WEB":    "WEB",
}

// normalizeSource converts a source string to its canonical form.
// Returns the original (uppercased) string if no alias mapping exists.
func normalizeSource(source string) string {
	upper := normalizeVariant(source)
	if canonical, ok := sourceAliases[upper]; ok {
		return canonical
	}
	return upper
}

func isWebSource(source string) bool {
	switch source {
	case "WEB", "WEBDL", "WEBRIP":
		return true
	default:
		return false
	}
}

// sourcesCompatible checks if two sources are compatible for cross-seed precheck.
// Plain "WEB" is ambiguous and matches both WEBDL and WEBRIP.
// WEBDL and WEBRIP are explicitly different and do not match each other.
// The final apply stage trusts file verification, so this is just for precheck gating.
func sourcesCompatible(source, candidate string) bool {
	if source == "" || candidate == "" {
		return true
	}
	if source == candidate {
		return true
	}

	if !isWebSource(source) || !isWebSource(candidate) {
		return false
	}

	// At this point both are web sources, but they differ.
	// WEBDL and WEBRIP are explicitly different and do not match each other.
	return source == "WEB" || candidate == "WEB"
}

// isWebSourceRelabel reports whether candidate is the same release as source with
// only its web-source label changed (e.g. WEBRip vs WEB-DL). The identical web
// encode is frequently relabeled across trackers, so when nothing but the web
// source differs we let the candidate reach the apply-stage file verification and
// qBittorrent recheck instead of dropping it on the label alone. Callers must
// still confirm the candidate size is within tolerance before trusting this.
func (s *Service) isWebSourceRelabel(source, candidate *rls.Release, sourceName, candidateName string, sourceTitles, candidateTitles []string, findIndividualEpisodes bool) bool {
	if source == nil || candidate == nil {
		return false
	}
	if !isWebSource(normalizeSource(source.Source)) || !isWebSource(normalizeSource(candidate.Source)) {
		return false
	}

	// Equalize the web-source label and re-run the full match. If it now matches,
	// the source label was the only difference between the two releases.
	probe := *candidate
	probe.Source = source.Source
	match, _ := s.releasesMatchWithReasonAndNamesAndTitles(source, &probe, sourceName, candidateName, sourceTitles, candidateTitles, findIndividualEpisodes)
	return match
}

// shouldAcceptWebSourceRelabel reports whether a candidate that the release match
// rejected solely on source mismatch should still be accepted as a cross-tracker
// web-source relabel (WEBRip<->WEB-DL). ignoreSizeCheck mirrors the main size gate:
// a single episode of a season-pack source is legitimately much smaller than its
// pack, so the full-size tolerance is bypassed in that case and the apply-stage
// file verification makes the final call.
func (s *Service) shouldAcceptWebSourceRelabel(
	source, candidate *rls.Release,
	sourceName, candidateName string,
	sourceTitles, candidateTitles []string,
	findIndividualEpisodes, ignoreSizeCheck bool,
	sourceSize, candidateSize int64,
	tolerancePercent float64,
	mismatchReason string,
) bool {
	if mismatchReason != sourceMismatchReason {
		return false
	}
	if !ignoreSizeCheck && !s.isSizeWithinTolerance(sourceSize, candidateSize, tolerancePercent) {
		return false
	}
	return s.isWebSourceRelabel(source, candidate, sourceName, candidateName, sourceTitles, candidateTitles, findIndividualEpisodes)
}

// joinNormalizedCodecSlice converts a codec slice to a normalized string for comparison.
// Applies codec aliasing so that x264, H.264, H264, and AVC are treated as equivalent.
func joinNormalizedCodecSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(slice))
	normalized := make([]string, 0, len(slice))
	for _, codec := range slice {
		n := normalizeVideoCodec(codec)
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		normalized = append(normalized, n)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, " ")
}

// getMatchTypeFromTitle checks if a candidate torrent has files matching what we want based on parsed title.
func (s *Service) getMatchTypeFromTitle(targetName, candidateName string, targetRelease, candidateRelease *rls.Release, candidateFiles qbt.TorrentFiles) string {
	// Build candidate release keys from actual files with enrichment.
	candidateReleases := make(map[releaseKey]int64)
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, s.stringNormalizer) {
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

	// Renamed-file fallback: the torrent-level release gate already matched this
	// candidate, but its files parse no episode-level keys (renamed files only
	// inherit the pack's series through enrichment, never an episode). Pass the
	// candidate through; the file-level matcher decides by size containment at
	// apply. Candidates with parseable episode keys skip this, so a well-named
	// pack that simply lacks the target episode still rejects here.
	if targetRelease.Series > 0 && candidateRelease.Series == targetRelease.Series &&
		!hasEpisodeLevelKeys(candidateReleases) {
		return "release-match"
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

func hasEpisodeLevelKeys(keys map[releaseKey]int64) bool {
	for key := range keys {
		if key.episode > 0 {
			return true
		}
	}
	return false
}

// MatchResult holds both the match type and a human-readable reason when there's no match.
type MatchResult struct {
	MatchType string // "exact", "partial-in-pack", "partial-contains", "size", "size-partial-in-pack", "size-partial-contains", or ""
	Reason    string // Human-readable reason when MatchType is "" (no match)
}

// getMatchTypeWithReason determines if files match for cross-seeding and provides
// a detailed reason when they don't match.
// tolerancePercent specifies the maximum size difference percentage for size matching (default 5%).
func (s *Service) getMatchTypeWithReason(sourceRelease, candidateRelease *rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles, tolerancePercent float64) MatchResult {
	var timer *prometheus.Timer
	if s.metrics != nil {
		timer = prometheus.NewTimer(s.metrics.GetMatchTypeDuration)
		defer timer.ObserveDuration()
		s.metrics.GetMatchTypeCalls.Inc()
	}

	normalizer := normalizerForService(s)

	// Check layout compatibility first (RAR vs extracted files)
	sourceLayout := classifyTorrentLayout(sourceFiles, normalizer)
	candidateLayout := classifyTorrentLayout(candidateFiles, normalizer)
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
		if !shouldIgnoreFile(sf.Name, normalizer) {
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
		if !shouldIgnoreFile(cf.Name, normalizer) {
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

	// Size-only containment: one side's files all pair 1:1 by exact size into
	// the other side. Rescues subset-of-pack matches whose file names rls cannot
	// parse (music albums in packs, renamed season packs). It runs before the
	// tolerant total-size tier because with unequal file counts a strict per-file
	// pairing is better evidence than totals landing inside the tolerance, and
	// the containment type engages the episode-in-pack layout at apply. Equal
	// counts cannot strictly contain (a full both-side pairing means equal
	// totals), so they stay with the size tier below.
	if len(filteredSourceFiles) != len(filteredCandidateFiles) {
		if containment := sizeContainmentMatchType(filteredSourceFiles, filteredCandidateFiles); containment != "" {
			if s.metrics != nil {
				s.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: containment, Reason: ""}
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

// sizeContainmentMatchType reports a size-only containment between the usable
// file lists: "size-partial-in-pack" when every source file pairs 1:1 by exact
// size into the candidate files, "size-partial-contains" for the reverse.
// Runs only after every name-aware tier failed. Ambiguous same-size pairs stay
// unmatched (the pairing takes a size-only pair only when it is the sole
// candidate), so size collisions reject instead of guessing; the recheck on
// apply is the final safety net.
func sizeContainmentMatchType(sourceFiles, candidateFiles []TorrentFile) string {
	if len(sourceFiles) == 0 || len(candidateFiles) == 0 {
		return ""
	}
	if sizeContainmentPairsAll(sourceFiles, candidateFiles) {
		return "size-partial-in-pack"
	}
	if sizeContainmentPairsAll(candidateFiles, sourceFiles) {
		return "size-partial-contains"
	}
	return ""
}

func sizeContainmentPairsAll(contained, container []TorrentFile) bool {
	if len(contained) > len(container) {
		return false
	}
	_, unmatched := matchSourceFilesToCandidates(toQbtTorrentFiles(contained), toQbtTorrentFiles(container))
	return len(unmatched) == 0
}

func toQbtTorrentFiles(files []TorrentFile) qbt.TorrentFiles {
	converted := make(qbt.TorrentFiles, 0, len(files))
	for _, f := range files {
		converted = append(converted, qbt.TorrentFile{Name: f.Name, Size: f.Size})
	}
	return converted
}

// getMatchType determines if files match for cross-seeding.
// Returns "exact" for perfect match, "partial" for season pack partial matches,
// "size" for total size match, or "" for no match.
// Uses streaming file comparison to reduce memory usage.
func (s *Service) getMatchType(sourceRelease, candidateRelease *rls.Release, sourceFiles, candidateFiles qbt.TorrentFiles) string {
	var timer *prometheus.Timer
	if s.metrics != nil {
		timer = prometheus.NewTimer(s.metrics.GetMatchTypeDuration)
		defer timer.ObserveDuration()
		s.metrics.GetMatchTypeCalls.Inc()
	}

	sourceLayout := classifyTorrentLayout(sourceFiles, s.stringNormalizer)
	candidateLayout := classifyTorrentLayout(candidateFiles, s.stringNormalizer)
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
		if !shouldIgnoreFile(sf.Name, s.stringNormalizer) {
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
		if !shouldIgnoreFile(cf.Name, s.stringNormalizer) {
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

	// Fill in missing bit depth from torrent.
	if enriched.BitDepth == "" && torrentRelease.BitDepth != "" {
		enriched.BitDepth = torrentRelease.BitDepth
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

// shouldIgnoreFile checks if a file should be ignored during matching.
// Uses hardcoded lists of extensions and path keywords to filter out scene
// metadata files, subtitles, samples, and other non-content files.
func shouldIgnoreFile(filename string, normalizer *stringutils.Normalizer[string, string]) bool {
	lower := normalizer.Normalize(filename)

	// Check extension matches
	for _, ext := range DefaultIgnoredExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	// Check path keyword matches (e.g., "sample", "proof", "extras")
	for _, keyword := range DefaultIgnoredPathKeywords {
		if strings.Contains(lower, keyword) {
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
