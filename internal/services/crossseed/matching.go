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

func isTVEpisode(r *rls.Release) bool {
	return isTVRelease(r) && r.Episode > 0
}

func absoluteEpisode(r *rls.Release) int {
	if r.Series != 0 {
		return 0
	}
	return r.Episode
}

func isTVSeasonPack(r *rls.Release) bool {
	return isTVRelease(r) && (r.Series > 0 || r.Type == rls.Series) && r.Episode == 0
}

const rejectReasonSeasonPackFromEpisode = "Season packs cannot be cross-seeded against single-episode torrents"

func rejectSeasonPackFromEpisode(newR, existingR *rls.Release) (reject bool, reason string) {
	if isTVSeasonPack(newR) && isTVEpisode(existingR) {
		return true, rejectReasonSeasonPackFromEpisode
	}
	return false, ""
}

type releaseKey struct {
	series  int
	episode int

	year  int
	month int
	day   int
}

func makeReleaseKey(r *rls.Release) releaseKey {
	if r.Series > 0 && r.Episode > 0 {
		return releaseKey{
			series:  r.Series,
			episode: r.Episode,
		}
	}

	if r.Series > 0 {
		return releaseKey{
			series: r.Series,
		}
	}

	if r.Year > 0 && r.Month > 0 && r.Day > 0 {
		return releaseKey{
			year:  r.Year,
			month: r.Month,
			day:   r.Day,
		}
	}

	if r.Year > 0 {
		return releaseKey{
			year: r.Year,
		}
	}

	return releaseKey{}
}

func (m matcher) parseReleaseName(name string) *rls.Release {
	return cmp.Or(m.releaseCache, releases.DefaultParser).Parse(name) //nolint:forbidigo // the one fallback to the default parser (ADR 0008)
}

func (m matcher) parseFileRelease(name string) *rls.Release {
	return m.parseReleaseName(rarVolumeSuffix.ReplaceAllString(name, ""))
}

func (k releaseKey) String() string {
	return fmt.Sprintf("%d|%d|%d|%d|%d", k.series, k.episode, k.year, k.month, k.day)
}

func (m matcher) compareReleases(sourceSide, candidateSide namedRelease, sourceTitles, candidateTitles []string, findIndividualEpisodes, stopAtFirst bool) releaseComparison {
	var comparison releaseComparison
	if sourceSide.release == candidateSide.release {
		return comparison
	}
	source, candidate := withTitleNumeralSeason(sourceSide.release, candidateSide.release), withTitleNumeralSeason(candidateSide.release, sourceSide.release)
	sourceSide.release, candidateSide.release = source, candidate

	if m.compareTitles(&comparison, source, candidate, sourceSide.rawName, candidateSide.rawName, sourceTitles, candidateTitles); !comparison.matched() {
		return comparison
	}
	return m.compareExceptTitle(comparison, sourceSide, candidateSide, findIndividualEpisodes, stopAtFirst, false)
}

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

const titleMismatchReason = "title mismatch"

const (
	seasonMismatchReason   = "season mismatch"
	episodeMismatchReason  = "episode mismatch"
	groupMismatchReason    = "group mismatch"
	checksumMismatchReason = "checksum mismatch"
)

func (m matcher) compareTitles(comparison *releaseComparison, source, candidate *rls.Release, sourceName, candidateName string, sourceExtraTitles, candidateExtraTitles []string) {
	var srcBuf, candBuf [16]string
	sourceTitles := m.appendNormalizedReleaseTitles(srcBuf[:0], source, sourceName)
	candidateTitles := m.appendNormalizedReleaseTitles(candBuf[:0], candidate, candidateName)
	switch {
	case len(sourceTitles) == 0 || len(candidateTitles) == 0:
		comparison.add(differenceEmptyTitle)
	case !titleSetsMatch(sourceTitles, candidateTitles, sourceExtraTitles, candidateExtraTitles):
		comparison.add(differenceTitle)
	}
}

