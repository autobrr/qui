// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
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
}

func TestMkdirAll_FileInTheWay(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	file := remotePath(dir, "file")
	writeFile(t, file, "x")

	err := b.MkdirAll(t.Context(), remotePath(dir, "file", "child"), fsutil.ContentDirMode)
	require.Error(t, err)
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

	require.Error(t, b.Remove(t.Context(), sub, fsops.RemoveOptions{}))
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
	err := b.Remove(t.Context(), remotePath(t.TempDir(), "nope"), fsops.RemoveOptions{Recursive: true})
	require.ErrorIs(t, err, fs.ErrNotExist)
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

	b, _ := newBackend(t)
	dir := t.TempDir()
	plan := linkPlan(t, dir)
	// Even a target that already links the source is a conflict over sftp:
	// without identity the backend cannot tell it from a stranger's file.
	require.NoError(t, os.MkdirAll(remotePath(plan.RootDir, "Sub -rf"), 0o755))
	require.NoError(t, os.Link(plan.Files[1].SourcePath, plan.Files[1].TargetPath))

	created, err := b.HardlinkTree(t.Context(), plan)
	require.Error(t, err)
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
