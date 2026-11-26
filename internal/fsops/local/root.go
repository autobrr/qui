// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/internal/fsops"
)

const (
	rootDirMode  fs.FileMode = 0o775
	rootFileMode fs.FileMode = 0o664
)

func (b *Backend) OpenRoot(ctx context.Context, dir string) (fsops.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := mkdirs(dir); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &root{r}, nil
}

func mkdirs(dir string) error {
	dir = filepath.Clean(dir)
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := mkdirs(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(dir, rootDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	_ = os.Chmod(dir, rootDirMode)
	return nil
}

type root struct{ r *os.Root }

func (r *root) Lstat(ctx context.Context, name string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := r.r.Lstat(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	return osFileInfoToLstat(fi, name, false), nil
}

func (r *root) Create(ctx context.Context, name string) (fsops.FileWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dir := path.Dir(name); dir != "." {
		if err := r.mkdirs(dir); err != nil {
			return nil, err
		}
	}
	native := filepath.FromSlash(name)
	f, err := r.r.OpenFile(native, os.O_WRONLY|os.O_CREATE|os.O_EXCL, rootFileMode)
	if err != nil {
		return nil, err
	}
	_ = r.r.Chmod(native, rootFileMode)
	return writer{f}, nil
}

func (r *root) OpenWrite(ctx context.Context, name string, offset int64) (fsops.FileWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := r.r.OpenFile(filepath.FromSlash(name), os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return writer{f}, nil
}

func (r *root) mkdirs(dir string) error {
	parts := strings.Split(dir, "/")
	for i := range parts {
		native := filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		err := r.r.Mkdir(native, rootDirMode)
		switch {
		case err == nil:
			_ = r.r.Chmod(native, rootDirMode)
		case !errors.Is(err, fs.ErrExist):
			return err
		}
	}
	return nil
}

func (r *root) RenameNoReplace(ctx context.Context, oldName, newName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	oldNative, newNative := filepath.FromSlash(oldName), filepath.FromSlash(newName)
	err := r.r.Link(oldNative, newNative)
	switch {
	case err == nil:
		return r.r.Remove(oldNative)
	case errors.Is(err, fs.ErrExist):
		return err
	}
	if _, err := r.r.Lstat(newNative); err == nil {
		return fmt.Errorf("rename %s: %w", newName, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return r.r.Rename(oldNative, newNative)
}

func (r *root) Link(ctx context.Context, oldName, newName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dir := path.Dir(newName); dir != "." {
		if err := r.mkdirs(dir); err != nil {
			return err
		}
	}
	return r.r.Link(filepath.FromSlash(oldName), filepath.FromSlash(newName))
}

func (r *root) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.r.Remove(filepath.FromSlash(name))
}

func (r *root) Close() error { return r.r.Close() }
