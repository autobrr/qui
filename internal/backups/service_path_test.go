// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestResolveBackupPath(t *testing.T) {
	root := t.TempDir()
	svc := NewService(nil, nil, nil, Config{WorkerCount: 1, BackupDir: root}, nil)

	tests := []struct {
		name     string
		rel      string
		wantAbs  string
		wantSafe bool
	}{
		{
			name:     "canonical prefixed blob path",
			rel:      "backups/torrents/ab/cd/test.torrent",
			wantAbs:  filepath.Join(root, "torrents", "ab", "cd", "test.torrent"),
			wantSafe: true,
		},
		{
			name:     "legacy unprefixed blob path resolves to the same file",
			rel:      "torrents/ab/cd/test.torrent",
			wantAbs:  filepath.Join(root, "torrents", "ab", "cd", "test.torrent"),
			wantSafe: true,
		},
		{
			name:     "run artifact path",
			rel:      "backups/instance-1/manifest.json",
			wantAbs:  filepath.Join(root, "instance-1", "manifest.json"),
			wantSafe: true,
		},
		{
			name:     "simple file",
			rel:      "test.torrent",
			wantAbs:  filepath.Join(root, "test.torrent"),
			wantSafe: true,
		},
		{name: "empty path", rel: "", wantSafe: false},
		{name: "traversal with ../", rel: "../../../etc/passwd", wantSafe: false},
		{name: "traversal in middle", rel: "backups/../../../etc/passwd", wantSafe: false},
		{name: "hidden traversal with dot segments", rel: "backups/./../../etc/passwd", wantSafe: false},
		{name: "mid-path traversal rejected even when it stays under the root", rel: "backups/x/../../etc/passwd", wantSafe: false},
		{
			name:     "dotted filename is not a traversal segment",
			rel:      "backups/..hidden.torrent",
			wantAbs:  filepath.Join(root, "..hidden.torrent"),
			wantSafe: true,
		},
		{
			name:     "uncleaned prefix still strips",
			rel:      "./backups/torrents/ab/test.torrent",
			wantAbs:  filepath.Join(root, "torrents", "ab", "test.torrent"),
			wantSafe: true,
		},
		{name: "absolute path unix", rel: "/etc/passwd", wantSafe: false},
		{name: "double dot only", rel: "..", wantSafe: false},
		{name: "dot dot slash", rel: "../", wantSafe: false},
		{name: "windows backslash traversal", rel: `..\..\evil.torrent`, wantSafe: false},
		{name: "windows absolute backslash", rel: `\evil\evil.torrent`, wantSafe: false},
		{name: "windows drive letter", rel: `C:\evil\evil.torrent`, wantSafe: false},
		{name: "windows drive letter forward slashes", rel: "C:/evil/evil.torrent", wantSafe: false},
		{name: "windows UNC path", rel: `\\server\share\evil.torrent`, wantSafe: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.ResolveBackupPath(tt.rel)
			if tt.wantSafe {
				require.Equal(t, tt.wantAbs, result)
			} else {
				assert.Empty(t, result, "expected unsafe path to resolve empty")
			}
		})
	}
}

func TestResolveBackupPathNoRoot(t *testing.T) {
	svc := NewService(nil, nil, nil, Config{WorkerCount: 1}, nil)
	assert.Empty(t, svc.ResolveBackupPath("backups/torrents/ab/test.torrent"))
}

func TestBackupDirOverridesDataDir(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	store := models.NewBackupStore(setupTestBackupDB(t))
	svc := NewService(store, nil, nil, Config{WorkerCount: 1, DataDir: dataDir, BackupDir: backupDir}, nil)

	require.Equal(t, filepath.Join(backupDir, "torrents"), svc.cacheDir)
	require.DirExists(t, svc.cacheDir)

	abs, base, err := svc.resolveBasePaths(t.Context(), nil, 7)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(backupDir, "instance-7"), abs)
	assert.Equal(t, filepath.Join("backups", "instance-7"), base)
}

func TestDeleteRunKeepsBlobReferencedUnderOtherSpelling(t *testing.T) {
	db := setupTestBackupDB(t)
	instanceID := insertTestInstance(t, db, "test-instance")
	store := models.NewBackupStore(db)

	backupDir := t.TempDir()
	svc := NewService(store, nil, nil, Config{WorkerCount: 1, DataDir: t.TempDir(), BackupDir: backupDir}, nil)

	blobAbs := filepath.Join(backupDir, "torrents", "aa", "bb", "cc", "shared.torrent")
	require.NoError(t, os.MkdirAll(filepath.Dir(blobAbs), 0o755))
	require.NoError(t, os.WriteFile(blobAbs, []byte("shared"), 0o600))

	ctx := t.Context()
	now := time.Unix(0, 0).UTC()
	makeRun := func(blobRel string) *models.BackupRun {
		run := &models.BackupRun{
			InstanceID:  instanceID,
			Kind:        models.BackupRunKindManual,
			Status:      models.BackupRunStatusSuccess,
			RequestedBy: "tester",
			RequestedAt: now,
			StartedAt:   &now,
			CompletedAt: &now,
		}
		require.NoError(t, store.CreateRun(ctx, run))
		require.NoError(t, store.InsertItems(ctx, run.ID, []models.BackupItem{{
			RunID:           run.ID,
			TorrentHash:     "shared-hash",
			Name:            "Shared Torrent",
			SizeBytes:       6,
			TorrentBlobPath: &blobRel,
		}}))
		return run
	}

	// Both spellings resolve to the same blob file (see TestResolveBackupPath).
	canonical := makeRun("backups/torrents/aa/bb/cc/shared.torrent")
	makeRun("torrents/aa/bb/cc/shared.torrent")

	require.NoError(t, svc.DeleteRun(ctx, canonical.ID))

	require.FileExists(t, blobAbs, "blob still referenced by the remaining run must survive the delete")
}

func TestBackupDirDefaultsUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	svc := NewService(nil, nil, nil, Config{WorkerCount: 1, DataDir: dataDir}, nil)

	require.Equal(t, filepath.Join(dataDir, "backups", "torrents"), svc.cacheDir)
	assert.Equal(t, filepath.Join(dataDir, "backups", "torrents", "ab", "x.torrent"),
		svc.ResolveBackupPath("backups/torrents/ab/x.torrent"))
}
