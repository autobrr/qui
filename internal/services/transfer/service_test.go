// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha1"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/fsops/remote"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// show is a torrent of two files in the folder Show.S01.1080p-GRP, with
// pieces of 16 bytes: piece 2 holds the end of the first file and the start
// of the second.
type show struct {
	data  []byte
	hash  string
	files map[string][]byte
}

const showName = "Show.S01.1080p-GRP"

func newShow(t *testing.T, name string) show {
	t.Helper()
	files := map[string][]byte{
		"Show.S01E01.mkv": bytes.Repeat([]byte(name+"-1|"), 10)[:40],
		"Show.S01E02.mkv": bytes.Repeat([]byte(name+"-2|"), 10)[:30],
	}
	data, hash := qbtstub.NewContentTorrent(t, name, 16,
		qbtstub.ContentFile{Path: "Show.S01E01.mkv", Data: files["Show.S01E01.mkv"]},
		qbtstub.ContentFile{Path: "Show.S01E02.mkv", Data: files["Show.S01E02.mkv"]})
	return show{data: data, hash: hash, files: files}
}

// disk is qui's own disk. Its switches imitate a target that cannot confirm
// a flush, that cannot make a hardlink, that damages a byte, that has little
// free space, that is on another filesystem than the source, or that a test
// watches during the copy.
type disk struct {
	*local.Backend
	free            int64
	unconfirmed     bool
	noLinks         bool
	otherFilesystem bool
	// damage flips the byte at an offset of the file with this final name.
	damage  map[string]int64
	onWrite func(name string)
	// onStatfs runs at each read of the free space, before it answers.
	onStatfs func()
	// failRemove fails the removal of each name below the root that it
	// reports.
	failRemove func(name string) bool
	// failOpenRoots is how many of the next OpenRoot calls fail.
	failOpenRoots int
	// lostAt fails each call that it reports, as the remote backend fails it
	// on a dropped connection: op "lstat" below a root or on an SFTP
	// instance, "link" below a root, and "readdir" and "openroot" on this
	// disk.
	lostAt func(op, name string) bool

	mu      sync.Mutex
	writes  []diskWrite
	creates map[string]int
	// writers is how many writers are open, and mostWriters the most that
	// were open at one time.
	writers, mostWriters int
}

// diskWrite is one write of the copy: n bytes at an offset of a file.
type diskWrite struct {
	name  string
	at, n int64
}

func (d *disk) log() ([]diskWrite, map[string]int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.writes), maps.Clone(d.creates)
}

func (d *disk) Statfs(ctx context.Context, path string) (*fsops.StatfsResult, error) {
	if d.onStatfs != nil {
		d.onStatfs()
	}
	if d.free > 0 {
		return &fsops.StatfsResult{BytesAvailable: d.free, BytesTotal: d.free}, nil
	}
	return d.Backend.Statfs(ctx, path)
}

func (d *disk) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	if d.otherFilesystem {
		return false, nil
	}
	return d.Backend.SameFilesystem(ctx, p1, p2)
}

func (d *disk) ReadDir(ctx context.Context, dir string) ([]fsops.DirEntry, error) {
	if d.lostAt != nil && d.lostAt("readdir", dir) {
		return nil, &fs.PathError{Op: "readdir", Path: dir, Err: fsops.ErrConnectionLost}
	}
	return d.Backend.ReadDir(ctx, dir)
}

func (d *disk) OpenRoot(ctx context.Context, dir string) (fsops.Root, error) {
	if d.lostAt != nil && d.lostAt("openroot", dir) {
		return nil, &fs.PathError{Op: "openroot", Path: dir, Err: fsops.ErrConnectionLost}
	}
	if d.failOpenRoots > 0 {
		d.failOpenRoots--
		return nil, &fs.PathError{Op: "openat", Path: dir, Err: fs.ErrPermission}
	}
	root, err := d.Backend.OpenRoot(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &diskRoot{Root: root, d: d}, nil
}

type diskRoot struct {
	fsops.Root
	d *disk
}

// sftpFiles is an instance over SFTP whose writes the disk records and
// watches like its own.
type sftpFiles struct {
	*remote.Backend
	d *disk
}

func (f *sftpFiles) OpenRoot(ctx context.Context, dir string) (fsops.Root, error) {
	root, err := f.Backend.OpenRoot(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &diskRoot{Root: root, d: f.d}, nil
}

func (f *sftpFiles) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	if f.d.lostAt != nil && f.d.lostAt("lstat", p) {
		return nil, &fs.PathError{Op: "lstat", Path: p, Err: fsops.ErrConnectionLost}
	}
	return f.Backend.Lstat(ctx, p)
}

// Statfs reads the free space of the host folder where the SFTP server of
// sshtest cannot: pkg/sftp serves statvfs only on Linux, macOS and Plan 9.
func (f *sftpFiles) Statfs(ctx context.Context, p string) (*fsops.StatfsResult, error) {
	stat, err := f.Backend.Statfs(ctx, p)
	if err != nil && strings.Contains(err.Error(), syscall.ENOTSUP.Error()) {
		return f.d.Backend.Statfs(ctx, hostPath(p))
	}
	return stat, err
}

// hostPath is the host path of a path that pkg/sftp serves.
func hostPath(p string) string {
	if native, ok := strings.CutPrefix(p, "/"); ok && filepath.VolumeName(native) != "" {
		return filepath.FromSlash(native)
	}
	return p
}

func (r *diskRoot) Create(ctx context.Context, name string) (fsops.FileWriter, error) {
	w, err := r.Root.Create(ctx, name)
	if err != nil {
		return nil, err
	}
	r.d.mu.Lock()
	if r.d.creates == nil {
		r.d.creates = map[string]int{}
	}
	r.d.creates[name]++
	r.d.mu.Unlock()
	return r.d.opened(&diskWriter{FileWriter: w, d: r.d, name: name}), nil
}

func (r *diskRoot) Lstat(ctx context.Context, name string) (*fsops.LstatInfo, error) {
	if r.d.lostAt != nil && r.d.lostAt("lstat", name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fsops.ErrConnectionLost}
	}
	return r.Root.Lstat(ctx, name)
}

func (r *diskRoot) Remove(ctx context.Context, name string) error {
	if r.d.failRemove != nil && r.d.failRemove(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrPermission}
	}
	return r.Root.Remove(ctx, name)
}

func (r *diskRoot) OpenWrite(ctx context.Context, name string, offset int64) (fsops.FileWriter, error) {
	w, err := r.Root.OpenWrite(ctx, name, offset)
	if err != nil {
		return nil, err
	}
	return r.d.opened(&diskWriter{FileWriter: w, d: r.d, name: name, at: offset}), nil
}

type diskWriter struct {
	fsops.FileWriter
	d      *disk
	name   string
	at     int64
	closed bool
}

