// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"math/bits"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

type matchDifference uint8

const (
	differenceEmptyTitle matchDifference = iota
	differenceTitle
	differenceArtist
	differenceYear
	differenceDate
	differenceContentType
	differenceCandidateNotTV
	differenceSourceNotTV
	differenceSeason
	differencePackVersusEpisode
	differenceEpisode
	differenceGroup
	differenceGroupMissing
	differenceSite
	differenceChecksum
	differenceSource
	differenceResolution
	differenceCollection
	differenceCodec
	differenceHDR
	differenceBitDepth
	differenceCut
	differenceEdition
	differenceLanguage
	differenceVersion
	differenceDisc
	differencePlatform
	differenceArchitecture
	differenceVariant
)

var matchDifferenceText = [...]struct{ field, reason string }{
	differenceEmptyTitle:        {"title", "empty normalized title"},
	differenceTitle:             {"title", titleMismatchReason},
	differenceArtist:            {"artist", "artist mismatch"},
	differenceYear:              {"year", "year mismatch"},
	differenceDate:              {"date", "date mismatch"},
	differenceContentType:       {"content-type", "content type mismatch"},
	differenceCandidateNotTV:    {"tv", "candidate not recognized as TV"},
	differenceSourceNotTV:       {"tv", "source not recognized as TV"},
	differenceSeason:            {"season", seasonMismatchReason},
	differencePackVersusEpisode: {"pack-versus-episode", "season pack versus episode mismatch"},
	differenceEpisode:           {"episode", episodeMismatchReason},
	differenceGroup:             {"group", groupMismatchReason},
	differenceGroupMissing:      {"group", groupMismatchReason},
	differenceSite:              {"site", "site mismatch"},
	differenceChecksum:          {"checksum", checksumMismatchReason},
	differenceSource:            {"source", sourceMismatchReason},
	differenceResolution:        {"resolution", "resolution mismatch"},
	differenceCollection:        {"collection", "collection mismatch"},
	differenceCodec:             {"codec", "codec mismatch"},
	differenceHDR:               {"hdr", "hdr mismatch"},
	differenceBitDepth:          {"bit-depth", "bit depth mismatch"},
	differenceCut:               {"cut", "cut mismatch"},
	differenceEdition:           {"edition", "edition mismatch"},
	differenceLanguage:          {"language", "language mismatch"},
	differenceVersion:           {"version", "version mismatch"},
	differenceDisc:              {"disc", "disc mismatch"},
	differencePlatform:          {"platform", "platform mismatch"},
	differenceArchitecture:      {"architecture", "architecture mismatch"},
	differenceVariant:           {"variant", ""},
}

type matchDifferences uint64

func differencesOf(differences ...matchDifference) matchDifferences {
	var set matchDifferences
	for _, difference := range differences {
		set |= 1 << difference
	}
	return set
}

func (set matchDifferences) needsVerification() bool {
	return set&differencesOf(differenceSeason, differenceEpisode, differenceGroup) != 0
}

func (set matchDifferences) has(difference matchDifference) bool {
	return set&(1<<difference) != 0
}

func (set matchDifferences) without(differences ...matchDifference) matchDifferences {
	return set &^ differencesOf(differences...)
}

func (set matchDifferences) first() matchDifference {
	return matchDifference(bits.TrailingZeros64(uint64(set)))
}

func (set matchDifferences) each(yield func(matchDifference) bool) {
	for rest := set; rest != 0; rest &= rest - 1 {
		if !yield(matchDifference(bits.TrailingZeros64(uint64(rest)))) {
			return
		}
	}
}

type releaseComparison struct {
	differences   matchDifferences
	variantReason string
}

func (c *releaseComparison) add(difference matchDifference) {
	c.differences |= 1 << difference
}

func (c releaseComparison) matched() bool {
	return c.differences == 0
}

func (c releaseComparison) reason() string {
	if c.matched() {
		return ""
	}
	return c.reasonOf(c.differences.first())
}

func (c releaseComparison) reasonOf(difference matchDifference) string {
	if difference == differenceVariant {
		return c.variantReason
	}
	return matchDifferenceText[difference].reason
}

type matchEvidence struct {
	Catalog         catalogFileEvidence
	SourceSize      int64
	CandidateSize   int64
	SourceFiles     qbt.TorrentFiles
	CandidateFiles  qbt.TorrentFiles
	SourceTitles    []string
	CandidateTitles []string
	EpisodeMap      *models.EpisodeMap
}

func (evidence matchEvidence) sizes() (source, candidate int64) {
	return cmp.Or(evidence.SourceSize, sourceFilesTotalSize(evidence.SourceFiles)),
		cmp.Or(evidence.CandidateSize, sourceFilesTotalSize(evidence.CandidateFiles))
}

type matchPolicy struct {
	CatalogFiles               bool
	TitleRescue                bool
	IndividualEpisodes         bool
	SizeTolerancePercent       float64
	AllowMissingCandidateGroup bool
	SkipSizeCheck              bool
	SeasonPack                 seasonPackMatchOptions
}

type matchVerdict struct {
	Matched               bool
	Class                 searchCandidateClass
	Strict                releaseComparison
	Relaxed               matchDifferences
	GroupFallbackIdentity string
	MappedEpisode         string
	RejectReason          string
	SizeRejected          bool
}

