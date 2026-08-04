// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildTrackedFileUpsert_SearchedIndexerIDs(t *testing.T) {
	t.Parallel()

	l := zerolog.New(io.Discard)
	modTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	existing := &models.DirScanFile{
		FilePath:           "/data/movies/movie.mkv",
		FileSize:           100,
		FileModTime:        modTime,
		Status:             models.DirScanFileStatusNoMatch,
		SearchedIndexerIDs: []int{1, 2},
	}
	idx := &trackedFilesIndex{
		byPath:   map[string]*models.DirScanFile{existing.FilePath: existing},
		byFileID: map[string]*models.DirScanFile{},
	}

	t.Run("unchanged file carries the search set forward", func(t *testing.T) {
		scanned := &ScannedFile{Path: existing.FilePath, Size: 100, ModTime: modTime}
		fileModel, err := buildTrackedFileUpsert(1, scanned, idx, false, &l)
		require.NoError(t, err)
		require.NotNil(t, fileModel)
		require.Equal(t, models.DirScanFileStatusNoMatch, fileModel.Status)
		require.Equal(t, []int{1, 2}, fileModel.SearchedIndexerIDs)
	})

	t.Run("changed file resets to pending and clears the search set", func(t *testing.T) {
		scanned := &ScannedFile{Path: existing.FilePath, Size: 200, ModTime: modTime}
		fileModel, err := buildTrackedFileUpsert(1, scanned, idx, false, &l)
		require.NoError(t, err)
		require.NotNil(t, fileModel)
		require.Equal(t, models.DirScanFileStatusPending, fileModel.Status)
		require.Nil(t, fileModel.SearchedIndexerIDs)
	})
}
