// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"strconv"
	"strings"

	"github.com/moistari/rls"
)

// searchCandidateClass identifies the search-only rule that admitted a result.
// The class is carried privately through cached search results so apply can
// preserve that decision without exposing a client-settable bypass.
type searchCandidateClass string

const (
	searchCandidateClassRejected         searchCandidateClass = "rejected"
	searchCandidateClassStrict           searchCandidateClass = "strict"
	searchCandidateClassWebSourceRelabel searchCandidateClass = "web-source-relabel"
	// searchCandidateClassExactSizeFallback means positive exact byte equality
	// replaced only the soft release-attribute checks rejected by strict matching.
	searchCandidateClassExactSizeFallback searchCandidateClass = "exact-size-fallback"
	searchCandidateClassTitleRescue       searchCandidateClass = "title-rescue"
)

// searchSizeEvidence describes size evidence available before a candidate
// torrent is downloaded. SourceSize is qBittorrent's wanted size and
// CandidateSize is the Torznab-advertised size.
type searchSizeEvidence string

const (
	searchSizeEvidenceNone  searchSizeEvidence = "none"
	searchSizeEvidenceExact searchSizeEvidence = "exact"
)

// searchCandidateInput contains the source and Torznab candidate data available
// to both main result filtering and alternate-query usability checks.
type searchCandidateInput struct {
	SourceRelease          *rls.Release
	CandidateRelease       *rls.Release
	SourceName             string
	CandidateName          string
	SourceTitles           []string
	CandidateTitles        []string
	SourceSize             int64
	CandidateSize          int64
	TolerancePercent       float64
	FindIndividualEpisodes bool
	RescueTitleMismatches  bool
}

// searchCandidateDecision records admission provenance, including which strict
// mismatch exact-size evidence relaxed. Rejected decisions never grant apply
// access to the release-prefilter bypass.
type searchCandidateDecision struct {
	Accepted             bool
	Class                searchCandidateClass
	SizeEvidence         searchSizeEvidence
	SourceTitles         []string
	RejectReason         string
	StrictMismatchReason string
	RelaxedDifferences   []string
	Score                float64
	MatchReason          string
	SizeRejected         bool
}

// Score bands mirror the explicit sorter: positive size evidence outranks the
// release scorer's tolerance-only range, and stricter classes display a higher
// score than relaxed classes within the same evidence tier.
const (
	sizeEvidenceFallbackScoreBonus = 2.0
	sizeEvidenceRelabelScoreBonus  = 3.0
	sizeEvidenceStrictScoreBonus   = 4.0
)