func (verdict matchVerdict) relaxedFields() []string {
	var fields []string
	for difference := range verdict.Relaxed.each {
		fields = append(fields, matchDifferenceText[difference].field)
	}
	return fields
}

func (m matcher) matchVerdict(source, candidate namedRelease, evidence matchEvidence, policy matchPolicy) matchVerdict {
	if policy.CatalogFiles {
		return m.catalogVerdict(source, candidate, evidence)
	}
	source, candidate, mappedEpisode := applyEpisodeMap(policy.SeasonPack.apply(source), policy.SeasonPack.apply(candidate), evidence.EpisodeMap)
	verdict := matchVerdict{Class: searchCandidateClassRejected, MappedEpisode: mappedEpisode}
	sourceSize, candidateSize := evidence.sizes()
	exactSize := positiveExactSize(sourceSize, candidateSize)
	ignoreSizeCheck := policy.IndividualEpisodes &&
		isTVSeasonPack(source.release) && isTVEpisode(candidate.release)

	stopAtFirst := (sourceSize <= 0 || candidateSize <= 0) && !policy.AllowMissingCandidateGroup
	strict := m.compareReleases(source, candidate, evidence.SourceTitles, evidence.CandidateTitles, policy.IndividualEpisodes, stopAtFirst)
	if policy.AllowMissingCandidateGroup {
		strict.differences = strict.differences.without(differenceGroupMissing)
	}
	verdict.Strict = strict

	class := searchCandidateClassStrict
	switch {
	case strict.matched():
	case isWebSourceRelabel(source, candidate, strict) &&
		sourceSize > 0 && candidateSize > 0 &&
		(ignoreSizeCheck || m.isSizeWithinTolerance(sourceSize, candidateSize, policy.SizeTolerancePercent)):
		class = searchCandidateClassWebSourceRelabel
	case policy.TitleRescue && strict.differences.first() == differenceTitle && exactSize:
		if ok, reason := m.releasesMatchExceptTitleWithReason(source, candidate, policy.IndividualEpisodes); !ok {
			verdict.RejectReason = reason
			return verdict
		}
		class = searchCandidateClassTitleRescue
	case exactSize:
		if ok, reason := m.validateExactSizeSearchIdentity(searchCandidateInput{
			Source:                     source,
			Candidate:                  candidate,
			SourceTitles:               evidence.SourceTitles,
			CandidateTitles:            evidence.CandidateTitles,
			AllowMissingCandidateGroup: policy.AllowMissingCandidateGroup,
		}); !ok {
			verdict.RejectReason = reason
			return verdict
		}
		for difference := range strict.differences.each {
			if !m.exactSizeRelaxes(difference, source, candidate, evidence) {
				verdict.RejectReason = strict.reasonOf(difference)
				return verdict
			}
		}
		class = searchCandidateClassExactSizeFallback
		verdict.Relaxed = strict.differences
		if strict.differences.has(differenceGroup) {
			verdict.GroupFallbackIdentity, _ = m.crossFieldGroupSiteFallbackIdentity(source, candidate)
		}
	default:
		verdict.RejectReason = strict.reason()
		return verdict
	}
	verdict.Class = class

	if reject, reason := rejectSeasonPackFromEpisode(candidate.release, source.release); reject {
		verdict.RejectReason = reason
		return verdict
	}

	if !ignoreSizeCheck && !policy.SkipSizeCheck && !m.isSizeWithinTolerance(sourceSize, candidateSize, policy.SizeTolerancePercent) {
		verdict.RejectReason = "size mismatch"
		verdict.SizeRejected = true
		return verdict
	}

	verdict.Matched = true
	return verdict
}

func isWebSourceRelabel(source, candidate namedRelease, strict releaseComparison) bool {
	return strict.differences == differencesOf(differenceSource) &&
		isWebSource(normalizeSource(source.release.Source)) &&
		isWebSource(normalizeSource(candidate.release.Source))
}

func (m matcher) titlesMatchInSourceSeason(source, candidate namedRelease, evidence matchEvidence) bool {
	moved := *candidate.release
	moved.Series = source.release.Series
	var comparison releaseComparison
	m.compareTitles(&comparison, source.release, &moved, source.rawName, candidate.rawName, evidence.SourceTitles, evidence.CandidateTitles)
	return comparison.matched()
}

func (m matcher) exactSizeRelaxes(difference matchDifference, source, candidate namedRelease, evidence matchEvidence) bool {
	switch difference {
	case differenceSeason:
		return isTVSeasonPack(source.release) && isTVSeasonPack(candidate.release) &&
			m.titlesMatchInSourceSeason(source, candidate, evidence)
	case differenceEpisode:
		return isTVEpisode(source.release) && isTVEpisode(candidate.release)
	case differenceGroup:
		return m.normalizedGroupSiteIdentity(source.release) != m.normalizedGroupSiteIdentity(candidate.release) &&
			m.crossFieldGroupSiteFallback(source, candidate)
	default:
		return encodeDifferences.has(difference)
	}
}

var encodeDifferences = differencesOf(
	differenceSource, differenceCollection, differenceCodec, differenceHDR,
	differenceBitDepth, differenceCut, differenceEdition, differenceLanguage, differenceVersion,
	differenceDisc, differencePlatform, differenceArchitecture, differenceVariant,
)
