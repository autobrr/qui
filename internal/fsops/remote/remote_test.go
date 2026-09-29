// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// fakeCreds stands in for the instance store: the row Get answers with, plus
// the decrypted key and pin, so the tests need no database.
type fakeCreds struct {
	key    string
	pin    []byte
	inst   *models.Instance
	getErr error
}

func (f fakeCreds) Get(context.Context, int) (*models.Instance, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.inst, nil
}
func (f fakeCreds) GetDecryptedSSHKey(*models.Instance) (string, error) { return f.key, nil }
func (f fakeCreds) GetHostKeyPin(*models.Instance) ([]byte, error)      { return f.pin, nil }

// newBackend starts an in-process SSH/SFTP server and returns a backend wired
// to it through a real connection pool. The server's sftp subsystem is rooted
// at the host filesystem, so t.TempDir() paths work as remote paths.
func newBackend(t *testing.T) (*Backend, *sshtest.Server) {
	t.Helper()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecGNU)

	host, portText, err := net.SplitHostPort(server.Addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	inst := &models.Instance{ID: 1, SSHHost: host, SSHPort: port, SSHUsername: "qui", SSHKeyEncrypted: "enc-v1", SSHHostKeyEncrypted: "enc-v1"}
	pool := sshpool.NewPool(sshpool.NewDialer(fakeCreds{
		key:  sshtest.PrivateKey(""),
		pin:  hostKey.PublicKey().Marshal(),
		inst: inst,
	}))
	t.Cleanup(pool.Close)

	return New(pool, inst), server
}

// remotePath converts a local test path to the slash form the remote side uses.
func remotePath(elem ...string) string {
	return filepath.ToSlash(filepath.Join(elem...))
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
	require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
}

func TestStat(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	file := remotePath(dir, "file.txt")
	writeFile(t, file, "hello")
	link := remotePath(dir, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	tests := []struct {
		name      string
		path      string
		lstat     bool
		isDir     bool
		isSymlink bool
		size      int64
		identity  bool
	}{
		{name: "file", path: file, size: 5, identity: true},
		{name: "dir", path: remotePath(dir), isDir: true, identity: true},
		{name: "symlink followed", path: link, size: 5, identity: true},
		// A symlink's own size is the length of its target path.
		{name: "symlink not followed", path: link, lstat: true, isSymlink: true, size: int64(len(file))},
		{name: "file lstat", path: file, lstat: true, size: 5, identity: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stat := b.Stat
			if tt.lstat {
				stat = b.Lstat
			}
			info, err := stat(t.Context(), tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.path, info.Path)
			assert.Equal(t, tt.isDir, info.IsDir)
			assert.Equal(t, tt.isSymlink, info.IsSymlink)
			if !tt.isDir {
				assert.Equal(t, tt.size, info.Size)
			}
			assert.False(t, info.ModTime.IsZero())

			// sftp attrs carry no inode, so identity is always the documented
			// degradation: zero FileID plus FileIDErr.
			assert.True(t, info.FileID.IsZero())
			assert.Zero(t, info.Nlinks)
			if tt.identity {
				require.ErrorIs(t, info.FileIDErr, errNoIdentity)
			} else {
				require.NoError(t, info.FileIDErr)
			}
		})
	}
}

