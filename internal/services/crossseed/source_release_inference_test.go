// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
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

func TestDeriveSearchSourceRelease_ReplaysCategoryMappedView(t *testing.T) {
	const instanceID = 1
	torrent := qbt.Torrent{
		Hash:     "category-mapped-source",
		Name:     "[Orbit] Azure Compass (2025) [720p]",
		Category: "forced-movie",
	}
	files := qbt.TorrentFiles{
		{Name: "[Orbit] Azure Compass - S01E03 (720p) [11111111].mkv", Size: 1},
	}
	instance := &models.Instance{ID: instanceID, Name: "main"}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				CategoryMappingRules: []models.CategoryMappingRule{
					{Categories: []string{torrent.Category}, ContentType: "movie"},
				},
			}, nil
		},
	}
	svc.syncManager = newFakeSyncManager(instance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{
		torrent.Hash: files,
	})

	parsed := svc.releaseCache.Parse(torrent.Name)
	searchView, contentInfo := svc.searchSourceReleaseViewAndContentInfo(context.Background(), &torrent, parsed, files)
	replayedView := svc.deriveSearchSourceRelease(context.Background(), instanceID, &torrent, parsed)

	require.Equal(t, "movie", contentInfo.ContentType)
	require.Equal(t, rls.Movie, searchView.release.Type,
		"the category rule must keep TV-looking files on the movie search path")
	require.Equal(t, searchView.release, replayedView.release)
	require.Equal(t, searchView.tagOrigin, replayedView.tagOrigin)
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

func TestSelectContentDetectionRelease_DiscLayout(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	const sourceName = "Space Badgers From Pluto (2011)"
	tests := []struct {
		name string
		file string
	}{
		{
			name: "BDMV",
			file: sourceName + "/BDMV/STREAM/00000.m2ts",
		},
		{
			name: "VIDEO_TS",
			file: sourceName + "/VIDEO_TS/VTS_01_1.VOB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := svc.releaseCache.Parse(sourceName)
			require.Equal(t, rls.Music, source.Type,
				"fixture only exercises the fix while the torrent name classifies as music")

			files := qbt.TorrentFiles{
				{Name: tt.file, Size: 1},
				{Name: sourceName + "/extras/misleading.mp3", Size: 10_000},
			}
			contentDetectionRelease, usedFile := svc.selectContentDetectionRelease(sourceName, source, files)

			require.False(t, usedFile, "disc layout must bypass largest-file parsing")
			require.Equal(t, "movie", DetermineContentTypeWithFiles(contentDetectionRelease, files).ContentType,
				"dominant audio must not override the authoritative disc classification")
			require.NotSame(t, source, contentDetectionRelease, "disc classification must not mutate the cached parse")
			require.Equal(t, rls.Music, source.Type, "disc classification must not mutate the cached parse")
		})
	}

	t.Run("keeps explicit TV structure", func(t *testing.T) {
		source := &rls.Release{Type: rls.Music, Title: "Signal Voyagers", Series: 2}
		files := qbt.TorrentFiles{
			{Name: "Signal Voyagers S02/VIDEO_TS/VTS_01_1.VOB", Size: 1},
			{Name: "Signal Voyagers S02/extras/misleading.mp3", Size: 10_000},
		}
		require.Equal(t, "tv", DetermineContentTypeWithFiles(source, files).ContentType,
			"disc classification must normalize explicit TV metadata without relying on the selector")

		contentDetectionRelease, usedFile := svc.selectContentDetectionRelease(source.Title, source, files)

		require.False(t, usedFile, "disc layout must bypass largest-file parsing")
		require.NotSame(t, source, contentDetectionRelease, "disc classification must not mutate the cached parse")
		require.Equal(t, rls.Music, source.Type, "disc classification must not mutate the cached parse")
		require.Equal(t, "tv", DetermineContentTypeWithFiles(contentDetectionRelease, files).ContentType,
			"dominant audio must not override explicit TV structure")
	})
}

func TestDiscLayoutDominantAudioKeepsTVSearchShape(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	const sourceName = "Signal.Voyagers.S02E03.DVDR"
	source := &rls.Release{Type: rls.Episode, Title: "Signal Voyagers", Series: 2, Episode: 3}
	files := qbt.TorrentFiles{
		{Name: sourceName + "/VIDEO_TS/VTS_01_1.VOB", Size: 1},
		{Name: sourceName + "/extras/misleading.mp3", Size: 10_000},
	}

	contentDetectionRelease, usedFile := svc.selectContentDetectionRelease(sourceName, source, files)
	require.False(t, usedFile)

	contentInfo := DetermineContentTypeWithFiles(contentDetectionRelease, files)
	require.Equal(t, "tv", contentInfo.ContentType)
	searchRelease := svc.selectSourceReleaseForSearch(source, contentDetectionRelease, files, contentInfo)
	query := BuildTorznabQuery(sourceName, searchRelease, contentInfo.IsMusic)
	require.NotNil(t, query.Episode)
	require.Equal(t, 3, *query.Episode)
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
		Source:        namedRelease{release: searchRelease, rawName: sourceName},
		Candidate:     namedRelease{release: svc.releaseCache.Parse(candidateName), rawName: candidateName},
		SourceSize:    sourceSize,
		CandidateSize: sourceSize,
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
