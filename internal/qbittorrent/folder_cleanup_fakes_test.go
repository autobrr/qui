// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// fakeRequirer hands out one backend and instance row, whatever the row's
// capabilities, so a test can model a remote instance that can write.
type fakeRequirer struct {
	backend  fsops.Backend
	instance models.Instance
	err      error
}

func (f *fakeRequirer) Require(_ context.Context, instanceID int, _ models.FilesystemCapability, _ ...models.FilesystemCapability) (fsops.Backend, *models.Instance, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	inst := f.instance
	inst.ID = instanceID
	return f.backend, &inst, nil
}

type fakeView struct {
	mu          sync.Mutex
	torrents    []qbt.Torrent
	folders     InstanceFolders
	liveErr     error
	foldersErr  error
	refreshes   atomic.Int32
	folderReads atomic.Int32
	// synced is when the last hinted sync landed, fakeSyncDelay after the
	// hint, as the sync manager's debounce does.
	synced time.Time
	// onSync, when set, is what qBittorrent holds but the cache does not
	// show yet: a hinted sync replaces torrents with it.
	onSync []qbt.Torrent
	// syncFails makes a hinted sync fail, so synced does not move.
	syncFails bool
	// lastSyncErr fails lastSync alone.
	lastSyncErr error
}

const fakeSyncDelay = 200 * time.Millisecond

func (v *fakeView) liveTorrents(context.Context, int) ([]qbt.Torrent, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]qbt.Torrent(nil), v.torrents...), v.liveErr
}

func (v *fakeView) ownRows(_ context.Context, _ int, hashes []string) (map[string]qbt.Torrent, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.liveErr != nil {
		return nil, v.liveErr
	}
	want := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		want[canonicalizeHash(hash)] = struct{}{}
	}
	rows := make(map[string]qbt.Torrent)
	for _, t := range v.torrents {
		if _, ok := want[canonicalizeHash(t.Hash)]; ok {
			rows[canonicalizeHash(t.Hash)] = t
		}
	}
	return rows, nil
}

func (v *fakeView) InstanceFolders(context.Context, fsops.PathDialect, int) (InstanceFolders, error) {
	v.folderReads.Add(1)
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.folders, v.foldersErr
}

func (v *fakeView) hintRefresh(int) {
	v.refreshes.Add(1)
	time.AfterFunc(fakeSyncDelay, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.syncFails {
			return
		}
		if v.onSync != nil {
			v.torrents, v.onSync = v.onSync, nil
		}
		v.synced = time.Now()
	})
}

func (v *fakeView) lastSync(context.Context, int) (time.Time, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.lastSyncErr != nil {
		return time.Time{}, v.lastSyncErr
	}
	return v.synced, v.liveErr
}

// setTorrentsOnSync changes qBittorrent's torrents without the cache seeing
// it until the next hinted sync.
func (v *fakeView) setTorrentsOnSync(torrents ...qbt.Torrent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onSync = torrents
}

func (v *fakeView) setTorrents(torrents ...qbt.Torrent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.torrents = torrents
}

// fakeIgnores matches with orphan scan's rule: the path is ignored, is under an
// ignored path, or holds one.
type fakeIgnores struct {
	paths []string
	err   error
}

func (f *fakeIgnores) IgnoredPathMatcher(_ context.Context, _ int, d fsops.PathDialect) (func(string) bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return func(p string) bool {
		key := fsops.NormalizePath(d, p)
		for _, ignored := range f.paths {
			ignoredKey := fsops.NormalizePath(d, ignored)
			if pathAtOrUnder(d, key, ignoredKey) || pathUnder(d, ignoredKey, key) {
				return true
			}
		}
		return false
	}, nil
}

// treeFS is a backend a test can lay files out on.
type treeFS interface {
	fsops.Backend
	mkdir(p string)
	write(p string)
	symlink(p string)
	remove(p string)
	exists(p string) bool
}

type localTree struct {
	*local.Backend
	t *testing.T
}

func (l localTree) mkdir(p string) { require.NoError(l.t, os.MkdirAll(p, 0o755)) }
func (l localTree) write(p string) {
	l.mkdir(filepath.Dir(p))
	require.NoError(l.t, os.WriteFile(p, []byte("x"), 0o600))
}

