// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

type fakeHelpers struct {
	instanceID int
	hashes     map[string]bool
}

func (f fakeHelpers) HelperCheck(instanceID int) func(hash string) bool {
	if instanceID != f.instanceID || len(f.hashes) == 0 {
		return nil
	}
	return func(hash string) bool { return f.hashes[hash] }
}

func TestBackupsLeaveOutHelperTorrents(t *testing.T) {
	t.Parallel()

	const (
		seed   = "aaaa000000000000000000000000000000000001"
		helper = "cccc000000000000000000000000000000000003"
		own    = "dddd000000000000000000000000000000000004"
	)
	live := []qbt.Torrent{
		{Hash: seed, Name: "Quiet.Harbor.2024.1080p.WEB-DL-GRP", TotalSize: 1000},
		{Hash: helper, Name: "Quiet.Harbor.2024.1080p.WEB-DL-GRP", TotalSize: 1000, Tags: "qui-transfer"},
		{Hash: own, Name: "Quiet.Harbor.2024.1080p.WEB-DL-GRP", TotalSize: 1000, Tags: "qui-transfer"},
	}

	for _, tc := range []struct {
		name       string
		helpers    map[string]bool
		wantBackup []string
		wantDelete []string
	}{
		{
			name:       "a helper torrent is left out",
			helpers:    map[string]bool{helper: true},
			wantBackup: []string{seed, own},
			wantDelete: []string{own},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := setupTestBackupDB(t)
			ctx := t.Context()
			instanceID := insertTestInstance(t, db, "helper-torrents")
			store := models.NewBackupStore(db)
			sm := &stubBackupSyncManager{torrents: live, exportData: []byte("d4:infod4:name4:showee")}
			svc := NewService(store, sm, Config{WorkerCount: 1, DataDir: t.TempDir()}, nil)
			svc.SetHelperTorrents(fakeHelpers{instanceID: instanceID, hashes: tc.helpers})

			result, err := svc.executeBackup(ctx, job{runID: 1, instanceID: instanceID, kind: models.BackupRunKindManual})
			require.NoError(t, err)
			backedUp := make([]string, 0, len(result.items))
			for _, item := range result.items {
				backedUp = append(backedUp, item.TorrentHash)
			}
			require.ElementsMatch(t, tc.wantBackup, backedUp, "backup")

			now := time.Now().UTC()
			run := &models.BackupRun{
				InstanceID: instanceID, Kind: models.BackupRunKindManual, Status: models.BackupRunStatusSuccess,
				RequestedBy: "tester", RequestedAt: now, StartedAt: &now, CompletedAt: &now,
			}
			require.NoError(t, store.CreateRun(ctx, run))
			require.NoError(t, store.InsertItems(ctx, run.ID, []models.BackupItem{{RunID: run.ID, TorrentHash: seed, Name: live[0].Name, SizeBytes: 1000}}))
			plan, err := svc.PlanRestoreDiff(ctx, run.ID, RestoreModeComplete, nil)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.wantDelete, plan.Torrents.Delete, "complete restore")
		})
	}
}
