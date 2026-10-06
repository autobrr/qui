// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlink"
)

var discLayoutMarkers = []string{"BDMV", "VIDEO_TS"}

var ignoredOrphanFileNames = []string{
	".DS_Store",
	".directory",
	"desktop.ini",
	"Thumbs.db",
}

// ignoredOrphanFileNamePrefixes are filename (not path) prefixes that are commonly created by
// OS/NAS layers and should not be treated as orphan content (e.g. ".fuse_hidden*", ".nfs*", "._*").
var ignoredOrphanFileNamePrefixes = []string{
	".fuse",
	".goutputstream-",
	".nfs",
	"._",
	".#",
	"~$",
}

// ignoredOrphanFileNameSuffixes are filename (not path) suffixes that are commonly created by
// torrent clients and should not be treated as orphan content (e.g. "*.parts" from qBittorrent).
var ignoredOrphanFileNameSuffixes = []string{
	".parts",
	".!qB",
}

// ignoredOrphanDirNames are directory names that should be skipped entirely during scanning.
// These are typically system metadata/recycle bins/snapshot internals, not real content.
var ignoredOrphanDirNames = []string{
	".AppleDB",
	".AppleDouble",
	".TemporaryItems",
	".Trashes",
	".Recycle.Bin",
	".recycle",
	".snapshot",
	".snapshots",
	".zfs",
	"@eaDir",
	"$RECYCLE.BIN",
	"#recycle",
	"lost+found",
	"System Volume Information",
}

// ignoredOrphanDirNamePrefixes are directory-name prefixes that should be skipped entirely.
var ignoredOrphanDirNamePrefixes = []string{
	".Trash-",
	"..",
}

// discUnitDecision represents the cached decision for disc-layout unit selection.
type discUnitDecision struct {
	chosenUnit      string // The unit path (or empty if grouping disabled)
	disableGrouping bool   // True means return the file path itself, not a unit
}

// walkScanRoot walks a directory tree and returns orphan files not in the TorrentFileMap.
// Only files are returned as orphans; directories go through walkScanRootCollectingDirs.
func walkScanRoot(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int, backend fsops.Backend) ([]OrphanFile, bool, error) {
	orphans, _, truncated, err := walkScanRootWithUnitFilter(ctx, root, tfm, ignorePaths, gracePeriod, maxFiles, nil, backend, false)
	return orphans, truncated, err
}

type scanWalker struct {
	ctx         context.Context
	root        string
	tfm         *TorrentFileMap
	ignorePaths []string
	gracePeriod time.Duration
	maxFiles    int
	unitFilter  func(unitPath string, isDiscUnit bool) bool
	backend     fsops.Backend
	d           fsops.PathDialect

	// seenDirs records directory mtimes and direct file counts when requested.
	// abandonedDirCandidates makes the final removal decision.
	collectDirs bool
	seenDirs    map[string]*AbandonedDir

	orphanUnits map[string]*OrphanFile
	// Each disc unit's first in-use and first orphan file in lexical walk order, see keepFirstInWalkOrder.
	discUnitFirstInUse  map[string]string
	discUnitFirstOrphan map[string]string
	discUnitCache       map[string]discUnitDecision
	seenFileIDs         map[hardlink.FileID]struct{}
	truncated           bool
}

func newScanWalker(
	ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
	backend fsops.Backend, collectDirs bool,
) *scanWalker {
	return &scanWalker{
		ctx:                 ctx,
		root:                root,
		tfm:                 tfm,
		ignorePaths:         ignorePaths,
		gracePeriod:         gracePeriod,
		maxFiles:            maxFiles,
		unitFilter:          unitFilter,
		backend:             backend,
		d:                   backend.Paths(),
		orphanUnits:         make(map[string]*OrphanFile),
		discUnitFirstInUse:  make(map[string]string),
		discUnitCache:       make(map[string]discUnitDecision),
		discUnitFirstOrphan: make(map[string]string),
		seenFileIDs:         make(map[hardlink.FileID]struct{}),
		collectDirs:         collectDirs,
		seenDirs:            make(map[string]*AbandonedDir),
	}
}

