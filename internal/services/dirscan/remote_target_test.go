// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/fsops/remote"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/hardlink"
)

// rowPool answers every read with row, and any remote backend it builds counts as an unexpected read.
func rowPool(row *models.Instance) *fsops.Pool {
	return fsops.NewPoolWithRemote(&fakeInstanceStore{instance: row}, local.NewBackend(), func(*models.Instance) fsops.Backend { return &remoteSpy{} })
}

// An exec walk that fell back to sftp reads no identity, which a row claiming Identity should explain once.
func TestScanLogsFilesWithoutIdentity(t *testing.T) {
	t.Parallel()

	identified := slashEntry("/data/scan/Film/a.mkv", "Film/a.mkv", 0)
	identified.FileID = remoteID(1)
	for _, tt := range []struct {
		name string
		tier models.SSHTier
		walk []fsops.WalkEntry
		want string
	}{
		{name: "exec row, every identity read", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified}},
		{name: "exec row, identity missing", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified, slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0), slashEntry("/data/scan/Film/c.mkv", "Film/c.mkv", 0)}, want: `"files":2`},
		{name: "exec row, one identity missing", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified, slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0)}, want: `"files":1`},
		{name: "sftp row never has identity", tier: models.SSHTierSFTP, walk: []fsops.WalkEntry{slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			walk := append([]fsops.WalkEntry{slashEntry("/data/scan", ".", fs.ModeDir), slashEntry("/data/scan/Film", "Film", fs.ModeDir)}, tt.walk...)
			row := remoteInstance(tt.tier)
			svc := &Service{torrentFiles: stubTorrentFiles{empty: true}, backendPool: rowPool(row)}
			var buf bytes.Buffer
			l := zerolog.New(&buf)

			_, _, ok := svc.runScanPhase(t.Context(), &models.DirScanDirectory{TargetInstanceID: row.ID}, &scanTarget{backend: newRecordingBackend(walk...), instance: row}, "/data/scan", 1, &l)

			require.True(t, ok)
			if tt.want == "" {
				require.NotContains(t, buf.String(), "no identity")
				return
			}
			require.Contains(t, buf.String(), "no identity")
			require.Contains(t, buf.String(), tt.want)
		})
	}
}

// A stored identity only names a file on the endpoint that read it, so another server's file with the
// same device and inode is a new file, even after the status write of the search phase.
//
// regression: #2919
func TestStoredIdentityFollowsTheEndpoint(t *testing.T) {
	base := remoteInstance(models.SSHTierExec)
	base.SSHUsername = "seed"
	changed := func(change func(*models.Instance)) *models.Instance {
		row := *base
		change(&row)
		return &row
	}
	const oldPath, newPath = "/data/old/film.mkv", "/data/new/film.mkv"
	for _, tt := range []struct {
		name       string
		next       *models.Instance
		wantRename bool
	}{
		{name: "same endpoint keeps the verdict through a rename", next: changed(func(*models.Instance) {}), wantRename: true},
		{name: "host", next: changed(func(r *models.Instance) { r.SSHHost = "other.example.invalid" })},
		{name: "port", next: changed(func(r *models.Instance) { r.SSHPort = 2222 })},
		{name: "account", next: changed(func(r *models.Instance) { r.SSHUsername = "alt" })},
		{name: "host key replaced", next: changed(func(r *models.Instance) { r.SSHHostKeyEncrypted = "enc-hostkey-2" })},
		{name: "private key replaced", next: changed(func(r *models.Instance) { r.SSHKeyEncrypted = "enc-key-2" })},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			modTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
			scan := func(p string) *ScanResult {
				return &ScanResult{Searchees: []*Searchee{{Name: "film", Path: p, Files: []*ScannedFile{
					{Path: p, RelPath: "film.mkv", Size: 100, ModTime: modTime, FileID: remoteID(1)},
				}}}}
			}
			l := zerolog.Nop()

			first := scan(oldPath)
			tracked, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, base, first, &seedingIndex{instance: base}, &l)
			require.NoError(t, err)
			require.NoError(t, svc.upsertDirScanFileStatus(ctx, dir.ID, first.Searchees[0].Files[0], tracked, dirScanFileUpdate{status: models.DirScanFileStatusNoMatch}))

			second, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, tt.next, scan(newPath), &seedingIndex{instance: tt.next}, &l)

			require.NoError(t, err)
			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			byPath := map[string]models.DirScanFileStatus{}
			for _, f := range files {
				byPath[f.FilePath] = f.Status
			}
			if tt.wantRename {
				require.Equal(t, map[string]models.DirScanFileStatus{newPath: models.DirScanFileStatusNoMatch}, byPath)
				require.Equal(t, models.DirScanFileStatusNoMatch, second.byPath[newPath].Status)
				return
			}
			require.Equal(t, map[string]models.DirScanFileStatus{
				oldPath: models.DirScanFileStatusNoMatch,
				newPath: models.DirScanFileStatusPending,
			}, byPath)
			require.Equal(t, models.DirScanFileStatusPending, second.byPath[newPath].Status)
		})
	}
}