func (l localTree) symlink(p string) {
	target := filepath.Join(l.t.TempDir(), "target")
	require.NoError(l.t, os.MkdirAll(target, 0o755))
	l.mkdir(filepath.Dir(p))
	require.NoError(l.t, os.Symlink(target, p))
}

func (l localTree) remove(p string) { require.NoError(l.t, os.RemoveAll(p)) }
func (l localTree) exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// memTree is a slash-dialect filesystem in memory: the shape of a remote unix
// host on any OS. A symlink is an entry that reports itself as one and holds
// whatever the test puts below its path.
type memTree struct {
	fsops.Backend
	mu        sync.Mutex
	kinds     map[string]memKind
	removeErr map[string]error
	// readErr, when set, fails a ReadDir of p with the error it returns, and
	// lstatErr an Lstat.
	readErr  func(p string) error
	lstatErr func(p string) error
	calls    atomic.Int32
}

type memKind uint8

const (
	memDir memKind = iota
	memFile
	memLink
	// memJunction is a Windows directory junction as Go reports it since
	// 1.23: neither a folder nor a symlink, with ModeIrregular.
	memJunction
)

func (k memKind) mode() fs.FileMode {
	switch k {
	case memDir:
		return fs.ModeDir
	case memLink:
		return fs.ModeSymlink
	case memJunction:
		return fs.ModeIrregular
	case memFile:
	}
	return 0
}

func newMemTree() *memTree {
	return &memTree{kinds: map[string]memKind{"/": memDir}, removeErr: map[string]error{}}
}

func (m *memTree) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (m *memTree) mkdir(p string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mkdirLocked(p)
}

func (m *memTree) mkdirLocked(p string) {
	for dir := path.Clean(p); dir != "/"; dir = path.Dir(dir) {
		if _, ok := m.kinds[dir]; !ok {
			m.kinds[dir] = memDir
		}
	}
}

func (m *memTree) write(p string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mkdirLocked(path.Dir(p))
	m.kinds[p] = memFile
}

func (m *memTree) symlink(p string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mkdirLocked(path.Dir(p))
	m.kinds[p] = memLink
}

func (m *memTree) junction(p string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mkdirLocked(path.Dir(p))
	m.kinds[p] = memJunction
}

func (m *memTree) remove(p string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.kinds {
		if name == p || strings.HasPrefix(name, p+"/") {
			delete(m.kinds, name)
		}
	}
}

func (m *memTree) exists(p string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.kinds[p]
	return ok
}

func notExist(op, p string) error { return &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist} }

func (m *memTree) Lstat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	kind, ok := m.kinds[p]
	if !ok {
		return nil, notExist("lstat", p)
	}
	if m.lstatErr != nil {
		if err := m.lstatErr(p); err != nil {
			return nil, &fs.PathError{Op: "lstat", Path: p, Err: err}
		}
	}
	return &fsops.LstatInfo{Path: p, IsDir: kind == memDir, IsSymlink: kind == memLink, Mode: kind.mode()}, nil
}

func (m *memTree) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	kind, ok := m.kinds[p]
	if !ok {
		return nil, notExist("stat", p)
	}
	return &fsops.LstatInfo{Path: p, IsDir: kind != memFile}, nil
}

