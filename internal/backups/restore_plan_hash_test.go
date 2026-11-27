// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestARestorePlanFindsEachTorrentByAnyCaseOfItsHash(t *testing.T) {
	kept, added, gone := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	db := setupTestBackupDB(t)
	instanceID := insertTestInstance(t, db, "restore-hash-case")
	store := models.NewBackupStore(db)
	run := &models.BackupRun{InstanceID: instanceID, Kind: models.BackupRunKindManual, Status: models.BackupRunStatusSuccess,
		RequestedBy: "tester", RequestedAt: time.Unix(0, 0).UTC()}
	require.NoError(t, store.CreateRun(t.Context(), run))
	require.NoError(t, store.InsertItems(t.Context(), run.ID, []models.BackupItem{
		{RunID: run.ID, TorrentHash: " " + strings.ToUpper(kept) + " ", Name: "Kept", Category: new("tv")},
		{RunID: run.ID, TorrentHash: added, Name: "Added", Category: new("movies")},
	}))
	reader := &stubBackupSyncManager{torrents: []qbt.Torrent{
		{Hash: strings.ToUpper(kept), Name: "Kept", Category: "movies"},
		{Hash: gone, Name: "Gone"},
	}}
	svc := NewService(store, reader, Config{WorkerCount: 1, DataDir: t.TempDir()}, nil)

	plan, err := svc.PlanRestoreDiff(t.Context(), run.ID, RestoreModeComplete, nil)
	require.NoError(t, err)
	require.Len(t, plan.Torrents.Add, 1)
	require.Equal(t, added, plan.Torrents.Add[0].Manifest.Hash)
	require.Len(t, plan.Torrents.Update, 1)
	require.Equal(t, kept, plan.Torrents.Update[0].Hash)
	require.Equal(t, kept, plan.Torrents.Update[0].Current.Hash)
	require.Equal(t, []string{gone}, plan.Torrents.Delete)

	plan, err = svc.PlanRestoreDiff(t.Context(), run.ID, RestoreModeComplete,
		&RestorePlanOptions{ExcludeHashes: []string{strings.ToUpper(added), " " + kept, strings.ToUpper(gone)}})
	require.NoError(t, err)
	require.Empty(t, plan.Torrents.Add)
	require.Empty(t, plan.Torrents.Update)
	require.Empty(t, plan.Torrents.Delete)
}
