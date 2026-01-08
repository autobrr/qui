// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var discLayoutMarkers = []string{"BDMV", "VIDEO_TS"}

// walkScanRoot walks a directory tree and returns orphan files not in the TorrentFileMap.
// Only files are returned as orphans - directories are cleaned up separately after file deletion.
func walkScanRoot(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int) ([]OrphanFile, bool, error) {
	return walkScanRootWithUnitFilter(ctx, root, tfm, ignorePaths, gracePeriod, maxFiles, nil)
}

// walkScanRootDiscUnits walks a directory tree and returns only disc-layout orphan units.
// This is intended for diagnostics/local tests to avoid materializing a huge orphan list.
func walkScanRootDiscUnits(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxUnits int) ([]OrphanFile, bool, error) {
	return walkScanRootWithUnitFilter(ctx, root, tfm, ignorePaths, gracePeriod, maxUnits, func(_ string, isDiscUnit bool) bool {
		return isDiscUnit
	})
}

func walkScanRootWithUnitFilter(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool) ([]OrphanFile, bool, error) {

	// We may collapse many files into a single orphan "unit" (e.g. disc-layout folders).
	// Keyed by orphan unit path.
	orphanUnits := make(map[string]*OrphanFile)
	// Tracks disc units that must be considered in-use because at least one file under
	// the disc unit is referenced by a torrent.
	discUnitsInUse := make(map[string]struct{})
	// Cache to avoid re-reading unit directories for every file in a disc layout.
	// Keyed by "<candidateParentAbs>|<marker>" and stores the chosen unit path.
	discUnitCache := make(map[string]string)
	// Tracks disc unit paths (directories) so we can suppress other orphan units contained within.
	discUnitPaths := make(map[string]struct{})

	truncated := false

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			if os.IsPermission(err) {
				return nil // Skip inaccessible, continue walk
			}
			return err
		}

		// Don't follow symlink directories
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil // Skip symlink files too
		}

		// Skip directories entirely - they're not orphans, only files are
		if d.IsDir() {
			// But check ignore paths to skip entire subtrees
			if isIgnoredPath(path, ignorePaths) {
				return fs.SkipDir
			}
			return nil
		}

		// Check ignore paths for files (boundary-safe prefix match)
		if isIgnoredPath(path, ignorePaths) {
			return nil
		}

		// Determine whether this file should be grouped into a disc-layout orphan unit.
		unitPath, isDiscUnit := discOrphanUnitWithContext(root, path, tfm, discUnitCache)

		// If in torrent file map, skip. For disc units, mark the unit as in-use to prevent
		// unsafe partial deletes (disc folder would contain a live torrent file).
		if tfm.Has(normalizePath(path)) {
			if isDiscUnit {
				discUnitsInUse[unitPath] = struct{}{}
				delete(orphanUnits, unitPath)
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil // Skip files we can't stat
		}

		// Grace period check
		if time.Since(info.ModTime()) < gracePeriod {
			return nil
		}

		// If this belongs to a disc unit that is in-use, do not track it as an orphan.
		if isDiscUnit {
			if _, inUse := discUnitsInUse[unitPath]; inUse {
				return nil
			}
			discUnitPaths[unitPath] = struct{}{}
		}

		if unitFilter != nil && !unitFilter(unitPath, isDiscUnit) {
			return nil
		}

		// Cap check is on unique orphan units, not raw file count.
		// When maxFiles <= 0, the scan is unbounded and truncation is disabled.
		if _, exists := orphanUnits[unitPath]; !exists {
			if maxFiles > 0 && len(orphanUnits) >= maxFiles {
				truncated = true
				return fs.SkipAll
			}
			orphanUnits[unitPath] = &OrphanFile{
				Path:       unitPath,
				Size:       0,
				ModifiedAt: info.ModTime(),
				Status:     FileStatusPending,
			}
		}

		entry := orphanUnits[unitPath]
		entry.Size += info.Size()
		if info.ModTime().After(entry.ModifiedAt) {
			entry.ModifiedAt = info.ModTime()
		}
		return nil
	})

	orphans := make([]OrphanFile, 0, len(orphanUnits))
	// Suppress any orphan units that are fully contained within a disc unit directory.
	// This keeps previews clean and avoids redundant deletes (e.g., sibling files next to BDMV).
	if len(discUnitPaths) > 0 {
		for unit := range orphanUnits {
			for discUnit := range discUnitPaths {
				if unit == discUnit {
					continue
				}
				if isPathUnder(unit, discUnit) {
					delete(orphanUnits, unit)
					break
				}
			}
		}
	}
	for _, o := range orphanUnits {
		orphans = append(orphans, *o)
	}

	return orphans, truncated, err
}