func (m *memTree) ReadDir(_ context.Context, p string) ([]fsops.DirEntry, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind, ok := m.kinds[p]; !ok || kind == memFile {
		return nil, notExist("readdir", p)
	}
	if m.readErr != nil {
		if err := m.readErr(p); err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: p, Err: err}
		}
	}
	var entries []fsops.DirEntry
	for name, kind := range m.kinds {
		if name != "/" && path.Dir(name) == p {
			entries = append(entries, fsops.DirEntry{Name: path.Base(name), IsDir: kind == memDir, IsSymlink: kind == memLink})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (m *memTree) Remove(_ context.Context, p string, opts fsops.RemoveOptions) error {
	m.calls.Add(1)
	if opts.Recursive {
		panic("folder cleanup must never remove recursively")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.removeErr[p]; err != nil {
		return &fs.PathError{Op: "remove", Path: p, Err: err}
	}
	if _, ok := m.kinds[p]; !ok {
		return notExist("remove", p)
	}
	for name := range m.kinds {
		if strings.HasPrefix(name, p+"/") {
			return &fs.PathError{Op: "remove", Path: p, Err: syscall.ENOTEMPTY}
		}
	}
	delete(m.kinds, p)
	return nil
}

// cleanupEnv is the TRaSH Guides layout under root, on one backend:
//
//	torrents/              default save path
//	torrents/incomplete/   download path
//	torrents/movies/       category "movies"
//	torrents/movies hd/    category "movies:hd"
//	torrents/tv/           category "tv"
//	torrents/tv/anime/     subcategory "tv/anime"
//	torrents/qui-links/    hardlink base dir
//	media/                 library
type cleanupEnv struct {
	t       *testing.T
	fs      treeFS
	d       fsops.PathDialect
	root    string
	view    *fakeView
	ignores *fakeIgnores
	req     *fakeRequirer
	fc      *FolderCleanup
	hashes  int
}

func (e *cleanupEnv) p(rel string) string {
	if rel == "" {
		return e.root
	}
	return e.d.Join(e.root, e.d.FromSlash(rel))
}

// tree lays out paths relative to root: a trailing slash is a folder, anything
// else a file.
func (e *cleanupEnv) tree(rels ...string) {
	for _, rel := range rels {
		if before, ok := strings.CutSuffix(rel, "/"); ok {
			e.fs.mkdir(e.p(before))
		} else {
			e.fs.write(e.p(rel))
		}
	}
}

func (e *cleanupEnv) rm(rels ...string) {
	for _, rel := range rels {
		e.fs.remove(e.p(rel))
	}
}

func (e *cleanupEnv) requireTree(present, absent []string) {
	e.t.Helper()
	for _, rel := range present {
		require.True(e.t, e.fs.exists(e.p(rel)), "%s must stay", rel)
	}
	for _, rel := range absent {
		require.False(e.t, e.fs.exists(e.p(rel)), "%s must be removed", rel)
	}
}

func (e *cleanupEnv) nextHash() string {
	e.hashes++
	return fmt.Sprintf("%040x", e.hashes)
}

func (e *cleanupEnv) rootFolder(save, root string) folderSnapshot {
	return folderSnapshot{InstanceID: 1, Hash: e.nextHash(), SavePath: e.p(save), ContentPath: e.p(save + "/" + root),
		Layout: layoutRootFolder, ContentExisted: true}
}

func (e *cleanupEnv) singleFile(save, file string) folderSnapshot {
	return folderSnapshot{InstanceID: 1, Hash: e.nextHash(), SavePath: e.p(save), ContentPath: e.p(save + "/" + file),
		Layout: layoutSingleFile, ContentExisted: true}
}

func (e *cleanupEnv) noRoot(save string, files ...string) folderSnapshot {
	return folderSnapshot{InstanceID: 1, Hash: e.nextHash(), SavePath: e.p(save), ContentPath: e.p(save),
		Layout: layoutNoRootFolder, Files: files, ContentExisted: true}
}

// moved marks snap as taken before a move rather than a delete.
func moved(snap folderSnapshot) folderSnapshot {
	snap.Move = true
	return snap
}

// row is snap's own cached row, the state the sync cache still shows right
// after qBittorrent accepted the request.
func row(snap folderSnapshot) qbt.Torrent {
	return qbt.Torrent{Hash: snap.Hash, SavePath: snap.SavePath, ContentPath: snap.ContentPath, DownloadPath: snap.DownloadPath}
}

// tick lets the worker run n ticks on the bubble's fake clock.
func (e *cleanupEnv) tick(n int) {
	for range n {
		time.Sleep(e.fc.tick)
		synctest.Wait()
	}
}

func (e *cleanupEnv) run(snaps ...folderSnapshot) {
	e.fc.enqueue(snaps, 0)
	e.tick(1)
}

type backendKind string

const (
	localBackend backendKind = "local"
	slashBackend backendKind = "slash"
	// remoteBackend is the slash fake behind a remote instance row that can
	// write, checked every tick so the module tests run unchanged.
	remoteBackend backendKind = "remote"
)

// newStoppedCleanupEnv is newCleanupEnv before start, for a test that sets
// the worker's timing.
func newStoppedCleanupEnv(t *testing.T, kind backendKind) *cleanupEnv {
	e := &cleanupEnv{t: t, ignores: &fakeIgnores{}}
	switch kind {
	case localBackend:
		e.fs = localTree{Backend: local.NewBackend(), t: t}
		e.root = t.TempDir()
	case slashBackend, remoteBackend:
		e.fs = newMemTree()
		e.root = "/data"
	}
	e.d = e.fs.Paths()
	e.tree("torrents/incomplete/", "torrents/movies/", "torrents/movies hd/", "torrents/tv/anime/", "torrents/qui-links/", "media/")
	// Categories movies, movies:hd, tv and tv/anime inherit their folders.
	e.view = &fakeView{folders: InstanceFolders{
		DefaultSavePath:     e.p("torrents"),
		DownloadPath:        e.p("torrents/incomplete"),
		CategorySaveFolders: []string{e.p("torrents/movies"), e.p("torrents/movies hd"), e.p("torrents/tv"), e.p("torrents/tv/anime")},
		DownloadFolders: []string{e.p("torrents/incomplete/movies"), e.p("torrents/incomplete/movies hd"),
			e.p("torrents/incomplete/tv"), e.p("torrents/incomplete/tv/anime")},
	}}
	e.req = &fakeRequirer{backend: e.fs, instance: models.Instance{HasLocalFilesystemAccess: true, HardlinkBaseDir: e.p("torrents/qui-links")}}
	if kind == remoteBackend {
		e.req.instance = models.Instance{SSHHost: "box.example.invalid", SSHKeyEncrypted: "k", SSHHostKeyEncrypted: "h",
			HardlinkBaseDir: e.p("torrents/qui-links")}
	}
	e.fc = NewFolderCleanup(e.req, e.view, e.ignores)
	if kind == remoteBackend {
		e.fc.remoteTick = e.fc.tick
	}
	return e
}

func newCleanupEnv(t *testing.T, kind backendKind) *cleanupEnv {
	e := newStoppedCleanupEnv(t, kind)
	e.start()
	return e
}

func (e *cleanupEnv) start() {
	e.fc.Start(e.t.Context())
	e.t.Cleanup(e.fc.Stop)
}

// eachBackend runs body on the real local backend, on the slash-dialect fake,
// and on that fake behind a remote instance, each inside a synctest bubble so
// the ticker and deadline run on fake time.
func eachBackend(t *testing.T, body func(t *testing.T, e *cleanupEnv)) {
	for _, kind := range []backendKind{localBackend, slashBackend, remoteBackend} {
		t.Run(string(kind), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				body(t, newCleanupEnv(t, kind))
			})
		})
	}
}

