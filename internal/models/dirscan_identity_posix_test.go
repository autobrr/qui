// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package models_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestDirScanStore_CaseDistinctDirectories(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			first := filepath.Join(root, "Media")
			second := filepath.Join(root, "media")
			require.NoError(t, os.Mkdir(first, 0o750))
			if err := os.Mkdir(second, 0o750); os.IsExist(err) {
				t.Skip("fixture filesystem is case-insensitive")
			} else {
				require.NoError(t, err)
			}
			firstInfo, err := os.Stat(first)
			require.NoError(t, err)
			secondInfo, err := os.Stat(second)
			require.NoError(t, err)
			require.False(t, os.SameFile(firstInfo, secondInfo))

			db := setupDirScanTestDB(t)
			instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			instance, err := instances.Create(t.Context(), "synthetic", "http://localhost:8080", "", "", nil, nil, false, nil)
			require.NoError(t, err)
			store := models.NewDirScanStore(db)
			_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{
				Path: first, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
			})
			require.NoError(t, err)
			path := second
			if operation == "update" {
				path = filepath.Join(root, "Other")
			}
			created, err := store.CreateDirectory(t.Context(), &models.DirScanDirectory{
				Path: path, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
			})
			require.NoError(t, err)
			if operation == "update" {
				_, err = store.UpdateDirectory(t.Context(), created.ID, &models.DirScanDirectoryUpdateParams{Path: &second})
				require.NoError(t, err)
			}
			stored, err := store.GetDirectory(t.Context(), created.ID)
			require.NoError(t, err)
			require.Equal(t, second, stored.Path)
		})
	}
}
