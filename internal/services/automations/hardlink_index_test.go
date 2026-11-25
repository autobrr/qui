// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlink"
)

func TestIsPathInsideBase(t *testing.T) {
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{
			name:     "normal nested path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "path equals base (edge case)",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents",
			expected: true,
		},
		{
			name:     "parent traversal with ..",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + ".." + sep + "secret.txt",
			expected: false,
		},
		{
			name:     "path that resolves to parent",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data",
			expected: false,
		},
		{
			name:     "sibling path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "other",
			expected: false,
		},
		{
			name:     "current directory dots are ok",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "." + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "path with trailing separator",
			basePath: sep + "data" + sep + "torrents" + sep,
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInsideBase(fsops.HostPaths, tt.basePath, tt.fullPath)
			if result != tt.expected {
				t.Errorf("isPathInsideBase(%q, %q) = %v, want %v",
					tt.basePath, tt.fullPath, result, tt.expected)
			}
		})
	}
}

func TestIsPathInsideBase_RelativeCleanedPaths(t *testing.T) {
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{
			name:     "redundant separators in base",
			basePath: sep + "data" + sep + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "redundant separators in full",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "dot components in both",
			basePath: sep + "data" + sep + "." + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "." + sep + "file.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInsideBase(fsops.HostPaths, tt.basePath, tt.fullPath)
			if result != tt.expected {
				t.Errorf("isPathInsideBase(%q, %q) = %v, want %v",
					tt.basePath, tt.fullPath, result, tt.expected)
			}
		})
	}
}

func TestAugmentCrossInstanceScope_NoDeficits(t *testing.T) {
	t.Parallel()
	index := &HardlinkIndex{
		ScopeByHash: map[string]string{
			"hash1": HardlinkScopeNone,
			"hash2": HardlinkScopeTorrentsOnly,
		},
		buildState: &hardlinkBuildState{
			globalFileIDMap:   make(map[fsops.FileKey]*fileIDTracker),
			seenPaths:         make(map[string]struct{}),
			torrentInfoByHash: make(map[string]*torrentFileInfo),
		},
	}

	s := &Service{}
	s.augmentCrossInstanceScope(t.Context(), 1, index)

	if index.CrossScopeByHash == nil {
		t.Fatal("expected CrossScopeByHash to be populated")
	}
	if len(index.CrossScopeByHash) != len(index.ScopeByHash) {
		t.Errorf("expected %d entries, got %d", len(index.ScopeByHash), len(index.CrossScopeByHash))
	}
	for hash, expected := range index.ScopeByHash {
		if got := index.CrossScopeByHash[hash]; got != expected {
			t.Errorf("hash %s: expected %q, got %q", hash, expected, got)
		}
	}
	if index.buildState == nil {
		t.Error("expected buildState to be retained after augmentation")
	}
}

func TestAugmentCrossInstanceScope_NilIndex(t *testing.T) {
	t.Parallel()
	s := &Service{}
	s.augmentCrossInstanceScope(t.Context(), 1, nil)

	index := &HardlinkIndex{}
	s.augmentCrossInstanceScope(t.Context(), 1, index)
	if index.CrossScopeByHash != nil {
		t.Error("expected CrossScopeByHash to remain nil with nil buildState")
	}
}

func TestAugmentCrossInstanceScope_DeficitWithNoOtherInstances(t *testing.T) {
	t.Parallel()
	fid := createFile(t, filepath.Join(t.TempDir(), "deficit-file"))
	tracker := &fileIDTracker{nlink: 2, uniquePathCount: 1}

	index := &HardlinkIndex{
		ScopeByHash: map[string]string{
			"hash1": HardlinkScopeOutsideQBitTorrent,
		},
		buildState: &hardlinkBuildState{
			globalFileIDMap: map[fsops.FileKey]*fileIDTracker{testFileKey(fid, nil): tracker},
			seenPaths:       make(map[string]struct{}),
			torrentInfoByHash: map[string]*torrentFileInfo{
				"hash1": {
					fileIDs:       []hardlink.FileID{fid},
					allAccessible: true,
					hasHardlinks:  true,
				},
			},
		},
	}

	s := &Service{}
	s.augmentCrossInstanceScope(t.Context(), 1, index)

	if index.CrossScopeByHash == nil {
		t.Fatal("expected CrossScopeByHash to be populated")
	}
	if got := index.CrossScopeByHash["hash1"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Errorf("expected %q, got %q", HardlinkScopeOutsideQBitTorrent, got)
	}
}

