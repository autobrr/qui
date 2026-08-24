// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestIsFinalTrackedFile(t *testing.T) {
	t.Parallel()

	enabled := func(ids ...int) map[int]struct{} {
		set := make(map[int]struct{}, len(ids))
		for _, id := range ids {
			set[id] = struct{}{}
		}
		return set
	}

	tests := []struct {
		name    string
		tracked *models.DirScanFile
		enabled map[int]struct{}
		want    bool
	}{
		{
			name:    "nil tracked file is not final",
			tracked: nil,
			enabled: enabled(1),
			want:    false,
		},
		{
			name:    "pending is not final",
			tracked: &models.DirScanFile{Status: models.DirScanFileStatusPending},
			enabled: enabled(1),
			want:    false,
		},
		{
			name:    "error is not final",
			tracked: &models.DirScanFile{Status: models.DirScanFileStatusError},
			enabled: enabled(1),
			want:    false,
		},
		{
			name:    "matched is final regardless of indexer set",
			tracked: &models.DirScanFile{Status: models.DirScanFileStatusMatched},
			enabled: enabled(1, 2, 3),
			want:    true,
		},
		{
			name:    "legacy no_match without a search set stays final",
			tracked: &models.DirScanFile{Status: models.DirScanFileStatusNoMatch},
			enabled: enabled(1, 2),
			want:    true,
		},
		{
			name: "no_match stays final while the enabled set is unchanged",
			tracked: &models.DirScanFile{
				Status:             models.DirScanFileStatusNoMatch,
				SearchedIndexerIDs: []int{1, 2},
			},
			enabled: enabled(1, 2),
			want:    true,
		},
		{
			name: "no_match reopens when a new indexer appears",
			tracked: &models.DirScanFile{
				Status:             models.DirScanFileStatusNoMatch,
				SearchedIndexerIDs: []int{1, 2},
			},
			enabled: enabled(1, 2, 3),
			want:    false,
		},
		{
			name: "no_match stays final when an indexer was removed",
			tracked: &models.DirScanFile{
				Status:             models.DirScanFileStatusNoMatch,
				SearchedIndexerIDs: []int{1, 2, 3},
			},
			enabled: enabled(1, 2),
			want:    true,
		},
		{
			name: "no_match stays final when the enabled set is unknown",
			tracked: &models.DirScanFile{
				Status:             models.DirScanFileStatusNoMatch,
				SearchedIndexerIDs: []int{1},
			},
			enabled: nil,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isFinalTrackedFile(tt.tracked, tt.enabled))
		})
	}
}

func TestWorkItemHasPendingFiles_ReopensNoMatchForNewIndexer(t *testing.T) {
	t.Parallel()

	searchee := &Searchee{
		Name: "Example.Movie.2024",
		Path: "/data/movies/Example.Movie.2024",
		Files: []*ScannedFile{
			{Path: "/data/movies/Example.Movie.2024/movie.mkv", Size: 100},
		},
	}
	item := searcheeWorkItem{searchee: searchee}
	trackedFiles := &trackedFilesIndex{
		byPath: map[string]*models.DirScanFile{
			"/data/movies/Example.Movie.2024/movie.mkv": {
				FilePath:           "/data/movies/Example.Movie.2024/movie.mkv",
				Status:             models.DirScanFileStatusNoMatch,
				SearchedIndexerIDs: []int{1},
			},
		},
		byFileID: map[string]*models.DirScanFile{},
	}

	require.False(t, workItemHasPendingFiles(item, trackedFiles, map[int]struct{}{1: {}}))
	require.True(t, workItemHasPendingFiles(item, trackedFiles, map[int]struct{}{1: {}, 2: {}}))
}
