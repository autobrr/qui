// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestClassifySearchCandidateExactSizeFallback(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const (
		sourceName    = "Example.Show.2024.S01.2160p.ATV.WEB-DL.DDP5.1.DV.HDR.H.265-NTb"
		candidateName = "Example Show 2024 S01 2160p ATVP WEB-DL DD+ 5.1 DV HDR10+ H.265-NTb"
		size          = int64(94_329_473_840)
	)
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)

	strict, strictReason := service.releasesMatchWithReasonAndNamesAndTitles(
		&source, &candidate, sourceName, candidateName, nil, nil, false,
	)
	require.False(t, strict)
	require.NotEmpty(t, strictReason)

	decision := service.classifySearchCandidate(searchCandidateInput{
		SourceRelease:    &source,
		CandidateRelease: &candidate,
		SourceName:       sourceName,
		CandidateName:    candidateName,
		SourceSize:       size,
		CandidateSize:    size,
		TolerancePercent: 5,
	})

	require.True(t, decision.Accepted)
	require.Equal(t, searchCandidateClassExactSizeFallback, decision.Class)
	require.True(t, decision.ExactSize)
	require.Contains(t, decision.RelaxedDifferences, "collection")
	require.Contains(t, decision.RelaxedDifferences, "hdr")
	require.Contains(t, decision.MatchReason, "exact byte size")
	require.Contains(t, decision.MatchReason, "relaxed collection")
}

func TestClassifySearchCandidateExactSizePreconditions(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	sourceName := "Example.Show.S01.2160p.ATV.WEB-DL.HDR.H.265-NTb"
	candidateName := "Example.Show.S01.2160p.ATVP.WEB-DL.HDR10+.H.265-NTb"
	source := rls.ParseString(sourceName)
	candidate := rls.ParseString(candidateName)
	const size = int64(9_432_947_384)

	tests := []struct {
		name          string
		sourceSize    int64
		candidateSize int64
	}{
		{name: "one byte difference", sourceSize: size, candidateSize: size + 1},
		{name: "within tolerance is not exact", sourceSize: size, candidateSize: size + size/100},
		{name: "zero equals zero", sourceSize: 0, candidateSize: 0},
		{name: "missing source size", sourceSize: 0, candidateSize: size},
		{name: "missing candidate size", sourceSize: size, candidateSize: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := service.classifySearchCandidate(searchCandidateInput{
				SourceRelease:    &source,
				CandidateRelease: &candidate,
				SourceName:       sourceName,
				CandidateName:    candidateName,
				SourceSize:       test.sourceSize,
				CandidateSize:    test.candidateSize,
				TolerancePercent: 5,
			})
			require.False(t, decision.Accepted)
			require.NotEqual(t, searchCandidateClassExactSizeFallback, decision.Class)
		})
	}
}

func TestClassifySearchCandidateExactSizeHardIdentity(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const size = int64(94_329_473_840)
	base := rls.ParseString("Example.Show.2024.S01.2160p.ATV.WEB-DL.HDR.H.265-NTb")

	tests := []struct {
		name   string
		mutate func(*rls.Release)
		reason string
	}{
		{name: "different title", mutate: func(r *rls.Release) { r.Title = "Different Show" }, reason: "title mismatch"},
		{name: "different season", mutate: func(r *rls.Release) { r.Series++ }, reason: "season mismatch"},
		{name: "missing resolution", mutate: func(r *rls.Release) { r.Resolution = "" }, reason: "resolution mismatch"},
		{name: "different resolution", mutate: func(r *rls.Release) { r.Resolution = "1080p" }, reason: "resolution mismatch"},
		{name: "missing group", mutate: func(r *rls.Release) { r.Group = ""; r.Site = "" }, reason: "group/site mismatch"},
		{name: "different group", mutate: func(r *rls.Release) { r.Group = "FLUX" }, reason: "group/site mismatch"},
		{name: "different checksum", mutate: func(r *rls.Release) { r.Sum = "DEADBEEF" }, reason: "checksum mismatch"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := base
			candidate := base
			source.Collection = "ATV"
			candidate.Collection = "ATVP"
			if test.name == "different checksum" {
				source.Sum = "AAAAAAAA"
			}
			test.mutate(&candidate)
			decision := service.classifySearchCandidate(searchCandidateInput{
				SourceRelease:    &source,
				CandidateRelease: &candidate,
				SourceName:       source.Title,
				CandidateName:    candidate.Title,
				SourceSize:       size,
				CandidateSize:    size,
				TolerancePercent: 5,
			})
			require.False(t, decision.Accepted)
			require.Equal(t, test.reason, decision.RejectReason)
		})
	}
}