func (d *disk) opened(w *diskWriter) *diskWriter {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writers++
	d.mostWriters = max(d.mostWriters, d.writers)
	return w
}

func (w *diskWriter) Close() error {
	if !w.closed {
		w.closed = true
		w.d.mu.Lock()
		w.d.writers--
		w.d.mu.Unlock()
	}
	return w.FileWriter.Close()
}

func (w *diskWriter) Write(p []byte) (int, error) {
	if w.d.onWrite != nil {
		w.d.onWrite(w.name)
	}
	data := p
	for final, offset := range w.d.damage {
		if strings.HasPrefix(w.name, final+".qui-transfer-") && offset >= w.at && offset < w.at+int64(len(p)) {
			data = bytes.Clone(p)
			data[offset-w.at] ^= 0xff
		}
	}
	n, err := w.FileWriter.Write(data)
	w.d.mu.Lock()
	w.d.writes = append(w.d.writes, diskWrite{name: w.name, at: w.at, n: int64(n)})
	w.d.mu.Unlock()
	w.at += int64(n)
	return n, err
}

func (w *diskWriter) Flush() (bool, error) {
	confirmed, err := w.FileWriter.Flush()
	return confirmed && !w.d.unconfirmed, err
}

// rig is a Transfer source and a Transfer target, each with a stub
// qBittorrent. newRig puts the source on a seedbox that qui reaches over
// SFTP, and the target on qui's own disk.
type rig struct {
	t         *testing.T
	instances *models.InstanceStore
	store     *transfer.Store
	sync      *qbittorrent.SyncManager
	src, dst  *qbtstub.Server
	// ssh serves the files of the source, and dstSSH those of a target that
	// qui reaches over SFTP.
	ssh, dstSSH *sshtest.Server
	srcDir      string
	dstDir      string
	srcSide     side
	source      *models.Instance
	target      *models.Instance
	disk        *disk
	db          *database.DB
	dbPath      string
	deps        transfer.Deps
	svc         *transfer.Service
	cancel      context.CancelFunc

	clockMu sync.Mutex
	clockAt time.Time
}

func (r *rig) now() time.Time {
	r.clockMu.Lock()
	defer r.clockMu.Unlock()
	if r.clockAt.IsZero() {
		return clock
	}
	return r.clockAt
}

func (r *rig) setClock(at time.Time) {
	r.clockMu.Lock()
	defer r.clockMu.Unlock()
	r.clockAt = at
}

var clock = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// side is where qui reaches the files of an instance of the rig.
type side int

const (
	overSFTP side = iota
	onDisk
	// noAccess: qui reaches only the qBittorrent of the instance, not its
	// files.
	noAccess
)

func newRig(t *testing.T, options ...func(*transfer.Config)) *rig {
	t.Helper()
	return newRigOn(t, overSFTP, onDisk, options...)
}

func newRigOn(t *testing.T, source, target side, options ...func(*transfer.Config)) *rig {
	t.Helper()
	srcDir, dstDir := t.TempDir(), t.TempDir()
	dbPath := testdb.CloneMigratedSQLite(t, "transfer-service")
	r := &rig{t: t, srcDir: srcDir, dstDir: dstDir, srcSide: source, dbPath: dbPath, disk: &disk{Backend: local.NewBackend()}}
	// The servers start first, so that they stop after the pools of qui.
	r.src, r.dst = qbtstub.New(t, pathOn(source, srcDir)), qbtstub.New(t, pathOn(target, dstDir))
	r.dst.HashCheck = true
	r.ssh = sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecSFTPOnly)
	r.dstSSH = sshtest.NewServer(t, sshtest.NewSigner(), sshtest.ExecSFTPOnly)
	r.db = openDB(t, dbPath)
	r.deps, r.instances, r.sync = wire(t, r.db, r.disk, r.now)
	r.store = r.deps.Store

	r.source = r.instanceOn(source, r.ssh, "seedbox", r.src.URL)
	r.target = r.instanceOn(target, r.dstSSH, "homelab", r.dst.URL)
	for _, option := range options {
		option(&r.deps.Config)
	}
	r.start()
	return r
}

func openDB(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// wire builds the parts of qui that the Service uses, on db.
func wire(t *testing.T, db *database.DB, d *disk, now func() time.Time) (transfer.Deps, *models.InstanceStore, *qbittorrent.SyncManager) {
	t.Helper()
	instances, err := models.NewInstanceStore(db, sshtest.EncryptionKey())
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)
	sshPool := sshpool.NewPool(sshpool.NewDialer(instances))
	t.Cleanup(sshPool.Close)
	files := fsops.NewPoolWithRemote(instances, d, func(inst *models.Instance) fsops.Backend {
		return &sftpFiles{Backend: remote.New(sshPool, inst), d: d}
	})
	store, err := transfer.NewStore(t.Context(), db)
	require.NoError(t, err)
	groups := crossseed.NewService(instances, syncManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)
	groups.SetBackendPool(files)
	return transfer.Deps{
		Store:     store,
		Instances: instances,
		Sync:      syncManager,
		Files:     files,
		Groups:    groups,
		Now:       now,
		Config: transfer.Config{Polls: 20, CheckPolls: 20, PollInterval: time.Millisecond, FlushBytes: 32,
			Reconnects: 20, ReconnectWait: time.Millisecond, PeerPolls: 3},
	}, instances, syncManager
}

func (r *rig) start() {
	ctx, cancel := context.WithCancel(r.t.Context())
	r.cancel = cancel
	svc := transfer.NewService(ctx, r.deps)
	r.svc = svc
	r.t.Cleanup(func() { closeService(r.t, svc) })
}

// closeService closes svc and waits for its runs with no deadline.
func closeService(t *testing.T, svc *transfer.Service) {
	t.Helper()
	require.NoError(t, svc.Close(context.Background()))
}

// restart stops the Service, and starts qui again on the same database.
func (r *rig) restart() {
	r.t.Helper()
	closeService(r.t, r.svc)
	store, err := transfer.NewStore(r.t.Context(), r.db)
	require.NoError(r.t, err)
	r.store, r.deps.Store = store, store
	r.start()
}

// sshInstance adds an instance whose files qui reaches on the SSH server of
// the source.
func (r *rig) sshInstance(name, url string) *models.Instance {
	r.t.Helper()
	return r.sshInstanceOn(r.ssh, name, url)
}

