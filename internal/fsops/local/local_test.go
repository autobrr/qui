// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

func TestPortableNotExistErrors(t *testing.T) {
	// The Backend contract promises errors.Is(err, fs.ErrNotExist) for
	// missing paths from every read/mutate method, not just Stat — remote
	// backends must map onto the same sentinels, so the local backend is
	// the reference.
	b := newBackend()
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "nope", "missing.mkv")

	_, err := b.Stat(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = b.Lstat(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = b.ReadDir(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = b.WalkDir(ctx, missing, fsops.WalkOptions{})
	require.ErrorIs(t, err, fs.ErrNotExist)
	err = b.Remove(ctx, missing, fsops.RemoveOptions{})
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = b.Statfs(ctx, missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestStat_FollowsSymlinkIdentity(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	target := filepath.Join(dir, "data.mkv")
	writeFile(t, target, "content")
	link := filepath.Join(dir, "link.mkv")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	viaLink, err := b.Stat(context.Background(), link)
	require.NoError(t, err)
	direct, err := b.Stat(context.Background(), target)
	require.NoError(t, err)

	// Stat follows the link: metadata and identity describe the target, so
	// symlinked torrent data keeps already-seeding detection.
	assert.False(t, viaLink.IsSymlink)
	assert.Equal(t, direct.Size, viaLink.Size)
	require.NoError(t, viaLink.FileIDErr)
	assert.False(t, viaLink.FileID.IsZero())
	assert.Equal(t, direct.FileID, viaLink.FileID)
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
	_, err := b.Stat(context.Background(), filepath.Join(t.TempDir(), "nonexistent", "path"))
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

func TestLstat_DirectoryHasIdentity(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()

	info, err := b.Lstat(context.Background(), dir)
	require.NoError(t, err)
	assert.False(t, info.FileID.IsZero())
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

func TestReadDir(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	writeFile(t, filepath.Join(dir, "b.txt"), "b")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))

	entries, err := b.ReadDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	assert.Contains(t, names, "a.txt")
	assert.Contains(t, names, "b.txt")
	assert.Contains(t, names, "subdir")
}

// syslessFileInfo is a regular-file FileInfo whose Sys() carries no
// platform stat data, so hardlink.GetFileID must fail on it.
type syslessFileInfo struct{}

func (syslessFileInfo) Name() string       { return "x.mkv" }
func (syslessFileInfo) Size() int64        { return 42 }
func (syslessFileInfo) Mode() fs.FileMode  { return 0 }
func (syslessFileInfo) ModTime() time.Time { return time.Unix(1700000000, 0) }
func (syslessFileInfo) IsDir() bool        { return false }
func (syslessFileInfo) Sys() any           { return nil }

func TestLstatConversion_FileIDFailureDegrades(t *testing.T) {
	// Identity failure must land in FileIDErr with the rest of the
	// metadata intact — not fail the conversion (one identity-opaque
	// file must not abort callers that only need size/mtime).
	path := filepath.Join(t.TempDir(), "missing", "x.mkv")
	info := osFileInfoToLstat(syslessFileInfo{}, path)

	require.Error(t, info.FileIDErr)
	assert.True(t, info.FileID.IsZero())
	assert.Equal(t, path, info.Path)
	assert.Equal(t, int64(42), info.Size)
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

func TestWalkDir_IgnoreDirNamesAndPrefixes(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "k")
	writeFile(t, filepath.Join(dir, "node_modules", "pkg.js"), "p")
	writeFile(t, filepath.Join(dir, "$recycle.bin", "old.mkv"), "r")
	writeFile(t, filepath.Join(dir, ".trash-1000", "deleted.mkv"), "t")

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{
		IgnoreDirNames:        []string{"node_modules", "$RECYCLE.BIN"},
		IgnoreDirNamePrefixes: []string{".Trash-"},
	})
	require.NoError(t, err)

	var relPaths []string
	for e := range ch {
		relPaths = append(relPaths, e.RelPath)
	}
	assert.Contains(t, relPaths, "keep.txt")
	assert.NotContains(t, relPaths, filepath.Join("node_modules", "pkg.js"))
	// Matching is case-insensitive: metadata dir case varies on disk.
	assert.NotContains(t, relPaths, filepath.Join("$recycle.bin", "old.mkv"))
	assert.NotContains(t, relPaths, filepath.Join(".trash-1000", "deleted.mkv"))
}

func TestWalkDir_ContextCancellation(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	// More files than the walk channel buffers (64), so the walk cannot
	// complete before the first receive and cancellation must cut it short.
	const total = 150
	for i := range total {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), "x")
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
	// After cancel, at most the buffered entries (64) plus one in-flight
	// send can still arrive; anything near the full tree means the walk
	// ignored cancellation.
	assert.Less(t, count, 100)
}

func TestStatfs_FilePath(t *testing.T) {
	// The contract is "the filesystem containing path" — a regular file is a
	// valid argument. unix.Statfs accepts files natively; the Windows
	// implementation resolves to the containing directory.
	b := newBackend()
	dir := t.TempDir()
	file := filepath.Join(dir, "content.mkv")
	writeFile(t, file, "data")

	result, err := b.Statfs(context.Background(), file)
	require.NoError(t, err)
	assert.Positive(t, result.BytesTotal)
}

func TestWalkDir_UnreadableSubdirEmitsEntryErrAndContinues(t *testing.T) {
	// The per-entry Err path is the load-bearing half of the WalkDir error
	// contract: an unreadable subdirectory must surface as an entry with Err
	// set while the rest of the walk continues.
	if runtime.GOOS == "windows" {
		t.Skip("0o000 permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	b := newBackend()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "readable.txt"), "r")
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	writeFile(t, filepath.Join(locked, "hidden.txt"), "h")
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	ch, err := b.WalkDir(context.Background(), dir, fsops.WalkOptions{})
	require.NoError(t, err)

	var errEntries, okPaths []string
	for e := range ch {
		if e.Err != nil {
			errEntries = append(errEntries, e.Path)
		} else {
			okPaths = append(okPaths, e.RelPath)
		}
	}
	assert.Contains(t, errEntries, locked, "unreadable dir surfaces as an entry with Err")
	assert.Contains(t, okPaths, "readable.txt", "walk continues past the unreadable dir")
}

func TestWalkDir_NonexistentRoot(t *testing.T) {
	b := newBackend()
	_, err := b.WalkDir(context.Background(), filepath.Join(t.TempDir(), "nonexistent", "root"), fsops.WalkOptions{})
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

func TestHardlinkTree_SkippedExists(t *testing.T) {
	b := newBackend()
	dir := t.TempDir()
	src := filepath.Join(dir, "source", "a.mkv")
	writeFile(t, src, "video data")

	treeRoot := filepath.Join(dir, "tree")
	plan := &hardlinktree.TreePlan{
		RootDir: treeRoot,
		Files: []hardlinktree.FilePlan{
			{SourcePath: src, TargetPath: filepath.Join(treeRoot, "a.mkv")},
		},
	}

	first, err := b.HardlinkTree(context.Background(), plan)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Created)
	assert.Equal(t, 0, first.SkippedExists)

	// Re-creating the same plan is idempotent: the existing link is skipped
	// and NOT recorded as this call's work.
	second, err := b.HardlinkTree(context.Background(), plan)
	require.NoError(t, err)
	assert.Equal(t, 0, second.Created)
	assert.Equal(t, 1, second.SkippedExists)
	assert.Empty(t, second.Files)

	// Removing the second (empty) result must leave the first call's link alone.
	require.NoError(t, b.RemoveTree(context.Background(), second))
	_, err = os.Stat(filepath.Join(treeRoot, "a.mkv"))
	require.NoError(t, err)
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
