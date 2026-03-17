// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linkdir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestEffectiveInstanceDirName(t *testing.T) {
	t.Parallel()

	name, err := EffectiveInstanceDirName("Movies", " movies-xseed ")
	require.NoError(t, err)
	assert.Equal(t, "movies-xseed", name)

	name, err = EffectiveInstanceDirName("Movies", " ")
	require.NoError(t, err)
	assert.Equal(t, "Movies", name)
}

func TestEffectiveInstanceDirName_RejectsTraversal(t *testing.T) {
	t.Parallel()

	_, err := EffectiveInstanceDirName("Movies", "../escape")
	require.ErrorContains(t, err, "path separators")
}

func TestValidateInstanceDirName(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateInstanceDirName("movies-xseed"))
	require.ErrorContains(t, ValidateInstanceDirName("../escape"), "path separators")
}

func TestBuildDestDir_ByInstanceUsesOverride(t *testing.T) {
	t.Parallel()

	dest, err := BuildDestDir(
		"/hardlinks",
		"by-instance",
		"movies-xseed",
		"abcdef1234567890",
		"My.Movie.2024",
		[]hardlinktree.TorrentFile{{Path: "movie.mkv", Size: 1000}},
	)
	require.NoError(t, err)

	normalized := filepath.ToSlash(dest)
	assert.Contains(t, normalized, "/hardlinks/movies-xseed/")
	assert.Contains(t, normalized, "My.Movie.2024--abcdef12")
}

func TestBuildDestDir_ByTrackerFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	dest, err := BuildDestDir(
		"/hardlinks",
		"by-tracker",
		"",
		"abcdef1234567890",
		"My.Movie.2024",
		[]hardlinktree.TorrentFile{{Path: "Release/movie.mkv", Size: 1000}},
	)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join("/hardlinks", "Unknown"), dest)
}

func TestBuildDestDir_ByInstanceRejectsTraversal(t *testing.T) {
	t.Parallel()

	_, err := BuildDestDir(
		"/hardlinks",
		"by-instance",
		"..",
		"abcdef1234567890",
		"My.Movie.2024",
		[]hardlinktree.TorrentFile{{Path: "movie.mkv", Size: 1000}},
	)
	require.ErrorContains(t, err, "traversal segment")
}

func TestFindMatchingBaseDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "downloads", "movie.mkv")
	baseDir := filepath.Join(tmp, "cross-seed")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(sourcePath, []byte("movie"), 0o600)
	require.NoError(t, err)

	match, err := FindMatchingBaseDir("  "+baseDir+"  ", sourcePath)
	require.NoError(t, err)
	assert.Equal(t, baseDir, match)
}
