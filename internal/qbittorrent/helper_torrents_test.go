// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

// fakeHelpers is the helper registry: the kind of each helper torrent of
// instance 1.
type fakeHelpers map[string]string

func (f fakeHelpers) HelperKinds(instanceID int) map[string]string {
	if instanceID != 1 || len(f) == 0 {
		return nil
	}
	return f
}

// newHelperRig has a helper torrent (hashA) and a torrent of the user with
// the same tag and no registry row (hashB) on instance 1.
func newHelperRig(t *testing.T) (*SyncManager, *qbtstub.Server, string) {
	t.Helper()
	root := t.TempDir()
	stub := qbtstub.New(t, root)
	for _, hash := range []string{hashA, hashB} {
		stub.Add(qbtstub.Torrent{Hash: hash, Name: "Show.S01E01-GRP", SavePath: filepath.Join(root, hash),
			Layout: qbtstub.NoRootFolder, Files: []string{"Show.S01E01.mkv"}, Tags: []string{"qui-transfer"}})
	}
	client, err := NewClientWithTimeout(1, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	sm.SetHelperTorrents(fakeHelpers{hashA: "transfer"})
	return sm, stub, root
}

func TestDeleteWithFilesRefusesAHelperTorrent(t *testing.T) {
	t.Parallel()
	sm, stub, root := newHelperRig(t)
	user := DeleteActor{Kind: DeleteActorUser}

	err := sm.DeleteTorrents(t.Context(), 1, []string{hashB, hashA}, true, user)
	require.ErrorIs(t, err, ErrHelperTorrentFiles)
	require.Zero(t, stub.Calls("torrents/delete"), "a refused delete removes no torrent of the batch")
	require.FileExists(t, filepath.Join(root, hashA, "Show.S01E01.mkv"))

	require.NoError(t, sm.DeleteTorrents(t.Context(), 1, []string{hashA}, false, user), "qui removes a helper and keeps its files")
	require.FileExists(t, filepath.Join(root, hashA, "Show.S01E01.mkv"))
}

// Only the registry makes a helper torrent: the tag of a torrent proves nothing.
func TestATaggedTorrentWithoutARegistryRowIsNoHelper(t *testing.T) {
	t.Parallel()
	sm, _, root := newHelperRig(t)

	resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: 1})
	require.NoError(t, err)
	badges := map[string]string{}
	for _, row := range resp.Torrents {
		badges[row.Hash] = row.QuiHelper
	}
	require.Equal(t, map[string]string{hashA: "transfer", hashB: ""}, badges)

	sm.instanceName = func(context.Context, int) (string, error) { return "seedbox", nil }
	views, err := sm.GetCachedInstanceTorrents(t.Context(), 1)
	require.NoError(t, err)
	clear(badges)
	for _, view := range views {
		badges[view.Hash] = view.QuiHelper
	}
	require.Equal(t, map[string]string{hashA: "transfer", hashB: ""}, badges)

	require.NoError(t, sm.DeleteTorrents(t.Context(), 1, []string{hashB}, true, DeleteActor{Kind: DeleteActorUser}))
	require.NoFileExists(t, filepath.Join(root, hashB, "Show.S01E01.mkv"))
}