func titleSetsMatch(sourceTitles, candidateTitles, sourceExtraTitles, candidateExtraTitles []string) bool {
	return normalizedTitleSetsOverlap(sourceTitles, candidateTitles) ||
		normalizedTitleSetContainsAny(sourceTitles, candidateExtraTitles) ||
		normalizedTitleSetContainsAny(candidateTitles, sourceExtraTitles)
}

func (m matcher) compareArtistAndDates(comparison *releaseComparison, source, candidate *rls.Release, isTV bool) {
	normalizer := m.normalizer()

	if source.Artist != "" && candidate.Artist != "" {
		sourceArtist := normalizer.Normalize(source.Artist)
		candidateArtist := normalizer.Normalize(candidate.Artist)
		if sourceArtist != candidateArtist {
			comparison.add(differenceArtist)
		}
	}

	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		comparison.add(differenceYear)
	}

	if source.Year > 0 && source.Month > 0 && source.Day > 0 &&
		candidate.Year > 0 && candidate.Month > 0 && candidate.Day > 0 {
		if source.Month != candidate.Month || source.Day != candidate.Day {
			comparison.add(differenceDate)
		}
	}

	if !isTV && source.Type != 0 && candidate.Type != 0 && source.Type != candidate.Type {
		comparison.add(differenceContentType)
	}
}

func (m matcher) releasesMatchExceptTitleWithReason(sourceSide, candidateSide namedRelease, findIndividualEpisodes bool) (bool, string) {
	comparison := m.compareExceptTitle(releaseComparison{}, sourceSide, candidateSide, findIndividualEpisodes, true, true)
	return comparison.matched(), comparison.reason()
}

func (m matcher) appendNormalizedReleaseTitles(dst []string, release *rls.Release, rawName string) []string {
	var buf [8]string
	for _, title := range m.appendReleaseTitles(buf[:0], release, rawName) {
		dst = addNormalizedTitle(dst, title)
	}
	return addNormalizedTitle(dst, titleWithoutSeasonNumeral(release))
}

func (m matcher) appendReleaseTitles(titles []string, release *rls.Release, rawName string) []string {
	titles = appendTitleAndAlts(titles, release)

	for _, rawTitle := range m.rawAKATitleParts(rawName) {
		titles = appendTitleAndAlts(titles, m.parseReleaseName(rawTitle))
	}

	if unslashed := slashAsSeparator.Replace(rawName); unslashed != rawName {
		parsed := m.parseReleaseName(unslashed)
		if strings.HasPrefix(parsed.Title, slashAsSeparator.Replace(releaseTitle(release))) {
			titles = appendTitleAndAlts(titles, parsed)
		}
	}

	return titles
}

func normalizedTitleSetContainsAny(titles, extraTitles []string) bool {
	for _, title := range extraTitles {
		if slices.Contains(titles, stringutils.NormalizeForMatching(title)) {
			return true
		}
	}
	return false
}

