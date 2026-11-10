// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"io/fs"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestMkdirAll_WalksUpOverExistingPrefix(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	target := remotePath(dir, "a", "b", "c")

	require.NoError(t, b.MkdirAll(t.Context(), target, fsutil.ContentDirMode))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Idempotent, like os.MkdirAll.
	require.NoError(t, b.MkdirAll(t.Context(), target, fsutil.ContentDirMode))

	// The created list is what rollback removes, so a pre-existing prefix
	// must never be on it.
	client, err := b.client(t.Context())
	require.NoError(t, err)
	created, err := mkdirAll(t.Context(), client, remotePath(dir, "a", "b", "c", "d", "e"))
	require.NoError(t, err)
	assert.Equal(t, []string{remotePath(dir, "a", "b", "c", "d"), remotePath(dir, "a", "b", "c", "d", "e")}, created)
}

func TestMkdirAll_SymlinkedAncestorIsADirectory(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	target := remotePath(dir, "target")
	require.NoError(t, os.Mkdir(target, 0o755))
	link := remotePath(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Like os.MkdirAll: the child lands in the target, and the link itself
	// is never recorded as created.
	client, err := b.client(t.Context())
	require.NoError(t, err)
	created, err := mkdirAll(t.Context(), client, remotePath(link, "child"))
	require.NoError(t, err)
	assert.Equal(t, []string{remotePath(link, "child")}, created)
	info, err := os.Stat(remotePath(target, "child"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestMkdirAll_FileInTheWay(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	file := remotePath(dir, "file")
	writeFile(t, file, "x")

	err := b.MkdirAll(t.Context(), remotePath(dir, "file", "child"), fsutil.ContentDirMode)
	require.ErrorIs(t, err, syscall.ENOTDIR)
}

func TestMkdir_SecondCallSeesExist(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	target := remotePath(dir, "Movie")

	require.NoError(t, b.Mkdir(t.Context(), target, fsutil.ContentDirMode))
	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// The server answers SSH_FX_FAILURE for an existing path. A caller that
	// reserves a folder name needs fs.ErrExist.
	require.ErrorIs(t, b.Mkdir(t.Context(), target, fsutil.ContentDirMode), fs.ErrExist)

	require.NoError(t, os.WriteFile(remotePath(dir, "file"), nil, 0o600))
	require.ErrorIs(t, b.Mkdir(t.Context(), remotePath(dir, "file"), fsutil.ContentDirMode), fs.ErrExist)
}

func TestRemove_File(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	// Option-like and shell-metacharacter names are bytes to sftp, nothing more.
	file := remotePath(dir, "-rf; rm $HOME `x`")
	writeFile(t, file, "x")

	require.NoError(t, b.Remove(t.Context(), file, fsops.RemoveOptions{}))
	_, err := os.Lstat(file)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRemove_NonRecursiveDirFailsWhenNotEmpty(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	sub := remotePath(dir, "sub")
	writeFile(t, remotePath(sub, "f"), "x")

	// Callers classify on ENOTEMPTY, which SSH_FX_FAILURE alone does not carry.
	// fsops.IsDirNotEmpty reads only the code, so this is all it can match.
	removeErr := b.Remove(t.Context(), sub, fsops.RemoveOptions{})
	require.ErrorIs(t, removeErr, syscall.ENOTEMPTY)
	require.True(t, fsops.IsDirNotEmpty(removeErr))
	_, err := os.Stat(sub)
	require.NoError(t, err)

	require.NoError(t, os.Remove(remotePath(sub, "f")))
	require.NoError(t, b.Remove(t.Context(), sub, fsops.RemoveOptions{}))
}

func TestRemove_Recursive_LeavesSymlinkTargetIntact(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	keep := remotePath(dir, "keep")
	writeFile(t, remotePath(keep, "precious"), "x")

	tree := remotePath(dir, "tree")
	writeFile(t, remotePath(tree, "a", "f1"), "x")
	writeFile(t, remotePath(tree, "f2"), "x")
	if err := os.Symlink(keep, remotePath(tree, "a", "linkdir")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	require.NoError(t, b.Remove(t.Context(), tree, fsops.RemoveOptions{Recursive: true}))

	_, err := os.Lstat(tree)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = os.Stat(remotePath(keep, "precious"))
	require.NoError(t, err, "the link target must survive")
}

func TestRemove_Recursive_ChildSwappedForSymlinkIsNotDescended(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	keep := remotePath(dir, "keep")
	writeFile(t, remotePath(keep, "precious"), "x")
	// The listing saw a directory here; by the time the child is removed it
	// is a symlink to keep, the swap SFTP v3 cannot rule out.
	child := remotePath(dir, "child")
	if err := os.Symlink(keep, child); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	client, err := b.client(t.Context())
	require.NoError(t, err)

	require.NoError(t, removeListed(t.Context(), client, child, true))

	_, err = os.Lstat(child)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = os.Stat(remotePath(keep, "precious"))
	require.NoError(t, err, "the link target must survive")
}

func TestRemove_Recursive_SymlinkRootRemovesLinkOnly(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	keep := remotePath(dir, "keep")
	writeFile(t, remotePath(keep, "precious"), "x")
	link := remotePath(dir, "link")
	if err := os.Symlink(keep, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	require.NoError(t, b.Remove(t.Context(), link, fsops.RemoveOptions{Recursive: true}))

	_, err := os.Lstat(link)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = os.Stat(remotePath(keep, "precious"))
	require.NoError(t, err)
}

func TestRemove_Missing(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	missing := remotePath(t.TempDir(), "nope")
	// Like os.RemoveAll and os.Remove respectively.
	require.NoError(t, b.Remove(t.Context(), missing, fsops.RemoveOptions{Recursive: true}))
	require.ErrorIs(t, b.Remove(t.Context(), missing, fsops.RemoveOptions{}), fs.ErrNotExist)
}

// linkPlan is a two-file plan rooted under dir, with one nested target.
func linkPlan(t *testing.T, dir string) *hardlinktree.TreePlan {
	t.Helper()
	src1 := remotePath(dir, "src", "one.mkv")
	src2 := remotePath(dir, "src", "two.mkv")
	writeFile(t, src1, "one")
	writeFile(t, src2, "two")
	root := remotePath(dir, "links", "Show.S01")
	return &hardlinktree.TreePlan{
		RootDir: root,
		Files: []hardlinktree.FilePlan{
			{SourcePath: src1, TargetPath: remotePath(root, "one.mkv")},
			{SourcePath: src2, TargetPath: remotePath(root, "Sub -rf", "two.mkv")},
		},
	}
}

func TestHardlinkTree_CreateAndRemoveTree(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	// A pre-existing prefix must not be recorded as created.
	require.NoError(t, os.MkdirAll(remotePath(dir, "links"), 0o755))
	plan := linkPlan(t, dir)

	created, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, 2, created.Created)
	assert.Equal(t, 0, created.SkippedExists)
	assert.ElementsMatch(t, []string{plan.Files[0].TargetPath, plan.Files[1].TargetPath}, created.Files)
	assert.ElementsMatch(t, []string{plan.RootDir, remotePath(plan.RootDir, "Sub -rf")}, created.Dirs)

	srcInfo, err := os.Stat(plan.Files[1].SourcePath)
	require.NoError(t, err)
	dstInfo, err := os.Stat(plan.Files[1].TargetPath)
	require.NoError(t, err)
	assert.True(t, os.SameFile(srcInfo, dstInfo), "target must be a hard link to the source")

	require.NoError(t, b.RemoveTree(t.Context(), created))
	_, err = os.Lstat(plan.RootDir)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = os.Stat(remotePath(dir, "links"))
	require.NoError(t, err, "the pre-existing prefix stays")
	_, err = os.Stat(plan.Files[0].SourcePath)
	require.NoError(t, err, "sources are untouched")
}

func TestHardlinkTree_ExistingTargetFailsAndRollsBack(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	// The pool reads the tier from a probe of the live connection, so the
	// fake must refuse exec for this to be the sftp tier at all.
	server.SetExec(sshtest.ExecSFTPOnly)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	// Even a target that already links the source is a conflict over sftp:
	// without identity the backend cannot tell it from a stranger's file.
	require.NoError(t, os.MkdirAll(remotePath(plan.RootDir, "Sub -rf"), 0o755))
	require.NoError(t, os.Link(plan.Files[1].SourcePath, plan.Files[1].TargetPath))

	created, err := b.HardlinkTree(t.Context(), plan)
	require.ErrorIs(t, err, hardlinktree.ErrTargetExists, "link mode tells this case from other create errors")
	assert.Nil(t, created)

	_, err = os.Lstat(plan.Files[0].TargetPath)
	require.ErrorIs(t, err, fs.ErrNotExist, "the link made before the conflict is rolled back")
	_, err = os.Stat(plan.Files[1].TargetPath)
	require.NoError(t, err, "the pre-existing target is not ours to remove")
}

func TestRemoveTree_SkipsNonEmptyDir(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	created, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	stranger := remotePath(plan.RootDir, "Sub -rf", "stranger")
	writeFile(t, stranger, "x")

	require.NoError(t, b.RemoveTree(t.Context(), created))

	_, err = os.Lstat(plan.Files[0].TargetPath)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = os.Stat(stranger)
	require.NoError(t, err, "a directory holding someone else's file stays")
	require.NoError(t, b.RemoveTree(t.Context(), nil))
}

func TestReflinkTree_Unsupported(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	_, err := b.ReflinkTree(t.Context(), linkPlan(t, t.TempDir()))
	require.ErrorIs(t, err, fsops.ErrUnsupported)
}

func TestHardlinkTree_ServerWithoutExtension(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	server.SetSFTP(sshtest.SFTPNoHardlink)
	dir := t.TempDir()
	plan := linkPlan(t, dir)

	created, err := b.HardlinkTree(t.Context(), plan)
	require.ErrorIs(t, err, fsops.ErrUnsupported)
	assert.Nil(t, created)
	_, err = os.Lstat(plan.RootDir)
	require.ErrorIs(t, err, fs.ErrNotExist, "nothing is created before the gate")

	// Every other operation still works on that server.
	require.NoError(t, b.MkdirAll(t.Context(), remotePath(dir, "plain"), fsutil.ContentDirMode))
}

func TestRemove_RefusedUnlinkNeverReportsMissing(t *testing.T) {
	t.Parallel()

	b, server := newBackend(t)
	server.SetSFTP(sshtest.SFTPRefuseRemove)
	client, err := b.client(t.Context())
	require.NoError(t, err)
	// Remove and rmdir fail with different errors, so Client.Remove names the
	// error from a link-following Stat, which reads "not found" for this
	// dangling link. Our own lstat answers instead.
	require.NoError(t, client.Symlink("/missing", "/link"))

	err = b.Remove(t.Context(), "/link", fsops.RemoveOptions{})
	require.Error(t, err)
	require.NotErrorIs(t, err, fs.ErrNotExist)
	_, err = client.Lstat("/link")
	require.NoError(t, err, "the link is still there")
}

// cancelHeldWrite cancels once the server holds a write, then lets the server
// answer it. The caller checks that the held write landed before the call
// returned.
func cancelHeldWrite(t *testing.T, server *sshtest.Server, cancel context.CancelFunc, done <-chan error) error {
	t.Helper()
	select {
	case <-server.Stalled():
	case <-time.After(5 * time.Second):
		t.Fatal("the server never held the write")
	}
	cancel()
	server.ReleaseStall()
	return <-done
}

func TestHardlinkTree_CancelledWriteCompletesAndRollsBack(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		rootExists bool
		oneFile    bool
		landed     []string
	}{
		// Without the root, the first write on the wire is the Mkdir of
		// its first missing ancestor; with it, the Link.
		{name: "mkdir", landed: []string{"links"}},
		{name: "link", rootExists: true, landed: []string{"links", "Show.S01", "one.mkv"}},
		// The last write in the plan: no later request fails on the cancel.
		{name: "last link", rootExists: true, oneFile: true, landed: []string{"links", "Show.S01", "one.mkv"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b, server := newBackend(t)
			dir := t.TempDir()
			plan := linkPlan(t, dir)
			second := plan.Files[1].TargetPath
			if tc.oneFile {
				plan.Files = plan.Files[:1]
			}
			if tc.rootExists {
				require.NoError(t, os.MkdirAll(plan.RootDir, 0o755))
			}
			server.SetSFTP(sshtest.SFTPHoldNextMutation)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				_, err := b.HardlinkTree(ctx, plan)
				done <- err
			}()

			err := cancelHeldWrite(t, server, cancel, done)
			require.ErrorIs(t, err, context.Canceled)
			_, err = os.Lstat(remotePath(append([]string{dir}, tc.landed...)...))
			require.ErrorIs(t, err, fs.ErrNotExist, "the write that landed after the cancel is rolled back")
			_, err = os.Lstat(second)
			require.ErrorIs(t, err, fs.ErrNotExist)
		})
	}
}

func TestRemove_CancelledRemoveCompletes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts fsops.RemoveOptions
		// remove is the path removed, held the entry whose remove is held.
		remove, held string
	}{
		{name: "file", remove: "f", held: "f"},
		{name: "tree", opts: fsops.RemoveOptions{Recursive: true}, remove: "d", held: "d/f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b, server := newBackend(t)
			dir := t.TempDir()
			writeFile(t, remotePath(dir, "f"), "f")
			writeFile(t, remotePath(dir, "d", "f"), "f")
			server.SetSFTP(sshtest.SFTPHoldNextMutation)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			var held error
			go func() {
				err := b.Remove(ctx, remotePath(dir, tc.remove), tc.opts)
				// Read as the call returns: a call that does not wait for
				// its held remove returns before the remove lands.
				_, held = os.Lstat(remotePath(dir, tc.held))
				done <- err
			}()

			err := cancelHeldWrite(t, server, cancel, done)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, held, fs.ErrNotExist, "the held remove landed before the call returned")
		})
	}
}

