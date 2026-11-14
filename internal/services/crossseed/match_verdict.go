// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"math/bits"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// matchDifference is one release rule that a pair of Read releases fails. The
// constants follow the order of the checks in compareReleases, so the first
// difference of a set gives the reason that the strict compare reports.
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
	// differenceGroupMissing is a source group that the candidate does not carry.
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
	// differenceVariant takes its reason from the variant tag, see releaseComparison.
	differenceVariant
)

// matchDifferenceText gives the field name that a search decision records and
// the reason that the cross-seed dialog and the logs show.
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

// matchDifferences is a set of matchDifference values.
type matchDifferences uint64

func differencesOf(differences ...matchDifference) matchDifferences {
	var set matchDifferences
	for _, difference := range differences {
		set |= 1 << difference
	}
	return set
}

// needsVerification reports whether the set holds a difference that equal
// reported sizes cannot settle: the season or episode number, or one group
// that is split across Group and Site.
func (set matchDifferences) needsVerification() bool {
	return set&differencesOf(differenceSeason, differenceEpisode, differenceGroup) != 0
}

func (set matchDifferences) has(difference matchDifference) bool {
	return set&(1<<difference) != 0
}

func (set matchDifferences) without(differences ...matchDifference) matchDifferences {
	return set &^ differencesOf(differences...)
}

// first is the first difference in check order. The set must not be empty.
func (set matchDifferences) first() matchDifference {
	return matchDifference(bits.TrailingZeros64(uint64(set)))
}

// each yields the differences in check order.
func (set matchDifferences) each(yield func(matchDifference) bool) {
	for rest := set; rest != 0; rest &= rest - 1 {
		if !yield(matchDifference(bits.TrailingZeros64(uint64(rest)))) {
			return
		}
	}
}

// releaseComparison is the result of compareReleases: every rule that the pair
// fails.
type releaseComparison struct {
	differences matchDifferences
	// variantReason names the variant tag that differs, such as "IMAX".
	variantReason string
}

func (c *releaseComparison) add(difference matchDifference) {
	c.differences |= 1 << difference
}

func (c releaseComparison) matched() bool {
	return c.differences == 0
}

// reason is the reason of the first difference, or "" for a match.
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

// matchEvidence is what a Match path knows about a pair beyond the two Read
// releases (GLOSSARY.md). A path that lacks a field leaves it empty.
type matchEvidence struct {
	SourceSize    int64
	CandidateSize int64
	// SourceFiles and CandidateFiles are the files of the pair. The verdict reads
	// them only as sizes: a side with no size takes the total size of its files.
	SourceFiles     qbt.TorrentFiles
	CandidateFiles  qbt.TorrentFiles
	SourceTitles    []string
	CandidateTitles []string
	// EpisodeMap is the Sonarr triple for the release qui looked up. It equates
	// a seasonless episode with its seasoned counterpart on the other side, see
	// applyEpisodeMap.
	EpisodeMap *models.EpisodeMap
}

// sizes gives the size of each side, from the files when the side has no size.
func (evidence matchEvidence) sizes() (source, candidate int64) {
	return cmp.Or(evidence.SourceSize, sourceFilesTotalSize(evidence.SourceFiles)),
		cmp.Or(evidence.CandidateSize, sourceFilesTotalSize(evidence.CandidateFiles))
}

// matchPolicy is the switches that a Match path sets on the Match verdict
// (GLOSSARY.md).
type matchPolicy struct {
	TitleRescue          bool
	IndividualEpisodes   bool
	SizeTolerancePercent float64
	// AllowMissingCandidateGroup lets a group or site that only the source carries
	// pass. The unknown-size preflight of the webhook check and the season pack
	// check set it.
	AllowMissingCandidateGroup bool
	// SkipSizeCheck drops the size check after the match classes. The first
	// pass of the season pack check, the batch matcher, and the content
	// prefilter set it, because the file check after each of them
	// compares the file sizes. Dedup sets it because it has no sizes.
	SkipSizeCheck bool
	// SeasonPack holds the season pack matching settings. Only the season pack
	// check sets it.
	SeasonPack seasonPackMatchOptions
}

// matchVerdict is the decision of one Match path on a pair of Read releases
// (GLOSSARY.md).
type matchVerdict struct {
	Matched bool
	// Class is the rule that admitted the pair, or searchCandidateClassRejected.
	// A pair that a class admits can still fail the season pack rule and the
	// size check, and then Matched is false.
	Class searchCandidateClass
	// Strict is the strict compare of the pair after the episode map. It stops
	// after a title difference.
	Strict releaseComparison
	// Relaxed is the set of differences that the exact size class accepted.
	Relaxed               matchDifferences
	GroupFallbackIdentity string
	// MappedEpisode names the episode map equality that the verdict used.
	MappedEpisode string
	RejectReason  string
	SizeRejected  bool
}

