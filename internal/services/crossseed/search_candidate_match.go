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

type searchCandidateClass string

const (
	searchCandidateClassFileEvidence      searchCandidateClass = "file-evidence"
	searchCandidateClassRejected          searchCandidateClass = "rejected"
	searchCandidateClassStrict            searchCandidateClass = "strict"
	searchCandidateClassWebSourceRelabel  searchCandidateClass = "web-source-relabel"
	searchCandidateClassExactSizeFallback searchCandidateClass = "exact-size-fallback"
	searchCandidateClassTitleRescue       searchCandidateClass = "title-rescue"
	searchCandidateClassHybridTolerance   searchCandidateClass = "hybrid-tolerance"
)

type searchSizeEvidence string

const (
	searchSizeEvidenceNone  searchSizeEvidence = "none"
	searchSizeEvidenceExact searchSizeEvidence = "exact"
)

type searchCandidateInput struct {
	Source                     namedRelease
	Candidate                  namedRelease
	SourceTitles               []string
	CandidateTitles            []string
	EpisodeMap                 *models.EpisodeMap
	SourceSize                 int64
	CandidateSize              int64
	TolerancePercent           float64
	FindIndividualEpisodes     bool
	RescueTitleMismatches      bool
	AllowMissingCandidateGroup bool
}

type searchCandidateDecision struct {
	Accepted              bool
	Class                 searchCandidateClass
	SizeEvidence          searchSizeEvidence
	SearchCandidateName   string
	GroupFallbackIdentity string
	SourceTitles          []string
	CandidateTitles       []string
	EpisodeMap            *models.EpisodeMap
	RejectReason          string
	StrictMismatchReason  string
	RelaxedDifferences    []string
	Relaxed               matchDifferences
	Score                 float64
	MatchReason           string
	SizeRejected          bool
}

type searchDecisionProvenance struct {
	Catalog               catalogFileEvidence
	Class                 searchCandidateClass
	SearchCandidateName   string
	SourceInstanceID      int
	SourceHash            string
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
	if bound.catalog() {
		return bound
	}
	bound.SourceInstanceID = instanceID
	bound.SourceHash = normalizeHash(hash)
	return bound
}

func (provenance searchDecisionProvenance) matchesSource(instanceID int, hash string) bool {
	return !provenance.catalog() || (provenance.SourceInstanceID == instanceID && provenance.SourceHash == normalizeHash(hash))
}

func (provenance searchDecisionProvenance) admitted() bool {
	return provenance.Class != "" && provenance.Class != searchCandidateClassRejected
}

func searchDecisionRequiresVerification(decision searchDecisionProvenance) bool {
	if decision.catalog() {
		return false
	}
	return decision.Class == searchCandidateClassTitleRescue ||
		decision.Relaxed.needsVerification()
}

const (
	sizeEvidenceFallbackScoreBonus = 2.0
	sizeEvidenceRelabelScoreBonus  = 3.0
	sizeEvidenceStrictScoreBonus   = 4.0
)

func (input searchCandidateInput) policy() matchPolicy {
	return matchPolicy{
		TitleRescue:                input.RescueTitleMismatches,
		IndividualEpisodes:         input.FindIndividualEpisodes,
		SizeTolerancePercent:       input.TolerancePercent,
		AllowMissingCandidateGroup: input.AllowMissingCandidateGroup,
	}
}

func (m matcher) classifySearchCandidate(input searchCandidateInput) searchCandidateDecision {
	evidence := matchEvidence{
		SourceSize:      input.SourceSize,
		CandidateSize:   input.CandidateSize,
		SourceTitles:    input.SourceTitles,
		CandidateTitles: input.CandidateTitles,
		EpisodeMap:      input.EpisodeMap,
	}
	policy := input.policy()
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
	case searchCandidateClassRejected, searchCandidateClassFileEvidence:
	}
	if verdict.MappedEpisode != "" {
		decision.MatchReason = verdict.MappedEpisode + "; " + decision.MatchReason
	}

	return decision
}

