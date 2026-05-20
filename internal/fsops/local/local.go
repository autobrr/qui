// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package local implements fsops.Backend by delegating to the local filesystem
// via os.*, pkg/hardlinktree, pkg/reflinktree, pkg/hardlink, and pkg/fsutil.
// This is the default backend used when qui and qBittorrent share a host.
package local

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// Backend implements fsops.Backend using the local filesystem.
type Backend struct{}

// NewBackend returns a local filesystem backend.
func NewBackend() *Backend { return &Backend{} }

// compile-time check
var _ fsops.Backend = (*Backend)(nil)

func (b *Backend) Stat(ctx context.Context, path string) (*fsops.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return osFileInfoToFsops(fi, path), nil
}

func (b *Backend) StatBatch(ctx context.Context, paths []string) ([]*fsops.FileInfo, []error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	infos := make([]*fsops.FileInfo, len(paths))
	errs := make([]error, len(paths))
	for i, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		fi, err := os.Stat(p)
		if err != nil {
			errs[i] = err
			continue
		}
		infos[i] = osFileInfoToFsops(fi, p)
	}
	return infos, errs, nil
}

func (b *Backend) Lstat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	return osFileInfoToLstat(fi, path)
}

func (b *Backend) LstatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	infos := make([]*fsops.LstatInfo, len(paths))
	errs := make([]error, len(paths))
	for i, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			errs[i] = err
			continue
		}
		info, err := osFileInfoToLstat(fi, p)
		if err != nil {
			errs[i] = err
			continue
		}
		infos[i] = info
	}
	return infos, errs, nil
}

func (b *Backend) ReadDir(ctx context.Context, path string, maxEntries int) ([]fsops.DirEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}

	truncated := false
	if maxEntries > 0 && len(entries) > maxEntries {
		entries = entries[:maxEntries]
		truncated = true
	}

	result := make([]fsops.DirEntry, len(entries))
	for i, e := range entries {
		info, _ := e.Info()
		de := fsops.DirEntry{
			Name:      e.Name(),
			IsDir:     e.IsDir(),
			IsSymlink: e.Type()&os.ModeSymlink != 0,
			Mode:      e.Type().Perm(),
		}
		if info != nil {
			de.Mode = info.Mode().Perm()
		}
		result[i] = de
	}
	return result, truncated, nil
}

func (b *Backend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Verify root exists before starting the goroutine.
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	ch := make(chan fsops.WalkEntry, 64)
	go func() {
		defer close(ch)
		count := 0
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return fs.SkipAll
			}

			if opts.MaxEntries > 0 && count >= opts.MaxEntries {
				return fs.SkipAll
			}

			// d is nil when filepath.WalkDir cannot stat the root (TOCTOU
			// between our pre-check and the walk's internal stat).
			if d == nil {
				return walkErr
			}

			// Skip hidden files/dirs if requested.
			name := d.Name()
			if opts.SkipHidden && len(name) > 0 && name[0] == '.' && path != root {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip ignored directory names.
			if d.IsDir() && path != root {
				if slices.Contains(opts.IgnoreDirNames, name) {
					return filepath.SkipDir
				}
			}

			// Skip ignored paths.
			for _, ignored := range opts.IgnorePaths {
				if path == ignored {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			entry := fsops.WalkEntry{}
			entry.Path = path

			rel, err := filepath.Rel(root, path)
			if err == nil {
				entry.RelPath = rel
			}

			if walkErr != nil {
				entry.Err = walkErr
			} else {
				fi, err := d.Info()
				if err != nil {
					entry.Err = err
				} else {
					entry.Size = fi.Size()
					entry.ModTime = fi.ModTime()
					entry.Mode = fi.Mode()
					entry.IsDir = fi.IsDir()
					entry.IsSymlink = fi.Mode()&os.ModeSymlink != 0

					if (opts.WantFileID || opts.WantNlinks) && fi.Mode().IsRegular() {
						fid, nlinks, fidErr := hardlink.GetFileID(fi, path)
						if fidErr == nil {
							entry.FileID = fid
							entry.Nlinks = nlinks
						}
					}
				}
			}

			select {
			case ch <- entry:
				count++
			case <-ctx.Done():
				return fs.SkipAll
			}
			return nil
		})
	}()
	return ch, nil
}

func (b *Backend) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return fsutil.SameFilesystem(p1, p2)
}

func (b *Backend) FileID(ctx context.Context, path string) (hardlink.FileID, uint64, error) {
	if err := ctx.Err(); err != nil {
		return hardlink.FileID{}, 0, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return hardlink.FileID{}, 0, err
	}
	return hardlink.GetFileID(fi, path)
}

func (b *Backend) MkdirAll(ctx context.Context, path string, perm fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}

func (b *Backend) Remove(ctx context.Context, path string, opts fsops.RemoveOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.Recursive {
		if len(opts.IgnorePaths) > 0 {
			return errors.New("recursive remove with IgnorePaths is not supported by the local backend")
		}
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (b *Backend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := hardlinktree.Create(plan)
	if err != nil {
		return &fsops.TreeCreateResult{RolledBack: true}, err
	}
	return &fsops.TreeCreateResult{Created: len(plan.Files)}, nil
}

func (b *Backend) ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := reflinktree.Create(plan)
	if err != nil {
		return &fsops.TreeCreateResult{RolledBack: true}, err
	}
	return &fsops.TreeCreateResult{Created: len(plan.Files)}, nil
}

func (b *Backend) RemoveTree(ctx context.Context, plan *hardlinktree.TreePlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return hardlinktree.Rollback(plan)
}

func (b *Backend) SupportsReflink(ctx context.Context, path string) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	supported, reason := reflinktree.SupportsReflink(path)
	return supported, reason, nil
}

func (b *Backend) Info(_ context.Context) (*fsops.BackendInfo, error) {
	return &fsops.BackendInfo{Kind: "local"}, nil
}

func (b *Backend) HealthCheck(_ context.Context) error {
	return nil
}

// osFileInfoToFsops converts an os.FileInfo to an fsops.FileInfo.
func osFileInfoToFsops(fi os.FileInfo, path string) *fsops.FileInfo {
	return &fsops.FileInfo{
		Path:      path,
		Size:      fi.Size(),
		ModTime:   fi.ModTime(),
		IsDir:     fi.IsDir(),
		IsSymlink: fi.Mode()&os.ModeSymlink != 0,
		Mode:      fi.Mode(),
	}
}

// osFileInfoToLstat converts an os.FileInfo from Lstat to an fsops.LstatInfo.
func osFileInfoToLstat(fi os.FileInfo, path string) (*fsops.LstatInfo, error) {
	info := &fsops.LstatInfo{
		FileInfo: *osFileInfoToFsops(fi, path),
	}
	if fi.Mode().IsRegular() {
		fid, nlinks, err := hardlink.GetFileID(fi, path)
		if err != nil {
			return nil, err
		}
		info.FileID = fid
		info.Nlinks = nlinks
	}
	return info, nil
}