// discOrphanUnit detects whether a file path belongs to a disc-layout folder.
// If so, it returns the deletion unit path (directory) that should represent the disc.
//
// Rules:
//   - Detects BDMV and VIDEO_TS directory markers (case-insensitive) anywhere in the path.
//   - Prefers the parent directory above the marker as the unit root.
//   - If the marker is directly under the scan root, the unit becomes the marker directory itself
//     (to avoid attempting to delete the scan root).
func discOrphanUnit(scanRoot, filePath string, cache map[string]string) (unitPath string, ok bool) {
	// Backwards-compatible wrapper (used only by local diagnostic code).
	return discOrphanUnitWithContext(scanRoot, filePath, nil, cache)
}

// discOrphanUnitWithContext detects whether a file path belongs to a disc-layout folder.
// If so, it returns the deletion unit path (directory) that should represent the disc.
//
// Note: sibling orphan files are suppressed at the end of the scan if a parent disc unit is chosen.
func discOrphanUnitWithContext(scanRoot, filePath string, tfm *TorrentFileMap, unitCache map[string]string) (unitPath string, ok bool) {
	root := filepath.Clean(scanRoot)
	path := filepath.Clean(filePath)

	// Work on a scan-root-relative path to avoid Windows drive letter edge cases.
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path, false
	}

	// Scan directory segments (exclude filename).
	relDir := filepath.Dir(rel)
	if relDir == "." {
		return path, false
	}
	segments := strings.Split(relDir, string(filepath.Separator))

	markerIndex := -1
	marker := ""
	for i, seg := range segments {
		segUpper := strings.ToUpper(seg)
		for _, m := range discLayoutMarkers {
			if segUpper == m {
				markerIndex = i
				marker = m
				break
			}
		}
		if markerIndex != -1 {
			break
		}
	}
	if markerIndex == -1 {
		return path, false
	}

	// Build unit relative path.
	var unitRel string
	if markerIndex == 0 {
		// Marker is directly under scan root; unit becomes the marker directory itself.
		unitRel = marker
	} else {
		// Unit is the parent directory above the marker.
		unitRel = filepath.Join(segments[:markerIndex]...)
		if unitRel == "." || unitRel == "" {
			unitRel = marker
		}
	}

	// Resolve absolute paths.
	candidateAbs := filepath.Clean(filepath.Join(root, unitRel))
	markerAbs := filepath.Clean(filepath.Join(candidateAbs, marker))
	if markerIndex == 0 {
		markerAbs = filepath.Clean(filepath.Join(root, marker))
	}

	// Safety: never return scan root as a deletion unit.
	if candidateAbs == root {
		return markerAbs, true
	}

	// If the marker isn't directly under scan root, prefer deleting the parent folder
	// when it's safe to do so. "Safe" here means: no sibling content that is referenced
	// by any torrent file. This allows collapsing disc roots that contain extra filesystem
	// cruft that isn't linked to any torrent.
	if markerIndex > 0 {
		key := normalizePath(candidateAbs) + "|" + marker
		if unitCache != nil {
			if v, ok := unitCache[key]; ok {
				return v, true
			}
		}

		preferParent := discParentIsSafeDiscRoot(candidateAbs, marker, tfm)
		chosen := markerAbs
		if preferParent {
			chosen = candidateAbs
		}
		if unitCache != nil {
			unitCache[key] = chosen
		}
		return chosen, true
	}

	return markerAbs, true
}

