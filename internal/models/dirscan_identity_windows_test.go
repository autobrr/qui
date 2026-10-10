// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestDirScanStore_WindowsDirectoryIdentity(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Media Library")
			alias := filepath.Join(root, "MEDIA LIBRARY")
			other := filepath.Join(root, "Other Library")
			require.NoError(t, os.Mkdir(path, 0o750))
			require.NoError(t, os.Mkdir(other, 0o750))
			info, err := os.Stat(path)
			require.NoError(t, err)
			aliasInfo, err := os.Stat(alias)
			require.NoError(t, err)
			require.True(t, os.SameFile(info, aliasInfo), "fixture must name one actual directory")

			db := setupDirScanTestDB(t)
			instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			instance, err := instances.Create(t.Context(), "synthetic", "http://localhost:8080", "", "", nil, nil, false, nil)
			require.NoError(t, err)
			store := models.NewDirScanStore(db)
			_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{
				Path: path, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
			})
			require.NoError(t, err)
			second, err := store.CreateDirectory(t.Context(), &models.DirScanDirectory{
				Path: other, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
			})
			require.NoError(t, err, "a different directory remains configurable")
			if operation == "create" {
				_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{
					Path: alias, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
				})
			} else {
				_, err = store.UpdateDirectory(t.Context(), second.ID, &models.DirScanDirectoryUpdateParams{Path: &alias})
			}
			if !errors.Is(err, models.ErrDuplicateDirScanDirectoryPath) {
				t.Errorf("one Windows directory cannot become two routing identities: got error %v", err)
			}
			stored, err := store.GetDirectory(t.Context(), second.ID)
			require.NoError(t, err)
			assert.Equal(t, other, stored.Path)
		})
	}
}
