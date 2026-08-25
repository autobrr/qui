// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

// The bracket-anime pack puts the fansub tag in Site and hands Group whatever
// word is left over, so it disagrees with its own scene-styled counterpart on
// both fields at once. The eztv names are the trap: an indexer label lands in
// Site too, and two different groups share it.
const (
	fansubBracketPack   = "[KIRI] Azure Compass S01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
	fansubTaggedPack    = "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	otherFansubBracket  = "[EMBER] Azure Compass S01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (EMBER)][Batch]"
	otherFansubTagged   = "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-EMBER"
	labelledLeftoverOne = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264.FoV[eztv]"
	labelledLeftoverTwo = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264.KILLERS[eztv]"
	labelledTaggedOne   = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-FoV[eztv]"
	labelledTaggedTwo   = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KILLERS[eztv]"
	bareFansubEpisode   = "[KIRI] Azure Compass - 01 (1080p)"
	// No trailing bracket to spend, so the leftover word is the fansub tag
	// again and Group repeats Site.
	fansubRepeatedTag = "[KIRI] Azure Compass S01 [Web][MKV][h265][1080p][AAC 2.0][Softsubs (KIRI)]"
	// eztv both labels other groups' releases and releases under its own name,
	// so every shape of "the label is also a group" has to stay rejected.
	labelAsTaggedGroup   = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-eztv"
	labelAsGroupAndSite  = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-eztv[eztv]"
	labelAsLeftoverGroup = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264.eztv"
	labelWithoutGroup    = "[eztv] Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264"
	fansubPackSize       = int64(71_052_546_722)
)

func newGroupFallbackService() *Service {
	return &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
}

func releaseHasGroupTag(release *rls.Release, group string) bool {
	for _, tag := range release.Tags() {
		if tag.Is(rls.TagTypeGroup) && strings.EqualFold(tag.Normalize(), group) {
			return true
		}
	}
	return false
}

func TestGroupFallbackUsesRawSiteWhenSelectedEpisodeHasNoGroup(t *testing.T) {
	const (
		split  = "[KIRI] Azure Compass S01E01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
		tagged = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	)
	svc := newGroupFallbackService()
	rawSplit := svc.releaseCache.Parse(split)
	decision := svc.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: svc.releaseCache.Parse(tagged), rawName: tagged},
		Candidate:     namedRelease{release: rawSplit, rawName: split},
		SourceSize:    2_000,
		CandidateSize: 2_000,
	})
	require.True(t, decision.Accepted)
	require.Equal(t, groupMismatchReason, decision.StrictMismatchReason)

	view := svc.applyTargetReleaseViewFromFiles(split, rawSplit, qbt.TorrentFiles{
		{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264.mkv", Size: 2_000},
	}, true)
	require.Empty(t, view.release.Site, "the selected-file view does not carry the raw Site field")
	require.True(t, svc.explicitGroupsFitFallbackIdentity(view, decision.GroupFallbackIdentity),
		"a selected file without a group must not contradict the raw split identity")
}

func TestGroupFallbackUsesRawSiteForExistingEpisode(t *testing.T) {
	const (
		split  = "[KIRI] Azure Compass S01E01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
		tagged = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	)
	svc := newGroupFallbackService()
	files := qbt.TorrentFiles{{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264.mkv", Size: 2_000}}
	torrent := &qbt.Torrent{Name: split}
	view := svc.searchSourceReleaseViewFromFiles(context.Background(), torrent, svc.releaseCache.Parse(split), files)
	decision := svc.classifySearchCandidate(searchCandidateInput{
		Source:        view,
		Candidate:     namedRelease{release: svc.releaseCache.Parse(tagged), rawName: tagged},
		SourceSize:    2_000,
		CandidateSize: 2_000,
	})
	require.True(t, decision.Accepted,
		"the raw split identity must remain available when selected files supply no group")
}

func classifyGroupPair(svc *Service, sourceName, candidateName string, sourceSize, candidateSize int64) searchCandidateDecision {
	return svc.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: svc.releaseCache.Parse(sourceName), rawName: sourceName},
		Candidate:     namedRelease{release: svc.releaseCache.Parse(candidateName), rawName: candidateName},
		SourceSize:    sourceSize,
		CandidateSize: candidateSize,
	})
}

// The whole rescue reads rls tag provenance, so pin the parses it reads. rls
// PR #18 makes a leading bracket set Group as well as Site; when that lands
// these pins fail rather than the rescue silently widening to indexer labels.
func TestGroupSiteFallback_ParsePreconditions(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()

	t.Run("the bracket pack splits one fansub name across Group and Site", func(t *testing.T) {
		release := svc.releaseCache.Parse(fansubBracketPack)
		require.Equal(t, "Batch", release.Group, "Group must still be the leftover word, not the fansub tag")
		require.Equal(t, "KIRI", release.Site)
		require.False(t, releaseHasGroupTag(release, release.Group), "rls only guessed this group")
	})

	t.Run("the scene pack carries a real group tag and no site", func(t *testing.T) {
		release := svc.releaseCache.Parse(fansubTaggedPack)
		require.Equal(t, "KIRI", release.Group)
		require.Empty(t, release.Site)
		require.True(t, releaseHasGroupTag(release, release.Group))
	})

	// If rls ever overwrites these with the indexer label, every eztv listing
	// collapses onto one identity and the negative cases below stop proving
	// anything. Fail here instead.
	for _, name := range []string{labelledLeftoverOne, labelledLeftoverTwo, labelledTaggedOne, labelledTaggedTwo} {
		t.Run("the indexer label never becomes the group in "+name, func(t *testing.T) {
			release := svc.releaseCache.Parse(name)
			require.Equal(t, "eztv", release.Site)
			require.NotEqual(t, "eztv", release.Group, "the label must stay out of Group")
			require.NotEmpty(t, release.Group)
		})
	}
}