func (r *rig) sshInstanceOn(server *sshtest.Server, name, url string) *models.Instance {
	r.t.Helper()
	ctx := r.t.Context()
	inst, err := r.instances.Create(ctx, name, url, "", "", nil, nil, false, new(false))
	require.NoError(r.t, err)
	host, portText, err := net.SplitHostPort(server.Addr)
	require.NoError(r.t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(r.t, err)
	require.NoError(r.t, r.instances.SetSSHCredentials(ctx, inst.ID, host, port, "qui", sshtest.PrivateKey("")))
	require.NoError(r.t, r.instances.SetHostKeyPin(ctx, inst.ID, host, port, server.HostKey.Marshal()))
	inst, err = r.instances.Get(ctx, inst.ID)
	require.NoError(r.t, err)
	return inst
}

// localInstance adds an instance whose files are on qui's own disk.
func (r *rig) localInstance(name, url string) *models.Instance {
	r.t.Helper()
	inst, err := r.instances.Create(r.t.Context(), name, url, "", "", nil, nil, false, new(true))
	require.NoError(r.t, err)
	return inst
}

func (r *rig) instanceOn(where side, server *sshtest.Server, name, url string) *models.Instance {
	r.t.Helper()
	switch where {
	case overSFTP:
		return r.sshInstanceOn(server, name, url)
	case noAccess:
		inst, err := r.instances.Create(r.t.Context(), name, url, "", "", nil, nil, false, new(false))
		require.NoError(r.t, err)
		return inst
	case onDisk:
	}
	return r.localInstance(name, url)
}

// pathOn is the host folder dir as the qBittorrent of an instance on where
// names it. Over SFTP that is the path that qui reaches: pkg/sftp names C:\x
// on Windows /C:/x.
func pathOn(where side, dir string) string {
	if where != overSFTP || filepath.VolumeName(dir) == "" {
		return dir
	}
	return "/" + filepath.ToSlash(dir)
}

// srcPath is a folder below srcDir as the qBittorrent of the source names it.
func (r *rig) srcPath(elem ...string) string {
	return pathOn(r.srcSide, filepath.Join(append([]string{r.srcDir}, elem...)...))
}

// seed writes the files of a torrent in the folder of the source and seeds
// it there.
func (r *rig) seed(s show) {
	r.t.Helper()
	write(r.t, filepath.Join(r.srcDir, showName), s.files)
	r.src.Seed(s.data, r.srcPath())
}

func write(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
}

func (r *rig) request(s show) transfer.Request {
	return transfer.Request{SourceInstanceID: r.source.ID, Hash: s.hash, TargetInstanceID: r.target.ID}
}

// run starts a Transfer and waits for its end.
func (r *rig) run(req transfer.Request) *transfer.Record {
	r.t.Helper()
	id, err := r.svc.Start(r.t.Context(), req)
	require.NoError(r.t, err)
	r.svc.Wait()
	record, err := r.store.Get(r.t.Context(), id)
	require.NoError(r.t, err)
	return record
}

// tree lists the files below dir with their content.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		rel, err := filepath.Rel(dir, path)
		require.NoError(t, err)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return out
}

func contents(s show, folder string) map[string]string {
	out := map[string]string{}
	for name, data := range s.files {
		out[folder+"/"+name] = string(data)
	}
	return out
}

// One Transfer copies the files, proves them, adds the torrent on the target
// in seed mode, starts it, and removes the torrent and its files from the
// source. A copy through qui records no hint.
func TestTransferMovesATorrent(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WaySFTP, record.Way)
	for _, line := range record.Log {
		require.Nil(t, line.Hint, line.Message)
	}
	require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has the files, and no temporary file")
	require.Equal(t, transfer.FlushConfirmed, record.FlushState)
	require.Equal(t, []byte{0b1111_1000}, record.Members[0].ProvenPieces, "the 5 pieces are Proven pieces")
	require.Equal(t, 1, r.dst.Writes("torrents/add"), "a confirmed flush of every piece needs no check on the target")
	add := r.dst.LastParams("torrents/add")
	require.Equal(t, "true", add.Get("seedMode"))
	require.Equal(t, "false", add.Get("autoTMM"))
	require.Equal(t, r.dstDir, add.Get("savepath"))
	seeding, err := r.sync.GetTorrentRefsFresh(t.Context(), r.target.ID, qbittorrent.TorrentRefFilter{Hashes: []string{s.hash}})
	require.NoError(t, err)
	require.Len(t, seeding, 1)
	require.Equal(t, "stalledUP", string(seeding[0].State))

	require.False(t, r.src.Has(s.hash))
	require.Empty(t, tree(t, r.srcDir))
	_, err = os.Stat(filepath.Join(r.srcDir, showName))
	require.ErrorIs(t, err, fs.ErrNotExist, "the emptied folder goes too")
	require.False(t, r.store.IsClaimed(r.source.ID, s.hash), "the finished Transfer releases the torrent")
}

// "After the transfer" decides what stays on the source, and "Leave stopped
// on the target" keeps the target torrent stopped.
func TestTransferFollowsTheAfterChoice(t *testing.T) {
	for _, tc := range []struct {
		name          string
		options       transfer.Options
		sourceTorrent bool
		sourceStopped bool
		targetState   string
	}{
		{name: "keep seeding on both", options: transfer.Options{After: transfer.AfterKeepSeeding}, sourceTorrent: true, targetState: "stalledUP"},
		{name: "leave stopped on the target", options: transfer.Options{After: transfer.AfterKeepSeeding, LeaveStopped: true},
			sourceTorrent: true, targetState: "stoppedUP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			req.Options = tc.options

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, showName), tree(t, r.srcDir), "the source keeps its files")
			require.Equal(t, tc.sourceTorrent, r.src.Has(s.hash))
			if tc.sourceTorrent {
				require.False(t, r.src.Stopped(s.hash), "the source seeds on")
			}
			seeding, err := r.sync.GetTorrentRefsFresh(t.Context(), r.target.ID, qbittorrent.TorrentRefFilter{Hashes: []string{s.hash}})
			require.NoError(t, err)
			require.Len(t, seeding, 1)
			require.Equal(t, tc.targetState, string(seeding[0].State))
		})
	}
}

// With no save path, a Transfer lands at the save path of its category on
// the target, as qBittorrent places a torrent of that category.
// regression: #4242
func TestTransferPutsATorrentAtTheSavePathOfItsCategory(t *testing.T) {
	for _, tc := range []struct {
		name         string
		categoryPath string
		folder       string
	}{
		{name: "category with its own save path", categoryPath: "sorted", folder: "sorted"},
		{name: "category without a save path", folder: "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			if tc.categoryPath != "" {
				r.dst.AddCategory("archive", filepath.Join(r.dstDir, tc.categoryPath))
			} else {
				r.dst.AddCategory("archive", "")
			}
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			req.Placement.Category = "archive"

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, tc.folder+"/"+showName), tree(t, r.dstDir))
			landed, err := r.sync.GetTorrentRefsFresh(t.Context(), r.target.ID, qbittorrent.TorrentRefFilter{Hashes: []string{s.hash}})
			require.NoError(t, err)
			require.Len(t, landed, 1)
			require.Equal(t, "archive", landed[0].Category)
			require.Equal(t, filepath.Join(r.dstDir, tc.folder), landed[0].SavePath)
		})
	}
}

