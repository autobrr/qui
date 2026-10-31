// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	routedName    = "Movie.2024.1080p.BluRay.x264-GRP"
	routedMatched = "3333333333333333333333333333333333333333"
	// routedMapped is where the Cross-seed instance sees the links folder.
	routedMapped = "/seeds/links"
)

// routedSync is the qBittorrent fake of a routed add. It records adds and
// created categories per instance, and reports the torrent on the instances
// in existing.
type routedSync struct {
	*rootlessSavePathSyncManager
	adds       map[int][]map[string]string
	categories map[int]map[string]string
	existing   map[int]bool
	addErr     error
	// The Cross-seed instance lists links at routedMapped. Each listing goes
	// in listed. listErr answers every listing, and the first hidden listings
	// miss every entry.
	crossSeedID int
	links       string
	listed      []string
	listErr     error
	hidden      int
}

func (f *routedSync) GetDirectoryContentCtx(_ context.Context, instanceID int, dir string, _ qbt.DirectoryContentMode, _ bool) (any, error) {
	f.listed = append(f.listed, dir)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.hidden > 0 {
		f.hidden--
		return []string{}, nil
	}
	rel, ok := strings.CutPrefix(dir, routedMapped)
	if !ok || instanceID != f.crossSeedID {
		return nil, fmt.Errorf("%w: 404", qbt.ErrUnexpectedStatus)
	}
	entries, err := os.ReadDir(filepath.Join(f.links, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("%w: 404", qbt.ErrUnexpectedStatus)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = dir + "/" + entry.Name()
	}
	return names, nil
}

func (f *routedSync) AddTorrent(_ context.Context, instanceID int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	f.adds[instanceID] = append(f.adds[instanceID], maps.Clone(options))
	return &qbt.TorrentAddResponse{}, nil
}

func (f *routedSync) HasTorrentByAnyHash(_ context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	if f.existing[instanceID] {
		return &qbt.Torrent{Hash: hashes[0], Name: routedName}, true, nil
	}
	return nil, false, nil
}

func (f *routedSync) CreateCategory(_ context.Context, instanceID int, name, savePath string) error {
	if f.categories[instanceID] == nil {
		f.categories[instanceID] = map[string]string{}
	}
	f.categories[instanceID][name] = savePath
	return nil
}

// routedRig is a Source instance "lib" in hardlink mode that routes to the
// Cross-seed instance "seeds", both on local fake qBittorrents. lib holds
// the matched torrent in downloads, and its base folder is links.
type routedRig struct {
	t *testing.T
	// ctx is the context of each add. It is t.Context() unless a test sets it.
	ctx       context.Context
	db        *database.DB
	svc       *Service
	sync      *routedSync
	settler   *Settler
	instances *models.InstanceStore
	routes    *models.CrossSeedRouteStore
	source    *models.Instance
	crossSeed *models.Instance
	downloads string
	links     string
	files     qbt.TorrentFiles
}

// newRoutedRig builds the rig. crossSeedDown puts the Cross-seed instance on
// a qBittorrent that answers 503 to everything.
func newRoutedRig(t *testing.T, crossSeedDown bool) *routedRig {
	t.Helper()
	ctx := t.Context()
	tmp := t.TempDir()
	r := &routedRig{t: t, ctx: ctx, downloads: filepath.Join(tmp, "downloads"), links: filepath.Join(tmp, "links")}
	r.db = testdb.NewMigratedSQLite(t, "routed-add")
	var err error
	r.instances, err = models.NewInstanceStore(r.db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	r.source, err = r.instances.Create(ctx, "lib", qbtstub.New(t, tmp).URL, "user", "pass", nil, nil, false, new(true))
	require.NoError(t, err)
	r.updateSource(&models.InstanceUpdateParams{UseHardlinks: new(true), HardlinkBaseDir: new(r.links), FallbackToRegularMode: new(true)})
	host := qbtstub.New(t, tmp).URL
	if crossSeedDown {
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(down.Close)
		host = down.URL
	}
	r.crossSeed, err = r.instances.Create(ctx, "seeds", host, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	r.routes = models.NewCrossSeedRouteStore(r.db)
	r.saveRoute(routedMapped)
	clients, err := qbittorrent.NewClientPool(r.instances, models.NewInstanceErrorStore(r.db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	routes := crossseedroute.NewService(r.routes, r.instances, clients, fsops.NewPool(r.instances, local.NewBackend()))

	r.files = qbt.TorrentFiles{{Name: routedName + "/" + routedName + ".mkv", Size: 100}}
	r.sync = &routedSync{
		rootlessSavePathSyncManager: &rootlessSavePathSyncManager{
			files: map[string]qbt.TorrentFiles{routedMatched: r.files},
			props: map[string]*qbt.TorrentProperties{routedMatched: {SavePath: r.downloads}},
		},
		adds:        map[int][]map[string]string{},
		categories:  map[int]map[string]string{},
		existing:    map[int]bool{},
		crossSeedID: r.crossSeed.ID,
		links:       r.links,
	}
	r.writeDownloads(r.files)

	r.settler = NewSettler(&settleFake{}, nil)
	r.svc = &Service{
		instanceStore:    r.instances,
		syncManager:      r.sync,
		blocklistStore:   models.NewCrossSeedBlocklistStore(r.db),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		releaseCache:     NewReleaseCache(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}
	r.svc.aligner = testAligner(r.sync)
	r.svc.SetBackendPool(fsops.NewPool(r.instances, local.NewBackend()))
	r.svc.SetCrossSeedRoutes(routes)
	r.svc.visibleRetryDelay = time.Nanosecond
	r.svc.adder = NewAdder(r.sync, r.svc.blocklistStore, r.settler, func(context.Context) int64 { return adderBudget }, r.svc.RunExternalProgram)
	r.svc.adder.SetCrossSeedRoutes(routes)
	return r
}

func (r *routedRig) updateSource(params *models.InstanceUpdateParams) {
	r.t.Helper()
	source, err := r.instances.Update(r.t.Context(), r.source.ID, r.source.Name, r.source.Host, r.source.Username, "", nil, nil, params)
	require.NoError(r.t, err)
	r.source = source
}

// saveRoute stores the route with mapped as the path of the links row,
// without the route rules.
func (r *routedRig) saveRoute(mapped string) {
	r.t.Helper()
	require.NoError(r.t, r.routes.Upsert(r.t.Context(), &models.CrossSeedRoute{
		SourceInstanceID:    r.source.ID,
		CrossSeedInstanceID: r.crossSeed.ID,
		Rows:                []models.CrossSeedRouteRow{{BaseDir: fsops.CleanPath(fsops.HostPaths, r.links), MappedPath: mapped}},
	}))
}

// writeDownloads writes the files of the matched torrent into downloads.
func (r *routedRig) writeDownloads(files qbt.TorrentFiles) {
	r.t.Helper()
	for _, file := range files {
		p := filepath.Join(r.downloads, filepath.FromSlash(file.Name))
		require.NoError(r.t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(r.t, os.WriteFile(p, make([]byte, file.Size), 0o600))
	}
}

func (r *routedRig) candidate() CrossSeedCandidate {
	return CrossSeedCandidate{
		InstanceID:   r.source.ID,
		InstanceName: r.source.Name,
		Torrents: []qbt.Torrent{{
			Hash: routedMatched, Name: routedName, Progress: 1, Size: 100, Category: "movies",
			SavePath: r.downloads, ContentPath: filepath.Join(r.downloads, routedName),
		}},
		MatchType: "exact",
	}
}

// add runs the per-candidate step for the matched torrent with sourceFiles,
// or the files of the matched torrent when sourceFiles is nil.
func (r *routedRig) add(req *CrossSeedRequest, addedTo map[int]struct{}, sourceFiles qbt.TorrentFiles) InstanceCrossSeedResult {
	r.t.Helper()
	if sourceFiles == nil {
		sourceFiles = r.files
	}
	if req == nil {
		req = &CrossSeedRequest{}
	}
	return r.svc.processCrossSeedCandidate(r.ctx, r.candidate(), []byte("torrent"), adderV1, "", routedName,
		req, r.svc.releaseCache.Parse(routedName), sourceFiles, nil, addedTo)
}

// linked reports whether the tree holds a hardlink of the downloaded file name.
func (r *routedRig) linked(treeRoot, name string) bool {
	r.t.Helper()
	got, err := os.Stat(filepath.Join(treeRoot, filepath.FromSlash(name)))
	if err != nil {
		return false
	}
	want, err := os.Stat(filepath.Join(r.downloads, filepath.FromSlash(name)))
	require.NoError(r.t, err)
	return os.SameFile(got, want)
}

// requireNoLinkTree fails when links holds a file.
func (r *routedRig) requireNoLinkTree() {
	r.t.Helper()
	_ = filepath.WalkDir(r.links, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			r.t.Errorf("link tree file left behind: %s", p)
		}
		return nil
	})
}

func (r *routedRig) requireRouted(result InstanceCrossSeedResult) {
	r.t.Helper()
	require.Equal(r.t, r.source.ID, result.InstanceID, "the result keeps the instance that holds the match")
	require.Equal(r.t, r.crossSeed.ID, result.CrossSeedInstanceID)
	require.Equal(r.t, "seeds", result.CrossSeedInstanceName)
	require.Empty(r.t, r.sync.adds[r.source.ID], "the Source instance never gets a routed add")
}

func TestRoutedAddLandsOnTheCrossSeedInstance(t *testing.T) {
	tests := []struct {
		name         string
		preset       string
		wantSavePath string
		wantTree     string
	}{
		{name: "flat", preset: "flat", wantSavePath: routedMapped, wantTree: ""},
		{name: "by-instance uses the Cross-seed instance name", preset: "by-instance", wantSavePath: routedMapped + "/seeds", wantTree: "seeds"},
		{name: "by-tracker", preset: "by-tracker", wantSavePath: routedMapped + "/Tracker", wantTree: "Tracker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			r.updateSource(&models.InstanceUpdateParams{HardlinkDirPreset: new(tt.preset)})

			result := r.add(&CrossSeedRequest{IndexerName: "Tracker"}, nil, nil)

			require.True(t, result.Success, result.Message)
			require.Equal(t, "added_hardlink", result.Status)
			r.requireRouted(result)
			adds := r.sync.adds[r.crossSeed.ID]
			require.Len(t, adds, 1)
			assert.Equal(t, tt.wantSavePath, adds[0]["savepath"])
			assert.Equal(t, "false", adds[0]["autoTMM"])
			assert.Equal(t, "movies.cross", adds[0]["category"])
			assert.Equal(t, map[string]string{"movies.cross": tt.wantSavePath}, r.sync.categories[r.crossSeed.ID],
				"the category is created on the Cross-seed instance with the mapped save path")
			assert.True(t, r.linked(filepath.Join(r.links, tt.wantTree), r.files[0].Name), "the tree is on the Source instance's disk")
		})
	}
}

// A user with no saved route sees no change: the add goes to the instance
// that holds the match, and the result names no Cross-seed instance.
func TestRoutedAddWithoutARouteAddsToTheSourceInstance(t *testing.T) {
	r := newRoutedRig(t, false)
	require.NoError(t, r.routes.Delete(t.Context(), r.source.ID))

	result := r.add(nil, nil, nil)

	require.True(t, result.Success, result.Message)
	require.Len(t, r.sync.adds[r.source.ID], 1)
	assert.Equal(t, r.links, r.sync.adds[r.source.ID][0]["savepath"])
	assert.Empty(t, r.sync.adds[r.crossSeed.ID])
	assert.Zero(t, result.CrossSeedInstanceID)
	assert.Empty(t, result.CrossSeedInstanceName)
}

func TestRoutedAddMapsSubfolders(t *testing.T) {
	tests := []struct {
		name       string
		files      qbt.TorrentFiles
		wantListed string
	}{
		{name: "disc layout", files: qbt.TorrentFiles{
			{Name: routedName + "/BDMV/STREAM/00000.m2ts", Size: 5},
			{Name: routedName + "/BDMV/index.bdmv", Size: 3},
		}, wantListed: routedMapped + "/" + routedName + "/BDMV/STREAM"},
		{name: "pack with subfolders", files: qbt.TorrentFiles{
			{Name: routedName + "/Disc 1/movie.part1.mkv", Size: 5},
			{Name: routedName + "/Disc 2/movie.part2.mkv", Size: 4},
		}, wantListed: routedMapped + "/" + routedName + "/Disc 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			r.files = tt.files
			r.sync.files[routedMatched] = tt.files
			r.writeDownloads(tt.files)

			result := r.add(nil, nil, tt.files)

			require.True(t, result.Success, result.Message)
			r.requireRouted(result)
			require.Len(t, r.sync.adds[r.crossSeed.ID], 1)
			assert.Equal(t, routedMapped, r.sync.adds[r.crossSeed.ID][0]["savepath"])
			assert.Equal(t, []string{tt.wantListed}, r.sync.listed, "the check lists the folder of a linked file")
			for _, file := range tt.files {
				assert.True(t, r.linked(r.links, file.Name), file.Name)
			}
		})
	}
}

// Each closed fallback fails with its own status. The Source instance has
// Fallback to regular mode on, so a regular add would land on it.
func TestRoutedAddNeverFallsBackToARegularAdd(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(r *routedRig)
		files      qbt.TorrentFiles
		req        *CrossSeedRequest
		wantStatus string
		wantMsg    string
	}{
		{
			// The route service reads a row that is still in link mode, and the
			// capability read reads the row that left it.
			name: "Source instance left link mode",
			setup: func(r *routedRig) {
				r.svc.SetBackendPool(fsops.NewPool(leftLinkMode{r.instances}, local.NewBackend()))
			},
			wantStatus: crossSeedUnavailableStatus,
			wantMsg:    "Cross-seed instance unavailable: lib must use hardlink or reflink mode to route its cross-seeds",
		},
		{
			name: "Source instance left local mode",
			setup: func(r *routedRig) {
				r.svc.SetBackendPool(fsops.NewPoolWithRemote(leftLocalMode{r.instances}, local.NewBackend(), func(*models.Instance) fsops.Backend {
					return local.NewBackend()
				}))
			},
			wantStatus: crossSeedUnavailableStatus,
			wantMsg:    "Cross-seed instance unavailable: lib needs Local Filesystem Access to route its cross-seeds",
		},
		{
			name: "instance read fails",
			setup: func(r *routedRig) {
				r.svc.SetBackendPool(fsops.NewPool(failingInstances{}, local.NewBackend()))
			},
			wantStatus: "error",
			wantMsg:    "Failed to read the Source instance: load instance 1: boom",
		},
		{
			name: "no base folder matches",
			setup: func(r *routedRig) {
				// The base folder cannot be created under a file.
				require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(r.links), "blocker"), nil, 0o600))
				r.links = filepath.Join(filepath.Dir(r.links), "blocker", "links")
				r.updateSource(&models.InstanceUpdateParams{HardlinkBaseDir: new(r.links)})
				r.saveRoute(routedMapped)
			},
			wantStatus: "hardlink_error",
			wantMsg:    "No suitable base directory",
		},
		{
			name: "link error",
			setup: func(r *routedRig) {
				r.svc.SetBackendPool(fsops.NewPool(r.instances, crossDeviceBackend{local.NewBackend(), []string{r.links}}))
			},
			wantStatus: "hardlink_error",
			wantMsg:    "Failed to create hardlink tree",
		},
		{
			name: "unsupported filesystem",
			setup: func(r *routedRig) {
				r.updateSource(&models.InstanceUpdateParams{UseHardlinks: new(false), UseReflinks: new(true)})
				r.svc.reflinkMaterializer = func(context.Context, string, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
					return nil, &linkUnsupportedError{reason: "no reflinks here"}
				}
			},
			wantStatus: "reflink_error",
			wantMsg:    "reflink not supported: no reflinks here",
		},
		{
			name: "existing target",
			setup: func(r *routedRig) {
				// Another torrent holds the root folder, and a stranger's file
				// sits in the hashed isolation folder.
				require.NoError(t, os.MkdirAll(filepath.Join(r.links, routedName), 0o755))
				stranger := filepath.Join(r.links, pathutil.HashedIsolationFolder(adderV1, routedName), filepath.FromSlash(r.files[0].Name))
				require.NoError(t, os.MkdirAll(filepath.Dir(stranger), 0o755))
				require.NoError(t, os.WriteFile(stranger, []byte("stranger"), 0o600))
			},
			wantStatus: "hardlink_error",
			wantMsg:    "Failed to create hardlink tree",
		},
		{
			name:       "failed add on the Cross-seed instance",
			setup:      func(r *routedRig) { r.sync.addErr = errors.New("boom") },
			wantStatus: "hardlink_error",
			wantMsg:    "Failed to add torrent: boom",
		},
		{
			name:       "unvalidated Manual match",
			files:      qbt.TorrentFiles{{Name: "Other/other.mkv", Size: 7}},
			req:        &CrossSeedRequest{ManualTargetHash: routedMatched},
			wantStatus: "error",
			wantMsg:    "an unvalidated Manual match cannot use it",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			if tt.setup != nil {
				tt.setup(r)
			}

			result := r.add(tt.req, nil, tt.files)

			require.False(t, result.Success)
			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Contains(t, result.Message, tt.wantMsg)
			r.requireRouted(result)
			if tt.name != "failed add on the Cross-seed instance" {
				assert.Empty(t, r.sync.adds[r.crossSeed.ID])
			}
			if tt.name != "existing target" {
				r.requireNoLinkTree()
			}
		})
	}
}

