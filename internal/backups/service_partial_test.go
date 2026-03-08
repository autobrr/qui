// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	qb "github.com/autobrr/qui/internal/qbittorrent"
)

func TestHandleJobMarksPartialBackupSuccessWhenExportRecovers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestBackupDB(t)
	store := models.NewBackupStore(db)
	instanceID := insertTestInstance(t, db, "partial-success")

	svc := newBackupTestService(t, store)
	svc.listTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{
			{Hash: "hash-a", Name: "Alpha", TotalSize: 11},
			{Hash: "hash-b", Name: "Bravo", TotalSize: 22},
			{Hash: "hash-c", Name: "Charlie", TotalSize: 33},
		}, nil
	}
	svc.exportTorrent = func(_ context.Context, _ int, hash string) ([]byte, string, string, error) {
		switch hash {
		case "hash-a":
			return []byte("alpha"), "Alpha", "", nil
		case "hash-b":
			return nil, "", "", errors.New("status code: 502: bad gateway")
		case "hash-c":
			return []byte("charlie"), "Charlie", "", nil
		default:
			return nil, "", "", errors.New("unexpected hash")
		}
	}
	svc.probeInstance = func(context.Context, int) error { return nil }

	run := createBackupRun(ctx, t, store, instanceID, svc.now())
	svc.handleJob(ctx, job{runID: run.ID, instanceID: instanceID, kind: models.BackupRunKindManual})

	savedRun, err := store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, models.BackupRunStatusSuccess, savedRun.Status)
	require.Equal(t, 2, savedRun.TorrentCount)
	require.NotNil(t, savedRun.ErrorMessage)
	require.Contains(t, *savedRun.ErrorMessage, "Partial backup")
	require.NotNil(t, savedRun.ManifestPath)

	items, err := store.ListItems(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "hash-a", items[0].TorrentHash)
	require.Equal(t, "hash-c", items[1].TorrentHash)

	manifest, err := svc.LoadManifest(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, manifest.Warnings, 1)
	require.Equal(t, "hash-b", manifest.Warnings[0].Hash)
	require.Equal(t, "Bravo", manifest.Warnings[0].Name)
}

func TestHandleJobStopsEarlyAndKeepsPartialBackupWhenInstanceDies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestBackupDB(t)
	store := models.NewBackupStore(db)
	instanceID := insertTestInstance(t, db, "partial-stop")

	svc := newBackupTestService(t, store)
	svc.listTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{
			{Hash: "hash-a", Name: "Alpha", TotalSize: 11},
			{Hash: "hash-b", Name: "Bravo", TotalSize: 22},
			{Hash: "hash-c", Name: "Charlie", TotalSize: 33},
		}, nil
	}

	exported := make([]string, 0, 3)
	svc.exportTorrent = func(_ context.Context, _ int, hash string) ([]byte, string, string, error) {
		exported = append(exported, hash)
		switch hash {
		case "hash-a":
			return []byte("alpha"), "Alpha", "", nil
		case "hash-b":
			return nil, "", "", errors.New("context deadline exceeded")
		default:
			t.Fatalf("unexpected export after probe failure: %s", hash)
			return nil, "", "", nil
		}
	}
	svc.probeInstance = func(context.Context, int) error { return errors.New("connection refused") }

	run := createBackupRun(ctx, t, store, instanceID, svc.now())
	svc.handleJob(ctx, job{runID: run.ID, instanceID: instanceID, kind: models.BackupRunKindManual})

	savedRun, err := store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, models.BackupRunStatusSuccess, savedRun.Status)
	require.Equal(t, 1, savedRun.TorrentCount)
	require.Equal(t, []string{"hash-a", "hash-b"}, exported)

	manifest, err := svc.LoadManifest(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, manifest.Items, 1)
	require.Equal(t, "hash-a", manifest.Items[0].Hash)
	require.Len(t, manifest.Warnings, 1)
	require.Equal(t, "hash-b", manifest.Warnings[0].Hash)
}