// The accepted case: equal reported pack size, one fansub name, two spellings.
func TestGroupSiteFallback_AdmitsSplitFansubIdentity(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()

	for _, tt := range []struct {
		name   string
		source string
		target string
	}{
		{name: "bracket source", source: fansubBracketPack, target: fansubTaggedPack},
		{name: "tagged source", source: fansubTaggedPack, target: fansubBracketPack},
	} {
		t.Run(tt.name, func(t *testing.T) {
			strict, reason := svc.releasesMatchWithReasonAndNamesAndTitles(
				svc.releaseCache.Parse(tt.source),
				svc.releaseCache.Parse(tt.target),
				tt.source, tt.target, nil, nil, false,
			)
			require.False(t, strict, "strict matching must still reject this pairing")
			require.Equal(t, groupMismatchReason, reason)

			decision := classifyGroupPair(svc, tt.source, tt.target, fansubPackSize, fansubPackSize)
			require.True(t, decision.Accepted, "reject reason: %s", decision.RejectReason)
			require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
			require.Equal(t, groupMismatchReason, decision.StrictMismatchReason)
			require.Contains(t, decision.RelaxedDifferences, "group")
			require.True(t, searchRelaxationRequiresVerification(decision.StrictMismatchReason),
				"a relaxed group must be hashed before it seeds")
		})
	}
}

// Every way two names can carry different groups without carrying the
// cross-field evidence. Equal sizes must not rescue any of them.
func TestGroupSiteFallback_RejectsWithoutCrossFieldEvidence(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()

	for _, tt := range []struct {
		name   string
		source string
		target string
	}{
		{name: "the tagged group names a different fansub", source: fansubBracketPack, target: otherFansubTagged},
		{name: "two bracket packs agree only on the leftover word", source: fansubBracketPack, target: otherFansubBracket},
		{name: "two leftover groups share an indexer label", source: labelledLeftoverOne, target: labelledLeftoverTwo},
		{name: "two tagged groups share an indexer label", source: labelledTaggedOne, target: labelledTaggedTwo},
		{name: "a tagged group meets a leftover group under one label", source: labelledTaggedOne, target: labelledLeftoverTwo},
		{name: "a properly tagged group is not up for reinterpretation", source: labelledTaggedTwo, target: labelAsTaggedGroup},
		{name: "the tagged side carries a label of its own", source: labelledLeftoverTwo, target: labelAsGroupAndSite},
		{name: "the tagged side only guessed its group too", source: labelledLeftoverTwo, target: labelAsLeftoverGroup},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := svc.releaseCache.Parse(tt.source)
			target := svc.releaseCache.Parse(tt.target)
			sourceSide := namedRelease{release: source, rawName: tt.source}
			targetSide := namedRelease{release: target, rawName: tt.target}
			require.False(t, svc.crossFieldGroupSiteFallback(sourceSide, targetSide))
			require.NotContains(t, svc.observedReleaseDifferences(sourceSide, targetSide), "group")

			decision := classifyGroupPair(svc, tt.source, tt.target, fansubPackSize, fansubPackSize)
			require.False(t, decision.Accepted, "class: %s", decision.Class)
		})
	}
}

func TestGroupSiteFallback_RejectsImpossiblePublicFieldsCheaply(t *testing.T) {
	svc := newGroupFallbackService()
	source := namedRelease{release: svc.releaseCache.Parse(fansubBracketPack), rawName: fansubBracketPack}
	target := namedRelease{release: svc.releaseCache.Parse(otherFansubTagged), rawName: otherFansubTagged}

	var matched bool
	allocations := testing.AllocsPerRun(100, func() {
		matched = svc.crossFieldGroupSiteFallback(source, target)
	})

	require.False(t, matched)
	// Public Group/Site fields already disprove this pairing. Detailed tag
	// provenance is reserved for pairs whose public fields can form the rescue.
	require.Less(t, allocations, 40.0)
}

// namedRelease can carry enriched fields whose origin is outside the release's
// own tags. The split Group must still be traceable to Text in either recorded
// origin; an unexplained value cannot buy an exact-size relaxation.
func TestGroupSiteFallback_RequiresFallbackGroupProvenance(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()
	source := *svc.releaseCache.Parse(fansubBracketPack)
	source.Group = "Unexplained"
	require.False(t, releaseHasGroupTag(&source, source.Group))

	decision := svc.classifySearchCandidate(searchCandidateInput{
		Source:        namedRelease{release: &source, rawName: fansubBracketPack},
		Candidate:     namedRelease{release: svc.releaseCache.Parse(fansubTaggedPack), rawName: fansubTaggedPack},
		SourceSize:    fansubPackSize,
		CandidateSize: fansubPackSize,
	})

	require.False(t, decision.Accepted, "class: %s", decision.Class)
	require.Equal(t, "group/site mismatch", decision.RejectReason)
}

