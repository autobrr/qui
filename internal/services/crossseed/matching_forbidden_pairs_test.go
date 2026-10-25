// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestIsTVEpisode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		release  *rls.Release
		expected bool
	}{
		{
			name:     "nil release",
			release:  nil,
			expected: false,
		},
		{
			name:     "empty release",
			release:  &rls.Release{},
			expected: false,
		},
		{
			name:     "movie (no series/episode)",
			release:  &rls.Release{Title: "Movie", Year: 2024},
			expected: false,
		},
		{
			name:     "season pack (series only)",
			release:  &rls.Release{Series: 1, Episode: 0},
			expected: false,
		},
		{
			name:     "episode (series and episode)",
			release:  &rls.Release{Series: 1, Episode: 5},
			expected: true,
		},
		{
			name:     "seasonless anime episode",
			release:  &rls.Release{Type: rls.Episode, Episode: 5},
			expected: true,
		},
		{
			name:     "episode type without episode number",
			release:  &rls.Release{Type: rls.Episode, Episode: 0},
			expected: false,
		},
		{
			name:     "episode type with series and episode",
			release:  &rls.Release{Type: rls.Episode, Series: 1, Episode: 5},
			expected: true,
		},
		{
			name:     "series type with episode number",
			release:  &rls.Release{Type: rls.Series, Episode: 5},
			expected: true,
		},
		{
			name:     "episode from season 2 (S02E03)",
			release:  &rls.Release{Series: 2, Episode: 3},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, isTVEpisode(tt.release))
		})
	}
}

func TestIsTVSeasonPack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		release  *rls.Release
		expected bool
	}{
		{
			name:     "nil release",
			release:  nil,
			expected: false,
		},
		{
			name:     "empty release",
			release:  &rls.Release{},
			expected: false,
		},
		{
			name:     "movie (no series/episode)",
			release:  &rls.Release{Title: "Movie", Year: 2024},
			expected: false,
		},
		{
			name:     "season pack (series only)",
			release:  &rls.Release{Series: 1, Episode: 0},
			expected: true,
		},
		{
			name:     "seasonless anime pack",
			release:  &rls.Release{Type: rls.Series, Episode: 0},
			expected: true,
		},
		{
			name:     "series type with series and no episode",
			release:  &rls.Release{Type: rls.Series, Series: 1, Episode: 0},
			expected: true,
		},
		{
			name:     "series type with episode number",
			release:  &rls.Release{Type: rls.Series, Episode: 5},
			expected: false,
		},
		{
			name:     "episode (series and episode)",
			release:  &rls.Release{Series: 1, Episode: 5},
			expected: false,
		},
		{
			name:     "season 2 pack",
			release:  &rls.Release{Series: 2, Episode: 0},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, isTVSeasonPack(tt.release))
		})
	}
}

func TestRejectSeasonPackFromEpisode(t *testing.T) {
	t.Parallel()

	episode := &rls.Release{Series: 1, Episode: 2}
	seasonPack := &rls.Release{Series: 1, Episode: 0}
	movie := &rls.Release{Title: "Movie", Year: 2024}

	tests := []struct {
		name         string
		newR         *rls.Release
		existingR    *rls.Release
		expectReject bool
		expectReason string
	}{
		{
			name:         "season pack vs episode (forbidden)",
			newR:         seasonPack,
			existingR:    episode,
			expectReject: true,
			expectReason: rejectReasonSeasonPackFromEpisode,
		},
		{
			name:         "episode vs season pack (allowed - reverse direction)",
			newR:         episode,
			existingR:    seasonPack,
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "episode vs episode (allowed)",
			newR:         episode,
			existingR:    &rls.Release{Series: 1, Episode: 3},
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "season pack vs season pack (allowed)",
			newR:         seasonPack,
			existingR:    &rls.Release{Series: 2, Episode: 0},
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "nil new release",
			newR:         nil,
			existingR:    episode,
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "nil existing release",
			newR:         seasonPack,
			existingR:    nil,
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "both nil releases",
			newR:         nil,
			existingR:    nil,
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "movie vs movie (non-TV content)",
			newR:         movie,
			existingR:    &rls.Release{Title: "Other Movie", Year: 2023},
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "movie vs episode (non-TV new)",
			newR:         movie,
			existingR:    episode,
			expectReject: false,
			expectReason: "",
		},
		{
			name:         "season pack vs movie (non-TV existing)",
			newR:         seasonPack,
			existingR:    movie,
			expectReject: false,
			expectReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reject, reason := rejectSeasonPackFromEpisode(tt.newR, tt.existingR)
			require.Equal(t, tt.expectReject, reject, "reject mismatch")
			require.Equal(t, tt.expectReason, reason, "reason mismatch")
		})
	}
}

