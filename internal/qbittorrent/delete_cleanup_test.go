// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops/local"
)

var testBackend = local.NewBackend()

func TestBuildManagedDeleteCleanupTargets_SingleFileUsesParentDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(trackerDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(ctx, baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	}, testBackend)

	require.Len(t, targets, 1)
	require.Equal(t, leafDir, targets[0].dir)
	require.Equal(t, baseDir, targets[0].baseDir)
}

func TestCleanupManagedDeleteTargets_RemovesEmptyParentsUntilBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(trackerDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(ctx, baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	}, testBackend)
	require.Len(t, targets, 1)

	require.NoError(t, os.Remove(filePath))
	cleanupManagedDeleteTargets(ctx, targets, testBackend)

	_, err := os.Stat(leafDir)
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = os.Stat(trackerDir)
	require.ErrorIs(t, err, os.ErrNotExist)

	info, err := os.Stat(baseDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestCleanupManagedDeleteTargets_RunsAfterRequestCancellation(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(trackerDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(context.Background(), baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	}, testBackend)
	require.Len(t, targets, 1)

	// Simulate the client disconnecting after qBittorrent accepted the delete:
	// the request context is already cancelled when pruning starts.
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, os.Remove(filePath))
	cleanupManagedDeleteTargets(requestCtx, targets, testBackend)

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
	ctx := context.Background()

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

	targets := buildManagedDeleteCleanupTargets(ctx, baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    movieADir,
			ContentPath: movieAPath,
		},
	}, testBackend)
	require.Len(t, targets, 1)

	require.NoError(t, os.Remove(movieAPath))
	cleanupManagedDeleteTargets(ctx, targets, testBackend)

	_, err := os.Stat(movieADir)
	require.ErrorIs(t, err, os.ErrNotExist)

	info, err := os.Stat(trackerDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	info, err = os.Stat(movieBDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestCleanupManagedDeleteTargets_RetriesWhileLeafDirStillBusy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	baseDir := t.TempDir()
	trackerDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(trackerDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(ctx, baseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	}, testBackend)
	require.Len(t, targets, 1)

	done := make(chan struct{})
	go func() {
		time.Sleep(75 * time.Millisecond)
		_ = os.Remove(filePath)
		close(done)
	}()

	cleanupManagedDeleteTargets(ctx, targets, testBackend)
	<-done

	_, err := os.Stat(leafDir)
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = os.Stat(trackerDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBuildManagedDeleteCleanupTargets_PrefersMostSpecificBaseDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rootDir := t.TempDir()
	baseDir := filepath.Join(rootDir, "cross-seeds")
	specificBaseDir := filepath.Join(baseDir, "tracker-a")
	leafDir := filepath.Join(specificBaseDir, "MovieA")
	filePath := filepath.Join(leafDir, "MovieA.mkv")

	require.NoError(t, os.MkdirAll(leafDir, 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	targets := buildManagedDeleteCleanupTargets(ctx, baseDir+","+specificBaseDir, []qbt.Torrent{
		{
			Hash:        "abc123",
			SavePath:    leafDir,
			ContentPath: filePath,
		},
	}, testBackend)

	require.Len(t, targets, 1)
	require.Equal(t, specificBaseDir, targets[0].baseDir)
}
