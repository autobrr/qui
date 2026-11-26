// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

const statxWant = unix.STATX_TYPE | unix.STATX_MODE | unix.STATX_NLINK | unix.STATX_INO | unix.STATX_SIZE | unix.STATX_MTIME

var errStatxFields = errors.New("statx returned no inode or link count")

func lstatPath(path string) (*fsops.LstatInfo, error) {
	return statxAt(unix.AT_FDCWD, path, path, false, func() (os.FileInfo, error) { return os.Lstat(path) })
}

func statPath(path string) (*fsops.LstatInfo, error) {
	return statxAt(unix.AT_FDCWD, path, path, true, func() (os.FileInfo, error) { return os.Stat(path) })
}

func lstatEntry(path string, d fs.DirEntry, wantID bool) (*fsops.LstatInfo, error) {
	if wantID && d.Type().IsRegular() {
		return statxAt(unix.AT_FDCWD, path, path, false, d.Info)
	}
	fi, err := d.Info()
	if err != nil {
		return nil, err
	}
	return statFields(fi, path, false), nil
}

func lstatIn(r *os.Root, name string) (*fsops.LstatInfo, error) {
	native := filepath.FromSlash(name)
	f, err := r.OpenFile(native, unix.O_PATH, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	conn, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}
	var stx unix.Statx_t
	if ctlErr := conn.Control(func(fd uintptr) {
		err = unix.Statx(int(fd), "", statxFlags(false)|unix.AT_EMPTY_PATH, statxWant, &stx)
	}); ctlErr != nil {
		return nil, ctlErr
	}
	return statxInfo(&stx, err, name, false, func() (os.FileInfo, error) { return r.Lstat(native) })
}

func statxAt(dirfd int, name, path string, follow bool, fallback func() (os.FileInfo, error)) (*fsops.LstatInfo, error) {
	var stx unix.Statx_t
	err := unix.Statx(dirfd, name, statxFlags(follow), statxWant, &stx)
	return statxInfo(&stx, err, path, follow, fallback)
}

func statxFlags(follow bool) int {
	flags := unix.AT_STATX_FORCE_SYNC | unix.AT_NO_AUTOMOUNT
	if !follow {
		flags |= unix.AT_SYMLINK_NOFOLLOW
	}
	return flags
}

func statxInfo(stx *unix.Statx_t, err error, path string, follow bool, fallback func() (os.FileInfo, error)) (*fsops.LstatInfo, error) {
	if err == nil && stx.Mask&statxWant != statxWant {
		err = errStatxFields
	}
	if err != nil {
		fi, statErr := fallback()
		if statErr != nil {
			return nil, statErr
		}
		info := statFields(fi, path, follow)
		switch {
		case !hasIdentity(info):
		case errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM):
			setStatIdentity(info, fi)
		default:
			info.FileIDErr = fmt.Errorf("statx %s: %w", path, err)
		}
		return info, nil
	}
	mode := statxMode(stx.Mode)
	info := &fsops.LstatInfo{
		Path:      path,
		Size:      int64(stx.Size),
		ModTime:   time.Unix(stx.Mtime.Sec, int64(stx.Mtime.Nsec)),
		IsDir:     mode.IsDir(),
		IsSymlink: mode&fs.ModeSymlink != 0,
		Mode:      mode,
	}
	if hasIdentity(info) {
		info.FileID = hardlink.UnixFileID(unix.Mkdev(stx.Dev_major, stx.Dev_minor), stx.Ino)
		info.Nlinks = uint64(stx.Nlink)
	}
	return info, nil
}

func statxMode(m uint16) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & unix.S_IFMT {
	case unix.S_IFBLK:
		mode |= fs.ModeDevice
	case unix.S_IFCHR:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case unix.S_IFDIR:
		mode |= fs.ModeDir
	case unix.S_IFIFO:
		mode |= fs.ModeNamedPipe
	case unix.S_IFLNK:
		mode |= fs.ModeSymlink
	case unix.S_IFSOCK:
		mode |= fs.ModeSocket
	}
	if m&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if m&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if m&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}