// A Transfer with no save path leaves the torrent on Automatic Torrent
// Management when that is the default mode of the target, as qBittorrent
// does for a torrent that a user adds there. A save path in the request
// keeps the torrent on manual management.
// regression: #4242
func TestTransferLeavesATorrentOnAutomaticManagement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		automatic bool
		category  string
		savePath  string
		folder    string
		managed   bool
	}{
		{name: "the target manages a category", automatic: true, category: "archive", folder: "sorted", managed: true},
		{name: "the target manages no category", automatic: true, managed: true},
		{name: "the request names a save path", automatic: true, category: "archive", savePath: "picked", folder: "picked"},
		{name: "the target adds on manual management", category: "archive", folder: "sorted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.dst.AutoManagement = tc.automatic
			r.dst.AddCategory("archive", filepath.Join(r.dstDir, "sorted"))
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			req.Placement = transfer.Placement{Category: tc.category}
			if tc.savePath != "" {
				req.Placement.SavePath = filepath.Join(r.dstDir, tc.savePath)
			}

			record := r.run(req)

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, path.Join(tc.folder, showName)), tree(t, r.dstDir))
			landed, err := r.sync.GetTorrentRefsFresh(t.Context(), r.target.ID, qbittorrent.TorrentRefFilter{Hashes: []string{s.hash}})
			require.NoError(t, err)
			require.Len(t, landed, 1)
			require.Equal(t, tc.managed, landed[0].AutoManaged)
			require.Equal(t, filepath.Join(r.dstDir, tc.folder), landed[0].SavePath)
			require.True(t, r.seeding(r.target.ID, s.hash), "the target seeds the torrent")
		})
	}
}

// Each entry check refuses with its reason, and records no Transfer.
func TestTransferRefuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(r *rig, s show) transfer.Request
		reason string
		moving bool
	}{
		{name: "a source before qBittorrent 5.1", setup: func(r *rig, s show) transfer.Request {
			r.src.WebAPIVersion = "2.11.2"
			return r.request(s)
		}, reason: "Transfer needs qBittorrent 5.1 or newer. Update qBittorrent on seedbox."},
		{name: "a target before qBittorrent 5.1", setup: func(r *rig, s show) transfer.Request {
			r.dst.WebAPIVersion = "2.11.2"
			return r.request(s)
		}, reason: "Transfer needs qBittorrent 5.1 or newer. Update qBittorrent on homelab."},
		{name: "an incomplete torrent", setup: func(r *rig, s show) transfer.Request {
			r.src.HashCheck = true
			require.NoError(t, os.WriteFile(filepath.Join(r.srcDir, showName, "Show.S01E02.mkv"), []byte("damaged"), 0o600))
			require.NoError(t, r.sync.BulkAction(t.Context(), r.source.ID, []string{s.hash}, "recheck"))
			return r.request(s)
		}, reason: "Only complete torrents can move"},
		{name: "a torrent with skipped files", setup: func(r *rig, s show) transfer.Request {
			require.NoError(t, r.sync.SetTorrentFilePriority(t.Context(), r.source.ID, s.hash, []int{1}, 0))
			return r.request(s)
		}, reason: "Torrents with skipped files are unsupported"},
		{name: "a hybrid torrent", setup: func(r *rig, s show) transfer.Request {
			r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.InfohashV2 = strings.Repeat("ab", 32) })
			return r.request(s)
		}, reason: "v2 and hybrid torrents are unsupported"},
		// regression: #4231
		{name: "a source that swapped two file names", setup: func(r *rig, s show) transfer.Request {
			r.renameOnSource(s, "Show.S01E01.mkv", "Show.S01E01.tmp", "Show.S01E02.mkv", "Show.S01E01.mkv",
				"Show.S01E01.tmp", "Show.S01E02.mkv")
			return r.request(s)
		}, reason: showName + "/Show.S01E02.mkv has the original name of another file of " + showName + "."},
		// regression: #4231
		{name: "a source that moved a file to the original name of another", setup: func(r *rig, s show) transfer.Request {
			r.renameOnSource(s, "Show.S01E01.mkv", "Show.S01E00.mkv", "Show.S01E02.mkv", "Show.S01E01.mkv")
			return r.request(s)
		}, reason: showName + "/Show.S01E01.mkv has the original name of another file of " + showName + "."},
		// regression: #4231
		{name: "a source that moved a file to the original name of another in other case", setup: func(r *rig, s show) transfer.Request {
			r.renameOnSource(s, "Show.S01E01.mkv", "Show.S01E00.mkv", "Show.S01E02.mkv", "show.s01e01.mkv")
			return r.request(s)
		}, reason: showName + "/show.s01e01.mkv has the original name of another file of " + showName + "."},
		// regression: #4231
		{name: "a source without a common folder that moved a file to the original name of another", setup: func(r *rig, s show) transfer.Request {
			for _, rename := range [][2]string{{"Show.S01E01.mkv", "Show.S01E02.mkv"}, {"Show.S01E02.mkv", "Extras/Show.S01E02.mkv"}} {
				require.NoError(t, r.sync.RenameTorrentFile(t.Context(), r.source.ID, s.hash, showName+"/"+rename[0], rename[1]))
			}
			return r.request(s)
		}, reason: "Show.S01E02.mkv has the original name of another file of " + showName + "."},
		// regression: #4231
		{name: "a torrent that qBittorrent moves", setup: func(r *rig, s show) transfer.Request {
			r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.Moving = true })
			return r.request(s)
		}, reason: "qBittorrent is moving " + showName + ".", moving: true},
		// regression: #4231
		{name: "a group member that qBittorrent moves", setup: func(r *rig, s show) transfer.Request {
			episode := r.seedEpisode(s)
			r.src.Edit(episode, func(t *qbtstub.Torrent) { t.Moving = true })
			return r.request(s)
		}, reason: "Show.S01E01.mkv shares the files of this torrent: qBittorrent is moving Show.S01E01.mkv.", moving: true},
		{name: "a torrent already on the target", setup: func(r *rig, s show) transfer.Request {
			write(t, filepath.Join(r.dstDir, showName), s.files)
			r.dst.Seed(s.data, r.dstDir)
			return r.request(s)
		}, reason: "Already on the target: homelab has this torrent."},
		{name: "a helper torrent", setup: func(r *rig, s show) transfer.Request {
			require.NoError(t, r.store.RegisterHelper(t.Context(), transfer.Helper{InstanceID: r.source.ID, Hash: s.hash,
				Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: 1}))
			return r.request(s)
		}, reason: "qui added this torrent for a Transfer or a Share."},
		{name: "one instance on both sides", setup: func(r *rig, s show) transfer.Request {
			req := r.request(s)
			req.TargetInstanceID = r.source.ID
			return req
		}, reason: "The Transfer source and the Transfer target are the same instance."},
		{name: "a copy through qui to a target without file access", setup: func(r *rig, s show) transfer.Request {
			other, err := r.instances.Create(t.Context(), "other client", r.dst.URL, "", "", nil, nil, false, new(false))
			require.NoError(t, err)
			req := r.request(s)
			req.TargetInstanceID = other.ID
			req.Options.ForcedWay = transfer.WaySFTP
			return req
		}, reason: "A copy through qui needs file access to other client, on qui's own disk or over SSH."},
		{name: "an unknown way", setup: func(r *rig, s show) transfer.Request {
			req := r.request(s)
			req.Options.ForcedWay = "carrier pigeon"
			return req
		}, reason: `Unknown way: "carrier pigeon".`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			req := tc.setup(r, s)

			_, err := r.svc.Start(t.Context(), req)

			var refusal *transfer.RefusalError
			require.ErrorAs(t, err, &refusal)
			require.Contains(t, refusal.Reason, tc.reason)
			require.Equal(t, tc.moving, errors.Is(err, transfer.ErrMoving), "a rule waits for the end of a move")
			require.False(t, r.store.IsClaimed(r.source.ID, s.hash), "a refused Transfer claims nothing")
			require.Zero(t, r.dst.Writes("torrents/add"))
		})
	}
}

