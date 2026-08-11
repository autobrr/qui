// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestSelectEligibleRootWork_SkipIndividualEpisodesKeepsSeasonPack(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Show.Name",
				Path: "/data/tv/Show.Name",
				Files: []*ScannedFile{
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E01.mkv", ModTime: fresh, Size: 100},
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E02.mkv", ModTime: fresh, Size: 100},
				},
			},
		},
	}

	kept := selectEligibleRootWork(scanResult, nil, NewParser(nil), 0, now, nil, false, nil)
	require.Len(t, kept.roots, 1)
	require.Len(t, kept.roots[0].items, 3)

	skipped := selectEligibleRootWork(scanResult, nil, NewParser(nil), 0, now, nil, true, nil)
	require.Len(t, skipped.roots, 1)
	require.Len(t, skipped.roots[0].items, 1)
	require.Equal(t, "Show Name S01", skipped.roots[0].items[0].searchee.Name)
	require.False(t, skipped.roots[0].items[0].isEpisode)
	require.Equal(t, 2, skipped.skippedEpisodes)
	require.Equal(t, 0, kept.skippedEpisodes)
}

func TestSelectEligibleRootWork_SkipIndividualEpisodesDropsUngroupableEpisode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)

	// One episode cannot make a season pack search, so the root has no work
	// left once individual episodes are off.
	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Show.Name",
				Path: "/data/tv/Show.Name",
				Files: []*ScannedFile{
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E01.mkv", ModTime: fresh, Size: 100},
				},
			},
		},
	}

	kept := selectEligibleRootWork(scanResult, nil, NewParser(nil), 0, now, nil, false, nil)
	require.Len(t, kept.roots, 1)

	skipped := selectEligibleRootWork(scanResult, nil, NewParser(nil), 0, now, nil, true, nil)
	require.Empty(t, skipped.roots)
	require.Equal(t, 1, skipped.discoveredFiles)
	require.Equal(t, 0, skipped.eligibleFiles)
	require.Equal(t, 1, skipped.skippedFiles)
}

func TestSelectEligibleRootWork_SkipIndividualEpisodesLeavesNonTVAlone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)

	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Movie.2024",
				Path: "/data/movies/Movie.2024",
				Files: []*ScannedFile{
					{Path: "/data/movies/Movie.2024/movie.mkv", ModTime: fresh, Size: 1000},
				},
			},
		},
	}

	skipped := selectEligibleRootWork(scanResult, nil, NewParser(nil), 0, now, nil, true, nil)
	require.Len(t, skipped.roots, 1)
	require.Len(t, skipped.roots[0].items, 1)
	require.Equal(t, "Movie.2024", skipped.roots[0].items[0].searchee.Name)
	require.Equal(t, 0, skipped.skippedEpisodes)
}

func TestSelectEligibleRootWork_SkipKeepsMixedAgePackForFreshEpisode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -30)
	fresh := now.Add(-2 * time.Hour)

	// An airing season: two episodes past the age cutoff, one grabbed today.
	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Show.Name",
				Path: "/data/tv/Show.Name",
				Files: []*ScannedFile{
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E01.mkv", ModTime: old, Size: 100},
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E02.mkv", ModTime: old, Size: 100},
					{Path: "/data/tv/Show.Name/Season 01/Show.Name.S01E03.mkv", ModTime: fresh, Size: 100},
				},
			},
		},
	}

	// Skip off: the mixed-age pack goes stale and the fresh episode carries the work.
	kept := selectEligibleRootWork(scanResult, nil, NewParser(nil), 3, now, nil, false, nil)
	require.Len(t, kept.roots, 1)
	require.Len(t, kept.roots[0].items, 1)
	require.True(t, kept.roots[0].items[0].isEpisode)

	// Skip on: the pack is the only search the fresh episode has, so it stays eligible.
	skipped := selectEligibleRootWork(scanResult, nil, NewParser(nil), 3, now, nil, true, nil)
	require.Len(t, skipped.roots, 1)
	require.Len(t, skipped.roots[0].items, 1)
	require.Equal(t, "Show Name S01", skipped.roots[0].items[0].searchee.Name)
	require.False(t, skipped.roots[0].items[0].isEpisode)
	// Only the fresh episode counts as avoided work; the stale ones drop as stale.
	require.Equal(t, 1, skipped.skippedEpisodes)
}

func TestSelectEligibleRootWork_SkipCountsOnlyPendingEpisodes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	fresh := now.Add(-24 * time.Hour)

	paths := []string{
		"/data/tv/Show.Name/Season 01/Show.Name.S01E01.mkv",
		"/data/tv/Show.Name/Season 01/Show.Name.S01E02.mkv",
	}
	scanResult := &ScanResult{
		Searchees: []*Searchee{
			{
				Name: "Show.Name",
				Path: "/data/tv/Show.Name",
				Files: []*ScannedFile{
					{Path: paths[0], ModTime: fresh, Size: 100},
					{Path: paths[1], ModTime: fresh, Size: 100},
				},
			},
		},
	}

	// Every file already searched everywhere: the episodes are finished work,
	// not avoided work, so they keep the all_final reason and the counter stays 0.
	trackedFiles := &trackedFilesIndex{
		byPath: map[string]*models.DirScanFile{
			paths[0]: {FilePath: paths[0], Status: models.DirScanFileStatusNoMatch, SearchedIndexerIDs: []int{1}},
			paths[1]: {FilePath: paths[1], Status: models.DirScanFileStatusNoMatch, SearchedIndexerIDs: []int{1}},
		},
		byFileID: map[string]*models.DirScanFile{},
	}
	enabled := map[int]struct{}{1: {}}

	skipped := selectEligibleRootWork(scanResult, trackedFiles, NewParser(nil), 0, now, enabled, true, nil)
	require.Empty(t, skipped.roots)
	require.Equal(t, 0, skipped.skippedEpisodes)
}