func createFile(t *testing.T, path string) hardlink.FileID {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("content-"+filepath.Base(path)), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	fid, _, err := hardlink.GetFileID(fi, path)
	if err != nil {
		t.Fatal(err)
	}
	return fid
}

func TestScope_MixedInsideAndOutsideFilesYieldBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	insideFile := filepath.Join(dir, "instance-a", "pack", "episode1.mkv")
	outsideFile := filepath.Join(dir, "instance-a", "pack", "episode2.mkv")
	createFile(t, insideFile)
	createFile(t, outsideFile)
	crossSeed := filepath.Join(dir, "instance-a", "torrent2", "episode1.mkv")
	library := filepath.Join(dir, "library", "episode2.mkv")
	for src, dst := range map[string]string{insideFile: crossSeed, outsideFile: library} {
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(src, dst); err != nil {
			t.Fatal(err)
		}
	}

	index := indexFrom(map[string]*torrentFileInfo{
		"pack":  scanOne(t, filepath.Join(dir, "instance-a", "pack"), "episode1.mkv", "episode2.mkv"),
		"hash2": scanOne(t, filepath.Join(dir, "instance-a", "torrent2"), "episode1.mkv"),
	})
	scope := index.ScopeByHash

	if got := scope["pack"]; got != HardlinkScopeBoth {
		t.Errorf("pack: expected %q, got %q", HardlinkScopeBoth, got)
	}
	if got := scope["hash2"]; got != HardlinkScopeTorrentsOnly {
		t.Errorf("hash2: expected %q, got %q", HardlinkScopeTorrentsOnly, got)
	}
}

type cancelAfterFirstHardlinkLstatBackend struct {
	fsops.Backend
	cancel context.CancelFunc
}

func (b *cancelAfterFirstHardlinkLstatBackend) Lstat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	info, err := b.Backend.Lstat(ctx, path)
	if err == nil {
		b.cancel()
	}
	return info, err
}

func TestGetHardlinkIndex_CanceledFinalScanIsNotCached(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"

	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "one.mkv"))
	createFile(t, filepath.Join(dir, "two.mkv"))

	scanCtx, cancel := context.WithCancel(t.Context())
	backend := &cancelAfterFirstHardlinkLstatBackend{
		Backend: localbackend.NewBackend(),
		cancel:  cancel,
	}
	const instanceID = 901
	service := newHardlinkIndexService(t, instanceID, backend, qbt.TorrentFiles{{Name: "one.mkv"}, {Name: "two.mkv"}})
	torrents := []qbt.Torrent{{Hash: hash, SavePath: dir}}

	service.GetHardlinkIndex(scanCtx, instanceID, torrentRefs(torrents))
	require.ErrorIs(t, scanCtx.Err(), context.Canceled)

	require.Equal(t, HardlinkScopeNone, service.GetHardlinkIndex(t.Context(), instanceID, torrentRefs(torrents)).ScopeByHash[hash])
}

type fakeFilesReader struct{ files qbt.TorrentFiles }

func (f fakeFilesReader) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, hash := range hashes {
		out[hash] = f.files
	}
	return out, nil
}

func (fakeFilesReader) GetCachedInstanceTorrents(context.Context, int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, nil
}

type stubInstanceGetter struct{}

func (stubInstanceGetter) Get(_ context.Context, id int) (*models.Instance, error) {
	return &models.Instance{ID: id, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}, nil
}

func newHardlinkIndexService(t *testing.T, instanceID int, backend fsops.Backend, files qbt.TorrentFiles) *Service {
	t.Helper()
	clearHardlinkIndex := func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, instanceID)
		globalHardlinkIndexCache.mu.Unlock()
	}
	clearHardlinkIndex()
	t.Cleanup(clearHardlinkIndex)
	return &Service{
		filesReader: fakeFilesReader{files: files},
		backendPool: fsops.NewPool(stubInstanceGetter{}, backend),
	}
}

type fakeQbit struct {
	*httptest.Server
	mu       sync.Mutex
	torrents map[string]qbt.Torrent
	syncs    atomic.Int32
}

func newFakeQbit(t *testing.T, torrents []qbt.Torrent, filesHandler http.HandlerFunc) *fakeQbit {
	t.Helper()
	q := &fakeQbit{torrents: make(map[string]qbt.Torrent, len(torrents))}
	for _, torrent := range torrents {
		q.torrents[torrent.Hash] = torrent
	}
	q.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			q.syncs.Add(1)
			q.writeMaindata(w)
		case "/api/v2/torrents/files":
			filesHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(q.Close)
	return q
}

