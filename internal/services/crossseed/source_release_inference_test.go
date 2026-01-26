// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestDeriveSourceReleaseForSearch_InferSeasonPackFromFiles(t *testing.T) {
	svc := &Service{
		releaseCache:                  NewReleaseCache(),
		stringNormalizer:              stringutils.NewDefaultNormalizer(),
		recoverErroredTorrentsEnabled: false,
	}

	source := svc.releaseCache.Parse("Frieren Beyond Journey's End (BD Remux 1080p AVC FLAC AAC) [Dual Audio] [PMR]")
	require.NotNil(t, source)
	require.Equal(t, 0, source.Series)
	require.Equal(t, 0, source.Episode)

	files := qbt.TorrentFiles{
		{Name: "Frieren Beyond Journey's End - S01E01 (BD Remux 1080p AVC FLAC AAC) [Dual Audio] [PMR].mkv", Size: 1},
		{Name: "Frieren Beyond Journey's End - S01E02 (BD Remux 1080p AVC FLAC AAC) [Dual Audio] [PMR].mkv", Size: 1},
		{Name: "Frieren Beyond Journey's End - S01E01.nfo", Size: 1},
	}

	derived := svc.deriveSourceReleaseForSearch(source, files)
	require.Equal(t, 1, derived.Series)
	require.Equal(t, 0, derived.Episode)
}

func TestDeriveSourceReleaseForSearch_InferSingleEpisodeFromFiles(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("Some Anime Title (WEB 1080p) [Group]")
	require.NotNil(t, source)
	require.Equal(t, 0, source.Series)
	require.Equal(t, 0, source.Episode)

	files := qbt.TorrentFiles{
		{Name: "Some Anime Title - S01E03 (WEB 1080p) [Group].mkv", Size: 1},
	}

	derived := svc.deriveSourceReleaseForSearch(source, files)
	require.Equal(t, 1, derived.Series)
	require.Equal(t, 3, derived.Episode)
}

func TestDeriveSourceReleaseForSearch_FileStructureOverridesEpisodeForPacks(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("Some.Show.S01E01.1080p.WEB-DL.x264-GROUP")
	require.NotNil(t, source)
	require.Equal(t, 1, source.Series)
	require.Equal(t, 1, source.Episode)

	files := qbt.TorrentFiles{
		{Name: "Some Show - S01E01 (1080p WEB-DL x264) [GROUP].mkv", Size: 1},
		{Name: "Some Show - S01E02 (1080p WEB-DL x264) [GROUP].mkv", Size: 1},
	}

	derived := svc.deriveSourceReleaseForSearch(source, files)
	require.Equal(t, 1, derived.Series)
	require.Equal(t, 0, derived.Episode)
}
