// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	routedSourceHash = "1111111111111111111111111111111111111111"
	routedHash       = "2222222222222222222222222222222222222222"
)

// routedRig is a Source instance with one torrent whose file a routed torrent
// on its Cross-seed instance hardlinks (ADR 0035). The Cross-seed instance is a
// fake qBittorrent that the test can take offline.
type routedRig struct {
	t         *testing.T
	service   *Service
	instances *models.InstanceStore
	clients   *qbittorrent.ClientPool
	reader    *routedFilesReader
	up        *atomic.Bool
	sourceID  int
	crossID   int
	dir       string
	// source is the Source instance torrent with the hardlinked file.
	source qbt.Torrent
}

// newRoutedRig builds the rig. With crossSeedLocal, the Cross-seed instance
// has Local Filesystem Access, so its row keeps the same path. Without it, the
// Cross-seed instance sees the link folder at /cs/links.
func newRoutedRig(t *testing.T, crossSeedLocal bool) *routedRig {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	links := filepath.Join(dir, "links")
	sourceFile := filepath.Join(dir, "data", "movie.mkv")
	createFile(t, sourceFile)
	require.NoError(t, os.MkdirAll(filepath.Join(links, "Movie"), 0o700))
	require.NoError(t, os.Link(sourceFile, filepath.Join(links, "Movie", "movie.mkv")))

	up := &atomic.Bool{}
	up.Store(true)
	qbit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case !up.Load():
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

	db := testdb.NewMigratedSQLite(t, "hardlink-routed")
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	source, err := instances.Create(ctx, "lib", "http://127.0.0.1:9", "admin", "password", nil, nil, false, new(true))
	require.NoError(t, err)
	crossSeed, err := instances.Create(ctx, "seeds", qbit.URL, "admin", "password", nil, nil, false, new(crossSeedLocal))
	require.NoError(t, err)

	row := models.CrossSeedRouteRow{BaseDir: links, MappedPath: "/cs/links"}
	crossSeedSavePath := "/cs/links/Movie"
	if crossSeedLocal {
		row.MappedPath = ""
		crossSeedSavePath = filepath.Join(links, "Movie")
	}
	routeStore := models.NewCrossSeedRouteStore(db)
	require.NoError(t, routeStore.Upsert(ctx, &models.CrossSeedRoute{SourceInstanceID: source.ID, CrossSeedInstanceID: crossSeed.ID, Rows: []models.CrossSeedRouteRow{row}}))

	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	backends := fsops.NewPool(instances, localbackend.NewBackend())

	forget := func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, source.ID)
		globalHardlinkIndexCache.mu.Unlock()
	}
	forget()
	t.Cleanup(forget)

	reader := &routedFilesReader{
		crossSeedID: crossSeed.ID,
		routed:      qbt.Torrent{Hash: routedHash, SavePath: crossSeedSavePath},
	}
	service := &Service{
		instanceStore:   instances,
		filesReader:     reader,
		routedTorrents:  reader,
		backendPool:     backends,
		crossSeedRoutes: crossseedroute.NewService(routeStore, instances, clients, backends),
	}
	return &routedRig{
		t:         t,
		service:   service,
		instances: instances,
		clients:   clients,
		reader:    reader,
		up:        up,
		sourceID:  source.ID,
		crossID:   crossSeed.ID,
		dir:       dir,
		source:    qbt.Torrent{Hash: routedSourceHash, SavePath: filepath.Join(dir, "data")},
	}
}

// index returns the hardlink index of the Source instance with its cross-instance scope.
func (r *routedRig) index(t *testing.T, torrents ...qbt.Torrent) *HardlinkIndex {
	t.Helper()
	index := r.service.GetHardlinkIndex(t.Context(), r.sourceID, torrents)
	require.NotNil(t, index)
	index.crossScopeMu.Lock()
	if index.CrossScopeByHash == nil && index.buildState != nil {
		r.service.augmentCrossInstanceScope(t.Context(), r.sourceID, index)
	}
	index.crossScopeMu.Unlock()
	return index
}

// routedFilesReader answers for both instances. Each torrent holds one file,
// movie.mkv, in its save path.
type routedFilesReader struct {
	crossSeedID int
	// routed is the torrent of the Cross-seed instance, as it reports it.
	routed qbt.Torrent
}

func (r *routedFilesReader) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		out[hash] = qbt.TorrentFiles{{Name: "movie.mkv", Size: 1}}
	}
	return out, nil
}