// A bracket name with no leftover word leaves Group empty, which is the
// long-standing "candidate simply lacks the tag" case. The rescue must not
// touch it: it needs both fields filled on one side.
func TestGroupSiteFallback_LeavesMissingIdentityAlone(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()
	siteOnly := svc.releaseCache.Parse(bareFansubEpisode)
	require.Empty(t, siteOnly.Group, "fixture must carry a site and no group")
	require.NotEmpty(t, siteOnly.Site)

	bare := svc.releaseCache.Parse("Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264")
	require.Empty(t, bare.Group)
	require.Empty(t, bare.Site)

	require.False(t, svc.crossFieldGroupSiteFallback(
		namedRelease{release: siteOnly, rawName: bareFansubEpisode},
		namedRelease{release: bare, rawName: "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264"},
	))

	// A leading label with nothing left over leaves Group empty too. Strict
	// matching already reads that side's identity off Site, so the rescue must
	// keep its hands off rather than declare the label a fansub name.
	labelled := svc.releaseCache.Parse(labelWithoutGroup)
	require.Empty(t, labelled.Group, "fixture must leave the group empty")
	require.Equal(t, "eztv", labelled.Site)
	require.False(t, svc.crossFieldGroupSiteFallback(
		namedRelease{release: labelled, rawName: labelWithoutGroup},
		namedRelease{release: svc.releaseCache.Parse(labelAsTaggedGroup), rawName: labelAsTaggedGroup},
	))

	// Strict matching tolerates the missing candidate tag exactly as before.
	ok, reason := svc.validateGroupSiteAndChecksum(siteOnly, bare, false)
	require.True(t, ok, "unexpected rejection: %s", reason)

	// An empty identity still cannot buy exact-size permission.
	ok, reason = svc.validateExactSizeSearchIdentity(searchCandidateInput{
		Source:        namedRelease{release: siteOnly, rawName: bareFansubEpisode},
		Candidate:     namedRelease{release: bare, rawName: "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264"},
		SourceSize:    fansubPackSize,
		CandidateSize: fansubPackSize,
	})
	require.False(t, ok)
	require.Equal(t, "group/site mismatch", reason)
}

// When the leftover word repeats the fansub tag, Group and Site say the same
// thing and every gate already agrees. Recording a relaxation there would hand
// apply a token nothing was rejected for.
func TestGroupSiteFallback_IgnoresRepeatedFansubTag(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()
	repeated := svc.releaseCache.Parse(fansubRepeatedTag)
	require.Equal(t, "KIRI", repeated.Group, "fixture must repeat the tag into Group")
	require.Equal(t, "KIRI", repeated.Site)

	tagged := svc.releaseCache.Parse(fansubTaggedPack)
	repeatedSide := namedRelease{release: repeated, rawName: fansubRepeatedTag}
	taggedSide := namedRelease{release: tagged, rawName: fansubTaggedPack}
	require.False(t, svc.crossFieldGroupSiteFallback(repeatedSide, taggedSide))
	require.NotContains(t, svc.observedReleaseDifferences(repeatedSide, taggedSide), "group")

	// The pairing still matches, on the codec the two names actually differ by.
	decision := classifyGroupPair(svc, fansubRepeatedTag, fansubTaggedPack, fansubPackSize, fansubPackSize)
	require.True(t, decision.Accepted, "reject reason: %s", decision.RejectReason)
	require.NotEqual(t, groupMismatchReason, decision.StrictMismatchReason)
}

// Apply re-derives the predicate and the recorded relaxation instead of
// trusting the request, and only for the torrent that supplied the size.
func TestFindCandidatesGroupFallbackBindsToSearchSource(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		sourceHash = "existing"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: fansubBracketPack, Size: fansubPackSize, Progress: 1}
	files := map[string]qbt.TorrentFiles{
		sourceHash: {
			{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize/2 + 1},
			{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
		},
	}

	newRequest := func() *FindCandidatesRequest {
		return &FindCandidatesRequest{
			TorrentName:       fansubTaggedPack,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:                 searchCandidateClassExactSizeFallback,
				SourceInstanceID:      instanceID,
				SourceHash:            sourceHash,
				StrictMismatchReason:  groupMismatchReason,
				RelaxedDifferences:    []string{"group"},
				GroupFallbackIdentity: "kiri",
			},
		}
	}

	run := func(t *testing.T, req *FindCandidatesRequest) *FindCandidatesResponse {
		t.Helper()
		svc := &Service{
			instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
			syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
			releaseCache:     NewReleaseCache(),
			stringNormalizer: stringutils.NewDefaultNormalizer(),
		}
		response, err := svc.FindCandidates(context.Background(), req)
		require.NoError(t, err)
		return response
	}

	t.Run("the bound source torrent reaches file validation", func(t *testing.T) {
		response := run(t, newRequest())
		require.Len(t, response.Candidates, 1, "the split fansub identity must survive the apply prefilter")
		require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)
	})

	for _, tt := range []struct {
		name   string
		mutate func(*FindCandidatesRequest)
	}{
		{
			name:   "another torrent supplied the size evidence",
			mutate: func(req *FindCandidatesRequest) { req.SearchDecision.SourceHash = "someotherhash" },
		},
		{
			name:   "another instance supplied the size evidence",
			mutate: func(req *FindCandidatesRequest) { req.SearchDecision.SourceInstanceID = instanceID + 1 },
		},
		{
			name: "a stored codec rejection cannot authorize a group mismatch",
			mutate: func(req *FindCandidatesRequest) {
				req.SearchDecision.StrictMismatchReason = "codec mismatch"
				req.SearchDecision.RelaxedDifferences = []string{"codec"}
			},
		},
		{
			name:   "the search never admitted this pairing",
			mutate: func(req *FindCandidatesRequest) { req.SearchDecision.Class = searchCandidateClassRejected },
		},
		{
			name: "a claimed group relaxation the names do not support",
			mutate: func(req *FindCandidatesRequest) {
				req.TorrentName = otherFansubTagged
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest()
			tt.mutate(req)
			require.Empty(t, run(t, req).Candidates)
		})
	}
}

