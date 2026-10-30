// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// matching.go groups all heuristics and helpers that decide whether two torrents
// describe the same underlying content.

// matcher carries only what the matching rules read. A rule that needs a
// client, a store, or the sync manager does not compile here (ADR 0008).
type matcher struct {
	releaseCache     *ReleaseCache
	stringNormalizer *stringutils.Normalizer[string, string]
	metrics          *ServiceMetrics
}

func (s *Service) matcher() matcher {
	return matcher{
		releaseCache:     s.releaseCache,
		stringNormalizer: s.stringNormalizer,
		metrics:          s.metrics,
	}
}

// normalizer falls back to the process-wide singleton instead of allocating a
// fresh normalizer. Every NewDefaultNormalizer() spins up a ttlcache whose
// startExpirations goroutine never terminates (the throwaway cache is never
// closed), and this runs once per release pair on the matching hot path.
func (m matcher) normalizer() *stringutils.Normalizer[string, string] {
	return cmp.Or(m.stringNormalizer, stringutils.DefaultNormalizer)
}

func normalizerForService(s *Service) *stringutils.Normalizer[string, string] {
	if s == nil {
		return stringutils.DefaultNormalizer
	}
	return s.matcher().normalizer()
}

func isTVRelease(r *rls.Release) bool {
	return r != nil && (r.Type == rls.Series || r.Type == rls.Episode || r.Series > 0 || r.Episode > 0)
}

// isTVEpisode returns true if the release is a TV episode, including anime-style
// absolute-numbered episodes that do not carry a season number.
func isTVEpisode(r *rls.Release) bool {
	return isTVRelease(r) && r.Episode > 0
}

