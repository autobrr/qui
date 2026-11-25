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

func TestHardlinkIndexCountsARoutedHelperTorrentAsATorrent(t *testing.T) {
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
	crossQbit := newFakeQbit(t, []qbt.Torrent{helper}, files)

	db := testdb.NewMigratedSQLite(t, "hardlink-routed-helper")
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	sourceInstance, err := instances.Create(ctx, "lib", sourceQbit.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	crossInstance, err := instances.Create(ctx, "seeds", crossQbit.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	routeStore := models.NewCrossSeedRouteStore(db)
	require.NoError(t, routeStore.Upsert(ctx, &models.CrossSeedRoute{
		SourceInstanceID: sourceInstance.ID, CrossSeedInstanceID: crossInstance.ID,
		Rows: []models.CrossSeedRouteRow{{BaseDir: links}},
	}))

	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	syncManager := qbittorrent.NewSyncManager(clients, nil)
	syncManager.SetFilesManager(filesmanager.NewService(db))
	syncManager.SetHelperTorrents(helperOnInstance{instanceID: crossInstance.ID, hash: routedHash})
	backends := fsops.NewPool(instances, localbackend.NewBackend())

	forget := func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, sourceInstance.ID)
		globalHardlinkIndexCache.mu.Unlock()
	}
	forget()
	t.Cleanup(forget)
	service := NewService(Config{}, instances, nil, nil, nil, syncManager, nil, nil, nil, backends)
	service.crossSeedRoutes = crossseedroute.NewService(routeStore, instances, clients, backends)

	index := service.GetHardlinkIndex(ctx, sourceInstance.ID, torrentRefs([]qbt.Torrent{source}))
	require.Equal(t, HardlinkScopeTorrentsOnly, index.ScopeByHash[routedSourceHash], "the routed Share torrent still holds the hardlink")
}
