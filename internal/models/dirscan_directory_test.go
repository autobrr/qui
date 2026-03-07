// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestDirScanStore_CreateDirectory_RejectsDuplicateCleanedPath(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instanceA, err := instanceStore.Create(ctx, "A", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	instanceB, err := instanceStore.Create(ctx, "B", "http://localhost:8081", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	_, err = store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media/tv",
		Enabled:             true,
		TargetInstanceID:    instanceA.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	_, err = store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media/tv/",
		Enabled:             true,
		TargetInstanceID:    instanceB.ID,
		ScanIntervalMinutes: 60,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, models.ErrDuplicateDirScanDirectoryPath))
}

func TestDirScanStore_UpdateDirectory_RejectsDuplicateCleanedPath(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	first, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media/tv",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	second, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/media/movies",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	updatedPath := first.Path + "/"
	_, err = store.UpdateDirectory(ctx, second.ID, &models.DirScanDirectoryUpdateParams{
		Path: &updatedPath,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, models.ErrDuplicateDirScanDirectoryPath))
}