// renameOnSource renames files of s on the source, in pairs of the old and
// the new name below the folder of the show.
func (r *rig) renameOnSource(s show, names ...string) {
	r.t.Helper()
	for pair := range slices.Chunk(names, 2) {
		require.NoError(r.t, r.sync.RenameTorrentFile(r.t.Context(), r.source.ID, s.hash, showName+"/"+pair[0], showName+"/"+pair[1]))
	}
}

// A file that the source renamed to a name that no other file of the
// torrent had keeps that name on the target.
// regression: #4231
func TestTransferKeepsTheFileNamesOfTheSource(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.renameOnSource(s, "Show.S01E01.mkv", "Show.S01E00.mkv", "Show.S01E02.mkv", "Show.S01E03.mkv")

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, map[string]string{
		showName + "/Show.S01E00.mkv": string(s.files["Show.S01E01.mkv"]),
		showName + "/Show.S01E03.mkv": string(s.files["Show.S01E02.mkv"]),
	}, tree(t, r.dstDir))
	files, err := r.sync.GetTorrentFiles(qbittorrent.WithForceFilesRefresh(t.Context()), r.target.ID, s.hash)
	require.NoError(t, err)
	require.Equal(t, []string{showName + "/Show.S01E00.mkv", showName + "/Show.S01E03.mkv"},
		[]string{(*files)[0].Name, (*files)[1].Name})
}

// A file that the source renamed in case only keeps its new name on the
// target. On a target that ignores case, the old name finds the copy, which
// is no conflict, also on a target without file identities.
// regression: #4231
func TestTransferKeepsARenameInCase(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target side
	}{
		{name: "on disk", target: onDisk},
		{name: "over SFTP", target: overSFTP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, tc.target)
			s := newShow(t, showName)
			r.seed(s)
			r.renameOnSource(s, "Show.S01E01.mkv", "Show.S01e01.mkv")

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, map[string]string{
				showName + "/Show.S01e01.mkv": string(s.files["Show.S01E01.mkv"]),
				showName + "/Show.S01E02.mkv": string(s.files["Show.S01E02.mkv"]),
			}, tree(t, r.dstDir))
		})
	}
}

// ignoresCase reports whether the filesystem of dir finds a file under
// another case of its name.
func ignoresCase(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "case-probe")
	require.NoError(t, os.WriteFile(probe, nil, 0o600))
	defer func() { require.NoError(t, os.Remove(probe)) }()
	_, err := os.Lstat(filepath.Join(dir, "CASE-PROBE"))
	return err == nil
}

// seedEpisode seeds a torrent of one file that is a file of s, as a
// cross-seed of one episode does.
func (r *rig) seedEpisode(s show) string {
	r.t.Helper()
	episode := "Show.S01E01.mkv"
	file, hash := singleFileTorrent(r.t, episode, s.files[episode])
	r.src.Seed(file, r.srcPath(showName))
	return hash
}

func singleFileTorrent(t *testing.T, name string, data []byte) ([]byte, string) {
	t.Helper()
	info := metainfo.Info{Name: name, PieceLength: 16, Length: int64(len(data))}
	for chunk := range slices.Chunk(data, 16) {
		sum := sha1.Sum(chunk)
		info.Pieces = append(info.Pieces, sum[:]...)
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes(), mi.HashInfoBytes().HexString()
}

// A Transfer records the size of its torrent at the start, for the list of
// the Transfers page.
func TestTransferRecordsTheSizeOfItsTorrent(t *testing.T) {
	r := newRig(t)
	show := newShow(t, showName)
	r.seed(show)
	id, err := r.svc.Start(t.Context(), r.request(show))
	require.NoError(t, err)
	r.svc.Wait()
	record, err := r.store.Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, int64(70), record.Size, "the two files of the show hold 40 and 30 bytes")
}