// leftLinkMode reads the Source instance as it is after it left link mode.
type leftLinkMode struct{ *models.InstanceStore }

func (s leftLinkMode) Get(ctx context.Context, id int) (*models.Instance, error) {
	instance, err := s.InstanceStore.Get(ctx, id)
	if err == nil && instance.Name == "lib" {
		instance.UseHardlinks = false
	}
	return instance, err
}

// leftLocalMode reads the Source instance as a remote instance.
type leftLocalMode struct{ *models.InstanceStore }

func (s leftLocalMode) Get(ctx context.Context, id int) (*models.Instance, error) {
	instance, err := s.InstanceStore.Get(ctx, id)
	if err == nil && instance.Name == "lib" {
		instance.HasLocalFilesystemAccess = false
		instance.SSHHost, instance.SSHKeyEncrypted, instance.SSHHostKeyEncrypted = "box.example.invalid", "enc-key", "enc-hostkey"
	}
	return instance, err
}

type failingInstances struct{}

func (failingInstances) Get(context.Context, int) (*models.Instance, error) {
	return nil, errors.New("boom")
}

func TestRoutedAddSkipsADuplicate(t *testing.T) {
	tests := []struct {
		name        string
		existingOn  string
		addedInRun  bool
		wantMessage string
	}{
		{name: "hash on the Cross-seed instance", existingOn: "seeds", wantMessage: "Torrent already exists in Cross-seed instance seeds"},
		{name: "hash on the Source instance", existingOn: "lib", wantMessage: "Torrent already exists in Source instance lib"},
		{name: "added to the Cross-seed instance earlier in the run", addedInRun: true, wantMessage: "Torrent already added to seeds in this run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			switch tt.existingOn {
			case "seeds":
				r.sync.existing[r.crossSeed.ID] = true
			case "lib":
				r.sync.existing[r.source.ID] = true
			}
			var addedTo map[int]struct{}
			if tt.addedInRun {
				addedTo = map[int]struct{}{r.crossSeed.ID: {}}
			}

			result := r.add(nil, addedTo, nil)

			assert.Equal(t, "exists", result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
			r.requireRouted(result)
			assert.Empty(t, r.sync.adds[r.crossSeed.ID])
			r.requireNoLinkTree()
		})
	}
}

