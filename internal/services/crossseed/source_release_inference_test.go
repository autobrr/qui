// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestDeriveSourceReleaseForSearch(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	tests := []struct {
		name            string
		source          string
		files           qbt.TorrentFiles
		expectedType    rls.Type
		expectedSeries  int
		expectedEpisode int
	}{
		{
			name:   "infer season pack from files",
			source: "Frieren Beyond Journey's End (BD Remux 1080p AVC FLAC AAC) [Dual Audio] [PMR]",
			files: qbt.TorrentFiles{
				{Name: "Frieren Beyond Journey's End - S01E01 (BD Remux 1080p AVC FLAC AAC) [Dual Audio] [PMR].mkv", Size: 1},
				{Name: "Frieren Beyond Journey's End - S01E02 (BD Remux 1080p AVC FLAC AAC) [Dual Audio] [PMR].mkv", Size: 1},
				{Name: "Frieren Beyond Journey's End - S01E01.nfo", Size: 1},
			},
			expectedType:    rls.Series,
			expectedSeries:  1,
			expectedEpisode: 0,
		},
		{
			name:   "infer single episode from files",
			source: "Some Anime Title (WEB 1080p) [Group]",
			files: qbt.TorrentFiles{
				{Name: "Some Anime Title - S01E03 (WEB 1080p) [Group].mkv", Size: 1},
			},
			expectedType:    rls.Episode,
			expectedSeries:  1,
			expectedEpisode: 3,
		},
		{
			name:   "file structure overrides episode for packs",
			source: "Some.Show.S01E01.1080p.WEB-DL.x264-GROUP",
			files: qbt.TorrentFiles{
				{Name: "Some Show - S01E01 (1080p WEB-DL x264) [GROUP].mkv", Size: 1},
				{Name: "Some Show - S01E02 (1080p WEB-DL x264) [GROUP].mkv", Size: 1},
			},
			expectedType:    rls.Series,
			expectedSeries:  1,
			expectedEpisode: 0,
		},
		{
			name:   "infer seasonless anime pack",
			source: "[SubsPlease] Classic Stars (1080p)",
			files: qbt.TorrentFiles{
				{Name: "[SubsPlease] Classic Stars - 01 (1080p) [11111111].mkv", Size: 1},
				{Name: "[SubsPlease] Classic Stars - 11 (1080p) [22222222].mkv", Size: 1},
			},
			expectedType:    rls.Series,
			expectedSeries:  0,
			expectedEpisode: 0,
		},
		{
			name:   "infer seasonless anime episode",
			source: "[SubsPlease] Classic Stars (1080p)",
			files: qbt.TorrentFiles{
				{Name: "[SubsPlease] Classic Stars - 11 (1080p) [22222222].mkv", Size: 1},
			},
			expectedType:    rls.Episode,
			expectedSeries:  0,
			expectedEpisode: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := svc.releaseCache.Parse(tt.source)
			require.NotNil(t, source)

			derived := svc.deriveSourceReleaseForSearch(source, tt.files)
			require.Equal(t, tt.expectedType, derived.Type)
			require.Equal(t, tt.expectedSeries, derived.Series)
			require.Equal(t, tt.expectedEpisode, derived.Episode)
		})
	}
}

func TestDeriveSourceReleaseForSearch_DoesNotInferSeasonlessTVFromNumberedMovieFiles(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("Some Movie 2024 1080p BluRay x264-GROUP")
	require.NotNil(t, source)
	require.Equal(t, rls.Movie, source.Type)
	require.Equal(t, 2024, source.Year)
	require.Equal(t, 0, source.Series)
	require.Equal(t, 0, source.Episode)

	tests := []struct {
		name  string
		files qbt.TorrentFiles
	}{
		{
			name: "multiple numbered files are not a seasonless pack",
			files: qbt.TorrentFiles{
				{Name: "Some Movie 2024 - 01 1080p BluRay x264-GROUP.mkv", Size: 1},
				{Name: "Some Movie 2024 - 02 1080p BluRay x264-GROUP.mkv", Size: 1},
			},
		},
		{
			name: "single numbered file is not a seasonless episode",
			files: qbt.TorrentFiles{
				{Name: "Some Movie 2024 - 01 1080p BluRay x264-GROUP.mkv", Size: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			derived := svc.deriveSourceReleaseForSearch(source, tt.files)
			require.Equal(t, rls.Movie, derived.Type)
			require.Equal(t, 0, derived.Series)
			require.Equal(t, 0, derived.Episode)
		})
	}
}
