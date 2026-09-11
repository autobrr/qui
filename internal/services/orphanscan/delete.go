// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/autobrr/qui/internal/fsops"
)

// ErrInUse indicates a deletion target contains files currently in use by torrents.
var ErrInUse = errors.New("contains in-use torrent file")

type deleteDisposition int

const (
	deleteDispositionDeleted deleteDisposition = iota
	deleteDispositionSkippedInUse
	deleteDispositionSkippedMissing
	deleteDispositionSkippedIgnored
	deleteDispositionSkippedNotEmpty
	deleteDispositionSkippedNotDirectory
)

// withinScanRoot checks that target is an absolute path strictly below scanRoot.
// Comparison is case-folded, because the scan root and the target can carry
// different casing of the same directory on a case-insensitive filesystem.
//
// The fold cannot let a delete escape the scan: target is always a path that a
// walk of one of the run's scan roots produced, so a match that needs the fold
// means the file sits under a different spelling of a root that was walked, not
// under a directory nobody scanned. Only the fence is spelled differently; the
// path handed to the backend's Remove is target itself.
func withinScanRoot(scanRoot, target string) error {
	if !filepath.IsAbs(target) {
		return fmt.Errorf("refusing non-absolute path: %s", target)
	}

	normRoot := normalizePath(scanRoot)
	normTarget := normalizePath(target)

	if normTarget == normRoot {
		return fmt.Errorf("refusing to delete scan root: %s", scanRoot)
	}
	if !isPathUnderNormalized(normTarget, normRoot) {
		return fmt.Errorf("path escapes scan root: %s", target)
	}
	return nil
}

// safeDeleteFile removes a single file with safety checks.
// Re-checks TorrentFileMap before deletion to handle torrents added since scan.
// Never removes directories.
func safeDeleteFile(ctx context.Context, scanRoot, target string, tfm *TorrentFileMap, backend fsops.Backend) (deleteDisposition, error) {
	if err := withinScanRoot(scanRoot, target); err != nil {
		return 0, err
	}

	// Re-check: torrent may have been added since scan (skip)
	if tfm.Has(normalizePath(target)) {
		return deleteDispositionSkippedInUse, nil
	}

	// Verify it's actually a file (not a directory)
	info, err := backend.Lstat(ctx, target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, err
	}
	if info.IsDir {
		return 0, fmt.Errorf("refusing to delete directory as file: %s", target)
	}

	if err := backend.Remove(ctx, target, fsops.RemoveOptions{}); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, err
	}
	return deleteDispositionDeleted, nil
}

// checkDirContainsInUseFile walks a directory and returns ErrInUse if any file is in the TorrentFileMap.
func checkDirContainsInUseFile(ctx context.Context, target string, tfm *TorrentFileMap, backend fsops.Backend) error {
	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := backend.WalkDir(walkCtx, target, fsops.WalkOptions{EmitStatErrors: true})
	if err != nil {
		cancelWalk()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("walk directory: %w", err)
	}
	defer func() {
		cancelWalk()
		for range ch { //nolint:revive // drain channel to release the walk goroutine
		}
	}()
	for entry := range ch {
		if entry.Err != nil {
			if errors.Is(entry.Err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("walk entry: %w", entry.Err)
		}
		if entry.StatErr != nil {
			// The path was enumerated but its metadata is unreadable, so it
			// cannot be verified against the file map. Fail closed: an in-use
			// match still counts, a vanished entry is fine, anything else
			// aborts the deletion rather than risk removing torrent-owned data.
			if err := checkFileInUse(entry.Path, tfm); err != nil {
				return err
			}
			if errors.Is(entry.StatErr, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat %s: %w", entry.Path, entry.StatErr)
		}
		if entry.IsDir {
			continue
		}
		if err := checkFileInUse(entry.Path, tfm); err != nil {
			return err
		}
	}
	return nil
}

func checkFileInUse(path string, tfm *TorrentFileMap) error {
	if tfm.Has(normalizePath(path)) {
		return fmt.Errorf("%w: %s", ErrInUse, path)
	}
	return nil
}

// safeDeleteTarget removes a file OR directory with safety checks.
// For directories, it deletes recursively, but first verifies that no file within
// the directory is currently referenced by TorrentFileMap or protected by ignorePaths.
// Symlinks are never followed.
func safeDeleteTarget(ctx context.Context, scanRoot, target string, tfm *TorrentFileMap, ignorePaths []string, backend fsops.Backend) (deleteDisposition, error) {
	if err := withinScanRoot(scanRoot, target); err != nil {
		return 0, err
	}
	if len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(target, ignorePaths) {
		return deleteDispositionSkippedIgnored, nil
	}

	info, err := backend.Lstat(ctx, target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, fmt.Errorf("stat target: %w", err)
	}

	if info.IsSymlink {
		return safeDeleteSymlink(ctx, target, tfm, backend)
	}
	if !info.IsDir {
		return safeDeleteFile(ctx, scanRoot, target, tfm, backend)
	}
	return safeDeleteDirectory(ctx, target, tfm, backend)
}

func safeDeleteSymlink(ctx context.Context, target string, tfm *TorrentFileMap, backend fsops.Backend) (deleteDisposition, error) {
	if tfm.Has(normalizePath(target)) {
		return deleteDispositionSkippedInUse, nil
	}
	if err := backend.Remove(ctx, target, fsops.RemoveOptions{}); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, fmt.Errorf("remove symlink: %w", err)
	}
	return deleteDispositionDeleted, nil
}

func safeDeleteDirectory(ctx context.Context, target string, tfm *TorrentFileMap, backend fsops.Backend) (deleteDisposition, error) {
	if err := checkDirContainsInUseFile(ctx, target, tfm, backend); err != nil {
		if errors.Is(err, ErrInUse) {
			return deleteDispositionSkippedInUse, nil
		}
		return 0, fmt.Errorf("check directory contents: %w", err)
	}

	if err := backend.Remove(ctx, target, fsops.RemoveOptions{Recursive: true}); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return deleteDispositionSkippedMissing, nil
		}
		return 0, fmt.Errorf("remove directory: %w", err)
	}
	return deleteDispositionDeleted, nil
}

// safeDeleteEmptyDir removes a previewed directory. A directory that is not
// empty now, or is no longer a directory, is reported as skipped rather than
// failed: a file inside it that the run kept is the usual reason, and nothing
// went wrong. Remove still refuses a non-empty directory, so the check before it
// only decides how the outcome is reported.
func safeDeleteEmptyDir(ctx context.Context, scanRoot, target string, backend fsops.Backend) (deleteDisposition, error) {
	if err := withinScanRoot(scanRoot, target); err != nil {
		return 0, err
	}

	info, err := backend.Lstat(ctx, target)
	if errors.Is(err, fs.ErrNotExist) {
		return deleteDispositionSkippedMissing, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir {
		return deleteDispositionSkippedNotDirectory, nil
	}

	entries, err := backend.ReadDir(ctx, target)
	if errors.Is(err, fs.ErrNotExist) {
		return deleteDispositionSkippedMissing, nil
	}
	if err != nil {
		return 0, err
	}
	if len(entries) > 0 {
		return deleteDispositionSkippedNotEmpty, nil
	}

	err = backend.Remove(ctx, target, fsops.RemoveOptions{})
	if errors.Is(err, fs.ErrNotExist) {
		return deleteDispositionSkippedMissing, nil
	}
	if err != nil {
		return 0, err
	}
	return deleteDispositionDeleted, nil
}
