// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsexec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRoots(t *testing.T) (*Roots, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "subdir", "file.txt"), []byte("data"), 0o600))
	roots, err := OpenRoots([]string{root})
	require.NoError(t, err)
	t.Cleanup(func() { roots.Close() })
	return roots, root
}

func TestResolveSafe_ValidPath(t *testing.T) {
	roots, root := setupRoots(t)
	sr, rel, err := roots.ResolveSafe(filepath.Join(root, "subdir", "file.txt"), false)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("subdir", "file.txt"), rel)
	assert.Equal(t, root, sr.Path())
}

func TestResolveSafe_RootItself(t *testing.T) {
	roots, root := setupRoots(t)

	// Non-destructive: root itself is OK.
	sr, rel, err := roots.ResolveSafe(root, false)
	require.NoError(t, err)
	assert.Equal(t, ".", rel)
	assert.NotNil(t, sr)

	// Destructive: root itself is rejected.
	_, _, err = roots.ResolveSafe(root, true)
	require.ErrorIs(t, err, ErrRootItself)
}

func TestResolveSafe_EmptyPath(t *testing.T) {
	roots, _ := setupRoots(t)
	_, _, err := roots.ResolveSafe("", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)
}

func TestResolveSafe_RelativePath(t *testing.T) {
	roots, _ := setupRoots(t)
	_, _, err := roots.ResolveSafe("subdir/file.txt", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)
}

func TestResolveSafe_NULByte(t *testing.T) {
	roots, root := setupRoots(t)
	_, _, err := roots.ResolveSafe(root+"/sub\x00dir", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)
	assert.Contains(t, err.Error(), "NUL")
}

func TestResolveSafe_DotDot(t *testing.T) {
	roots, root := setupRoots(t)

	// "../" in a clean path would have been removed by filepath.Clean,
	// so the path isn't clean and is rejected.
	_, _, err := roots.ResolveSafe(root+"/../etc/passwd", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)
	assert.Contains(t, err.Error(), "not clean")
}

func TestResolveSafe_UncleanPath(t *testing.T) {
	roots, root := setupRoots(t)

	// Double separator.
	_, _, err := roots.ResolveSafe(root+"//subdir", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)

	// Trailing separator.
	_, _, err = roots.ResolveSafe(root+"/subdir/", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)
}

func TestResolveSafe_OutsideRoot(t *testing.T) {
	roots, _ := setupRoots(t)
	_, _, err := roots.ResolveSafe("/etc/passwd", false)
	require.ErrorIs(t, err, ErrPathNotAllowed)
}

func TestResolveSafe_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root1, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root2, "b.txt"), []byte("b"), 0o600))

	roots, err := OpenRoots([]string{root1, root2})
	require.NoError(t, err)
	defer roots.Close()

	sr1, rel1, err := roots.ResolveSafe(filepath.Join(root1, "a.txt"), false)
	require.NoError(t, err)
	assert.Equal(t, "a.txt", rel1)
	assert.Equal(t, root1, sr1.Path())

	sr2, rel2, err := roots.ResolveSafe(filepath.Join(root2, "b.txt"), false)
	require.NoError(t, err)
	assert.Equal(t, "b.txt", rel2)
	assert.Equal(t, root2, sr2.Path())
}

func TestResolveSafe_SymlinkEscape(t *testing.T) {
	roots, root := setupRoots(t)

	// Create a symlink inside the root that points outside.
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))

	// ResolveSafe only validates the path prefix — it doesn't follow symlinks.
	// The os.Root handle itself prevents symlink escape.
	sr, rel, err := roots.ResolveSafe(filepath.Join(root, "escape", "secret.txt"), false)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("escape", "secret.txt"), rel)

	// But the actual os.Root operation should reject the symlink escape.
	_, statErr := sr.Root().Lstat(rel)
	// os.Root follows symlinks but rejects absolute symlinks and ones that escape.
	// The symlink points to an absolute path outside the root, so this should fail.
	require.Error(t, statErr, "os.Root should reject symlink escape")
}

func TestResolveSafe_SymlinkWithinRoot(t *testing.T) {
	roots, root := setupRoots(t)

	// Create a relative symlink within the root (allowed by os.Root).
	require.NoError(t, os.Symlink("subdir/file.txt", filepath.Join(root, "link.txt")))

	sr, rel, err := roots.ResolveSafe(filepath.Join(root, "link.txt"), false)
	require.NoError(t, err)

	// os.Root follows relative symlinks within the root.
	fi, err := sr.Root().Lstat(rel)
	require.NoError(t, err)
	assert.NotEqual(t, os.FileMode(0), fi.Mode()&os.ModeSymlink, "Lstat should report symlink")
}

func TestCheckDeviceID_SameDevice(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.txt"), []byte("data"), 0o600))

	sr, err := OpenSafeRoot(root)
	require.NoError(t, err)
	defer sr.Close()

	require.NoError(t, sr.CheckDeviceID("file.txt"))
}

func TestOpenRoots_Empty(t *testing.T) {
	_, err := OpenRoots(nil)
	require.Error(t, err)
}

func TestOpenRoots_NonexistentDir(t *testing.T) {
	_, err := OpenRoots([]string{"/nonexistent/dir/xyz"})
	require.Error(t, err)
}

func TestOpenRoots_NotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("data"), 0o600))
	_, err := OpenRoots([]string{f})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestValidatePath_PATHMAXBoundary(t *testing.T) {
	// Very long but valid path should pass validation.
	segments := make([]string, 0, 50)
	for range 50 {
		segments = append(segments, "abcdefghijklmnop")
	}
	long := "/" + filepath.Join(segments...)
	err := validatePath(long)
	require.NoError(t, err)
}
