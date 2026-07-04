// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestExecuteBackupCapturesSavePath(t *testing.T) {
	tests := []struct {
		name             string
		includeSavePaths bool
		torrentSavePath  string
		categorySavePath string
		wantStored       string
	}{
		{
			name:             "divergent path is stored",
			includeSavePaths: true,
			torrentSavePath:  "/data/cross-seed/Show--abc12345",
			categorySavePath: "/data/movies",
			wantStored:       "/data/cross-seed/Show--abc12345",
		},
		{
			name:             "path matching category is left to category placement",
			includeSavePaths: true,
			torrentSavePath:  "/data/movies",
			categorySavePath: "/data/movies",
			wantStored:       "",
		},
		{
			name:             "toggle off never stores a path",
			includeSavePaths: false,
			torrentSavePath:  "/data/cross-seed/Show--abc12345",
			categorySavePath: "/data/movies",
			wantStored:       "",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTestBackupDB(t)
			ctx := context.Background()
			instanceID := insertTestInstance(t, db, fmt.Sprintf("savepath-capture-%d", i))
			store := models.NewBackupStore(db)

			require.NoError(t, store.UpsertSettings(ctx, &models.BackupSettings{
				InstanceID:        instanceID,
				Enabled:           true,
				HourlyEnabled:     true,
				KeepHourly:        1,
				IncludeCategories: true,
				IncludeSavePaths:  tt.includeSavePaths,
			}))

			sm := &stubBackupSyncManager{
				torrents: []qbt.Torrent{{
					Hash:      "h1",
					Name:      "Show",
					Category:  "movies",
					SavePath:  tt.torrentSavePath,
					TotalSize: 100,
				}},
				categories: map[string]qbt.Category{
					"movies": {Name: "movies", SavePath: tt.categorySavePath},
				},
				exportData:  []byte("torrent-bytes"),
				exportName:  "Show",
				exportTrack: "tracker",
			}

			svc := NewService(store, sm, nil, Config{WorkerCount: 1, DataDir: t.TempDir()}, nil)
			svc.now = func() time.Time { return time.Unix(0, 0).UTC() }

			result, err := svc.executeBackup(ctx, job{runID: int64(i + 1), instanceID: instanceID, kind: models.BackupRunKindManual})
			require.NoError(t, err)
			require.Len(t, result.items, 1)

			if tt.wantStored == "" {
				require.Nil(t, result.items[0].SavePath)
			} else {
				require.NotNil(t, result.items[0].SavePath)
				require.Equal(t, tt.wantStored, *result.items[0].SavePath)
			}
		})
	}
}

func TestBackupItemSavePathRoundTrip(t *testing.T) {
	db := setupTestBackupDB(t)
	ctx := context.Background()
	instanceID := insertTestInstance(t, db, "savepath-roundtrip")
	store := models.NewBackupStore(db)

	now := time.Unix(0, 0).UTC()
	run := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindManual,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "tester",
		RequestedAt: now,
	}
	require.NoError(t, store.CreateRun(ctx, run))

	savePath := "/data/cross-seed/Show--abc12345"
	require.NoError(t, store.InsertItems(ctx, run.ID, []models.BackupItem{
		{RunID: run.ID, TorrentHash: "hash-divergent", Name: "Divergent", SizeBytes: 10, SavePath: &savePath},
		{RunID: run.ID, TorrentHash: "hash-plain", Name: "Plain", SizeBytes: 20},
	}))

	items, err := store.ListItems(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	byHash := make(map[string]*models.BackupItem, len(items))
	for _, it := range items {
		byHash[it.TorrentHash] = it
	}
	require.NotNil(t, byHash["hash-divergent"].SavePath)
	require.Equal(t, savePath, *byHash["hash-divergent"].SavePath)
	require.Nil(t, byHash["hash-plain"].SavePath)

	got, err := store.GetItemByHash(ctx, run.ID, "hash-divergent")
	require.NoError(t, err)
	require.NotNil(t, got.SavePath)
	require.Equal(t, savePath, *got.SavePath)

	// LoadManifest must rehydrate the path so it reaches the restore planner.
	svc := NewService(store, nil, nil, Config{WorkerCount: 1, DataDir: t.TempDir()}, nil)
	manifest, err := svc.LoadManifest(ctx, run.ID)
	require.NoError(t, err)

	var divergent *ManifestItem
	for idx := range manifest.Items {
		if manifest.Items[idx].Hash == "hash-divergent" {
			divergent = &manifest.Items[idx]
		}
	}
	require.NotNil(t, divergent)
	require.Equal(t, savePath, divergent.SavePath)
}
