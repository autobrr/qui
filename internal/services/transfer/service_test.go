// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // BitTorrent v1 pieces are SHA-1
	"errors"
	"io"
	"io/fs"
	"maps"
	"net"
	"os"
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

	mu      sync.Mutex
	writes  []diskWrite
	creates map[string]int
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

func (d *disk) OpenRoot(ctx context.Context, dir string) (fsops.Root, error) {
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
	return &diskWriter{FileWriter: w, d: r.d, name: name}, nil
}

func (r *diskRoot) OpenWrite(ctx context.Context, name string, offset int64) (fsops.FileWriter, error) {
	w, err := r.Root.OpenWrite(ctx, name, offset)
	if err != nil {
		return nil, err
	}
	return &diskWriter{FileWriter: w, d: r.d, name: name, at: offset}, nil
}

type diskWriter struct {
	fsops.FileWriter
	d    *disk
	name string
	at   int64
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
	r.svc = transfer.NewService(ctx, r.deps)
	r.t.Cleanup(r.svc.Close)
}

// restart stops the Service, and starts qui again on the same database.
func (r *rig) restart() {
	r.t.Helper()
	r.svc.Close()
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
// source.
func TestTransferMovesATorrent(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
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
		{name: "remove the torrent and keep the files", options: transfer.Options{After: transfer.AfterKeepFiles}, targetState: "stalledUP"},
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

// Each entry check refuses with its reason, and records no Transfer.
func TestTransferRefuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(r *rig, s show) transfer.Request
		reason string
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
			require.False(t, r.store.IsClaimed(r.source.ID, s.hash), "a refused Transfer claims nothing")
			require.Zero(t, r.dst.Writes("torrents/add"))
		})
	}
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
		sum := sha1.Sum(chunk) //nolint:gosec // BitTorrent v1 pieces are SHA-1
		info.Pieces = append(info.Pieces, sum[:]...)
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes(), mi.HashInfoBytes().HexString()
}

