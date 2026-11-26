// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

func openRoot(t *testing.T, b *Backend, dir string) fsops.Root {
	t.Helper()
	root, err := b.OpenRoot(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func create(t *testing.T, root fsops.Root, name, content string) {
	t.Helper()
	w, err := root.Create(t.Context(), name)
	require.NoError(t, err)
	_, err = w.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func TestRoot_RenameNeverReplaces(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		hardlinks bool
	}{
		{name: "hardlinks, rename replaces", hardlinks: true},
		{name: "no hardlinks, rename replaces"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b, server := newBackend(t)
			if !tt.hardlinks {
				server.SetSFTP(sshtest.SFTPNoHardlink)
			}
			dir := t.TempDir()
			root := openRoot(t, b, remotePath(dir))
			create(t, root, "Sub/a.bin.tmp", "ours")
			create(t, root, "Sub/b.bin.tmp", "ours too")
			writeFile(t, filepath.Join(dir, "Sub", "b.bin"), "theirs")

			require.NoError(t, root.RenameNoReplace(t.Context(), "Sub/a.bin.tmp", "Sub/a.bin"))
			err := root.RenameNoReplace(t.Context(), "Sub/b.bin.tmp", "Sub/b.bin")

			require.ErrorIs(t, err, fs.ErrExist)
			for name, want := range map[string]string{"a.bin": "ours", "b.bin.tmp": "ours too", "b.bin": "theirs"} {
				got, err := os.ReadFile(filepath.Join(dir, "Sub", name))
				require.NoError(t, err, name)
				assert.Equal(t, want, string(got), name)
			}
			_, err = root.Lstat(t.Context(), "Sub/a.bin.tmp")
			require.ErrorIs(t, err, fs.ErrNotExist)
		})
	}
}

func TestRoot_CreatesFilesTheGroupCanWrite(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := filepath.Join(t.TempDir(), "save")
	root := openRoot(t, b, remotePath(dir))

	create(t, root, "Show.S01-GRP/Sub/a.bin", "data")

	got, err := os.ReadFile(filepath.Join(dir, "Show.S01-GRP", "Sub", "a.bin"))
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))
	info, err := root.Lstat(t.Context(), "Show.S01-GRP/Sub/a.bin")
	require.NoError(t, err)
	assert.Equal(t, int64(4), info.Size)
	if runtime.GOOS == "windows" {
		return
	}
	for p, want := range map[string]fs.FileMode{
		dir:                                0o775,
		filepath.Join(dir, "Show.S01-GRP"): 0o775,
		filepath.Join(dir, "Show.S01-GRP", "Sub"):          0o775,
		filepath.Join(dir, "Show.S01-GRP", "Sub", "a.bin"): 0o664,
	} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, want, info.Mode().Perm(), p)
	}
}

func TestRoot_CreateNeverOpensAnExistingFile(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.bin"), "theirs")
	root := openRoot(t, b, remotePath(dir))

	_, err := root.Create(t.Context(), "a.bin")

	require.ErrorIs(t, err, fs.ErrExist)
	got, err := os.ReadFile(filepath.Join(dir, "a.bin"))
	require.NoError(t, err)
	assert.Equal(t, "theirs", string(got))
}

func TestRoot_StaysInsideItsFolder(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	parent := t.TempDir()
	root := openRoot(t, b, remotePath(parent, "save"))

	for _, name := range []string{"../outside.bin", "/outside.bin", `..\outside.bin`, "C:/outside.bin", "Sub/../../outside.bin", "./outside.bin"} {
		_, err := root.Create(t.Context(), name)
		require.Error(t, err, name)
	}
	t.Run("through a symlink", func(t *testing.T) {
		err := os.Symlink(parent, filepath.Join(parent, "save", "link"))
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink creation requires Developer Mode or SeCreateSymbolicLinkPrivilege: %v", err)
		}
		require.NoError(t, err)
		_, err = root.Create(t.Context(), "link/outside.bin")
		require.Error(t, err)
		_, err = root.OpenWrite(t.Context(), "link/outside.bin", 0)
		require.Error(t, err)
		require.Error(t, root.RenameNoReplace(t.Context(), "a.bin", "link/outside.bin"))
	})

	_, err := os.Lstat(filepath.Join(parent, "outside.bin"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRoot_OpenWriteWritesIntoAnExistingFile(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	root := openRoot(t, b, remotePath(dir))
	create(t, root, "Sub/a.bin", "0123456789")

	w, err := root.OpenWrite(t.Context(), "Sub/a.bin", 4)
	require.NoError(t, err)
	_, err = w.Write([]byte("xy"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	got, err := os.ReadFile(filepath.Join(dir, "Sub", "a.bin"))
	require.NoError(t, err)
	assert.Equal(t, "0123xy6789", string(got))
}

func TestRoot_OpenWriteNeverCreatesAFile(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	root := openRoot(t, b, remotePath(dir))

	_, err := root.OpenWrite(t.Context(), "a.bin", 0)

	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = os.Lstat(filepath.Join(dir, "a.bin"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRoot_LinksAndRemoves(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	root := openRoot(t, b, remotePath(dir))
	create(t, root, "Show/a.bin", "x")

	require.NoError(t, root.Link(t.Context(), "Show/a.bin", "Show/a.bin.link"))
	require.ErrorIs(t, root.Link(t.Context(), "Show/a.bin", "Show/a.bin.link"), fs.ErrExist)
	got, err := os.ReadFile(filepath.Join(dir, "Show", "a.bin.link"))
	require.NoError(t, err)
	assert.Equal(t, "x", string(got))

	for _, name := range []string{"Show/a.bin.link", "Show/a.bin", "Show"} {
		require.NoError(t, root.Remove(t.Context(), name), name)
	}
	require.ErrorIs(t, root.Remove(t.Context(), "Show"), fs.ErrNotExist)
	_, err = os.Lstat(filepath.Join(dir, "Show"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRoot_LinkMakesTheFoldersOfTheNewName(t *testing.T) {
	t.Parallel()

	b, _ := newBackend(t)
	dir := t.TempDir()
	root := openRoot(t, b, remotePath(dir))
	create(t, root, "Show/a.bin", "x")

	require.NoError(t, root.Link(t.Context(), "Show/a.bin", "links/Show/a.bin"))

	got, err := os.ReadFile(filepath.Join(dir, "links", "Show", "a.bin"))
	require.NoError(t, err)
	assert.Equal(t, "x", string(got))
}