func TestClassifySearchCandidateExactSizeTVAndContentIdentity(t *testing.T) {
	service := &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
	const size = int64(94_329_473_840)

	t.Run("different episode", func(t *testing.T) {
		source := rls.ParseString("Example.Show.S01E01.2160p.ATV.WEB-DL.H.265-NTb")
		candidate := rls.ParseString("Example.Show.S01E02.2160p.ATVP.WEB-DL.H.265-NTb")
		decision := service.classifySearchCandidate(searchCandidateInput{
			SourceRelease:    &source,
			CandidateRelease: &candidate,
			SourceName:       source.Title,
			CandidateName:    candidate.Title,
			SourceSize:       size,
			CandidateSize:    size,
			TolerancePercent: 5,
		})
		require.False(t, decision.Accepted)
		require.Equal(t, "episode mismatch", decision.RejectReason)
	})

	t.Run("forbidden season pack from episode", func(t *testing.T) {
		source := rls.ParseString("Example.Show.S01E01.2160p.ATV.WEB-DL.H.265-NTb")
		candidate := rls.ParseString("Example.Show.S01.2160p.ATVP.WEB-DL.H.265-NTb")
		decision := service.classifySearchCandidate(searchCandidateInput{
			SourceRelease:          &source,
			CandidateRelease:       &candidate,
			SourceName:             source.Title,
			CandidateName:          candidate.Title,
			SourceSize:             size,
			CandidateSize:          size,
			TolerancePercent:       5,
			FindIndividualEpisodes: true,
		})
		require.False(t, decision.Accepted)
		require.Equal(t, rejectReasonSeasonPackFromEpisode, decision.RejectReason)
	})

	t.Run("different non TV type", func(t *testing.T) {
		source := rls.Release{Type: rls.Movie, Title: "Shared Title", Resolution: "2160p", Group: "NTb", Collection: "ATV"}
		candidate := source
		candidate.Type = rls.Music
		candidate.Collection = "ATVP"
		decision := service.classifySearchCandidate(searchCandidateInput{
			SourceRelease:    &source,
			CandidateRelease: &candidate,
			SourceName:       source.Title,
			CandidateName:    candidate.Title,
			SourceSize:       size,
			CandidateSize:    size,
			TolerancePercent: 5,
		})
		require.False(t, decision.Accepted)
		require.Equal(t, "content type mismatch", decision.RejectReason)
	})
}

func TestSearchCandidateInternalMetadataIsNotJSON(t *testing.T) {
	resultBytes, err := json.Marshal(TorrentSearchResult{
		Title:               "candidate",
		SearchDecisionClass: searchCandidateClassExactSizeFallback,
	})
	require.NoError(t, err)
	require.NotContains(t, string(resultBytes), "exact-size-fallback")

	requestBytes, err := json.Marshal(CrossSeedRequest{
		SearchDecisionClass:     searchCandidateClassExactSizeFallback,
		AdvertisedCandidateSize: 123,
	})
	require.NoError(t, err)
	require.NotContains(t, string(requestBytes), "exact-size-fallback")
	require.NotContains(t, string(requestBytes), "123")
}

func TestSortScoredTorrentSearchResultsExactSizePriority(t *testing.T) {
	now := time.Now()
	items := []scoredTorrentSearchResult{
		{result: jackett.SearchResult{Title: "tolerance", Seeders: 100, PublishDate: now}, score: 10, class: searchCandidateClassStrict},
		{result: jackett.SearchResult{Title: "fallback", Seeders: 1, PublishDate: now.Add(-time.Hour)}, score: 2, exactSize: true, class: searchCandidateClassExactSizeFallback},
		{result: jackett.SearchResult{Title: "strict", Seeders: 0, PublishDate: now.Add(-2 * time.Hour)}, score: 1, exactSize: true, class: searchCandidateClassStrict},
	}

	sortScoredTorrentSearchResults(items)

	require.Equal(t, []string{"strict", "fallback", "tolerance"}, []string{
		items[0].result.Title,
		items[1].result.Title,
		items[2].result.Title,
	})
}