func TestPortableNotExistErrors(t *testing.T) {
	t.Parallel()

	// The Backend contract promises errors.Is(err, fs.ErrNotExist) for a
	// missing path from every read method, and that the path survives.
	b, _ := newBackend(t)
	ctx := t.Context()
	missing := remotePath(t.TempDir(), "nope", "missing.mkv")

	_, err := b.Stat(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Contains(t, err.Error(), missing)

	_, err = b.Lstat(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = b.ReadDir(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = b.WalkDir(ctx, missing, fsops.WalkOptions{})
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = b.Statfs(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReadDir(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "a.txt"), "a")
	require.NoError(t, os.Mkdir(remotePath(dir, "sub"), 0o755))
	if err := os.Symlink(remotePath(dir, "a.txt"), remotePath(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	entries, err := b.ReadDir(t.Context(), remotePath(dir))
	require.NoError(t, err)

	byName := map[string]fsops.DirEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	require.Len(t, byName, 3)
	assert.False(t, byName["a.txt"].IsDir)
	assert.False(t, byName["a.txt"].IsSymlink)
	assert.True(t, byName["sub"].IsDir)
	assert.True(t, byName["link.txt"].IsSymlink)
}

func TestWalkDir_Basic(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "a.txt"), "aaa")
	writeFile(t, remotePath(dir, "sub", "b.txt"), "bbb")

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{WantFileID: true})
	require.NoError(t, err)

	byRel := map[string]fsops.WalkEntry{}
	for entry := range ch {
		require.NoError(t, entry.Err)
		byRel[entry.RelPath] = entry
	}

	require.Len(t, byRel, 4)
	assert.Equal(t, remotePath(dir), byRel["."].Path)
	assert.True(t, byRel["."].IsDir)
	assert.Equal(t, int64(3), byRel["a.txt"].Size)
	assert.Equal(t, remotePath(dir, "a.txt"), byRel["a.txt"].Path)
	assert.True(t, byRel["sub"].IsDir)
	assert.Equal(t, path.Join("sub", "b.txt"), byRel[path.Join("sub", "b.txt")].RelPath)

	// WantFileID cannot be honoured over sftp, so regular files carry the
	// reason and directories are left alone, exactly as local does.
	require.ErrorIs(t, byRel["a.txt"].FileIDErr, errNoIdentity)
	require.NoError(t, byRel["sub"].FileIDErr)
}

func TestWalkDir_SkipsAndIgnores(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "keep.txt"), "k")
	writeFile(t, remotePath(dir, ".hidden"), "h")
	writeFile(t, remotePath(dir, ".hiddendir", "inside.txt"), "i")
	writeFile(t, remotePath(dir, "node_modules", "pkg.js"), "p")
	writeFile(t, remotePath(dir, "$recycle.bin", "old.mkv"), "r")
	writeFile(t, remotePath(dir, ".trash-1000", "deleted.mkv"), "t")
	ignored := remotePath(dir, "ignored.txt")
	writeFile(t, ignored, "i")

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{
		SkipHidden:            true,
		IgnoreDirNames:        []string{"node_modules", "$RECYCLE.BIN"},
		IgnoreDirNamePrefixes: []string{".Trash-"},
		IgnorePaths:           []string{ignored},
	})
	require.NoError(t, err)

	var relPaths []string
	for entry := range ch {
		relPaths = append(relPaths, entry.RelPath)
	}

	assert.Contains(t, relPaths, "keep.txt")
	assert.NotContains(t, relPaths, ".hidden")
	assert.NotContains(t, relPaths, path.Join(".hiddendir", "inside.txt"))
	assert.NotContains(t, relPaths, path.Join("node_modules", "pkg.js"))
	// Matching is case-insensitive: metadata dir case varies on disk.
	assert.NotContains(t, relPaths, path.Join("$recycle.bin", "old.mkv"))
	assert.NotContains(t, relPaths, path.Join(".trash-1000", "deleted.mkv"))
	assert.NotContains(t, relPaths, "ignored.txt")

	// An ignored root walks nothing, as it does locally.
	ch, err = b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{IgnorePaths: []string{remotePath(dir)}})
	require.NoError(t, err)
	assert.Empty(t, slices.Collect(func(yield func(fsops.WalkEntry) bool) {
		for entry := range ch {
			if !yield(entry) {
				return
			}
		}
	}))
}

