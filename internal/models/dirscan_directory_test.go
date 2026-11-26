// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestDirScanStore_CreateDirectory_PersistsAllowedDownloadClients(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	created, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/usenet/tv",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
		AllowedDownloadClients: []string{
			"SABnzbd",
			"NZBGet",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"SABnzbd", "NZBGet"}, created.AllowedDownloadClients)
}

func TestDirScanStore_UpdateDirectory_PersistsAllowedDownloadClients(t *testing.T) {
	ctx := context.Background()
	db := setupDirScanTestDB(t)

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	created, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
		Path:                "/data/usenet/movies",
		Enabled:             true,
		TargetInstanceID:    instance.ID,
		ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)

	allowed := []string{"qBittorrent"}
	updated, err := store.UpdateDirectory(ctx, created.ID, &models.DirScanDirectoryUpdateParams{
		AllowedDownloadClients: &allowed,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"qBittorrent"}, updated.AllowedDownloadClients)
}