func TestExecuteCrossSeedSearchAttemptPropagatesExactSizeDecision(t *testing.T) {
	const size = int64(94_329_473_840)
	var captured *CrossSeedRequest
	service := &Service{
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: func(_ context.Context, request *CrossSeedRequest) (*CrossSeedResponse, error) {
			captured = request
			return &CrossSeedResponse{Success: true}, nil
		},
	}
	state := &searchRunState{opts: SearchRunOptions{InstanceID: 1}}
	match := TorrentSearchResult{
		Indexer:             "Indexer",
		IndexerID:           7,
		Title:               "candidate",
		DownloadURL:         "https://example.invalid/candidate.torrent",
		Size:                size,
		SearchDecisionClass: searchCandidateClassExactSizeFallback,
	}

	result, err := service.executeCrossSeedSearchAttempt(
		context.Background(),
		state,
		&qbt.Torrent{Hash: "source", Name: "source"},
		match,
		time.Now(),
	)

	require.NoError(t, err)
	require.Equal(t, models.CrossSeedSearchResultStatusAdded, result.Status)
	require.NotNil(t, captured)
	require.Equal(t, searchCandidateClassExactSizeFallback, captured.SearchDecisionClass)
	require.Equal(t, size, captured.AdvertisedCandidateSize)
}

func TestCrossSeedRejectsInvalidAdvertisedExactSize(t *testing.T) {
	torrentData := createTestTorrent(
		t,
		"Example.Show.S01.2160p.ATVP.WEB-DL.HDR10+.H.265-NTb",
		[]string{"Example.Show.S01E01.mkv"},
		256*1024,
	)
	meta, err := ParseTorrentMetadataWithInfo(torrentData)
	require.NoError(t, err)
	var actualSize int64
	for _, file := range meta.Files {
		actualSize += file.Size
	}
	service := &Service{releaseCache: NewReleaseCache()}

	_, err = service.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:             base64.StdEncoding.EncodeToString(torrentData),
		SearchDecisionClass:     searchCandidateClassExactSizeFallback,
		AdvertisedCandidateSize: actualSize + 1,
		TargetInstanceIDs:       []int{1},
		FindIndividualEpisodes:  false,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "advertised size")
	require.Contains(t, err.Error(), "downloaded torrent size")
}

func TestFindCandidatesExactSizeFallbackIsScopedAndContinuesToFileValidation(t *testing.T) {
	const (
		instanceID   = 1
		torrentSize  = int64(94_329_473_840)
		targetName   = "Example.Show.2024.S01.2160p.ATVP.WEB-DL.DV.HDR10+.H.265-NTb"
		existingName = "Example.Show.2024.S01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb"
		existingHash = "existing"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{
		Hash:     existingHash,
		Name:     existingName,
		Size:     torrentSize,
		Progress: 1,
	}
	files := map[string]qbt.TorrentFiles{
		existingHash: {
			{
				Name: "Example.Show.2024.S01E01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize,
			},
		},
	}
	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	directResponse, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.Empty(t, directResponse.Candidates, "direct requests must retain strict release matching")

	fallbackResponse, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
		ExactSizeFallback: true,
		TorrentSize:       torrentSize,
	})
	require.NoError(t, err)
	require.Len(t, fallbackResponse.Candidates, 1)
	require.Len(t, fallbackResponse.Candidates[0].Torrents, 1)
	require.Equal(t, existingHash, fallbackResponse.Candidates[0].Torrents[0].Hash)
	require.NotEmpty(t, fallbackResponse.Candidates[0].MatchType)

	wrongSizeResponse, err := service.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
		ExactSizeFallback: true,
		TorrentSize:       torrentSize + 1,
	})
	require.NoError(t, err)
	require.Empty(t, wrongSizeResponse.Candidates)

	incompatibleFiles := map[string]qbt.TorrentFiles{
		existingHash: {
			{
				Name: "Example.Show.2024.S02E01.2160p.ATV.WEB-DL.DV.HDR.H.265-NTb.mkv",
				Size: torrentSize,
			},
		},
	}
	incompatibleService := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{existing}, incompatibleFiles),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	incompatibleResponse, err := incompatibleService.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       targetName,
		TargetInstanceIDs: []int{instanceID},
		ExactSizeFallback: true,
		TorrentSize:       torrentSize,
	})
	require.NoError(t, err)
	require.Empty(t, incompatibleResponse.Candidates, "fallback must not bypass file-level release validation")
}
