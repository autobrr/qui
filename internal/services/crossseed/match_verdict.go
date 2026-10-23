// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"math/bits"

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
	// differenceChecksumMissing is a source CRC tag that the candidate does not carry.
	differenceChecksumMissing
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
	differenceChecksumMissing:   {"checksum", checksumMismatchReason},
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
	SourceSize      int64
	CandidateSize   int64
	SourceTitles    []string
	CandidateTitles []string
	// EpisodeMap is the Sonarr triple for the release qui looked up. It equates
	// a seasonless episode with its seasoned counterpart on the other side, see
	// applyEpisodeMap.
	EpisodeMap *models.EpisodeMap
}

// matchPolicy is the switches that a Match path sets on the Match verdict
// (GLOSSARY.md).
type matchPolicy struct {
	TitleRescue          bool
	IndividualEpisodes   bool
	SizeTolerancePercent float64
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
	Relaxed matchDifferences
	// OneSidedChecksum is a CRC tag on one name only, with exact sizes.
	OneSidedChecksum bool
	// ChecksumOnly means that the one-sided CRC tag is the only difference.
	ChecksumOnly          bool
	GroupFallbackIdentity string
	// StrictChecksumReplay marks a strict match with a one-sided CRC tag that
	// also matches in reverse once the tag is equal. Apply compares in reverse.
	StrictChecksumReplay bool
	// MappedEpisode names the episode map equality that the verdict used.
	MappedEpisode string
	RejectReason  string
	SizeRejected  bool
}

// relaxedFields lists the fields of the exact size class in check order. A
// CRC tag on the candidate only is not a strict difference, so it comes last.
func (verdict matchVerdict) relaxedFields() []string {
	var fields []string
	for difference := range verdict.Relaxed.each {
		fields = append(fields, matchDifferenceText[difference].field)
	}
	if verdict.OneSidedChecksum && !verdict.Relaxed.has(differenceChecksumMissing) {
		fields = append(fields, "checksum")
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
	source, candidate, mappedEpisode := applyEpisodeMap(source, candidate, evidence.EpisodeMap)
	verdict := matchVerdict{Class: searchCandidateClassRejected, MappedEpisode: mappedEpisode}
	exactSize := positiveExactSize(evidence.SourceSize, evidence.CandidateSize)
	ignoreSizeCheck := policy.IndividualEpisodes &&
		isTVSeasonPack(source.release) && isTVEpisode(candidate.release)

	strict := m.compareReleases(source, candidate, evidence.SourceTitles, evidence.CandidateTitles, policy.IndividualEpisodes, false)
	verdict.Strict = strict
	verdict.OneSidedChecksum = exactSize && m.hasOneSidedChecksum(source.release, candidate.release)

	class := searchCandidateClassStrict
	switch {
	case strict.matched():
		// Apply compares the pair in reverse, where a CRC tag on the local name
		// only is a strict difference.
		verdict.StrictChecksumReplay = verdict.OneSidedChecksum &&
			m.compareReleases(candidate, source, evidence.CandidateTitles, evidence.SourceTitles, policy.IndividualEpisodes, false).
				differences.without(differenceChecksumMissing) == 0
	// A one-sided CRC tag with a source difference goes to exact size, which
	// records the checksum.
	case (!verdict.OneSidedChecksum || strict.differences.first() != differenceSource) &&
		isWebSourceRelabel(source, candidate, strict) &&
		(ignoreSizeCheck || m.isSizeWithinTolerance(evidence.SourceSize, evidence.CandidateSize, policy.SizeTolerancePercent)):
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
		if verdict.OneSidedChecksum && strict.differences == differencesOf(differenceChecksumMissing) {
			verdict.ChecksumOnly = true
		} else {
			if ok, reason := m.validateExactSizeSearchIdentity(searchCandidateInput{
				Source:          source,
				Candidate:       candidate,
				SourceTitles:    evidence.SourceTitles,
				CandidateTitles: evidence.CandidateTitles,
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
	if reject, reason := rejectSeasonPackFromEpisode(candidate.release, source.release, true); reject {
		verdict.RejectReason = reason
		return verdict
	}

	if !ignoreSizeCheck && !m.isSizeWithinTolerance(evidence.SourceSize, evidence.CandidateSize, policy.SizeTolerancePercent) {
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
	// validateExactSizeSearchIdentity already rejects two CRC tags that conflict.
	case differenceChecksumMissing,
		differenceSource, differenceCollection, differenceCodec, differenceHDR, differenceBitDepth,
		differenceCut, differenceEdition, differenceLanguage, differenceVersion, differenceDisc,
		differencePlatform, differenceArchitecture, differenceVariant:
		return true
	default:
		return false
	}
}
