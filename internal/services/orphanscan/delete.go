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

var ErrInUse = errors.New("contains in-use torrent file")

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

func withinScanRoot(d fsops.PathDialect, scanRoot, target string) error {
	if !d.IsAbs(target) {
		return fmt.Errorf("refusing non-absolute path: %s", target)
	}

	normRoot := fsops.NormalizePath(d, scanRoot)
	normTarget := fsops.NormalizePath(d, target)

	if normTarget == normRoot {
		return fmt.Errorf("refusing to delete scan root: %s", scanRoot)
	}
	if isAbsoluteFilesystemRoot(d, normRoot) || !isPathUnderNormalized(d, normTarget, normRoot) {
		return fmt.Errorf("path escapes scan root: %s", target)
	}
	return nil
}

func safeDeleteFile(ctx context.Context, scanRoot, target string, protected *protectedPaths, backend fsops.Backend) (deleteDisposition, error) {
	d := backend.Paths()
	if err := withinScanRoot(d, scanRoot, target); err != nil {
		return 0, err
	}

	if protected.torrentFile(target) {
		return deleteDispositionSkippedInUse, nil
	}

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
		for range ch {
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

func checkEntryInUse(ctx context.Context, entry *fsops.WalkEntry, protected *protectedPaths, backend fsops.Backend) error {
	if err := checkFileInUse(entry.Path, protected); err != nil {
		return err
	}
	info := &entry.LstatInfo
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

type inUseIDs struct {
	instance    *models.Instance
	keys        map[fsops.FileKey]struct{}
	linkedInUse map[fsops.FileKey][]string
	fileDirs    map[string]struct{}
}

func collectInUseIDs(ctx context.Context, backend fsops.Backend, instance *models.Instance, paths []string, failDenied bool) (*inUseIDs, error) {
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
			if isUnresolvableLinkError(err) {
				unresolvable.add(unique[i])
				continue
			}
			if pathGone(err) {
				continue
			}
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
		return true, nil
	}
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

func pathGone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP)
}

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

func sameEntry(d fsops.PathDialect, dirIDs map[string]fsops.FileKey, a, b string) bool {
	if fsops.NormalizePath(d, d.Base(a)) != fsops.NormalizePath(d, d.Base(b)) {
		return false
	}
	aID, aOK := dirIDs[d.Dir(a)]
	bID, bOK := dirIDs[d.Dir(b)]
	return !aOK || !bOK || aID == bID
}

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