func isPathUnder(childPath, parentPath string) bool {
	child := normalizePath(childPath)
	parent := normalizePath(parentPath)
	if child == parent {
		return false
	}
	if !strings.HasPrefix(child, parent) {
		return false
	}
	if len(child) == len(parent) {
		return false
	}
	return child[len(parent)] == filepath.Separator
}

func discParentIsPureDiscRoot(parentAbs string, marker string) bool {
	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		// If we can't confidently evaluate contents, don't risk deleting the parent folder.
		return false
	}

	allowedDirs := map[string]struct{}{}
	switch strings.ToUpper(marker) {
	case "BDMV":
		allowedDirs["BDMV"] = struct{}{}
		allowedDirs["CERTIFICATE"] = struct{}{}
	case "VIDEO_TS":
		allowedDirs["VIDEO_TS"] = struct{}{}
		allowedDirs["AUDIO_TS"] = struct{}{}
	default:
		allowedDirs[strings.ToUpper(marker)] = struct{}{}
	}

	allowedFiles := map[string]struct{}{
		"DESKTOP.INI": {},
		"THUMBS.DB":   {},
		".DS_STORE":   {},
	}

	for _, e := range entries {
		nameUpper := strings.ToUpper(e.Name())
		if e.IsDir() {
			if _, ok := allowedDirs[nameUpper]; ok {
				continue
			}
			return false
		}
		if _, ok := allowedFiles[nameUpper]; ok {
			continue
		}
		return false
	}

	return true
}

func discParentIsSafeDiscRoot(parentAbs string, marker string, tfm *TorrentFileMap) bool {
	// If we don't have a torrent map, retain the stricter "pure disc root" behavior.
	if tfm == nil {
		return discParentIsPureDiscRoot(parentAbs, marker)
	}

	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		// If we can't confidently evaluate contents, don't risk deleting the parent folder.
		return false
	}

	allowedDirs := map[string]struct{}{}
	switch strings.ToUpper(marker) {
	case "BDMV":
		allowedDirs["BDMV"] = struct{}{}
		allowedDirs["CERTIFICATE"] = struct{}{}
	case "VIDEO_TS":
		allowedDirs["VIDEO_TS"] = struct{}{}
		allowedDirs["AUDIO_TS"] = struct{}{}
	default:
		allowedDirs[strings.ToUpper(marker)] = struct{}{}
	}

	allowedFiles := map[string]struct{}{
		"DESKTOP.INI": {},
		"THUMBS.DB":   {},
		".DS_STORE":   {},
	}

	for _, e := range entries {
		nameUpper := strings.ToUpper(e.Name())
		full := filepath.Join(parentAbs, e.Name())
		if e.IsDir() {
			if _, ok := allowedDirs[nameUpper]; ok {
				continue
			}
			// Any torrent file anywhere under this sibling dir makes it unsafe to delete the parent.
			if tfm.HasAnyInDir(normalizePath(full)) {
				return false
			}
			continue
		}
		if _, ok := allowedFiles[nameUpper]; ok {
			continue
		}
		// Any torrent file that is this sibling file makes it unsafe to delete the parent.
		if tfm.Has(normalizePath(full)) {
			return false
		}
	}

	return true
}

// isIgnoredPath checks if path matches any ignore prefix with boundary safety.
// Ensures /data/foo doesn't match /data/foobar (requires separator after prefix).
func isIgnoredPath(path string, ignorePaths []string) bool {
	for _, prefix := range ignorePaths {
		if path == prefix {
			return true
		}
		if strings.HasPrefix(path, prefix) {
			// Ensure match is at path boundary
			if len(path) > len(prefix) && path[len(prefix)] == filepath.Separator {
				return true
			}
		}
	}
	return false
}

// NormalizeIgnorePaths validates and normalizes ignore paths.
// All paths must be absolute.
func NormalizeIgnorePaths(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned := filepath.Clean(p)
		if !filepath.IsAbs(cleaned) {
			return nil, fmt.Errorf("ignore path must be absolute: %s", p)
		}
		result = append(result, cleaned)
	}
	return result, nil
}