func TestRoutedAddAppliesOnlyTheCrossSeedInstanceBlocklist(t *testing.T) {
	for _, blockedOn := range []string{"seeds", "lib"} {
		t.Run(blockedOn, func(t *testing.T) {
			r := newRoutedRig(t, false)
			instanceID := r.crossSeed.ID
			if blockedOn == "lib" {
				instanceID = r.source.ID
			}
			_, err := r.svc.blocklistStore.Upsert(t.Context(), &models.CrossSeedBlocklistEntry{InstanceID: instanceID, InfoHash: adderV1})
			require.NoError(t, err)

			result := r.add(nil, nil, nil)

			r.requireRouted(result)
			if blockedOn == "seeds" {
				assert.Equal(t, "blocked", result.Status)
				assert.Empty(t, r.sync.adds[r.crossSeed.ID])
				return
			}
			assert.Equal(t, "added_hardlink", result.Status, result.Message)
		})
	}
}

// An unavailable Cross-seed instance fails the add before the Pre-add check:
// the Cross-seed instance reports the hash, and the add still fails as
// unavailable, with no tree and no add.
func TestRoutedAddFailsWhenTheCrossSeedInstanceIsUnavailable(t *testing.T) {
	tests := []struct {
		name    string
		down    bool
		wantMsg string
	}{
		{name: "offline", down: true, wantMsg: "Cross-seed instance unavailable: seeds: "},
		{name: "disabled in qui", wantMsg: "Cross-seed instance unavailable: seeds: qBittorrent instance is disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, tt.down)
			if !tt.down {
				_, err := r.instances.SetActiveState(t.Context(), r.crossSeed.ID, false)
				require.NoError(t, err)
			}
			r.sync.existing[r.crossSeed.ID] = true

			result := r.add(nil, nil, nil)

			assert.Equal(t, crossSeedUnavailableStatus, result.Status)
			assert.Contains(t, result.Message, tt.wantMsg)
			r.requireRouted(result)
			assert.Empty(t, r.sync.adds[r.crossSeed.ID])
			r.requireNoLinkTree()
		})
	}
}

