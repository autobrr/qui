// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// Tag changes go through the full tag methods: qBittorrent 5.1 gets one
// setTags for each tag set, older versions one addTags or removeTags for each
// tag, and qui syncs after the edits so the list shows the new tags.
func TestApplyTagChanges(t *testing.T) {
	const (
		hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	for _, tc := range []struct {
		webAPI                   string
		setTags, addTags, rmTags int
	}{
		{webAPI: "2.11.4", setTags: 2},
		{webAPI: "2.9.3", addTags: 2, rmTags: 1},
	} {
		t.Run(tc.webAPI, func(t *testing.T) {
			dir := t.TempDir()
			stub := qbtstub.New(t, dir)
			stub.WebAPIVersion = tc.webAPI
			stub.Add(qbtstub.Torrent{Hash: hashA, Name: "Show.S01E01-GRP", SavePath: dir, Layout: qbtstub.SingleFile, Tags: []string{"keep", "old"}})
			stub.Add(qbtstub.Torrent{Hash: hashB, Name: "Show.S01E02-GRP", SavePath: dir, Layout: qbtstub.SingleFile, Tags: []string{"old"}})

			db := testdb.NewMigratedSQLite(t, t.Name())
			instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
			require.NoError(t, err)
			instance, err := instanceStore.Create(t.Context(), "test", stub.URL, "", "", nil, nil, false, new(false))
			require.NoError(t, err)
			clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { _ = clientPool.Close() })
			sm := qbittorrent.NewSyncManager(clientPool, nil)
			client, err := sm.GetClient(t.Context(), instance.ID)
			require.NoError(t, err)
			require.NoError(t, client.SyncOwner().Sync(t.Context()))

			svc := NewService(Config{}, instanceStore, nil, nil, nil, sm, nil, nil, nil, nil)
			syncs := stub.Syncs()
			svc.applyTagChanges(t.Context(), instance.ID, map[string]*tagChange{
				hashA: {desired: tagSet("keep", "new1"), toAdd: []string{"new1"}, toRemove: []string{"old"}},
				hashB: {desired: tagSet("new1", "new2"), toAdd: []string{"new1", "new2"}, toRemove: []string{"old"}},
			})

			require.Equal(t, tc.setTags, stub.Writes("torrents/setTags"))
			require.Equal(t, tc.addTags, stub.Writes("torrents/addTags"))
			require.Equal(t, tc.rmTags, stub.Writes("torrents/removeTags"))
			require.Equal(t, []string{"keep", "new1"}, stub.Tags(hashA))
			require.Equal(t, []string{"new1", "new2"}, stub.Tags(hashB))
			require.Eventually(t, func() bool { return stub.Syncs() > syncs }, 5*time.Second, 10*time.Millisecond)
			// The list reads this cache, so the sync must put the new tags in it.
			require.Eventually(t, func() bool {
				cached := map[string][]string{}
				for _, torrent := range client.SyncOwner().SnapshotUnchecked().Torrents() {
					cached[torrent.Hash] = slices.Sorted(slices.Values(splitTags(torrent.Tags)))
				}
				return slices.Equal(cached[hashA], []string{"keep", "new1"}) && slices.Equal(cached[hashB], []string{"new1", "new2"})
			}, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func tagSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}
