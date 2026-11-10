// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
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
	deleteDispositionSkippedDirInUseThroughAlias
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
	held, err := protected.inUse.holds(ctx, backend, info)
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

// checkDirContainsInUseFile returns ErrInUse if a torrent owns a file in the
// directory, by path or by identity. It also fails when it cannot read an
// entry, its identity or a symlink's target, since that could be a torrent's file.
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
	held, err := protected.inUse.holds(ctx, backend, info)
	if err != nil {
		return fmt.Errorf("cannot read the identity of the files in this folder: %w", err)
	}
	if held {
		return fmt.Errorf("%w: %s", errInUseThroughAlias, entry.Path)
	}
	return nil
}

// followSymlink returns what the symlink at p points to. A dangling link, or a
// loop reported as ELOOP, returns nil, since no torrent can use a file through it.
// Any other failure is returned, as for a regular entry whose identity cannot
// be read, because the target could be a torrent's file. Unlike collectInUseIDs,
// that includes the code Windows reports for a link it cannot resolve.
func followSymlink(ctx context.Context, p string, backend fsops.Backend) (*fsops.LstatInfo, error) {
	info, err := backend.Stat(ctx, p)
	if err != nil {
		if pathGone(err) {
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

// inUseIDs holds the identity of every torrent file on disk at delete time, and
// of the folders along the torrents' paths when a run lists folders. The file
// map protects by path, so without these a torrent that reaches a listed file
// or folder through a second path, such as a bind mount or a symlinked folder,
// would lose it. One set holds both, which is safe only because
// holds asks about regular files: a symlink to a reached folder inside a disc
// unit must not keep the unit (#3612).
type inUseIDs struct {
	instance *models.Instance
	keys     map[fsops.FileKey]struct{}
	// linkedInUse holds the paths of torrent files with more than one link.
	linkedInUse map[fsops.FileKey][]string
	// fileDirs are the folders that hold a torrent file found on disk, as
	// reachedDirs takes them.
	fileDirs map[string]struct{}
}

// collectInUseIDs reads the identity of each torrent file or folder through
// backend, following links, as qBittorrent does. A path that is not on disk
// protects nothing, since qBittorrent cannot read it either. A path whose link
// cannot be resolved is skipped too, on the assumption that qBittorrent on the
// same host cannot read it either. A path qui may not look up is skipped, as
// the scan skips a denied subtree, unless failDenied is set. Any other failure
// is returned, a denied identity read included, because a file the run cannot
// identify could be one of the targets. Set failDenied for torrent folders: a
// denied one could be an alias that qBittorrent can follow to a listed folder.
func collectInUseIDs(ctx context.Context, backend fsops.Backend, instance *models.Instance, paths []string, failDenied bool) (*inUseIDs, error) {
	// Cross-seeds share files, so the same path comes once per torrent.
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			unique = append(unique, p)
		}
	}
	ids := &inUseIDs{
		instance:    instance,
		keys:        make(map[fsops.FileKey]struct{}, len(unique)),
		linkedInUse: make(map[fsops.FileKey][]string),
		fileDirs:    make(map[string]struct{}),
	}
	infos, errs := fsops.StatAll(ctx, backend, unique)
	var denied, unresolvable skippedPaths
	for i := range unique {
		if err := errs[i]; err != nil {
			// Before pathGone, which also matches ELOOP and would skip it unlogged.
			if isUnresolvableLinkError(err) {
				unresolvable.add(unique[i])
				continue
			}
			if pathGone(err) {
				continue
			}
			// A backend that wraps both errors lost the link, which says
			// nothing about the file's permissions.
			if !failDenied && errors.Is(err, fs.ErrPermission) && !errors.Is(err, fsops.ErrConnectionLost) {
				denied.add(unique[i])
				continue
			}
			return nil, err
		}
		info := infos[i]
		if !info.Mode.IsRegular() && !info.IsDir {
			continue
		}
		if info.Mode.IsRegular() {
			ids.fileDirs[backend.Paths().Dir(unique[i])] = struct{}{}
		}
		key, err := fileKey(info, instance)
		if err != nil {
			return nil, err
		}
		ids.keys[key] = struct{}{}
		if info.Mode.IsRegular() && info.Nlinks > 1 {
			ids.linkedInUse[key] = append(ids.linkedInUse[key], unique[i])
		}
	}
	ids.addPathsReadBeforeALink(unique, infos, errs)
	unresolvable.warn("orphanscan: skipping torrent paths whose links cannot be resolved")
	denied.warn("orphanscan: skipping torrent paths qui may not look up")
	return ids, nil
}

// addPathsReadBeforeALink adds the torrent paths that were read with one link
// to a file that a later read found with more. A hardlink made while the paths
// were read does that, and holds must compare a target with every entry.
func (ids *inUseIDs) addPathsReadBeforeALink(paths []string, infos []*fsops.LstatInfo, errs []error) {
	for i, p := range paths {
		if errs[i] != nil || !infos[i].Mode.IsRegular() || infos[i].Nlinks > 1 {
			continue
		}
		key, _ := fileKey(infos[i], ids.instance)
		if linked, ok := ids.linkedInUse[key]; ok {
			ids.linkedInUse[key] = append(linked, p)
		}
	}
}

// skippedPaths counts the paths one collectInUseIDs call skips for one reason.
type skippedPaths struct {
	count int
	first string
}

func (s *skippedPaths) add(p string) {
	if s.count == 0 {
		s.first = p
	}
	s.count++
}

func (s *skippedPaths) warn(msg string) {
	if s.count > 0 {
		log.Warn().Int("count", s.count).Str("first", s.first).Msg(msg)
	}
}

// holds reports whether a torrent file shares the identity of info, a regular
// file read with Lstat. Like the scan, a target with more than one link is
// held only when it is a torrent file's own directory entry; another hardlink
// of it is not.
func (ids *inUseIDs) holds(ctx context.Context, backend fsops.Backend, info *fsops.LstatInfo) (bool, error) {
	if !info.Mode.IsRegular() {
		return false, nil
	}
	held, err := ids.reaches(info)
	if !held || err != nil || info.Nlinks <= 1 {
		return held, err
	}
	key, _ := fsops.FileKeyOf(info.FileID, ids.instance)
	paths, ok := ids.linkedInUse[key]
	if !ok {
		// The torrent's file had one link when it was read.
		return true, nil
	}
	// The scan never keys a torrent path that is itself a symlink, but here a
	// torrent can reach the target through one, and the link's entry is not the
	// target's.
	// A torrent path that is gone since collectInUseIDs ran protects nothing,
	// as in collectInUseIDs.
	linkInfos, linkErrs := fsops.LstatAll(ctx, backend, paths)
	live := make([]string, 0, len(paths))
	for i := range paths {
		if pathGone(linkErrs[i]) {
			continue
		}
		if linkErrs[i] != nil {
			return false, fmt.Errorf("read torrent file %s: %w", paths[i], linkErrs[i])
		}
		if linkInfos[i].IsSymlink {
			return true, nil
		}
		live = append(live, paths[i])
	}
	paths = live
	d := backend.Paths()
	dirs := make([]string, 0, len(paths)+1)
	dirs = append(dirs, d.Dir(info.Path))
	for _, p := range paths {
		dirs = append(dirs, d.Dir(p))
	}
	dirIDs := readDirIDs(ctx, backend, ids.instance, dirs)
	for _, p := range paths {
		if sameEntry(d, dirIDs, info.Path, p) {
			return true, nil
		}
	}
	return false, nil
}

// pathGone reports whether err says that no file is at the path any more.
func pathGone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP)
}

