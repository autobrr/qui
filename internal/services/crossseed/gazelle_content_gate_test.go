// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestGazellePlausibleContent(t *testing.T) {
	normalizer := stringutils.NewDefaultNormalizer()

	tests := []struct {
		name  string
		files qbt.TorrentFiles
		want  bool
	}{
		{
			name: "movie mkv",
			files: qbt.TorrentFiles{
				{Name: "Some.Movie.2019.1080p.BluRay.x264-GRP/some.movie.2019.mkv", Size: 8_000_000_000},
				{Name: "Some.Movie.2019.1080p.BluRay.x264-GRP/some.movie.2019.nfo", Size: 4_000},
			},
			want: false,
		},
		{
			name: "movie with mp3 soundtrack folder",
			files: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 8_000_000_000},
				{Name: "Movie/Soundtrack/01 - theme.mp3", Size: 9_000_000},
			},
			want: false,
		},
		{
			name: "flac album with badly parsed name",
			files: qbt.TorrentFiles{
				{Name: "weird_name_2019/01.flac", Size: 40_000_000},
				{Name: "weird_name_2019/cover.jpg", Size: 900_000},
			},
			want: true,
		},
		{
			name: "m4b audiobook",
			files: qbt.TorrentFiles{
				{Name: "Author - Book/book.m4b", Size: 300_000_000},
				{Name: "Author - Book/cover.jpg", Size: 100_000},
			},
			want: true,
		},
		{
			name:  "epub book",
			files: qbt.TorrentFiles{{Name: "Author - Book.epub", Size: 2_000_000}},
			want:  true,
		},
		{
			name:  "game iso",
			files: qbt.TorrentFiles{{Name: "Game/game.iso", Size: 50_000_000_000}},
			want:  false,
		},
		{
			name:  "no usable files",
			files: qbt.TorrentFiles{{Name: "release/release.nfo", Size: 4_000}},
			want:  false,
		},
		{
			name:  "empty file list",
			files: qbt.TorrentFiles{},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gazellePlausibleContent(tt.files, normalizer))
		})
	}
}

func newGazelleGateService(t *testing.T, sourceTorrent qbt.Torrent, sourceFiles qbt.TorrentFiles) (*Service, *int) {
	t.Helper()

	callCount := 0
	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(_ context.Context, _ *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		callCount++
		return nil, nil
	}
	t.Cleanup(func() {
		findGazelleMatch = prevFindMatch
	})

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager: &gazelleSkipHashSyncManager{
			torrents: []qbt.Torrent{sourceTorrent},
			filesByHash: map[string]qbt.TorrentFiles{
				normalizeHash(sourceTorrent.Hash): sourceFiles,
			},
		},
	}
	return svc, &callCount
}

func TestSearchGazelleMatches_NonGazelleVideoSourceSkipsRemoteLookup(t *testing.T) {
	sourceTorrent := qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "Some.Movie.2019.1080p.BluRay.x264-GRP",
		Progress: 1.0,
		Size:     8_000_000_000,
		Tracker:  "https://tracker.example.org/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Some.Movie.2019.1080p.BluRay.x264-GRP/some.movie.2019.mkv", Size: 8_000_000_000},
	}

	svc, callCount := newGazelleGateService(t, sourceTorrent, sourceFiles)
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	results, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(context.Background(), 1, &sourceTorrent, sourceFiles, "", false, clients)
	require.True(t, gazelleConfigured, "gazelle stays configured so gazelle-only runs do not error")
	require.False(t, lookupAttempted)
	require.Empty(t, results)
	require.Equal(t, 0, *callCount, "video torrent from non-Gazelle source must not hit RED/OPS")
}

func TestSearchGazelleMatches_NonGazelleMusicSourceStillSearches(t *testing.T) {
	sourceTorrent := qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "weird_name_2019",
		Progress: 1.0,
		Size:     40_000_000,
		Tracker:  "https://tracker.example.org/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "weird_name_2019/01.flac", Size: 40_000_000},
	}

	svc, callCount := newGazelleGateService(t, sourceTorrent, sourceFiles)
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	_, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(context.Background(), 1, &sourceTorrent, sourceFiles, "", false, clients)
	require.True(t, gazelleConfigured)
	require.True(t, lookupAttempted)
	require.Equal(t, 1, *callCount)
}

func TestSearchGazelleMatches_GazelleSourceBypassesContentGate(t *testing.T) {
	// E-learning videos exist on RED; a RED-sourced torrent must be searched on
	// the sibling site regardless of file extensions.
	sourceTorrent := qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "Some Course (2019)",
		Progress: 1.0,
		Size:     2_000_000_000,
		Tracker:  "https://flacsfor.me/abc/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Some Course (2019)/lesson01.mp4", Size: 2_000_000_000},
	}

	svc, callCount := newGazelleGateService(t, sourceTorrent, sourceFiles)
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	_, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(context.Background(), 1, &sourceTorrent, sourceFiles, "redacted.sh", true, clients)
	require.True(t, gazelleConfigured)
	require.True(t, lookupAttempted)
	require.Equal(t, 1, *callCount)
}