// Each walk filter on its own, so a mutation of one is not hidden by another:
// a prefix rule without SkipHidden, a name rule that must not drop a file of
// that name, and identity only when the caller asked for it.
func TestWalkDir_FiltersOnTheirOwn(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "keep.txt"), "k")
	writeFile(t, remotePath(dir, ".trash-1000", "deleted.mkv"), "t")
	writeFile(t, remotePath(dir, "node_modules"), "a file, not a directory")
	writeFile(t, remotePath(dir, "sub", "node_modules", "pkg.js"), "p")

	collect := func(opts fsops.WalkOptions) ([]string, []fsops.WalkEntry) {
		ch, err := b.WalkDir(t.Context(), remotePath(dir), opts)
		require.NoError(t, err)
		var rels []string
		var entries []fsops.WalkEntry
		for e := range ch {
			rels = append(rels, e.RelPath)
			entries = append(entries, e)
		}
		return rels, entries
	}

	rels, _ := collect(fsops.WalkOptions{IgnoreDirNamePrefixes: []string{".Trash-"}})
	assert.Contains(t, rels, "keep.txt")
	assert.NotContains(t, rels, ".trash-1000", "the prefix rule alone must hide the directory")
	assert.NotContains(t, rels, path.Join(".trash-1000", "deleted.mkv"))

	rels, _ = collect(fsops.WalkOptions{IgnoreDirNames: []string{"node_modules"}})
	assert.Contains(t, rels, "node_modules", "the name rule applies to directories only")
	assert.NotContains(t, rels, path.Join("sub", "node_modules", "pkg.js"))

	_, entries := collect(fsops.WalkOptions{})
	for _, e := range entries {
		require.NoError(t, e.FileIDErr, "%s: identity is only reported absent when it was asked for", e.RelPath)
	}
}

// writeCutTree lays out a tree for the connection-lost walks: "a" holds more
// files than the walk channel buffers, so the walk is parked inside it when
// the test cuts the connection, "m" is the directory that read fails on, and
// "z" is a sibling after it that must never be reached.
func writeCutTree(t *testing.T, dir string) {
	t.Helper()
	for i := range 100 {
		writeFile(t, remotePath(dir, "a", fmt.Sprintf("f%03d.txt", i)), "x")
	}
	writeFile(t, remotePath(dir, "m", "mid.txt"), "x")
	writeFile(t, remotePath(dir, "z", "last.txt"), "x")
}

// drainAfterCut reads the rest of the walk and returns its one Err entry,
// failing if a second Err or any entry after the first arrives: a walk that
// lost its connection must stop, not step over the directory and go on.
func drainAfterCut(t *testing.T, ch <-chan fsops.WalkEntry) fsops.WalkEntry {
	t.Helper()
	var cut *fsops.WalkEntry
	for entry := range ch {
		if cut != nil {
			t.Fatalf("entry %q arrived after the walk lost its connection at %q", entry.RelPath, cut.RelPath)
		}
		if entry.Err != nil {
			cut = &entry
		}
	}
	require.NotNil(t, cut, "the walk must end with an Err entry")
	return *cut
}

// A pool failure mid-walk ends the walk with one Err entry carrying
// ErrConnectionLost, so a consumer that skips per-directory errors still sees
// that the tree was cut short.
func TestWalkDir_PoolFailureEndsTheWalkWithConnectionLost(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeCutTree(t, dir)

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)
	for range 3 {
		<-ch
	}
	b.pool.Close()

	cut := drainAfterCut(t, ch)
	require.ErrorIs(t, cut.Err, fsops.ErrConnectionLost)
	assert.Equal(t, "m", cut.RelPath, "the directory the walk could not read is named")
}