func (q *fakeQbit) writeMaindata(w http.ResponseWriter) {
	q.mu.Lock()
	defer q.mu.Unlock()
	rows := make(map[string]any, len(q.torrents))
	for hash, torrent := range q.torrents {
		rows[hash] = map[string]any{
			"name": torrent.Name, "save_path": torrent.SavePath, "content_path": torrent.ContentPath,
			"size": torrent.Size, "total_size": torrent.TotalSize, "progress": 1,
		}
	}
	_ = json.MarshalWrite(w, map[string]any{"rid": 1, "full_update": true, "torrents": rows})
}

func (q *fakeQbit) set(torrent qbt.Torrent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.torrents[torrent.Hash] = torrent
}

type hardlinkIndexRig struct {
	service    *Service
	instanceID int
	db         *database.DB
	qbit       *fakeQbit
	clients    *qbittorrent.ClientPool
}

func newHardlinkIndexRig(t *testing.T, name string, backend fsops.Backend, maindata []qbt.Torrent, filesHandler http.HandlerFunc) *hardlinkIndexRig {
	t.Helper()

	qbit := newFakeQbit(t, maindata, filesHandler)

	db := testdb.NewMigratedSQLite(t, name)
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), name, qbit.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	globalHardlinkIndexCache.mu.Lock()
	delete(globalHardlinkIndexCache.indices, instance.ID)
	globalHardlinkIndexCache.mu.Unlock()
	t.Cleanup(func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, instance.ID)
		globalHardlinkIndexCache.mu.Unlock()
	})

	syncManager := qbittorrent.NewSyncManager(clientPool, nil)
	syncManager.SetFilesManager(filesmanager.NewService(db))

	return &hardlinkIndexRig{
		service: &Service{
			filesReader: syncManager,
			backendPool: fsops.NewPool(instanceStore, backend),
		},
		instanceID: instance.ID,
		db:         db,
		qbit:       qbit,
		clients:    clientPool,
	}
}

func (r *hardlinkIndexRig) moveMaindata(t *testing.T, torrent qbt.Torrent) {
	t.Helper()
	r.qbit.set(torrent)
	client, err := r.clients.GetClient(t.Context(), r.instanceID)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
}

func ageFilesCache(t *testing.T, db *database.DB, d time.Duration) {
	t.Helper()
	at := time.Now().UTC().Add(-d)
	_, err := db.ExecContext(t.Context(), "UPDATE torrent_files_sync SET last_synced_at = ?, print_synced_at = ?", at, at)
	require.NoError(t, err)
}

func shiftFilesCache(t *testing.T, db *database.DB, d time.Duration) {
	t.Helper()
	type stamps struct {
		instanceID, hashID int64
		synced             time.Time
		printSynced        sql.NullTime
	}
	rows, err := db.QueryContext(t.Context(), "SELECT instance_id, torrent_hash_id, last_synced_at, print_synced_at FROM torrent_files_sync")
	require.NoError(t, err)
	var all []stamps
	for rows.Next() {
		var row stamps
		require.NoError(t, rows.Scan(&row.instanceID, &row.hashID, &row.synced, &row.printSynced))
		all = append(all, row)
	}
	require.NoError(t, rows.Close())
	require.NoError(t, rows.Err())
	require.NotEmpty(t, all)

	for _, row := range all {
		var printSynced any
		if row.printSynced.Valid {
			printSynced = row.printSynced.Time.Add(-d)
		}
		_, err := db.ExecContext(t.Context(), "UPDATE torrent_files_sync SET last_synced_at = ?, print_synced_at = ? WHERE instance_id = ? AND torrent_hash_id = ?",
			row.synced.Add(-d), printSynced, row.instanceID, row.hashID)
		require.NoError(t, err)
	}
}

func singleFileTorrent(hash, dir, file string) qbt.Torrent {
	return qbt.Torrent{Hash: hash, Name: file, SavePath: dir, ContentPath: filepath.Join(dir, file), Size: 1, TotalSize: 1}
}

