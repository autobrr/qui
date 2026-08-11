// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func newBackend() *Backend { return NewBackend() }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestStat_ExistingFile(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeFile(t, path, "hello")

	fi, err := b.Stat(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, path, fi.Path)
	assert.Equal(t, int64(5), fi.Size)
	assert.False(t, fi.IsDir)
	assert.False(t, fi.IsSymlink)
	assert.False(t, fi.ModTime.IsZero())
}

func TestStat_Directory(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()

	fi, err := b.Stat(context.Background(), dir)
	require.NoError(t, err)
	assert.True(t, fi.IsDir)
}

func TestStat_NotFound(t *testing.T) {
	b := newBackend()
	_, err := b.Stat(context.Background(), "/nonexistent/path/xyz")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestStat_CancelledContext(t *testing.T) {
	b := newBackend()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Stat(ctx, t.TempDir())
	require.ErrorIs(t, err, context.Canceled)
}

func TestStatBatch(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.txt")
	writeFile(t, existing, "data")
	missing := filepath.Join(dir, "missing.txt")

	infos, errs, err := b.StatBatch(context.Background(), []string{existing, missing})
	require.NoError(t, err)
	require.Len(t, infos, 2)
	require.Len(t, errs, 2)

	assert.NotNil(t, infos[0])
	require.NoError(t, errs[0])
	assert.Equal(t, int64(4), infos[0].Size)

	assert.Nil(t, infos[1])
	assert.True(t, os.IsNotExist(errs[1]))
}

func TestLstat_RegularFile(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeFile(t, path, "data")

	info, err := b.Lstat(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, path, info.Path)
	assert.False(t, info.IsSymlink)
	assert.False(t, info.FileID.IsZero())
	assert.Equal(t, uint64(1), info.Nlinks)
}

func TestLstat_Symlink(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeFile(t, target, "data")
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.Symlink(target, link))

	info, err := b.Lstat(context.Background(), link)
	require.NoError(t, err)
	assert.True(t, info.IsSymlink)
	// Symlinks are not regular files, so FileID should be zero.
	assert.True(t, info.FileID.IsZero())
}

func TestLstat_Hardlink(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	original := filepath.Join(dir, "original.txt")
	writeFile(t, original, "shared")
	hardlink := filepath.Join(dir, "hardlink.txt")
	require.NoError(t, os.Link(original, hardlink))

	origInfo, err := b.Lstat(context.Background(), original)
	require.NoError(t, err)
	linkInfo, err := b.Lstat(context.Background(), hardlink)
	require.NoError(t, err)

	assert.Equal(t, origInfo.FileID, linkInfo.FileID)
	assert.Equal(t, uint64(2), origInfo.Nlinks)
	assert.Equal(t, uint64(2), linkInfo.Nlinks)
}

func TestLstatBatch(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	writeFile(t, f1, "aaa")
	missing := filepath.Join(dir, "missing.txt")

	infos, errs, err := b.LstatBatch(context.Background(), []string{f1, missing})
	require.NoError(t, err)
	assert.NotNil(t, infos[0])
	require.NoError(t, errs[0])
	assert.Nil(t, infos[1])
	assert.True(t, os.IsNotExist(errs[1]))
}

func TestReadDir(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	writeFile(t, filepath.Join(dir, "b.txt"), "b")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))

	entries, truncated, err := b.ReadDir(context.Background(), dir, 0)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Len(t, entries, 3)

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	assert.Contains(t, names, "a.txt")
	assert.Contains(t, names, "b.txt")
	assert.Contains(t, names, "subdir")
}

func TestReadDir_Truncated(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	for i := range 5 {
		writeFile(t, filepath.Join(dir, string(rune('a'+i))+".txt"), "x")
	}

	entries, truncated, err := b.ReadDir(context.Background(), dir, 2)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.Len(t, entries, 2)
}