// readDirIDs reads the identity of each folder, following links. A folder
// whose identity cannot be read is left out, so sameEntry keeps a hardlink in
// it when the names match.
func readDirIDs(ctx context.Context, backend fsops.Backend, instance *models.Instance, dirs []string) map[string]fsops.FileKey {
	slices.Sort(dirs)
	dirs = slices.Compact(dirs)
	infos, errs := fsops.StatAll(ctx, backend, dirs)
	ids := make(map[string]fsops.FileKey, len(dirs))
	failed, first, firstErr := 0, "", error(nil)
	for i, dir := range dirs {
		err := errs[i]
		if err == nil {
			err = infos[i].FileIDErr
		}
		if err != nil {
			if failed == 0 {
				first, firstErr = dir, err
			}
			failed++
			continue
		}
		if key, ok := fsops.FileKeyOf(infos[i].FileID, instance); ok {
			ids[dir] = key
		}
	}
	if failed > 0 {
		log.Debug().Int("count", failed).Str("first", first).Err(firstErr).
			Msg("orphanscan: keeping hardlinks with a matching name in folders qui cannot identify")
	}
	return ids
}

// sameEntry reports whether two paths to one file are one directory entry:
// the same name in the same folder, as a bind mount or a symlinked folder
// shows it. A hardlink is an entry of its own. A folder of unknown identity
// could be the same one. Names compare like the torrent file map does, since a
// case-insensitive volume opens a torrent's file under any case of its name.
func sameEntry(d fsops.PathDialect, dirIDs map[string]fsops.FileKey, a, b string) bool {
	if fsops.NormalizePath(d, d.Base(a)) != fsops.NormalizePath(d, d.Base(b)) {
		return false
	}
	aID, aOK := dirIDs[d.Dir(a)]
	bID, bOK := dirIDs[d.Dir(b)]
	return !aOK || !bOK || aID == bID
}

// reaches reports whether a torrent's path leads to info, a folder read with
// Lstat, under any spelling.
func (ids *inUseIDs) reaches(info *fsops.LstatInfo) (bool, error) {
	if ids == nil {
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
// only decides how the outcome is reported. A directory a torrent reaches under
// another path stays, since a torrent that has not written yet leaves it empty.
func safeDeleteEmptyDir(ctx context.Context, scanRoot, target string, protected *protectedPaths, backend fsops.Backend) (deleteDisposition, error) {
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
	reached, err := protected.inUse.reaches(info)
	if err != nil {
		return 0, err
	}
	if reached {
		return deleteDispositionSkippedDirInUseThroughAlias, nil
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