// One Transfer runs at a time for each source. The next one waits.
func TestTransferQueuesBySource(t *testing.T) {
	r := newRig(t)
	first, second := newShow(t, showName), newShow(t, "Other.Show.S02.720p-GRP")
	r.seed(first)
	write(t, filepath.Join(r.srcDir, "Other.Show.S02.720p-GRP"), second.files)
	r.src.Seed(second.data, r.srcPath())
	copying, held := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	// Runs before the rig closes the Service, also when the test fails first.
	t.Cleanup(release)
	var once sync.Once
	r.disk.onWrite = func(string) {
		once.Do(func() {
			close(copying)
			<-held
		})
	}

	firstID, err := r.svc.Start(t.Context(), r.request(first))
	require.NoError(t, err)
	<-copying
	secondID, err := r.svc.Start(t.Context(), r.request(second))
	require.NoError(t, err)
	waiting, err := r.store.Get(t.Context(), secondID)
	require.NoError(t, err)
	require.Equal(t, transfer.StateWaiting, waiting.State)
	_, err = r.svc.Start(t.Context(), r.request(second))
	require.ErrorIs(t, err, transfer.ErrClaimed, "a waiting Transfer holds its torrent")
	release()
	r.svc.Wait()

	for _, id := range []int64{firstID, secondID} {
		record, err := r.store.Get(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	}
}

// A cancel of a Transfer that waits for its turn ends it at once. It never
// ran, so it changed nothing, and its end runs no cleanup on the instances.
// The Transfer that runs goes on.
func TestCancelOfAWaitingTransferRunsNoCleanup(t *testing.T) {
	r := newRig(t)
	first, second := newShow(t, showName), newShow(t, "Other.Show.S02.720p-GRP")
	r.seed(first)
	write(t, filepath.Join(r.srcDir, "Other.Show.S02.720p-GRP"), second.files)
	r.src.Seed(second.data, r.srcPath())
	copying, held := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	t.Cleanup(release)
	var once sync.Once
	r.disk.onWrite = func(string) {
		once.Do(func() {
			close(copying)
			<-held
		})
	}

	firstID, err := r.svc.Start(t.Context(), r.request(first))
	require.NoError(t, err)
	<-copying
	secondID, err := r.svc.Start(t.Context(), r.request(second))
	require.NoError(t, err)
	require.NoError(t, r.svc.Cancel(t.Context(), secondID))
	require.False(t, r.get(secondID).FinishedAt.IsZero(), "the Transfer ended before Cancel answered")
	release()
	r.svc.Wait()

	cancelled := r.get(secondID)
	require.Equal(t, transfer.StateCancelled, cancelled.State)
	steps := make([]string, 0, len(cancelled.Steps))
	for _, step := range cancelled.Steps {
		steps = append(steps, step.Name)
	}
	require.Equal(t, []string{"start", "cancelled"}, steps, "no cleanup step")
	require.False(t, r.store.IsClaimed(r.source.ID, second.hash), "the cancel frees the torrent")
	require.False(t, r.src.Stopped(second.hash))
	require.Equal(t, transfer.StateDone, r.get(firstID).State)
}

// regression: #4242
// A cancel that saved its state, but that a stop of qui cut before the end,
// ends the Transfer at the next start. The Transfer does not run.
func TestASavedCancelEndsTheTransferAtTheNextStart(t *testing.T) {
	r := newRig(t)
	// The first write of a cancel of a waiting Transfer, without the second.
	record := r.waiting("bbbb")
	record.State, record.RetryAt = transfer.StateCancelled, time.Time{}
	record.Steps = []transfer.Step{{Name: "start", At: r.now()}, {Name: string(transfer.StateCancelled), At: r.now()}}
	require.NoError(t, r.store.Save(t.Context(), record))
	require.True(t, r.store.IsClaimed(r.source.ID, "bbbb"))

	r.restart()
	r.svc.Wait()

	cancelled := r.get(record.ID)
	require.Equal(t, transfer.StateCancelled, cancelled.State)
	require.False(t, cancelled.FinishedAt.IsZero(), "the Transfer ended")
	steps := make([]string, 0, len(cancelled.Steps))
	for _, step := range cancelled.Steps {
		steps = append(steps, step.Name)
	}
	require.Equal(t, []string{"start", "cancelled"}, steps, "the Transfer did not run")
	require.False(t, r.store.IsClaimed(r.source.ID, "bbbb"), "the end frees the torrent")
}

// regression: #4242
// Cancel saves the cancel before it answers, also while the cleanup of
// another cancel runs: a stop of qui then cannot lose it, and the next start
// runs the cleanup at once.
func TestACancelIsSavedBeforeCancelAnswers(t *testing.T) {
	r := newRig(t)
	first, second := newShow(t, showName), newShow(t, "Other.Show.S02.720p-GRP")
	r.seed(first)
	write(t, filepath.Join(r.srcDir, "Other.Show.S02.720p-GRP"), second.files)
	r.src.Seed(second.data, r.srcPath())
	// Each one fails for a time after its copy, and keeps its files on the target.
	r.dst.Reject = http.StatusInternalServerError
	firstID, secondID := r.run(r.request(first)).ID, r.run(r.request(second)).ID
	r.dst.Reject = 0
	require.Equal(t, transfer.StateFailed, r.get(secondID).State)
	require.False(t, r.get(secondID).RetryAt.IsZero())

	removing, held := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	t.Cleanup(release)
	var once sync.Once
	r.disk.failRemove = func(string) bool {
		once.Do(func() {
			close(removing)
			<-held
		})
		return false
	}
	require.NoError(t, r.svc.Cancel(t.Context(), firstID))
	<-removing
	require.NoError(t, r.svc.Cancel(t.Context(), secondID))

	saved := r.get(secondID)
	require.Equal(t, transfer.StateCancelled, saved.State)
	require.True(t, saved.RetryAt.IsZero(), "the next start does not wait for the retry time")
	require.Equal(t, "clean up", saved.Steps[len(saved.Steps)-1].Name)

	release()
	r.svc.Wait()
	for _, id := range []int64{firstID, secondID} {
		record := r.get(id)
		require.Equal(t, transfer.StateCancelled, record.State, record.FailureReason)
		require.False(t, record.FinishedAt.IsZero())
	}
	require.Empty(t, tree(t, r.dstDir))
}

// A file at a path that the copy writes stops the Transfer, and qui leaves
// the file as it is. So does a file at the original name of a file that the
// source renamed: the rename after the add would move it over the copy.
func TestTransferStopsAtAConflict(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rename []string
		theirs string
		// sameSize gives the file in the way the size of the file of the
		// torrent at its name.
		sameSize bool
		// inCase needs a target that tells case apart, to hold a second file
		// at the old name.
		inCase bool
	}{
		{name: "a file at a name that the copy writes", theirs: "Show.S01E02.mkv"},
		// regression: #4231
		{name: "a file at the original name of a renamed file", rename: []string{"Show.S01E01.mkv", "Show.S01E00.mkv"},
			theirs: "Show.S01E01.mkv", sameSize: true},
		// regression: #4231
		{name: "a file at the original name of a file renamed in case", rename: []string{"Show.S01E01.mkv", "Show.S01e01.mkv"},
			theirs: "Show.S01E01.mkv", sameSize: true, inCase: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			if tc.inCase && ignoresCase(t, r.dstDir) {
				t.Skip("the target ignores case, so it cannot hold a second file at the old name")
			}
			s := newShow(t, showName)
			r.seed(s)
			r.renameOnSource(s, tc.rename...)
			theirs := []byte("theirs")
			if tc.sameSize {
				theirs = bytes.Repeat([]byte("t"), len(s.files[tc.theirs]))
			}
			write(t, filepath.Join(r.dstDir, showName), map[string][]byte{tc.theirs: theirs})

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateFailed, record.State, "target: %v", tree(t, r.dstDir))
			// regression: #4231
			require.Contains(t, record.FailureReason, "Conflict: a file is in the way on homelab at "+filepath.Join(r.dstDir, showName, tc.theirs))
			require.Equal(t, map[string]string{showName + "/" + tc.theirs: string(theirs)}, tree(t, r.dstDir))
			require.True(t, r.src.Has(s.hash))
			require.False(t, r.src.Stopped(s.hash))
		})
	}
}