// A connection that drops while a directory read is in flight ends the walk
// the same way: the read fails with pkg/sftp's connection-lost status, not
// with a refusal the walk would step over.
func TestWalkDir_DropDuringReadDirEndsTheWalkWithConnectionLost(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	dir := t.TempDir()
	// The listing of "a" is done and the walk is parked inside it when the
	// trap is armed, so the next request is the opendir for "m".
	writeCutTree(t, dir)

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)
	for range 3 {
		<-ch
	}
	server.SetSFTP(sshtest.SFTPDropOnNextRequest)

	cut := drainAfterCut(t, ch)
	require.ErrorIs(t, cut.Err, fsops.ErrConnectionLost)
	require.ErrorContains(t, cut.Err, sftp.ErrSSHFxConnectionLost.Error())
	assert.Equal(t, "m", cut.RelPath)
}

func TestLostConnection(t *testing.T) {
	t.Parallel()

	assert.True(t, lostConnection(sftp.ErrSSHFxConnectionLost))
	assert.True(t, lostConnection(fmt.Errorf("wrapped: %w", net.ErrClosed)))
	assert.True(t, lostConnection(io.EOF))
	assert.False(t, lostConnection(fs.ErrPermission), "a refused directory is not a lost connection")
	assert.False(t, lostConnection(sftp.ErrSSHFxNoSuchFile))
}

// readCalls runs every read method against p, for the tests that assert how
// each one reports a failure.
func readCalls(b *Backend, p string) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"stat":    func(ctx context.Context) error { _, err := b.Stat(ctx, p); return err },
		"lstat":   func(ctx context.Context) error { _, err := b.Lstat(ctx, p); return err },
		"readdir": func(ctx context.Context) error { _, err := b.ReadDir(ctx, p); return err },
		"walkdir": func(ctx context.Context) error {
			_, err := b.WalkDir(ctx, p, fsops.WalkOptions{})
			return err
		},
		"statfs":         func(ctx context.Context) error { _, err := b.Statfs(ctx, p); return err },
		"samefilesystem": func(ctx context.Context) error { _, err := b.SameFilesystem(ctx, p, p); return err },
	}
}

// A pool error is a lost connection from every read, and its cause is text
// only. A cause that matches fs.ErrPermission would read as a denied path to a
// consumer that steps over those.
func TestReadsReportAPoolErrorAsConnectionLost(t *testing.T) {
	t.Parallel()

	inst := &models.Instance{ID: 1}
	pool := sshpool.NewPool(sshpool.NewDialer(fakeCreds{getErr: fmt.Errorf("open database: %w", fs.ErrPermission)}))
	t.Cleanup(pool.Close)
	b := New(pool, inst)

	for name, call := range readCalls(b, "/data") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := call(t.Context())
			require.ErrorIs(t, err, fsops.ErrConnectionLost)
			require.NotErrorIs(t, err, fs.ErrPermission)
			assert.ErrorContains(t, err, "open database", "the cause stays readable")
		})
	}
}

// An instance that left remote mode is refused by the pool, and every read
// reports that as a lost connection rather than as an answer about the path.
func TestReadsReportAnInstanceThatLeftRemoteModeAsConnectionLost(t *testing.T) {
	t.Parallel()

	inst := &models.Instance{ID: 1, HasLocalFilesystemAccess: true, SSHHost: "127.0.0.1", SSHKeyEncrypted: "enc-v1", SSHHostKeyEncrypted: "enc-v1"}
	pool := sshpool.NewPool(sshpool.NewDialer(fakeCreds{inst: inst}))
	t.Cleanup(pool.Close)
	b := New(pool, inst)

	for name, call := range readCalls(b, "/data") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := call(t.Context())
			require.ErrorIs(t, err, fsops.ErrConnectionLost)
			assert.ErrorContains(t, err, sshpool.ErrNotRemote.Error())
		})
	}
}

