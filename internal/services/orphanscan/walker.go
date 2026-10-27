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
	orphans, _, truncated, err := walkScanRootWithUnitFilter(ctx, root, protected, gracePeriod, maxFiles, nil, backend, instance, false, nil)
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
	instance    *models.Instance // the row backend was built from; scopes file keys
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
	scanRootCache       map[string]bool
	keys                *fileKeys
	truncated           bool
}

func newScanWalker(
	ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
	backend fsops.Backend, instance *models.Instance, collectDirs bool, keys *fileKeys,
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
		scanRootCache:       make(map[string]bool),
		discUnitsWithOrphan: make(map[string]struct{}),
		keys:                keys,
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

// fileKeys spans every root of a run. A second path to an nlink==1 file no
// torrent owns is a bind mount or a path through a symlinked folder and is
// listed once (#1212), while hardlinks no torrent owns are listed at every
// path. A torrent's file protects every path with its identity, since a bind
// mount or a symlinked folder can reach a hardlink too (#3449).
type fileKeys struct {
	inUse map[fsops.FileKey]struct{}
	// Single-link files no torrent owns, as a chain per key through one slab: one slice per key costs an allocation per orphan.
	others map[fsops.FileKey]int
	// linked holds the hardlinked ones, checked only against inUse, so their sizes go unused.
	linked map[fsops.FileKey]int
	refs   []aliasRef
	// incomplete is set once a regular file had no key, so a second path to it could go unmatched.
	incomplete bool
}

type aliasRef struct {
	path string
	prev int   // the key's previous ref, or -1
	size int64 // bytes counted into an orphan's Size, or -1 when the file only sits inside one
}

func newFileKeys() *fileKeys {
	return &fileKeys{
		inUse:  make(map[fsops.FileKey]struct{}),
		others: make(map[fsops.FileKey]int),
		linked: make(map[fsops.FileKey]int),
	}
}

func (k *fileKeys) addOther(key fsops.FileKey, path string, nlinks uint64) int {
	chains := k.others
	if nlinks > 1 {
		chains = k.linked
	}
	prev, ok := chains[key]
	if !ok {
		prev = -1
	}
	k.refs = append(k.refs, aliasRef{path: path, prev: prev, size: -1})
	chains[key] = len(k.refs) - 1
	return len(k.refs) - 1
}

func (k *fileKeys) of(entry fsops.WalkEntry, instance *models.Instance) (fsops.FileKey, bool) {
	key, ok := fsops.FileKeyOf(entry.FileID, instance)
	if !ok {
		k.incomplete = true
	}
	return key, ok
}

// dropAliasOrphans runs once every root is walked, so walk and root order
// cannot decide whether a delete reaches a torrent's file through an alias.
func dropAliasOrphans(d fsops.PathDialect, orphans []OrphanFile, keys *fileKeys) []OrphanFile {
	inUse, aliased := keys.inUseAndAliased()
	if len(aliased) == 0 && len(inUse) == 0 {
		return orphans
	}

	owners := newOrphanOwners(d, orphans)
	drop := make([]bool, len(orphans))
	for _, path := range inUse {
		if i, ok := owners.of(path); ok {
			drop[i] = true
		}
	}
	dups := make([]aliasDups, len(orphans))
	for _, head := range aliased {
		keys.countDups(head, owners, drop, dups)
	}

	out := make([]OrphanFile, 0, len(orphans))
	for i := range orphans {
		// Of paths to one file, the lexically first is listed, wherever the walk met it.
		if drop[i] || (orphans[i].files > 0 && dups[i].files >= orphans[i].files) {
			continue
		}
		orphan := orphans[i]
		orphan.Size -= dups[i].bytes
		out = append(out, orphan)
	}
	return out
}

// inUseAndAliased returns the paths of keys a torrent owns, and the newest ref
// of each other key with more than one path.
func (k *fileKeys) inUseAndAliased() (inUse []string, aliased []int) {
	for key, head := range k.others {
		if _, ok := k.inUse[key]; ok {
			inUse = k.paths(inUse, head)
		} else if k.refs[head].prev >= 0 {
			aliased = append(aliased, head)
		}
	}
	for key, head := range k.linked {
		if _, ok := k.inUse[key]; ok {
			inUse = k.paths(inUse, head)
		}
	}
	return inUse, aliased
}

func (k *fileKeys) paths(paths []string, head int) []string {
	for r := head; r >= 0; r = k.refs[r].prev {
		paths = append(paths, k.refs[r].path)
	}
	return paths
}

// aliasDups counts the files and bytes in one orphan that are a file already
// listed at another path.
type aliasDups struct {
	files int
	bytes int64
}

type listedRef struct {
	orphan int
	path   string
	size   int64
}

// countDups marks every listed path of one key but the lexically first as a
// duplicate in the orphan whose delete removes it. Dropped orphans do not count.
func (k *fileKeys) countDups(head int, owners orphanOwners, drop []bool, dups []aliasDups) {
	var listed []listedRef
	first := -1
	for r := head; r >= 0; r = k.refs[r].prev {
		ref := k.refs[r]
		if ref.size < 0 {
			continue
		}
		i, ok := owners.of(ref.path)
		if !ok || drop[i] {
			continue
		}
		listed = append(listed, listedRef{orphan: i, path: ref.path, size: ref.size})
		if first < 0 || ref.path < listed[first].path {
			first = len(listed) - 1
		}
	}
	for j, ref := range listed {
		if j != first {
			dups[ref.orphan].files++
			dups[ref.orphan].bytes += ref.size
		}
	}
}

// orphanOwners finds the orphan whose delete removes a path: the file itself
// or a disc unit above it.
type orphanOwners struct {
	d     fsops.PathDialect
	units map[string]int
}

func newOrphanOwners(d fsops.PathDialect, orphans []OrphanFile) orphanOwners {
	units := make(map[string]int, len(orphans))
	for i := range orphans {
		units[orphans[i].Path] = i
	}
	return orphanOwners{d: d, units: units}
}

func (o orphanOwners) of(path string) (int, bool) {
	for {
		if i, ok := o.units[path]; ok {
			return i, true
		}
		parent := o.d.Dir(path)
		if parent == path {
			return 0, false
		}
		path = parent
	}
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
		discEntry.files += entry.files
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
	orphans, dirs, _, err := walkScanRootWithUnitFilter(ctx, root, protected, gracePeriod, 0, nil, backend, instance, true, nil)
	return orphans, dirs, err
}

// recordInUse walks a save path that is a symlink through the link and adds
// the identities of the torrent files it finds to keys, so their twins at the
// target count as aliases. It skips no ignore paths, since one could only hide
// files in use.
func recordInUse(ctx context.Context, root string, protected *protectedPaths, backend fsops.Backend, instance *models.Instance, keys *fileKeys) error {
	walkCtx, cancelWalk := context.WithCancel(ctx)
	ch, err := backend.WalkDir(walkCtx, root, fsops.WalkOptions{
		IgnoreDirNames:        ignoredOrphanDirNames,
		IgnoreDirNamePrefixes: ignoredOrphanDirNamePrefixes,
		WantFileID:            true,
		FollowRoot:            true,
	})
	if err != nil {
		cancelWalk()
		return fmt.Errorf("walk %s: %w", root, err)
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
			// As in the scan walk, a cut connection must not read as a denied
			// subtree, or a cut-short tree would protect less than it should.
			// A subtree that vanished during the walk has no files left to
			// protect, and its error must not read as a missing target.
			if errors.Is(entry.Err, fs.ErrPermission) && entry.Path != root &&
				!errors.Is(entry.Err, fsops.ErrConnectionLost) {
				continue
			}
			if errors.Is(entry.Err, fs.ErrNotExist) && entry.Path != root {
				continue
			}
			return entry.Err
		}
		if entry.IsDir || entry.IsSymlink || !protected.torrentFile(entry.Path) {
			continue
		}
		// Without its identity the file's twin at the target would be listed
		// with no walk error. Over sftp no file has one, so the walk goes on.
		if entry.FileIDErr != nil && (instance == nil || models.FilesystemCapabilitiesOf(instance).Identity) {
			return fmt.Errorf("file identity %s: %w", entry.Path, entry.FileIDErr)
		}
		if key, ok := keys.of(entry, instance); ok {
			keys.inUse[key] = struct{}{}
		}
	}
	return ctx.Err()
}

