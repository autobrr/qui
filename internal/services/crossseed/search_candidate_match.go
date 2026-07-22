// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"strings"

	"github.com/moistari/rls"
)

type searchCandidateClass string

const (
	searchCandidateClassRejected          searchCandidateClass = "rejected"
	searchCandidateClassStrict            searchCandidateClass = "strict"
	searchCandidateClassWebSourceRelabel  searchCandidateClass = "web-source-relabel"
	searchCandidateClassExactSizeFallback searchCandidateClass = "exact-size-fallback"
)

type searchSizeEvidence string

const (
	searchSizeEvidenceNone  searchSizeEvidence = "none"
	searchSizeEvidenceExact searchSizeEvidence = "exact"
)

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
}

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
	case decision.SizeEvidence.matches():
		if ok, reason := s.validateExactSizeSearchIdentity(input); ok {
			class = searchCandidateClassExactSizeFallback
			decision.RelaxedDifferences = softMetadataDifferences(input.SourceRelease, input.CandidateRelease)
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
	case searchCandidateClassExactSizeFallback:
		decision.Score += sizeEvidenceFallbackScoreBonus
		decision.MatchReason = decision.SizeEvidence.matchReason() + "; strict title/season/resolution/group"
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

func (s *Service) validateExactSizeSearchIdentity(input searchCandidateInput) (bool, string) {
	source := input.SourceRelease
	candidate := input.CandidateRelease
	if source == nil || candidate == nil {
		return false, "missing parsed release"
	}

	isTV := isTVRelease(source) || isTVRelease(candidate)
	if ok, reason := s.validateTitleArtistAndDates(
		source,
		candidate,
		input.SourceName,
		input.CandidateName,
		input.SourceTitles,
		input.CandidateTitles,
		isTV,
	); !ok {
		return false, reason
	}
	if ok, reason := validateTVStructure(source, candidate, input.FindIndividualEpisodes, isTV); !ok {
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

	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if (sourceSum != "" || candidateSum != "") && sourceSum != candidateSum {
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

func softMetadataDifferences(source, candidate *rls.Release) []string {
	if source == nil || candidate == nil {
		return nil
	}

	normalizer := normalizerForService(nil)
	differences := make([]string, 0, 12)
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
	if compatible, _ := checkVariantsCompatible(source, candidate); !compatible {
		differences = append(differences, "variant")
	}

	return differences
}
