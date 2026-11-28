// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

type fakeHelpers map[string]string

func (f fakeHelpers) HelperKinds(instanceID int) map[string]string {
	if instanceID != 1 || len(f) == 0 {
		return nil
	}
	return f
}

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

func TestATaggedTorrentWithoutARegistryRowIsNoHelper(t *testing.T) {
	t.Parallel()
	sm, _, root := newHelperRig(t)

	resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: 1, WithHelperTorrents: true})
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
	require.Equal(t, map[string]string{hashB: ""}, badges)

	require.NoError(t, sm.DeleteTorrents(t.Context(), 1, []string{hashB}, true, DeleteActor{Kind: DeleteActorUser}))
	require.NoFileExists(t, filepath.Join(root, hashB, "Show.S01E01.mkv"))
}

func TestDeleteWithFilesRefusesAHybridHelperTorrentByOtherHash(t *testing.T) {
	t.Parallel()
	const (
		hybridV1 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
		hybridV2 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1b2b2b2b2b2b2b2b2b2b2b2b2"
	)
	root := t.TempDir()
	stub := qbtstub.New(t, root)
	stub.Add(qbtstub.Torrent{
		Hash: hybridV1, InfohashV1: hybridV1, InfohashV2: hybridV2,
		Name: "Show.S01E01-GRP", SavePath: filepath.Join(root, hybridV1),
		Layout: qbtstub.NoRootFolder, Files: []string{"Show.S01E01.mkv"},
	})
	client, err := NewClientWithTimeout(1, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))

	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)
	sm.SetHelperTorrents(fakeHelpers{hybridV1: "transfer"})

	err = sm.DeleteTorrents(t.Context(), 1, []string{hybridV2}, true, DeleteActor{Kind: DeleteActorUser})
	require.ErrorIs(t, err, ErrHelperTorrentFiles)
	require.Zero(t, stub.Calls("torrents/delete"))
	require.FileExists(t, filepath.Join(root, hybridV1, "Show.S01E01.mkv"))

	sm.SetHelperTorrents(fakeHelpers{hybridV2: "transfer"})
	require.ErrorIs(t, sm.RefuseHelperFiles(t.Context(), 1, []string{"all"}), ErrHelperTorrentFiles)

	// Unsynced client forces GetTorrentRefsFresh and still refuses by alternate hash.
	unsynced, err := NewClientWithTimeout(1, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(unsynced.optimisticUpdates.Close)
	smFresh := NewSyncManager(&ClientPool{clients: map[int]*Client{1: unsynced}, creationLocks: map[int]*sync.Mutex{}}, nil)
	smFresh.SetHelperTorrents(fakeHelpers{hybridV1: "transfer"})
	require.ErrorIs(t, smFresh.RefuseHelperFiles(t.Context(), 1, []string{hybridV2}), ErrHelperTorrentFiles)

	// A failing fresh sync fails closed when helpers exist on the instance.
	var failSync atomic.Bool
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.4"))
		case "/api/v2/sync/maindata":
			if failSync.Load() {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true}`))
		default:
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	t.Cleanup(badSrv.Close)
	down, err := NewClientWithTimeout(1, badSrv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(down.optimisticUpdates.Close)
	require.NoError(t, down.SyncOwner().Sync(t.Context()))
	failSync.Store(true)
	smDown := NewSyncManager(&ClientPool{clients: map[int]*Client{1: down}, creationLocks: map[int]*sync.Mutex{}}, nil)
	smDown.SetHelperTorrents(fakeHelpers{hybridV1: "transfer"})
	require.Error(t, smDown.RefuseHelperFiles(t.Context(), 1, []string{hybridV2}))
}