// Verification is decided from the rejection the search stored. A soft search
// cause cannot authorize a hard apply cause, while independently recorded hard
// causes and hard-to-soft drift can compose behind the same full-check gate.
func TestFindCandidatesRelaxationCauseCompatibility(t *testing.T) {
	t.Parallel()

	const (
		instanceID  = 1
		sourceHash  = "existing"
		otherSeason = "[KIRI] Azure Compass S02 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}

	run := func(t *testing.T, existingName string, req *FindCandidatesRequest) *FindCandidatesResponse {
		t.Helper()
		existing := qbt.Torrent{Hash: sourceHash, Name: existingName, Size: fansubPackSize, Progress: 1}
		series := 1
		if strings.Contains(existingName, "S02") {
			series = 2
		}
		files := map[string]qbt.TorrentFiles{
			sourceHash: {
				{Name: fmt.Sprintf("Azure.Compass.S%02dE01.1080p.WEB-DL.H.264-KIRI.mkv", series), Size: fansubPackSize/2 + 1},
				{Name: fmt.Sprintf("Azure.Compass.S%02dE02.1080p.WEB-DL.H.264-KIRI.mkv", series), Size: fansubPackSize - (fansubPackSize/2 + 1)},
			},
		}
		svc := &Service{
			instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
			syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
			releaseCache:     NewReleaseCache(),
			stringNormalizer: stringutils.NewDefaultNormalizer(),
		}
		response, err := svc.FindCandidates(context.Background(), req)
		require.NoError(t, err)
		return response
	}

	newRequest := func(differences ...string) *FindCandidatesRequest {
		return &FindCandidatesRequest{
			TorrentName:       fansubTaggedPack,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision: searchDecisionProvenance{
				Class:                 searchCandidateClassExactSizeFallback,
				SourceInstanceID:      instanceID,
				SourceHash:            sourceHash,
				StrictMismatchReason:  "codec mismatch",
				RelaxedDifferences:    differences,
				GroupFallbackIdentity: "kiri",
			},
		}
	}

	// A retitled listing parses Group and Site as the same fansub tag, so the
	// search recorded a group difference it was never rejected for. Apply then
	// reads the real pack name, rejects on the group, and would otherwise spend
	// that incidental token while verification still reads the stored codec.
	t.Run("a stored codec rejection cannot spend an incidental group token", func(t *testing.T) {
		require.False(t, searchRelaxationRequiresVerification("codec mismatch"),
			"the premise: a stored codec rejection seeds without a hash check")
		require.Empty(t, run(t, fansubBracketPack, newRequest("codec", "group")).Candidates)
	})

	t.Run("a stored codec rejection cannot spend an incidental season token", func(t *testing.T) {
		require.Empty(t, run(t, otherSeason, newRequest("codec", "season")).Candidates)
	})

	t.Run("a stored group rejection may spend a recorded codec behind verification", func(t *testing.T) {
		req := newRequest("group", "codec")
		req.SearchDecision.StrictMismatchReason = groupMismatchReason
		existingName := strings.Replace(fansubTaggedPack, "H.264", "H.265", 1)

		require.Len(t, run(t, existingName, req).Candidates, 1)
	})

	t.Run("two independently recorded verification causes may compose", func(t *testing.T) {
		req := newRequest("season", "group")
		req.SearchDecision.StrictMismatchReason = seasonMismatchReason

		response := run(t, fansubBracketPack, req)
		require.Len(t, response.Candidates, 1,
			"search recorded both hard relaxations, so apply may rederive either one behind the same full-check gate")
	})

	t.Run("group search cause may replay a recorded season cause", func(t *testing.T) {
		req := newRequest("group", "season")
		req.SearchDecision.StrictMismatchReason = groupMismatchReason

		response := run(t, otherSeason, req)
		require.Len(t, response.Candidates, 1,
			"the accepted apply-time season cause must carry its own match-type bypass")
	})
}

// Search can heal a bracket episode's identity from its selected file without
// recording a relaxation. Apply must select the same file-derived identity for
// the bound source even though the raw torrent name already parses as TV.
func TestFindCandidates_ReplaysFileDerivedStrictEpisodeIdentity(t *testing.T) {
	t.Parallel()

	const (
		instanceID     = 1
		sourceHash     = "existing-episode"
		bracketEpisode = "[KIRI] Azure Compass S01E01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
		taggedEpisode  = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: bracketEpisode, Size: fansubPackSize, Progress: 1}
	files := map[string]qbt.TorrentFiles{
		sourceHash: {
			{Name: taggedEpisode + ".mkv", Size: fansubPackSize},
		},
	}
	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       taggedEpisode,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision: searchDecisionProvenance{
			Class:            searchCandidateClassStrict,
			SourceInstanceID: instanceID,
			SourceHash:       sourceHash,
		},
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1)
	require.Equal(t, sourceHash, response.Candidates[0].Torrents[0].Hash)
}