func TestDeletionSelection_UnreadableHardlinkScopeDoesNotDelete(t *testing.T) {
	const instanceID = 902
	service := newHardlinkIndexService(t, instanceID, localbackend.NewBackend(), qbt.TorrentFiles{{Name: "data.bin", Priority: 1}})
	dir := t.TempDir()
	torrents := []qbt.Torrent{
		{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SavePath: filepath.Join(dir, "a")},
		{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SavePath: filepath.Join(dir, "b")},
	}
	index := service.GetHardlinkIndex(t.Context(), instanceID, torrentRefs(torrents))
	require.Empty(t, index.ScopeByHash)
	rule := &models.Automation{
		ID: 1, Enabled: true, TrackerPattern: "*",
		Conditions: &ActionConditions{Delete: &DeleteAction{
			Enabled: true,
			Mode:    DeleteModeWithFilesIncludeCrossSeeds,
			Condition: &RuleCondition{
				Field: FieldHardlinkScope, Operator: OperatorEqual, Value: HardlinkScopeOutsideQBitTorrent, Negate: true,
			},
		}},
	}
	evalCtx := &EvalContext{InstanceHasTrustedIdentity: true, HardlinkScopeByHash: index.ScopeByHash}
	require.Empty(t, selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx).triggers)

	for _, torrent := range torrents {
		index.ScopeByHash[torrent.Hash] = HardlinkScopeNone
	}
	selection := selectTestDeletions(t, []*models.Automation{rule}, torrents, evalCtx)
	require.Len(t, selection.triggers, len(torrents))
	for _, torrent := range torrents {
		require.Contains(t, selection.byHash, torrent.Hash)
	}
}

func TestBlockedDeleteCandidates_UnknownHardlinkScope(t *testing.T) {
	const instanceID = 903
	service := newHardlinkIndexService(t, instanceID, localbackend.NewBackend(), qbt.TorrentFiles{{Name: "data.bin", Priority: 1}})
	dir := t.TempDir()
	const unknown = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const stale = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const readable = "cccccccccccccccccccccccccccccccccccccccc"
	torrents := []qbt.Torrent{
		{Hash: unknown, SavePath: filepath.Join(dir, "unknown"), Category: "movies"},
		{Hash: stale, SavePath: filepath.Join(dir, "stale")},
		{Hash: readable, SavePath: filepath.Join(dir, "readable")},
	}
	createFile(t, filepath.Join(torrents[1].SavePath, "data.bin"))
	createFile(t, filepath.Join(torrents[2].SavePath, "data.bin"))
	index := service.GetHardlinkIndex(t.Context(), instanceID, torrentRefs(torrents))
	require.NotContains(t, index.ScopeByHash, unknown)
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[stale])
	require.NoError(t, os.Remove(filepath.Join(torrents[1].SavePath, "data.bin")))
	torrentByHash := map[string]qbt.Torrent{unknown: torrents[0], stale: torrents[1], readable: torrents[2]}
	pending := &deletionSelection{byHash: map[string]selectedDeletion{
		unknown: {rule: ruleRef{id: 1}}, stale: {rule: ruleRef{id: 1}}, readable: {rule: ruleRef{id: 1}},
	}}
	scope := &RuleCondition{Field: FieldHardlinkScope, Operator: OperatorEqual, Value: HardlinkScopeNone}
	category := &RuleCondition{Field: FieldCategory, Operator: OperatorEqual, Value: "movies"}
	rule := &models.Automation{Conditions: &ActionConditions{Delete: &DeleteAction{Enabled: true, Condition: scope}}}
	rules := map[int]*models.Automation{1: rule}

	blocked := service.blockedDeleteCandidates(t.Context(), instanceID, index, torrentRefMap(torrentByHash), pending, rules)
	require.Equal(t, map[string]string{unknown: "hardlink scope unknown", stale: "files no longer readable"}, blocked)

	rule.Conditions.Delete.Condition = &RuleCondition{Operator: OperatorOr, Conditions: []*RuleCondition{scope, category}}
	require.True(t, EvaluateConditionWithContext(rule.Conditions.Delete.Condition, torrents[0], &EvalContext{InstanceHasTrustedIdentity: true}, 0))
	blocked = service.blockedDeleteCandidates(t.Context(), instanceID, index, torrentRefMap(torrentByHash), pending, rules)
	require.Contains(t, blocked, unknown)

	blocked = service.blockedDeleteCandidates(t.Context(), instanceID, nil, torrentRefMap(torrentByHash), pending, rules)
	require.Len(t, blocked, len(torrents))

	rule.Conditions.Delete.Condition = category
	for _, index := range []*HardlinkIndex{index, nil} {
		require.Empty(t, service.blockedDeleteCandidates(t.Context(), instanceID, index, torrentRefMap(torrentByHash), pending, rules))
	}
}

