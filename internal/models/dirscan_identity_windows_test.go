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
	"golang.org/x/sys/windows"

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

func TestDirScanStore_WindowsMissingDirectoryIdentity(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "Future Library")
			alias := filepath.Join(root, "FUTURE LIBRARY")
			other := filepath.Join(root, "Other Library")
			db := setupDirScanTestDB(t)
			instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			instance, err := instances.Create(t.Context(), "synthetic", "http://localhost:8080", "", "", nil, nil, false, nil)
			require.NoError(t, err)
			store := models.NewDirScanStore(db)
			_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{
				Path: path, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
			})
			require.NoError(t, err, "a missing directory remains configurable")
			second, err := store.CreateDirectory(t.Context(), &models.DirScanDirectory{
				Path: other, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
			})
			require.NoError(t, err, "a different missing directory remains configurable")
			if operation == "create" {
				_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{
					Path: alias, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
				})
			} else {
				_, err = store.UpdateDirectory(t.Context(), second.ID, &models.DirScanDirectoryUpdateParams{Path: &alias})
			}
			require.ErrorIs(t, err, models.ErrDuplicateDirScanDirectoryPath)
			stored, err := store.GetDirectory(t.Context(), second.ID)
			require.NoError(t, err)
			assert.Equal(t, other, stored.Path)
		})
	}
}

func TestDirScanStore_WindowsCaseSensitiveDirectoryIdentity(t *testing.T) {
	root := t.TempDir()
	name, err := windows.UTF16PtrFromString(root)
	require.NoError(t, err)
	handle, err := windows.CreateFile(name, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	require.NoError(t, err)
	flags := []byte{1, 0, 0, 0} // FILE_CS_FLAG_CASE_SENSITIVE_DIR
	err = windows.SetFileInformationByHandle(handle, windows.FileCaseSensitiveInfo, &flags[0], uint32(len(flags)))
	require.NoError(t, windows.CloseHandle(handle))
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
		t.Skipf("case-sensitive directories unavailable: %v", err)
	}
	require.NoError(t, err)
	first := filepath.Join(root, "Media")
	second := filepath.Join(root, "MEDIA")
	require.NoError(t, os.Mkdir(first, 0o750))
	require.NoError(t, os.Mkdir(second, 0o750))
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
	_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{Path: first, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
	require.NoError(t, err)
	_, err = store.CreateDirectory(t.Context(), &models.DirScanDirectory{Path: second, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
	require.ErrorIs(t, err, models.ErrDuplicateDirScanDirectoryPath, "webhook routing cannot distinguish these physical directories")
	other := filepath.Join(root, "Other")
	created, err := store.CreateDirectory(t.Context(), &models.DirScanDirectory{Path: other, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
	require.NoError(t, err)
	_, err = store.UpdateDirectory(t.Context(), created.ID, &models.DirScanDirectoryUpdateParams{Path: &second})
	require.ErrorIs(t, err, models.ErrDuplicateDirScanDirectoryPath)
	stored, err := store.GetDirectory(t.Context(), created.ID)
	require.NoError(t, err)
	require.Equal(t, other, stored.Path)
}
