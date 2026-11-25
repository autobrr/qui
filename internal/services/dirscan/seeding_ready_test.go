// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestSyncManagerSeedingReady(t *testing.T) {
	ctx := t.Context()
	stub := qbtstub.New(t, t.TempDir())
	stub.FailSyncs = 1

	db := testdb.NewMigratedSQLite(t, "dirscan-seeding-ready")
	instanceStore, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "synthetic", stub.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	_, err = pool.GetClient(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stub.Syncs())
	files := syncManagerTorrentFiles{qbittorrent.NewSyncManager(pool, nil)}

	require.ErrorContains(t, files.SeedingReady(ctx, instance.ID), "waiting for first sync")
	select {
	case <-stub.Synced(3):
	case <-time.After(10 * time.Second):
		t.Fatal("the hinted sync never landed")
	}
	require.NoError(t, files.SeedingReady(ctx, instance.ID))

	require.ErrorContains(t, syncManagerTorrentFiles{}.SeedingReady(ctx, instance.ID), "client pool unavailable")
}