// On the exec tier a target that is already a link to its source is skipped,
// the way the local backend skips it (#3237). The skip is not recorded: the
// link belongs to whoever made it first.
func TestHardlinkTree_ExecTierSkipsOwnLink(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	require.NoError(t, os.MkdirAll(remotePath(plan.RootDir, "Sub -rf"), 0o755))
	require.NoError(t, os.Link(plan.Files[1].SourcePath, plan.Files[1].TargetPath))

	created, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, 1, created.Created)
	assert.Equal(t, 1, created.SkippedExists)
	assert.Equal(t, []string{plan.Files[0].TargetPath}, created.Files, "the skipped link is not ours to record")

	for _, fp := range plan.Files {
		src, _ := unixID(t, fp.SourcePath, true)
		dst, _ := unixID(t, fp.TargetPath, false)
		assert.Equal(t, src, dst, "%s links its source", fp.TargetPath)
	}
}

// A stranger's file at the target is still the collision link mode refuses,
// on the exec tier as on sftp: identity is what tells the two apart.
func TestHardlinkTree_ExecTierStrangerStillRefuses(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	writeFile(t, plan.Files[1].TargetPath, "stranger")

	created, err := b.HardlinkTree(t.Context(), plan)
	require.ErrorIs(t, err, hardlinktree.ErrTargetExists)
	assert.Nil(t, created)

	_, err = os.Lstat(plan.Files[0].TargetPath)
	require.ErrorIs(t, err, fs.ErrNotExist, "the link made before the conflict is rolled back")
	body, err := os.ReadFile(plan.Files[1].TargetPath)
	require.NoError(t, err)
	assert.Equal(t, "stranger", string(body), "the stranger's file is untouched")
}

