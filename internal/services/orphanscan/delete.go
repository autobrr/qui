// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

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
func withinScanRoot(d fsops.PathDialect, scanRoot, target string) error {
	if !d.IsAbs(target) {
		return fmt.Errorf("refusing non-absolute path: %s", target)
	}

	normRoot := fsops.NormalizePath(d, scanRoot)
	normTarget := fsops.NormalizePath(d, target)

	if normTarget == normRoot {
		return fmt.Errorf("refusing to delete scan root: %s", scanRoot)
	}
	// Same boundary as findScanRoot, including a UNC share root without a trailing separator.
	if isAbsoluteFilesystemRoot(d, normRoot) || !isPathUnderNormalized(d, normTarget, normRoot) {
		return fmt.Errorf("path escapes scan root: %s", target)
	}
	return nil
}

// safeDeleteFile removes a single file with safety checks.
// Re-checks torrent ownership before deletion to handle torrents added since scan.
// Never removes directories.
func safeDeleteFile(ctx context.Context, scanRoot, target string, protected *protectedPaths, backend fsops.Backend) (deleteDisposition, error) {
	d := backend.Paths()
	if err := withinScanRoot(d, scanRoot, target); err != nil {
		return 0, err
	}

	// Re-check: torrent may have been added since scan (skip)
	if protected.torrentFile(target) {
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

// checkDirContainsInUseFile walks a directory and returns ErrInUse if a torrent owns any file in it.
func checkDirContainsInUseFile(ctx context.Context, target string, protected *protectedPaths, backend fsops.Backend) error {
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
			if err := checkFileInUse(entry.Path, protected); err != nil {
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
		if err := checkFileInUse(entry.Path, protected); err != nil {
			return err
		}
	}
	return nil
}

func checkFileInUse(path string, protected *protectedPaths) error {
	if protected.torrentFile(path) {
		return fmt.Errorf("%w: %s", ErrInUse, path)
	}
	return nil
}

// safeDeleteTarget removes a file OR directory with safety checks.
// For directories, it deletes recursively, but first verifies that no file within
// the directory is a Protected path.
// Symlinks are never followed.
func safeDeleteTarget(ctx context.Context, scanRoot, target string, protected *protectedPaths, backend fsops.Backend) (deleteDisposition, error) {
	d := backend.Paths()
	if err := withinScanRoot(d, scanRoot, target); err != nil {
		return 0, err
	}
	if protected.ignoredOrHoldsIgnored(target) {
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
		return safeDeleteSymlink(ctx, target, protected, backend)
	}
	if !info.IsDir {
		return safeDeleteFile(ctx, scanRoot, target, protected, backend)
	}
	return safeDeleteDirectory(ctx, target, protected, backend)
}

func safeDeleteSymlink(ctx context.Context, target string, protected *protectedPaths, backend fsops.Backend) (deleteDisposition, error) {
	if protected.torrentFile(target) {
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

func safeDeleteDirectory(ctx context.Context, target string, protected *protectedPaths, backend fsops.Backend) (deleteDisposition, error) {
	// The walk below sees only files on disk. A torrent that has not written a
	// file yet still owns the folder.
	if protected.torrentFileIn(target) {
		return deleteDispositionSkippedInUse, nil
	}
	if err := checkDirContainsInUseFile(ctx, target, protected, backend); err != nil {
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
	if err := withinScanRoot(backend.Paths(), scanRoot, target); err != nil {
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