// One Transfer runs at a time for each source. The next one waits.
func TestTransferQueuesBySource(t *testing.T) {
	r := newRig(t)
	first, second := newShow(t, showName), newShow(t, "Other.Show.S02.720p-GRP")
	r.seed(first)
	write(t, filepath.Join(r.srcDir, "Other.Show.S02.720p-GRP"), second.files)
	r.src.Seed(second.data, r.srcPath())
	copying, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.disk.onWrite = func(string) {
		once.Do(func() {
			close(copying)
			<-release
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
	close(release)
	r.svc.Wait()

	for _, id := range []int64{firstID, secondID} {
		record, err := r.store.Get(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	}
}

// qui checks the free space on the target before it writes anything.
func TestTransferNeedsFreeSpace(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.disk.free = 69

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Contains(t, record.FailureReason, "Not enough free space on homelab")
	require.Empty(t, tree(t, r.dstDir))
	require.True(t, r.src.Has(s.hash))
}

// A file at a path that the copy writes stops the Transfer, and qui leaves
// the file as it is.
func TestTransferStopsAtAConflict(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{"Show.S01E02.mkv": []byte("theirs")})

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Contains(t, record.FailureReason, "Conflict: a file is in the way on homelab at "+filepath.Join(r.dstDir, showName)+"/Show.S01E02.mkv")
	require.Equal(t, map[string]string{showName + "/Show.S01E02.mkv": "theirs"}, tree(t, r.dstDir))
	require.True(t, r.src.Has(s.hash))
	require.False(t, r.src.Stopped(s.hash))
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

// The check on the target names the pieces that failed. With qBittorrent's
// setting "Append .!qB extension to incomplete files", the check renames the
// incomplete temporary file, and the cleanup removes it under that name.
func TestTransferCheckOnTheTargetFindsBadPieces(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.disk.unconfirmed = true
	r.dst.IncompleteFilesExt = true
	r.disk.damage = map[string]int64{showName + "/Show.S01E02.mkv": 20}

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "The check on homelab failed 1 of 5 pieces: 3.", record.FailureReason, "pieces 0, 1, 2 and 4 passed")
	require.Empty(t, tree(t, r.dstDir), "the cleanup removes the temporary files, also under their .!qB names")
	helpers, err := r.store.HelpersOf(t.Context(), transfer.OwnerTransfer, record.ID)
	require.NoError(t, err)
	require.Len(t, helpers, 1)
	require.False(t, r.dst.Has(helpers[0].Hash))
	require.False(t, r.dst.Has(s.hash), "qui never adds a torrent that is not proven")
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
	require.False(t, r.src.Stopped(s.hash))
}

func logOf(record *transfer.Record) string {
	lines := make([]string, 0, len(record.Log))
	for _, line := range record.Log {
		lines = append(lines, line.Message)
	}
	return strings.Join(lines, "\n")
}

// A copy that stops halfway keeps its partial data, and each Proven piece in
// the record is on the target and passes its check.
func TestTransferStoppedHalfwayKeepsProvenPieces(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	writes := 0
	r.disk.onWrite = func(string) {
		if writes++; writes == 5 {
			r.cancel()
		}
	}

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	r.svc.Wait()

	record, err := r.store.Get(t.Context(), id)
	require.NoError(t, err)
	require.True(t, record.FinishedAt.IsZero(), "the Transfer stays unfinished")
	require.True(t, r.store.IsClaimed(r.source.ID, s.hash))
	proven := record.Members[0].ProvenPieces
	require.NotEmpty(t, proven)
	mi, err := metainfo.Load(bytes.NewReader(s.data))
	require.NoError(t, err)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	var stream []byte
	for _, f := range info.Files {
		data, err := os.ReadFile(filepath.Join(r.dstDir, showName, f.Path[0]+".qui-transfer-"+strconv.FormatInt(id, 10)))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		require.NoError(t, err)
		stream = append(stream, data...)
	}
	count := 0
	for piece := range info.NumPieces() {
		if proven[piece/8]&(0x80>>(piece%8)) == 0 {
			continue
		}
		count++
		start := int64(piece) * info.PieceLength
		end := min(start+info.PieceLength, info.TotalLength())
		require.LessOrEqual(t, end, int64(len(stream)), "piece %d is on the target", piece)
		sum := sha1.Sum(stream[start:end]) //nolint:gosec // BitTorrent v1 pieces are SHA-1
		require.Equal(t, info.Pieces[piece*sha1.Size:(piece+1)*sha1.Size], sum[:], "piece %d passes its check", piece)
	}
	require.Less(t, count, info.NumPieces(), "the copy stopped halfway")
	require.True(t, r.src.Has(s.hash))
}

// After the Transfer, qui removes only the files that it copied, and keeps
// them all when another torrent reads them or another instance uses the
// same SSH server.
func TestTransferKeepsSourceFiles(t *testing.T) {
	t.Run("a file that qui did not copy", func(t *testing.T) {
		r := newRig(t)
		s := newShow(t, showName)
		r.seed(s)
		write(t, filepath.Join(r.srcDir, showName), map[string][]byte{"Show.S01.nfo": []byte("notes")})

		record := r.run(r.request(s))

		require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
		require.Equal(t, map[string]string{showName + "/Show.S01.nfo": "notes"}, tree(t, r.srcDir))
	})
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
}

// With no Transfer rows, the Service sends nothing to any instance.
func TestTransferServiceIdleWithoutTransfers(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)

	r.svc.Close()
	svc := transfer.NewService(t.Context(), r.deps)
	svc.Wake()
	svc.Close()

	require.Zero(t, r.src.Requests())
	require.Zero(t, r.dst.Requests())
	require.Zero(t, r.ssh.Dials(), "no SSH connection")
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

// A finished Transfer stays finished: a Wake and a restart send nothing.
func TestTransferServiceLeavesFinishedTransfers(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	write(t, filepath.Join(r.dstDir, showName), map[string][]byte{"Show.S01E02.mkv": []byte("theirs")})
	require.Equal(t, transfer.FailurePermanent, r.run(r.request(s)).FailureClass)
	require.NoError(t, os.RemoveAll(filepath.Join(r.dstDir, showName)))
	require.Equal(t, transfer.StateDone, r.run(r.request(s)).State)
	src, dst := r.src.Requests(), r.dst.Requests()

	r.svc.Wake()
	r.svc.Wait()
	r.restart()
	r.svc.Wait()

	require.Equal(t, src, r.src.Requests())
	require.Equal(t, dst, r.dst.Requests())
}
