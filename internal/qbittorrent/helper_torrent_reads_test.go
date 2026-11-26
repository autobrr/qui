// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/syncsnap"
)

func refHashes(torrents []*qbt.Torrent) []string {
	hashes := make([]string, 0, len(torrents))
	for _, t := range torrents {
		hashes = append(hashes, t.Hash)
	}
	return hashes
}

func viewHashes(views []CrossInstanceTorrentView) []string {
	hashes := make([]string, 0, len(views))
	for _, v := range views {
		hashes = append(hashes, v.Hash)
	}
	return hashes
}

func listedWithoutOption(t *testing.T, sm *SyncManager, filters FilterOptions) ([]string, error) {
	t.Helper()
	resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: 1, Filters: filters, SidebarCounts: true})
	if err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(resp.Torrents))
	for _, row := range resp.Torrents {
		hashes = append(hashes, row.Hash)
	}
	require.Equal(t, 1, resp.Total)
	require.Equal(t, 1, resp.Counts.Tags["qui-transfer"], "the Sidebar counts of a view without the option leave the Helper torrent out")
	return hashes, nil
}

func TestFullListReadsLeaveOutHelperTorrents(t *testing.T) {
	t.Parallel()
	sm, _, _ := newHelperRig(t)
	sm.instanceName = func(context.Context, int) (string, error) { return "seedbox", nil }
	ctx := t.Context()

	reads := map[string]func() ([]string, error){
		"torrent refs": func() ([]string, error) {
			refs, err := sm.GetTorrentRefs(ctx, 1, TorrentRefFilter{})
			return refHashes(refs), err
		},
		"torrent refs by tag": func() ([]string, error) {
			refs, err := sm.GetTorrentRefs(ctx, 1, TorrentRefFilter{Tag: "qui-transfer"})
			return refHashes(refs), err
		},
		"fresh torrent refs": func() ([]string, error) {
			refs, err := sm.GetTorrentRefsFresh(ctx, 1, TorrentRefFilter{})
			return refHashes(refs), err
		},
		"torrent refs within a max age": func() ([]string, error) {
			refs, err := sm.GetTorrentRefsWithin(ctx, 1, TorrentRefFilter{}, time.Hour)
			return refHashes(refs), err
		},
		"all torrent refs": func() ([]string, error) {
			refs, err := sm.GetAllTorrentRefs(ctx, 1)
			return refHashes(refs), err
		},
		"cached cross-instance read": func() ([]string, error) {
			views, err := sm.GetCachedInstanceTorrents(ctx, 1)
			return viewHashes(views), err
		},
		"fresh cross-instance read": func() ([]string, error) {
			views, err := sm.ReadInstanceTorrents(ctx, 1)
			return viewHashes(views), err
		},
		"instance snapshot list": func() ([]string, error) {
			return refHashes(sm.ListSnapshot(1, sm.InstanceSnapshot(ctx, 1))), nil
		},
		"listed torrent of each hash": func() ([]string, error) {
			listed := sm.ListedLookup(1, sm.InstanceSnapshot(ctx, 1))
			var hashes []string
			for _, hash := range []string{hashA, hashB} {
				if torrent, ok := listed(hash); ok {
					hashes = append(hashes, torrent.Hash)
				}
			}
			return hashes, nil
		},
		"torrent list without the option": func() ([]string, error) {
			return listedWithoutOption(t, sm, FilterOptions{})
		},
		"filtered torrent list without the option": func() ([]string, error) {
			return listedWithoutOption(t, sm, FilterOptions{Tags: []string{"qui-transfer"}})
		},
	}
	for name, read := range reads {
		hashes, err := read()
		require.NoError(t, err, name)
		require.Equal(t, []string{hashB}, hashes, "%s keeps the real torrent and leaves out the Helper torrent", name)
	}
}