func skipSymlinksOnWindows(t *testing.T, e *cleanupEnv) {
	if runtime.GOOS == "windows" && e.d == fsops.HostPaths {
		t.Skip("creating a symlink needs a privilege on Windows")
	}
}

// captureLogFields records, for each message logged at level or above while it
// is installed, the message and the string value of field, or the values of a
// list field joined by commas.
func captureLogFields(t *testing.T, level zerolog.Level, field string) func() [][2]string {
	var mu sync.Mutex
	var buf strings.Builder
	orig := log.Logger
	log.Logger = zerolog.New(lockedWriter{&mu, &buf}).Level(level)
	t.Cleanup(func() { log.Logger = orig })
	return func() [][2]string {
		mu.Lock()
		defer mu.Unlock()
		var out [][2]string
		for line := range strings.Lines(buf.String()) {
			var entry map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &entry))
			var value string
			switch v := entry[field].(type) {
			case string:
				value = v
			case []any:
				parts := make([]string, len(v))
				for i, part := range v {
					parts[i], _ = part.(string)
				}
				value = strings.Join(parts, ",")
			}
			out = append(out, [2]string{entry["message"].(string), value})
		}
		return out
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *strings.Builder
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// captureLogs records the messages logged at level or above while it is
// installed. Tests that use it must not run in parallel.
func captureLogs(t *testing.T, level zerolog.Level) func() []string {
	var mu sync.Mutex
	var msgs []string
	orig := log.Logger
	log.Logger = log.Logger.Hook(zerolog.HookFunc(func(_ *zerolog.Event, l zerolog.Level, msg string) {
		if l >= level {
			mu.Lock()
			msgs = append(msgs, msg)
			mu.Unlock()
		}
	}))
	t.Cleanup(func() { log.Logger = orig })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), msgs...)
	}
}