func TestHandleJobFailsWhenInstanceDiesBeforeAnyTorrentExports(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestBackupDB(t)
	store := models.NewBackupStore(db)
	instanceID := insertTestInstance(t, db, "hard-fail")

	svc := newBackupTestService(t, store)
	svc.listTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "hash-a", Name: "Alpha", TotalSize: 11}}, nil
	}
	svc.exportTorrent = func(context.Context, int, string) ([]byte, string, string, error) {
		return nil, "", "", errors.New("connection reset by peer")
	}
	svc.probeInstance = func(context.Context, int) error { return errors.New("connection refused") }

	run := createBackupRun(ctx, t, store, instanceID, svc.now())
	svc.handleJob(ctx, job{runID: run.ID, instanceID: instanceID, kind: models.BackupRunKindManual})

	savedRun, err := store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, models.BackupRunStatusFailed, savedRun.Status)
	require.NotNil(t, savedRun.ErrorMessage)
	require.Contains(t, *savedRun.ErrorMessage, "instance became unavailable")

	items, err := store.ListItems(ctx, run.ID)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestLoadManifestFallsBackToDatabaseForLegacyRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestBackupDB(t)
	store := models.NewBackupStore(db)
	instanceID := insertTestInstance(t, db, "legacy-manifest")

	svc := newBackupTestService(t, store)
	run := createBackupRun(ctx, t, store, instanceID, svc.now())

	now := svc.now()
	require.NoError(t, store.UpdateRunMetadata(ctx, run.ID, func(r *models.BackupRun) error {
		r.Status = models.BackupRunStatusSuccess
		r.CompletedAt = &now
		r.Categories = map[string]models.CategorySnapshot{
			"movies": {SavePath: "/downloads/movies"},
		}
		r.Tags = []string{"tag-a"}
		return nil
	}))

	require.NoError(t, store.InsertItems(ctx, run.ID, []models.BackupItem{{
		RunID:       run.ID,
		TorrentHash: "hash-a",
		Name:        "Alpha",
		SizeBytes:   11,
	}}))

	manifest, err := svc.LoadManifest(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, manifest.Items, 1)
	require.Nil(t, manifest.Warnings)
	require.Equal(t, "hash-a", manifest.Items[0].Hash)
}

func newBackupTestService(t *testing.T, store *models.BackupStore) *Service {
	t.Helper()

	svc := NewService(store, &qb.SyncManager{}, nil, Config{
		DataDir:        t.TempDir(),
		WorkerCount:    1,
		ExportThrottle: 0,
	}, nil)
	svc.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	svc.getCategories = func(context.Context, int) (map[string]qbt.Category, error) { return map[string]qbt.Category{}, nil }
	svc.getTags = func(context.Context, int) ([]string, error) { return nil, nil }
	svc.getWebAPIVersion = func(context.Context, int) (string, error) { return "2.8.11", nil }
	svc.probeInstance = func(context.Context, int) error { return nil }
	return svc
}

func createBackupRun(ctx context.Context, t *testing.T, store *models.BackupStore, instanceID int, now time.Time) *models.BackupRun {
	t.Helper()

	run := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindManual,
		Status:      models.BackupRunStatusPending,
		RequestedBy: "tester",
		RequestedAt: now,
	}
	require.NoError(t, store.CreateRun(ctx, run))
	return run
}

func TestLoadManifestPrefersSavedManifestFileForWarnings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := setupTestBackupDB(t)
	store := models.NewBackupStore(db)
	instanceID := insertTestInstance(t, db, "saved-manifest")

	svc := newBackupTestService(t, store)
	svc.listTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{
			{Hash: "hash-a", Name: "Alpha", TotalSize: 11},
			{Hash: "hash-b", Name: "Bravo", TotalSize: 22},
		}, nil
	}
	svc.exportTorrent = func(_ context.Context, _ int, hash string) ([]byte, string, string, error) {
		if hash == "hash-a" {
			return []byte("alpha"), "Alpha", "", nil
		}
		return nil, "", "", errors.New("status code: 503: service unavailable")
	}

	run := createBackupRun(ctx, t, store, instanceID, svc.now())
	svc.handleJob(ctx, job{runID: run.ID, instanceID: instanceID, kind: models.BackupRunKindManual})

	savedRun, err := store.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.NotNil(t, savedRun.ManifestPath)

	manifestPath := savedRun.ManifestPath
	require.NotNil(t, manifestPath)
	_, statErr := os.Stat(filepath.Join(svc.cfg.DataDir, *manifestPath))
	require.NoError(t, statErr)

	manifest, err := svc.LoadManifest(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, manifest.Warnings, 1)
	require.Equal(t, "hash-b", manifest.Warnings[0].Hash)
}