func (m matcher) hybridToleranceVerdict(input searchCandidateInput, evidence matchEvidence, policy matchPolicy, verdict matchVerdict) matchVerdict {
	if verdict.Class != searchCandidateClassRejected || verdict.Strict.reason() != "HYBRID" ||
		input.SourceSize <= 0 || input.CandidateSize <= 0 {
		return verdict
	}
	hybrid := m.matchVerdict(withoutHybrid(input.Source), withoutHybrid(input.Candidate), evidence, policy)
	if hybrid.Class != searchCandidateClassStrict {
		return verdict
	}
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

func applyEpisodeMap(source, candidate namedRelease, episodeMap *models.EpisodeMap) (mappedSource, mappedCandidate namedRelease, reason string) {
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

	sourceSum := normalizer.Normalize(source.Sum)
	candidateSum := normalizer.Normalize(candidate.Sum)
	if sourceSum != "" && candidateSum != "" && sourceSum != candidateSum {
		return false, checksumMismatchReason
	}

	sourceArtist := normalizer.Normalize(source.Artist)
	candidateArtist := normalizer.Normalize(candidate.Artist)
	if (sourceArtist != "" || candidateArtist != "") && sourceArtist != candidateArtist {
		return false, "artist mismatch"
	}
	if source.Year > 0 && candidate.Year > 0 && source.Year != candidate.Year {
		return false, "year mismatch"
	}
	if source.Month != candidate.Month || source.Day != candidate.Day {
		return false, "date mismatch"
	}

	return true, ""
}

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

func (m matcher) downloadReadsAsListing(decision searchDecisionProvenance, local, download namedRelease, findIndividualEpisodes bool) bool {
	listed := decision.listedDifferences()
	var tags releaseComparison
	m.compareGroupSiteAndChecksum(&tags, local, download)
	if tags.differences.without(differenceGroupMissing)&^listed != 0 {
		return false
	}
	listing, download, _ := applyEpisodeMap(m.readName(decision.SearchCandidateName), download, decision.EpisodeMap)
	if decision.Class == searchCandidateClassHybridTolerance {
		listing, download = withoutHybrid(listing), withoutHybrid(download)
	}
	titles := m.appendReleaseTitles(slices.Concat(decision.SourceTitles, decision.CandidateTitles), local.release, local.rawName)
	comparison := m.compareReleases(listing, download, titles, nil, findIndividualEpisodes, false)
	if comparison.differences == differencesOf(differenceTitle) && listed.has(differenceTitle) {
		comparison = m.compareExceptTitle(comparison, listing, download, findIndividualEpisodes, false, false)
	}
	if comparison.differences.without(differenceGroupMissing)&^listed != 0 {
		return false
	}
	if decision.Class != searchCandidateClassStrict && !m.explicitGroupsAgree(listing, download) {
		return false
	}
	return !listed.has(differenceGroup) || m.explicitGroupsFitFallbackIdentity(download, decision.GroupFallbackIdentity)
}

func candidateRequiresVerification(candidate CrossSeedCandidate, selectedHash string, req *CrossSeedRequest) bool {
	if candidate.titleRescue {
		return true
	}
	if req != nil && req.ManualTargetHash != "" {
		return true
	}
	if req == nil || candidate.InstanceID != req.SearchDecision.SourceInstanceID {
		return false
	}
	sourceHash := normalizeHash(req.SearchDecision.SourceHash)
	return sourceHash != "" && normalizeHash(selectedHash) == sourceHash &&
		searchDecisionRequiresVerification(req.SearchDecision)
}

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

type namedRelease struct {
	release       *rls.Release
	rawName       string
	tagOrigin     *rls.Release
	seasonNumeral int
}

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

func (m matcher) splitGroupSiteMatchesTaggedGroup(split, tagged namedRelease) bool {
	if split.release == nil || tagged.release == nil {
		return false
	}

	normalizer := m.normalizer()

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

	taggedProvenance := m.groupTagProvenance(tagged)
	return taggedProvenance.explicitGroups.contains(splitSite) &&
		taggedProvenance.explicitGroups.onlyContains(splitSite)
}

type normalizedIdentitySet map[string]struct{}

func (set normalizedIdentitySet) contains(identity string) bool {
	_, ok := set[identity]
	return ok
}

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

func (m matcher) tagOrigins(side namedRelease) [3]*rls.Release {
	var rawRelease *rls.Release
	if side.rawName != "" {
		rawRelease = m.parseReleaseName(side.rawName)
	}
	return [3]*rls.Release{side.release, side.tagOrigin, rawRelease}
}

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
