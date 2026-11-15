// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// routeFixture is a Source instance (id 1) in local mode that routes its
// base folder baseDir to /seeds/links on a Cross-seed instance (id 2). The
// Cross-seed instance has no Local Filesystem Access, so the peer rule never
// merges it.
type routeFixture struct {
	svc       *Service
	store     *models.OrphanScanStore
	instances *models.InstanceStore
	source    *models.Instance
	crossSeed *models.Instance
	root      string
	baseDir   string
	// torrents and files are what the Cross-seed instance reports.
	torrents []qbt.Torrent
	files    map[string]qbt.TorrentFiles
}

// newRouteFixture starts the Cross-seed instance's fake qBittorrent, or one
// that answers 503 to every call when offline is true.
func newRouteFixture(t *testing.T, offline bool) *routeFixture {
	t.Helper()
	ctx := t.Context()
	root := t.TempDir()
	baseDir := filepath.Join(root, "links")

	qbit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case offline:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case r.URL.Path == "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case r.URL.Path == "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.2"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(qbit.Close)

	db := testdb.NewMigratedSQLite(t, "orphanscan-cross-seed-route")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	source, err := instances.Create(ctx, "lib", "http://127.0.0.1:1", "user", "pass", nil, nil, false, new(true))
	require.NoError(t, err)
	source, err = instances.Update(ctx, source.ID, source.Name, source.Host, source.Username, "", nil, nil,
		&models.InstanceUpdateParams{UseHardlinks: new(true), HardlinkBaseDir: new(baseDir)})
	require.NoError(t, err)
	crossSeed, err := instances.Create(ctx, "seeds", qbit.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	routeStore := models.NewCrossSeedRouteStore(db)
	require.NoError(t, routeStore.Upsert(ctx, &models.CrossSeedRoute{
		SourceInstanceID:    source.ID,
		CrossSeedInstanceID: crossSeed.ID,
		Rows:                []models.CrossSeedRouteRow{{BaseDir: fsops.CleanPath(fsops.HostPaths, baseDir), MappedPath: "/seeds/links"}},
	}))
	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })

	store := models.NewOrphanScanStore(db)
	f := &routeFixture{
		svc:       NewService(DefaultConfig(), nil, store, nil, nil, fsops.NewPool(instances, local.NewBackend())),
		store:     store,
		instances: instances,
		source:    source,
		crossSeed: crossSeed,
		root:      root,
		baseDir:   baseDir,
		files:     map[string]qbt.TorrentFiles{},
	}
	f.svc.SetCrossSeedRoutes(crossseedroute.NewService(routeStore, instances, clients, fsops.NewPool(instances, local.NewBackend())))

	sync := stubSync(f.svc)
	sync.listInstances = func(ctx context.Context) ([]*models.Instance, error) { return instances.List(ctx) }
	sync.getClient = func(context.Context, int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-10 * time.Second)}, nil
	}
	ownRoot := filepath.Join(root, "library")
	sync.getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
		if instanceID == crossSeed.ID {
			return f.torrents, nil
		}
		return []qbt.Torrent{{Hash: "own", SavePath: ownRoot, State: qbt.TorrentStatePausedUp}}, nil
	}
	sync.getTorrentFilesBatch = func(_ context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
		if instanceID != crossSeed.ID {
			return map[string]qbt.TorrentFiles{"own": {{Name: "own.mkv", Size: 1}}}, nil
		}
		files := map[string]qbt.TorrentFiles{}
		for _, hash := range hashes {
			if list, ok := f.files[hash]; ok {
				files[hash] = list
			}
		}
		return files, nil
	}
	return f
}

