// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

func statTID(t *testing.T, path string) hardlink.FileID {
	t.Helper()
	fi, err := os.Lstat(path)
	require.NoError(t, err)
	st, ok := fi.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return hardlink.UnixFileID(st.Dev, st.Ino)
}

func TestIdentity_HardlinkedFileReadsTwoLinksOnEveryRead(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "Show.S01E01.1080p.WEB-DL-GRP.mkv")
	link := filepath.Join(dir, "library", "Show.S01E01.mkv")
	writeFile(t, original, "episode")
	require.NoError(t, os.Mkdir(filepath.Dir(link), 0o750))
	require.NoError(t, os.Link(original, link))
	want := statTID(t, original)

	b := NewBackend()
	reads := map[string]func(string) (*fsops.LstatInfo, error){
		"lstat": func(p string) (*fsops.LstatInfo, error) { return b.Lstat(t.Context(), p) },
		"stat":  func(p string) (*fsops.LstatInfo, error) { return b.Stat(t.Context(), p) },
		"root": func(p string) (*fsops.LstatInfo, error) {
			root, err := b.OpenRoot(t.Context(), dir)
			require.NoError(t, err)
			defer func() { require.NoError(t, root.Close()) }()
			rel, err := filepath.Rel(dir, p)
			require.NoError(t, err)
			return root.Lstat(t.Context(), filepath.ToSlash(rel))
		},
		"walk": func(p string) (*fsops.LstatInfo, error) {
			ch, err := b.WalkDir(t.Context(), dir, fsops.WalkOptions{WantFileID: true})
			require.NoError(t, err)
			var found *fsops.LstatInfo
			for e := range ch {
				if e.Path == p {
					found = &e.LstatInfo
				}
			}
			require.NotNil(t, found)
			return found, nil
		},
	}
	for name, read := range reads {
		for _, p := range []string{original, link} {
			info, err := read(p)
			require.NoError(t, err, name)
			require.NoError(t, info.FileIDErr, name)
			require.Equal(t, want, info.FileID, "%s %s", name, p)
			require.Equal(t, uint64(2), info.Nlinks, "%s %s", name, p)
			require.Equal(t, int64(len("episode")), info.Size, name)
		}
	}
}