// A transport that drops while a request is in flight is a lost connection
// from every read, not only from the walk.
func TestReadsReportADroppedTransportAsConnectionLost(t *testing.T) {
	t.Parallel()

	dir := remotePath(t.TempDir())
	for _, name := range []string{"stat", "lstat", "readdir", "walkdir", "statfs", "samefilesystem"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			b, server := newBackend(t)
			_, err := b.Stat(t.Context(), dir)
			require.NoError(t, err)
			server.SetSFTP(sshtest.SFTPDropOnNextRequest)

			err = readCalls(b, dir)[name](t.Context())
			require.ErrorIs(t, err, fsops.ErrConnectionLost)
			require.NotErrorIs(t, err, fs.ErrNotExist)
			require.NotErrorIs(t, err, fs.ErrPermission)
		})
	}
}

// A path the server refuses is an answer about that path, not a lost
// connection.
func TestReadsKeepAServerPermissionDenial(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("0o000 permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	b, _ := newBackend(t)
	locked := remotePath(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	writeFile(t, remotePath(locked, "hidden.txt"), "h")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	for name, call := range map[string]func() error{
		"stat":    func() error { _, err := b.Stat(t.Context(), remotePath(locked, "hidden.txt")); return err },
		"readdir": func() error { _, err := b.ReadDir(t.Context(), locked); return err },
	} {
		err := call()
		require.ErrorIs(t, err, fs.ErrPermission, name)
		assert.NotErrorIs(t, err, fsops.ErrConnectionLost, name)
	}
}

func TestWalkDir_DoesNotDescendSymlinkedDir(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "target", "inside.txt"), "i")
	if err := os.Symlink(remotePath(dir, "target"), remotePath(dir, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)

	byRel := map[string]fsops.WalkEntry{}
	for entry := range ch {
		byRel[entry.RelPath] = entry
	}

	assert.True(t, byRel["link"].IsSymlink)
	assert.False(t, byRel["link"].IsDir, "readdir attrs are lstat-style")
	assert.Contains(t, byRel, path.Join("target", "inside.txt"))
	assert.NotContains(t, byRel, path.Join("link", "inside.txt"), "a symlinked dir must not be descended")
}

func TestWalkDir_UnreadableSubdirEmitsEntryErrAndContinues(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("0o000 permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "readable.txt"), "r")
	locked := remotePath(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	writeFile(t, remotePath(locked, "hidden.txt"), "h")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)

	var errPaths, okRelPaths []string
	for entry := range ch {
		if entry.Err != nil {
			require.ErrorIs(t, entry.Err, fs.ErrPermission)
			errPaths = append(errPaths, entry.Path)
			continue
		}
		okRelPaths = append(okRelPaths, entry.RelPath)
	}

	assert.Contains(t, errPaths, locked, "an unreadable dir surfaces as an entry with Err")
	assert.Contains(t, okRelPaths, "readable.txt", "the walk continues past it")
}

func TestWalkDir_ContextCancellation(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	dir := t.TempDir()
	// More files than the walk channel buffers (64), so the walk cannot
	// complete before the first receive and cancellation must cut it short.
	const total = 150
	for i := range total {
		writeFile(t, remotePath(dir, fmt.Sprintf("f%03d.txt", i)), "x")
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := b.WalkDir(ctx, remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)

	count := 0
	for range ch {
		count++
		if count >= 3 {
			cancel()
			break
		}
	}
	for range ch {
		count++
	}
	assert.Less(t, count, 100, "a cancelled walk must not run the tree out")

	// The connection is shared, so one caller giving up must leave it usable:
	// the next call succeeds without a redial.
	_, err = b.Stat(t.Context(), remotePath(dir))
	require.NoError(t, err)
	assert.Equal(t, 1, server.Accepts(), "a cancelled call must not drop the shared connection")
}

// A walk that is mid-tree when the connection drops finishes on the redialled
// connection, because walk takes the client per directory (com6056, #2739).
func TestWalkDir_SurvivesDroppedConnection(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	dir := t.TempDir()
	// "a" holds more files than the walk channel buffers, so the walk is parked
	// inside it when the connection drops, and "z" is still to be read.
	for i := range 100 {
		writeFile(t, remotePath(dir, "a", fmt.Sprintf("f%03d.txt", i)), "x")
	}
	writeFile(t, remotePath(dir, "z", "last.txt"), "x")

	ch, err := b.WalkDir(t.Context(), remotePath(dir), fsops.WalkOptions{})
	require.NoError(t, err)
	for range 3 {
		<-ch
	}

	server.DropConnections()
	require.Eventually(t, func() bool {
		_, err := b.Stat(t.Context(), remotePath(dir))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)

	sawLast := false
	for entry := range ch {
		require.NoError(t, entry.Err, entry.Path)
		if entry.RelPath == path.Join("z", "last.txt") {
			sawLast = true
		}
	}
	assert.True(t, sawLast, "the walk reaches z on the redialled connection")
}

func TestStatfs(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := remotePath(t.TempDir())
	result, err := b.Statfs(t.Context(), dir)
	require.NoError(t, err)
	assert.Positive(t, result.BytesAvailable)
	assert.Positive(t, result.BytesTotal)
	assert.LessOrEqual(t, result.BytesAvailable, result.BytesTotal)
}

// Available is what this user may fill (Bavail), never the free figure that
// includes the root reserve; a free-space gate must agree with the local
// backend on the same volume.
func TestStatfsResultUsesBavail(t *testing.T) {
	t.Parallel()

	stat := &sftp.StatVFS{Frsize: 4096, Blocks: 1000, Bfree: 100, Bavail: 50}
	result := statfsResult(stat)
	assert.Equal(t, int64(4096*50), result.BytesAvailable)
	assert.Equal(t, int64(4096*1000), result.BytesTotal)
	assert.Less(t, result.BytesAvailable, int64(stat.FreeSpace()), "the root reserve is not available space")
}

func TestStatfsWithoutStatvfsExtension(t *testing.T) {
	// SetSFTPExtensions mutates a package global, so this test cannot run in
	// parallel with the ones that need statvfs advertised.
	// Restore whatever pkg/sftp advertised before, not a copy of today's list.
	advertised := extensionNames(t)
	require.NoError(t, sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com"))
	t.Cleanup(func() {
		require.NoError(t, sftp.SetSFTPExtensions(advertised...))
	})

	b, _ := newBackend(t)
	dir := remotePath(t.TempDir())

	_, err := b.Statfs(t.Context(), dir)
	require.ErrorIs(t, err, fsops.ErrUnsupported)
	assert.Contains(t, err.Error(), "statvfs@openssh.com")
	assert.Contains(t, err.Error(), dir)

	_, err = b.SameFilesystem(t.Context(), dir, dir)
	require.ErrorIs(t, err, fsops.ErrUnsupported)
	assert.Contains(t, err.Error(), statvfsExtension, "the missing extension, not a zero fsid, is the reason")
}

func TestSameFilesystem(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	first := remotePath(dir, "a")
	second := remotePath(dir, "b")
	require.NoError(t, os.Mkdir(first, 0o755))
	require.NoError(t, os.Mkdir(second, 0o755))

	same, err := b.SameFilesystem(t.Context(), first, second)
	if runtime.GOOS != "darwin" {
		// pkg/sftp's linux statvfs server sends no fsid (OpenSSH's does).
		require.ErrorIs(t, err, fsops.ErrUnsupported)
		return
	}
	require.NoError(t, err)
	assert.True(t, same)
}

func TestSameFsid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		first  uint64
		second uint64
		same   bool
		err    bool
	}{
		{name: "equal", first: 7, second: 7, same: true},
		{name: "different", first: 7, second: 8},
		{name: "first zero", first: 0, second: 7, err: true},
		{name: "second zero", first: 7, second: 0, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			same, err := sameFsid(&sftp.StatVFS{Fsid: tt.first}, &sftp.StatVFS{Fsid: tt.second})
			if tt.err {
				require.ErrorIs(t, err, fsops.ErrUnsupported)
				assert.Contains(t, err.Error(), "filesystem id")
				assert.False(t, same)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.same, same)
		})
	}
}

func TestSupportsReflink(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	supported, reason, err := b.SupportsReflink(t.Context(), remotePath(t.TempDir()))
	require.NoError(t, err)
	assert.False(t, supported)
	assert.NotEmpty(t, reason)
}

func TestWriteMethodsAreUnsupported(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	ctx := t.Context()
	dir := remotePath(t.TempDir())

	tests := []struct {
		op   string
		call func() error
	}{
		{op: "mkdirall", call: func() error { return b.MkdirAll(ctx, dir, 0o755) }},
		{op: "remove", call: func() error { return b.Remove(ctx, dir, fsops.RemoveOptions{}) }},
		{op: "hardlinktree", call: func() error { _, err := b.HardlinkTree(ctx, nil); return err }},
		{op: "reflinktree", call: func() error { _, err := b.ReflinkTree(ctx, nil); return err }},
		{op: "removetree", call: func() error {
			return b.RemoveTree(ctx, &fsops.TreeCreateResult{Files: []string{dir}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			require.ErrorIs(t, err, fsops.ErrUnsupported)
			// The message reaches the user through a failed job, so it has to
			// say which operation was refused.
			assert.Contains(t, err.Error(), tt.op)
		})
	}

	// A nil handle means nothing to remove — safe on every backend.
	require.NoError(t, b.RemoveTree(ctx, nil))
}

func TestCancelledContext(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := remotePath(t.TempDir())

	tests := []struct {
		name string
		call func() error
	}{
		{name: "stat", call: func() error { _, err := b.Stat(ctx, dir); return err }},
		{name: "lstat", call: func() error { _, err := b.Lstat(ctx, dir); return err }},
		{name: "readdir", call: func() error { _, err := b.ReadDir(ctx, dir); return err }},
		{name: "walkdir", call: func() error {
			_, err := b.WalkDir(ctx, dir, fsops.WalkOptions{})
			return err
		}},
		{name: "statfs", call: func() error { _, err := b.Statfs(ctx, dir); return err }},
		{name: "samefilesystem", call: func() error { _, err := b.SameFilesystem(ctx, dir, dir); return err }},
		{name: "mkdirall", call: func() error { return b.MkdirAll(ctx, dir, 0o755) }},
		{name: "remove", call: func() error { return b.Remove(ctx, dir, fsops.RemoveOptions{}) }},
		{name: "hardlinktree", call: func() error { _, err := b.HardlinkTree(ctx, nil); return err }},
		{name: "reflinktree", call: func() error { _, err := b.ReflinkTree(ctx, nil); return err }},
		{name: "removetree", call: func() error { return b.RemoveTree(ctx, nil) }},
		{name: "supportsreflink", call: func() error { _, _, err := b.SupportsReflink(ctx, dir); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, tt.call(), context.Canceled)
		})
	}
}

func TestReconnectsAfterServerDropsConnection(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	dir := remotePath(t.TempDir())

	_, err := b.Stat(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, 1, server.Accepts())

	server.DropConnections()

	// The pool's watcher clears the dead entry asynchronously, so the redial
	// is what is being waited for here, not the drop.
	require.Eventually(t, func() bool {
		_, err := b.Stat(t.Context(), dir)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, 2, server.Accepts())
}

// extensionNames reads the extensions the in-process server currently
// advertises, through a client, since pkg/sftp exposes no getter for its list.
func extensionNames(t *testing.T) []string {
	t.Helper()

	b, _ := newBackend(t)
	client, err := b.client(t.Context())
	require.NoError(t, err)
	var names []string
	for _, name := range []string{"hardlink@openssh.com", "posix-rename@openssh.com", "statvfs@openssh.com", "fsync@openssh.com"} {
		if _, ok := client.HasExtension(name); ok {
			names = append(names, name)
		}
	}
	return names
}