// candidateDirs excludes directories with known retained files. The counts
// only reject candidates; abandonedDirCandidates checks all remaining ones.
func (w *scanWalker) candidateDirs(deleted []OrphanFile) []AbandonedDir {
	if len(w.seenDirs) == 0 {
		return nil
	}
	for _, file := range deleted {
		if dir := w.seenDirs[w.d.Dir(file.Path)]; dir != nil {
			dir.directFiles--
		}
	}
	var dirs []AbandonedDir
	for _, dir := range w.seenDirs {
		// ponytail: disc children can lower this count; exclude their paths
		// from subtraction if the extra rereads become costly.
		if dir.directFiles > 0 {
			continue
		}
		// A torrent that has not written its payload yet still owns its save path.
		if w.tfm.HasAnyInDir(normalizePath(w.d, dir.Path)) {
			continue
		}
		dirs = append(dirs, *dir)
	}
	return dirs
}

// shouldSkipDuplicate dedups nlink==1 files only: seeing the same FileID twice
// at nlink==1 means the same file is reachable through a second path (bind
// mount, mergerfs branch), so the alias must not be treated as an independent
// orphan (#1212). Hardlinked files (nlink>1) are distinct directory entries and
// each path is reported.
func (w *scanWalker) shouldSkipDuplicate(fid hardlink.FileID, nlinks uint64) bool {
	if fid.IsZero() || nlinks > 1 {
		return false
	}
	if _, exists := w.seenFileIDs[fid]; exists {
		return true
	}
	w.seenFileIDs[fid] = struct{}{}
	return false
}

func (w *scanWalker) markInUse(unitPath, path string, isDiscUnit bool) {
	if !isDiscUnit {
		return
	}
	keepFirstInWalkOrder(w.d, w.discUnitFirstInUse, unitPath, path)
	delete(w.orphanUnits, unitPath)
}

// keepFirstInWalkOrder lets a walk that interleaves sibling directories, as a
// concurrent remote walk does, decide disc units the way the lexical local walk does.
func keepFirstInWalkOrder(d fsops.PathDialect, firsts map[string]string, unitPath, path string) {
	if first, ok := firsts[unitPath]; !ok || walksBefore(d, path, first) {
		firsts[unitPath] = path
	}
}

// walksBefore reports whether a lexical walk reaches file a before file b.
// It compares names segment by segment, so "AUX/x" comes before "AUX.d/y".
func walksBefore(d fsops.PathDialect, a, b string) bool {
	sep := d.Separator()
	for i := range min(len(a), len(b)) {
		if a[i] == b[i] {
			continue
		}
		if strings.HasPrefix(a[i:], sep) {
			return true
		}
		if strings.HasPrefix(b[i:], sep) {
			return false
		}
		return a[i] < b[i]
	}
	return len(a) < len(b)
}

func (w *scanWalker) isDiscUnitInUse(unitPath string) bool {
	_, ok := w.discUnitFirstInUse[unitPath]
	return ok
}

// outermostDiscUnit returns the outermost disc root that contains normUnit.
// Inner disc roots also fold into the outer root, so a unit folded into an
// inner root could lose its size, depending on map order (#3002).
func outermostDiscUnit(d fsops.PathDialect, normUnit string, discRoots map[string]string) (string, bool) {
	outermost := ""
	// Not d.Dir: it cleans each parent again, and normUnit is already clean.
	sep := d.Separator()
	for dir, _, ok := strings.CutLast(normUnit, sep); ok && dir != ""; dir, _, ok = strings.CutLast(dir, sep) {
		if discUnitPath, ok := discRoots[dir]; ok {
			outermost = discUnitPath
		}
	}
	return outermost, outermost != ""
}

