// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"fmt"
	"testing"
	"testing/synctest"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type fakeHelpers map[string]string

func (f fakeHelpers) HelperKinds(instanceID int) map[string]string {
	if instanceID != 1 || len(f) == 0 {
		return nil
	}
	return f
}

func TestStatisticsCountNoHelperTorrent(t *testing.T) {
	const (
		seed   = "aaaa000000000000000000000000000000000001"
		helper = "cccc000000000000000000000000000000000003"
		own    = "dddd000000000000000000000000000000000004"
	)
	torrent := func(hash, tags string) string {
		return fmt.Sprintf(`%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","added_on":1,"size":1000,"state":"stalledUP","uploaded":500,"downloaded":1000,"tags":%q}`, hash, tags)
	}
	owner, syncManager := newStubSync(t, func() string {
		return fmt.Sprintf(`{"rid":1,"full_update":true,"torrents":{%s,%s,%s}}`,
			torrent(seed, ""), torrent(helper, "qui-transfer"), torrent(own, "qui-transfer"))
	})

	for _, tc := range []struct {
		name    string
		helpers fakeHelpers
		want    []string
	}{
		{name: "a helper torrent is left out", helpers: fakeHelpers{helper: "transfer"}, want: []string{seed, own}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				module := &Module{wake: make(chan struct{}, 1)}
				adapter := NewAdapter(module, syncManager)
				adapter.SetHelperTorrents(tc.helpers)
				require.NoError(t, owner.Sync(t.Context()))
				adapter.HandleSyncUpdate(1, qbt.SyncUpdate{Snapshot: owner.SnapshotUnchecked()})
				synctest.Wait()

				readings := queued(module)
				require.Len(t, readings, 1)
				hashes := make([]string, 0, len(readings[0].Torrents))
				for _, torrent := range readings[0].Torrents {
					hashes = append(hashes, torrent.Hash)
				}
				require.ElementsMatch(t, tc.want, hashes)
			})
		})
	}
}
