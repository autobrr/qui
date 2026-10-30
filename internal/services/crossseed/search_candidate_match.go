// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/autobrr/rls"

	"github.com/autobrr/qui/internal/models"
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
	// searchCandidateClassHybridTolerance means a HYBRID tag on one side was the
	// only strict difference and the reported sizes are within the size
	// tolerance. The file comparison after the download decides.
	searchCandidateClassHybridTolerance searchCandidateClass = "hybrid-tolerance"
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
// search classification and alternate-query checks.
type searchCandidateInput struct {
	Source          namedRelease
	Candidate       namedRelease
	SourceTitles    []string
	CandidateTitles []string
	// EpisodeMap is the Sonarr triple for the release qui looked up (the search
	// source or the announce). It equates a seasonless episode with its seasoned
	// counterpart on the other side; see applyEpisodeMap.
	EpisodeMap                 *models.EpisodeMap
	SourceSize                 int64
	CandidateSize              int64
	TolerancePercent           float64
	FindIndividualEpisodes     bool
	RescueTitleMismatches      bool
	AllowMissingCandidateGroup bool
}

// searchCandidateDecision records admission provenance, including which strict
// mismatch exact-size evidence relaxed. Rejected decisions never grant apply
// access to the release-prefilter bypass.
type searchCandidateDecision struct {
	Accepted     bool
	Class        searchCandidateClass
	SizeEvidence searchSizeEvidence
	// SearchCandidateName is the listing that search judged.
	SearchCandidateName   string
	GroupFallbackIdentity string
	SourceTitles          []string
	CandidateTitles       []string
	EpisodeMap            *models.EpisodeMap
	RejectReason          string
	StrictMismatchReason  string
	RelaxedDifferences    []string
	// Relaxed is the set of differences that the exact size class accepted.
	Relaxed      matchDifferences
	Score        float64
	MatchReason  string
	SizeRejected bool
}

// searchDecisionProvenance is the private part of a search decision that apply
// keeps. It travels as one value from classification through cached results
// to apply so adding a safety field cannot leave one transport path behind.
type searchDecisionProvenance struct {
	Class searchCandidateClass
	// SearchCandidateName lets apply reject new metadata in the downloaded name.
	SearchCandidateName string
	SourceInstanceID    int
	SourceHash          string
	// Relaxed is the set of differences that the exact size class accepted.
	// Verification reads it.
	Relaxed               matchDifferences
	GroupFallbackIdentity string
	SourceTitles          []string
	CandidateTitles       []string
	EpisodeMap            *models.EpisodeMap
}

func (decision searchCandidateDecision) provenance() searchDecisionProvenance {
	return searchDecisionProvenance{
		Class:                 decision.Class,
		SearchCandidateName:   decision.SearchCandidateName,
		Relaxed:               decision.Relaxed,
		GroupFallbackIdentity: decision.GroupFallbackIdentity,
		SourceTitles:          slices.Clone(decision.SourceTitles),
		CandidateTitles:       slices.Clone(decision.CandidateTitles),
		EpisodeMap:            decision.EpisodeMap,
	}
}

func (provenance searchDecisionProvenance) clone() searchDecisionProvenance {
	provenance.SourceTitles = slices.Clone(provenance.SourceTitles)
	provenance.CandidateTitles = slices.Clone(provenance.CandidateTitles)
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
		decision.Relaxed.needsVerification()
}

// Score bands mirror the explicit sorter: positive size evidence outranks the
// release scorer's tolerance-only range, and stricter classes display a higher
// score than relaxed classes within the same evidence tier.
const (
	sizeEvidenceFallbackScoreBonus = 2.0
	sizeEvidenceRelabelScoreBonus  = 3.0
	sizeEvidenceStrictScoreBonus   = 4.0
)