// TestBuildFileMap_ProtectsRoutedFilesAtSourcePaths covers a Cross-seed
// instance that reports its torrents at mapped paths. A Pending file is no
// hardlink of a Source instance file, so only the route keeps it.
func TestBuildFileMap_ProtectsRoutedFilesAtSourcePaths(t *testing.T) {
	t.Parallel()

	f := newRouteFixture(t, false)
	f.torrents = []qbt.Torrent{
		{Hash: "pending", SavePath: "/seeds/links/tracker", ContentPath: "/seeds/links/tracker/Show", State: qbt.TorrentStateDownloading},
		{Hash: "checking", SavePath: "/seeds/links/checking", State: qbt.TorrentStateCheckingUp},
		{Hash: "magnet", SavePath: "/seeds/links/magnet", HasMetadata: new(false), State: qbt.TorrentStateMetaDl},
		// Outside the route rows: none of these reach the Source instance.
		{Hash: "elsewhere", SavePath: "/seeds/other", State: qbt.TorrentStatePausedUp},
		{Hash: "elsewherechecking", SavePath: "/seeds/other/checking", State: qbt.TorrentStateCheckingUp},
	}
	f.files["pending"] = qbt.TorrentFiles{{Name: "Show/e01.mkv", Size: 1}}
	f.files["checking"] = qbt.TorrentFiles{}
	f.files["magnet"] = qbt.TorrentFiles{}
	f.files["elsewhere"] = qbt.TorrentFiles{{Name: "x.mkv", Size: 1}}

	result, err := f.svc.buildFileMap(t.Context(), f.source, newTestBackend(), scanScope{TorrentPaths: true})
	require.NoError(t, err)

	pending := filepath.Join(f.baseDir, "tracker", "Show", "e01.mkv")
	require.True(t, newTestProtected(t, fsops.HostPaths, nil, result.fileMap).torrentFile(pending), "the routed Pending file must be protected at its Source instance path")
	require.Contains(t, result.torrentPaths, pending)
	require.Equal(t, 2, result.fileMap.Len(), "only the own file and the routed file are protected")
	require.Equal(t, []string{filepath.Join(f.baseDir, "checking")}, result.skippedRoots)
	require.Equal(t, []string{filepath.Join(f.baseDir, "magnet")}, result.metadataRoots)
	require.Equal(t, []string{filepath.Join(f.root, "library")}, result.scanRoots, "the route adds no scan roots")
}

// A scan that cannot read the Cross-seed instance does not know which files
// it uses, so it fails, also for an instance turned off in qui (ADR 0035).
func TestBuildFileMap_FailsWhenCrossSeedInstanceUnavailable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		offline  bool
		disabled bool
	}{
		{name: "offline", offline: true},
		{name: "turned off in qui", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newRouteFixture(t, tc.offline)
			if tc.disabled {
				_, err := f.instances.SetActiveState(t.Context(), f.crossSeed.ID, false)
				require.NoError(t, err)
			}

			_, err := f.svc.buildFileMap(t.Context(), f.source, newTestBackend(), scanScope{})
			require.ErrorIs(t, err, crossseedroute.ErrUnavailable)
			require.ErrorContains(t, err, "Cross-seed instance unavailable: seeds")
			require.ErrorContains(t, err, "Enable it in qui or clear the cross-seed route")
		})
	}
}

// The delete run builds the file map again, so a Cross-seed instance that
// goes away after the preview fails the delete before it removes a file.
func TestExecuteDeletion_FailsWhenCrossSeedInstanceBecomesUnavailable(t *testing.T) {
	t.Parallel()

	f := newRouteFixture(t, false)
	stray := filepath.Join(f.root, "library", "stray.txt")
	writeFile(t, filepath.Join(f.root, "library", "own.mkv"))
	writeFile(t, stray)
	_, err := f.store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          f.source.ID,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupMaxFiles: 100,
	})
	require.NoError(t, err)
	stubSync(f.svc).folders(qbittorrent.InstanceFolders{})

	runID, err := f.store.CreateRunIfNoActive(t.Context(), f.source.ID, "manual")
	require.NoError(t, err)
	f.svc.executeScan(t.Context(), f.source.ID, runID)
	run, err := f.store.GetRun(t.Context(), runID)
	require.NoError(t, err)
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	files, err := f.store.GetFilesForDeletion(t.Context(), runID)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, stray, files[0].FilePath)

	_, err = f.instances.SetActiveState(t.Context(), f.crossSeed.ID, false)
	require.NoError(t, err)
	f.svc.executeDeletion(t.Context(), f.source.ID, runID)

	run, err = f.store.GetRun(t.Context(), runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.ErrorMessage, "Cross-seed instance unavailable")
	require.FileExists(t, stray)
}

// An instance without a route scans as before: the Cross-seed instance's
// torrents add nothing, and its outage does not fail the scan.
func TestBuildFileMap_NoRouteKeepsTheScan(t *testing.T) {
	t.Parallel()

	f := newRouteFixture(t, true)
	require.NoError(t, f.svc.crossSeedRoutes.Delete(t.Context(), f.source.ID))
	f.torrents = []qbt.Torrent{{Hash: "pending", SavePath: "/seeds/links/tracker", State: qbt.TorrentStateDownloading}}
	f.files["pending"] = qbt.TorrentFiles{{Name: "e01.mkv", Size: 1}}

	result, err := f.svc.buildFileMap(t.Context(), f.source, newTestBackend(), scanScope{})
	require.NoError(t, err)
	require.Equal(t, 1, result.fileMap.Len())
	require.Empty(t, result.skippedRoots)
	require.Equal(t, []string{filepath.Join(f.root, "library")}, result.scanRoots)
}