func (w *scanWalker) mergeSuppressedUnitsIntoDiscUnits() {
	if len(w.discUnitFirstOrphan) == 0 {
		return
	}

	// Do not fold: two real sibling directories that differ only by case would
	// merge into one on a case-sensitive filesystem.
	discRoots := make(map[string]string)
	for du, firstOrphan := range w.discUnitFirstOrphan {
		// Keeps what the lexical local walk reports: siblings are hidden only when an orphan disc file comes first.
		if firstInUse, ok := w.discUnitFirstInUse[du]; ok && walksBefore(w.d, firstInUse, firstOrphan) {
			continue
		}
		discRoots[cleanPath(w.d, du)] = du
	}

	for unit, entry := range w.orphanUnits {
		discUnitPath, ok := outermostDiscUnit(w.d, cleanPath(w.d, unit), discRoots)
		if !ok {
			continue
		}
		w.mergeIntoDiscUnit(unit, discUnitPath, entry)
	}
}

func (w *scanWalker) mergeIntoDiscUnit(unit, discUnitPath string, entry *OrphanFile) {
	discEntry, ok := w.orphanUnits[discUnitPath]
	if ok {
		discEntry.Size += entry.Size
		if entry.ModifiedAt.After(discEntry.ModifiedAt) {
			discEntry.ModifiedAt = entry.ModifiedAt
		}
	}
	delete(w.orphanUnits, unit)
}

func (w *scanWalker) orphans() []OrphanFile {
	w.mergeSuppressedUnitsIntoDiscUnits()

	orphans := make([]OrphanFile, 0, len(w.orphanUnits))
	for _, o := range w.orphanUnits {
		orphans = append(orphans, *o)
	}
	return orphans
}

// walkScanRootCollectingDirs also returns candidates for empty-directory cleanup.
// It never caps the walk; the run's cap applies afterwards.
func walkScanRootCollectingDirs(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, backend fsops.Backend,
) ([]OrphanFile, []AbandonedDir, error) {
	orphans, dirs, _, err := walkScanRootWithUnitFilter(ctx, root, tfm, ignorePaths, gracePeriod, 0, nil, backend, true)
	return orphans, dirs, err
}