func TestIdentity_StatxErrorLeavesNoIdentity(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Movie.2019.1080p.BluRay-GRP.mkv")
	writeFile(t, file, "movie")
	require.NoError(t, os.Link(file, filepath.Join(dir, "linked.mkv")))
	symlink := filepath.Join(dir, "symlink.mkv")
	require.NoError(t, os.Symlink(file, symlink))
	lstat := func(p string) func() (os.FileInfo, error) {
		return func() (os.FileInfo, error) { return os.Lstat(p) }
	}

	missingNlink := unix.Statx_t{Mask: statxWant &^ unix.STATX_NLINK, Nlink: 2, Ino: 7, Mode: unix.S_IFREG | 0o644}
	tests := []struct {
		name   string
		stx    unix.Statx_t
		err    error
		wantIs error
	}{
		{name: "statx fails", err: unix.EIO, wantIs: unix.EIO},
		{name: "statx leaves out the link count", stx: missingNlink, wantIs: errStatxFields},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := statxInfo(&tt.stx, tt.err, file, false, lstat(file))
			require.NoError(t, err)
			require.ErrorIs(t, info.FileIDErr, tt.wantIs)
			require.True(t, info.FileID.IsZero())
			require.Zero(t, info.Nlinks)
			require.True(t, info.Mode.IsRegular())
			require.Equal(t, int64(len("movie")), info.Size)
		})
	}

	for _, unavailable := range []error{unix.ENOSYS, unix.EPERM} {
		info, err := statxInfo(&unix.Statx_t{}, unavailable, file, false, lstat(file))
		require.NoError(t, err)
		require.NoError(t, info.FileIDErr, "a kernel or sandbox without statx reads the stat count")
		require.Equal(t, statTID(t, file), info.FileID)
		require.Equal(t, uint64(2), info.Nlinks)
	}

	info, err := statxInfo(&unix.Statx_t{}, unix.EIO, symlink, false, lstat(symlink))
	require.NoError(t, err)
	require.True(t, info.IsSymlink)
	require.NoError(t, info.FileIDErr)

	_, err = statxInfo(&unix.Statx_t{}, unix.EIO, filepath.Join(dir, "gone.mkv"), false, lstat(filepath.Join(dir, "gone.mkv")))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestIdentity_StatFieldsMatchOsLstat(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{
		"file": filepath.Join(dir, "file.mkv"),
		"dir":  filepath.Join(dir, "dir"),
		"link": filepath.Join(dir, "link"),
		"fifo": filepath.Join(dir, "fifo"),
		"sock": filepath.Join(dir, "sock"),
		"suid": filepath.Join(dir, "suid"),
	}
	writeFile(t, paths["file"], "data")
	require.NoError(t, os.Mkdir(paths["dir"], 0o750))
	require.NoError(t, os.Chmod(paths["dir"], 0o750|fs.ModeSticky|fs.ModeSetgid))
	require.NoError(t, os.Symlink(paths["file"], paths["link"]))
	require.NoError(t, unix.Mkfifo(paths["fifo"], 0o600))
	listener, err := net.Listen("unix", paths["sock"])
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	writeFile(t, paths["suid"], "x")
	require.NoError(t, os.Chmod(paths["suid"], 0o700|fs.ModeSetuid))

	for name, p := range paths {
		fi, err := os.Lstat(p)
		require.NoError(t, err)
		info, err := lstatPath(p)
		require.NoError(t, err)
		require.Equal(t, fi.Mode(), info.Mode, name)
		require.Equal(t, fi.Size(), info.Size, name)
		require.True(t, fi.ModTime().Equal(info.ModTime), name)
		require.Equal(t, fi.IsDir(), info.IsDir, name)
		require.Equal(t, fi.Mode()&fs.ModeSymlink != 0, info.IsSymlink, name)
		require.Equal(t, fi.Mode().IsRegular() || fi.IsDir(), !info.FileID.IsZero(), name)
	}

	followed, err := statPath(paths["link"])
	require.NoError(t, err)
	require.True(t, followed.Mode.IsRegular())
	require.Equal(t, statTID(t, paths["file"]), followed.FileID)

	_, err = lstatPath(filepath.Join(dir, "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRootLstat_FifoInThePathFailsWithoutOpeningIt(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))
	root := openRoot(t, dir)

	done := make(chan error, 1)
	go func() {
		_, err := root.Lstat(t.Context(), "fifo/Movie.2019.1080p.BluRay-GRP.mkv")
		done <- err
	}()
	for {
		select {
		case err := <-done:
			require.ErrorIs(t, err, syscall.ENOTDIR)
			info, err := root.Lstat(t.Context(), "fifo")
			require.NoError(t, err)
			require.Equal(t, fs.ModeNamedPipe, info.Mode.Type())
			return
		default:
		}
		if fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0); err == nil {
			require.NoError(t, unix.Close(fd))
			t.Fatal("Lstat opened the FIFO for reading, which blocks until a writer opens it")
		}
	}
}

func TestRootLstat_ResolvesNamesInsideTheRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Movie.2019.1080p.BluRay-GRP.mkv"), "top")
	deep := filepath.Join(dir, "deep", "Movie.2019.1080p.BluRay-GRP.mkv")
	writeFile(t, deep, "deep")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "deep", "inner"), 0o750))
	require.NoError(t, os.Symlink(filepath.Join("deep", "inner"), filepath.Join(dir, "link")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "deep", "deep"), 0o750))
	root := openRoot(t, dir)

	info, err := root.Lstat(t.Context(), "deep/")
	require.NoError(t, err)
	require.Equal(t, statTID(t, filepath.Join(dir, "deep")), info.FileID, "a trailing slash names the folder itself")

	info, err = root.Lstat(t.Context(), "link/../Movie.2019.1080p.BluRay-GRP.mkv")
	require.NoError(t, err)
	require.Equal(t, statTID(t, deep), info.FileID)

	info, err = root.Lstat(t.Context(), "link")
	require.NoError(t, err)
	require.True(t, info.IsSymlink)
	require.True(t, info.FileID.IsZero())

	_, err = root.Lstat(t.Context(), "..")
	require.Error(t, err, "a name above the root stays outside it")
}