func (r *routedFilesReader) GetAllTorrents(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
	if instanceID != r.crossSeedID {
		return nil, nil
	}
	return []qbt.Torrent{r.routed}, nil
}

func (r *routedFilesReader) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	if instanceID != r.crossSeedID {
		return nil, nil
	}
	routed := r.routed
	return []qbittorrent.CrossInstanceTorrentView{{TorrentView: &qbittorrent.TorrentView{Torrent: &routed}}}, nil
}

func TestHardlinkIndexCountsRoutedLinksAsOtherTorrents(t *testing.T) {
	rig := newRoutedRig(t, false)
	// A second Source instance torrent holds the same file. Without the routed
	// link, the two would form a delete group.
	copyDir := filepath.Join(rig.dir, "copy")
	require.NoError(t, os.MkdirAll(copyDir, 0o700))
	require.NoError(t, os.Link(filepath.Join(rig.dir, "data", "movie.mkv"), filepath.Join(copyDir, "movie.mkv")))
	sourceCopy := qbt.Torrent{Hash: "3333333333333333333333333333333333333333", SavePath: copyDir}

	index := rig.index(t, rig.source, sourceCopy)

	require.Equal(t, HardlinkScopeTorrentsOnly, index.ScopeByHash[routedSourceHash])
	require.Equal(t, HardlinkScopeTorrentsOnly, index.CrossScopeByHash[routedSourceHash])
	// Deleting both Source torrents frees nothing while the routed torrent
	// holds the file, so neither takes the other along.
	require.Nil(t, index.GetHardlinkCopies(routedSourceHash))
	require.Nil(t, index.GetHardlinkCopies(sourceCopy.Hash))
	require.NotContains(t, index.SignatureByHash, routedHash)
	require.NotContains(t, index.ScopeByHash, routedHash)
}

func TestHardlinkIndexUpdateKeepsRoutedLinks(t *testing.T) {
	rig := newRoutedRig(t, false)
	// Unlinked torrents keep one new torrent inside the incremental budget.
	torrents := make([]qbt.Torrent, 1, 5)
	torrents[0] = rig.source
	for _, hash := range []string{"aa", "bb", "cc"} {
		createFile(t, filepath.Join(rig.dir, hash, "movie.mkv"))
		torrents = append(torrents, qbt.Torrent{Hash: hash, SavePath: filepath.Join(rig.dir, hash)})
	}
	built := rig.index(t, torrents...)
	require.Equal(t, HardlinkScopeTorrentsOnly, built.ScopeByHash[routedSourceHash])

	createFile(t, filepath.Join(rig.dir, "dd", "movie.mkv"))
	updated := rig.index(t, append(torrents, qbt.Torrent{Hash: "dd", SavePath: filepath.Join(rig.dir, "dd")})...)

	require.NotSame(t, built, updated)
	require.Equal(t, built.builtAt, updated.builtAt, "the update must be incremental, not a full build")
	require.Equal(t, HardlinkScopeNone, updated.ScopeByHash["dd"])
	require.Equal(t, HardlinkScopeTorrentsOnly, updated.ScopeByHash[routedSourceHash])
	require.Equal(t, HardlinkScopeTorrentsOnly, updated.CrossScopeByHash[routedSourceHash])
}

func TestHardlinkIndexRebuildsWhenRoutedTorrentsChange(t *testing.T) {
	rig := newRoutedRig(t, false)
	require.Equal(t, HardlinkScopeTorrentsOnly, rig.index(t, rig.source).ScopeByHash[routedSourceHash])

	// The routed torrent moves out of the link folder, so its link no longer counts.
	rig.reader.routed.SavePath = "/cs/elsewhere"

	require.Equal(t, HardlinkScopeOutsideQBitTorrent, rig.index(t, rig.source).ScopeByHash[routedSourceHash])
}