func walkScanRootWithUnitFilter(
	ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
	backend fsops.Backend, collectDirs bool,
) ([]OrphanFile, []AbandonedDir, bool, error) {
	w := newScanWalker(ctx, root, tfm, ignorePaths, gracePeriod, maxFiles, unitFilter, backend, collectDirs)

	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := backend.WalkDir(walkCtx, root, fsops.WalkOptions{
		IgnoreDirNames:        ignoredOrphanDirNames,
		IgnoreDirNamePrefixes: ignoredOrphanDirNamePrefixes,
		IgnorePaths:           ignorePaths,
		WantFileID:            true,
		EmitStatErrors:        true,
	})
	if err != nil {
		cancelWalk()
		return nil, nil, false, fmt.Errorf("walk %s: %w", root, err)
	}
	defer func() {
		cancelWalk()
		for range ch { //nolint:revive // drain channel to avoid leaking sender goroutine
		}
	}()

	for entry := range ch {
		if ctx.Err() != nil {
			break
		}

		if entry.Err != nil {
			// A backend that wraps both errors must not have its cut read as a
			// denied subtree. Skipping it would take a cut-short tree as complete.
			if errors.Is(entry.Err, fs.ErrPermission) && entry.Path != root &&
				!errors.Is(entry.Err, fsops.ErrConnectionLost) {
				continue
			}
			return nil, nil, false, entry.Err
		}

		// Skip symlinks
		if entry.IsSymlink {
			continue
		}

		// Handle directories: skip ignored paths and ignored dir names
		if entry.IsDir {
			// Note: backend.WalkDir handles IgnorePaths/IgnoreDirNames via WalkOptions,
			// but orphanscan has its own ignore logic that runs at the walker level.
			// Directories are never orphan files; they feed disc-unit detection and
			// the abandoned-directory candidates.
			if w.collectDirs && entry.Path != w.root {
				w.seenDirs[entry.Path] = &AbandonedDir{Path: entry.Path, ModTime: entry.ModTime}
			}
			continue
		}

		// Handle files
		path := entry.Path
		if w.collectDirs {
			parent := w.d.Dir(path)
			if dir := w.seenDirs[parent]; dir != nil {
				dir.directFiles++
			}
		}
		if isIgnoredPath(w.d, path, w.ignorePaths) {
			continue
		}

		unitPath, isDiscUnit := discOrphanUnitWithContext(ctx, w.root, path, w.tfm, w.discUnitCache, w.ignorePaths, w.backend)
		normPath := normalizePath(w.d, path)
		if w.tfm.Has(normPath) {
			w.markInUse(unitPath, path, isDiscUnit)
			w.shouldSkipDuplicate(entry.FileID, entry.Nlinks)
			continue
		}
		if entry.StatErr != nil {
			continue
		}

		name := w.d.Base(path)
		if isIgnoredOrphanFileName(name) {
			continue
		}

		if !entry.ModTime.IsZero() && time.Since(entry.ModTime) < w.gracePeriod {
			continue
		}
		if w.shouldSkipDuplicate(entry.FileID, entry.Nlinks) {
			continue
		}
		if isDiscUnit {
			keepFirstInWalkOrder(w.d, w.discUnitFirstOrphan, unitPath, path)
			if w.isDiscUnitInUse(unitPath) {
				continue
			}
		}
		if w.unitFilter != nil && !w.unitFilter(unitPath, isDiscUnit) {
			continue
		}

		existing, exists := w.orphanUnits[unitPath]
		if !exists {
			if w.maxFiles > 0 && len(w.orphanUnits) >= w.maxFiles {
				w.truncated = true
				break
			}
			existing = &OrphanFile{Path: unitPath, Status: FileStatusPending}
			w.orphanUnits[unitPath] = existing
		}
		existing.Size += entry.Size
		if existing.ModifiedAt.IsZero() || entry.ModTime.After(existing.ModifiedAt) {
			existing.ModifiedAt = entry.ModTime
		}
	}

	orphans := w.orphans()
	return orphans, w.candidateDirs(orphans), w.truncated, ctx.Err()
}

// findDiscMarker scans path segments for a disc-layout marker (BDMV, VIDEO_TS).
// Returns the marker index, actual on-disk segment name, and uppercase marker.
func findDiscMarker(d fsops.PathDialect, relDir string) (markerIndex int, markerSegment, markerUpper string, found bool) {
	i := 0
	for seg := range strings.SplitSeq(relDir, d.Separator()) {
		segUpper := strings.ToUpper(seg)
		if slices.Contains(discLayoutMarkers, segUpper) {
			return i, seg, segUpper, true
		}
		i++
	}
	return -1, "", "", false
}

// buildDiscCandidatePaths builds the candidate parent and marker absolute paths.
func buildDiscCandidatePaths(d fsops.PathDialect, root string, segments []string, markerIndex int, markerSegment string) (candidateAbs, markerAbs string) {
	var unitRel, markerRel string
	if markerIndex == 0 {
		unitRel = markerSegment
		markerRel = markerSegment
	} else {
		// The segments split a clean relative path, so their join is clean and never empty.
		unitRel = strings.Join(segments[:markerIndex], d.Separator())
		markerRel = d.Join(unitRel, markerSegment)
	}
	candidateAbs = d.Clean(d.Join(root, unitRel))
	markerAbs = d.Clean(d.Join(root, markerRel))
	return candidateAbs, markerAbs
}

