// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package local implements fsops.Backend by delegating to the local filesystem
// via os.*, pkg/hardlinktree, pkg/reflinktree, pkg/hardlink, and pkg/fsutil.
// This is the default backend used when qui and qBittorrent share a host.
package local

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

func (b *Backend) Stat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return osFileInfoToLstat(fi, path), nil
}

func (b *Backend) Lstat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	return osFileInfoToLstat(fi, path), nil
}

func (b *Backend) ReadDir(ctx context.Context, path string) ([]fsops.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	result := make([]fsops.DirEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, fsops.DirEntry{
			Name:      e.Name(),
			IsDir:     e.IsDir(),
			IsSymlink: e.Type()&os.ModeSymlink != 0,
		})
	}
	return result, nil
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
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
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

			// Skip ignored directory names. Case-insensitive: these are OS/NAS
			// metadata dirs ($RECYCLE.BIN, @eaDir) whose on-disk case varies.
			if d.IsDir() && path != root {
				if slices.ContainsFunc(opts.IgnoreDirNames, func(ignored string) bool {
					return strings.EqualFold(ignored, name)
				}) {
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

			entry := fsops.WalkEntry{
				Path: path}

			rel, err := filepath.Rel(root, path)
			if err == nil {
				entry.RelPath = rel
			}

			if walkErr != nil {
				entry.Err = walkErr
			} else {
				fi, err := d.Info()
				if err != nil {
					// An entry that vanished or can't be stat'd is skipped, not
					// surfaced: consumers (orphanscan, dirscan) skipped such
					// files pre-migration, and emitting it as Err would read as
					// a walk failure and abort whole scans. Err stays reserved
					// for enumeration-level failures.
					return nil
				}
				entry.Size = fi.Size()
				entry.ModTime = fi.ModTime()
				entry.Mode = fi.Mode()
				entry.IsDir = fi.IsDir()
				entry.IsSymlink = fi.Mode()&os.ModeSymlink != 0

				if opts.WantFileID && fi.Mode().IsRegular() {
					fid, nlinks, fidErr := hardlink.GetFileID(fi, path)
					if fidErr != nil {
						// Identity failure must not read as an unreadable
						// entry — that would abort whole scans over one odd
						// file. Callers that need identity check FileIDErr.
						entry.FileIDErr = fidErr
					} else {
						entry.FileID = fid
						entry.Nlinks = nlinks
					}
				}
			}

			select {
			case ch <- entry:
			case <-ctx.Done():
				return fs.SkipAll
			}
			return nil
		})
		// Surface a walk abort as a final entry so the caller knows the walk
		// did not complete. Per-entry enumeration errors (unreadable
		// subdirectory, permission denied on a child) are emitted inline
		// above and do NOT abort the walk, so this fires only when the walk
		// itself returned an error — in practice the root-stat TOCTOU case,
		// where the callback propagates walkErr. Context cancellation is not
		// an error — the caller initiated it.
		if walkErr != nil && ctx.Err() == nil {
			entry := fsops.WalkEntry{Err: walkErr,
				Path: root}
			select {
			case ch <- entry:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (b *Backend) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return fsutil.SameFilesystem(p1, p2)
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
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (b *Backend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	created, err := hardlinktree.Create(plan)
	if err != nil {
		return nil, err
	}
	return treeCreateResult(created, plan), nil
}

func (b *Backend) ReflinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	created, err := reflinktree.Create(plan)
	if err != nil {
		return nil, err
	}
	return treeCreateResult(created, plan), nil
}

func (b *Backend) RemoveTree(ctx context.Context, created *fsops.TreeCreateResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if created == nil {
		return nil
	}
	handle := &hardlinktree.Created{Files: created.Files, Dirs: created.Dirs}
	return handle.Rollback()
}

func treeCreateResult(created *hardlinktree.Created, plan *hardlinktree.TreePlan) *fsops.TreeCreateResult {
	return &fsops.TreeCreateResult{
		Created:       len(created.Files),
		SkippedExists: len(plan.Files) - len(created.Files),
		Files:         created.Files,
		Dirs:          created.Dirs,
	}
}

func (b *Backend) SupportsReflink(ctx context.Context, path string) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	supported, reason := reflinktree.SupportsReflink(path)
	return supported, reason, nil
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
// Identity failure degrades to FileIDErr rather than failing the conversion:
// callers that only want size/mtime keep working, callers that need identity
// check FileIDErr.
func osFileInfoToLstat(fi os.FileInfo, path string) *fsops.LstatInfo {
	info := &fsops.LstatInfo{
		FileInfo: *osFileInfoToFsops(fi, path),
	}
	if fi.Mode().IsRegular() {
		fid, nlinks, err := hardlink.GetFileID(fi, path)
		if err != nil {
			info.FileIDErr = err
		} else {
			info.FileID = fid
			info.Nlinks = nlinks
		}
	}
	return info
}
