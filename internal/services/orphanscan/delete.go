// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// ErrInUse indicates a deletion target contains files currently in use by torrents.
var ErrInUse = errors.New("contains in-use torrent file")

// errInUseThroughAlias is ErrInUse for a file a torrent reaches through another path.
var errInUseThroughAlias = fmt.Errorf("%w through another path", ErrInUse)

type deleteDisposition int

const (
	deleteDispositionDeleted deleteDisposition = iota
	deleteDispositionSkippedInUse
	deleteDispositionSkippedInUseThroughAlias
	deleteDispositionSkippedFolderInUseThroughAlias
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
	if !info.Mode.IsRegular() {
		return 0, fmt.Errorf("refusing to delete non-regular file: %s", target)
	}
	held, err := protected.inUse.holds(info)
	if err != nil {
		return 0, err
	}
	if held {
		return deleteDispositionSkippedInUseThroughAlias, nil
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
	ch, err := backend.WalkDir(walkCtx, target, fsops.WalkOptions{EmitStatErrors: true, WantFileID: protected.inUse != nil})
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
		if err := checkEntryInUse(ctx, &entry, protected, backend); err != nil {
			return err
		}
	}
	return nil
}

// checkEntryInUse returns ErrInUse if a torrent owns the walked non-directory
// entry, by path or by identity.
func checkEntryInUse(ctx context.Context, entry *fsops.WalkEntry, protected *protectedPaths, backend fsops.Backend) error {
	if err := checkFileInUse(entry.Path, protected); err != nil {
		return err
	}
	info := &entry.LstatInfo
	// The removal takes a symlink with the folder, so a torrent that reaches
	// a file through the link would lose its path to it. collectInUseIDs
	// followed the link, so compare where it points.
	if entry.IsSymlink && protected.inUse != nil {
		var err error
		if info, err = followSymlink(ctx, entry.Path, backend); err != nil || info == nil {
			return err
		}
	}
	held, err := protected.inUse.holds(info)
	if err != nil {
		return fmt.Errorf("cannot read the identity of the files in this folder: %w", err)
	}
	if held {
		return fmt.Errorf("%w: %s", errInUseThroughAlias, entry.Path)
	}
	return nil
}

// followSymlink returns what the symlink at p points to. A link that leads
// nowhere, dangling or in a loop, returns nil, since no torrent can use a file
// through it.
// Any other failure is returned, as for a regular entry whose identity cannot
// be read, because the target could be a torrent's file.
func followSymlink(ctx context.Context, p string, backend fsops.Backend) (*fsops.LstatInfo, error) {
	info, err := backend.Stat(ctx, p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) {
			return nil, nil
		}
		return nil, fmt.Errorf("follow symlink %s: %w", p, err)
	}
	return info, nil
}

func checkFileInUse(path string, protected *protectedPaths) error {
	if protected.torrentFile(path) {
		return fmt.Errorf("%w: %s", ErrInUse, path)
	}
	return nil
}

// inUseIDs holds the identity of every torrent file on disk at delete time.
// The file map protects by path, so without these a torrent that reaches a
// listed file through a second path since the scan, such as a bind mount, a
// symlinked folder or a hardlink, would lose it. The scan keeps such a file by
// identity, and the delete keeps it the same way.
type inUseIDs struct {
	instance *models.Instance
	keys     map[fsops.FileKey]struct{}
}

// collectInUseIDs reads the identity of each torrent path through backend,
// following links, as qBittorrent does. A path that is not on disk protects
// nothing. A path qui may not read is skipped, as the scan skips a denied
// subtree. Any other failure is returned, because a file the run cannot
// identify could be one of the targets.
func collectInUseIDs(ctx context.Context, backend fsops.Backend, instance *models.Instance, paths []string) (*inUseIDs, error) {
	// Cross-seeds share files, so the same path comes once per torrent.
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			unique = append(unique, p)
		}
	}
	ids := &inUseIDs{instance: instance, keys: make(map[fsops.FileKey]struct{}, len(unique))}
	infos, errs := fsops.StatAll(ctx, backend, unique)
	denied, firstDenied := 0, ""
	for i := range unique {
		if err := errs[i]; err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				continue
			}
			// A backend that wraps both errors lost the link, which says
			// nothing about the file's permissions.
			if errors.Is(err, fs.ErrPermission) && !errors.Is(err, fsops.ErrConnectionLost) {
				if denied == 0 {
					firstDenied = unique[i]
				}
				denied++
				continue
			}
			return nil, err
		}
		info := infos[i]
		if !info.Mode.IsRegular() {
			continue
		}
		key, err := fileKey(info, instance)
		if err != nil {
			return nil, err
		}
		ids.keys[key] = struct{}{}
	}
	if denied > 0 {
		log.Warn().Int("count", denied).Str("first", firstDenied).Msg("orphanscan: skipping torrent files qui may not read")
	}
	return ids, nil
}

// holds reports whether a torrent file shares the identity of info, a regular
// file read with Lstat. Like the scan (#3487), a target with more than one
// link is kept when one of them is a torrent's.
func (ids *inUseIDs) holds(info *fsops.LstatInfo) (bool, error) {
	if ids == nil || !info.Mode.IsRegular() {
		return false, nil
	}
	key, err := fileKey(info, ids.instance)
	if err != nil {
		return false, err
	}
	_, ok := ids.keys[key]
	return ok, nil
}

func fileKey(info *fsops.LstatInfo, instance *models.Instance) (fsops.FileKey, error) {
	if info.FileIDErr != nil {
		return fsops.FileKey{}, fmt.Errorf("file identity %s: %w", info.Path, info.FileIDErr)
	}
	key, ok := fsops.FileKeyOf(info.FileID, instance)
	if !ok {
		return fsops.FileKey{}, fmt.Errorf("file identity %s: the filesystem reported none", info.Path)
	}
	return key, nil
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
	if protected.torrentFile(target) || protected.torrentFileIn(target) {
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
		if errors.Is(err, errInUseThroughAlias) {
			return deleteDispositionSkippedFolderInUseThroughAlias, nil
		}
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
