// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
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
