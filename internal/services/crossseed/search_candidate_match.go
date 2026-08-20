// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/releases"
)

// searchCandidateClass identifies the search-only rule that admitted a result.
// The class is carried privately through cached search results so apply can
// preserve that decision without exposing a client-settable bypass.
type searchCandidateClass string

const (
	searchCandidateClassRejected         searchCandidateClass = "rejected"
	searchCandidateClassStrict           searchCandidateClass = "strict"
	searchCandidateClassWebSourceRelabel searchCandidateClass = "web-source-relabel"
	// searchCandidateClassExactSizeFallback means positive exact reported-size equality
	// replaced a relaxable check rejected by strict matching: a release attribute,
	// or a season or episode number that keeps the same pack-or-episode shape.
	searchCandidateClassExactSizeFallback searchCandidateClass = "exact-size-fallback"
	searchCandidateClassTitleRescue       searchCandidateClass = "title-rescue"
)

// searchSizeEvidence describes size evidence available before a candidate
// torrent is downloaded. SourceSize is qBittorrent's reported total size (with
// wanted size as a compatibility fallback), and CandidateSize is the
// Torznab-advertised size.
type searchSizeEvidence string

const (
	searchSizeEvidenceNone  searchSizeEvidence = "none"
	searchSizeEvidenceExact searchSizeEvidence = "exact"
)

// searchCandidateInput contains the two release views and size evidence used by
// search classification, alternate-query checks, and apply-time decision replay.
type searchCandidateInput struct {
	Source                 namedRelease
	Candidate              namedRelease
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
	Accepted     bool
	Class        searchCandidateClass
	SizeEvidence searchSizeEvidence
	// StrictChecksumReplay preserves a directional strict match when apply sees
	// only the same one-sided checksum in reverse.
	StrictChecksumReplay bool
	// SearchCandidateName binds replay to the Torznab name search evaluated.
	SearchCandidateName   string
	GroupFallbackIdentity string
	SourceTitles          []string
	RejectReason          string
	StrictMismatchReason  string
	RelaxedDifferences    []string
	Score                 float64
	MatchReason           string
	SizeRejected          bool
}

// searchDecisionProvenance is the private, replayable part of a search
// decision. It travels as one value from classification through cached results
// to apply so adding a safety field cannot leave one transport path behind.
type searchDecisionProvenance struct {
	Class searchCandidateClass
	// StrictChecksumReplay is separate from RelaxedDifferences because search did
	// not relax a mismatch and the result keeps strict ranking.
	StrictChecksumReplay bool
	// SearchCandidateName lets apply reject new metadata in the downloaded name.
	SearchCandidateName   string
	SourceInstanceID      int
	SourceHash            string
	StrictMismatchReason  string
	RelaxedDifferences    []string
	GroupFallbackIdentity string
	SourceTitles          []string
}

func (decision searchCandidateDecision) provenance() searchDecisionProvenance {
	return searchDecisionProvenance{
		Class:                 decision.Class,
		StrictChecksumReplay:  decision.StrictChecksumReplay,
		SearchCandidateName:   decision.SearchCandidateName,
		StrictMismatchReason:  decision.StrictMismatchReason,
		RelaxedDifferences:    slices.Clone(decision.RelaxedDifferences),
		GroupFallbackIdentity: decision.GroupFallbackIdentity,
		SourceTitles:          slices.Clone(decision.SourceTitles),
	}
}

func (provenance searchDecisionProvenance) clone() searchDecisionProvenance {
	provenance.RelaxedDifferences = slices.Clone(provenance.RelaxedDifferences)
	provenance.SourceTitles = slices.Clone(provenance.SourceTitles)
	return provenance
}

func (provenance searchDecisionProvenance) bindSource(instanceID int, hash string) searchDecisionProvenance {
	bound := provenance.clone()
	bound.SourceInstanceID = instanceID
	bound.SourceHash = normalizeHash(hash)
	return bound
}