func TestReadsOfNamedHashesFindHelperTorrents(t *testing.T) {
	t.Parallel()
	sm, _, _ := newHelperRig(t)
	ctx := t.Context()
	named := TorrentRefFilter{Hashes: []string{hashA}}

	refs, err := sm.GetTorrentRefs(ctx, 1, named)
	require.NoError(t, err)
	require.Equal(t, []string{hashA}, refHashes(refs))

	refs, err = sm.GetTorrentRefsFresh(ctx, 1, named)
	require.NoError(t, err)
	require.Equal(t, []string{hashA}, refHashes(refs))

	refs, err = sm.GetTorrentRefsWithin(ctx, 1, named, time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{hashA}, refHashes(refs))

	lists := map[string]FilterOptions{
		"two named hashes":                 {Hashes: []string{hashA, hashB}},
		"a named hash with another filter": {Hashes: []string{hashA}, Tags: []string{"qui-transfer"}},
	}
	for name, filters := range lists {
		resp, err := sm.ListTorrents(ctx, TorrentListView{InstanceID: 1, Filters: filters, SidebarCounts: true})
		require.NoError(t, err, name)
		rows := make([]string, 0, len(resp.Torrents))
		for _, row := range resp.Torrents {
			rows = append(rows, row.Hash)
		}
		require.Contains(t, rows, hashA, "%s finds the Helper torrent", name)
		require.Equal(t, 1, resp.Counts.Tags["qui-transfer"], "%s keeps the Helper torrent out of the Sidebar counts", name)
	}
}

func TestReadersThatAskForHelperTorrentsGetThem(t *testing.T) {
	t.Parallel()
	sm, _, _ := newHelperRig(t)
	ctx := t.Context()

	refs, err := sm.GetTorrentRefs(ctx, 1, TorrentRefFilter{WithHelperTorrents: true})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{hashA, hashB}, refHashes(refs))

	resp, err := sm.ListTorrents(ctx, TorrentListView{InstanceID: 1, SidebarCounts: true, WithHelperTorrents: true})
	require.NoError(t, err)
	badges := map[string]string{}
	for _, row := range resp.Torrents {
		badges[row.Hash] = row.QuiHelper
	}
	require.Equal(t, map[string]string{hashA: "transfer", hashB: ""}, badges)
	require.Equal(t, 2, resp.Total)
	require.Equal(t, 2, resp.Counts.Tags["qui-transfer"], "the Sidebar counts of the torrent list count the Helper torrent")

	require.ErrorIs(t, sm.RefuseHelperFiles(ctx, 1, []string{"all"}), ErrHelperTorrentFiles, "the delete guard's all list sees the Helper torrent")
}

func TestHelperTorrentsGetNoFinishOrAddEvent(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	client := &Client{instanceID: 1}
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	sm.SetHelperTorrents(fakeHelpers{hashA: "share"})

	synctest.Test(t, func(t *testing.T) {
		notified := make(chan qbt.Torrent, 4)
		finished := make(chan qbt.Torrent, 4)
		added := make(chan []*qbt.Torrent, 4)
		sm.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) { notified <- torrent },
			func(_ context.Context, _ int, torrent qbt.Torrent) { finished <- torrent })
		sm.SetTorrentAddedHandler(func(_ context.Context, _ int, torrents []*qbt.Torrent) { added <- torrents })

		seed := map[string]qbt.Torrent{"cccccccccccccccccccccccccccccccccccccccc": {State: qbt.TorrentStateUploading}}
		client.handleCompletionUpdates(syncOf(t, src, seed))
		client.handleAddedUpdates(syncOf(t, src, seed))

		now := time.Now().Unix()
		downloading := qbt.Torrent{CompletionOn: -1, AmountLeft: 10, AddedOn: now, State: qbt.TorrentStateDownloading}
		client.handleAddedUpdates(syncOf(t, src, map[string]qbt.Torrent{hashA: downloading}))
		synctest.Wait()
		require.Empty(t, added, "a sync that adds only a Helper torrent calls no added handler")

		update := syncOf(t, src, map[string]qbt.Torrent{hashA: downloading, hashB: downloading})
		client.handleCompletionUpdates(update)
		client.handleAddedUpdates(update)
		synctest.Wait()
		require.Equal(t, []string{hashB}, refHashes(<-added), "the added handler gets only the real torrent")
		require.Empty(t, added)

		done := qbt.Torrent{CompletionOn: now, AddedOn: now, State: qbt.TorrentStateUploading}
		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{hashA: done, hashB: done}))
		synctest.Wait()
		require.Equal(t, hashB, (<-notified).Hash)
		require.Equal(t, hashB, (<-finished).Hash)
		require.Empty(t, notified, "the finish notification skips the Helper torrent")
		require.Empty(t, finished, "the completion handler skips the Helper torrent")
	})
}