func TestFindCandidates_DerivesBoundPackBeforeEpisodeShapeGate(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		sourceHash = "raw-episode-pack"
		rawEpisode = "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI"
		packName   = "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI"
		size       = int64(2000)
	)
	files := qbt.TorrentFiles{
		{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: 1001},
		{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: 999},
	}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	torrent := qbt.Torrent{Hash: sourceHash, Name: rawEpisode, Size: size, TotalSize: size, Progress: 1}
	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{sourceHash: files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	raw := svc.releaseCache.Parse(rawEpisode)
	require.True(t, isTVEpisode(raw), "the raw fixture must reach the episode shape gate")
	view := svc.searchSourceReleaseViewFromFiles(context.Background(), &torrent, raw, files)
	require.True(t, isTVSeasonPack(view.release), "search must derive the pack from its files")
	decision := svc.classifySearchCandidate(searchCandidateInput{
		Source:                 view,
		Candidate:              namedRelease{release: svc.releaseCache.Parse(packName), rawName: packName},
		SourceSize:             size,
		CandidateSize:          size,
		FindIndividualEpisodes: true,
	})
	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassStrict, decision.Class)

	response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:            packName,
		TargetInstanceIDs:      []int{instanceID},
		FindIndividualEpisodes: true,
		SearchDecision:         decision.provenance().bindSource(instanceID, sourceHash),
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"apply must derive the bound pack before it rejects pack-from-episode pairings")
	require.False(t, response.seasonPackEpisodeCandidates,
		"the derived pack must not trigger the episode-based assembly diversion")

	result := svc.processCrossSeedCandidate(
		context.Background(),
		response.Candidates[0],
		nil,
		"incoming",
		"",
		packName,
		&CrossSeedRequest{SearchDecision: decision.provenance().bindSource(instanceID, sourceHash)},
		svc.releaseCache.Parse(packName),
		files,
		nil,
	)
	require.NotEqual(t, "no_match", result.Status,
		"the add-plan gate must use the same derived pack identity as candidate discovery")
	require.Contains(t, result.Message, "torrent properties",
		"the fixture must pass plan selection and stop at its unimplemented property lookup")
}

func TestFindCandidates_ReplaysFileStructureWithoutReplacingExplicitGroup(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		sourceHash   = "explicit-group-source"
		sourceName   = "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
		incomingName = "Azure.Compass.S02.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	)
	files := qbt.TorrentFiles{
		{Name: "Azure.Compass.S02E01.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv", Size: 5001},
		{Name: "Azure.Compass.S02E02.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv", Size: 5002},
	}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
			Hash: sourceHash, Name: sourceName, Progress: 1,
		}}, map[string]qbt.TorrentFiles{sourceHash: files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	raw := svc.releaseCache.Parse(sourceName)
	require.True(t, releaseHasExplicitGroupTag(raw))
	view := svc.searchSourceReleaseViewFromFiles(context.Background(), &qbt.Torrent{Name: sourceName}, raw, files)
	require.Equal(t, 2, view.release.Series)
	require.Equal(t, "KIRI", view.release.Group)

	response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       incomingName,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision: searchDecisionProvenance{
			Class:            searchCandidateClassStrict,
			SourceInstanceID: instanceID,
			SourceHash:       sourceHash,
		},
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1)
}

func TestFindCandidates_ReplaysRelaxedStructureWhenRawNamesMatch(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		sourceHash = "retitled-season-source"
		rawS01     = "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
		size       = int64(10_003)
	)
	files := qbt.TorrentFiles{
		{Name: "Azure.Compass.S02E01.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv", Size: 5001},
		{Name: "Azure.Compass.S02E02.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv", Size: 5002},
	}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	torrent := qbt.Torrent{Hash: sourceHash, Name: rawS01, Size: size, TotalSize: size, Progress: 1}
	svc := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{sourceHash: files}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	rawSource := svc.releaseCache.Parse(rawS01)
	searchSource := svc.searchSourceReleaseViewFromFiles(context.Background(), &torrent, rawSource, files)
	require.Equal(t, 2, searchSource.release.Series)
	decision := svc.classifySearchCandidate(searchCandidateInput{
		Source:        searchSource,
		Candidate:     namedRelease{release: svc.releaseCache.Parse(rawS01), rawName: rawS01},
		SourceSize:    size,
		CandidateSize: size,
	})
	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.Equal(t, seasonMismatchReason, decision.StrictMismatchReason)

	response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       rawS01,
		TargetInstanceIDs: []int{instanceID},
		SearchDecision: searchDecisionProvenance{
			Class:                decision.Class,
			SourceInstanceID:     instanceID,
			SourceHash:           sourceHash,
			StrictMismatchReason: decision.StrictMismatchReason,
			RelaxedDifferences:   decision.RelaxedDifferences,
		},
	})
	require.NoError(t, err)
	require.Len(t, response.Candidates, 1,
		"raw S01 equality must not hide the bound source's file-derived S02 relaxation")
}

// enrichReleaseFromTorrent fills a missing Group from the torrent name but keeps
// the file's tags, so an enriched release carries a group its own tags never
// mention. Reading provenance off those tags alone reads every enriched group as
// a word rls merely guessed at. The raw name has to settle it.
func TestGroupSiteFallback_ReadsProvenanceThroughEnrichment(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()
	enrich := func(torrentName, fileName string) *rls.Release {
		enriched := enrichReleaseFromTorrent(
			svc.releaseCache.Parse(fileName),
			svc.releaseCache.Parse(torrentName),
		)
		require.False(t, releaseHasGroupTag(enriched, enriched.Group),
			"fixture must lose the group tag: the enriched release keeps the file's tags")
		return enriched
	}

	t.Run("an enriched scene group is not up for reinterpretation", func(t *testing.T) {
		const torrentName = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-FoV"
		enriched := enrich(torrentName, "[eztv] Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264.mkv")
		require.Equal(t, "FoV", enriched.Group, "the group comes from the torrent name")
		require.Equal(t, "eztv", enriched.Site, "the site comes from the file name")

		require.False(t, svc.crossFieldGroupSiteFallback(
			namedRelease{release: enriched, rawName: torrentName},
			namedRelease{release: svc.releaseCache.Parse(labelAsTaggedGroup), rawName: labelAsTaggedGroup}),
			"FoV must not be reinterpreted as the eztv label")
	})

	t.Run("an enriched scene group still rescues the bracket spelling", func(t *testing.T) {
		const torrentName = fansubTaggedPack
		enriched := enrich(torrentName, "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264.mkv")
		require.Equal(t, "KIRI", enriched.Group)

		require.True(t, svc.crossFieldGroupSiteFallback(
			namedRelease{release: enriched, rawName: torrentName},
			namedRelease{release: svc.releaseCache.Parse(fansubBracketPack), rawName: fansubBracketPack}),
			"losing the tag to enrichment must not lose the rescue")
	})
}

