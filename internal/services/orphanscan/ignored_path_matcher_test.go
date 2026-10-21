// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// Folder cleanup reads the ignore paths through this matcher, and they must
// protect a folder with orphan scan turned off.
func TestIgnoredPathMatcher(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "orphanscan-ignored-path-matcher")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "a", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	root := t.TempDir()
	keep := filepath.Join(root, "torrents", "Keep")
	store := models.NewOrphanScanStore(db)
	svc := NewService(DefaultConfig(), nil, store, nil, nil, nil)

	ignored, err := svc.IgnoredPathMatcher(t.Context(), instance.ID, fsops.HostPaths)
	require.NoError(t, err)
	require.False(t, ignored(keep), "an instance without settings ignores nothing")

	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          instance.ID,
		Enabled:             false,
		IgnorePaths:         []string{keep},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
	})
	require.NoError(t, err)

	ignored, err = svc.IgnoredPathMatcher(t.Context(), instance.ID, fsops.HostPaths)
	require.NoError(t, err)
	for p, want := range map[string]bool{
		keep:                                     true,
		filepath.Join(keep, "Sub"):               true,
		filepath.Join(root, "torrents"):          true, // holds an ignored path
		filepath.Join(root, "torrents", "Kee"):   false,
		filepath.Join(root, "torrents", "Other"): false,
	} {
		require.Equal(t, want, ignored(p), p)
	}

	svc.store = models.NewOrphanScanStore(settingsFailingQuerier{Querier: db})
	_, err = svc.IgnoredPathMatcher(t.Context(), instance.ID, fsops.HostPaths)
	require.Error(t, err, "an unreadable ignore list must not read as an empty one")
}