// TestRejectReasonConstant verifies the package constant has expected value.
func TestRejectReasonConstant(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, rejectReasonSeasonPackFromEpisode)
	require.Contains(t, rejectReasonSeasonPackFromEpisode, "Season packs")
	require.Contains(t, rejectReasonSeasonPackFromEpisode, "single-episode")
}

// A stored verdict for one local episode never lets a downloaded season pack
// through, whatever the individual episodes setting. The listing named the
// episode, so the download check alone passes the pack when individual
// episodes is on.
func TestFindCandidatesRejectsPackDownloadForStoredEpisodeVerdict(t *testing.T) {
	const (
		instanceID  = 1
		sourceHash  = "local-episode"
		episodeName = "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP"
		packName    = "Kaiju.Squad.S02.1080p.WEB.H264-GRP"
		size        = int64(1_000_000)
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: episodeName, Size: size, TotalSize: size, Progress: 1}
	service := manualMatchTestService(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
		sourceHash: {{Name: episodeName + ".mkv", Size: size}},
	})

	find := func(download string, individualEpisodes bool) []CrossSeedCandidate {
		response, err := service.FindCandidates(t.Context(), &FindCandidatesRequest{
			TorrentName:            download,
			TargetInstanceIDs:      []int{instanceID},
			FindIndividualEpisodes: individualEpisodes,
			SearchDecision: searchDecisionProvenance{
				Class:               searchCandidateClassStrict,
				SearchCandidateName: episodeName,
				SourceInstanceID:    instanceID,
				SourceHash:          sourceHash,
			},
		})
		require.NoError(t, err)
		return response.Candidates
	}

	for _, individualEpisodes := range []bool{false, true} {
		require.Len(t, find(episodeName, individualEpisodes), 1, "the episode download: individual episodes %v", individualEpisodes)
		require.Empty(t, find(packName, individualEpisodes), "the pack download: individual episodes %v", individualEpisodes)
	}
}

// Apply has no sizes for a local torrent that search did not compare, so the
// web source relabel does not admit it. The stored relabel verdict of the
// torrent that search compared still holds.
func TestFindCandidatesRelabelNeedsTheStoredVerdict(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "local-webrip"
		localName  = "Kaiju.Squad.S02E05.1080p.AMZN.WEBRip.DD2.0.x264-GRP"
		listing    = "Kaiju.Squad.S02E05.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP"
		size       = int64(1_000_000)
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: sourceHash, Name: localName, Size: size, TotalSize: size, Progress: 1}
	service := manualMatchTestService(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
		sourceHash: {{Name: localName + ".mkv", Size: size}},
	})

	find := func(decision searchDecisionProvenance) []CrossSeedCandidate {
		response, err := service.FindCandidates(t.Context(), &FindCandidatesRequest{
			TorrentName:       listing,
			TargetInstanceIDs: []int{instanceID},
			SearchDecision:    decision,
		})
		require.NoError(t, err)
		return response.Candidates
	}

	require.Empty(t, find(searchDecisionProvenance{}), "a new verdict without sizes")
	require.Len(t, find(searchDecisionProvenance{
		Class:               searchCandidateClassWebSourceRelabel,
		SearchCandidateName: listing,
		SourceInstanceID:    instanceID,
		SourceHash:          sourceHash,
	}), 1, "the stored relabel verdict")
}
