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
		{
			// The cross-seed ignore keywords match on the whole path, so a
			// release directory named "[Bonus Tracks]" hides every file below it.
			name: "flac album under a bonus tracks directory",
			files: qbt.TorrentFiles{
				{Name: "Artist - Album (Deluxe) [Bonus Tracks]/01 - Track.flac", Size: 40_000_000},
				{Name: "Artist - Album (Deluxe) [Bonus Tracks]/cover.jpg", Size: 900_000},
			},
			want: true,
		},
		{
			// Booklet scans outweigh individual tracks in many lossy releases.
			name: "mp3 album with booklet scans larger than any track",
			files: qbt.TorrentFiles{
				{Name: "Artist - Album [MP3 V0]/01.mp3", Size: 9_000_000},
				{Name: "Artist - Album [MP3 V0]/02.mp3", Size: 8_000_000},
				{Name: "Artist - Album [MP3 V0]/03.mp3", Size: 9_000_000},
				{Name: "Artist - Album [MP3 V0]/04.mp3", Size: 8_000_000},
				{Name: "Artist - Album [MP3 V0]/Scans/booklet-01.tif", Size: 24_000_000},
				{Name: "Artist - Album [MP3 V0]/Scans/booklet-02.tif", Size: 8_000_000},
			},
			want: true,
		},
		{
			name: "concert video with a separate flac audio track",
			files: qbt.TorrentFiles{
				{Name: "Live/show.mkv", Size: 20_000_000_000},
				{Name: "Live/audio.flac", Size: 800_000_000},
			},
			want: false,
		},
		{
			name:  "uppercase extension",
			files: qbt.TorrentFiles{{Name: "Artist - Album/01 - Track.FLAC", Size: 40_000_000}},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gazellePlausibleContent(tt.files))
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
	require.True(t, gazelleConfigured, "Gazelle stays configured, so Gazelle-only runs do not fail")
	require.False(t, lookupAttempted)
	require.Empty(t, results)
	require.Equal(t, 0, *callCount, "qui must not query RED/OPS for a video torrent from a source that is not Gazelle")
}

func TestSearchGazelleMatches_BonusDirectoryStillSearches(t *testing.T) {
	// The cross-seed ignore keywords match on the whole path. A release
	// directory named "[Bonus Tracks]" must not hide the album from Gazelle.
	sourceTorrent := qbt.Torrent{
		Hash:     "223759985c562a644428312c8cd3585d04686847",
		Name:     "Artist - Album (Deluxe) [Bonus Tracks]",
		Progress: 1.0,
		Size:     40_000_000,
		Tracker:  "https://tracker.example.org/announce",
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Artist - Album (Deluxe) [Bonus Tracks]/01 - Track.flac", Size: 40_000_000},
	}

	svc, callCount := newGazelleGateService(t, sourceTorrent, sourceFiles)
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	_, gazelleConfigured, lookupAttempted := svc.searchGazelleMatches(context.Background(), 1, &sourceTorrent, sourceFiles, "", false, clients)
	require.True(t, gazelleConfigured)
	require.True(t, lookupAttempted)
	require.Equal(t, 1, *callCount)
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
	// E-learning videos exist on RED. Therefore qui must search the sibling site
	// for a torrent from RED, and the file extensions do not matter.
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
