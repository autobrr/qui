// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

func newMatcherService() *Service {
	return &Service{
		releaseCache:     releases.NewDefaultParser(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
}

func TestGetMatchTypeWithReason_SizeContainment(t *testing.T) {
	t.Parallel()

	episodeRelease := rls.Release{Title: "Show", Series: 1, Episode: 1}
	packRelease := rls.Release{Title: "Renamed Pack", Series: 1}
	albumRelease := rls.Release{Title: "Artist Album"}
	albumPackRelease := rls.Release{Title: "Renamed Album Pack"}

	episodeFiles := qbt.TorrentFiles{
		{Name: "Show.S01E01.1080p.mkv", Size: 1 << 30},
	}
	renamedPackFiles := qbt.TorrentFiles{
		{Name: "pack/ep1_renamed.mkv", Size: 1 << 30},
		{Name: "pack/ep2_renamed.mkv", Size: 2 << 30},
	}

	tests := []struct {
		name             string
		sourceRelease    *rls.Release
		candidateRelease *rls.Release
		sourceFiles      qbt.TorrentFiles
		candidateFiles   qbt.TorrentFiles
		want             string
	}{
		{
			name:             "single file pairs by size into renamed pack",
			sourceRelease:    &episodeRelease,
			candidateRelease: &packRelease,
			sourceFiles:      episodeFiles,
			candidateFiles:   renamedPackFiles,
			want:             "size-partial-in-pack",
		},
		{
			name:             "pack contains the candidate file by size",
			sourceRelease:    &packRelease,
			candidateRelease: &episodeRelease,
			sourceFiles:      renamedPackFiles,
			candidateFiles:   episodeFiles,
			want:             "size-partial-contains",
		},
		{
			name:             "no candidate file with a matching size",
			sourceRelease:    &episodeRelease,
			candidateRelease: &packRelease,
			sourceFiles:      qbt.TorrentFiles{{Name: "Show.S01E01.1080p.mkv", Size: 4 << 30}},
			candidateFiles:   renamedPackFiles,
			want:             "",
		},
		{
			name:             "ambiguous same-size candidates reject instead of guessing",
			sourceRelease:    &episodeRelease,
			candidateRelease: &packRelease,
			sourceFiles:      episodeFiles,
			candidateFiles: qbt.TorrentFiles{
				{Name: "pack/first_renamed.mkv", Size: 1 << 30},
				{Name: "pack/second_renamed.mkv", Size: 1 << 30},
			},
			want: "",
		},
		{
			// Track names parse no release keys, so every name-aware tier fails,
			// and the candidate's largest file differs so the largest-file fallback
			// stays out. The shared base name then breaks the same-size tie inside
			// the size-only pairing.
			name:             "same base name breaks the same-size tie",
			sourceRelease:    &albumRelease,
			candidateRelease: &albumPackRelease,
			sourceFiles:      qbt.TorrentFiles{{Name: "intro_song.flac", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "album/intro_song.flac", Size: 1 << 30},
				{Name: "album/other_track.flac", Size: 1 << 30},
				{Name: "album/big_track.flac", Size: 2 << 30},
			},
			want: "size-partial-in-pack",
		},
		{
			name:             "full both-side pairing stays a size match",
			sourceRelease:    &episodeRelease,
			candidateRelease: &packRelease,
			sourceFiles:      episodeFiles,
			candidateFiles:   qbt.TorrentFiles{{Name: "pack/ep1_renamed.mkv", Size: 1 << 30}},
			want:             "size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newMatcherService()
			result := svc.getMatchTypeWithReason(tt.sourceRelease, tt.candidateRelease, tt.sourceFiles, tt.candidateFiles, 5.0)
			if tt.want == "" {
				require.Empty(t, result.MatchType)
				require.NotEmpty(t, result.Reason)
				return
			}
			require.Equal(t, tt.want, result.MatchType)
		})
	}
}

func TestFindBestCandidateMatch_SizeContainmentAgainstRenamedPack(t *testing.T) {
	t.Parallel()

	svc := newMatcherService()
	svc.syncManager = &candidateSelectionSyncManager{
		files: map[string]qbt.TorrentFiles{
			"pack": {
				{Name: "pack/ep1_renamed.mkv", Size: 1 << 30},
				{Name: "pack/ep2_renamed.mkv", Size: 2 << 30},
			},
		},
	}

	sourceRelease := rls.Release{Title: "Show", Series: 1, Episode: 1}
	sourceFiles := qbt.TorrentFiles{{Name: "Show.S01E01.1080p.mkv", Size: 1 << 30}}

	candidate := CrossSeedCandidate{
		Torrents: []qbt.Torrent{
			// Same show and season as the source: the discovery gates only pass
			// same-release packs, so the fixture must model one.
			{Hash: "pack", Name: "Show.S01.1080p.WEB-DL", Progress: 1.0},
		},
	}

	filesByHash := svc.batchLoadCandidateFiles(context.Background(), 1, candidate.Torrents)
	require.NotEmpty(t, filesByHash)

	matched, matchedFiles, matchType, rejectReason := svc.findBestCandidateMatch(
		context.Background(), candidate, &sourceRelease, sourceFiles, filesByHash, 5.0,
	)
	require.NotNil(t, matched, "expected a match, got reject reason: %s", rejectReason)
	require.Len(t, matchedFiles, 2)
	// The matcher runs with swapped sides at apply, so the swapped-back type must
	// read as "the new torrent's files are inside the existing pack".
	require.Equal(t, "size-partial-in-pack", matchType)
}

func TestGetMatchTypeFromTitle_RenamedPackFallback(t *testing.T) {
	t.Parallel()

	episode := rls.Release{Title: "Show", Series: 1, Episode: 2}
	pack := rls.Release{Title: "Show", Series: 1}
	otherSeasonPack := rls.Release{Title: "Show", Series: 3}

	renamedFiles := qbt.TorrentFiles{
		{Name: "pack/first_video.mkv", Size: 1 << 30},
		{Name: "pack/second_video.mkv", Size: 2 << 30},
	}
	wellNamedFiles := qbt.TorrentFiles{
		{Name: "pack/Show.S01E01.mkv", Size: 1 << 30},
		{Name: "pack/Show.S01E03.mkv", Size: 2 << 30},
	}

	svc := newMatcherService()

	got := svc.getMatchTypeFromTitle("Show.S01E02.mkv", "Show.S01.Pack", &episode, &pack, renamedFiles)
	require.Equal(t, "release-match", got, "renamed same-season pack must pass discovery for the file-level matcher")

	got = svc.getMatchTypeFromTitle("Show.S01E02.mkv", "Show.S01.Pack", &episode, &pack, wellNamedFiles)
	require.Empty(t, got, "well-named pack without the target episode must still reject at discovery")

	got = svc.getMatchTypeFromTitle("Show.S01E02.mkv", "Show.S03.Pack", &episode, &otherSeasonPack, renamedFiles)
	require.Empty(t, got, "renamed pack from another season must not pass")
}

func TestMatchTypePriority_SizeContainmentRanksBelowSize(t *testing.T) {
	t.Parallel()

	require.Greater(t, matchTypePriority("size"), matchTypePriority("size-partial-in-pack"))
	require.Greater(t, matchTypePriority("size-partial-in-pack"), matchTypePriority("size-partial-contains"))
	require.Positive(t, matchTypePriority("size-partial-contains"))
	require.Greater(t, matchTypePriority("partial-contains"), matchTypePriority("size"))
}