func TestBuildFullPathRejectsNonAbsoluteBase(t *testing.T) {
	for _, base := range []string{"", ".", "relative/dir"} {
		if _, ok := buildFullPath(fsops.HostPaths, base, "Show.S01/episode.mkv"); ok {
			t.Errorf("buildFullPath(%q, ...) = ok, want rejected: a relative join resolves against the working directory", base)
		}
	}
}

func TestGetHardlinkIndex_ExpiredRebuildReadsCachedFileLists(t *testing.T) {
	const (
		hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		hashC = "cccccccccccccccccccccccccccccccccccccccc"
		hashD = "dddddddddddddddddddddddddddddddddddddddd"
	)

	dir := t.TempDir()
	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		createFile(t, filepath.Join(dir, name))
	}

	var fileRequests atomic.Int32
	maindata := []qbt.Torrent{
		singleFileTorrent(hashA, dir, "a.mkv"), singleFileTorrent(hashB, dir, "b.mkv"),
		singleFileTorrent(hashC, dir, "c.mkv"), singleFileTorrent(hashD, dir, "d.mkv"),
	}
	rig := newHardlinkIndexRig(t, "hardlink-index-expired-rebuild", localbackend.NewBackend(), maindata, func(w http.ResponseWriter, r *http.Request) {
		fileRequests.Add(1)
		_, _ = fmt.Fprintf(w, `[{"name":"%.1s.mkv","priority":1}]`, r.URL.Query().Get("hash"))
	})
	all := []qbt.Torrent{{Hash: hashA, SavePath: dir}, {Hash: hashB, SavePath: dir}, {Hash: hashC, SavePath: dir}, {Hash: hashD, SavePath: dir}}
	torrents := all[:2]

	index := rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(2), fileRequests.Load())
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashA])

	require.NoError(t, os.Link(filepath.Join(dir, "a.mkv"), filepath.Join(t.TempDir(), "a-copy.mkv")))
	expireHardlinkIndex(t, rig.instanceID)
	ageFilesCache(t, rig.db, 30*time.Minute)

	index = rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(2), fileRequests.Load())
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.ScopeByHash[hashA])

	expireHardlinkIndex(t, rig.instanceID)
	torrents = all[:3]

	index = rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(3), fileRequests.Load())
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashC])

	require.NoError(t, os.Link(filepath.Join(dir, "a.mkv"), filepath.Join(dir, "d.mkv")))
	ageFilesCache(t, rig.db, 30*time.Minute)
	torrents = all

	index = rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(4), fileRequests.Load())
	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash[hashA])
	require.Equal(t, HardlinkScopeBoth, index.ScopeByHash[hashD])
}

func TestGetHardlinkIndex_StaleCachedNameLeavesScopeUnknown(t *testing.T) {
	const (
		hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "a.mkv"))
	createFile(t, filepath.Join(dir, "b.mkv"))

	var fileRequests atomic.Int32
	names := map[string]string{hashA: "a.mkv", hashB: "b.mkv"}
	maindata := []qbt.Torrent{singleFileTorrent(hashA, dir, "a.mkv"), singleFileTorrent(hashB, dir, "b.mkv")}
	rig := newHardlinkIndexRig(t, "hardlink-index-stale-name", localbackend.NewBackend(), maindata, func(w http.ResponseWriter, r *http.Request) {
		fileRequests.Add(1)
		_, _ = fmt.Fprintf(w, `[{"name":%q,"priority":1}]`, names[r.URL.Query().Get("hash")])
	})
	torrents := []qbt.Torrent{{Hash: hashA, SavePath: dir}, {Hash: hashB, SavePath: dir}}

	index := rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(2), fileRequests.Load())
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashA])

	require.NoError(t, os.Rename(filepath.Join(dir, "a.mkv"), filepath.Join(dir, "a-renamed.mkv")))
	names[hashA] = "a-renamed.mkv"
	expireHardlinkIndex(t, rig.instanceID)
	ageFilesCache(t, rig.db, 30*time.Minute)

	index = rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(2), fileRequests.Load())
	require.Empty(t, index.ScopeByHash[hashA])
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashB])

	expireHardlinkIndex(t, rig.instanceID)
	ageFilesCache(t, rig.db, 2*time.Hour)
	rig.moveMaindata(t, singleFileTorrent(hashA, dir, "a-renamed.mkv"))

	index = rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(3), fileRequests.Load())
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashA])
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashB])
}