func TestRoutedAddFailsWhenTheStoredRouteBreaksARule(t *testing.T) {
	r := newRoutedRig(t, false)
	r.saveRoute("seeds/links")

	result := r.add(nil, nil, nil)

	assert.Equal(t, crossSeedUnavailableStatus, result.Status)
	assert.Equal(t, "Cross-seed instance unavailable: mapped path seeds/links for "+fsops.CleanPath(fsops.HostPaths, r.links)+" is not an absolute path", result.Message)
	r.requireRouted(result)
	assert.Empty(t, r.sync.adds[r.crossSeed.ID])
	r.requireNoLinkTree()
}

// The Match verdict compares the candidate's instance with the instance of the
// search decision, so a routed add keeps the recheck that a relaxed match needs.
func TestRoutedAddKeepsTheRecheckOfARelaxedMatch(t *testing.T) {
	r := newRoutedRig(t, false)

	result := r.add(&CrossSeedRequest{SearchDecision: searchDecisionProvenance{
		Class:            searchCandidateClassExactSizeFallback,
		SourceInstanceID: r.source.ID,
		SourceHash:       routedMatched,
		Relaxed:          differencesOf(differenceGroup),
	}}, nil, nil)

	require.True(t, result.Success, result.Message)
	r.requireRouted(result)
	require.Equal(t, "true", r.sync.adds[r.crossSeed.ID][0]["paused"])
	queued := <-r.settler.queue
	assert.Equal(t, r.crossSeed.ID, queued.instanceID, "Settle rechecks on the Cross-seed instance")
	assert.True(t, queued.verificationRequired)
}

