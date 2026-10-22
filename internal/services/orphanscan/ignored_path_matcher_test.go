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

	settings := &models.OrphanScanSettings{
		InstanceID:          instance.ID,
		Enabled:             false,
		IgnorePaths:         []string{keep},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
	}
	_, err = store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	// TestProtectedPaths holds the matching rules. This only checks that the
	// matcher reads the settings and protects a folder that holds an ignore path.
	ignored, err = svc.IgnoredPathMatcher(t.Context(), instance.ID, fsops.HostPaths)
	require.NoError(t, err)
	require.True(t, ignored(keep))
	require.True(t, ignored(filepath.Join(root, "torrents")), "a folder that holds an ignore path is protected")

	settings.IgnorePaths = []string{"torrents/Keep"}
	_, err = store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)
	_, err = svc.IgnoredPathMatcher(t.Context(), instance.ID, fsops.HostPaths)
	require.Error(t, err, "an ignore path that is not valid must not read as an empty list")

	svc.store = models.NewOrphanScanStore(settingsFailingQuerier{Querier: db})
	_, err = svc.IgnoredPathMatcher(t.Context(), instance.ID, fsops.HostPaths)
	require.Error(t, err, "an unreadable ignore list must not read as an empty one")
}
