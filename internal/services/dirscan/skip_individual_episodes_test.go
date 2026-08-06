// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
}
