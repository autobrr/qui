// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/biglib"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// The bytes that the stream allocates to build the payload of one view on a
// library of biglib.Size torrents (ADR 0051).
func TestMemoryBudget_StreamPayload(t *testing.T) {
	const budget = 10 * biglib.MB

	client, err := qbittorrent.NewClientWithTimeout(1, biglib.Serve(t, biglib.Torrents(biglib.Size)), "", "", "", nil, nil, false, time.Minute, time.Minute)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	db := testdb.NewMigratedSQLite(t, "memory-budget")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "main", "http://qbt.example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	require.Equal(t, 1, instance.ID)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	testutil.SetUnexportedField(t, pool, "clients", map[int]*qbittorrent.Client{instance.ID: client})
	sm := qbittorrent.NewSyncManager(pool, nil)

	manager := NewStreamManager(pool, sm, sm, fakeInstanceStore{})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Sort: "added_on", Order: "desc", Limit: 300}}
	group := &subscriptionGroup{key: streamOptionsKey(opts), options: opts}

	allocated := biglib.AllocatedBytes(5, func() {
		payload := manager.buildGroupUpdatePayload(group, opts, &StreamMeta{InstanceID: 1, IncludeCounts: true})
		require.NotNil(t, payload)
		require.Empty(t, payload.Err)
	})
	t.Logf("one stream payload allocates %.1f MB", float64(allocated)/biglib.MB)
	require.LessOrEqual(t, allocated, uint64(budget))
}