// A symlink at the target reports its own inode, never its target's, so it
// can not pass as the source and stays a refusal.
func TestHardlinkTree_ExecTierSymlinkAtTargetRefuses(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	require.NoError(t, os.MkdirAll(remotePath(plan.RootDir, "Sub -rf"), 0o755))
	require.NoError(t, os.Symlink(plan.Files[1].SourcePath, plan.Files[1].TargetPath))

	_, err := b.HardlinkTree(t.Context(), plan)
	require.ErrorIs(t, err, hardlinktree.ErrTargetExists)
}

// A skipped link survives a later failure in the same plan: it was never
// recorded, so rollback never reaches it.
func TestHardlinkTree_ExecTierSkippedLinkSurvivesLaterFailure(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	require.NoError(t, os.MkdirAll(plan.RootDir, 0o755))
	require.NoError(t, os.Link(plan.Files[0].SourcePath, plan.Files[0].TargetPath))
	writeFile(t, plan.Files[1].TargetPath, "stranger")

	_, err := b.HardlinkTree(t.Context(), plan)
	require.ErrorIs(t, err, hardlinktree.ErrTargetExists)

	src, _ := unixID(t, plan.Files[0].SourcePath, true)
	dst, _ := unixID(t, plan.Files[0].TargetPath, false)
	assert.Equal(t, src, dst, "the pre-existing link is left in place")
}