func TestDeleteRecheckCountsRoutedLinks(t *testing.T) {
	scope := &RuleCondition{Field: FieldHardlinkScope, Operator: OperatorEqual, Value: HardlinkScopeTorrentsOnly}
	cross := &RuleCondition{Field: FieldHardlinkScopeCross, Operator: OperatorEqual, Value: HardlinkScopeTorrentsOnly}
	for name, condition := range map[string]*RuleCondition{"scope": scope, "cross scope": cross} {
		t.Run(name, func(t *testing.T) {
			rig := newRoutedRig(t, false)
			index := rig.index(t, rig.source)
			rule := &models.Automation{Conditions: &ActionConditions{Delete: &DeleteAction{Enabled: true, Condition: condition}}}
			ruleByID := map[int]*models.Automation{1: rule}
			matched := index.ScopeByHash
			if condition == cross {
				matched = index.CrossScopeByHash
			}
			require.Equal(t, HardlinkScopeTorrentsOnly, matched[routedSourceHash], "the rule must select the torrent")

			blocked := rig.service.blockedDeleteCandidates(t.Context(), rig.sourceID, index,
				map[string]qbt.Torrent{routedSourceHash: rig.source},
				&deletionSelection{byHash: map[string]selectedDeletion{routedSourceHash: {rule: ruleRef{id: 1}}}}, ruleByID)

			require.Empty(t, blocked, "the re-check must see the routed link as a link to another torrent")
		})
	}
}

// A Cross-seed instance with Local Filesystem Access reports the routed file
// at the Source instance path, and the cross-instance step reads it there. A
// library link stays outside: a second count of the routed path would hide it.
func TestCrossScopeCountsARoutedLinkOnce(t *testing.T) {
	rig := newRoutedRig(t, true)
	library := filepath.Join(rig.dir, "library", "movie.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(library), 0o700))
	require.NoError(t, os.Link(filepath.Join(rig.dir, "data", "movie.mkv"), library))

	index := rig.index(t, rig.source)

	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash[routedSourceHash])
	require.Equal(t, HardlinkScopeBoth, index.CrossScopeByHash[routedSourceHash])
}

func TestHardlinkScopeIsUnknownWhileTheCrossSeedInstanceIsUnavailable(t *testing.T) {
	tests := map[string]struct {
		down, back func(*routedRig)
	}{
		"offline": {
			down: func(r *routedRig) { r.up.Store(false) },
			back: func(r *routedRig) {
				r.up.Store(true)
				r.clients.ResetFailureTracking(r.crossID) // the retry wait after the failed login is over
			},
		},
		"turned off in qui": {
			down: func(r *routedRig) { r.setActive(false) },
			back: func(r *routedRig) { r.setActive(true) },
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rig := newRoutedRig(t, false)
			tt.down(rig)

			index := rig.index(t, rig.source)

			require.NotContains(t, index.ScopeByHash, routedSourceHash)
			require.NotContains(t, index.CrossScopeByHash, routedSourceHash)
			require.Nil(t, cachedHardlinkIndex(rig.sourceID), "an index without the routed links must not be cached")
			rule := &models.Automation{Conditions: &ActionConditions{Delete: &DeleteAction{Enabled: true, IncludeHardlinks: true,
				Condition: &RuleCondition{Field: FieldHardlinkScope, Operator: OperatorEqual, Value: HardlinkScopeTorrentsOnly}}}}
			blocked := rig.service.blockedDeleteCandidates(t.Context(), rig.sourceID, index,
				map[string]qbt.Torrent{routedSourceHash: rig.source},
				&deletionSelection{byHash: map[string]selectedDeletion{routedSourceHash: {rule: ruleRef{id: 1}}}}, map[int]*models.Automation{1: rule})
			require.Equal(t, map[string]string{routedSourceHash: "hardlink scope unknown"}, blocked)

			tt.back(rig)

			index = rig.index(t, rig.source)
			require.Equal(t, HardlinkScopeTorrentsOnly, index.ScopeByHash[routedSourceHash])
			require.Equal(t, HardlinkScopeTorrentsOnly, index.CrossScopeByHash[routedSourceHash])
		})
	}
}

func (r *routedRig) setActive(active bool) {
	_, err := r.instances.SetActiveState(r.t.Context(), r.crossID, active)
	require.NoError(r.t, err)
}

// Without a saved route, the index reads as before: the other link is outside
// qBittorrent, and the index is cached.
func TestHardlinkIndexWithoutARouteIsUnchanged(t *testing.T) {
	rig := newRoutedRig(t, false)
	require.NoError(t, rig.service.crossSeedRoutes.Delete(t.Context(), rig.sourceID))

	index := rig.index(t, rig.source)

	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.ScopeByHash[routedSourceHash])
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.CrossScopeByHash[routedSourceHash])
	require.Same(t, index, cachedHardlinkIndex(rig.sourceID))
}
