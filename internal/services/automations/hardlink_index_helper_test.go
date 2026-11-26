// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type helperOnInstance struct {
	instanceID int
	hash       string
}

func (h helperOnInstance) HelperKinds(instanceID int) map[string]string {
	if instanceID != h.instanceID {
		return nil
	}
	return map[string]string{h.hash: "share"}
}

type helperLinkRig struct {
	service  *Service
	sourceID int
	source   qbt.Torrent
}

func newHelperLinkRig(t *testing.T, routed bool) *helperLinkRig {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	links := filepath.Join(dir, "links")
	sourceFile := filepath.Join(dir, "data", "movie.mkv")
	createFile(t, sourceFile)
	require.NoError(t, os.MkdirAll(filepath.Join(links, "Movie"), 0o700))
	require.NoError(t, os.Link(sourceFile, filepath.Join(links, "Movie", "movie.mkv")))

	files := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"movie.mkv","size":1,"priority":1}]`))
	}
	source := singleFileTorrent(routedSourceHash, filepath.Join(dir, "data"), "movie.mkv")
	helper := singleFileTorrent(routedHash, filepath.Join(links, "Movie"), "movie.mkv")
	sourceQbit := newFakeQbit(t, []qbt.Torrent{source}, files)
	otherQbit := newFakeQbit(t, []qbt.Torrent{helper}, files)

	db := testdb.NewMigratedSQLite(t, "hardlink-helper")
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	sourceInstance, err := instances.Create(ctx, "lib", sourceQbit.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	otherInstance, err := instances.Create(ctx, "seeds", otherQbit.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)

	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	syncManager := qbittorrent.NewSyncManager(clients, nil)
	syncManager.SetFilesManager(filesmanager.NewService(db))
	syncManager.SetHelperTorrents(helperOnInstance{instanceID: otherInstance.ID, hash: routedHash})
	backends := fsops.NewPool(instances, localbackend.NewBackend())

	forget := func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, sourceInstance.ID)
		globalHardlinkIndexCache.mu.Unlock()
	}
	forget()
	t.Cleanup(forget)
	service := NewService(Config{}, instances, nil, nil, nil, syncManager, nil, nil, nil, backends)
	if routed {
		routeStore := models.NewCrossSeedRouteStore(db)
		require.NoError(t, routeStore.Upsert(ctx, &models.CrossSeedRoute{
			SourceInstanceID: sourceInstance.ID, CrossSeedInstanceID: otherInstance.ID,
			Rows: []models.CrossSeedRouteRow{{BaseDir: links}},
		}))
		service.crossSeedRoutes = crossseedroute.NewService(routeStore, instances, clients, backends)
	}
	return &helperLinkRig{service: service, sourceID: sourceInstance.ID, source: source}
}

func TestHardlinkIndexCountsARoutedHelperTorrentAsATorrent(t *testing.T) {
	rig := newHelperLinkRig(t, true)

	index := rig.service.GetHardlinkIndex(t.Context(), rig.sourceID, torrentRefs([]qbt.Torrent{rig.source}))
	require.Equal(t, HardlinkScopeTorrentsOnly, index.ScopeByHash[routedSourceHash], "the routed Share torrent still holds the hardlink")
}

func TestCrossScopeFindsAHelperTorrentOnAnotherInstance(t *testing.T) {
	rig := newHelperLinkRig(t, false)

	index := rig.service.GetHardlinkIndex(t.Context(), rig.sourceID, torrentRefs([]qbt.Torrent{rig.source}))
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.ScopeByHash[routedSourceHash])
	index.crossScopeMu.Lock()
	rig.service.augmentCrossInstanceScope(t.Context(), rig.sourceID, index)
	index.crossScopeMu.Unlock()
	require.Equal(t, HardlinkScopeTorrentsOnly, index.CrossScopeByHash[routedSourceHash], "the Transfer torrent on the other instance holds the hardlink")
}
