// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
)

// sortDeepestFirst orders directories so a child is always judged, and removed,
// before its parent.
func sortDeepestFirst(dirs []AbandonedDir) []AbandonedDir {
	sorted := make([]AbandonedDir, len(dirs))
	copy(sorted, dirs)
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i].Path) != len(sorted[j].Path) {
			return len(sorted[i].Path) > len(sorted[j].Path)
		}
		return sorted[i].Path < sorted[j].Path
	})
	return sorted
}

// abandonedDirCandidates narrows directories to those safe to remove. deleted
// are the orphans this run removes, so a directory holding only those counts as
// empty, and one inside a deleted disc unit goes with the unit. Deepest first,
// so removing them in order never meets a non-empty directory.
func abandonedDirCandidates(
	ctx context.Context,
	dirs []AbandonedDir,
	deleted []OrphanFile,
	scanRoots, categoryPaths []string,
	protected *protectedPaths,
	gracePeriod time.Duration,
	backend fsops.Backend,
) []OrphanFile {
	if len(dirs) == 0 {
		return nil
	}

	d := backend.Paths()
	// The protected sets are the same for every candidate, so normalize them
	// once rather than once per directory.
	normRoots := normalizePaths(d, scanRoots)
	normCategories := normalizePaths(d, categoryPaths)
	// Keyed like kept, by the spelling the walk produced: case-folding here
	// would let a surviving case-twin pass for the deleted orphan.
	deletedPaths := make(map[string]struct{}, len(deleted))
	for i := range deleted {
		deletedPaths[deleted[i].Path] = struct{}{}
	}

	kept := make(map[string]struct{}, len(dirs))
	out := make([]OrphanFile, 0, len(dirs))

	for _, dir := range dirs {
		if underDeletedPath(d, dir.Path, deletedPaths) {
			continue
		}
		normDir := fsops.NormalizePath(d, dir.Path)
		if slices.Contains(normRoots, normDir) {
			continue
		}
		if protected.ignored(dir.Path) {
			continue
		}
		if isCategoryDestinationNormalized(d, normDir, normCategories) {
			continue
		}
		if !dir.ModTime.IsZero() && time.Since(dir.ModTime) < gracePeriod {
			continue
		}
		if !childrenAllKept(ctx, dir.Path, kept, deletedPaths, backend) {
			continue
		}

		kept[dir.Path] = struct{}{}
		out = append(out, OrphanFile{
			Path:           dir.Path,
			IsAbandonedDir: true,
			ModifiedAt:     dir.ModTime,
			Status:         FileStatusPending,
		})
	}

	return out
}

// childrenAllKept reports whether dir can be emptied by removing directories
// already kept and the orphans the file pass deletes, so a file the run leaves
// behind, an ignored subtree or a symlink leaves it alone.
func childrenAllKept(ctx context.Context, dir string, kept, deletedPaths map[string]struct{}, backend fsops.Backend) bool {
	entries, err := backend.ReadDir(ctx, dir)
	if err != nil {
		log.Debug().Err(err).Str("dir", dir).Msg("orphanscan: could not read directory, not treating it as abandoned")
		return false
	}

	d := backend.Paths()
	for _, entry := range entries {
		// The walk never reports a symlink, so one can never be in deletedPaths.
		if entry.IsSymlink {
			return false
		}
		child := d.Join(dir, entry.Name)
		if _, gone := deletedPaths[child]; gone {
			continue
		}
		if !entry.IsDir {
			return false
		}
		if _, ok := kept[child]; !ok {
			return false
		}
	}
	return true
}

// underDeletedPath reports whether dir is, or sits inside, a path the file pass
// removes. Only a disc unit is a directory-shaped orphan, and the file pass
// removes the whole unit, so nothing below it is left for the directory pass.
func underDeletedPath(d fsops.PathDialect, dir string, deletedPaths map[string]struct{}) bool {
	for {
		if _, gone := deletedPaths[dir]; gone {
			return true
		}
		parent := d.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// normalizePaths normalizes a whole list once, for repeated membership tests.
func normalizePaths(d fsops.PathDialect, paths []string) []string {
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = fsops.NormalizePath(d, p)
	}
	return normalized
}

// isCategoryDestinationNormalized reports whether normPath is a category
// destination or holds one below it. Both stay: qBittorrent will save into them
// again. Callers normalize once and test many paths against the same set.
func isCategoryDestinationNormalized(d fsops.PathDialect, normPath string, normCategories []string) bool {
	for _, nCategory := range normCategories {
		if normPath == nCategory || isPathUnderNormalized(d, nCategory, normPath) {
			return true
		}
	}
	return false
}

// torrentDirs returns every folder above the torrent files, and each of
// saveRoots and the folders above it, up to the filesystem root, once each.
// A torrent can reach a folder under another spelling before it writes
// anything, and an unwritten subfolder is reached only by its file's path, so
// each level counts. saveRoots are the save paths of torrents with no file
// list yet, which say where they will write.
func torrentDirs(d fsops.PathDialect, files []string, saveRoots ...string) []string {
	seen := make(map[string]struct{})
	var dirs []string
	for _, file := range files {
		dirs = addUpToRoot(d, d.Dir(file), seen, dirs)
	}
	for _, root := range saveRoots {
		dirs = addUpToRoot(d, root, seen, dirs)
	}
	return dirs
}

// reachedDirs is torrentDirs for a file map result.
func reachedDirs(d fsops.PathDialect, result *buildFileMapResult) []string {
	return torrentDirs(d, result.torrentPaths, slices.Concat(result.metadataRoots, result.skippedRoots)...)
}

// addUpToRoot stops at the first folder already seen, so a shared prefix costs once.
func addUpToRoot(d fsops.PathDialect, dir string, seen map[string]struct{}, added []string) []string {
	for {
		if _, ok := seen[dir]; ok {
			return added
		}
		seen[dir] = struct{}{}
		added = append(added, dir)
		parent := d.Dir(dir)
		if parent == dir {
			return added
		}
		dir = parent
	}
}

// dropDirsTorrentsReach removes each listed folder a torrent reaches under
// another spelling, such as a symlink, a bind mount or a junction, along with
// the listed folders that hold it, which can no longer be emptied. A folder
// whose identity cannot be read could be one of them, so it goes too.
func dropDirsTorrentsReach(ctx context.Context, dirs []OrphanFile, reached *inUseIDs, backend fsops.Backend) []OrphanFile {
	paths := make([]string, len(dirs))
	for i := range dirs {
		paths[i] = dirs[i].Path
	}
	infos, errs := fsops.LstatAll(ctx, backend, paths)

	d := backend.Paths()
	kept := make(map[string]struct{})
	for i, p := range paths {
		err := errs[i]
		if err == nil {
			var ok bool
			if ok, err = reached.reaches(infos[i]); err == nil && !ok {
				continue
			}
		}
		log.Debug().Err(err).Str("dir", p).Msg("orphanscan: a torrent may use this directory under another path, not treating it as abandoned")
		addUpToRoot(d, p, kept, nil)
	}
	if len(kept) == 0 {
		return dirs
	}

	out := make([]OrphanFile, 0, len(dirs))
	for _, dir := range dirs {
		if _, ok := kept[dir.Path]; !ok {
			out = append(out, dir)
		}
	}
	return out
}