// The mirror of the case above: the file carries a real group tag the torrent
// name never had, so the enriched release keeps its own provenance and the raw
// name is the one that knows nothing.
func TestGroupSiteFallback_KeepsFileGroupProvenance(t *testing.T) {
	t.Parallel()

	svc := newGroupFallbackService()
	const torrentName = labelWithoutGroup
	enriched := enrichReleaseFromTorrent(
		svc.releaseCache.Parse("[eztv] Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv"),
		svc.releaseCache.Parse(torrentName),
	)
	require.Equal(t, "KIRI", enriched.Group, "the file's own group tag survives enrichment")
	require.Equal(t, "eztv", enriched.Site)
	require.False(t, releaseHasGroupTag(releases.DefaultParser.Parse(torrentName), enriched.Group),
		"fixture must give the torrent name no say: it never mentions this group")

	require.False(t, svc.crossFieldGroupSiteFallback(
		namedRelease{release: enriched, rawName: torrentName},
		namedRelease{release: svc.releaseCache.Parse(labelAsTaggedGroup), rawName: labelAsTaggedGroup}),
		"a group the file tagged properly is not a word rls guessed at")
}

// A release selected from a file keeps that file's tags while its raw torrent
// name can carry another explicit group. Both origins are evidence: a raw group
// may corroborate the split Site, but a different raw group must veto the
// exact-size rescue on either side.
func TestGroupSiteFallback_RejectsConflictingEnrichmentProvenance(t *testing.T) {
	t.Parallel()

	const (
		rawFoV         = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-FoV"
		rawKIRI        = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
		bracketEpisode = "[KIRI] Azure Compass S01E01 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
		taggedEpisode  = "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	)

	svc := newGroupFallbackService()
	splitFrom := func(rawName string) *rls.Release {
		return enrichReleaseFromTorrent(
			svc.releaseCache.Parse(bracketEpisode+".mkv"),
			svc.releaseCache.Parse(rawName),
		)
	}
	taggedFrom := func(rawName string) *rls.Release {
		return enrichReleaseFromTorrent(
			svc.releaseCache.Parse(taggedEpisode+".mkv"),
			svc.releaseCache.Parse(rawName),
		)
	}

	splitWithConflict := splitFrom(rawFoV)
	require.Equal(t, "Batch", splitWithConflict.Group)
	require.Equal(t, "KIRI", splitWithConflict.Site)
	require.True(t, releaseHasGroupTag(svc.releaseCache.Parse(rawFoV), "FoV"),
		"fixture must carry an explicit conflicting raw group")

	taggedWithConflict := taggedFrom(rawFoV)
	require.Equal(t, "KIRI", taggedWithConflict.Group)
	require.Empty(t, taggedWithConflict.Site)
	require.True(t, releaseHasGroupTag(taggedWithConflict, "KIRI"),
		"fixture must keep its file-level explicit group")

	for _, tt := range []struct {
		name             string
		sourceRelease    *rls.Release
		sourceName       string
		candidateRelease *rls.Release
		candidateName    string
		wantAccepted     bool
	}{
		{
			name:             "the split side raw name conflicts",
			sourceRelease:    splitWithConflict,
			sourceName:       rawFoV,
			candidateRelease: svc.releaseCache.Parse(taggedEpisode),
			candidateName:    taggedEpisode,
		},
		{
			name:             "the tagged side raw name conflicts",
			sourceRelease:    taggedWithConflict,
			sourceName:       rawFoV,
			candidateRelease: svc.releaseCache.Parse(bracketEpisode),
			candidateName:    bracketEpisode,
		},
		{
			name:             "the split side raw group corroborates its site",
			sourceRelease:    splitFrom(rawKIRI),
			sourceName:       rawKIRI,
			candidateRelease: svc.releaseCache.Parse(taggedEpisode),
			candidateName:    taggedEpisode,
			wantAccepted:     true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decision := svc.classifySearchCandidate(searchCandidateInput{
				Source:        namedRelease{release: tt.sourceRelease, rawName: tt.sourceName},
				Candidate:     namedRelease{release: tt.candidateRelease, rawName: tt.candidateName},
				SourceSize:    fansubPackSize,
				CandidateSize: fansubPackSize,
			})

			if tt.wantAccepted {
				require.True(t, decision.Accepted, "reject reason: %s", decision.RejectReason)
				require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
				require.Equal(t, groupMismatchReason, decision.StrictMismatchReason)
				require.Contains(t, decision.RelaxedDifferences, "group")
				return
			}

			require.False(t, decision.Accepted, "class: %s", decision.Class)
			require.Equal(t, "group/site mismatch", decision.RejectReason)
			require.NotContains(t, decision.RelaxedDifferences, "group")
		})
	}
}

