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
	"github.com/autobrr/qui/internal/models"
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

// walkScanRoot walks a directory tree and returns orphan files that are not
// Protected paths.
// Only files are returned as orphans; directories go through walkScanRootCollectingDirs.
func walkScanRoot(ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, maxFiles int, backend fsops.Backend, instance *models.Instance) ([]OrphanFile, bool, error) {
	orphans, _, truncated, err := walkScanRootWithUnitFilter(ctx, root, protected, gracePeriod, maxFiles, nil, backend, instance, false)
	return orphans, truncated, err
}

type scanWalker struct {
	ctx         context.Context
	root        string
	protected   *protectedPaths
	gracePeriod time.Duration
	maxFiles    int
	unitFilter  func(unitPath string, isDiscUnit bool) bool
	backend     fsops.Backend
	instance    *models.Instance // the row backend was built from; scopes seenFileIDs
	d           fsops.PathDialect

	// seenDirs records directory mtimes and direct file counts when requested.
	// abandonedDirCandidates makes the final removal decision.
	collectDirs bool
	seenDirs    map[string]*AbandonedDir

	orphanUnits map[string]*OrphanFile
	// The walk only records these; resolveDiscUnits decides, so walk order cannot change the result.
	discUnitsInUse      map[string]struct{}
	discUnitsWithOrphan map[string]struct{}
	discUnitCache       map[string]discUnitDecision
	seenFileIDs         map[fsops.FileKey]struct{}
	truncated           bool
}

