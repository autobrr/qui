// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestDirScanStore_UpsertFile_ReplacesChangedFileIDForSamePath(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	file := &models.DirScanFile{
		DirectoryID: dir.ID,
		FilePath:    "/data/media/file.mkv",
		FileSize:    1,
		FileModTime: time.Now(),
		FileID:      bytesOfLength(12, 1),
		Status:      models.DirScanFileStatusPending,
	}
	require.NoError(t, store.UpsertFile(ctx, file))

	file.FileID = bytesOfLength(24, 2)
	require.NoError(t, store.UpsertFile(ctx, file))

	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, file.FileID, files[0].FileID)
}

func TestDirScanStore_UpsertFile_RoundTripsSearchedIndexerIDs(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	file := &models.DirScanFile{
		DirectoryID:        dir.ID,
		FilePath:           "/data/media/file.mkv",
		FileSize:           1,
		FileModTime:        time.Now(),
		Status:             models.DirScanFileStatusNoMatch,
		SearchedIndexerIDs: []int{3, 7},
	}
	require.NoError(t, store.UpsertFile(ctx, file))

	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, []int{3, 7}, files[0].SearchedIndexerIDs)

	// A nil set writes NULL and reads back as nil.
	file.SearchedIndexerIDs = nil
	require.NoError(t, store.UpsertFile(ctx, file))
	files, err = store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Nil(t, files[0].SearchedIndexerIDs)
}

func TestDirScanStore_RequeueNoMatchFiles(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	seed := []*models.DirScanFile{
		{FilePath: "/data/media/a.mkv", Status: models.DirScanFileStatusNoMatch, SearchedIndexerIDs: []int{1}},
		{FilePath: "/data/media/b.mkv", Status: models.DirScanFileStatusNoMatch},
		{FilePath: "/data/media/c.mkv", Status: models.DirScanFileStatusMatched, MatchedTorrentHash: "abc"},
		{FilePath: "/data/media/d.mkv", Status: models.DirScanFileStatusPending},
	}
	for _, f := range seed {
		f.DirectoryID = dir.ID
		f.FileSize = 1
		f.FileModTime = time.Now()
		require.NoError(t, store.UpsertFile(ctx, f))
	}

	requeued, err := store.RequeueNoMatchFiles(ctx, dir.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, requeued)

	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 4)

	byPath := make(map[string]*models.DirScanFile, len(files))
	for _, f := range files {
		byPath[f.FilePath] = f
	}
	require.Equal(t, models.DirScanFileStatusPending, byPath["/data/media/a.mkv"].Status)
	require.Nil(t, byPath["/data/media/a.mkv"].SearchedIndexerIDs)
	require.Equal(t, models.DirScanFileStatusPending, byPath["/data/media/b.mkv"].Status)
	require.Equal(t, models.DirScanFileStatusMatched, byPath["/data/media/c.mkv"].Status)
	require.Equal(t, "abc", byPath["/data/media/c.mkv"].MatchedTorrentHash)
	require.Equal(t, models.DirScanFileStatusPending, byPath["/data/media/d.mkv"].Status)
}

func bytesOfLength(length int, value byte) []byte {
	result := make([]byte, length)
	for i := range result {
		result[i] = value
	}
	return result
}