var slashAsSeparator = strings.NewReplacer("/", " ", `\`, " ")

func sameServiceCollection(collection string) string {
	switch collection {
	case "atv":
		return "atvp"
	case "max":
		return "hmax"
	}
	return collection
}

func releaseTitle(release *rls.Release) string {
	if release == nil {
		return ""
	}
	return release.Title
}

func titleWithoutSeasonNumeral(release *rls.Release) string {
	if release == nil || release.Series <= 0 || isYearBearingMovieRelease(release) {
		return ""
	}
	if season, title := readTitleNumeral(release.Title); season == release.Series {
		return title
	}
	return ""
}

func readTitleNumeral(title string) (int, string) {
	rest, last, ok := strings.CutLast(title, " ")
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

func appendTitleAndAlts(titles []string, release *rls.Release) []string {
	if release == nil {
		return append(titles, "")
	}
	return append(append(titles, release.Title), release.Alt...)
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
		if parsed := m.parseReleaseName(part); parsed.Series > 0 || parsed.Episode > 0 {
			break
		}
	}
	if len(titles) < 2 {
		return nil
	}
	return titles
}

func addNormalizedTitle(titles []string, title string) []string {
	if title == "" {
		return titles
	}
	normalized := stringutils.NormalizeForMatching(title)
	if normalized == "" || slices.Contains(titles, normalized) {
		return titles
	}
	return append(titles, normalized)
}

func normalizedTitleSetsOverlap(sourceTitles, candidateTitles []string) bool {
	for _, title := range sourceTitles {
		if slices.Contains(candidateTitles, title) {
			return true
		}
	}
	return false
}

func compareTVStructure(comparison *releaseComparison, source, candidate *rls.Release, findIndividualEpisodes, isTV bool) {
	if !isTV {
		return
	}

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

	if !findIndividualEpisodes && sourceIsPack != candidateIsPack {
		comparison.add(differencePackVersusEpisode)
	}

	if !sourceIsPack && !candidateIsPack && source.Episode != candidate.Episode {
		comparison.add(differenceEpisode)
	}
}

func (m matcher) compareGroupSiteAndChecksum(comparison *releaseComparison, sourceSide, candidateSide namedRelease) {
	source, candidate := sourceSide.release, candidateSide.release
	normalizer := m.normalizer()
	sourceGroup := normalizer.Normalize((source.Group))
	candidateGroup := normalizer.Normalize((candidate.Group))
	sourceSite := normalizer.Normalize(source.Site)
	candidateSite := normalizer.Normalize(candidate.Site)
	spaceJoined := m.spaceJoinedGroupSite(source, candidate)

	switch {
	case m.dashSuffixGroupSpellsSite(sourceSide, candidateSide):
		candidateGroup = sourceSite
	case m.dashSuffixGroupSpellsSite(candidateSide, sourceSide):
		sourceGroup = candidateSite
	}

	bracketGroup := sourceGroup != "" && sourceGroup == sourceSite && !releaseHasExplicitGroupTag(source)
	if sourceGroup != "" && !bracketGroup && !spaceJoined {
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

	if sourceSite != "" && !spaceJoined {
		candidateSiteIdentity := candidateSite
		if candidateSiteIdentity == "" {
			candidateSiteIdentity = candidateGroup
		}
		if candidateSiteIdentity != "" && sourceSite != candidateSiteIdentity {
			comparison.add(differenceSite)
		}
	}

	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if sourceSum != "" && candidateSum != "" && sourceSum != candidateSum {
		comparison.add(differenceChecksum)
	}
}

func (m matcher) spaceJoinedGroupSite(source, candidate *rls.Release) bool {
	normalizer := m.normalizer()
	joined := func(split, whole *rls.Release) bool {
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

const sourceMismatchReason = "source mismatch"

func (m matcher) compareFormatAndCodec(comparison *releaseComparison, source, candidate *rls.Release) {
	normalizer := m.normalizer()

	sourceSource := normalizeSource(source.Source)
	candidateSource := normalizeSource(candidate.Source)
	if !sourcesCompatible(sourceSource, candidateSource) {
		comparison.add(differenceSource)
	}

	sourceRes := normalizer.Normalize((source.Resolution))
	candidateRes := normalizer.Normalize((candidate.Resolution))
	if sourceRes != candidateRes {
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

	sourceCollection := sameServiceCollection(normalizer.Normalize(source.Collection))
	candidateCollection := sameServiceCollection(normalizer.Normalize(candidate.Collection))
	if sourceCollection != "" && candidateCollection != "" && sourceCollection != candidateCollection {
		comparison.add(differenceCollection)
	}

	if len(source.Codec) > 0 && len(candidate.Codec) > 0 {
		sourceCodec := joinNormalizedCodecSlice(source.Codec)
		candidateCodec := joinNormalizedCodecSlice(candidate.Codec)
		if sourceCodec != candidateCodec {
			comparison.add(differenceCodec)
		}
	}

	sourceHDR := joinNormalizedHDRSlice(source.HDR)
	candidateHDR := joinNormalizedHDRSlice(candidate.HDR)
	if sourceHDR != "" && candidateHDR != "" && sourceHDR != candidateHDR {
		comparison.add(differenceHDR)
	}

	sourceBitDepth := normalizer.Normalize(source.BitDepth)
	candidateBitDepth := normalizer.Normalize(candidate.BitDepth)
	if sourceBitDepth != "" && candidateBitDepth != "" && sourceBitDepth != candidateBitDepth {
		comparison.add(differenceBitDepth)
	}
}

func (m matcher) compareMetadataFlags(comparison *releaseComparison, source, candidate *rls.Release) {
	normalizer := m.normalizer()

	if len(source.Cut) > 0 && len(candidate.Cut) > 0 {
		sourceCut := joinNormalizedSlice(source.Cut)
		candidateCut := joinNormalizedSlice(candidate.Cut)
		if sourceCut != candidateCut {
			comparison.add(differenceCut)
		}
	}

	if len(source.Edition) > 0 && len(candidate.Edition) > 0 {
		sourceEdition := joinNormalizedSlice(source.Edition)
		candidateEdition := joinNormalizedSlice(candidate.Edition)
		if sourceEdition != candidateEdition {
			comparison.add(differenceEdition)
		}
	}

	if len(source.Language) > 0 && len(candidate.Language) > 0 {
		if joinNormalizedSlice(source.Language) != joinNormalizedSlice(candidate.Language) {
			comparison.add(differenceLanguage)
		}
	}

	sourceVersion := normalizer.Normalize(source.Version)
	candidateVersion := normalizer.Normalize(candidate.Version)
	if sourceVersion != "" && candidateVersion != "" && sourceVersion != candidateVersion {
		comparison.add(differenceVersion)
	}

	sourceDisc := normalizer.Normalize(source.Disc)
	candidateDisc := normalizer.Normalize(candidate.Disc)
	if sourceDisc != "" && candidateDisc != "" && sourceDisc != candidateDisc {
		comparison.add(differenceDisc)
	}

	sourcePlatform := normalizer.Normalize(source.Platform)
	candidatePlatform := normalizer.Normalize(candidate.Platform)
	if sourcePlatform != "" && candidatePlatform != "" && sourcePlatform != candidatePlatform {
		comparison.add(differencePlatform)
	}

	sourceArch := normalizer.Normalize(source.Arch)
	candidateArch := normalizer.Normalize(candidate.Arch)
	if sourceArch != "" && candidateArch != "" && sourceArch != candidateArch {
		comparison.add(differenceArchitecture)
	}
}

func compareReleaseVariants(comparison *releaseComparison, source, candidate *rls.Release) {
	if compatible, reason := checkVariantsCompatible(source, candidate); !compatible {
		comparison.add(differenceVariant)
		comparison.variantReason = cmp.Or(reason, "variant mismatch")
	}
}

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
	tags := make([]string, len(slice))
	for i, tag := range slice {
		if tag == "HDR" {
			tag = "HDR10"
		}
		tags[i] = tag
	}
	return strings.Join(releases.NormalizeHDRTags(tags), " ")
}

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

func normalizeVideoCodec(codec string) string {
	upper := normalizeVariant(codec)
	if canonical, ok := videoCodecAliases[upper]; ok {
		return canonical
	}
	return upper
}

var sourceAliases = map[string]string{
	"WEB-DL": "WEBDL",
	"WEBDL":  "WEBDL",
	"WEBRIP": "WEBRIP",
	"WEB":    "WEB",
}

func normalizeSource(source string) string {
	upper := strings.TrimPrefix(normalizeVariant(source), "UHD.")
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

	return source == "WEB" || candidate == "WEB"
}

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

func (m matcher) getMatchTypeFromTitle(targetRelease, candidateRelease *rls.Release, candidateFiles qbt.TorrentFiles) string {
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

	if targetRelease.Series > 0 && targetRelease.Episode > 0 {
		targetKey := releaseKey{
			series:  targetRelease.Series,
			episode: targetRelease.Episode,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else if targetRelease.Series > 0 {
		for key := range candidateReleases {
			if key.series == targetRelease.Series && key.episode > 0 {
				return "partial-contains"
			}
		}
	} else if targetRelease.Year > 0 && targetRelease.Month > 0 && targetRelease.Day > 0 {
		targetKey := releaseKey{
			year:  targetRelease.Year,
			month: targetRelease.Month,
			day:   targetRelease.Day,
		}
		if _, exists := candidateReleases[targetKey]; exists {
			return "partial-in-pack"
		}
	} else {
		targetYear := makeReleaseKey(targetRelease).year
		for key := range candidateReleases {
			if key.series == 0 && key.month == 0 && (targetYear == 0 || key.year == targetYear) {
				return "partial-in-pack"
			}
		}
	}

	candidateSeries := candidateRelease.Series
	if candidateSeries == 0 && seasonlessEpisodes {
		candidateSeries = 1
	}
	if targetRelease.Series > 0 && candidateSeries == targetRelease.Series &&
		!hasEpisodeLevelKeys(candidateReleases) {
		return "release-match"
	}

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

type MatchResult struct {
	MatchType string
	Reason    string
}

func (m matcher) getMatchTypeWithReason(localRelease, newRelease *rls.Release, localFiles, newFiles qbt.TorrentFiles, tolerancePercent float64) MatchResult {
	var timer *prometheus.Timer
	if m.metrics != nil {
		timer = prometheus.NewTimer(m.metrics.GetMatchTypeDuration)
		defer timer.ObserveDuration()
		m.metrics.GetMatchTypeCalls.Inc()
	}

	normalizer := m.normalizer()

	localLayout := classifyTorrentLayout(localFiles, normalizer)
	newLayout := classifyTorrentLayout(newFiles, normalizer)
	if localLayout != LayoutUnknown && newLayout != LayoutUnknown && localLayout != newLayout {
		if m.metrics != nil {
			m.metrics.GetMatchTypeNoMatch.Inc()
		}
		reason := fmt.Sprintf("Layout mismatch: source is %s, candidate is %s", layoutDescription(localLayout), layoutDescription(newLayout))
		return MatchResult{MatchType: "", Reason: reason}
	}

	var (
		filteredLocalFiles []TorrentFile
		filteredNewFiles   []TorrentFile
		totalLocalSize     int64
		totalNewSize       int64
		localReleaseKeys   = make(map[releaseKey]int64)
		newReleaseKeys     = make(map[releaseKey]int64)
	)

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

	if m.streamingExactMatch(filteredLocalFiles, filteredNewFiles) {
		if m.metrics != nil {
			m.metrics.GetMatchTypeExactMatch.Inc()
		}
		return MatchResult{MatchType: "exact", Reason: ""}
	}

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

	if len(filteredLocalFiles) != len(filteredNewFiles) {
		if containment := sizeContainmentMatchType(filteredLocalFiles, filteredNewFiles); containment != "" {
			if m.metrics != nil {
				m.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: containment, Reason: ""}
		}
	}

	if totalLocalSize > 0 && len(filteredLocalFiles) > 0 {
		if m.isSizeWithinTolerance(totalLocalSize, totalNewSize, tolerancePercent) {
			if m.metrics != nil {
				m.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: "size", Reason: ""}
		}
	}

	if len(localReleaseKeys) == 0 && len(newReleaseKeys) == 0 &&
		len(filteredLocalFiles) > 0 && len(filteredNewFiles) > 0 {
		if m.streamingLargestFileMatch(filteredLocalFiles, filteredNewFiles) {
			if m.metrics != nil {
				m.metrics.GetMatchTypeSizeMatch.Inc()
			}
			return MatchResult{MatchType: "size", Reason: ""}
		}
	}

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

	if len(localFiles) != len(newFiles) {
		return fmt.Sprintf("File count mismatch: source has %d files, existing has %d files",
			len(localFiles), len(newFiles))
	}

	if len(localKeys) == 0 && len(newKeys) == 0 {
		return "Unable to parse release metadata from filenames"
	}

	if len(localKeys) > 0 && len(newKeys) > 0 {
		return "Release metadata doesn't match between source and existing files"
	}

	return "Files don't match (structure or naming differs)"
}

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

func (m matcher) streamingExactMatch(sourceFiles, candidateFiles []TorrentFile) bool {
	if len(sourceFiles) != len(candidateFiles) {
		return false
	}

	sourceMap := make(map[string]int64, len(sourceFiles))
	for _, sf := range sourceFiles {
		sourceMap[sf.Name] = sf.Size
	}

	for _, cf := range candidateFiles {
		if sourceSize, exists := sourceMap[cf.Name]; !exists || sourceSize != cf.Size {
			return false
		}
	}

	return true
}

func (m matcher) streamingLargestFileMatch(sourceFiles, candidateFiles []TorrentFile) bool {
	var (
		srcPath  string
		srcSize  int64
		candPath string
		candSize int64
	)

	for _, sf := range sourceFiles {
		if sf.Size > srcSize {
			srcSize = sf.Size
			srcPath = sf.Name
		}
	}

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

func enrichReleaseFromTorrent(fileRelease *rls.Release, torrentRelease *rls.Release) *rls.Release {
	enriched := *fileRelease

	bracketGroup := enriched.Group != "" && enriched.Group == enriched.Site && !releaseHasExplicitGroupTag(fileRelease)
	if (enriched.Group == "" || bracketGroup) && torrentRelease.Group != "" {
		enriched.Group = torrentRelease.Group
	}

	if enriched.Resolution == "" && torrentRelease.Resolution != "" {
		enriched.Resolution = torrentRelease.Resolution
	}

	if len(enriched.Codec) == 0 && len(torrentRelease.Codec) > 0 {
		enriched.Codec = torrentRelease.Codec
	}

	if len(enriched.Audio) == 0 && len(torrentRelease.Audio) > 0 {
		enriched.Audio = torrentRelease.Audio
	}

	if enriched.Source == "" && torrentRelease.Source != "" {
		enriched.Source = torrentRelease.Source
	}

	if len(enriched.HDR) == 0 && len(torrentRelease.HDR) > 0 {
		enriched.HDR = torrentRelease.HDR
	}

	if enriched.BitDepth == "" && torrentRelease.BitDepth != "" {
		enriched.BitDepth = torrentRelease.BitDepth
	}

	if enriched.Series == 0 && torrentRelease.Series > 0 {
		enriched.Series = torrentRelease.Series
	}

	if enriched.Year == 0 && torrentRelease.Year > 0 {
		enriched.Year = torrentRelease.Year
	}

	return &enriched
}

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

	threshold := float64(len(subset)) * 0.8
	return float64(matchCount) >= threshold
}

func (m matcher) isSizeWithinTolerance(sourceSize, candidateSize int64, tolerancePercent float64) bool {
	if sourceSize == 0 || candidateSize == 0 {
		return sourceSize == candidateSize
	}

	if tolerancePercent < 0 {
		tolerancePercent = 0
	}

	if tolerancePercent == 0 {
		return sourceSize == candidateSize
	}

	tolerance := float64(sourceSize) * (tolerancePercent / 100.0)
	minAcceptableSize := float64(sourceSize) - tolerance
	maxAcceptableSize := float64(sourceSize) + tolerance

	candidateSizeFloat := float64(candidateSize)
	return candidateSizeFloat >= minAcceptableSize && candidateSizeFloat <= maxAcceptableSize
}
