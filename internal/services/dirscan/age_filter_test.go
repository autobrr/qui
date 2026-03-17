// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestSelectEligibleRootWork_TVKeepsOnlyFreshEpisodeItems(t *testing.T) {
	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -10)
	fresh := now.Add(-24 * time.Hour)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Show.Name",
				Path: "/data/tv/Show.Name",
				Files: []*ScannedFile{
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E01.mkv", ModTime: old, Size: 100},
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E02.mkv", ModTime: fresh, Size: 100},
				},
			},
		},
	}

	selection := selectEligibleRootWork(scanResult, nil, NewParser(nil), 3, now, nil)

	require.Equal(t, now.AddDate(0, 0, -3), selection.cutoff)
	require.Equal(t, 2, selection.discoveredFiles)
	require.Equal(t, 1, selection.eligibleFiles)
	require.Equal(t, 1, selection.skippedFiles)
	require.Len(t, selection.roots, 1)
	require.Len(t, selection.roots[0].items, 1)
	require.Equal(t, "Show.Name.S01E02", selection.roots[0].items[0].searchee.Name)
}

func TestSelectEligibleRootWork_IgnoresFreshSubtitleBumps(t *testing.T) {
	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -10)
	fresh := now.Add(-2 * time.Hour)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Movie.2024",
				Path: "/data/movies/Movie.2024",
				Files: []*ScannedFile{
					{Path: "/data/movies/Movie.2024/movie.mkv", ModTime: old, Size: 1000},
					{Path: "/data/movies/Movie.2024/movie.srt", ModTime: fresh, Size: 10},
				},
			},
		},
	}

	selection := selectEligibleRootWork(scanResult, nil, NewParser(nil), 3, now, nil)

	require.Equal(t, 2, selection.discoveredFiles)
	require.Equal(t, 0, selection.eligibleFiles)
	require.Equal(t, 2, selection.skippedFiles)
	require.Empty(t, selection.roots)
}

func TestSelectEligibleRootWork_TreatsAOBAsAudioContent(t *testing.T) {
	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -10)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Album",
				Path: "/data/music/Album",
				Files: []*ScannedFile{
					{Path: "/data/music/Album/AUDIO_TS/ATS_01_1.AOB", ModTime: old, Size: 1000},
				},
			},
		},
	}

	selection := selectEligibleRootWork(scanResult, nil, NewParser(nil), 3, now, nil)

	require.Equal(t, 1, selection.discoveredFiles)
	require.Equal(t, 0, selection.eligibleFiles)
	require.Equal(t, 1, selection.skippedFiles)
	require.Empty(t, selection.roots)
}

func TestWorkItemIsStale_KeepsFreshSeasonPack(t *testing.T) {
	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)

	root := &Searchee{
		Name: "Show.Name",
		Path: "/data/tv/Show.Name",
		Files: []*ScannedFile{
			{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E01.mkv", ModTime: fresh, Size: 100},
			{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E02.mkv", ModTime: fresh, Size: 100},
		},
	}

	items := buildSearcheeWorkItems(root, NewParser(nil))
	// root + 2 episode work items
	require.Len(t, items, 3)

	var seasonItem *searcheeWorkItem
	for i := range items {
		item := &items[i]
		if item.searchee == nil {
			continue
		}
		if item.searchee.Path == root.Path && len(item.searchee.Files) == len(root.Files) {
			seasonItem = item
			break
		}
	}

	require.NotNil(t, seasonItem)
	require.False(t, workItemIsStale(*seasonItem, now.AddDate(0, 0, -3)))
}

func TestMaxSearcheeAgeDaysFromSettings(t *testing.T) {
	require.Equal(t, 0, maxSearcheeAgeDaysFromSettings(nil))
	require.Equal(t, 0, maxSearcheeAgeDaysFromSettings(&models.DirScanSettings{MaxSearcheeAgeDays: 0}))
	require.Equal(t, 0, maxSearcheeAgeDaysFromSettings(&models.DirScanSettings{MaxSearcheeAgeDays: -1}))
	require.Equal(t, 14, maxSearcheeAgeDaysFromSettings(&models.DirScanSettings{MaxSearcheeAgeDays: 14}))
}
