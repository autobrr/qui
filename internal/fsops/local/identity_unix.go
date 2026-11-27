// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !linux && !windows

package local

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/autobrr/qui/internal/fsops"
)

func lstatPath(path string) (*fsops.LstatInfo, error) {
	fi, err := os.Lstat(path)
	return identify(fi, err, path, false)
}

func statPath(path string) (*fsops.LstatInfo, error) {
	fi, err := os.Stat(path)
	return identify(fi, err, path, true)
}

func lstatIn(r *os.Root, name string) (*fsops.LstatInfo, error) {
	fi, err := r.Lstat(filepath.FromSlash(name))
	return identify(fi, err, name, false)
}

func lstatEntry(path string, d fs.DirEntry, wantID bool) (*fsops.LstatInfo, error) {
	fi, err := d.Info()
	if err != nil {
		return nil, err
	}
	info := statFields(fi, path, false)
	if wantID && info.Mode.IsRegular() {
		setStatIdentity(info, fi)
	}
	return info, nil
}

func identify(fi os.FileInfo, err error, path string, follow bool) (*fsops.LstatInfo, error) {
	if err != nil {
		return nil, err
	}
	info := statFields(fi, path, follow)
	if hasIdentity(info) {
		setStatIdentity(info, fi)
	}
	return info, nil
}
