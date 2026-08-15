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
			name:   "infer seasonless anime pack when files parse to same episode",
			source: "[SubsPlease] Classic Stars (1080p)",
			files: qbt.TorrentFiles{
				{Name: "[SubsPlease] Classic Stars - 11 (1080p) [11111111].mkv", Size: 1},
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

func TestSelectSourceReleaseForSearch_UsesTVDetectionReleaseForTVCategories(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := svc.releaseCache.Parse("[SubsPlease] Some, Anime (2025) [720p]")
	require.NotNil(t, source)
	require.Equal(t, rls.Movie, source.Type)

	files := qbt.TorrentFiles{
		{Name: "[SubsPlease] Some, Anime (2025) [720p]/[SubsPlease] Some, Anime - 37 (720p) [11111111].mkv", Size: 1},
	}
	contentDetectionRelease, _ := svc.selectContentDetectionRelease("[SubsPlease] Some, Anime (2025) [720p]", source, files)
	contentInfo := DetermineContentType(contentDetectionRelease)
	require.Equal(t, "tv", contentInfo.ContentType)

	searchRelease := svc.selectSourceReleaseForSearch(source, contentDetectionRelease, files, contentInfo)
	require.Equal(t, rls.Episode, searchRelease.Type)
	require.Equal(t, 37, searchRelease.Episode)

	query := buildSafeSearchQuery("[SubsPlease] Some, Anime (2025) [720p]", searchRelease, searchRelease.Title)
	require.Equal(t, "Some, Anime", query.Query)
	require.NotNil(t, query.Episode)
	require.Equal(t, 37, *query.Episode)
}

func TestSelectContentDetectionRelease_MusicNameKeepsEpisodeMarkersFromFiles(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	sourceName := "Space Badgers From Pluto (2011)"
	source := svc.releaseCache.Parse(sourceName)
	require.NotNil(t, source)
	require.Equal(t, "music", DetermineContentType(source).ContentType,
		"fixture only exercises the fix while the torrent name classifies as music")

	files := qbt.TorrentFiles{
		{Name: sourceName + "/Space Badgers from Pluto (2011) - 101 - The Journey Starts [abc123].avi", Size: 1},
	}

	contentDetectionRelease, usedFile := svc.selectContentDetectionRelease(sourceName, source, files)
	require.True(t, usedFile, "episode markers in the files must win over a music name parse")
	require.Equal(t, "tv", DetermineContentTypeWithFiles(contentDetectionRelease, files).ContentType)
}

// Regression: parsing the full torrent-relative path invented a group and hard-rejected
// byte-identical candidates with "group/site mismatch". See selectContentDetectionRelease.
func TestSelectContentDetectionRelease_RepeatedFolderNameKeepsGroup(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	const sourceName = "[FanSubs] Azure Compass - 1168 (1080p) [0A043BA1]"
	const sourceSize = 1414363469
	files := qbt.TorrentFiles{{Name: sourceName + "/" + sourceName + ".mkv", Size: sourceSize}}

	source := svc.releaseCache.Parse(sourceName)
	contentDetectionRelease, usedFile := svc.selectContentDetectionRelease(sourceName, source, files)
	require.True(t, usedFile, "must use the file: a sourceRelease fallback also has an empty group")
	require.Empty(t, contentDetectionRelease.Group, "repeated folder name must not invent a group")

	contentInfo := DetermineContentTypeWithFiles(contentDetectionRelease, files)
	searchRelease := svc.selectSourceReleaseForSearch(source, contentDetectionRelease, files, contentInfo)

	const candidateName = sourceName + ".mkv"
	ok, reason := svc.validateExactSizeSearchIdentity(searchCandidateInput{
		SourceRelease:    searchRelease,
		CandidateRelease: svc.releaseCache.Parse(candidateName),
		SourceName:       sourceName,
		CandidateName:    candidateName,
		SourceSize:       sourceSize,
		CandidateSize:    sourceSize,
	})
	require.True(t, ok, "byte-identical candidate rejected: %s", reason)
}

func TestSelectSourceReleaseForSearch_SeasonPackKeepsTorrentIdentity(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	sourceName := "Silver.Gear.Labyrinth.S02.720p.CR.WEB-DL.AAC2.0.H.264-ALPHA"
	source := svc.releaseCache.Parse(sourceName)
	require.NotNil(t, source)

	// File names must share the torrent title or content detection discards them as unrelated.
	files := qbt.TorrentFiles{
		{
			Name: "Silver.Gear.Labyrinth.S02.720p.CR.WEB-DL.AAC2.0.H.264-ALPHA/" +
				"[BETA] Silver Gear Labyrinth S2 - 01 (720p) [11111111].mkv",
			Size: 1,
		},
		{
			Name: "Silver.Gear.Labyrinth.S02.720p.CR.WEB-DL.AAC2.0.H.264-ALPHA/" +
				"[BETA] Silver Gear Labyrinth S2 - 02 (720p) [22222222].mkv",
			Size: 2,
		},
	}

	contentDetectionRelease, _ := svc.selectContentDetectionRelease(sourceName, source, files)
	contentInfo := DetermineContentType(contentDetectionRelease)
	require.Equal(t, "tv", contentInfo.ContentType)

	searchRelease := svc.selectSourceReleaseForSearch(source, contentDetectionRelease, files, contentInfo)
	require.Equal(t, rls.Series, searchRelease.Type)
	require.Equal(t, 2, searchRelease.Series)
	require.Equal(t, 0, searchRelease.Episode)
	require.Equal(t, source.Title, searchRelease.Title)
	require.Equal(t, source.Group, searchRelease.Group)
	require.Equal(t, source.Site, searchRelease.Site)
	require.Equal(t, source.Sum, searchRelease.Sum)
	require.NotEqual(t, contentDetectionRelease.Site, searchRelease.Site)
	require.NotEqual(t, contentDetectionRelease.Sum, searchRelease.Sum)

	candidate := svc.releaseCache.Parse("Silver.Gear.Labyrinth.S02.720p.CR.WEB-DL.AAC2.0.H.264-ALPHA")
	match, reason := svc.releasesMatchWithReason(searchRelease, candidate, false)
	require.True(t, match, "season pack candidate should not be rejected by file-level identity, got %q", reason)
}