func TestRoutedAddStaysOutOfThePartialPool(t *testing.T) {
	r := newRoutedRig(t, false)
	store, err := models.NewCrossSeedStore(r.db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	r.svc.partialPool = &partialPoolCoordinator{
		instanceStore:   r.instances,
		automationStore: store,
		settings: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{PooledPartialCompletionEnabled: true}, nil
		},
	}
	// The NFO is not on disk, so the tree is partial.
	sourceFiles := append(qbt.TorrentFiles{}, r.files...)
	sourceFiles = append(sourceFiles, qbt.TorrentFile{Name: routedName + "/" + routedName + ".nfo", Size: 1})
	require.True(t, r.svc.partialPool.joinEnabled(t.Context(), r.source, true, &CrossSeedRequest{}, false), "the pool takes an add that is not routed")

	result := r.add(nil, nil, sourceFiles)

	require.True(t, result.Success, result.Message)
	r.requireRouted(result)
	assert.NotContains(t, result.Message, "pooled completion pending")
	queued := <-r.settler.queue
	assert.Equal(t, r.crossSeed.ID, queued.instanceID, "Settle, not the pool, resumes the routed add")
}

// The Cross-seed instance has no route, so a match on it stays on it. Its add
// still checks the hashes on its Source instance.
func TestRoutedAddMatchOnTheCrossSeedInstanceStaysThere(t *testing.T) {
	tests := []struct {
		name    string
		hasHash bool
		// source is "disabled", "offline", or empty for an online Source instance.
		source      string
		wantStatus  string
		wantMessage []string
	}{
		{name: "Source instance without the hash", wantStatus: "added_hardlink"},
		{name: "hash on the Source instance", hasHash: true, wantStatus: "exists", wantMessage: []string{"Torrent already exists in Source instance lib"}},
		{name: "turned-off Source instance is skipped", hasHash: true, source: "disabled", wantStatus: "added_hardlink"},
		{name: "offline Source instance fails the add", hasHash: true, source: "offline", wantStatus: "error",
			wantMessage: []string{"instance unavailable: lib: ", "Enable lib or clear its cross-seed route"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			_, err := r.instances.Update(t.Context(), r.crossSeed.ID, r.crossSeed.Name, r.crossSeed.Host, r.crossSeed.Username, "", nil, nil,
				&models.InstanceUpdateParams{HasLocalFilesystemAccess: new(true), UseHardlinks: new(true), HardlinkBaseDir: new(r.links)})
			require.NoError(t, err)
			r.sync.existing[r.source.ID] = tt.hasHash
			switch tt.source {
			case "disabled":
				_, err = r.instances.SetActiveState(t.Context(), r.source.ID, false)
				require.NoError(t, err)
			case "offline":
				down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
				}))
				t.Cleanup(down.Close)
				_, err = r.instances.Update(t.Context(), r.source.ID, r.source.Name, down.URL, r.source.Username, "", nil, nil, &models.InstanceUpdateParams{})
				require.NoError(t, err)
			}
			candidate := r.candidate()
			candidate.InstanceID, candidate.InstanceName = r.crossSeed.ID, r.crossSeed.Name

			result := r.svc.processCrossSeedCandidate(t.Context(), candidate, []byte("torrent"), adderV1, "", routedName,
				&CrossSeedRequest{}, r.svc.releaseCache.Parse(routedName), r.files, nil, nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Equal(t, r.crossSeed.ID, result.InstanceID)
			assert.Zero(t, result.CrossSeedInstanceID)
			assert.Empty(t, r.sync.adds[r.source.ID])
			if tt.wantStatus != "added_hardlink" {
				for _, want := range tt.wantMessage {
					assert.Contains(t, result.Message, want)
				}
				assert.Empty(t, r.sync.adds[r.crossSeed.ID])
				return
			}
			require.Len(t, r.sync.adds[r.crossSeed.ID], 1)
			assert.Equal(t, r.links, r.sync.adds[r.crossSeed.ID][0]["savepath"])
		})
	}
}