// A whole tree left on disk without its torrent is reused, which is what lets
// cross-seed recreate a torrent whose own earlier tree occupies its target.
func TestHardlinkTree_ExecTierReusesWholeTree(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	first, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 2, first.Created)

	again, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, 0, again.Created)
	assert.Equal(t, 2, again.SkippedExists)
	assert.Empty(t, again.Files)
	require.NoError(t, b.RemoveTree(t.Context(), again), "removing a result that recorded nothing is a no-op")
	for _, fp := range plan.Files {
		_, err := os.Lstat(fp.TargetPath)
		require.NoError(t, err, "%s still belongs to the first run", fp.TargetPath)
	}
}

// link(2) does not follow a symlinked source, so the link it makes carries
// the symlink's own inode; comparing both sides unfollowed is what lets a
// re-run recognise it.
func TestHardlinkTree_ExecTierSymlinkedSourceReuses(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	linked := plan.Files[0].SourcePath
	viaLink := remotePath(dir, "src", "one.via-symlink.mkv")
	require.NoError(t, os.Symlink(linked, viaLink))
	plan.Files[0].SourcePath = viaLink

	first, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 2, first.Created)
	again, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, 2, again.SkippedExists)
}

// A collision check cut short while the stat is running is the caller's
// error, never a permanent ErrTargetExists that tells the user to clean up
// their own link. The connection is opened and probed first, so the hang is
// in the stat itself and not in the connect.
func TestTargetIsSource_JobStoppedDuringStatIsAnError(t *testing.T) {
	t.Parallel()

	b, server := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	_, err := b.Stat(t.Context(), plan.Files[0].SourcePath)
	require.NoError(t, err)
	server.SetExec(sshtest.ExecHang)

	same, err := b.targetIsSource(cancelOnStall(t, server), plan.Files[0])
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, same)
}