// classifySearchCandidate applies the shared search-only admission rules.
// Exact-size fallback requires equal reported sizes plus strict title,
// resolution, group/site, checksum, artist, and date identity, and the TV shape
// rules around packs and episodes. It may relax descriptive attributes such as
// source, collection, HDR, codec, or bit depth, and the season and episode
// numbers that indexers rewrite. Apply later uses the private decision class to
// skip its duplicate release prefilter; normal torrent-file validation remains
// authoritative.
func (s *Service) classifySearchCandidate(input searchCandidateInput) searchCandidateDecision {
	decision := searchCandidateDecision{
		Class:        searchCandidateClassRejected,
		SizeEvidence: classifySearchSizeEvidence(input.SourceSize, input.CandidateSize),
	}
	ignoreSizeCheck := input.FindIndividualEpisodes &&
		isTVSeasonPack(input.SourceRelease) && isTVEpisode(input.CandidateRelease)

	strictMatch, mismatchReason := s.releasesMatchWithReasonAndNamesAndTitles(
		input.SourceRelease,
		input.CandidateRelease,
		input.SourceName,
		input.CandidateName,
		input.SourceTitles,
		input.CandidateTitles,
		input.FindIndividualEpisodes,
	)
	decision.StrictMismatchReason = mismatchReason

	class := searchCandidateClassStrict
	switch {
	case strictMatch:
	case s.shouldAcceptWebSourceRelabel(
		input.SourceRelease,
		input.CandidateRelease,
		input.SourceName,
		input.CandidateName,
		input.SourceTitles,
		input.CandidateTitles,
		input.FindIndividualEpisodes,
		ignoreSizeCheck,
		input.SourceSize,
		input.CandidateSize,
		input.TolerancePercent,
		mismatchReason,
	):
		class = searchCandidateClassWebSourceRelabel
	case input.RescueTitleMismatches &&
		mismatchReason == titleMismatchReason &&
		decision.SizeEvidence.matches():
		if ok, reason := s.releasesMatchExceptTitleWithReason(
			input.SourceRelease,
			input.CandidateRelease,
			input.FindIndividualEpisodes,
		); ok {
			class = searchCandidateClassTitleRescue
		} else {
			decision.RejectReason = reason
			return decision
		}
	case decision.SizeEvidence.matches():
		relaxedDifferences := recordedReleaseDifferences(input.SourceRelease, input.CandidateRelease)
		if ok, reason := s.validateExactSizeFallback(input, mismatchReason, relaxedDifferences); ok {
			class = searchCandidateClassExactSizeFallback
			decision.RelaxedDifferences = relaxedDifferences
		} else {
			decision.RejectReason = reason
			return decision
		}
	default:
		decision.RejectReason = mismatchReason
		return decision
	}

	// Search context: candidate is the new torrent, source is the existing torrent.
	if reject, reason := rejectSeasonPackFromEpisode(
		input.CandidateRelease,
		input.SourceRelease,
		input.FindIndividualEpisodes,
	); reject {
		decision.RejectReason = reason
		return decision
	}

	if !ignoreSizeCheck && !s.isSizeWithinTolerance(input.SourceSize, input.CandidateSize, input.TolerancePercent) {
		decision.RejectReason = "size mismatch"
		decision.SizeRejected = true
		return decision
	}

	decision.Accepted = true
	decision.Class = class
	decision.SourceTitles = slices.Clone(input.SourceTitles)
	decision.Score, decision.MatchReason = evaluateReleaseMatch(input.SourceRelease, input.CandidateRelease)
	if decision.Score <= 0 {
		decision.Score = 1
	}

	switch class {
	case searchCandidateClassTitleRescue:
		decision.MatchReason = "Title rescue · full check required"
	case searchCandidateClassExactSizeFallback:
		decision.Score += sizeEvidenceFallbackScoreBonus
		decision.MatchReason = decision.SizeEvidence.matchReason() + "; strict title/resolution/group"
		if len(decision.RelaxedDifferences) > 0 {
			decision.MatchReason += "; relaxed " + strings.Join(decision.RelaxedDifferences, ",")
		}
	case searchCandidateClassWebSourceRelabel:
		if decision.SizeEvidence.matches() {
			decision.Score += sizeEvidenceRelabelScoreBonus
			decision.MatchReason = decision.SizeEvidence.matchReason() + "; web-source relabel; " + decision.MatchReason
		} else {
			decision.MatchReason = "web-source relabel; " + decision.MatchReason
		}
	case searchCandidateClassStrict:
		if decision.SizeEvidence.matches() {
			decision.Score += sizeEvidenceStrictScoreBonus
			decision.MatchReason = decision.SizeEvidence.matchReason() + "; strict metadata; " + decision.MatchReason
		}
	case searchCandidateClassRejected:
	}

	return decision
}

// positiveExactSize requires both search APIs to report the same non-zero byte
// count. Missing sizes and tolerance-only matches cannot activate the fallback.
func positiveExactSize(sourceSize, candidateSize int64) bool {
	return sourceSize > 0 && candidateSize > 0 && sourceSize == candidateSize
}

func classifySearchSizeEvidence(sourceSize, candidateSize int64) searchSizeEvidence {
	if positiveExactSize(sourceSize, candidateSize) {
		return searchSizeEvidenceExact
	}
	return searchSizeEvidenceNone
}