// Identity stored in local mode keeps its exact bytes, and never matches a remote file.
func TestStoredIdentityOfALocalRowIsUnchanged(t *testing.T) {
	t.Parallel()

	local := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	id := hardlink.FileIDFromBytes(append([]byte{byte(hardlink.KindUnix)}, make([]byte, 24)...))
	idx := &trackedFilesIndex{instance: local, endpoint: endpointTag(local)}
	require.Equal(t, id.Bytes(), idx.storedFileID(id))
	require.Nil(t, idx.storedFileID(hardlink.FileID{}))

	remote := remoteInstance(models.SSHTierExec)
	remoteIdx := &trackedFilesIndex{instance: remote, endpoint: endpointTag(remote)}
	_, ok := remoteIdx.keyOfStored(id.Bytes())
	require.False(t, ok)
	_, ok = idx.keyOfStored(remoteIdx.storedFileID(remoteID(1)))
	require.False(t, ok)
}

// A dial reads the row, not the run's copy, so an SSH edit during the walk can read the new server under the old scope.
//
// regression: #2919
func TestScanFailsWhenTheEndpointChangesDuringTheWalk(t *testing.T) {
	walked := remoteInstance(models.SSHTierExec)
	otherHost := *walked
	otherHost.SSHHost = "other.example.invalid"
	otherPin := *walked
	otherPin.SSHHostKeyEncrypted = "enc-hostkey-2"
	localNow := *walked
	localNow.HasLocalFilesystemAccess = true
	editedByTheWalk := &fakeInstanceStore{instance: walked}
	for _, tt := range []struct {
		name       string
		store      instanceGetter
		duringWalk func()
		wantOK     bool
		wantErr    string
	}{
		{name: "unchanged", store: &fakeInstanceStore{instance: walked}, wantOK: true},
		{name: "other host", store: &fakeInstanceStore{instance: &otherHost}, wantErr: errRemoteTargetChanged.Error()},
		{name: "host key replaced", store: &fakeInstanceStore{instance: &otherPin}, wantErr: errRemoteTargetChanged.Error()},
		{name: "local access turned on", store: &fakeInstanceStore{instance: &localNow}, wantErr: errRemoteTargetChanged.Error()},
		{name: "credentials removed", store: &fakeInstanceStore{instance: &models.Instance{ID: walked.ID}}, wantErr: errRemoteTargetChanged.Error()},
		{name: "row unreadable", store: failingInstanceStore{}, wantErr: "database is locked"},
		{name: "row reads as walked until the walk runs", store: editedByTheWalk, duringWalk: func() { editedByTheWalk.instance = &otherHost }, wantErr: errRemoteTargetChanged.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			svc, store, dir := newDirScanTestService(t)
			runID, err := store.CreateRunIfNoActive(ctx, dir.ID, "scheduled", "")
			require.NoError(t, err)
			svc.torrentFiles = stubTorrentFiles{empty: true}
			svc.backendPool = fsops.NewPoolWithRemote(tt.store, local.NewBackend(), func(*models.Instance) fsops.Backend { return &remoteSpy{} })
			backend := &rowEditedDuringWalk{recordingBackend: newRecordingBackend(slashEntry("/data/scan", ".", fs.ModeDir), slashEntry("/data/scan/film.mkv", "film.mkv", 0)), edit: tt.duringWalk}
			l := zerolog.Nop()

			result, _, ok := svc.runScanPhase(ctx, dir, &scanTarget{backend: backend, instance: walked}, "/data/scan", runID, &l)

			require.Equal(t, tt.wantOK, ok)
			run, err := store.GetRun(ctx, runID)
			require.NoError(t, err)
			if tt.wantOK {
				require.Len(t, result.Searchees, 1)
				return
			}
			require.Nil(t, result)
			require.Equal(t, models.DirScanRunStatusFailed, run.Status)
			require.Contains(t, run.ErrorMessage, tt.wantErr)
		})
	}
}