// The route maps the preset folder before the category save path changes to
// slashes, so a base folder with a backslash, as on a Windows Source instance,
// maps under its row.
func TestRoutedCategorySavePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, preset, baseDir, mapped, want string
		hostToo                             bool
	}{
		{name: "by-tracker", preset: "by-tracker", baseDir: "/data/hl", mapped: `D:\seeds`, want: "D:/seeds/Tracker", hostToo: true},
		{name: "by-instance", preset: "by-instance", baseDir: "/data/hl", mapped: "/seeds", want: "/seeds/seeds", hostToo: true},
		{name: "flat", preset: "flat", baseDir: "/data/hl", mapped: `D:\seeds`, want: `D:\seeds`, hostToo: true},
		{name: "same path", preset: "by-tracker", baseDir: "/data/hl", want: "/data/hl/Tracker", hostToo: true},
		{name: "backslash base folder", preset: "by-tracker", baseDir: `/data/hl\x`, mapped: "/seeds", want: "/seeds/Tracker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eachDialect(t, tt.hostToo, func(t *testing.T, d fsops.PathDialect, conv func(string) string) {
				baseDir := conv(tt.baseDir)
				candidate := CrossSeedCandidate{InstanceName: "lib", route: &candidateRoute{
					rows:      []models.CrossSeedRouteRow{{BaseDir: fsops.CleanPath(d, baseDir), MappedPath: tt.mapped}},
					crossSeed: &models.Instance{ID: 2, Name: "seeds"},
				}}
				instance := &models.Instance{Name: "lib", HardlinkDirPreset: tt.preset}

				got := (&Service{}).buildCategorySavePath(t.Context(), d, instance, baseDir, "", candidate, &CrossSeedRequest{IndexerName: "Tracker"})

				require.Equal(t, tt.want, got)
			})
		})
	}
}