func newScanWalker(
	ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
	backend fsops.Backend, instance *models.Instance, collectDirs bool,
) *scanWalker {
	return &scanWalker{
		ctx:                 ctx,
		root:                root,
		protected:           protected,
		gracePeriod:         gracePeriod,
		maxFiles:            maxFiles,
		unitFilter:          unitFilter,
		backend:             backend,
		instance:            instance,
		d:                   backend.Paths(),
		orphanUnits:         make(map[string]*OrphanFile),
		discUnitsInUse:      make(map[string]struct{}),
		discUnitCache:       make(map[string]discUnitDecision),
		discUnitsWithOrphan: make(map[string]struct{}),
		seenFileIDs:         make(map[fsops.FileKey]struct{}),
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
		if w.protected.torrentFileIn(dir.Path) {
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
	if nlinks > 1 {
		return false
	}
	key, ok := fsops.FileKeyOf(fid, w.instance)
	if !ok {
		return false
	}
	if _, exists := w.seenFileIDs[key]; exists {
		return true
	}
	w.seenFileIDs[key] = struct{}{}
	return false
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

func (w *scanWalker) resolveDiscUnits() {
	// A disc unit with an in-use file stays whole and hides nothing beside it.
	// A torrent file that is not on disk yet also puts the unit in use.
	for du := range w.discUnitsWithOrphan {
		if w.protected.torrentFileIn(du) {
			w.discUnitsInUse[du] = struct{}{}
		}
	}
	for du := range w.discUnitsInUse {
		delete(w.orphanUnits, du)
	}

	// Do not fold: two real sibling directories that differ only by case would
	// merge into one on a case-sensitive filesystem.
	discRoots := make(map[string]string)
	for du := range w.discUnitsWithOrphan {
		if _, inUse := w.discUnitsInUse[du]; !inUse {
			discRoots[fsops.CleanPath(w.d, du)] = du
		}
	}
	if len(discRoots) == 0 {
		return
	}

	for unit, entry := range w.orphanUnits {
		discUnitPath, ok := outermostDiscUnit(w.d, fsops.CleanPath(w.d, unit), discRoots)
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
	w.resolveDiscUnits()

	orphans := make([]OrphanFile, 0, len(w.orphanUnits))
	for _, o := range w.orphanUnits {
		orphans = append(orphans, *o)
	}
	return orphans
}

// walkScanRootCollectingDirs also returns candidates for empty-directory cleanup.
// It never caps the walk; the run's cap applies afterwards.
func walkScanRootCollectingDirs(ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, backend fsops.Backend, instance *models.Instance,
) ([]OrphanFile, []AbandonedDir, error) {
	orphans, dirs, _, err := walkScanRootWithUnitFilter(ctx, root, protected, gracePeriod, 0, nil, backend, instance, true)
	return orphans, dirs, err
}

func walkScanRootWithUnitFilter(
	ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
	backend fsops.Backend, instance *models.Instance, collectDirs bool,
) ([]OrphanFile, []AbandonedDir, bool, error) {
	w := newScanWalker(ctx, root, protected, gracePeriod, maxFiles, unitFilter, backend, instance, collectDirs)

	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := backend.WalkDir(walkCtx, root, fsops.WalkOptions{
		IgnoreDirNames:        ignoredOrphanDirNames,
		IgnoreDirNamePrefixes: ignoredOrphanDirNamePrefixes,
		IgnorePaths:           protected.ignorePaths,
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
		if w.protected.ignored(path) {
			continue
		}

		unitPath, isDiscUnit := discOrphanUnitWithContext(ctx, w.root, path, w.protected, w.discUnitCache, w.backend)
		if w.protected.torrentFile(path) {
			if isDiscUnit {
				w.discUnitsInUse[unitPath] = struct{}{}
			}
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
			w.discUnitsWithOrphan[unitPath] = struct{}{}
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
func chooseDiscUnit(ctx context.Context, candidateAbs, markerAbs, markerUpper string, protected *protectedPaths, backend fsops.Backend) discUnitDecision {
	parentProtected := protected.ignoredOrHoldsIgnored(candidateAbs)
	markerProtected := protected.ignoredOrHoldsIgnored(markerAbs)

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
	if discParentIsSafeDiscRoot(ctx, candidateAbs, markerUpper, protected, backend) {
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
	protected *protectedPaths,
	unitCache map[string]discUnitDecision,
	backend fsops.Backend,
) (unitPath string, ok bool) {
	// Do not fold: the cached decision holds the first caller's absolute path, so
	// two case-variant sibling directories would share one entry.
	key := fsops.CleanPath(backend.Paths(), candidateAbs) + "|" + markerUpper
	if unitCache != nil {
		if decision, ok := unitCache[key]; ok {
			if decision.disableGrouping {
				return originalPath, false
			}
			return decision.chosenUnit, true
		}
	}

	decision := chooseDiscUnit(ctx, candidateAbs, markerAbs, markerUpper, protected, backend)
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
func discOrphanUnitWithContext(ctx context.Context, scanRoot, filePath string, protected *protectedPaths, unitCache map[string]discUnitDecision, backend fsops.Backend) (unitPath string, ok bool) {
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
		return discUnitFromParentMarker(ctx, path, candidateAbs, markerAbs, markerUpper, protected, unitCache, backend)
	}

	if protected.ignoredOrHoldsIgnored(markerAbs) {
		return path, false
	}

	return markerAbs, true
}

// isPathUnderNormalized checks if child is strictly under parent.
// Both paths must be clean in d. Callers choose whether to fold case.
func isPathUnderNormalized(d fsops.PathDialect, child, parent string) bool {
	if len(child) <= len(parent) || !strings.HasPrefix(child, parent) {
		return false
	}
	// "/" and "D:\" already end in the separator. A UNC share root may not
	// ("\\server\share"), so the next character of the child still has to be one.
	sep := d.Separator()
	return strings.HasSuffix(parent, sep) || strings.HasPrefix(child[len(parent):], sep)
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

func discParentIsSafeDiscRoot(ctx context.Context, parentAbs, marker string, protected *protectedPaths, backend fsops.Backend) bool {
	if protected.files == nil {
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
			if protected.torrentFileIn(full) {
				return false
			}
			continue
		}
		if _, ok := allowedFiles[nameUpper]; ok {
			continue
		}
		if protected.torrentFile(full) {
			return false
		}
	}

	return true
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

// IgnoredPathMatcher reports, in d, whether a path is protected by the
// instance's ignore paths: the path is ignored or holds an ignored path. Folder
// cleanup uses it whether orphan scan is on or off, so one list protects a
// folder everywhere.
func (s *Service) IgnoredPathMatcher(ctx context.Context, instanceID int, d fsops.PathDialect) (func(path string) bool, error) {
	settings, err := s.store.GetSettings(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to read orphan scan settings: %w", err)
	}
	var paths []string
	if settings != nil {
		paths = settings.IgnorePaths
	}
	protected, err := newProtectedPaths(d, paths, nil)
	if err != nil {
		return nil, err
	}
	return protected.ignoredOrHoldsIgnored, nil
}