func (evidence searchSizeEvidence) matches() bool {
	return evidence == searchSizeEvidenceExact
}

func (evidence searchSizeEvidence) priority() int {
	switch evidence {
	case searchSizeEvidenceExact:
		return 1
	case searchSizeEvidenceNone:
		return 0
	}
	return 0
}

func (evidence searchSizeEvidence) matchReason() string {
	switch evidence {
	case searchSizeEvidenceExact:
		return "exact byte size"
	case searchSizeEvidenceNone:
		return ""
	}
	return ""
}

// validateExactSizeSearchIdentity enforces identity attributes that exact size
// must never replace. It returns the first hard mismatch for search diagnostics.
func (s *Service) validateExactSizeSearchIdentity(input searchCandidateInput) (bool, string) {
	source := input.SourceRelease
	candidate := input.CandidateRelease
	if source == nil || candidate == nil {
		return false, "missing parsed release"
	}

	if ok, reason := s.validateTitleArtistAndDates(
		source,
		candidate,
		input.SourceName,
		input.CandidateName,
		input.SourceTitles,
		input.CandidateTitles,
		isTVRelease(source) || isTVRelease(candidate),
	); !ok {
		return false, reason
	}

	normalizer := normalizerForService(s)
	sourceResolution := normalizer.Normalize(source.Resolution)
	candidateResolution := normalizer.Normalize(candidate.Resolution)
	if sourceResolution == "" || candidateResolution == "" || sourceResolution != candidateResolution {
		return false, "resolution mismatch"
	}

	sourceIdentity := normalizedGroupSiteIdentity(s, source)
	candidateIdentity := normalizedGroupSiteIdentity(s, candidate)
	if sourceIdentity == "" || candidateIdentity == "" || sourceIdentity != candidateIdentity {
		return false, "group/site mismatch"
	}

	// A CRC32 tag rides on the anime file name, and indexer titles routinely drop
	// it, so one side alone carrying a checksum is absence of evidence. Two tags
	// that disagree still prove different files.
	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if sourceSum != "" && candidateSum != "" && sourceSum != candidateSum {
		return false, "checksum mismatch"
	}

	// Missing artist/date metadata cannot establish the high-confidence identity
	// required by this fallback when the other release explicitly carries it.
	sourceArtist := normalizer.Normalize(source.Artist)
	candidateArtist := normalizer.Normalize(candidate.Artist)
	if (sourceArtist != "" || candidateArtist != "") && sourceArtist != candidateArtist {
		return false, "artist mismatch"
	}
	if source.Year != candidate.Year {
		return false, "year mismatch"
	}
	if source.Month != candidate.Month || source.Day != candidate.Day {
		return false, "date mismatch"
	}

	return true, ""
}

// validateExactSizeFallback keeps hard release identity strict and permits only
// mismatch categories explicitly recorded as relaxed for this search result.
func (s *Service) validateExactSizeFallback(input searchCandidateInput, mismatchReason string, relaxedDifferences []string) (bool, string) {
	if ok, reason := s.validateExactSizeSearchIdentity(input); !ok {
		return false, reason
	}

	difference, ok := exactSizeRelaxedDifferenceForReason(input, mismatchReason)
	if !ok || !slices.Contains(relaxedDifferences, difference) {
		return false, mismatchReason
	}

	return true, ""
}