func newTwoTorrentRig(t *testing.T, name string) (*hardlinkIndexRig, []qbt.Torrent, *atomic.Int32) {
	t.Helper()
	const (
		hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "a.mkv"))
	createFile(t, filepath.Join(dir, "b.mkv"))

	fileRequests := &atomic.Int32{}
	maindata := []qbt.Torrent{singleFileTorrent(hashA, dir, "a.mkv"), singleFileTorrent(hashB, dir, "b.mkv")}
	rig := newHardlinkIndexRig(t, name, localbackend.NewBackend(), maindata, func(w http.ResponseWriter, r *http.Request) {
		fileRequests.Add(1)
		_, _ = fmt.Fprintf(w, `[{"name":"%.1s.mkv","priority":1}]`, r.URL.Query().Get("hash"))
	})
	torrents := []qbt.Torrent{{Hash: hashA, SavePath: dir}, {Hash: hashB, SavePath: dir}}

	index := rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(2), fileRequests.Load())
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[hashA])
	return rig, torrents, fileRequests
}

func TestGetHardlinkIndex_RebuildTwoHoursLaterSendsNoFileRequest(t *testing.T) {
	rig, torrents, fileRequests := newTwoTorrentRig(t, "hardlink-index-two-hours")
	syncs := rig.qbit.syncs.Load()

	expireHardlinkIndex(t, rig.instanceID)
	shiftFilesCache(t, rig.db, 2*time.Hour)

	index := rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(2), fileRequests.Load())
	require.Equal(t, syncs, rig.qbit.syncs.Load(), "the print read sends no sync/maindata")
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash[torrents[0].Hash])
}

func TestGetHardlinkIndex_RowPastItsBackstopIsFetchedOnce(t *testing.T) {
	rig, torrents, fileRequests := newTwoTorrentRig(t, "hardlink-index-backstop")

	expireHardlinkIndex(t, rig.instanceID)
	ageFilesCache(t, rig.db, 23*time.Hour)
	rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(4), fileRequests.Load())

	expireHardlinkIndex(t, rig.instanceID)
	rig.service.GetHardlinkIndex(t.Context(), rig.instanceID, torrentRefs(torrents))
	require.Equal(t, int32(4), fileRequests.Load(), "the refetched lists are current again")
}

func expireHardlinkIndex(t *testing.T, instanceID int) {
	t.Helper()
	globalHardlinkIndexCache.mu.Lock()
	defer globalHardlinkIndexCache.mu.Unlock()
	index := globalHardlinkIndexCache.indices[instanceID]
	require.NotNil(t, index)
	index.builtAt = time.Now().Add(-hardlinkIndexTTL)
}

func ageHardlinkIndex(t *testing.T, instanceID int, d time.Duration) {
	t.Helper()
	globalHardlinkIndexCache.mu.Lock()
	defer globalHardlinkIndexCache.mu.Unlock()
	index := globalHardlinkIndexCache.indices[instanceID]
	require.NotNil(t, index)
	index.builtAt = index.builtAt.Add(-d)
}

func TestGetHardlinkIndex_IncrementalUpdatesDoNotDelayTheFullRebuild(t *testing.T) {
	const instanceID = 904
	service := newHardlinkIndexService(t, instanceID, localbackend.NewBackend(), qbt.TorrentFiles{{Name: "data.bin", Priority: 1}})
	dir := t.TempDir()

	var torrents []qbt.Torrent
	addTorrent := func(name string) {
		savePath := filepath.Join(dir, name)
		createFile(t, filepath.Join(savePath, "data.bin"))
		torrents = append(torrents, qbt.Torrent{Hash: name, SavePath: savePath})
	}
	for _, name := range []string{"t1", "t2", "t3", "t4", "t5", "t6"} {
		addTorrent(name)
	}

	index := service.GetHardlinkIndex(t.Context(), instanceID, torrentRefs(torrents))
	require.Equal(t, HardlinkScopeNone, index.ScopeByHash["t1"])

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "library"), 0o700))
	require.NoError(t, os.Link(filepath.Join(dir, "t1", "data.bin"), filepath.Join(dir, "library", "data.bin")))

	for _, name := range []string{"t7", "t8"} {
		ageHardlinkIndex(t, instanceID, hardlinkIndexTTL*2/5)
		addTorrent(name)
		index = service.GetHardlinkIndex(t.Context(), instanceID, torrentRefs(torrents))
		require.Equal(t, HardlinkScopeNone, index.ScopeByHash["t1"], "an incremental update does not read t1 again")
	}

	ageHardlinkIndex(t, instanceID, hardlinkIndexTTL*2/5)
	index = service.GetHardlinkIndex(t.Context(), instanceID, torrentRefs(torrents))
	require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.ScopeByHash["t1"], "the full rebuild reads t1 again")
}