// relaxedFields lists the fields of the exact size class in check order.
func (verdict matchVerdict) relaxedFields() []string {
	var fields []string
	for difference := range verdict.Relaxed.each {
		fields = append(fields, matchDifferenceText[difference].field)
	}
	return fields
}

// matchVerdict decides one pair. The source is the local torrent, and the
// candidate is the torrent that qui would add.
//
// Exact size admits a pair that the strict compare rejects when equal
// reported sizes back it: title, resolution, artist, and date stay strict, the
// CRC tags must not conflict, and the season or episode number may differ only
// between two packs or two episodes. A group differs only when one fansub tag
// is split across Group and Site, and that pair needs a full hash check.
func (m matcher) matchVerdict(source, candidate namedRelease, evidence matchEvidence, policy matchPolicy) matchVerdict {
	source, candidate, mappedEpisode := applyEpisodeMap(policy.SeasonPack.apply(source), policy.SeasonPack.apply(candidate), evidence.EpisodeMap)
	verdict := matchVerdict{Class: searchCandidateClassRejected, MappedEpisode: mappedEpisode}
	sourceSize, candidateSize := evidence.sizes()
	exactSize := positiveExactSize(sourceSize, candidateSize)
	ignoreSizeCheck := policy.IndividualEpisodes &&
		isTVSeasonPack(source.release) && isTVEpisode(candidate.release)

	// Only the relabel and the exact size class read past the first difference,
	// and both need two sizes. A path without them, such as dedup, stops at the
	// first group of rules that differs. A missing group that the policy allows
	// is removed after the compare, so it must not stop the compare early.
	stopAtFirst := (sourceSize <= 0 || candidateSize <= 0) && !policy.AllowMissingCandidateGroup
	strict := m.compareReleases(source, candidate, evidence.SourceTitles, evidence.CandidateTitles, policy.IndividualEpisodes, stopAtFirst)
	if policy.AllowMissingCandidateGroup {
		strict.differences = strict.differences.without(differenceGroupMissing)
	}
	verdict.Strict = strict

	class := searchCandidateClassStrict
	switch {
	case strict.matched():
	// The relabel needs both sizes: a path without them, such as apply for
	// another local torrent, keeps the strict compare.
	case isWebSourceRelabel(source, candidate, strict) &&
		sourceSize > 0 && candidateSize > 0 &&
		(ignoreSizeCheck || m.isSizeWithinTolerance(sourceSize, candidateSize, policy.SizeTolerancePercent)):
		class = searchCandidateClassWebSourceRelabel
	case policy.TitleRescue && strict.differences.first() == differenceTitle && exactSize:
		// The titles differ, so a Season numeral in one title says nothing about
		// the other name, and this compare reads no numeral season.
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

	// Only a Manual match skips this rule, and it does not ask the verdict.
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

// isWebSourceRelabel reports whether the web source label (WEBRip against
// WEB-DL) is the only difference. Trackers relabel the same web encode, so the
// pair goes on to the file check and the recheck at apply.
func isWebSourceRelabel(source, candidate namedRelease, strict releaseComparison) bool {
	return strict.differences == differencesOf(differenceSource) &&
		isWebSource(normalizeSource(source.release.Source)) &&
		isWebSource(normalizeSource(candidate.release.Source))
}

// titlesMatchInSourceSeason reports whether the titles still match when the
// candidate takes the source season. A Season numeral that names the season
// of the candidate, as in "Show III S03", is a second season marker, and
// equal sizes do not overrule it.
func (m matcher) titlesMatchInSourceSeason(source, candidate namedRelease, evidence matchEvidence) bool {
	moved := *candidate.release
	moved.Series = source.release.Series
	var comparison releaseComparison
	m.compareTitles(&comparison, source.release, &moved, source.rawName, candidate.rawName, evidence.SourceTitles, evidence.CandidateTitles)
	return comparison.matched()
}

// exactSizeRelaxes reports whether equal reported sizes may stand in for one
// strict difference. Season and episode are the identity fields that indexers
// rewrite: a tracker with one entry per cour stamps S01 on every season, and
// absolute numbering meets renumbered candidates. Only a like-for-like shape
// may retire that number.
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

// encodeDifferences describe the encode, not the content, so equal reported
// sizes may stand in for them. validateExactSizeSearchIdentity already rejects
// two CRC tags that conflict.
var encodeDifferences = differencesOf(
	differenceSource, differenceCollection, differenceCodec, differenceHDR,
	differenceBitDepth, differenceCut, differenceEdition, differenceLanguage, differenceVersion,
	differenceDisc, differencePlatform, differenceArchitecture, differenceVariant,
)
