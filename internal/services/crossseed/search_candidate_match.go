// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
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
	Accepted              bool
	Class                 searchCandidateClass
	SizeEvidence          searchSizeEvidence
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
	Class                 searchCandidateClass
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
	bound.SourceHash = hash
	return bound
}

func (provenance searchDecisionProvenance) admitted() bool {
	return provenance.Class != "" && provenance.Class != searchCandidateClassRejected
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
// hash check. The fallback may relax descriptive attributes such as source,
// collection, HDR,
// codec, or bit depth, and the season and episode numbers that indexers rewrite.
// Apply later uses the private decision class to skip its duplicate release
// prefilter; normal torrent-file validation remains authoritative.
func (s *Service) classifySearchCandidate(input searchCandidateInput) searchCandidateDecision {
	decision := searchCandidateDecision{
		Class:        searchCandidateClassRejected,
		SizeEvidence: classifySearchSizeEvidence(input.SourceSize, input.CandidateSize),
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
	// Strict checksum matching is directional: an existing release without a
	// CRC accepts a candidate that carries one, but apply compares the downloaded
	// torrent in the opposite direction. With positive exact-size evidence, use
	// the replayable fallback for that one-sided claim. If source relabeling was
	// the strict rejection, keep it as the cause and record checksum separately.
	exactOneSidedChecksum := decision.SizeEvidence.matches() &&
		s.hasOneSidedChecksum(input.Source.release, input.Candidate.release)
	if strictMatch && exactOneSidedChecksum {
		strictMatch = false
		mismatchReason = "checksum mismatch"
	}
	preferExactSizeFallback := exactOneSidedChecksum && mismatchReason == sourceMismatchReason
	decision.StrictMismatchReason = mismatchReason

	class := searchCandidateClassStrict
	switch {
	case strictMatch:
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
		relaxedDifferences := s.recordedReleaseDifferences(input.Source, input.Candidate)
		if ok, reason := s.validateExactSizeFallback(input, mismatchReason, relaxedDifferences); ok {
			class = searchCandidateClassExactSizeFallback
			decision.RelaxedDifferences = relaxedDifferences
			if slices.Contains(relaxedDifferences, "group") {
				decision.GroupFallbackIdentity, _ = s.crossFieldGroupSiteFallbackIdentity(input.Source, input.Candidate)
			}
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
// they seed. It keys on the causal rejection rather than the recorded difference
// list, which also holds numbers strict matching never objected to: an
// episode-from-pack pairing records an episode delta while being rejected for
// something else entirely.
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
	splitProvenance := s.groupTagProvenance(split)
	if splitGroup == "" || splitGroup == splitSite ||
		!splitProvenance.fallbackGroups.contains(splitGroup) ||
		splitProvenance.explicitGroups.contains(splitGroup) ||
		!splitProvenance.explicitGroups.onlyContains(splitSite) {
		return false
	}

	// The tagged side must spell that same site name as a real group tag, and
	// carry no label of its own to confuse it with.
	if normalizer.Normalize(tagged.release.Group) != splitSite ||
		normalizer.Normalize(tagged.release.Site) != "" {
		return false
	}
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

// recordedReleaseDifferences lists the fields the two releases disagree on. The
// list records differences, it does not judge them: an entry here only permits
// the exact-size fallback to override the one strict rejection that names the
// same field. Whether the add must be hashed first is decided by that rejection,
// in searchRelaxationRequiresVerification.
//
// Group is the exception, and it is recorded only when the two names carry the
// cross-field evidence of one fansub tag split across Group and Site. A plain
// disagreement between two groups leaves no entry, so nothing downstream can
// relax it. The evidence is circumstantial, so an entry here still buys only a
// hash check, never a seed.
func (s *Service) recordedReleaseDifferences(sourceSide, candidateSide namedRelease) []string {
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
	if s.crossFieldGroupSiteFallback(sourceSide, candidateSide) {
		differences = append(differences, "group")
	}
	if compatible, _ := checkVariantsCompatible(source, candidate); !compatible {
		differences = append(differences, "variant")
	}

	return differences
}