// classifySearchCandidate asks the Match verdict about one search result and
// adds the score and the provenance that apply keeps.
func (m matcher) classifySearchCandidate(input searchCandidateInput) searchCandidateDecision {
	evidence := matchEvidence{
		SourceSize:      input.SourceSize,
		CandidateSize:   input.CandidateSize,
		SourceTitles:    input.SourceTitles,
		CandidateTitles: input.CandidateTitles,
		EpisodeMap:      input.EpisodeMap,
	}
	policy := matchPolicy{
		TitleRescue:                input.RescueTitleMismatches,
		IndividualEpisodes:         input.FindIndividualEpisodes,
		SizeTolerancePercent:       input.TolerancePercent,
		AllowMissingCandidateGroup: input.AllowMissingCandidateGroup,
	}
	verdict := m.hybridToleranceVerdict(input, evidence, policy, m.matchVerdict(input.Source, input.Candidate, evidence, policy))
	decision := searchCandidateDecision{
		Class:                 searchCandidateClassRejected,
		SizeEvidence:          classifySearchSizeEvidence(input.SourceSize, input.CandidateSize),
		SearchCandidateName:   input.Candidate.rawName,
		GroupFallbackIdentity: verdict.GroupFallbackIdentity,
		RejectReason:          verdict.RejectReason,
		StrictMismatchReason:  verdict.Strict.reason(),
		SizeRejected:          verdict.SizeRejected,
	}
	if verdict.Class == searchCandidateClassExactSizeFallback {
		decision.RelaxedDifferences = verdict.relaxedFields()
		decision.Relaxed = verdict.Relaxed
	}
	if verdict.Class == searchCandidateClassHybridTolerance {
		decision.RelaxedDifferences = []string{"variant"}
	}
	if !verdict.Matched {
		return decision
	}

	source, candidate, _ := applyEpisodeMap(input.Source, input.Candidate, input.EpisodeMap)
	decision.Accepted = true
	decision.Class = verdict.Class
	decision.SourceTitles = slices.Clone(input.SourceTitles)
	decision.CandidateTitles = slices.Clone(input.CandidateTitles)
	decision.EpisodeMap = input.EpisodeMap
	decision.Score, decision.MatchReason = evaluateReleaseMatch(source.release, candidate.release)
	if decision.Score <= 0 {
		decision.Score = 1
	}
	switch decision.Class {
	case searchCandidateClassTitleRescue:
		decision.MatchReason = "Title rescue · full check required"
	case searchCandidateClassExactSizeFallback:
		decision.Score += sizeEvidenceFallbackScoreBonus
		strictFields := "; strict title/resolution/group"
		if slices.Contains(decision.RelaxedDifferences, "group") {
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
	case searchCandidateClassHybridTolerance:
		decision.MatchReason = "one-sided HYBRID within size tolerance; " + decision.MatchReason
	case searchCandidateClassRejected:
	}
	if verdict.MappedEpisode != "" {
		decision.MatchReason = verdict.MappedEpisode + "; " + decision.MatchReason
	}

	return decision
}

// hybridToleranceVerdict lets search pass a pair whose only strict difference
// is a HYBRID tag on one side, when the reported sizes are within the size
// tolerance. The file comparison after the download decides. Any other strict
// difference, IMAX included, still rejects the pair. Only search asks for this
// verdict, so the other Match paths keep HYBRID strict.
func (m matcher) hybridToleranceVerdict(input searchCandidateInput, evidence matchEvidence, policy matchPolicy, verdict matchVerdict) matchVerdict {
	// isSizeWithinTolerance accepts two zero sizes, so require positive sizes.
	if verdict.Class != searchCandidateClassRejected || verdict.Strict.reason() != "HYBRID" ||
		input.SourceSize <= 0 || input.CandidateSize <= 0 {
		return verdict
	}
	hybrid := m.matchVerdict(withoutHybrid(input.Source), withoutHybrid(input.Candidate), evidence, policy)
	if hybrid.Class != searchCandidateClassStrict {
		return verdict
	}
	// The second verdict checks the size tolerance, and skips it where a strict
	// match skips it.
	if hybrid.SizeRejected {
		gap := math.Abs(float64(input.CandidateSize-input.SourceSize)) / float64(input.SourceSize) * 100
		verdict.RejectReason = fmt.Sprintf("%s (size differs by %.2f%%)", verdict.Strict.reason(), gap)
		return verdict
	}
	hybrid.Class = searchCandidateClassHybridTolerance
	hybrid.Strict = verdict.Strict
	return hybrid
}

func withoutHybrid(read namedRelease) namedRelease {
	release := *read.release
	release.Other = slices.DeleteFunc(slices.Clone(release.Other), func(value string) bool {
		return variantValueMatches(normalizeVariant(value), "HYBRID")
	})
	read.release = &release
	return read
}

// applyEpisodeMap rewrites the seasonless side of a mixed-scheme pair to the
// mapped season and episode when the map equates the two: the seasonless side
// carries the map's absolute number and the seasoned side carries its season
// and episode. The strict matcher then sees a same-scheme pair. Any other pair
// is left alone, so same-scheme pairs and a tracker that numbers the episode
// differently from Sonarr keep today's episode mismatch. The returned reason
// names the equality for the decision trace.
func applyEpisodeMap(source, candidate namedRelease, episodeMap *models.EpisodeMap) (mappedSource, mappedCandidate namedRelease, reason string) {
	// A special maps to season 0, which would let a seasonless episode pose as
	// the seasoned side; only a positive triple names two distinct schemes.
	if episodeMap == nil || episodeMap.Season <= 0 || episodeMap.Episode <= 0 || episodeMap.Absolute <= 0 ||
		source.release == nil || candidate.release == nil {
		return source, candidate, ""
	}
	seasonless := func(r *rls.Release) bool { return r.Series == 0 && r.Episode == episodeMap.Absolute }
	seasoned := func(r *rls.Release) bool { return r.Series == episodeMap.Season && r.Episode == episodeMap.Episode }
	var side *namedRelease
	switch {
	case seasonless(source.release) && seasoned(candidate.release):
		side = &source
	case seasonless(candidate.release) && seasoned(source.release):
		side = &candidate
	default:
		return source, candidate, ""
	}
	mapped := *side.release
	mapped.Series = episodeMap.Season
	mapped.Episode = episodeMap.Episode
	side.release = &mapped
	return source, candidate, fmt.Sprintf("mapped episode %d = S%02dE%02d", episodeMap.Absolute, episodeMap.Season, episodeMap.Episode)
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
// With AllowMissingCandidateGroup, a candidate that carries no group and no
// site passes the group check.
func (m matcher) validateExactSizeSearchIdentity(input searchCandidateInput) (bool, string) {
	source := input.Source.release
	candidate := input.Candidate.release
	if source == nil || candidate == nil {
		return false, "missing parsed release"
	}

	var identity releaseComparison
	m.compareTitles(&identity, source, candidate, input.Source.rawName, input.Candidate.rawName, input.SourceTitles, input.CandidateTitles)
	m.compareArtistAndDates(&identity, source, candidate, isTVRelease(source) || isTVRelease(candidate))
	if !identity.matched() {
		return false, identity.reason()
	}

	normalizer := m.normalizer()
	sourceResolution := normalizer.Normalize(source.Resolution)
	candidateResolution := normalizer.Normalize(candidate.Resolution)
	if sourceResolution == "" || candidateResolution == "" || sourceResolution != candidateResolution {
		return false, "resolution mismatch"
	}

	sourceIdentity := m.normalizedGroupSiteIdentity(source)
	candidateIdentity := m.normalizedGroupSiteIdentity(candidate)
	switch {
	case m.dashSuffixGroupSpellsSite(input.Source, input.Candidate):
		candidateIdentity = normalizer.Normalize(source.Site)
	case m.dashSuffixGroupSpellsSite(input.Candidate, input.Source):
		sourceIdentity = normalizer.Normalize(candidate.Site)
	}
	if sourceIdentity == "" || (candidateIdentity == "" && !input.AllowMissingCandidateGroup) ||
		(candidateIdentity != "" && sourceIdentity != candidateIdentity &&
			!m.spaceJoinedGroupSite(source, candidate) &&
			!m.crossFieldGroupSiteFallback(input.Source, input.Candidate)) {
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
	// Trackers add a year to names that the local torrent does not carry, so a
	// year on one side only is not a conflict. Two different years are.
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false, "year mismatch"
	}
	if source.Month != candidate.Month || source.Day != candidate.Day {
		return false, "date mismatch"
	}

	return true, ""
}

// listedDifferences are the fields in which the listing differs from the local
// torrent that search compared, as the stored verdict records them. Title
// rescue compares no field after the title.
func (provenance searchDecisionProvenance) listedDifferences() matchDifferences {
	switch provenance.Class {
	case searchCandidateClassWebSourceRelabel:
		return differencesOf(differenceSource)
	case searchCandidateClassTitleRescue:
		return differencesOf(differenceTitle)
	default:
		return provenance.Relaxed
	}
}

// downloadReadsAsListing reports whether the downloaded torrent reads the same
// as the listing that search judged, apart from the fields that the stored
// verdict lists as different. Apply keeps that verdict for the local torrent
// that search compared, so the download must not bring a new difference.
func (m matcher) downloadReadsAsListing(decision searchDecisionProvenance, local, download namedRelease, findIndividualEpisodes bool) bool {
	listed := decision.listedDifferences()
	// The listing can drop the group, the fansub tag, or the CRC tag. A tag on the
	// local torrent and another on the download still prove another release.
	var tags releaseComparison
	m.compareGroupSiteAndChecksum(&tags, local, download)
	if tags.differences.without(differenceGroupMissing)&^listed != 0 {
		return false
	}
	listing, download, _ := applyEpisodeMap(m.readName(decision.SearchCandidateName), download, decision.EpisodeMap)
	if decision.Class == searchCandidateClassHybridTolerance {
		// The download can keep or drop the HYBRID tag that search let pass.
		listing, download = withoutHybrid(listing), withoutHybrid(download)
	}
	// The download can name the local torrent or another title of the show that
	// search resolved. Only the download is held against those titles, so a
	// listing that matches its own alias cannot admit a download of another show.
	titles := m.appendReleaseTitles(slices.Concat(decision.SourceTitles, decision.CandidateTitles), local.release, local.rawName)
	comparison := m.compareReleases(listing, download, titles, nil, findIndividualEpisodes, false)
	if comparison.differences == differencesOf(differenceTitle) && listed.has(differenceTitle) {
		comparison = m.compareExceptTitle(comparison, listing, download, findIndividualEpisodes, false, false)
	}
	if comparison.differences&^listed != 0 {
		return false
	}
	// A relaxed verdict rests on the group of the listing, so a selected file
	// or a raw name that names another group breaks it.
	if decision.Class != searchCandidateClassStrict && !m.explicitGroupsAgree(listing, download) {
		return false
	}
	return !listed.has(differenceGroup) || m.explicitGroupsFitFallbackIdentity(download, decision.GroupFallbackIdentity)
}

// candidateRequiresVerification reports whether search provenance for the
// selected local source requires the add to be hashed before it seeds. Exact-
// size and strict alternatives may be grouped in one candidate, so the stored
// relaxation must remain bound to the torrent that supplied its size evidence.
func candidateRequiresVerification(candidate CrossSeedCandidate, selectedHash string, req *CrossSeedRequest) bool {
	if candidate.titleRescue {
		return true
	}
	// A Manual match always verifies before seeding, validated files or not:
	// the pinned target bypassed the release and content gates, so the recheck
	// is the arbiter. There is deliberately no way to skip it.
	if req != nil && req.ManualTargetHash != "" {
		return true
	}
	if req == nil || candidate.InstanceID != req.SearchDecision.SourceInstanceID {
		return false
	}
	sourceHash := normalizeHash(req.SearchDecision.SourceHash)
	return sourceHash != "" && normalizeHash(selectedHash) == sourceHash &&
		req.SearchDecision.Relaxed.needsVerification()
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
func (m matcher) crossFieldGroupSiteFallback(source, candidate namedRelease) bool {
	_, ok := m.crossFieldGroupSiteFallbackIdentity(source, candidate)
	return ok
}

func (m matcher) crossFieldGroupSiteFallbackIdentity(source, candidate namedRelease) (string, bool) {
	normalizer := m.normalizer()
	for _, sourceView := range m.groupIdentityViews(source) {
		for _, candidateView := range m.groupIdentityViews(candidate) {
			if m.splitGroupSiteMatchesTaggedGroup(sourceView, candidateView) {
				return normalizer.Normalize(sourceView.release.Site), true
			}
			if m.splitGroupSiteMatchesTaggedGroup(candidateView, sourceView) {
				return normalizer.Normalize(candidateView.release.Site), true
			}
		}
	}
	return "", false
}

// namedRelease is the Read release (GLOSSARY.md): the public release fields,
// raw name, and selected-file tags of one name. Keeping the origin
// inside it prevents a caller from passing file-derived fields while silently
// dropping provenance. readRelease builds it, and titleKeys reads its Title keys.
type namedRelease struct {
	release   *rls.Release
	rawName   string
	tagOrigin *rls.Release
	// seasonNumeral is the season inferred from the Season numeral of a
	// seasonless pack. release.Series stays 0, so the main search query and a
	// listing that names another season still read the pack as seasonless.
	seasonNumeral int
}

// groupIdentityViews exposes both the selected-file-derived public view and the
// raw torrent/search name. File inference can repair TV structure while losing
// a Site field that only the raw bracket form carries. Explicit-group vetoes
// still inspect every origin through groupTagProvenance.
func (m matcher) groupIdentityViews(side namedRelease) []namedRelease {
	views := make([]namedRelease, 0, 2)
	views = append(views, side)
	if side.rawName == "" {
		return views
	}
	rawView := side
	rawView.release = m.parseReleaseName(side.rawName)
	normalizer := m.normalizer()
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
func (m matcher) splitGroupSiteMatchesTaggedGroup(split, tagged namedRelease) bool {
	if split.release == nil || tagged.release == nil {
		return false
	}

	normalizer := m.normalizer()

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

	splitProvenance := m.groupTagProvenance(split)
	if !splitProvenance.fallbackGroups.contains(splitGroup) ||
		splitProvenance.explicitGroups.contains(splitGroup) ||
		!splitProvenance.explicitGroups.onlyContains(splitSite) {
		return false
	}

	// The tagged side must spell that same site name as a real group tag, and
	// carry no label of its own to confuse it with.
	taggedProvenance := m.groupTagProvenance(tagged)
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
func (m matcher) groupTagProvenance(side namedRelease) groupTagProvenance {
	provenance := groupTagProvenance{
		explicitGroups: make(normalizedIdentitySet),
		fallbackGroups: make(normalizedIdentitySet),
	}
	normalizer := m.normalizer()
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

	for _, origin := range m.tagOrigins(side) {
		add(origin)
	}
	return provenance
}

// tagOrigins returns the release, its selected file, and its parsed raw
// torrent/search name. An origin the side does not record is nil.
func (m matcher) tagOrigins(side namedRelease) [3]*rls.Release {
	var rawRelease *rls.Release
	if side.rawName != "" {
		rawRelease = m.parseReleaseName(side.rawName)
	}
	return [3]*rls.Release{side.release, side.tagOrigin, rawRelease}
}

// dashSuffixGroupSpellsSite reports whether grouped, which carries a Group and
// no Site, names the fansub in the Site of sited. rls keeps a bracket name such
// as "[Moe-Fansub]" whole in Site, but splits the suffix "x265-Moe-Fansub" at
// the last dash into a text tag "Moe" and Group "Fansub". Only the full run of
// text tags before the group rebuilds the name, so "x265-Fansub" and
// "x265-Big-Moe-Fansub" stay different groups. The rebuild reads every origin
// in tagOrigins, because a view built from the files can carry
// the Group of the torrent name without the tags of that name. Every origin on
// either side with a group tag must carry the Group and rebuild the Site, so a
// selected file that names "Big-Moe-Fansub" or "Other" vetoes a torrent name
// that says "Moe-Fansub". An origin without a group tag has no say.
func (m matcher) dashSuffixGroupSpellsSite(sited, grouped namedRelease) bool {
	if sited.release == nil || grouped.release == nil {
		return false
	}
	normalizer := m.normalizer()
	site := normalizer.Normalize(sited.release.Site)
	group := normalizer.Normalize(grouped.release.Group)
	if site == "" || group == "" || normalizer.Normalize(grouped.release.Site) != "" {
		return false
	}

	// spells reports whether no origin of side contradicts the rebuild, and
	// whether at least one origin spells it.
	spells := func(side namedRelease) (agrees, spelled bool) {
		for _, origin := range m.tagOrigins(side) {
			if origin == nil {
				continue
			}
			groupTag, name := dashSuffixGroupName(origin)
			switch normalizer.Normalize(groupTag) {
			case "":
				continue
			case group:
				if normalizer.Normalize(name) != site {
					return false, false
				}
			default:
				return false, false
			}
			spelled = true
		}
		return true, spelled
	}
	if agrees, spelled := spells(grouped); !agrees || !spelled {
		return false
	}
	agrees, _ := spells(sited)
	return agrees
}

// dashSuffixGroupName returns the group tag and that tag joined with the text
// tags directly before it. Any other tag, such as a codec or HDR tag, ends the
// run.
func dashSuffixGroupName(release *rls.Release) (groupTag, name string) {
	tags := release.Tags()
	groupIndex := -1
	for i, tag := range slices.Backward(tags) {
		if tag.Is(rls.TagTypeGroup) {
			groupIndex = i
			break
		}
	}
	if groupIndex < 0 {
		return "", ""
	}

	groupTag = tags[groupIndex].Text()
	parts := []string{groupTag}
	for i := groupIndex - 1; i >= 0; i-- {
		if tags[i].Is(rls.TagTypeDelim, rls.TagTypeWhitespace) {
			continue
		}
		if !tags[i].Is(rls.TagTypeText) {
			break
		}
		parts = append(parts, tags[i].Text())
	}
	slices.Reverse(parts)
	return groupTag, strings.Join(parts, "-")
}

func (m matcher) explicitGroupsAgree(left, right namedRelease) bool {
	groups := m.groupTagProvenance(left).explicitGroups
	maps.Copy(groups, m.groupTagProvenance(right).explicitGroups)
	return len(groups) <= 1
}

// explicitGroupsFitFallbackIdentity rejects a file/raw-name group that differs
// from the cross-field identity search actually rescued. Cached decisions carry
// that identity so retitling cannot make a fallback word look authoritative.
func (m matcher) explicitGroupsFitFallbackIdentity(side namedRelease, expectedIdentity string) bool {
	if side.release == nil {
		return false
	}

	normalizer := m.normalizer()
	expected := normalizer.Normalize(expectedIdentity)
	identityVisible := false
	for _, view := range m.groupIdentityViews(side) {
		if view.release != nil && (expected == normalizer.Normalize(view.release.Group) ||
			expected == normalizer.Normalize(view.release.Site)) {
			identityVisible = true
			break
		}
	}
	if expected == "" || !identityVisible {
		return false
	}
	return m.groupTagProvenance(side).explicitGroups.onlyContains(expected)
}

func (m matcher) normalizedGroupSiteIdentity(release *rls.Release) string {
	if release == nil {
		return ""
	}
	normalizer := m.normalizer()
	if group := normalizer.Normalize(release.Group); group != "" {
		return group
	}
	return normalizer.Normalize(release.Site)
}

// verifyReason is why the selected match must pass a full recheck before it
// seeds, or VerifyNone.
func verifyReason(candidate CrossSeedCandidate, selectedHash string, req *CrossSeedRequest) VerifyReason {
	switch {
	case candidate.titleRescue:
		return VerifyTitleRescue
	case req != nil && req.ManualTargetHash != "":
		return VerifyManual
	case candidateRequiresVerification(candidate, selectedHash, req):
		return VerifyRelaxed
	}
	return VerifyNone
}