func exactSizeRelaxedDifferenceForReason(input searchCandidateInput, mismatchReason string) (string, bool) {
	reason := strings.ToLower(strings.TrimSpace(mismatchReason))
	reason = strings.TrimSuffix(reason, " mismatch")
	difference := strings.ReplaceAll(reason, " ", "-")
	// Season and episode are the identity fields indexers rewrite: a tracker with
	// one entry per cour stamps S01 on every season, and absolute numbering meets
	// renumbered candidates. Only a like-for-like shape may retire that number.
	// validateTVStructure reports a season mismatch BEFORE it compares pack
	// against episode, so without these guards a differing season would skip the
	// shape check entirely.
	switch difference {
	case "season":
		if isTVSeasonPack(input.SourceRelease) && isTVSeasonPack(input.CandidateRelease) {
			return difference, true
		}
		return "", false
	case "episode":
		if isTVEpisode(input.SourceRelease) && isTVEpisode(input.CandidateRelease) {
			return difference, true
		}
		return "", false
	case "source", "collection", "codec", "hdr", "bit-depth", "cut", "edition", "language", "version", "disc", "platform", "architecture", "checksum":
		return difference, true
	}

	compatible, variantReason := checkVariantsCompatible(input.SourceRelease, input.CandidateRelease)
	if !compatible && strings.EqualFold(strings.TrimSpace(mismatchReason), variantReason) {
		return "variant", true
	}

	return "", false
}

// searchRelaxedStructure reports whether a search decision retired a season or
// episode difference. Equal file sizes cannot confirm which episode a torrent
// holds, so those pairings must be hashed before they seed.
func searchRelaxedStructure(relaxedDifferences []string) bool {
	return slices.Contains(relaxedDifferences, "season") ||
		slices.Contains(relaxedDifferences, "episode")
}

func normalizedGroupSiteIdentity(s *Service, release *rls.Release) string {
	if release == nil {
		return ""
	}
	normalizer := normalizerForService(s)
	if group := normalizer.Normalize(release.Group); group != "" {
		return group
	}
	return normalizer.Normalize(release.Site)
}

// recordedReleaseDifferences lists every field the two releases disagree on.
// Most are descriptive, but season, episode and checksum are not: they decide
// whether the add must be hashed before it seeds, so a new entry here is never
// automatically safe to relax.
func recordedReleaseDifferences(source, candidate *rls.Release) []string {
	if source == nil || candidate == nil {
		return nil
	}

	normalizer := normalizerForService(nil)
	var differences []string
	add := func(name, sourceValue, candidateValue string) {
		if sourceValue != candidateValue && !slices.Contains(differences, name) {
			differences = append(differences, name)
		}
	}

	add("source", normalizeSource(source.Source), normalizeSource(candidate.Source))
	sourceCollection := normalizer.Normalize(strings.TrimSpace(source.Collection + " " + source.Subtitle))
	candidateCollection := normalizer.Normalize(strings.TrimSpace(candidate.Collection + " " + candidate.Subtitle))
	add("collection", sourceCollection, candidateCollection)
	add("codec", joinNormalizedCodecSlice(source.Codec), joinNormalizedCodecSlice(candidate.Codec))
	add("hdr", joinNormalizedHDRSlice(source.HDR), joinNormalizedHDRSlice(candidate.HDR))
	add("bit-depth", normalizer.Normalize(source.BitDepth), normalizer.Normalize(candidate.BitDepth))
	add("cut", joinNormalizedSlice(source.Cut), joinNormalizedSlice(candidate.Cut))
	add("edition", joinNormalizedSlice(source.Edition), joinNormalizedSlice(candidate.Edition))
	add("language", joinNormalizedSlice(source.Language), joinNormalizedSlice(candidate.Language))
	add("version", normalizer.Normalize(source.Version), normalizer.Normalize(candidate.Version))
	add("disc", normalizer.Normalize(source.Disc), normalizer.Normalize(candidate.Disc))
	add("platform", normalizer.Normalize(source.Platform), normalizer.Normalize(candidate.Platform))
	add("architecture", normalizer.Normalize(source.Arch), normalizer.Normalize(candidate.Arch))
	add("checksum", normalizer.Normalize(source.Sum), normalizer.Normalize(candidate.Sum))
	add("season", strconv.Itoa(source.Series), strconv.Itoa(candidate.Series))
	add("episode", strconv.Itoa(source.Episode), strconv.Itoa(candidate.Episode))
	if compatible, _ := checkVariantsCompatible(source, candidate); !compatible {
		differences = append(differences, "variant")
	}

	return differences
}
