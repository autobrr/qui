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

	assert.Equal(t, "movies-xseed", EffectiveInstanceDirName("Movies", " movies-xseed "))
	assert.Equal(t, "Movies", EffectiveInstanceDirName("Movies", " "))
}

func TestBuildDestDir_ByInstanceUsesOverride(t *testing.T) {
	t.Parallel()

	dest := BuildDestDir(
		"/hardlinks",
		"by-instance",
		"movies-xseed",
		"abcdef1234567890",
		"My.Movie.2024",
		[]hardlinktree.TorrentFile{{Path: "movie.mkv", Size: 1000}},
	)

	normalized := filepath.ToSlash(dest)
	assert.Contains(t, normalized, "/hardlinks/movies-xseed/")
	assert.Contains(t, normalized, "My.Movie.2024--abcdef12")
}

func TestBuildDestDir_ByTrackerFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	dest := BuildDestDir(
		"/hardlinks",
		"by-tracker",
		"",
		"abcdef1234567890",
		"My.Movie.2024",
		[]hardlinktree.TorrentFile{{Path: "Release/movie.mkv", Size: 1000}},
	)

	assert.Equal(t, filepath.Join("/hardlinks", "Unknown"), dest)
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
