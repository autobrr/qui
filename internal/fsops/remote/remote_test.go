// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
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

// fakeCreds stands in for the instance store: the dialer reads only these two
// values, so the tests need no database.
type fakeCreds struct {
	key string
	pin []byte
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

	pool := sshpool.NewPool(sshpool.NewDialer(fakeCreds{
		key: sshtest.PrivateKey(""),
		pin: hostKey.PublicKey().Marshal(),
	}))
	t.Cleanup(pool.Close)

	// SSHHostKeyEncrypted is what the pool keys its memo on, so it has to hold
	// something even though this fake decrypts to a fixed key.
	inst := &models.Instance{
		ID: 1, SSHHost: host, SSHPort: port, SSHUsername: "qui",
		SSHHostKeyEncrypted: "enc-v1",
	}
	return New(pool, inst), server
}

// remotePath converts a local test path to the slash form the remote side uses.
func remotePath(elem ...string) string {
	return filepath.ToSlash(filepath.Join(elem...))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestStat(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	file := remotePath(dir, "file.txt")
	writeFile(t, file, "hello")
	link := remotePath(dir, "link.txt")
	require.NoError(t, os.Symlink(file, link))

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
	require.NoError(t, os.Symlink(remotePath(dir, "a.txt"), remotePath(dir, "link.txt")))

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
}

func TestWalkDir_DoesNotDescendSymlinkedDir(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, remotePath(dir, "target", "inside.txt"), "i")
	require.NoError(t, os.Symlink(remotePath(dir, "target"), remotePath(dir, "link")))

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

func TestStatfs(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	result, err := b.Statfs(t.Context(), remotePath(t.TempDir()))
	require.NoError(t, err)
	assert.Positive(t, result.BytesAvailable)
	assert.Positive(t, result.BytesTotal)
	assert.LessOrEqual(t, result.BytesAvailable, result.BytesTotal)
}

func TestStatfsWithoutStatvfsExtension(t *testing.T) {
	// SetSFTPExtensions mutates a package global, so this test cannot run in
	// parallel with the ones that need statvfs advertised.
	require.NoError(t, sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com"))
	t.Cleanup(func() {
		require.NoError(t, sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com", "statvfs@openssh.com"))
	})

	b, _ := newBackend(t)
	dir := remotePath(t.TempDir())

	_, err := b.Statfs(t.Context(), dir)
	require.ErrorIs(t, err, fsops.ErrUnsupported)
	assert.Contains(t, err.Error(), "statvfs@openssh.com")
	assert.Contains(t, err.Error(), dir)

	_, err = b.SameFilesystem(t.Context(), dir, dir)
	require.ErrorIs(t, err, fsops.ErrUnsupported)
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