func TestBlockedDeleteCandidates_CrossInstanceScope(t *testing.T) {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scope := &RuleCondition{Field: FieldHardlinkScope, Operator: OperatorEqual, Value: HardlinkScopeOutsideQBitTorrent}
	cross := &RuleCondition{Field: FieldHardlinkScopeCross, Operator: OperatorEqual, Value: HardlinkScopeTorrentsOnly}

	tests := []struct {
		name        string
		condition   *RuleCondition
		augment     bool
		linkLibrary bool
		want        map[string]string
	}{
		{name: "cross scope, linked on the other instance only", condition: cross, augment: true},
		{name: "single scope after the cross-instance step", condition: scope, augment: true},
		{name: "both scopes", condition: &RuleCondition{Operator: OperatorAnd, Conditions: []*RuleCondition{scope, cross}}, augment: true},
		{
			name: "cross scope, links changed on disk", condition: cross, augment: true, linkLibrary: true,
			want: map[string]string{hash: "hardlink scope changed from " + HardlinkScopeTorrentsOnly + " to " + HardlinkScopeBoth},
		},
		{
			name: "cross scope never computed", condition: cross,
			want: map[string]string{hash: "hardlink scope unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			fileA := filepath.Join(dir, "a", "movie.mkv")
			createFile(t, fileA)
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o700))
			require.NoError(t, os.Link(fileA, filepath.Join(dir, "b", "movie.mkv")))

			db := testdb.NewMigratedSQLite(t, "hardlink-cross-scope")
			instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
			require.NoError(t, err)
			instanceA, err := instanceStore.Create(t.Context(), "a", "http://127.0.0.1:9", "", "", nil, nil, false, new(true))
			require.NoError(t, err)
			_, err = instanceStore.Create(t.Context(), "b", "http://127.0.0.1:9", "", "", nil, nil, false, new(true))
			require.NoError(t, err)
			service := newHardlinkIndexService(t, instanceA.ID, localbackend.NewBackend(), nil)
			service.instanceStore = instanceStore
			service.backendPool = fsops.NewPool(instanceStore, localbackend.NewBackend())
			service.filesReader = crossFilesReader{savePath: filepath.Join(dir, "b"), files: qbt.TorrentFiles{{Name: "movie.mkv"}}}

			torrent := qbt.Torrent{Hash: hash, SavePath: filepath.Join(dir, "a")}
			index := service.GetHardlinkIndex(t.Context(), instanceA.ID, torrentRefs([]qbt.Torrent{torrent}))
			require.Equal(t, HardlinkScopeOutsideQBitTorrent, index.ScopeByHash[hash])
			if tt.augment {
				index.crossScopeMu.Lock()
				service.augmentCrossInstanceScope(t.Context(), instanceA.ID, index)
				index.crossScopeMu.Unlock()
				require.Equal(t, HardlinkScopeTorrentsOnly, index.CrossScopeByHash[hash])
			}
			if tt.linkLibrary {
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "library"), 0o700))
				require.NoError(t, os.Link(fileA, filepath.Join(dir, "library", "movie.mkv")))
			}

			rule := &models.Automation{Conditions: &ActionConditions{Delete: &DeleteAction{Enabled: true, Condition: tt.condition}}}
			blocked := service.blockedDeleteCandidates(t.Context(), instanceA.ID, index,
				torrentRefMap(map[string]qbt.Torrent{hash: torrent}),
				&deletionSelection{byHash: map[string]selectedDeletion{hash: {rule: ruleRef{id: 1}}}}, map[int]*models.Automation{1: rule})
			require.Equal(t, tt.want, blocked)
		})
	}
}

func testFileKey(id hardlink.FileID, instance *models.Instance) fsops.FileKey {
	key, _ := fsops.FileKeyOf(id, instance)
	return key
}