func TestWalkDir_Basic(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeFile(t, filepath.Join(dir, "sub", "b.txt"), "bbb")

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{})
	require.NoError(t, err)

	var entries []fsops.WalkEntry
	for e := range ch {
		entries = append(entries, e)
	}

	// Should have: dir itself, a.txt, sub/, sub/b.txt
	assert.GreaterOrEqual(t, len(entries), 4)

	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.RelPath
	}
	assert.Contains(t, paths, "a.txt")
	assert.Contains(t, paths, filepath.Join("sub", "b.txt"))
}

func TestWalkDir_SkipHidden(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "visible.txt"), "v")
	writeFile(t, filepath.Join(dir, ".hidden"), "h")
	writeFile(t, filepath.Join(dir, ".hiddendir", "inside.txt"), "i")

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{SkipHidden: true})
	require.NoError(t, err)

	var relPaths []string
	for e := range ch {
		relPaths = append(relPaths, e.RelPath)
	}
	assert.Contains(t, relPaths, "visible.txt")
	assert.NotContains(t, relPaths, ".hidden")
	assert.NotContains(t, relPaths, filepath.Join(".hiddendir", "inside.txt"))
}

func TestWalkDir_IgnoreDirNames(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "k")
	writeFile(t, filepath.Join(dir, "node_modules", "pkg.js"), "p")

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{IgnoreDirNames: []string{"node_modules"}})
	require.NoError(t, err)

	var relPaths []string
	for e := range ch {
		relPaths = append(relPaths, e.RelPath)
	}
	assert.Contains(t, relPaths, "keep.txt")
	assert.NotContains(t, relPaths, filepath.Join("node_modules", "pkg.js"))
}

func TestWalkDir_MaxEntries(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	for i := range 10 {
		writeFile(t, filepath.Join(dir, string(rune('a'+i))+".txt"), "x")
	}

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{MaxEntries: 3})
	require.NoError(t, err)

	var count int
	for range ch {
		count++
	}
	assert.LessOrEqual(t, count, 3)
}

func TestWalkDir_ContextCancellation(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	for i := range 50 {
		writeFile(t, filepath.Join(dir, string(rune('a'+i%26))+"_"+string(rune('0'+i/26))+".txt"), "x")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := b.WalkDir(ctx, dir, fsops.WalkOptions{})
	require.NoError(t, err)

	// Read a few entries then cancel.
	count := 0
	for range ch {
		count++
		if count >= 3 {
			cancel()
			break
		}
	}
	// Drain remaining entries (channel should close promptly).
	for range ch {
		count++
	}
	// Should have stopped well before 50+1 entries.
	assert.Less(t, count, 52)
}

func TestWalkDir_NonexistentRoot(t *testing.T) {
	b := newBackend()
	_, err := b.WalkDir(context.Background(), "/nonexistent/root/xyz", fsops.WalkOptions{})
	require.Error(t, err)
}

func TestWalkDir_WithFileID(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "file.txt"), "data")

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{WantFileID: true})
	require.NoError(t, err)

	foundFileWithID := false
	for e := range ch {
		if e.RelPath == "file.txt" && !e.FileID.IsZero() {
			foundFileWithID = true
		}
	}
	assert.True(t, foundFileWithID)
}

func TestStatfs(t *testing.T) {
	b := newBackend()
	result, err := b.Statfs(context.Background(), t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, result.BytesAvailable)
	assert.Positive(t, result.BytesTotal)
	assert.LessOrEqual(t, result.BytesAvailable, result.BytesTotal)
}

func TestSameFilesystem_SameDir(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	d1 := filepath.Join(dir, "a")
	d2 := filepath.Join(dir, "b")
	require.NoError(t, os.Mkdir(d1, 0o755))
	require.NoError(t, os.Mkdir(d2, 0o755))

	same, err := b.SameFilesystem(context.Background(), d1, d2)
	require.NoError(t, err)
	assert.True(t, same)
}

func TestFileID(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeFile(t, path, "data")

	fid, nlinks, err := b.FileID(context.Background(), path)
	require.NoError(t, err)
	assert.False(t, fid.IsZero())
	assert.Equal(t, uint64(1), nlinks)
}

