// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

type Backend struct{}

func NewBackend() *Backend { return &Backend{} }

var _ fsops.Backend = (*Backend)(nil)

func (b *Backend) Stat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return osFileInfoToLstat(fi, path, true), nil
}

func (b *Backend) Lstat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	return osFileInfoToLstat(fi, path, false), nil
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
		mode := e.Type()
		if mode&fs.ModeIrregular != 0 {
			mode = localFileMode(filepath.Join(path, e.Name()), mode, false)
		}
		result = append(result, fsops.DirEntry{
			Name:      e.Name(),
			IsDir:     e.IsDir(),
			IsSymlink: e.Type()&os.ModeSymlink != 0,
			Mode:      mode,
		})
	}
	return result, nil
}

func (b *Backend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	walked, err := walkTarget(root, opts.FollowRoot)
	if err != nil {
		return nil, err
	}

	ch := make(chan fsops.WalkEntry, 64)
	go func() {
		defer close(ch)
		walkErr := filepath.WalkDir(walked, func(path string, d fs.DirEntry, walkErr error) error {
			return visitWalkEntry(ctx, ch, root, walked, opts, path, d, walkErr)
		})
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

func walkTarget(root string, followRoot bool) (string, error) {
	if !followRoot {
		return root, nil
	}
	if fi, err := os.Lstat(root); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return filepath.EvalSymlinks(root)
	}
	return root, nil
}

func visitWalkEntry(ctx context.Context, ch chan<- fsops.WalkEntry, root, walked string, opts fsops.WalkOptions, path string, d fs.DirEntry, walkErr error) error {
	if ctx.Err() != nil {
		return fs.SkipAll
	}

	if d == nil {
		return walkErr
	}

	if walked != root {
		rel, err := filepath.Rel(walked, path)
		if err != nil {
			return err
		}
		path = filepath.Join(root, rel)
	}

	if opts.Skip(d.Name(), path, d.IsDir(), path == root) {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	entry, ok := newWalkEntry(root, path, d, walkErr, opts)
	if !ok {
		return nil
	}

	select {
	case ch <- entry:
	case <-ctx.Done():
		return fs.SkipAll
	}
	return nil
}

func newWalkEntry(root, path string, d fs.DirEntry, walkErr error, opts fsops.WalkOptions) (fsops.WalkEntry, bool) {
	entry := fsops.WalkEntry{
		Path: path}

	rel, err := filepath.Rel(root, path)
	if err == nil {
		entry.RelPath = rel
	}

	if walkErr != nil {
		entry.Err = walkErr
		return entry, true
	}

	fi, err := d.Info()
	if err != nil {
		if !opts.EmitStatErrors {
			return entry, false
		}
		entry.StatErr = err
		return entry, true
	}

	entry.Size = fi.Size()
	entry.ModTime = fi.ModTime()
	entry.Mode = localFileMode(path, fi.Mode(), false)
	entry.IsDir = fi.IsDir()
	entry.IsSymlink = fi.Mode()&os.ModeSymlink != 0

	if opts.WantFileID && entry.Mode.IsRegular() {
		fid, nlinks, fidErr := hardlink.GetFileID(fi, path)
		if fidErr != nil {
			entry.FileIDErr = fidErr
		} else {
			entry.FileID = fid
			entry.Nlinks = nlinks
		}
	}
	return entry, true
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

func (b *Backend) Mkdir(ctx context.Context, path string, perm fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Mkdir(path, perm)
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

func osFileInfoToFsops(fi os.FileInfo, path string, follow bool) *fsops.FileInfo {
	return &fsops.FileInfo{
		Path:      path,
		Size:      fi.Size(),
		ModTime:   fi.ModTime(),
		IsDir:     fi.IsDir(),
		IsSymlink: fi.Mode()&os.ModeSymlink != 0,
		Mode:      localFileMode(path, fi.Mode(), follow),
	}
}

func osFileInfoToLstat(fi os.FileInfo, path string, follow bool) *fsops.LstatInfo {
	info := &fsops.LstatInfo{
		FileInfo: *osFileInfoToFsops(fi, path, follow),
	}
	if info.Mode.IsRegular() || fi.IsDir() {
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

func (b *Backend) Paths() fsops.PathDialect { return fsops.HostPaths }