func TestWebhookNotificationCountsRoutedFailures(t *testing.T) {
	t.Parallel()

	for _, failed := range []InstanceCrossSeedResult{
		{InstanceName: "lib", Status: crossSeedUnavailableStatus, Message: "Cross-seed instance unavailable: seeds: offline"},
		{InstanceName: "lib", Status: crossSeedCannotSeeStatus, Message: "Cross-seed instance cannot see the data at /seeds/links"},
	} {
		t.Run(failed.Status, func(t *testing.T) {
			t.Parallel()
			notifier := &recordingNotifier{}
			s := &Service{notifier: notifier}
			resp := &CrossSeedResponse{Results: []InstanceCrossSeedResult{failed}}

			s.notifyWebhookApply(t.Context(), &AutobrrApplyRequest{TorrentName: routedName}, resp, nil, time.Now())

			require.Len(t, notifier.events, 1)
			require.Equal(t, 1, notifier.events[0].CrossSeed.Failed)
			require.Equal(t, failed.Message, notifier.events[0].ErrorMessage)
		})
	}
}

// The top folder is taken by an empty folder, so the tree goes into the
// folder with a short hash. The second add finds every target there already
// linked, so its build creates no file. The check lists the folder that the
// tree used.
func TestRoutedAddChecksTheFolderThatTheTreeUsed(t *testing.T) {
	r := newRoutedRig(t, false)
	require.NoError(t, os.MkdirAll(filepath.Join(r.links, routedName), 0o755))
	hashed := routedMapped + "/" + pathutil.HashedIsolationFolder(adderV1, routedName)

	for i := range 2 {
		result := r.add(nil, nil, nil)

		require.True(t, result.Success, "add %d: %s", i+1, result.Message)
		assert.Equal(t, hashed, r.sync.adds[r.crossSeed.ID][i]["savepath"], "add %d", i+1)
		assert.Equal(t, hashed+"/"+routedName, r.sync.listed[i], "add %d lists the folder of a linked file", i+1)
	}
}

func TestRoutedAddStopsWhenTheCrossSeedInstanceCannotSeeTheData(t *testing.T) {
	tests := []struct {
		name        string
		hidden      int
		listErr     error
		wantStatus  string
		wantMessage string
		wantListed  int
	}{
		{
			name:        "the entry stays missing",
			hidden:      100,
			wantStatus:  crossSeedCannotSeeStatus,
			wantMessage: "Cross-seed instance cannot see the data at " + routedMapped + "/" + routedName,
			wantListed:  1 + visibleRetries,
		},
		{
			name:        "an answer other than 200",
			listErr:     fmt.Errorf("%w: 404", qbt.ErrUnexpectedStatus),
			wantStatus:  crossSeedCannotSeeStatus,
			wantMessage: "Cross-seed instance cannot see the data at " + routedMapped + "/" + routedName,
			wantListed:  1 + visibleRetries,
		},
		{
			name:        "a transport error",
			listErr:     errors.New("connection refused"),
			wantStatus:  crossSeedUnavailableStatus,
			wantMessage: "Cross-seed instance unavailable: seeds: list " + routedMapped + "/" + routedName + ": connection refused",
			wantListed:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			r.sync.hidden, r.sync.listErr = tt.hidden, tt.listErr

			result := r.add(nil, nil, nil)

			require.False(t, result.Success)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
			assert.Len(t, r.sync.listed, tt.wantListed)
			r.requireRouted(result)
			assert.Empty(t, r.sync.adds[r.crossSeed.ID], "a failed check adds nothing")
			assert.Empty(t, r.sync.categories[r.crossSeed.ID], "a failed check creates no category")
			r.requireNoLinkTree()
		})
	}
}