// Season-pack search keeps the torrent name's identity fields but must also
// retain the selected file's group tags as provenance. Otherwise the merge that
// restores the pack shape can erase a conflicting explicit file group and turn
// it into an exact-size group rescue.
func TestSearchTorrentMatches_GroupFallbackPreservesSelectedFileProvenance(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "0123456789abcdef0123456789abcdef01234567"
	)

	for _, tt := range []struct {
		name          string
		fileGroup     string
		wantResults   int
		wantDecision  searchCandidateClass
		wantRelaxedBy string
	}{
		{name: "conflicting file group vetoes rescue", fileGroup: "FoV"},
		{
			name:          "corroborating file group keeps rescue",
			fileGroup:     "KIRI",
			wantResults:   1,
			wantDecision:  searchCandidateClassExactSizeFallback,
			wantRelaxedBy: "group",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/rss+xml")
				_, err := fmt.Fprintf(w, `<rss version="2.0"><channel><title>Anime Indexer</title><item>
					<title>%s</title><guid>group-provenance</guid><size>%d</size>
					<enclosure url="%s/candidate.torrent" length="%d" type="application/x-bittorrent" />
				</item></channel></rss>`, fansubTaggedPack, fansubPackSize, server.URL, fansubPackSize)
				if err != nil {
					t.Errorf("write Torznab response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			firstSize := fansubPackSize/2 + 1
			sourceFiles := qbt.TorrentFiles{
				{
					Name: "[KIRI] Azure Compass S01E01 1080p WEB-DL H.264-" + tt.fileGroup + ".mkv",
					Size: firstSize,
				},
				{
					Name: "[KIRI] Azure Compass S01E02 1080p WEB-DL H.264-" + tt.fileGroup + ".mkv",
					Size: fansubPackSize - firstSize,
				},
			}
			instance := &models.Instance{ID: instanceID, Name: "main"}
			source := qbt.Torrent{
				Hash:      sourceHash,
				Name:      fansubBracketPack,
				Size:      fansubPackSize,
				TotalSize: fansubPackSize,
				Progress:  1,
			}
			svc := &Service{
				instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
				jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
					{
						ID:             1,
						Name:           "Anime Indexer",
						BaseURL:        server.URL,
						Backend:        models.TorznabBackendNative,
						TimeoutSeconds: 5,
						Enabled:        true,
					},
				}),
				syncManager:      newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{sourceHash: sourceFiles}),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}

			selected, usedFile := svc.selectContentDetectionRelease(source.Name, svc.releaseCache.Parse(source.Name), sourceFiles)
			require.True(t, usedFile, "fixture must select the largest file")
			require.Equal(t, tt.fileGroup, selected.Group)
			require.True(t, releaseHasGroupTag(selected, tt.fileGroup), "fixture must carry an explicit selected-file group")

			response, _, _, err := svc.searchTorrentMatches(
				context.Background(),
				instanceID,
				sourceHash,
				TorrentSearchOptions{IndexerIDs: []int{1}, SkipGazelle: true},
				nil,
			)
			require.NoError(t, err)
			require.Len(t, response.Results, tt.wantResults)
			if tt.wantResults == 0 {
				return
			}
			require.Equal(t, tt.wantDecision, response.Results[0].SearchDecision.Class)
			require.Equal(t, groupMismatchReason, response.Results[0].SearchDecision.StrictMismatchReason)
			require.Contains(t, response.Results[0].SearchDecision.RelaxedDifferences, tt.wantRelaxedBy)
			require.Equal(t, "kiri", response.Results[0].SearchDecision.GroupFallbackIdentity)
		})
	}
}