type instanceGetter interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

// rowEditedDuringWalk runs edit when the walk starts, so a read of the row before the walk sees the old one.
type rowEditedDuringWalk struct {
	*recordingBackend
	edit func()
}

func (b *rowEditedDuringWalk) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	if b.edit != nil {
		b.edit()
	}
	return b.recordingBackend.WalkDir(ctx, root, opts)
}

// remoteRunSearcher answers by search query and records every query.
type remoteRunSearcher struct {
	indexerID int
	results   map[string][]jackett.SearchResult
	queries   []string
	answered  int
}

func (s *remoteRunSearcher) SearchWithScope(_ context.Context, req *jackett.TorznabSearchRequest, _ string) error {
	label := req.Query
	if req.Episode != nil {
		label = fmt.Sprintf("%s E%02d", req.Query, *req.Episode)
	}
	s.queries = append(s.queries, label)
	results := s.results[label]
	if len(results) > 0 {
		s.answered++
	}
	req.OnAllComplete(&jackett.SearchResponse{
		Results:             results,
		RequestedIndexerIDs: []int{s.indexerID},
		CoveredIndexerIDs:   []int{s.indexerID},
	}, nil)
	return nil
}

// countingInstanceStore answers with first for the first reads, then with later.
type countingInstanceStore struct {
	first, later *models.Instance
	firstReads   int32
	reads        atomic.Int32
}

func (s *countingInstanceStore) Get(context.Context, int) (*models.Instance, error) {
	if s.reads.Add(1) <= s.firstReads {
		return s.first, nil
	}
	return s.later, nil
}

const (
	remoteRunFilmFolder = "Synthetic.Film.2024.1080p.WEB.x264-GRP"
	remoteRunFilmRoot   = "Synthetic.Film.2024.1080p.WEB.x264-OTHER"
	remoteRunFilmSize   = int64(1 << 20)
)

type remoteRunFixture struct {
	svc      *Service
	store    *models.DirScanStore
	dir      *models.DirScanDirectory
	row      *models.Instance
	backend  answerBackend
	searcher *remoteRunSearcher
	manager  *alignFakeManager
	cacheDB  *cacheTouchDB
	d        fsops.PathDialect
}