func TestRoutedAddPassesWhenTheEntryShowsUpOnARetry(t *testing.T) {
	r := newRoutedRig(t, false)
	r.sync.hidden = 1

	result := r.add(nil, nil, nil)

	require.True(t, result.Success, result.Message)
	assert.Len(t, r.sync.listed, 2)
	assert.Len(t, r.sync.adds[r.crossSeed.ID], 1)
}

// After a check fails for a base folder, a later add under that folder in
// the same run lists once. A new run retries again.
func TestRoutedAddRetriesAFailedFolderOncePerRun(t *testing.T) {
	r := newRoutedRig(t, false)
	r.sync.hidden = 100
	r.ctx = withRouteRun(t.Context())

	first := r.add(nil, nil, nil)
	second := r.add(nil, nil, nil)

	assert.Equal(t, crossSeedCannotSeeStatus, first.Status)
	assert.Equal(t, crossSeedCannotSeeStatus, second.Status)
	assert.Len(t, r.sync.listed, 1+visibleRetries+1, "the second add lists once")

	r.sync.listed = nil
	r.ctx = withRouteRun(t.Context())
	assert.Equal(t, crossSeedCannotSeeStatus, r.add(nil, nil, nil).Status)
	assert.Len(t, r.sync.listed, 1+visibleRetries, "a new run retries")
}

// A Source instance's candidate runs first, so the routed add builds its own
// link tree on the Cross-seed instance before a match on that instance can add
// the torrent there. The sort keeps the order of the other candidates, and
// without a route it changes nothing.
func TestRoutedCandidatesPutSourceInstancesFirst(t *testing.T) {
	tests := []struct {
		name    string
		noRoute bool
		want    []string
	}{
		{name: "with a route", want: []string{"lib", "seeds", "other"}},
		{name: "without a route", noRoute: true, want: []string{"seeds", "other", "lib"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, false)
			if tt.noRoute {
				require.NoError(t, r.routes.Delete(t.Context(), r.source.ID))
			}
			candidates := []CrossSeedCandidate{
				{InstanceID: r.crossSeed.ID, InstanceName: "seeds"},
				{InstanceID: r.crossSeed.ID + 100, InstanceName: "other"},
				{InstanceID: r.source.ID, InstanceName: "lib"},
			}

			r.svc.sortSourceInstancesFirst(t.Context(), candidates)

			names := make([]string, len(candidates))
			for i, c := range candidates {
				names[i] = c.InstanceName
			}
			assert.Equal(t, tt.want, names)
		})
	}
}

// A Library Scan on a Source instance does not start while its Cross-seed
// instance is unavailable. With the Cross-seed instance up, or with no route,
// the start goes on to the next check, which fails here because no Torznab
// search is set up.
func TestLibraryScanNeedsTheCrossSeedInstance(t *testing.T) {
	tests := []struct {
		name        string
		noRoute     bool
		down        bool
		off         bool
		wantErr     string
		wantInvalid bool
	}{
		{name: "available", wantErr: "torznab search is not configured"},
		{name: "no route", noRoute: true, off: true, wantErr: "torznab search is not configured"},
		{name: "offline", down: true, wantErr: "Cross-seed instance unavailable: seeds: ", wantInvalid: true},
		{name: "disabled in qui", off: true, wantErr: "Cross-seed instance unavailable: seeds: qBittorrent instance is disabled", wantInvalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoutedRig(t, tt.down)
			if tt.noRoute {
				require.NoError(t, r.routes.Delete(t.Context(), r.source.ID))
			}
			if tt.off {
				_, err := r.instances.SetActiveState(t.Context(), r.crossSeed.ID, false)
				require.NoError(t, err)
			}
			store, err := models.NewCrossSeedStore(r.db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			r.svc.automationStore = store

			_, err = r.svc.StartSearchRun(t.Context(), SearchRunOptions{InstanceID: r.source.ID})

			require.ErrorContains(t, err, tt.wantErr)
			if tt.wantInvalid {
				require.ErrorIs(t, err, ErrInvalidRequest)
			}
			runs, err := store.ListSearchRuns(t.Context(), r.source.ID, 10, 0)
			require.NoError(t, err)
			assert.Empty(t, runs, "no run starts")
		})
	}
}