// Apply must retain the same selected-file provenance on both sides of the
// pairing. Search cannot inspect the downloaded candidate's files, and apply
// reparses the existing source, so either season-pack merge can otherwise hide
// an explicit group conflict behind the Group/Site fallback.
func TestFindCandidates_GroupFallbackPreservesSelectedFileProvenance(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		sourceHash = "bound-source"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}

	t.Run("incoming selected-file conflict vetoes rescue", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			fileGroup   string
			wantMatches int
		}{
			{name: "conflicting group", fileGroup: "FoV"},
			{name: "corroborating group", fileGroup: "KIRI", wantMatches: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				targetFiles := qbt.TorrentFiles{
					{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize/2 + 1},
					{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
				}
				existingFiles := qbt.TorrentFiles{
					{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize/2 + 1},
					{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
				}
				svc := &Service{
					instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
					syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
						Hash: sourceHash, Name: fansubTaggedPack, Size: fansubPackSize, Progress: 1,
					}}, map[string]qbt.TorrentFiles{sourceHash: existingFiles}),
					releaseCache:     NewReleaseCache(),
					stringNormalizer: stringutils.NewDefaultNormalizer(),
				}

				target := svc.applyTargetReleaseViewFromFiles(
					fansubBracketPack,
					svc.releaseCache.Parse(fansubBracketPack),
					targetFiles,
					true,
				)
				require.NotNil(t, target.release)
				require.NotNil(t, target.tagOrigin)
				require.True(t, releaseHasGroupTag(target.tagOrigin, tt.fileGroup))

				response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
					TorrentName:       fansubBracketPack,
					TargetRelease:     target,
					TargetInstanceIDs: []int{instanceID},
					SearchDecision: searchDecisionProvenance{
						Class:                 searchCandidateClassExactSizeFallback,
						SourceInstanceID:      instanceID,
						SourceHash:            sourceHash,
						StrictMismatchReason:  groupMismatchReason,
						RelaxedDifferences:    []string{"group"},
						GroupFallbackIdentity: "kiri",
					},
				})
				require.NoError(t, err)
				require.Len(t, response.Candidates, tt.wantMatches)
			})
		}
	})

	t.Run("existing selected-file conflict vetoes rescue", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			fileGroup   string
			wantMatches int
		}{
			{name: "conflicting group", fileGroup: "FoV"},
			{name: "corroborating group", fileGroup: "KIRI", wantMatches: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				sourceFiles := qbt.TorrentFiles{
					{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize/2 + 1},
					{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
				}
				svc := &Service{
					instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
					syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
						Hash: sourceHash, Name: fansubBracketPack, Size: fansubPackSize, Progress: 1,
					}}, map[string]qbt.TorrentFiles{sourceHash: sourceFiles}),
					releaseCache:     NewReleaseCache(),
					stringNormalizer: stringutils.NewDefaultNormalizer(),
				}

				response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
					TorrentName:       fansubTaggedPack,
					TargetInstanceIDs: []int{instanceID},
					SearchDecision: searchDecisionProvenance{
						Class:                 searchCandidateClassExactSizeFallback,
						SourceInstanceID:      instanceID,
						SourceHash:            sourceHash,
						StrictMismatchReason:  groupMismatchReason,
						RelaxedDifferences:    []string{"group"},
						GroupFallbackIdentity: "kiri",
					},
				})
				require.NoError(t, err)
				require.Len(t, response.Candidates, tt.wantMatches)
			})
		}
	})

	t.Run("strict raw replay still checks selected-file conflicts", func(t *testing.T) {
		existingFiles := qbt.TorrentFiles{
			{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize/2 + 1},
			{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
		}
		for _, tt := range []struct {
			name        string
			fileGroup   string
			wantMatches int
		}{
			{name: "conflicting incoming group", fileGroup: "FoV"},
			{name: "fallback word cannot become the explicit group", fileGroup: "Batch"},
			{name: "corroborating incoming group", fileGroup: "KIRI", wantMatches: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				targetFiles := qbt.TorrentFiles{
					{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize/2 + 1},
					{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
				}
				svc := &Service{
					instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
					syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
						Hash: sourceHash, Name: fansubBracketPack, Size: fansubPackSize, Progress: 1,
					}}, map[string]qbt.TorrentFiles{sourceHash: existingFiles}),
					releaseCache:     NewReleaseCache(),
					stringNormalizer: stringutils.NewDefaultNormalizer(),
				}
				target := svc.applyTargetReleaseViewFromFiles(
					fansubBracketPack,
					svc.releaseCache.Parse(fansubBracketPack),
					targetFiles,
					true,
				)

				response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
					TorrentName:       fansubBracketPack,
					TargetRelease:     target,
					TargetInstanceIDs: []int{instanceID},
					SearchDecision: searchDecisionProvenance{
						Class:                 searchCandidateClassExactSizeFallback,
						SourceInstanceID:      instanceID,
						SourceHash:            sourceHash,
						StrictMismatchReason:  groupMismatchReason,
						RelaxedDifferences:    []string{"group"},
						GroupFallbackIdentity: "kiri",
					},
				})
				require.NoError(t, err)
				require.Len(t, response.Candidates, tt.wantMatches)
			})
		}
	})

	t.Run("non-group fallback still checks selected-file conflicts", func(t *testing.T) {
		const targetName = "Azure.Compass.S01.1080p.WEBRip.AAC2.0.H.264-KIRI"
		existingFiles := qbt.TorrentFiles{
			{Name: "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize/2 + 1},
			{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
		}
		for _, tt := range []struct {
			fileGroup   string
			wantMatches int
		}{
			{fileGroup: "FoV"},
			{fileGroup: "KIRI", wantMatches: 1},
		} {
			t.Run(tt.fileGroup, func(t *testing.T) {
				targetFiles := qbt.TorrentFiles{
					{Name: "Azure.Compass.S01E01.1080p.WEBRip.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize/2 + 1},
					{Name: "Azure.Compass.S01E02.1080p.WEBRip.H.264-" + tt.fileGroup + ".mkv", Size: fansubPackSize - (fansubPackSize/2 + 1)},
				}
				svc := &Service{
					instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
					syncManager: newFakeSyncManager(instance, []qbt.Torrent{{
						Hash: sourceHash, Name: fansubTaggedPack, Size: fansubPackSize, Progress: 1,
					}}, map[string]qbt.TorrentFiles{sourceHash: existingFiles}),
					releaseCache:     NewReleaseCache(),
					stringNormalizer: stringutils.NewDefaultNormalizer(),
				}
				target := svc.applyTargetReleaseViewFromFiles(
					targetName,
					svc.releaseCache.Parse(targetName),
					targetFiles,
					true,
				)
				require.Equal(t, "KIRI", target.release.Group)
				require.True(t, releaseHasGroupTag(target.tagOrigin, tt.fileGroup))

				response, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
					TorrentName:       targetName,
					TargetRelease:     target,
					TargetInstanceIDs: []int{instanceID},
					SearchDecision: searchDecisionProvenance{
						Class:                searchCandidateClassExactSizeFallback,
						SourceInstanceID:     instanceID,
						SourceHash:           sourceHash,
						StrictMismatchReason: sourceMismatchReason,
						RelaxedDifferences:   []string{"source"},
					},
				})
				require.NoError(t, err)
				require.Len(t, response.Candidates, tt.wantMatches)
			})
		}
	})
}
