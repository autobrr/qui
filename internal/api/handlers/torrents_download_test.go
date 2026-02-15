// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilePathCandidates_ContentPathSingleFileFallback(t *testing.T) {
	t.Parallel()

	savePath := "/downloads/tv"
	relativePath := "Show.S01E01.mkv"
	contentPath := "/downloads/tv/Show.S01E01/Show.S01E01.mkv"

	candidates := filePathCandidates(savePath, "", contentPath, relativePath, true)

	require.Contains(t, candidates, filepath.Clean(filepath.Join(savePath, relativePath)))
	require.Contains(t, candidates, filepath.Clean(contentPath))
	require.Contains(t, candidates, filepath.Clean(filepath.Join(filepath.Dir(contentPath), relativePath)))
}

func TestFilePathCandidates_ContentPathMultiFileFallback(t *testing.T) {
	t.Parallel()

	savePath := "/downloads"
	relativePath := "Show.S01/Show.S01E01.mkv"
	contentPath := "/downloads/Show.S01"

	candidates := filePathCandidates(savePath, "", contentPath, relativePath, false)

	require.Contains(t, candidates, filepath.Clean(filepath.Join(savePath, relativePath)))
	require.Contains(t, candidates, filepath.Clean(filepath.Join(contentPath, relativePath)))
}

func TestFilePathCandidates_DeduplicatesEquivalentPaths(t *testing.T) {
	t.Parallel()

	savePath := "/downloads"
	relativePath := "Movie.mkv"
	contentPath := "/downloads/Movie.mkv"

	candidates := filePathCandidates(savePath, "", contentPath, relativePath, true)

	want := filepath.Clean("/downloads/Movie.mkv")
	count := 0
	for _, candidate := range candidates {
		if candidate == want {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestFilePathCandidates_UsesDownloadPath(t *testing.T) {
	t.Parallel()

	savePath := "/downloads"
	downloadPath := "/tmp/incomplete"
	relativePath := "Show.S01/Show.S01E01.mkv"
	contentPath := "/downloads/Show.S01"

	candidates := filePathCandidates(savePath, downloadPath, contentPath, relativePath, false)

	require.GreaterOrEqual(t, len(candidates), 3)
	require.Equal(t, filepath.Clean(filepath.Join(savePath, relativePath)), candidates[0])
	require.Equal(t, filepath.Clean(filepath.Join(downloadPath, relativePath)), candidates[1])
	require.Contains(t, candidates, filepath.Clean(filepath.Join(contentPath, relativePath)))
}