func walkScanRootWithUnitFilter(
	ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
	backend fsops.Backend, instance *models.Instance, collectDirs bool, keys *fileKeys,
) ([]OrphanFile, []AbandonedDir, bool, error) {
	// Without the run's keys this one root is the run, so the walk drops aliases itself.
	ownKeys := keys == nil
	if ownKeys {
		keys = newFileKeys()
	}
	// A save path that is itself a symlink is walked only to learn which files
	// are in use there. Ignore paths, other roots and other torrents are
	// spelled at the target, so nothing is listed through the link.
	if info, err := backend.Lstat(ctx, root); err == nil && info.IsSymlink {
		return nil, nil, false, recordInUse(ctx, root, protected, backend, instance, keys)
	}
	w := newScanWalker(ctx, root, protected, gracePeriod, maxFiles, unitFilter, backend, instance, collectDirs, keys)

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
			// A subtree that vanished during the walk has nothing left to list,
			// and its error must not read as a missing root.
			if errors.Is(entry.Err, fs.ErrNotExist) && entry.Path != root {
				continue
			}
			return nil, nil, false, entry.Err
		}

		// Junctions are ModeIrregular on Windows, not symlinks or directories.
		if entry.IsSymlink || (!entry.IsDir && !entry.Mode.IsRegular()) {
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

		unitPath, isDiscUnit := discOrphanUnitWithContext(ctx, w.root, path, w.protected, w.discUnitCache, w.scanRootCache, w.backend)
		if w.protected.torrentFile(path) {
			if isDiscUnit {
				w.discUnitsInUse[unitPath] = struct{}{}
			}
			if key, ok := w.keys.of(entry, w.instance); ok {
				w.keys.inUse[key] = struct{}{}
			}
			continue
		}
		if entry.StatErr != nil {
			continue
		}
		// Recorded before the grace and name checks: a disc unit's delete removes those files too.
		ref := -1
		if key, ok := w.keys.of(entry, w.instance); ok {
			ref = w.keys.addOther(key, path, entry.Nlinks)
		}

		name := w.d.Base(path)
		if isIgnoredOrphanFileName(name) {
			continue
		}

		if !entry.ModTime.IsZero() && time.Since(entry.ModTime) < w.gracePeriod {
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
		existing.files++
		if ref >= 0 {
			w.keys.refs[ref].size = entry.Size
		}
		if existing.ModifiedAt.IsZero() || entry.ModTime.After(existing.ModifiedAt) {
			existing.ModifiedAt = entry.ModTime
		}
	}

	orphans := w.orphans()
	if ownKeys {
		orphans = dropAliasOrphans(w.d, orphans, keys)
	}
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
func discOrphanUnitWithContext(ctx context.Context, scanRoot, filePath string, protected *protectedPaths, unitCache map[string]discUnitDecision, rootCache map[string]bool, backend fsops.Backend) (unitPath string, ok bool) {
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
	// A disc unit never is or holds a nested scan root: the delete would refuse
	// it, or remove the root with it. The marker is the unit instead, under the
	// ignore check a walk of that root applies, or else its files one by one.
	if cachedHoldsScanRoot(protected, rootCache, candidateAbs) {
		if cachedHoldsScanRoot(protected, rootCache, markerAbs) || protected.ignoredOrHoldsIgnored(markerAbs) {
			return path, false
		}
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

// cachedHoldsScanRoot remembers protected.holdsScanRoot per folder: it
// normalizes the path, and it runs for every file below a disc marker.
func cachedHoldsScanRoot(protected *protectedPaths, cache map[string]bool, dir string) bool {
	if is, ok := cache[dir]; ok {
		return is
	}
	is := protected.holdsScanRoot(dir)
	if cache != nil {
		cache[dir] = is
	}
	return is
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
