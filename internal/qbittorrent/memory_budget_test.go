// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/biglib"
)

// The bytes that one list read with a Torrent view allocates on a library of
// biglib.Size torrents (ADR 0051).
func TestMemoryBudget_ListRead(t *testing.T) {
	const budget = 10 * biglib.MB

	client, err := NewClientWithTimeout(1, biglib.Serve(t, biglib.Torrents(biglib.Size)), "", "", "", nil, nil, false, time.Minute, time.Minute)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	view := TorrentListView{InstanceID: 1, Sort: "added_on", Order: "desc", Limit: 300}

	allocated := biglib.AllocatedBytes(5, func() {
		response, err := sm.ListTorrents(t.Context(), view)
		require.NoError(t, err)
		require.Len(t, response.Torrents, 300)
	})
	t.Logf("one list read allocates %.1f MB", float64(allocated)/biglib.MB)
	require.LessOrEqual(t, allocated, uint64(budget))
}