// chooseDiscUnit decides whether to use the parent folder, marker folder, or disable grouping.
func chooseDiscUnit(ctx context.Context, candidateAbs, markerAbs, markerUpper string, tfm *TorrentFileMap, ignorePaths []string, backend fsops.Backend) discUnitDecision {
	d := backend.Paths()
	parentProtected := len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(d, candidateAbs, ignorePaths)
	markerProtected := len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(d, markerAbs, ignorePaths)

	if parentProtected && markerProtected {
		return discUnitDecision{disableGrouping: true}
	}
	if parentProtected {
		return discUnitDecision{chosenUnit: markerAbs}
	}
	if markerProtected {
		return discUnitDecision{disableGrouping: true}
	}

	// Neither protected: check torrent file safety
	if discParentIsSafeDiscRoot(ctx, candidateAbs, markerUpper, tfm, backend) {
		return discUnitDecision{chosenUnit: candidateAbs}
	}
	return discUnitDecision{chosenUnit: markerAbs}
}

func discRelativeDir(d fsops.PathDialect, root, path string) (string, bool) {
	rel, err := d.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}

	relDir := d.Dir(rel)
	if relDir == "." {
		return "", false
	}

	return relDir, true
}

func discUnitFromParentMarker(
	ctx context.Context,
	originalPath, candidateAbs, markerAbs, markerUpper string,
	tfm *TorrentFileMap,
	unitCache map[string]discUnitDecision,
	ignorePaths []string,
	backend fsops.Backend,
) (unitPath string, ok bool) {
	// Do not fold: the cached decision holds the first caller's absolute path, so
	// two case-variant sibling directories would share one entry.
	key := cleanPath(backend.Paths(), candidateAbs) + "|" + markerUpper
	if unitCache != nil {
		if decision, ok := unitCache[key]; ok {
			if decision.disableGrouping {
				return originalPath, false
			}
			return decision.chosenUnit, true
		}
	}

	decision := chooseDiscUnit(ctx, candidateAbs, markerAbs, markerUpper, tfm, ignorePaths, backend)
	if unitCache != nil {
		unitCache[key] = decision
	}
	if decision.disableGrouping {
		return originalPath, false
	}
	return decision.chosenUnit, true
}

// discOrphanUnitWithContext groups disc files without including ignored content.
// Sibling orphans are suppressed after the walk if it selects a parent disc unit.
func discOrphanUnitWithContext(ctx context.Context, scanRoot, filePath string, tfm *TorrentFileMap, unitCache map[string]discUnitDecision, ignorePaths []string, backend fsops.Backend) (unitPath string, ok bool) {
	d := backend.Paths()
	root := d.Clean(scanRoot)
	path := d.Clean(filePath)

	relDir, ok := discRelativeDir(d, root, path)
	if !ok {
		return path, false
	}

	markerIndex, markerSegment, markerUpper, found := findDiscMarker(d, relDir)
	if !found {
		return path, false
	}

	candidateAbs, markerAbs := buildDiscCandidatePaths(d, root, strings.Split(relDir, d.Separator()), markerIndex, markerSegment)
	if candidateAbs == root {
		return markerAbs, true
	}

	if markerIndex > 0 {
		return discUnitFromParentMarker(ctx, path, candidateAbs, markerAbs, markerUpper, tfm, unitCache, ignorePaths, backend)
	}

	if len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(d, markerAbs, ignorePaths) {
		return path, false
	}

	return markerAbs, true
}

// isPathUnderNormalized checks if child is strictly under parent.
// Both paths must be clean in d. Callers choose whether to fold case.
func isPathUnderNormalized(d fsops.PathDialect, child, parent string) bool {
	return len(child) > len(parent) && strings.HasPrefix(child, parent) &&
		strings.HasPrefix(child[len(parent):], d.Separator())
}

var discRootAllowedFiles = map[string]struct{}{
	"DESKTOP.INI": {},
	"THUMBS.DB":   {},
	".DS_STORE":   {},
}

func discAllowedDirs(marker string) map[string]struct{} {
	switch strings.ToUpper(marker) {
	case "BDMV":
		return map[string]struct{}{
			"BDMV":        {},
			"CERTIFICATE": {},
		}
	case "VIDEO_TS":
		return map[string]struct{}{
			"VIDEO_TS": {},
			"AUDIO_TS": {},
		}
	default:
		return map[string]struct{}{strings.ToUpper(marker): {}}
	}
}