// absoluteEpisode is the episode of a name with no season, such as "Show - 81".
// It is 0 when the name has a season or no episode.
func absoluteEpisode(r *rls.Release) int {
	if r.Series != 0 {
		return 0
	}
	return r.Episode
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
//
// Returns (reject=true, reason) if the pairing should be rejected, (false, "") otherwise.
func rejectSeasonPackFromEpisode(newR, existingR *rls.Release) (reject bool, reason string) {
	if isTVSeasonPack(newR) && isTVEpisode(existingR) {
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

// parseReleaseName is the matcher's only parser. A matcher with no release cache
// falls back to the process-wide default parser, as normalizer does (ADR 0008).
func (m matcher) parseReleaseName(name string) *rls.Release {
	return cmp.Or(m.releaseCache, releases.DefaultParser).Parse(name) //nolint:forbidigo // the one fallback to the default parser (ADR 0008)
}

// parseFileRelease removes RAR volume suffixes so .s01 does not become season 1.
// Torrent titles use parseReleaseName because a trailing .S01 can name a season.
func (m matcher) parseFileRelease(name string) *rls.Release {
	return m.parseReleaseName(rarVolumeSuffix.ReplaceAllString(name, ""))
}

// String serializes the releaseKey into a stable string for caching purposes.
func (k releaseKey) String() string {
	return fmt.Sprintf("%d|%d|%d|%d|%d", k.series, k.episode, k.year, k.month, k.day)
}

// compareReleases runs the release rules on a pair and records each field
// that differs. It returns after a title difference, because no rule relaxes
// one over the rest of the set: title rescue runs its own compare. With
// stopAtFirst it returns after the first group of rules that finds a
// difference, which is enough for a caller that reads only the first reason.
func (m matcher) compareReleases(sourceSide, candidateSide namedRelease, sourceTitles, candidateTitles []string, findIndividualEpisodes, stopAtFirst bool) releaseComparison {
	var comparison releaseComparison
	if sourceSide.release == candidateSide.release {
		return comparison
	}
	// rls reads a bare "Title III" as a movie, so only the other side can say that its numeral is a season.
	source, candidate := withTitleNumeralSeason(sourceSide.release, candidateSide.release), withTitleNumeralSeason(candidateSide.release, sourceSide.release)
	sourceSide.release, candidateSide.release = source, candidate

	if m.compareTitles(&comparison, source, candidate, sourceSide.rawName, candidateSide.rawName, sourceTitles, candidateTitles); !comparison.matched() {
		return comparison
	}
	return m.compareExceptTitle(comparison, sourceSide, candidateSide, findIndividualEpisodes, stopAtFirst, false)
}

// compareExceptTitle runs every release rule after the title rule, in the
// order of the strict compare.
func (m matcher) compareExceptTitle(comparison releaseComparison, sourceSide, candidateSide namedRelease, findIndividualEpisodes, stopAtFirst, tolerateMissingCandidateTags bool) releaseComparison {
	source, candidate := sourceSide.release, candidateSide.release
	isTV := isTVRelease(source) || isTVRelease(candidate)
	stop := func() bool { return stopAtFirst && !comparison.matched() }

	if m.compareArtistAndDates(&comparison, source, candidate, isTV); stop() {
		return comparison
	}
	if compareTVStructure(&comparison, source, candidate, findIndividualEpisodes, isTV); stop() {
		return comparison
	}
	m.compareGroupSiteAndChecksum(&comparison, sourceSide, candidateSide)
	if tolerateMissingCandidateTags {
		comparison.differences = comparison.differences.without(differenceGroupMissing)
	}
	if stop() {
		return comparison
	}
	if m.compareFormatAndCodec(&comparison, source, candidate); stop() {
		return comparison
	}
	if m.compareMetadataFlags(&comparison, source, candidate); stop() {
		return comparison
	}
	compareReleaseVariants(&comparison, source, candidate)
	return comparison
}

// titleMismatchReason is the rejection reason emitted when two releases differ
// only by title. The title-rescue gates key off this exact value, so it is a
// named constant rather than an inline literal.
const titleMismatchReason = "title mismatch"

// These are rejections the exact-size fallback carries into apply. The
// structural and group reasons force a hash check before the add seeds. Callers
// key off these exact values.
const (
	seasonMismatchReason   = "season mismatch"
	episodeMismatchReason  = "episode mismatch"
	groupMismatchReason    = "group mismatch"
	checksumMismatchReason = "checksum mismatch"
)

func (m matcher) compareTitles(comparison *releaseComparison, source, candidate *rls.Release, sourceName, candidateName string, sourceExtraTitles, candidateExtraTitles []string) {
	// Title should match closely but not necessarily exactly.
	// Use punctuation-stripping normalization to handle differences like
	// "Bob's Burgers" vs "Bobs.Burgers" (apostrophes lost in dot notation).
	sourceTitles := m.normalizedReleaseTitles(source, sourceName)
	candidateTitles := m.normalizedReleaseTitles(candidate, candidateName)
	switch {
	case len(sourceTitles) == 0 || len(candidateTitles) == 0:
		comparison.add(differenceEmptyTitle)
	case !titleSetsMatch(sourceTitles, candidateTitles, sourceExtraTitles, candidateExtraTitles):
		// Title mismatches are expected for most candidates - don't log to avoid noise
		comparison.add(differenceTitle)
	}
}

// titleSetsMatch reports whether two sets of normalized release titles name the
// same work. The extra titles are ARR aliases of the other side.
func titleSetsMatch(sourceTitles, candidateTitles map[string]struct{}, sourceExtraTitles, candidateExtraTitles []string) bool {
	// Accept any overlap between normalized title sets. Each set contains complete
	// normalized title entries from Title, Alt, and parsed AKA parts, so legitimate
	// alternate titles can match without requiring strict equality of one parsed title.
	//
	// This still avoids false positives between related-but-distinct TV franchises
	// (e.g. "FBI" vs "FBI Most Wanted") because overlap is checked on full normalized
	// title entries, not arbitrary substrings.
	//
	// Extra titles (ARR aliases) are loop-invariant across a scan while this
	// function runs once per scanned torrent, and Sonarr alias lists are uncapped.
	// Probe them against the other side's set instead of inserting them, so the
	// per-pair maps stay small and never regrow per torrent.
	return normalizedTitleSetsOverlap(sourceTitles, candidateTitles) ||
		normalizedTitleSetContainsAny(sourceTitles, candidateExtraTitles) ||
		normalizedTitleSetContainsAny(candidateTitles, sourceExtraTitles)
}

func (m matcher) compareArtistAndDates(comparison *releaseComparison, source, candidate *rls.Release, isTV bool) {
	normalizer := m.normalizer()

	// Artist must match for content with artist metadata (music, 0day scene radio shows, etc.)
	// This prevents matching different artists with the same show/album title.
	if source.Artist != "" && candidate.Artist != "" {
		sourceArtist := normalizer.Normalize(source.Artist)
		candidateArtist := normalizer.Normalize(candidate.Artist)
		if sourceArtist != candidateArtist {
			comparison.add(differenceArtist)
		}
	}

	// Year should match if both are present.
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		comparison.add(differenceYear)
	}

	// For date-based releases (0day scene), require exact date match including month and day.
	// This prevents matching releases from different dates within the same year.
	if source.Year > 0 && source.Month > 0 && source.Day > 0 &&
		candidate.Year > 0 && candidate.Month > 0 && candidate.Day > 0 {
		if source.Month != candidate.Month || source.Day != candidate.Day {
			comparison.add(differenceDate)
		}
	}

	// For non-TV content where rls has inferred a concrete content type (movie, music,
	// audiobook, etc.), require the types to match. This prevents, for example,
	// music releases from matching audiobooks with similar titles.
	if !isTV && source.Type != 0 && candidate.Type != 0 && source.Type != candidate.Type {
		comparison.add(differenceContentType)
	}
}

// releasesMatchExceptTitleWithReason keeps every normal release rule except title.
// Retitled listings are usually bare-file re-uploads that also drop the -GROUP
// tag, so an absent candidate group is tolerated here; conflicting tags still
// reject. The Match verdict pairs this with an exact-size gate, because
// with the title ignored a sparsely parsed name can leave nothing else to
// reject on. A path that adds data gets the paused-add full recheck as the
// final authority; the batch matcher only reports existing pairings and pairs
// their file sizes or links.
func (m matcher) releasesMatchExceptTitleWithReason(sourceSide, candidateSide namedRelease, findIndividualEpisodes bool) (bool, string) {
	comparison := m.compareExceptTitle(releaseComparison{}, sourceSide, candidateSide, findIndividualEpisodes, true, true)
	return comparison.matched(), comparison.reason()
}

func (m matcher) normalizedReleaseTitles(release *rls.Release, rawName string) map[string]struct{} {
	titles := make(map[string]struct{})
	var buf [8]string
	for _, title := range m.appendReleaseTitles(buf[:0], release, rawName) {
		addNormalizedTitle(titles, title)
	}
	addNormalizedTitle(titles, titleWithoutSeasonNumeral(release))
	return titles
}

// appendReleaseTitles appends the titles that the matcher reads from a name:
// the main title first, then the alternate title, each AKA part, and each side
// of a slash title. An entry can be "". The title without the Season numeral
// is not one of them, because it depends on the season of the other side.
func (m matcher) appendReleaseTitles(titles []string, release *rls.Release, rawName string) []string {
	titles = append(titles, releaseTitle(release), releaseAlt(release))

	// Cached parse: this runs once per library torrent per search.
	for _, rawTitle := range m.rawAKATitleParts(rawName) {
		parsed := m.parseReleaseName(rawTitle)
		titles = append(titles, parsed.Title, parsed.Alt)
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
		parsed := m.parseReleaseName(unslashed)
		if strings.HasPrefix(parsed.Title, releaseTitle(release)) {
			titles = append(titles, parsed.Title, parsed.Alt)
		}
	}

	return titles
}

// normalizedTitleSetContainsAny reports whether any extra title normalizes to
// an entry of the set. It never mutates the set.
func normalizedTitleSetContainsAny(titles map[string]struct{}, extraTitles []string) bool {
	for _, title := range extraTitles {
		if _, exists := titles[stringutils.NormalizeForMatching(title)]; exists {
			return true
		}
	}
	return false
}

func releaseTitle(release *rls.Release) string {
	if release == nil {
		return ""
	}
	return release.Title
}

// titleWithoutSeasonNumeral drops a last-word Roman numeral that equals the
// season marker, so "Kaiju Squad 100 III S03" also reads as the
// "Kaiju Squad 100" a tracker lists as S03. Any value counts, because the
// numeral and the marker agree. Returns "" when no numeral drops.
func titleWithoutSeasonNumeral(release *rls.Release) string {
	if release == nil || release.Series <= 0 || isYearBearingMovieRelease(release) {
		return ""
	}
	if season, title := readTitleNumeral(release.Title); season == release.Series {
		return title
	}
	return ""
}

// readTitleNumeral reads the last word of a title as a Roman numeral. It
// returns the numeral's value and the title without it, or 0 when the last
// word is no numeral.
func readTitleNumeral(title string) (int, string) {
	rest, last, ok := strings.CutLast(title, " ")
	// Runs per library torrent per search, so reject a word with a non-numeral letter before the numeral is parsed.
	if !ok || strings.Trim(last, "IVXLCDMivxlcdm") != "" {
		return 0, ""
	}
	value := 0
	for i := range len(last) {
		digit := romanDigitValue(last[i])
		if i+1 < len(last) && digit < romanDigitValue(last[i+1]) {
			value -= digit
		} else {
			value += digit
		}
	}
	// Only the canonical spelling counts, so "IIII" or "IC" stays part of the title.
	var canonical [16]byte
	if !strings.EqualFold(last, string(appendRomanNumeral(canonical[:0], value))) {
		return 0, ""
	}
	return value, strings.TrimSpace(rest)
}

func romanDigitValue(symbol byte) int {
	return [...]int{1, 5, 10, 50, 100, 500, 1000}[strings.IndexByte("ivxlcdm", symbol|0x20)]
}

func appendRomanNumeral(dst []byte, n int) []byte {
	for _, r := range []struct {
		value  int
		symbol string
	}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for ; n >= r.value; n -= r.value {
			dst = append(dst, r.symbol...)
		}
	}
	return dst
}

func releaseAlt(release *rls.Release) string {
	if release == nil {
		return ""
	}
	return release.Alt
}

func (m matcher) rawAKATitleParts(rawName string) []string {
	if rawName == "" || !strings.Contains(rawName, " AKA ") {
		return nil
	}

	const minAKATitleLength = 4
	parts := strings.Split(rawName, " AKA ")
	titles := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < minAKATitleLength {
			continue
		}
		titles = append(titles, part)
		// An AKA after the season or episode starts an episode title, not another show title.
		if parsed := m.parseReleaseName(part); parsed.Series > 0 || parsed.Episode > 0 {
			break
		}
	}
	if len(titles) < 2 {
		return nil
	}
	return titles
}