// newRemoteRun is every piece of a run on a remote instance over a fake host, in dialect d. The library
// holds a season folder with no results, a film with one result, a film in a folder with no useful name, a
// file a torrent seeds by inode, and a file renamed since its verdict.
func newRemoteRun(t *testing.T, d fsops.PathDialect, store func(row *models.Instance) instanceGetter) *remoteRunFixture {
	t.Helper()
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "dirscan-remote-run")
	key := []byte("01234567890123456789012345678901")
	instanceStore, err := models.NewInstanceStore(db, key)
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, new(false))
	require.NoError(t, err)
	indexerStore, err := models.NewTorznabIndexerStore(db, key)
	require.NoError(t, err)
	indexer, err := indexerStore.Create(ctx, "Indexer", "http://indexer.example.invalid", "api-key", nil, nil, true, 0, 30)
	require.NoError(t, err)

	lib := dialectPath(d, "/data/lib")
	dirStore := models.NewDirScanStore(db)
	dir, err := dirStore.CreateDirectory(ctx, &models.DirScanDirectory{Path: lib, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
	require.NoError(t, err)
	row := remoteInstance(models.SSHTierExec)
	row.ID = instance.ID

	film := d.Join(d.Join(lib, remoteRunFilmFolder), "film.mkv")
	seeded := d.Join(d.Join(lib, "Other.Film.2023.1080p.WEB.x264-GRP"), "other.mkv")
	renamed := d.Join(d.Join(lib, "Renamed.Film.2022.1080p.WEB.x264-GRP"), "renamed.mkv")
	const season = "Example.Show.S01.1080p.WEB-DL.x264-GRP"
	entry := func(p, rel string, mode fs.FileMode, size int64, id hardlink.FileID) fsops.WalkEntry {
		e := slashEntry(p, rel, mode)
		e.Size = size
		e.FileID = id
		return e
	}
	join := func(parts ...string) string { return strings.Join(parts, d.Separator()) }
	walk := []fsops.WalkEntry{entry(lib, ".", fs.ModeDir, 0, hardlink.FileID{})}
	for _, folder := range []string{season, remoteRunFilmFolder, d.Base(d.Dir(seeded)), d.Base(d.Dir(renamed)), "Disc 1"} {
		walk = append(walk, entry(d.Join(lib, folder), folder, fs.ModeDir, 0, hardlink.FileID{}))
	}
	for ep := 1; ep <= 2; ep++ {
		name := fmt.Sprintf("Example.Show.S01E%02d.1080p.WEB-DL.x264-GRP.mkv", ep)
		walk = append(walk, entry(d.Join(d.Join(lib, season), name), join(season, name), 0, 1000, remoteID(byte(10+ep))))
	}
	walk = append(walk,
		entry(film, join(remoteRunFilmFolder, "film.mkv"), 0, remoteRunFilmSize, remoteID(1)),
		entry(seeded, join(d.Base(d.Dir(seeded)), "other.mkv"), 0, 500, remoteID(2)),
		entry(renamed, join(d.Base(d.Dir(renamed)), "renamed.mkv"), 0, 700, remoteID(3)),
		entry(d.Join(d.Join(lib, "Disc 1"), "Boxed.Film.2021.1080p.WEB.x264-GRP.mkv"), join("Disc 1", "Boxed.Film.2021.1080p.WEB.x264-GRP.mkv"), 0, 900, remoteID(4)),
	)
	seedSave := dialectPath(d, "/data/seed")
	backend := newAnswerBackend(func(p string) (*fsops.LstatInfo, error) {
		switch {
		case p == seedSave || p == d.Join(lib, remoteRunFilmFolder):
			return &fsops.LstatInfo{Path: p, IsDir: true}, nil
		case p == d.Join(seedSave, "seed.mkv"):
			return &fsops.LstatInfo{Path: p, Size: 500, FileID: remoteID(2)}, nil
		}
		return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}, walk...)
	backend.d = d

	modTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	for i := range walk {
		walk[i].ModTime = modTime
	}
	require.NoError(t, dirStore.UpsertFile(ctx, &models.DirScanFile{
		DirectoryID: dir.ID, FilePath: dialectPath(d, "/data/lib/Old.Name/renamed.mkv"), FileSize: 700, FileModTime: modTime,
		FileID: append(remoteID(3).Bytes(), endpointTag(row)...), Status: models.DirScanFileStatusNoMatch,
	}))

	torrent := &metainfo.Info{Name: remoteRunFilmRoot, PieceLength: 16384, Pieces: []byte(strings.Repeat("p", 20)), Files: []metainfo.FileInfo{{Length: remoteRunFilmSize, Path: []string{"film.mkv"}}}}
	torrentBytes := buildTorrentBytes(t, torrent)
	cache := models.NewTorznabTorrentCacheStore(db)
	require.NoError(t, cache.Store(ctx, &models.TorznabTorrentCacheEntry{IndexerID: indexer.ID, CacheKey: "film", TorrentData: torrentBytes}))
	searcher := &remoteRunSearcher{indexerID: indexer.ID, results: map[string][]jackett.SearchResult{}}
	filmResult := jackett.SearchResult{Title: remoteRunFilmRoot, IndexerID: indexer.ID, DownloadURL: "http://indexer.example.invalid/dl/film", GUID: "film", Size: remoteRunFilmSize}
	searcher.results["Synthetic Film"] = []jackett.SearchResult{filmResult}

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	manager := &alignFakeManager{hash: parsed.InfoHash, files: qbt.TorrentFiles{{Name: remoteRunFilmRoot + "/film.mkv", Size: remoteRunFilmSize}}}
	adder := testAdder(manager, (&settleFake{}).idle(), nil)
	cacheDB := &cacheTouchDB{DB: db, done: make(chan struct{}, 8)}
	parser := NewParser(nil)
	pool := fsops.NewPoolWithRemote(store(row), &remoteSpy{}, func(*models.Instance) fsops.Backend { return backend })
	svc := NewService(DefaultConfig(), dirStore, nil, nil, nil, nil, nil, nil, pool, adder)
	svc.parser = parser
	svc.torrentFiles = onlyTorrent(seedSave, "seed.mkv")
	svc.jackettService = jackett.NewService(indexerStore, jackett.WithTorrentCache(models.NewTorznabTorrentCacheStore(cacheDB)))
	svc.searcher = NewSearcher(searcher, parser)
	svc.searchGate = searchGate{interval: time.Nanosecond}
	svc.injector = NewInjector(nil, crossseed.NewAligner(manager), adder, nil, nil, pool)
	return &remoteRunFixture{svc: svc, store: dirStore, dir: dir, row: row, backend: backend, searcher: searcher, manager: manager, cacheDB: cacheDB, d: d}
}

// run runs one scan to its end, and joins the detached touch of each cached download before cleanup.
func (f *remoteRunFixture) run(t *testing.T) *models.DirScanRun {
	t.Helper()
	ctx := t.Context()
	runID, err := f.store.CreateRun(ctx, f.dir.ID, triggeredByWebhook, f.dir.Path)
	require.NoError(t, err)

	f.svc.executeScan(ctx, f.dir.ID, runID)

	for range f.searcher.answered {
		select {
		case <-f.cacheDB.done:
		case <-time.After(10 * time.Second):
			t.Fatal("torrent cache touch did not finish")
		}
	}
	run, err := f.store.GetRun(ctx, runID)
	require.NoError(t, err)
	return run
}

func (f *remoteRunFixture) statuses(t *testing.T) map[string]models.DirScanFileStatus {
	t.Helper()
	files, err := f.store.ListFiles(t.Context(), f.dir.ID, nil, 50, 0)
	require.NoError(t, err)
	statuses := make(map[string]models.DirScanFileStatus, len(files))
	for _, file := range files {
		statuses[file.FilePath] = file.Status
	}
	return statuses
}

// sshCreds is the credential source of a pool that reaches one test server.
type sshCreds struct {
	row *models.Instance
	pin []byte
}

func (c sshCreds) Get(context.Context, int) (*models.Instance, error) { return c.row, nil }
func (c sshCreds) GetDecryptedSSHKey(*models.Instance) (string, error) {
	return sshtest.PrivateKey(""), nil
}
func (c sshCreds) GetHostKeyPin(*models.Instance) ([]byte, error) { return c.pin, nil }
func (sshCreds) SetSSHProbe(context.Context, *models.Instance, models.SSHTier, bool) error {
	return nil
}

// Sftp can redial here, so only the walk's cut keeps a seeded file with no row from reading as new and going to the indexers.
// regression: #3838
func TestScanFailsWhenFindLosesTheConnection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fake exec handler runs the local GNU find and stat")
	}
	ctx := t.Context()
	f := newRemoteRun(t, fsops.SlashPaths, func(row *models.Instance) instanceGetter { return &fakeInstanceStore{instance: row} })

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)
	host, port, err := net.SplitHostPort(server.Addr)
	require.NoError(t, err)
	f.row.SSHHost = host
	f.row.SSHPort, err = strconv.Atoi(port)
	require.NoError(t, err)
	f.row.SSHUsername = "qui"
	sshPool := sshpool.NewPool(sshpool.NewDialer(sshCreds{row: f.row, pin: hostKey.PublicKey().Marshal()}))
	t.Cleanup(sshPool.Close)
	f.svc.backendPool = fsops.NewPoolWithRemote(&fakeInstanceStore{instance: f.row}, &remoteSpy{}, func(row *models.Instance) fsops.Backend { return remote.New(sshPool, row) })

	lib, seed := t.TempDir(), t.TempDir()
	const name = "Synthetic.Film.2020.1080p.WEB.x264-GRP.mkv"
	tracked := filepath.Join(lib, "Tracked.Film.2019.1080p.WEB.x264-GRP.mkv")
	require.NoError(t, os.WriteFile(filepath.Join(lib, name), []byte("seeded"), 0o600))
	require.NoError(t, os.WriteFile(tracked, []byte("tracked"), 0o600))
	require.NoError(t, os.Link(filepath.Join(lib, name), filepath.Join(seed, name)))
	f.svc.torrentFiles = onlyTorrent(seed, name)
	f.dir, err = f.store.CreateDirectory(ctx, &models.DirScanDirectory{Path: lib, Enabled: true, TargetInstanceID: f.row.ID, ScanIntervalMinutes: 60})
	require.NoError(t, err)
	info, err := os.Stat(tracked)
	require.NoError(t, err)
	require.NoError(t, f.store.UpsertFile(ctx, &models.DirScanFile{
		DirectoryID: f.dir.ID, FilePath: tracked, FileSize: info.Size(), FileModTime: info.ModTime(), Status: models.DirScanFileStatusNoMatch,
	}))
	before := f.statuses(t)

	server.DropOnExec("-mindepth", "")
	run := f.run(t)
	require.Equal(t, models.DirScanRunStatusFailed, run.Status)
	require.Contains(t, run.ErrorMessage, fsops.ErrConnectionLost.Error())
	require.Empty(t, f.searcher.queries, "a failed walk must reach no indexer")
	require.Equal(t, before, f.statuses(t))

	// The pool hands out the dropped client until its watcher clears the entry.
	backend, _, err := f.svc.backendPool.Require(ctx, f.row.ID, models.CapabilityRead)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := backend.Lstat(ctx, lib)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond)

	run = f.run(t)
	require.Equal(t, models.DirScanRunStatusSuccess, run.Status, run.ErrorMessage)
	require.Equal(t, models.DirScanFileStatusAlreadySeeding, f.statuses(t)[filepath.Join(lib, name)], "the next run reads the identity again")
}

