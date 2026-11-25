// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package web

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	//go:embed all:dist
	Dist embed.FS

	DistDirFS = MustSubFS(Dist, "dist")
)

type defaultFS struct {
	prefix string
	fs     fs.FS
}

func (fs defaultFS) Open(name string) (fs.File, error) {
	if fs.fs == nil {
		return os.Open(name)
	}
	return fs.fs.Open(name)
}

func MustSubFS(currentFs fs.FS, fsRoot string) fs.FS {
	subFs, err := subFS(currentFs, fsRoot)
	if err != nil {
		panic(fmt.Errorf("can not create sub FS, invalid root given, err: %w", err))
	}
	return subFs
}

func subFS(currentFs fs.FS, root string) (fs.FS, error) {
	root = filepath.ToSlash(filepath.Clean(root))
	if dFS, ok := currentFs.(*defaultFS); ok {
		if !filepath.IsAbs(root) {
			root = filepath.Join(dFS.prefix, root)
		}
		return &defaultFS{
			prefix: root,
			fs:     os.DirFS(root),
		}, nil
	}
	return fs.Sub(currentFs, root)
}