func addNormalizedTitle(titles map[string]struct{}, title string) {
	// NormalizeForMatching does a cache lookup even for "", and most releases have no Alt.
	if title == "" {
		return
	}
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

func compareTVStructure(comparison *releaseComparison, source, candidate *rls.Release, findIndividualEpisodes, isTV bool) {
	if !isTV {
		return
	}

	// For TV shows, season and episode structure must match based on settings.
	sourceIsTV := isTVRelease(source)
	candidateIsTV := isTVRelease(candidate)
	sourceIsPack := isTVSeasonPack(source)
	candidateIsPack := isTVSeasonPack(candidate)

	if sourceIsTV && !candidateIsTV {
		comparison.add(differenceCandidateNotTV)
	}
	if candidateIsTV && !sourceIsTV {
		comparison.add(differenceSourceNotTV)
	}

	if source.Series > 0 && candidate.Series > 0 && source.Series != candidate.Series {
		comparison.add(differenceSeason)
	}

	// Strict matching: season packs only match season packs. Flexible matching
	// lets a season pack match an individual episode.
	if !findIndividualEpisodes && sourceIsPack != candidateIsPack {
		comparison.add(differencePackVersusEpisode)
	}

	// Individual episodes always need exact episode matching.
	if !sourceIsPack && !candidateIsPack && source.Episode != candidate.Episode {
		comparison.add(differenceEpisode)
	}
}

func (m matcher) compareGroupSiteAndChecksum(comparison *releaseComparison, sourceSide, candidateSide namedRelease) {
	source, candidate := sourceSide.release, candidateSide.release
	// Group tags should match for proper cross-seeding compatibility.
	// Different release groups often have different encoding settings and file structures.
	normalizer := m.normalizer()
	sourceGroup := normalizer.Normalize((source.Group))
	candidateGroup := normalizer.Normalize((candidate.Group))
	sourceSite := normalizer.Normalize(source.Site)
	candidateSite := normalizer.Normalize(candidate.Site)
	spaceJoined := m.spaceJoinedGroupSite(source, candidate)

	// A hyphenated fansub name reads as Site in its bracket form and as a split
	// Group in its dash-suffix form. Compare the rebuilt suffix name instead.
	switch {
	case m.dashSuffixGroupSpellsSite(sourceSide, candidateSide):
		candidateGroup = sourceSite
	case m.dashSuffixGroupSpellsSite(candidateSide, sourceSide):
		sourceGroup = candidateSite
	}

	// Only enforce group matching if the source has a group tag
	if sourceGroup != "" && !spaceJoined {
		candidateGroupIdentity := candidateGroup
		if candidateGroupIdentity == "" {
			candidateGroupIdentity = candidateSite
		}
		switch {
		case candidateGroupIdentity == "":
			comparison.add(differenceGroupMissing)
		case sourceGroup != candidateGroupIdentity:
			comparison.add(differenceGroup)
		}
	}
	// If source has no group, we don't care about candidate's group

	// Site field is used by anime releases where group is in brackets like [SubsPlease].
	// rls parses these as Site rather than Group. Different fansub groups can never
	// cross-seed, but many indexer titles omit the site tag entirely. Treat mismatched
	// non-empty site tags as incompatible, but don't reject candidates that simply
	// lack this metadata.
	if sourceSite != "" && !spaceJoined {
		candidateSiteIdentity := candidateSite
		if candidateSiteIdentity == "" {
			candidateSiteIdentity = candidateGroup
		}
		if candidateSiteIdentity != "" && sourceSite != candidateSiteIdentity {
			comparison.add(differenceSite)
		}
	}

	// Sum field contains the CRC32 checksum for anime releases like [32ECE75A].
	// Different checksums mean different files with 100% certainty. Indexer
	// titles and announces often drop the tag, so a tag on one name only is
	// no difference.
	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if sourceSum != "" && candidateSum != "" && sourceSum != candidateSum {
		comparison.add(differenceChecksum)
	}
}

// spaceJoinedGroupSite reports whether one name writes the group and the
// collective as "- Kyo Mura)" and the other as "- Kyo) [Mura]". rls reads the
// first as one group with a space, so only the other name shows the split.
func (m matcher) spaceJoinedGroupSite(source, candidate *rls.Release) bool {
	normalizer := m.normalizer()
	joined := func(split, whole *rls.Release) bool {
		// Most names carry no Site, so skip the normalizer lookups on this hot path.
		if split.Site == "" || whole.Site != "" {
			return false
		}
		group := normalizer.Normalize(split.Group)
		site := normalizer.Normalize(split.Site)
		return group != "" && group != site &&
			normalizer.Normalize(whole.Group) == group+" "+site
	}
	return joined(source, candidate) || joined(candidate, source)
}

// sourceMismatchReason is the rejection reason for an incompatible source, such
// as WEBRip against WEB-DL.
const sourceMismatchReason = "source mismatch"

func (m matcher) compareFormatAndCodec(comparison *releaseComparison, source, candidate *rls.Release) {
	normalizer := m.normalizer()

	// Source must be compatible if both are present.
	// WEB is ambiguous and matches both WEB-DL and WEBRip.
	// WEB-DL and WEBRip are explicitly different and do not match.
	// Other sources (BluRay, HDTV, etc.) must match exactly.
	sourceSource := normalizeSource(source.Source)
	candidateSource := normalizeSource(candidate.Source)
	if !sourcesCompatible(sourceSource, candidateSource) {
		comparison.add(differenceSource)
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
			comparison.add(differenceResolution)
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
		comparison.add(differenceCollection)
	}

	// Codec must match if both are present (AVC vs HEVC produce different files).
	// Uses codec aliasing so x264/H.264/H264/AVC are treated as equivalent.
	if len(source.Codec) > 0 && len(candidate.Codec) > 0 {
		sourceCodec := joinNormalizedCodecSlice(source.Codec)
		candidateCodec := joinNormalizedCodecSlice(candidate.Codec)
		if sourceCodec != candidateCodec {
			comparison.add(differenceCodec)
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
		comparison.add(differenceHDR)
	}

	// Bit depth should match when both are present (8-bit vs 10-bit are different encodes).
	// We intentionally don't enforce "either present" here since indexer titles often omit it.
	sourceBitDepth := normalizer.Normalize(source.BitDepth)
	candidateBitDepth := normalizer.Normalize(candidate.BitDepth)
	if sourceBitDepth != "" && candidateBitDepth != "" && sourceBitDepth != candidateBitDepth {
		comparison.add(differenceBitDepth)
	}
}

func (m matcher) compareMetadataFlags(comparison *releaseComparison, source, candidate *rls.Release) {
	normalizer := m.normalizer()

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
			comparison.add(differenceCut)
		}
	}

	// Edition must match if both are present (Remastered vs Original are different)
	if len(source.Edition) > 0 && len(candidate.Edition) > 0 {
		sourceEdition := joinNormalizedSlice(source.Edition)
		candidateEdition := joinNormalizedSlice(candidate.Edition)
		if sourceEdition != candidateEdition {
			comparison.add(differenceEdition)
		}
	}

	// Language must match if both are present (FRENCH vs ENGLISH are different audio/subs).
	// A missing tag means unknown, not English: trackers like Aither label the original
	// audio language (JAPANESE, KOREAN) on releases that other trackers publish untagged,
	// so assuming ENGLISH rejected every anime cross-seed. Genuine dub mismatches are
	// caught downstream by the exact per-file size comparison before anything is injected.
	if len(source.Language) > 0 && len(candidate.Language) > 0 {
		if joinNormalizedSlice(source.Language) != joinNormalizedSlice(candidate.Language) {
			comparison.add(differenceLanguage)
		}
	}

	// Version must match if both are present (v2 often has different files than v1)
	sourceVersion := normalizer.Normalize(source.Version)
	candidateVersion := normalizer.Normalize(candidate.Version)
	if sourceVersion != "" && candidateVersion != "" && sourceVersion != candidateVersion {
		comparison.add(differenceVersion)
	}

	// Disc must match if both are present (Disc1 vs Disc2 are different content)
	sourceDisc := normalizer.Normalize(source.Disc)
	candidateDisc := normalizer.Normalize(candidate.Disc)
	if sourceDisc != "" && candidateDisc != "" && sourceDisc != candidateDisc {
		comparison.add(differenceDisc)
	}

	// Platform must match if both are present (Windows vs macOS are different binaries)
	sourcePlatform := normalizer.Normalize(source.Platform)
	candidatePlatform := normalizer.Normalize(candidate.Platform)
	if sourcePlatform != "" && candidatePlatform != "" && sourcePlatform != candidatePlatform {
		comparison.add(differencePlatform)
	}

	// Architecture must match if both are present (x64 vs x86 are different binaries)
	sourceArch := normalizer.Normalize(source.Arch)
	candidateArch := normalizer.Normalize(candidate.Arch)
	if sourceArch != "" && candidateArch != "" && sourceArch != candidateArch {
		comparison.add(differenceArchitecture)
	}
}

