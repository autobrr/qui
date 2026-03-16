// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestBuildManagedDeleteCleanupTargets_SingleFileUsesParentDir(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(trackerDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	})

	require.Len(t, targets, 1)
	require.Equal(t, leafDir, targets[0].dir)
	require.Equal(t, baseDir, targets[0].baseDir)
}

func TestCleanupManagedDeleteTargets_RemovesEmptyParentsUntilBase(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(trackerDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	})
	require.Len(t, targets, 1)

	require.NoError(t, os.Remove(filePath))
	cleanupManagedDeleteTargets(targets)

	_, err := os.Stat(leafDir)
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = os.Stat(trackerDir)
	require.ErrorIs(t, err, os.ErrNotExist)

	info, err := os.Stat(baseDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestCleanupManagedDeleteTargets_StopsAtNonEmptyParent(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	movieADir := filepath.Join(trackerDir, "MovieA")
	movieBDir := filepath.Join(trackerDir, "MovieB")
	movieAPath := filepath.Join(movieADir, "MovieA.mkv")
	movieBPath := filepath.Join(movieBDir, "MovieB.mkv")

	require.NoError(t, os.MkdirAll(movieADir, 0o755))
	require.NoError(t, os.MkdirAll(movieBDir, 0o755))
	require.NoError(t, os.WriteFile(movieAPath, []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(movieBPath, []byte("b"), 0o600))

	targets := buildManagedDeleteCleanupTargets(baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    movieADir,
			ContentPath: movieAPath,
		},
	})
	require.Len(t, targets, 1)

	require.NoError(t, os.Remove(movieAPath))
	cleanupManagedDeleteTargets(targets)

	_, err := os.Stat(movieADir)
	require.ErrorIs(t, err, os.ErrNotExist)

	info, err := os.Stat(trackerDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	info, err = os.Stat(movieBDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}
