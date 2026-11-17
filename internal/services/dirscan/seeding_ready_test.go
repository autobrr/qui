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

// An instance that has not synced since qui started is not ready, and the
// check asks for a sync, since nothing else may start one. After a sync the
// instance is ready. A client qui cannot get names its own cause.
//
// regression: #3613
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
	// GetClient runs the first sync, which the stub fails. It runs before
	// NewSyncManager because the pool starts a tracker health loop for a
	// client it creates once a sync manager is registered, and that loop could
	// hold the pool's instance lock when the hint fires.
	_, err = pool.GetClient(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stub.Syncs())
	files := syncManagerTorrentFiles{qbittorrent.NewSyncManager(pool, nil)}

	require.ErrorContains(t, files.SeedingReady(ctx, instance.ID), "waiting for first sync")
	// Syncs never overlap, and each applies its result before the next
	// request, so a third request means the good second sync has landed.
	select {
	case <-stub.Synced(3):
	case <-time.After(10 * time.Second):
		t.Fatal("the hinted sync never landed")
	}
	require.NoError(t, files.SeedingReady(ctx, instance.ID))

	require.ErrorContains(t, syncManagerTorrentFiles{}.SeedingReady(ctx, instance.ID), "client pool unavailable")
}