func TestFileID_Hardlinked(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	f1 := filepath.Join(dir, "f1.txt")
	writeFile(t, f1, "shared")
	f2 := filepath.Join(dir, "f2.txt")
	require.NoError(t, os.Link(f1, f2))

	fid1, nl1, err := b.FileID(context.Background(), f1)
	require.NoError(t, err)
	fid2, nl2, err := b.FileID(context.Background(), f2)
	require.NoError(t, err)

	assert.Equal(t, fid1, fid2)
	assert.Equal(t, uint64(2), nl1)
	assert.Equal(t, uint64(2), nl2)
}

func TestMkdirAll(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c")

	require.NoError(t, b.MkdirAll(context.Background(), deep, 0o755))

	fi, err := os.Stat(deep)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestRemove_File(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeFile(t, path, "data")

	require.NoError(t, b.Remove(context.Background(), path, fsops.RemoveOptions{}))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestRemove_Recursive(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	writeFile(t, filepath.Join(sub, "file.txt"), "data")

	require.NoError(t, b.Remove(context.Background(), sub, fsops.RemoveOptions{Recursive: true}))
	_, err := os.Stat(sub)
	assert.True(t, os.IsNotExist(err))
}

func TestRemove_NonRecursive_DirFails(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	writeFile(t, filepath.Join(sub, "file.txt"), "data")

	err := b.Remove(context.Background(), sub, fsops.RemoveOptions{})
	require.Error(t, err, "removing non-empty dir without recursive should fail")
}

func TestHardlinkTree_CreateAndRemove(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()

	// Create source files.
	src1 := filepath.Join(dir, "source", "a.mkv")
	src2 := filepath.Join(dir, "source", "b.srt")
	writeFile(t, src1, "video data")
	writeFile(t, src2, "subtitle data")

	treeRoot := filepath.Join(dir, "tree")
	plan := &hardlinktree.TreePlan{
		RootDir: treeRoot,
		Files: []hardlinktree.FilePlan{
			{SourcePath: src1, TargetPath: filepath.Join(treeRoot, "a.mkv")},
			{SourcePath: src2, TargetPath: filepath.Join(treeRoot, "b.srt")},
		},
	}

	// Create the tree.
	result, err := b.HardlinkTree(context.Background(), plan)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Created)
	assert.False(t, result.RolledBack)

	// Verify files exist and are hardlinks.
	fi1, err := os.Stat(filepath.Join(treeRoot, "a.mkv"))
	require.NoError(t, err)
	assert.Equal(t, int64(10), fi1.Size())

	srcFi, err := os.Stat(src1)
	require.NoError(t, err)
	assert.True(t, os.SameFile(srcFi, fi1))

	// Remove the tree using the create result's recorded files/dirs.
	require.NoError(t, b.RemoveTree(context.Background(), result))
	_, err = os.Stat(filepath.Join(treeRoot, "a.mkv"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(treeRoot)
	assert.True(t, os.IsNotExist(err))
}

func TestSupportsReflink(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()

	supported, reason, err := b.SupportsReflink(context.Background(), dir)
	require.NoError(t, err)
	// Result depends on the filesystem, but the call should not error.
	_ = supported
	_ = reason
}

func TestInfo(t *testing.T) {
	b := newBackend()
	info, err := b.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "local", info.Kind)
	assert.Empty(t, info.HelperVersion)
}

func TestHealthCheck(t *testing.T) {
	b := newBackend()
	require.NoError(t, b.HealthCheck(context.Background()))
}

func TestWalkDir_IgnorePaths(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "k")
	ignoredFile := filepath.Join(dir, "ignored.txt")
	writeFile(t, ignoredFile, "i")

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{
		IgnorePaths: []string{ignoredFile},
	})
	require.NoError(t, err)

	var relPaths []string
	for e := range ch {
		relPaths = append(relPaths, e.RelPath)
	}
	assert.Contains(t, relPaths, "keep.txt")
	assert.NotContains(t, relPaths, "ignored.txt")
}

func TestStatBatch_CancelledMidway(t *testing.T) {
	b := newBackend()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	// Let the timeout fire.
	time.Sleep(1 * time.Millisecond)

	_, _, err := b.StatBatch(ctx, []string{"/any/path"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