// A look at the files that gets no answer, as on a dropped SFTP connection,
// finds nothing: no file in the way, no missing copy, and no file that
// cannot be linked. The Transfer fails for a retry and keeps what it
// copied, on the source, on the target, and when it resumes.
// regression: #4231
func TestTransferRetriesALookupThatGetsNoAnswer(t *testing.T) {
	const episode, temp = "Show.S01E01.mkv", "Show.S01E01.mkv.qui-transfer-1"
	for _, tc := range []struct {
		name string
		// op is the call that fails, any lookup when empty, and at the base
		// name that it fails at, any name when empty.
		op, at   string
		onSource bool
		rename   []string
		// theirs puts a file of the size of the episode at its name on a
		// target that tells case apart.
		theirs bool
		// resumed ends qui at the third write of the copy first, or at
		// crashAt.
		resumed bool
		crashAt string
		// placed moves the temporary file of the episode to its name after
		// the end: a link is what a stop between the link and the remove of
		// the rename leaves, and a rename is a rename that ended.
		placed func(oldname, newname string) error
		// headStart puts the episode on the target first.
		headStart bool
	}{
		{name: "a name of the Transfer torrent on the source", at: temp, onSource: true},
		{name: "a temporary name", at: temp},
		{name: "a file of the torrent", at: episode},
		{name: "the original name of a renamed file", at: episode, rename: []string{episode, "Show.S01E00.mkv"}},
		{name: "the folder of a file renamed in case", at: showName, rename: []string{episode, "Show.S01e01.mkv"}, theirs: true},
		{name: "a file of the torrent on a resume", at: episode, resumed: true},
		{name: "a temporary file on a resume", at: temp, resumed: true},
		{name: "a file in place on a resume", at: temp, resumed: true, crashAt: "after:place", placed: os.Link},
		{name: "a file renamed into place on a resume", at: episode, resumed: true, crashAt: "after:place", placed: os.Rename},
		{name: "the save path", op: "openroot"},
		{name: "the link of a head start", op: "link", at: episode + ".qui-link-1", headStart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			if tc.theirs && ignoresCase(t, r.dstDir) {
				t.Skip("the target ignores case, so it cannot hold a second file at the old name")
			}
			s := newShow(t, showName)
			r.seed(s)
			r.renameOnSource(s, tc.rename...)
			source := tree(t, r.srcDir)
			want := maps.Clone(source)
			if tc.theirs {
				theirs := bytes.Repeat([]byte("t"), len(s.files[episode]))
				write(t, filepath.Join(r.dstDir, showName), map[string][]byte{episode: theirs})
				want[showName+"/"+episode] = string(theirs)
			}
			if tc.headStart {
				write(t, filepath.Join(r.dstDir, showName), map[string][]byte{episode: s.files[episode]})
			}
			req := r.request(s)
			if tc.onSource {
				r.connect()
				req.Options.ForcedWay = transfer.WayBitTorrent
			}
			lost := func(op, name string) bool {
				return (tc.op == "" && op != "openroot" && op != "link" || op == tc.op) &&
					strings.HasPrefix(name, pathOn(overSFTP, r.srcDir)) == tc.onSource &&
					(tc.at == "" || path.Base(filepath.ToSlash(name)) == tc.at)
			}

			var record *transfer.Record
			var copied map[string]string
			if tc.resumed {
				runCrashChild(t, crash{DB: r.dbPath, Request: req, At: cmp.Or(tc.crashAt, "write:3")}, make(chan *os.Process, 1), false)
				if tc.placed != nil {
					dir := filepath.Join(r.dstDir, showName)
					require.NoError(t, tc.placed(filepath.Join(dir, temp), filepath.Join(dir, episode)))
				}
				copied = tree(t, r.dstDir)
				require.NotEmpty(t, copied)
				r.disk.lostAt = lost
				r.restart()
				r.svc.Wait()
				var id int64
				require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT id FROM torrent_transfers`).Scan(&id))
				record = r.get(id)
			} else {
				r.disk.lostAt = lost
				record = r.run(req)
			}

			require.Equal(t, transfer.StateFailed, record.State)
			require.Equal(t, transfer.FailureTemporary, record.FailureClass, record.FailureReason)
			require.Contains(t, record.FailureReason, fsops.ErrConnectionLost.Error())
			require.True(t, record.FinishedAt.IsZero())
			require.True(t, r.store.IsClaimed(r.source.ID, s.hash))
			require.Equal(t, source, tree(t, r.srcDir), "the source keeps its files")
			switch {
			case tc.resumed:
				require.Equal(t, copied, tree(t, r.dstDir), "qui keeps what it copied")
			case tc.rename != nil:
				require.Equal(t, want, tree(t, r.dstDir), "qui keeps the copy in place")
			case tc.onSource:
				require.Zero(t, r.src.Writes("torrents/renameFile"))
				r.requireNoHelpers(record)
			case tc.headStart:
				require.Equal(t, map[string]string{showName + "/" + episode: string(s.files[episode])}, tree(t, r.dstDir))
			}
		})
	}
}

// A file that lands at a path of the copy during the copy stops the
// Transfer at the rename into place. When qui cannot remove its temporary
// file there, the cleanup still keeps the file in the way.
// regression: #4231
func TestTransferKeepsAFileInTheWayWhenItCannotRemoveItsCopy(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	theirs := filepath.Join(r.dstDir, showName, "Show.S01E02.mkv")
	var writeErr error
	put := sync.OnceFunc(func() { writeErr = os.WriteFile(theirs, []byte("theirs"), 0o600) })
	r.disk.onWrite = func(string) { put() }
	r.disk.failRemove = func(name string) bool {
		return strings.HasPrefix(name, showName+"/Show.S01E02.mkv.qui-transfer-")
	}

	record := r.run(r.request(s))

	require.NoError(t, writeErr)
	require.Equal(t, transfer.StateFailed, record.State)
	require.Contains(t, record.FailureReason, "Conflict: a file is in the way on homelab")
	data, err := os.ReadFile(theirs)
	require.NoError(t, err, "the cleanup keeps the file in the way")
	require.Equal(t, "theirs", string(data))
	require.True(t, r.src.Has(s.hash))
}

// A change of the source torrent during the copy fails the Transfer and
// leaves the source as it is. After a move, a removal, or a file rename, no
// retry finds the files, so qui removes what it wrote. After any other
// change, qui keeps what it wrote for the retry.
func TestTransferFailsWhenTheSourceChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(r *rig, s show)
		reason string
		class  transfer.FailureClass
	}{
		{name: "a move", change: func(r *rig, s show) {
			r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.SavePath = filepath.Join(r.srcDir, "moved") })
		}, reason: "it moved", class: transfer.FailurePermanent},
		// regression: #4231
		{name: "a move that has not ended", change: func(r *rig, s show) {
			r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.Moving = true })
		}, reason: "it moved", class: transfer.FailurePermanent},
		{name: "a rename", change: func(r *rig, s show) {
			r.src.Edit(s.hash, func(t *qbtstub.Torrent) { t.Name = "Show.S01.REPACK.1080p-GRP" })
		}, reason: "it was renamed", class: transfer.FailureTemporary},
		{name: "a file rename", change: func(r *rig, s show) {
			require.NoError(t, r.sync.RenameTorrentFile(r.t.Context(), r.source.ID, s.hash,
				showName+"/Show.S01E02.mkv", showName+"/Show.S01E02.REPACK.mkv"))
		}, reason: "a file was renamed", class: transfer.FailurePermanent},
		{name: "a recheck", change: func(r *rig, s show) {
			r.src.CheckSyncs = 1000
			require.NoError(t, r.sync.BulkAction(r.t.Context(), r.source.ID, []string{s.hash}, "recheck"))
		}, reason: "qBittorrent checks it again", class: transfer.FailureTemporary},
		{name: "a file that grows", change: func(r *rig, s show) {
			f, err := os.OpenFile(filepath.Join(r.srcDir, showName, "Show.S01E02.mkv"), os.O_APPEND|os.O_WRONLY, 0o600)
			require.NoError(t, err)
			_, err = f.WriteString("more")
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}, reason: "the file " + showName + "/Show.S01E02.mkv changed its size", class: transfer.FailureTemporary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newShow(t, showName)
			r.seed(s)
			var once sync.Once
			r.disk.onWrite = func(string) { once.Do(func() { tc.change(r, s) }) }

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateFailed, record.State)
			require.Contains(t, record.FailureReason, "The torrent changed on the source during the copy: "+tc.reason)
			require.Equal(t, tc.class, record.FailureClass)
			if tc.class == transfer.FailurePermanent {
				require.Empty(t, tree(t, r.dstDir), "qui removes what it wrote")
			} else {
				require.NotEmpty(t, tree(t, r.dstDir), "qui keeps what it wrote")
				require.True(t, r.store.IsClaimed(r.source.ID, s.hash))
			}
			require.True(t, r.src.Has(s.hash))
			require.Zero(t, r.dst.Writes("torrents/add"))
		})
	}
}

// Without a confirmed flush, a check-only helper torrent checks every piece
// on the target before qui touches the source. qui writes the registry row
// of the helper before its add.
func TestTransferChecksOnTheTargetWithoutAConfirmedFlush(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.disk.unconfirmed = true
	helperAtAdd := map[string]bool{}
	r.dst.OnAdd = func(hash string) { helperAtAdd[hash] = r.store.IsHelper(r.target.ID, hash) }

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.FlushUnconfirmed, record.FlushState)
	require.Empty(t, record.Members[0].ProvenPieces, "no flush was confirmed, so no piece is proven")
	helpers, err := r.store.HelpersOf(t.Context(), transfer.OwnerTransfer, record.ID)
	require.NoError(t, err)
	require.Len(t, helpers, 1)
	require.Equal(t, map[string]bool{helpers[0].Hash: true, s.hash: false}, helperAtAdd,
		"the helper had its registry row at its add")
	require.False(t, r.dst.Has(helpers[0].Hash), "qui removed the helper")
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "Proof: the check on homelab passed all 5 pieces.")
	require.False(t, r.src.Has(s.hash))
}

func logOf(record *transfer.Record) string {
	lines := make([]string, 0, len(record.Log))
	for _, line := range record.Log {
		lines = append(lines, line.Message)
	}
	return strings.Join(lines, "\n")
}

// regression: #4231
// A write that the target never answers ignores the stop. Close waits for it
// only until its context ends, so that a shutdown keeps its time limit, and
// the Transfer stays unfinished for the next start.
func TestTransferCloseGivesUpOnARunThatDoesNotStop(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	stalled, held := make(chan struct{}), make(chan struct{})
	stall, release := sync.OnceFunc(func() { close(stalled) }), sync.OnceFunc(func() { close(held) })
	// Registered after the rig, so that it runs before the rig closes the
	// Service, also when the test fails first.
	t.Cleanup(release)
	r.disk.onWrite = func(string) {
		stall()
		<-held
	}

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	<-stalled
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- r.svc.Close(ctx) }()
	select {
	case err := <-closed:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "Close still waits for a write that ignores the stop")
	}

	release()
	r.svc.Wait()
	record, err := r.store.Get(t.Context(), id)
	require.NoError(t, err)
	require.True(t, record.FinishedAt.IsZero(), "the Transfer stays unfinished")
}

// After the Transfer, qui keeps the files on the source when another torrent
// reads them or another instance uses the same SSH server.
func TestTransferKeepsSourceFiles(t *testing.T) {
	t.Run("a file that another torrent reads", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		var once sync.Once
		episode := ""
		r.disk.onWrite = func(string) { once.Do(func() { episode = r.seedEpisode(s) }) }

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.False(t, r.src.Has(s.hash))
		require.True(t, r.src.Has(episode))
		require.Equal(t, contents(s, showName), tree(t, r.srcDir))
		require.Contains(t, logOf(record), "kept its files, because another torrent reads them: 70 B stay in use.")
	})
	t.Run("another instance on the same SSH server", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		r.sshInstance("second client", qbtstub.New(t, r.srcPath()).URL)

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.False(t, r.src.Has(s.hash))
		require.Equal(t, contents(s, showName), tree(t, r.srcDir))
		require.Contains(t, logOf(record), "kept its files, because second client uses the same SSH server: 70 B stay in use.")
	})
	// regression: #4231
	t.Run("another instance on the same SSH server, added while the removal waits for its retry", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		refused := false
		r.src.OnWrite = func(endpoint string) {
			r.src.Reject = 0
			if endpoint == "torrents/delete" && !refused {
				refused = true
				r.src.Reject = http.StatusInternalServerError
			}
		}
		record := r.run(r.request(s))
		require.Equal(t, transfer.FailureTemporary, record.FailureClass, record.FailureReason)
		r.sshInstance("second client", qbtstub.New(t, r.srcPath()).URL)

		r.setClock(clock.Add(2 * time.Hour))
		r.svc.Wake()
		r.svc.Wait()

		record = r.get(record.ID)
		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.False(t, r.src.Has(s.hash))
		require.Equal(t, contents(s, showName), tree(t, r.srcDir))
		require.Contains(t, logOf(record), "kept its files, because second client uses the same SSH server: 70 B stay in use.")
	})
}

// With no Transfer rows, the Service sends nothing to any instance, and
// writes nothing.
func TestTransferServiceIdleWithoutTransfers(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)

	closeService(t, r.svc)
	svc := transfer.NewService(t.Context(), r.deps)
	svc.Wake()
	closeService(t, svc)

	require.Zero(t, r.src.Requests())
	require.Zero(t, r.dst.Requests())
	require.Zero(t, r.ssh.Dials(), "no SSH connection")
	writes, creates := r.disk.log()
	require.Empty(t, writes)
	require.Empty(t, creates)
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))

	backend, err := r.deps.Files.GetBackend(t.Context(), r.source.ID)
	require.NoError(t, err)
	for range 2 {
		reader, err := backend.OpenRead(t.Context(), filepath.ToSlash(filepath.Join(r.srcDir, showName, "Show.S01E01.mkv")), 0)
		require.NoError(t, err)
		_, err = io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
	}
	require.Equal(t, 1, r.ssh.Accepts(), "other file features read on one connection")
}