func discAllowedNames(marker string) (allowedDirs, allowedFiles map[string]struct{}) {
	return discAllowedDirs(marker), discRootAllowedFiles
}

func discParentIsPureDiscRoot(ctx context.Context, parentAbs, marker string, backend fsops.Backend) bool {
	entries, err := backend.ReadDir(ctx, parentAbs)
	if err != nil {
		return false
	}

	allowedDirs, allowedFiles := discAllowedNames(marker)
	for _, e := range entries {
		nameUpper := strings.ToUpper(e.Name)
		if e.IsDir {
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

func discParentIsSafeDiscRoot(ctx context.Context, parentAbs, marker string, tfm *TorrentFileMap, backend fsops.Backend) bool {
	if tfm == nil {
		return discParentIsPureDiscRoot(ctx, parentAbs, marker, backend)
	}

	entries, err := backend.ReadDir(ctx, parentAbs)
	if err != nil {
		return false
	}

	d := backend.Paths()
	allowedDirs, allowedFiles := discAllowedNames(marker)
	for _, e := range entries {
		nameUpper := strings.ToUpper(e.Name)
		full := d.Join(parentAbs, e.Name)
		if e.IsDir {
			if _, ok := allowedDirs[nameUpper]; ok {
				continue
			}
			if tfm.HasAnyInDir(normalizePath(d, full)) {
				return false
			}
			continue
		}
		if _, ok := allowedFiles[nameUpper]; ok {
			continue
		}
		if tfm.Has(normalizePath(d, full)) {
			return false
		}
	}

	return true
}

// isIgnoredPath checks if path matches any ignore prefix with boundary safety.
// Ensures /data/foo doesn't match /data/foobar (requires separator after prefix).
// Uses normalizePath for consistent comparison across platforms.
func isIgnoredPath(d fsops.PathDialect, path string, ignorePaths []string) bool {
	normPath := normalizePath(d, path)
	for _, prefix := range ignorePaths {
		normPrefix := normalizePath(d, prefix)
		if normPath == normPrefix || isPathUnderNormalized(d, normPath, normPrefix) {
			return true
		}
	}
	return false
}

func isIgnoredOrphanFileName(name string) bool {
	for _, exact := range ignoredOrphanFileNames {
		if strings.EqualFold(name, exact) {
			return true
		}
	}
	for _, prefix := range ignoredOrphanFileNamePrefixes {
		if hasPrefixFold(name, prefix) {
			return true
		}
	}
	for _, suffix := range ignoredOrphanFileNameSuffixes {
		if hasSuffixFold(name, suffix) {
			return true
		}
	}
	return false
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func hasSuffixFold(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

// isPathProtectedByIgnorePaths checks if a path should not be deleted because:
//  1. The path itself is ignored, OR
//  2. The path contains any ignored descendant (any ignore path has this path as a parent)
//
// This prevents deleting directories that contain ignored content.
func isPathProtectedByIgnorePaths(d fsops.PathDialect, path string, ignorePaths []string) bool {
	// Check if the path itself is ignored
	if isIgnoredPath(d, path, ignorePaths) {
		return true
	}

	// Check if any ignore path is a descendant of this path
	normPath := normalizePath(d, path)
	for _, ignorePath := range ignorePaths {
		if isPathUnderNormalized(d, normalizePath(d, ignorePath), normPath) {
			return true
		}
	}
	return false
}

// NormalizeIgnorePaths validates and normalizes ignore paths in d, the dialect of
// the instance's backend.
// All paths must be absolute. The result is stored in settings and shown in the
// UI, so it keeps the casing the user typed; matching case-folds on both sides
// (see isIgnoredPath).
func NormalizeIgnorePaths(d fsops.PathDialect, paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned := d.Clean(p)
		if !d.IsAbs(cleaned) {
			return nil, fmt.Errorf("ignore path must be absolute: %s", p)
		}
		result = append(result, cleanPath(d, cleaned))
	}
	return result, nil
}