// A source that vanished after the plan was built is not a reason to fall
// back to a regular add into the matched torrent's path: the occupied target
// still refuses, as it does on the sftp tier and the local backend.
func TestHardlinkTree_ExecTierMissingSourceStillRefuses(t *testing.T) {
	t.Parallel()

	b, _ := newExecBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	require.NoError(t, os.MkdirAll(plan.RootDir, 0o755))
	require.NoError(t, os.Link(plan.Files[0].SourcePath, plan.Files[0].TargetPath))
	require.NoError(t, os.Remove(plan.Files[0].SourcePath))

	_, err := b.HardlinkTree(t.Context(), plan)
	require.ErrorIs(t, err, hardlinktree.ErrTargetExists)
}

// The stored tier lags a key that just gained a shell: the probe writes the
// row, not this backend's copy. The live connection decides, so the first
// reuse after the upgrade already works.
func TestHardlinkTree_StaleStoredTierStillReusesOnExecHost(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("the fake exec handler runs the local GNU stat")
	}

	b, _ := newBackend(t) // row says sftp; the fake answers exec
	require.NotEqual(t, models.SSHTierExec, b.inst.SSHTier)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	require.NoError(t, os.MkdirAll(remotePath(plan.RootDir, "Sub -rf"), 0o755))
	require.NoError(t, os.Link(plan.Files[1].SourcePath, plan.Files[1].TargetPath))

	created, err := b.HardlinkTree(t.Context(), plan)
	require.NoError(t, err)
	assert.Equal(t, 1, created.SkippedExists)
}

// When Link fails and the lstat that follows fails too for a reason other
// than an absent target, that reason is the error, not the Link failure.
func TestHardlinkTree_LstatFailureAfterLinkIsReported(t *testing.T) {
	t.Parallel()
	// Host directory modes cannot inject this failure on Windows or as root.
	b, server := newBackend(t)
	server.SetSFTP(sshtest.SFTPRefuseLinkAndLstat)
	client, err := b.client(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.Mkdir("/links"))
	plan := &hardlinktree.TreePlan{
		RootDir: "/links",
		Files:   []hardlinktree.FilePlan{{SourcePath: "/source.mkv", TargetPath: "/links/one.mkv"}},
	}

	_, err = b.HardlinkTree(t.Context(), plan)
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrPermission)
	assert.NotContains(t, err.Error(), "hardlink ", "the lstat failure is the cause, not the link")
	assert.Contains(t, err.Error(), "lstat")
}