// One run on a remote instance in each dialect: the walk, the seeding index, a rename kept by identity, the
// TV grouping, the search metadata read from a file name, and an add aligned to the on-disk folder.
func TestExecuteScanWalksRemoteInstance(t *testing.T) {
	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			f := newRemoteRun(t, tt.d, func(row *models.Instance) instanceGetter { return &fakeInstanceStore{instance: row} })

			run := f.run(t)

			require.Equal(t, models.DirScanRunStatusSuccess, run.Status, run.ErrorMessage)
			require.Equal(t, []string{"Boxed Film", "Boxed Film", "Example Show", "Example Show E01", "Example Show E02", "Synthetic Film"}, f.searcher.queries)
			lib := tt.d.Join(dialectPath(tt.d, "/data"), "lib")
			inLib := func(folder, name string) string { return tt.d.Join(tt.d.Join(lib, folder), name) }
			require.Equal(t, map[string]models.DirScanFileStatus{
				inLib("Disc 1", "Boxed.Film.2021.1080p.WEB.x264-GRP.mkv"):                                        models.DirScanFileStatusNoMatch,
				inLib("Example.Show.S01.1080p.WEB-DL.x264-GRP", "Example.Show.S01E01.1080p.WEB-DL.x264-GRP.mkv"): models.DirScanFileStatusNoMatch,
				inLib("Example.Show.S01.1080p.WEB-DL.x264-GRP", "Example.Show.S01E02.1080p.WEB-DL.x264-GRP.mkv"): models.DirScanFileStatusNoMatch,
				inLib(remoteRunFilmFolder, "film.mkv"):                                                           models.DirScanFileStatusMatched,
				inLib("Other.Film.2023.1080p.WEB.x264-GRP", "other.mkv"):                                         models.DirScanFileStatusAlreadySeeding,
				inLib("Renamed.Film.2022.1080p.WEB.x264-GRP", "renamed.mkv"):                                     models.DirScanFileStatusNoMatch,
			}, f.statuses(t), "the seeded file by inode, the renamed file by its stored identity")
			require.Equal(t, lib, f.manager.addOptions["savepath"])
			require.Equal(t, [][2]string{{remoteRunFilmRoot, remoteRunFilmFolder}}, f.manager.folderRenames, "the add aligns to the folder's name in the backend's dialect")
		})
	}
}

// The row changes after the walk's recheck, so the search phase runs and only its add is refused, with
// nothing read on the new server.
func TestAddRefusedWhenEndpointChangesDuringRun(t *testing.T) {
	f := newRemoteRun(t, fsops.SlashPaths, func(row *models.Instance) instanceGetter {
		other := *row
		other.SSHHost = "other.example.invalid"
		return &countingInstanceStore{first: row, later: &other, firstReads: 2}
	})

	run := f.run(t)

	require.Equal(t, models.DirScanRunStatusSuccess, run.Status, run.ErrorMessage)
	require.Contains(t, f.searcher.queries, "Synthetic Film")
	require.Nil(t, f.manager.addOptions, "nothing may be added over paths read on another server")
	film := "/data/lib/" + remoteRunFilmFolder
	require.NotContains(t, f.backend.stated, film, "the new server must see none of the reads")
	require.Equal(t, models.DirScanFileStatusError, f.statuses(t)[film+"/film.mkv"])
	injections, err := f.store.ListRunInjections(t.Context(), f.dir.ID, run.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, injections, 1)
	require.Contains(t, injections[0].ErrorMessage, errRemoteTargetChanged.Error())
}