func compareReleaseVariants(comparison *releaseComparison, source, candidate *rls.Release) {
	// Certain variant tags must match for safe cross-seeding.
	// IMAX/HYBRID always require exact match (different video masters).
	// REPACK/PROPER require exact match for non-pack content, but season packs
	// are exempt since a pack might contain a REPACK of just one episode.
	if compatible, reason := checkVariantsCompatible(source, candidate); !compatible {
		comparison.add(differenceVariant)
		comparison.variantReason = cmp.Or(reason, "variant mismatch")
	}
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

// joinNormalizedHDRSlice reads a generic HDR tag as HDR10 (see Generic HDR tag in GLOSSARY.md).
func joinNormalizedHDRSlice(slice []string) string {
	tags := make([]string, len(slice))
	for i, tag := range slice {
		// The parser stores canonical tags, so an exact compare is enough.
		if tag == "HDR" {
			tag = "HDR10"
		}
		tags[i] = tag
	}
	return strings.Join(releases.NormalizeHDRTags(tags), " ")
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
func (m matcher) getMatchTypeFromTitle(targetRelease, candidateRelease *rls.Release, candidateFiles qbt.TorrentFiles) string {
	// Build candidate release keys from actual files with enrichment.
	candidateReleases := make(map[releaseKey]int64)
	seasonlessEpisodes := false
	for _, cf := range candidateFiles {
		if !shouldIgnoreFile(cf.Name, m.normalizer()) {
			fileRelease := m.parseFileRelease(cf.Name)
			seasonlessEpisodes = seasonlessEpisodes || (fileRelease.Series == 0 && fileRelease.Episode > 0)
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
		// something from their names. Here the key holds at most a year, and a
		// missing year agrees with any year, as in the torrent-level match.
		targetYear := makeReleaseKey(targetRelease).year
		for key := range candidateReleases {
			if key.series == 0 && key.month == 0 && (targetYear == 0 || key.year == targetYear) {
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
	// A fansub batch without a season ("[Group] Show - 01 (BD 1080p).mkv")
	// counts as season 1.
	candidateSeries := candidateRelease.Series
	if candidateSeries == 0 && seasonlessEpisodes {
		candidateSeries = 1
	}
	if targetRelease.Series > 0 && candidateSeries == targetRelease.Series &&
		!hasEpisodeLevelKeys(candidateReleases) {
		return "release-match"
	}

	// Fallback: the files give no release keys, as a seasonless anime episode
	// ("Show - 1150") does, but the titles match. Accept the same absolute episode.
	if len(candidateReleases) == 0 {
		targetTitle := stringutils.NormalizeForMatching(targetRelease.Title)
		candidateTitle := stringutils.NormalizeForMatching(candidateRelease.Title)
		if targetTitle != "" && targetTitle == candidateTitle {
			targetEp := absoluteEpisode(targetRelease)
			candidateEp := absoluteEpisode(candidateRelease)

			if targetEp > 0 && targetEp == candidateEp {
				log.Debug().
					Str("title", targetRelease.Title).
					Int("episode", targetEp).
					Msg("Falling back to title+episode candidate match")
				return "partial-in-pack"
			}

			// For content without usable file-level metadata (games, apps, scene releases
			// with RAR files), trust the torrent-level Match verdict that got us here
			// when neither name has an episode. A batch such as "Show - 01 - 03" has no
			// single episode, so two batches must name the same episodes.
			// A year alone does not block: the torrent-level match compared it.
			targetKey := makeReleaseKey(targetRelease)
			if targetKey.series == 0 && targetKey.month == 0 && targetEp == 0 && candidateEp == 0 &&
				slices.EqualFunc(targetRelease.SeriesEpisodes(), candidateRelease.SeriesEpisodes(), slices.Equal[[]int]) {
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
// The partial types name the new torrent: "partial-in-pack" means the new files
// sit in the local pack, and "partial-contains" means the new files contain the
// local files. The "size-" types mean the same, paired by size alone.
type MatchResult struct {
	MatchType string // "exact", "partial-in-pack", "partial-contains", "size", "size-partial-in-pack", "size-partial-contains", or ""
	Reason    string // Human-readable reason when MatchType is "" (no match)
}

// getMatchTypeWithReason determines if files match for cross-seeding and provides
// a detailed reason when they don't match. localFiles belong to the torrent that
// qui already has, and newFiles to the torrent that qui would add.
// tolerancePercent specifies the maximum size difference percentage for size matching (default 5%).
func (m matcher) getMatchTypeWithReason(localRelease, newRelease *rls.Release, localFiles, newFiles qbt.TorrentFiles, tolerancePercent float64) MatchResult {
	var timer *prometheus.Timer
	if m.metrics != nil {
		timer = prometheus.NewTimer(m.metrics.GetMatchTypeDuration)
		defer timer.ObserveDuration()
		m.metrics.GetMatchTypeCalls.Inc()
	}

	normalizer := m.normalizer()

	// Check layout compatibility first (RAR vs extracted files)
	localLayout := classifyTorrentLayout(localFiles, normalizer)
	newLayout := classifyTorrentLayout(newFiles, normalizer)
	if localLayout != LayoutUnknown && newLayout != LayoutUnknown && localLayout != newLayout {
		if m.metrics != nil {
			m.metrics.GetMatchTypeNoMatch.Inc()
		}
		reason := fmt.Sprintf("Layout mismatch: source is %s, candidate is %s", layoutDescription(localLayout), layoutDescription(newLayout))
		return MatchResult{MatchType: "", Reason: reason}
	}

	// Stream through files to build filtered lists and accumulate sizes
	var (
		filteredLocalFiles []TorrentFile
		filteredNewFiles   []TorrentFile
		totalLocalSize     int64
		totalNewSize       int64
		localReleaseKeys   = make(map[releaseKey]int64)
		newReleaseKeys     = make(map[releaseKey]int64)
	)

	// Process local files
	for _, lf := range localFiles {
		if !shouldIgnoreFile(lf.Name, normalizer) {
			filteredLocalFiles = append(filteredLocalFiles, TorrentFile{
				Name: lf.Name,
				Size: lf.Size,
			})
			totalLocalSize += lf.Size

			fileRelease := m.parseFileRelease(lf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, localRelease)
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				if existingSize, exists := localReleaseKeys[key]; !exists || lf.Size > existingSize {
					localReleaseKeys[key] = lf.Size
				}
			}
		}
	}

	// Process new files
	for _, nf := range newFiles {
		if !shouldIgnoreFile(nf.Name, normalizer) {
			filteredNewFiles = append(filteredNewFiles, TorrentFile{
				Name: nf.Name,
				Size: nf.Size,
			})
			totalNewSize += nf.Size

			fileRelease := m.parseFileRelease(nf.Name)
			enrichedRelease := enrichReleaseFromTorrent(fileRelease, newRelease)
			key := makeReleaseKey(enrichedRelease)
			if key != (releaseKey{}) {
				if existingSize, exists := newReleaseKeys[key]; !exists || nf.Size > existingSize {
					newReleaseKeys[key] = nf.Size
				}
			}
		}
	}

	// Check for exact file match
	if m.streamingExactMatch(filteredLocalFiles, filteredNewFiles) {
		if m.metrics != nil {
			m.metrics.GetMatchTypeExactMatch.Inc()
		}
		return MatchResult{MatchType: "exact", Reason: ""}
	}

	// Check for partial match
	if len(localReleaseKeys) > 0 && len(newReleaseKeys) > 0 {
		if m.checkPartialMatch(localReleaseKeys, newReleaseKeys) {
			if m.metrics != nil {
				m.metrics.GetMatchTypePartialMatch.Inc()
			}
			return MatchResult{MatchType: "partial-contains", Reason: ""}
		}

		if m.checkPartialMatch(newReleaseKeys, localReleaseKeys) {
			if m.metrics != nil {
				m.metrics.GetMatchTypePartialMatch.Inc()
			}
			return MatchResult{MatchType: "partial-in-pack", Reason: ""}
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
	if len(filteredLocalFiles) != len(filteredNewFiles) {
		if containment := sizeContainmentMatchType(filteredLocalFiles, filteredNewFiles); containment != "" {
			if m.metrics != nil {
				m.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: containment, Reason: ""}
		}
	}

	// Size match with tolerance
	if totalLocalSize > 0 && len(filteredLocalFiles) > 0 {
		if m.isSizeWithinTolerance(totalLocalSize, totalNewSize, tolerancePercent) {
			if m.metrics != nil {
				m.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: "size", Reason: ""}
		}
	}

	// Fallback to largest file match
	if len(localReleaseKeys) == 0 && len(newReleaseKeys) == 0 &&
		len(filteredLocalFiles) > 0 && len(filteredNewFiles) > 0 {
		if m.streamingLargestFileMatch(filteredLocalFiles, filteredNewFiles) {
			if m.metrics != nil {
				m.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: "size", Reason: ""}
		}
	}

	// Build detailed reason for no match
	if m.metrics != nil {
		m.metrics.GetMatchTypeNoMatch.Inc()
	}

	reason := buildNoMatchReason(
		filteredLocalFiles, filteredNewFiles,
		totalLocalSize, totalNewSize,
		localReleaseKeys, newReleaseKeys,
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
	localFiles, newFiles []TorrentFile,
	localSize, newSize int64,
	localKeys, newKeys map[releaseKey]int64,
	tolerancePercent float64,
) string {
	if len(localFiles) == 0 {
		return "No usable files in source torrent after filtering"
	}
	if len(newFiles) == 0 {
		return "No usable files in existing torrent after filtering"
	}

	// Size mismatch - calculate actual difference percentage
	if localSize != newSize {
		var diffPercent float64
		if localSize > 0 {
			diff := localSize - newSize
			if diff < 0 {
				diff = -diff
			}
			diffPercent = (float64(diff) / float64(localSize)) * 100
		}
		return fmt.Sprintf("Size mismatch: source %.2f GB vs existing %.2f GB (%.2f%% difference, tolerance %.1f%%)",
			float64(localSize)/(1024*1024*1024),
			float64(newSize)/(1024*1024*1024),
			diffPercent,
			tolerancePercent)
	}

	// File count mismatch with same size (rare but possible)
	if len(localFiles) != len(newFiles) {
		return fmt.Sprintf("File count mismatch: source has %d files, existing has %d files",
			len(localFiles), len(newFiles))
	}

	// Release keys couldn't be parsed
	if len(localKeys) == 0 && len(newKeys) == 0 {
		return "Unable to parse release metadata from filenames"
	}

	// Keys don't overlap
	if len(localKeys) > 0 && len(newKeys) > 0 {
		return "Release metadata doesn't match between source and existing files"
	}

	return "Files don't match (structure or naming differs)"
}

// sizeContainmentMatchType reports a size-only containment between the usable
// file lists: "size-partial-in-pack" when every new file pairs 1:1 by exact
// size into the local files, "size-partial-contains" for the reverse.
// Runs only after every name-aware tier failed. Ambiguous same-size pairs stay
// unmatched (the pairing takes a size-only pair only when it is the sole
// candidate), so size collisions reject instead of guessing; the recheck on
// apply is the final safety net.
func sizeContainmentMatchType(localFiles, newFiles []TorrentFile) string {
	if len(localFiles) == 0 || len(newFiles) == 0 {
		return ""
	}
	if sizeContainmentPairsAll(localFiles, newFiles) {
		return "size-partial-contains"
	}
	if sizeContainmentPairsAll(newFiles, localFiles) {
		return "size-partial-in-pack"
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

// streamingExactMatch checks if two file lists have exactly matching paths and sizes.
// Uses streaming comparison to avoid storing all files in memory.
func (m matcher) streamingExactMatch(sourceFiles, candidateFiles []TorrentFile) bool {
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
func (m matcher) streamingLargestFileMatch(sourceFiles, candidateFiles []TorrentFile) bool {
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

// shouldIgnoreFile reports whether a file is a sidecar, a sample, or a bonus
// video. Real titles begin with keywords ("Trailer.Park.Boys.S01E01"), so a
// keyword at the start of a release name is a title word.
func shouldIgnoreFile(filename string, normalizer *stringutils.Normalizer[string, string]) bool {
	lower := normalizer.Normalize(filename)
	if slices.Contains(DefaultIgnoredExtensions, path.Ext(lower)) {
		return true
	}

	dir, base := path.Split(lower)
	for segment := range strings.SplitSeq(dir, "/") {
		if slices.Contains(DefaultIgnoredPathKeywords, segment) || slices.Contains(DefaultIgnoredPathKeywords, strings.TrimSuffix(segment, "s")) ||
			slices.Contains(DefaultIgnoredPathKeywords, strings.TrimSuffix(segment, "es")) {
			return true
		}
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	for _, keyword := range DefaultIgnoredPathKeywords {
		if after, ok := strings.CutPrefix(stem, keyword); ok && (strings.TrimLeft(after, "0123456789") == "" || after[0] == '-') {
			return true
		}
		if before, ok := strings.CutSuffix(stem, keyword); ok && strings.IndexByte("-._ ", before[len(before)-1]) >= 0 {
			return true
		}
	}
	return false
}

// checkPartialMatch checks if subset files are contained in superset files.
// Returns true if all subset files have matching release keys and sizes in superset.
func (m matcher) checkPartialMatch(subset, superset map[releaseKey]int64) bool {
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

// isSizeWithinTolerance checks if two torrent sizes are within the specified tolerance percentage.
// A tolerance of 5.0 means the candidate size can be ±5% of the source size.
func (m matcher) isSizeWithinTolerance(sourceSize, candidateSize int64, tolerancePercent float64) bool {
	if sourceSize == 0 || candidateSize == 0 {
		return sourceSize == candidateSize // Both must be zero to match
	}

	if tolerancePercent < 0 {
		tolerancePercent = 0 // Negative tolerance doesn't make sense
	}

	// If tolerance is 0, require exact match
	if tolerancePercent == 0 {
		return sourceSize == candidateSize
	}

	// Calculate acceptable size range
	tolerance := float64(sourceSize) * (tolerancePercent / 100.0)
	minAcceptableSize := float64(sourceSize) - tolerance
	maxAcceptableSize := float64(sourceSize) + tolerance

	candidateSizeFloat := float64(candidateSize)
	return candidateSizeFloat >= minAcceptableSize && candidateSizeFloat <= maxAcceptableSize
}