func (provenance searchDecisionProvenance) admitted() bool {
	return provenance.Class != "" && provenance.Class != searchCandidateClassRejected
}

func searchDecisionRequiresVerification(decision searchDecisionProvenance) bool {
	return decision.Class == searchCandidateClassTitleRescue ||
		searchRelaxationRequiresVerification(decision.StrictMismatchReason)
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
// resolution, artist, and date identity, non-conflicting checksums, and the TV
// shape rules around packs and episodes. Group/site identity also stays strict
// except for the provenance-backed cross-field rescue, which requires a full
// hash check. Checksum-only replay preserves an ordinary strict decision after
// equalizing the missing CRC; it also permits the narrow no-group checksum
// case. The fallback may relax descriptive attributes such as source,
// collection, HDR, codec, or bit depth, and the season and episode numbers that
// indexers rewrite.
// Apply later uses the private decision provenance to replay the release
// prefilter; normal torrent-file validation remains authoritative.
func (s *Service) classifySearchCandidate(input searchCandidateInput) searchCandidateDecision {
	decision := searchCandidateDecision{
		Class:               searchCandidateClassRejected,
		SizeEvidence:        classifySearchSizeEvidence(input.SourceSize, input.CandidateSize),
		SearchCandidateName: input.Candidate.rawName,
	}
	ignoreSizeCheck := input.FindIndividualEpisodes &&
		isTVSeasonPack(input.Source.release) && isTVEpisode(input.Candidate.release)

	strictMatch, mismatchReason := s.releasesMatchWithReasonAndNamesAndTitles(
		input.Source.release,
		input.Candidate.release,
		input.Source.rawName,
		input.Candidate.rawName,
		input.SourceTitles,
		input.CandidateTitles,
		input.FindIndividualEpisodes,
	)
	// Strict checksum matching is directional: an existing release without a CRC
	// accepts a candidate that carries one. Keep that result strict, but record a
	// private replay token because apply normally compares the pair in reverse.
	// When the source alone carries a CRC, equalizing that missing evidence may
	// also recover an otherwise strict match. If source relabeling was the real
	// rejection, keep it as the cause and record checksum separately.
	exactOneSidedChecksum := decision.SizeEvidence.matches() &&
		s.hasOneSidedChecksum(input.Source.release, input.Candidate.release)
	strictOneSidedChecksum := strictMatch && exactOneSidedChecksum
	strictChecksumReplay := strictOneSidedChecksum &&
		s.oneSidedChecksumIsOnlyStrictDifference(reverseSearchCandidateInput(input))
	checksumOnlyFallback := exactOneSidedChecksum && !strictMatch &&
		s.oneSidedChecksumIsOnlyStrictDifference(input)
	preferExactSizeFallback := exactOneSidedChecksum && mismatchReason == sourceMismatchReason
	decision.StrictMismatchReason = mismatchReason

	class := searchCandidateClassStrict
	switch {
	case strictMatch:
		if strictChecksumReplay {
			decision.StrictChecksumReplay = true
		}
	case !preferExactSizeFallback && s.shouldAcceptWebSourceRelabel(
		input.Source.release,
		input.Candidate.release,
		input.Source.rawName,
		input.Candidate.rawName,
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
			input.Source.release,
			input.Candidate.release,
			input.FindIndividualEpisodes,
		); ok {
			class = searchCandidateClassTitleRescue
		} else {
			decision.RejectReason = reason
			return decision
		}
	case decision.SizeEvidence.matches():
		// A one-sided CRC is missing evidence in either direction. If equalizing it
		// makes the ordinary strict matcher pass, preserve that decision without
		// imposing the fallback-only requirement for a non-empty group identity.
		var relaxedDifferences []string
		if checksumOnlyFallback {
			relaxedDifferences = []string{"checksum"}
		} else {
			if ok, reason := s.validateExactSizeSearchIdentity(input); !ok {
				decision.RejectReason = reason
				return decision
			}
			observedDifferences := s.observedReleaseDifferences(input.Source, input.Candidate)
			var fallbackAccepted bool
			var rejectReason string
			relaxedDifferences, fallbackAccepted, rejectReason = s.replayRelaxedDifferences(
				input,
				mismatchReason,
				observedDifferences,
			)
			if !fallbackAccepted {
				decision.RejectReason = rejectReason
				return decision
			}
		}
		if exactOneSidedChecksum && !slices.Contains(relaxedDifferences, "checksum") {
			relaxedDifferences = append(relaxedDifferences, "checksum")
		}
		class = searchCandidateClassExactSizeFallback
		decision.RelaxedDifferences = relaxedDifferences
		if slices.Contains(relaxedDifferences, "group") {
			decision.GroupFallbackIdentity, _ = s.crossFieldGroupSiteFallbackIdentity(input.Source, input.Candidate)
		}
	default:
		decision.RejectReason = mismatchReason
		return decision
	}

	// Search context: candidate is the new torrent, source is the existing torrent.
	if reject, reason := rejectSeasonPackFromEpisode(
		input.Candidate.release,
		input.Source.release,
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
	decision.Score, decision.MatchReason = evaluateReleaseMatch(input.Source.release, input.Candidate.release)
	if decision.Score <= 0 {
		decision.Score = 1
	}

	switch class {
	case searchCandidateClassTitleRescue:
		decision.MatchReason = "Title rescue · full check required"
	case searchCandidateClassExactSizeFallback:
		decision.Score += sizeEvidenceFallbackScoreBonus
		strictFields := "; strict title/resolution/group"
		if checksumOnlyFallback {
			strictFields = "; one-sided checksum only"
		} else if slices.Contains(decision.RelaxedDifferences, "group") {
			strictFields = "; strict title/resolution"
		}
		decision.MatchReason = decision.SizeEvidence.matchReason() + strictFields
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

func (s *Service) hasOneSidedChecksum(source, candidate *rls.Release) bool {
	if source == nil || candidate == nil {
		return false
	}
	normalizer := normalizerForService(s)
	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	return (sourceSum == "") != (candidateSum == "")
}

func reverseSearchCandidateInput(input searchCandidateInput) searchCandidateInput {
	input.Source, input.Candidate = input.Candidate, input.Source
	input.SourceTitles, input.CandidateTitles = input.CandidateTitles, input.SourceTitles
	return input
}

// oneSidedChecksumIsOnlyStrictDifference checks the normal matcher again after
// equalizing a missing CRC. This preserves strict matches in either checksum
// direction without weakening the exact-size fallback's group and resolution
// requirements for any additional mismatch.
func (s *Service) oneSidedChecksumIsOnlyStrictDifference(input searchCandidateInput) bool {
	if !s.hasOneSidedChecksum(input.Source.release, input.Candidate.release) {
		return false
	}
	replayInput, ok := s.withRelaxedDifferenceNeutralized(input, "checksum")
	if !ok {
		return false
	}
	matches, _ := s.releasesMatchWithReasonAndNamesAndTitles(
		replayInput.Source.release,
		replayInput.Candidate.release,
		replayInput.Source.rawName,
		replayInput.Candidate.rawName,
		replayInput.SourceTitles,
		replayInput.CandidateTitles,
		replayInput.FindIndividualEpisodes,
	)
	return matches
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
		return "exact reported size"
	case searchSizeEvidenceNone:
		return ""
	}
	return ""
}

// validateExactSizeSearchIdentity enforces identity attributes that exact size
// must never replace. It returns the first hard mismatch for search diagnostics.
func (s *Service) validateExactSizeSearchIdentity(input searchCandidateInput) (bool, string) {
	source := input.Source.release
	candidate := input.Candidate.release
	if source == nil || candidate == nil {
		return false, "missing parsed release"
	}

	if ok, reason := s.validateTitleArtistAndDates(
		source,
		candidate,
		input.Source.rawName,
		input.Candidate.rawName,
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
	if sourceIdentity == "" || candidateIdentity == "" ||
		(sourceIdentity != candidateIdentity &&
			!s.crossFieldGroupSiteFallback(input.Source, input.Candidate)) {
		return false, "group/site mismatch"
	}

	// A CRC32 tag rides on the anime file name, and indexer titles routinely drop
	// it, so one side alone carrying a checksum is absence of evidence. Two tags
	// that disagree still prove different files.
	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if sourceSum != "" && candidateSum != "" && sourceSum != candidateSum {
		return false, checksumMismatchReason
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
// mismatch categories explicitly observed in search or authorized by its cached
// decision. It removes one actual strict rejection at a time and returns only
// the differences it had to spend. This distinction matters when an indexer
// merely omitted a field: absence is not permission for the downloaded torrent
// to replace that field with a conflicting value.
func (s *Service) validateExactSizeFallback(input searchCandidateInput, mismatchReason string, allowedDifferences []string) ([]string, bool, string) {
	if ok, reason := s.validateExactSizeSearchIdentity(input); !ok {
		return nil, false, reason
	}
	return s.replayRelaxedDifferences(input, mismatchReason, allowedDifferences)
}

// replayRelaxedDifferences removes only the strict rejection categories search
// recorded, one at a time. Each removal exposes the next current rejection.
func (s *Service) replayRelaxedDifferences(input searchCandidateInput, mismatchReason string, allowedDifferences []string) ([]string, bool, string) {
	variantsCompatible, variantReason := checkVariantsCompatible(input.Source.release, input.Candidate.release)
	replayInput := input
	usedDifferences := make([]string, 0, len(allowedDifferences))
	for {
		difference, ok := exactSizeRelaxedDifferenceForReason(replayInput, mismatchReason)
		if !ok || !slices.Contains(allowedDifferences, difference) || slices.Contains(usedDifferences, difference) {
			return nil, false, mismatchReason
		}
		usedDifferences = append(usedDifferences, difference)

		replayInput, ok = s.withRelaxedDifferenceNeutralized(replayInput, difference)
		if !ok {
			return nil, false, "invalid recorded release difference"
		}
		matches, reason := s.releasesMatchWithReasonAndNamesAndTitles(
			replayInput.Source.release,
			replayInput.Candidate.release,
			replayInput.Source.rawName,
			replayInput.Candidate.rawName,
			replayInput.SourceTitles,
			replayInput.CandidateTitles,
			replayInput.FindIndividualEpisodes,
		)
		if matches {
			// Collection, cut, and edition can also carry variant tokens. Removing
			// their earlier metadata rejection must not erase an independently
			// failing IMAX/HYBRID/REPACK/PROPER rule from replay provenance.
			if !variantsCompatible && !slices.Contains(usedDifferences, "variant") {
				if !slices.Contains(allowedDifferences, "variant") {
					return nil, false, variantReason
				}
				usedDifferences = append(usedDifferences, "variant")
			}
			return usedDifferences, true, ""
		}
		mismatchReason = reason
	}
}

// searchCandidateMetadataConsistent binds replay to the values the indexer
// advertised. Apply may tolerate omitted tags and categories search already
// recorded, but an unrelated populated field cannot silently change while an
// earlier mismatch hides it.
func (s *Service) searchCandidateMetadataConsistent(
	advertisedName string,
	actual namedRelease,
	allowedDifferences []string,
	actualTitles []string,
	findIndividualEpisodes bool,
) (bool, string) {
	if advertisedName == "" || actual.release == nil {
		return true, ""
	}

	advertised := releases.DefaultParser.Parse(advertisedName)
	if advertised == nil {
		return false, "missing advertised release metadata"
	}
	actualRelease := *actual.release
	advertisedRelease := *advertised
	normalizer := normalizerForService(s)
	advertisedSum := normalizer.Normalize(advertisedRelease.Sum)
	actualSum := normalizer.Normalize(actualRelease.Sum)
	if advertisedSum != "" && actualSum != "" && advertisedSum != actualSum {
		return false, checksumMismatchReason
	}

	// Group provenance and checksum direction are validated against the bound
	// local source. Ignore them here so this check owns only advertised field
	// values that the asymmetric local comparison could otherwise miss.
	advertisedRelease.Group = ""
	advertisedRelease.Site = ""
	advertisedRelease.Sum = ""
	actualRelease.Group = ""
	actualRelease.Site = ""
	actualRelease.Sum = ""
	input := searchCandidateInput{
		Source: namedRelease{
			release: &advertisedRelease,
			rawName: advertisedName,
		},
		Candidate: namedRelease{
			release:   &actualRelease,
			rawName:   actual.rawName,
			tagOrigin: actual.tagOrigin,
		},
		CandidateTitles:        slices.Clone(actualTitles),
		FindIndividualEpisodes: findIndividualEpisodes,
	}
	matches, mismatchReason := s.releasesMatchWithReasonAndNamesAndTitles(
		input.Source.release,
		input.Candidate.release,
		input.Source.rawName,
		input.Candidate.rawName,
		input.SourceTitles,
		input.CandidateTitles,
		findIndividualEpisodes,
	)
	if matches {
		return true, ""
	}
	_, ok, reason := s.replayRelaxedDifferences(input, mismatchReason, allowedDifferences)
	return ok, reason
}

// withRelaxedDifferenceNeutralized removes one release-field rejection so the
// strict matcher can expose the next one. Callers iterate until strict matching
// succeeds or an unrecorded difference appears.
func (s *Service) withRelaxedDifferenceNeutralized(input searchCandidateInput, difference string) (searchCandidateInput, bool) {
	if input.Source.release == nil || input.Candidate.release == nil {
		return input, false
	}

	source := *input.Source.release
	candidate := *input.Candidate.release
	switch difference {
	case "source":
		candidate.Source = source.Source
	case "collection":
		candidate.Collection = source.Collection
	case "codec":
		candidate.Codec = slices.Clone(source.Codec)
	case "hdr":
		candidate.HDR = slices.Clone(source.HDR)
	case "bit-depth":
		candidate.BitDepth = source.BitDepth
	case "cut":
		candidate.Cut = slices.Clone(source.Cut)
	case "edition":
		candidate.Edition = slices.Clone(source.Edition)
	case "language":
		candidate.Language = slices.Clone(source.Language)
	case "version":
		candidate.Version = source.Version
	case "disc":
		candidate.Disc = source.Disc
	case "platform":
		candidate.Platform = source.Platform
	case "architecture":
		candidate.Arch = source.Arch
	case "checksum":
		candidate.Sum = source.Sum
	case "season":
		candidate.Series = source.Series
	case "episode":
		candidate.Episode = source.Episode
	case "group":
		normalizer := normalizerForService(s)
		if normalizedGroupSiteIdentity(s, &source) == normalizedGroupSiteIdentity(s, &candidate) {
			return input, false
		}
		identity, ok := s.crossFieldGroupSiteFallbackIdentity(input.Source, input.Candidate)
		if !ok {
			return input, false
		}
		source.Group = normalizer.Normalize(identity)
		source.Site = ""
		candidate.Group = source.Group
		candidate.Site = ""
	case "variant":
		candidate.Collection = source.Collection
		candidate.Other = slices.Clone(source.Other)
		candidate.Edition = slices.Clone(source.Edition)
		candidate.Cut = slices.Clone(source.Cut)
	default:
		return input, false
	}

	input.Source.release = &source
	input.Candidate.release = &candidate
	return input, true
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
		if isTVSeasonPack(input.Source.release) && isTVSeasonPack(input.Candidate.release) {
			return difference, true
		}
		return "", false
	case "episode":
		if isTVEpisode(input.Source.release) && isTVEpisode(input.Candidate.release) {
			return difference, true
		}
		return "", false
	// A group rejection means the two identities differ, which
	// validateExactSizeSearchIdentity has already re-derived the cross-field
	// evidence for. It runs first and returns early, so there is nothing left to
	// prove here.
	case "group":
		return difference, true
	case "source", "collection", "codec", "hdr", "bit-depth", "cut", "edition", "language", "version", "disc", "platform", "architecture", "checksum":
		return difference, true
	}

	compatible, variantReason := checkVariantsCompatible(input.Source.release, input.Candidate.release)
	if !compatible && strings.EqualFold(strings.TrimSpace(mismatchReason), variantReason) {
		return "variant", true
	}

	return "", false
}

// searchRelaxedStructure reports whether the strict rejection a search decision
// overrode was the season or episode number itself. Equal reported sizes cannot
// confirm which episode a torrent holds, so those pairings must be hashed before
// they seed. It keys on the causal rejection because that is the field which
// justified admission and determines the verification policy.
func searchRelaxedStructure(strictMismatchReason string) bool {
	switch normalizedMismatchReason(strictMismatchReason) {
	case seasonMismatchReason, episodeMismatchReason:
		return true
	}
	return false
}

// searchRelaxationRequiresVerification reports whether the strict rejection a
// search decision overrode was one that equal reported sizes cannot settle: the
// season or episode number, or a group identity admitted on cross-field
// evidence. Those pairings must be hashed before they seed.
func searchRelaxationRequiresVerification(strictMismatchReason string) bool {
	if searchRelaxedStructure(strictMismatchReason) {
		return true
	}
	return normalizedMismatchReason(strictMismatchReason) == groupMismatchReason
}

// candidateRequiresVerification reports whether search provenance for the
// selected local source requires the add to be hashed before it seeds. Exact-
// size and strict alternatives may be grouped in one candidate, so the stored
// relaxation must remain bound to the torrent that supplied its size evidence.
func candidateRequiresVerification(candidate CrossSeedCandidate, selectedHash string, req *CrossSeedRequest) bool {
	if candidate.titleRescue {
		return true
	}
	if req == nil || candidate.InstanceID != req.SearchDecision.SourceInstanceID {
		return false
	}
	sourceHash := normalizeHash(req.SearchDecision.SourceHash)
	return sourceHash != "" && normalizeHash(selectedHash) == sourceHash &&
		searchRelaxationRequiresVerification(req.SearchDecision.StrictMismatchReason)
}

// searchRelaxationAuthorizesCurrentReason reports whether apply may spend a
// search decision on the rejection it is looking at now. Search and apply can
// observe different independently recorded differences after file-derived TV
// structure is merged back into a torrent name. They may compose only within
// a compatible verification class: a soft rejection must never authorize a
// hard one without a hash check. A stored hard rejection may authorize a
// recorded soft difference because that pairing is still forced through the
// full verification the stored reason requires.
func searchRelaxationAuthorizesCurrentReason(storedReason, currentReason string) bool {
	if searchRelaxationRequiresVerification(currentReason) {
		return searchRelaxationRequiresVerification(storedReason)
	}
	return true
}

func normalizedMismatchReason(reason string) string {
	return strings.ToLower(strings.TrimSpace(reason))
}

// crossFieldGroupSiteFallback reports whether a group mismatch carries the
// marks of rls splitting one fansub name across two fields. A bracket-anime
// name puts the fansub tag in Site and leaves Group to the last unused word, so
// "[KIRI] Show S01 [...][Batch]" parses as Group "Batch" plus Site "KIRI" while
// the scene-styled counterpart "Show.S01...-KIRI" parses as Group "KIRI" with no
// site. The evidence is the cross-field agreement between the split side's Site
// and the tagged side's Group. A shared Site is never evidence: an indexer label
// such as [eztv] also lands in Site and is stamped on every group that tracker
// lists, so two of its listings agreeing there says nothing. Even then this is
// eligibility for verification, never proof of identity.
func (s *Service) crossFieldGroupSiteFallback(source, candidate namedRelease) bool {
	_, ok := s.crossFieldGroupSiteFallbackIdentity(source, candidate)
	return ok
}

func (s *Service) crossFieldGroupSiteFallbackIdentity(source, candidate namedRelease) (string, bool) {
	normalizer := normalizerForService(s)
	for _, sourceView := range s.groupIdentityViews(source) {
		for _, candidateView := range s.groupIdentityViews(candidate) {
			if s.splitGroupSiteMatchesTaggedGroup(sourceView, candidateView) {
				return normalizer.Normalize(sourceView.release.Site), true
			}
			if s.splitGroupSiteMatchesTaggedGroup(candidateView, sourceView) {
				return normalizer.Normalize(candidateView.release.Site), true
			}
		}
	}
	return "", false
}

// namedRelease keeps the public release fields, raw name, and selected-file
// tags as one identity view. Keeping the origin inside the view prevents a
// caller from passing file-derived fields while silently dropping provenance.
type namedRelease struct {
	release   *rls.Release
	rawName   string
	tagOrigin *rls.Release
}

// groupIdentityViews exposes both the selected-file-derived public view and the
// raw torrent/search name. File inference can repair TV structure while losing
// a Site field that only the raw bracket form carries. Explicit-group vetoes
// still inspect every origin through groupTagProvenance.
func (s *Service) groupIdentityViews(side namedRelease) []namedRelease {
	views := make([]namedRelease, 0, 2)
	views = append(views, side)
	if side.rawName == "" {
		return views
	}
	rawView := side
	rawView.release = releases.DefaultParser.Parse(side.rawName)
	normalizer := normalizerForService(s)
	currentGroup := ""
	if side.release != nil {
		currentGroup = normalizer.Normalize(side.release.Group)
	}
	if rawView.release == nil || currentGroup != "" &&
		currentGroup != normalizer.Normalize(rawView.release.Group) {
		return views
	}
	return append(views, rawView)
}

// splitGroupSiteMatchesTaggedGroup checks one orientation: split carries the
// fansub name in Site with a leftover word in Group, tagged carries that same
// name as a real release-group tag and nothing in Site.
func (s *Service) splitGroupSiteMatchesTaggedGroup(split, tagged namedRelease) bool {
	if split.release == nil || tagged.release == nil {
		return false
	}

	normalizer := normalizerForService(s)

	// The split side must name a group rls only guessed at, taking the word left
	// over once the real tags were consumed. A name that spells its group out
	// properly and still carries a site is an ordinary release under an indexer
	// label, and its group is not up for reinterpretation.
	//
	// The two names must also differ. A pack whose leftover word is the fansub
	// tag again parses as Group == Site, and reads as the same identity every
	// gate downstream already agrees on: relaxing it would record a difference
	// nothing was ever rejected for.
	splitGroup := normalizer.Normalize(split.release.Group)
	splitSite := normalizer.Normalize(split.release.Site)
	taggedGroup := normalizer.Normalize(tagged.release.Group)
	taggedSite := normalizer.Normalize(tagged.release.Site)
	if splitGroup == "" || splitSite == "" || splitGroup == splitSite ||
		taggedGroup != splitSite || taggedSite != "" {
		return false
	}

	splitProvenance := s.groupTagProvenance(split)
	if !splitProvenance.fallbackGroups.contains(splitGroup) ||
		splitProvenance.explicitGroups.contains(splitGroup) ||
		!splitProvenance.explicitGroups.onlyContains(splitSite) {
		return false
	}

	// The tagged side must spell that same site name as a real group tag, and
	// carry no label of its own to confuse it with.
	taggedProvenance := s.groupTagProvenance(tagged)
	return taggedProvenance.explicitGroups.contains(splitSite) &&
		taggedProvenance.explicitGroups.onlyContains(splitSite)
}

type normalizedIdentitySet map[string]struct{}

func (set normalizedIdentitySet) contains(identity string) bool {
	_, ok := set[identity]
	return ok
}

// onlyContains permits an empty set. Callers that require positive provenance
// pair it with contains.
func (set normalizedIdentitySet) onlyContains(identity string) bool {
	for value := range set {
		if value != identity {
			return false
		}
	}
	return true
}

type groupTagProvenance struct {
	explicitGroups normalizedIdentitySet
	fallbackGroups normalizedIdentitySet
}

func releaseHasExplicitGroupTag(release *rls.Release) bool {
	if release == nil {
		return false
	}
	for _, tag := range release.Tags() {
		if tag.Is(rls.TagTypeGroup) {
			return true
		}
	}
	return false
}

// groupTagProvenance combines every recorded origin of an enriched release. An
// explicit group from the release, its selected file, or its raw torrent/search
// name is a contradiction unless it agrees with the identity this fallback
// proposes.
func (s *Service) groupTagProvenance(side namedRelease) groupTagProvenance {
	provenance := groupTagProvenance{
		explicitGroups: make(normalizedIdentitySet),
		fallbackGroups: make(normalizedIdentitySet),
	}
	normalizer := normalizerForService(s)
	add := func(release *rls.Release) {
		if release == nil {
			return
		}
		for _, tag := range release.Tags() {
			identity := normalizer.Normalize(tag.Normalize())
			if identity == "" {
				continue
			}
			switch {
			case tag.Is(rls.TagTypeGroup):
				provenance.explicitGroups[identity] = struct{}{}
			case tag.Is(rls.TagTypeText):
				provenance.fallbackGroups[identity] = struct{}{}
			}
		}
	}

	add(side.release)
	add(side.tagOrigin)
	if side.rawName != "" {
		add(releases.DefaultParser.Parse(side.rawName))
	}
	return provenance
}

func (s *Service) explicitGroupsAgree(left, right namedRelease) bool {
	groups := s.groupTagProvenance(left).explicitGroups
	maps.Copy(groups, s.groupTagProvenance(right).explicitGroups)
	return len(groups) <= 1
}

// explicitGroupsFitFallbackIdentity rejects a file/raw-name group that differs
// from the cross-field identity search actually rescued. Cached decisions carry
// that identity so retitling cannot make a fallback word look authoritative.
func (s *Service) explicitGroupsFitFallbackIdentity(side namedRelease, expectedIdentity string) bool {
	if side.release == nil {
		return false
	}

	normalizer := normalizerForService(s)
	expected := normalizer.Normalize(expectedIdentity)
	identityVisible := false
	for _, view := range s.groupIdentityViews(side) {
		if view.release != nil && (expected == normalizer.Normalize(view.release.Group) ||
			expected == normalizer.Normalize(view.release.Site)) {
			identityVisible = true
			break
		}
	}
	if expected == "" || !identityVisible {
		return false
	}
	return s.groupTagProvenance(side).explicitGroups.onlyContains(expected)
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

// observedReleaseDifferences lists fields whose parsed values differ. It is an
// upper bound, not an authorization list: validateExactSizeFallback stores only
// the strict rejections it actually had to remove. This distinction prevents a
// field omitted by an indexer from authorizing a conflicting value after the
// torrent is downloaded.
//
// Group is the exception, and it is reported only when the two names carry the
// cross-field evidence of one fansub tag split across Group and Site. A plain
// disagreement between two groups leaves no entry. If strict matching spends
// this category, the evidence buys only a hash check, never a seed.
func (s *Service) observedReleaseDifferences(sourceSide, candidateSide namedRelease) []string {
	source := sourceSide.release
	candidate := candidateSide.release
	if source == nil || candidate == nil {
		return nil
	}

	normalizer := normalizerForService(s)
	var differences []string
	add := func(name, sourceValue, candidateValue string) {
		if sourceValue != candidateValue && !slices.Contains(differences, name) {
			differences = append(differences, name)
		}
	}

	add("source", normalizeSource(source.Source), normalizeSource(candidate.Source))
	add("collection", normalizer.Normalize(source.Collection), normalizer.Normalize(candidate.Collection))
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
	if s.crossFieldGroupSiteFallback(sourceSide, candidateSide) {
		differences = append(differences, "group")
	}
	if compatible, _ := checkVariantsCompatible(source, candidate); !compatible {
		differences = append(differences, "variant")
	}

	return differences
}
