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

var ignoredOrphanFileNamePrefixes = []string{
	".fuse",
	".goutputstream-",
	".nfs",
	"._",
	".#",
	"~$",
}

var ignoredOrphanFileNameSuffixes = []string{
	".parts",
	".!qB",
}

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

var ignoredOrphanDirNamePrefixes = []string{
	".Trash-",
	"..",
}

type discUnitDecision struct {
	chosenUnit      string
	disableGrouping bool
}

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
	instance    *models.Instance
	d           fsops.PathDialect

	collectDirs bool
	seenDirs    map[string]*AbandonedDir

	orphanUnits         map[string]*OrphanFile
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
		if dir.directFiles > 0 {
			continue
		}
		if w.protected.torrentFileIn(dir.Path) {
			continue
		}
		dirs = append(dirs, *dir)
	}
	return dirs
}

type fileKeys struct {
	inUse       map[fsops.FileKey]struct{}
	others      map[fsops.FileKey]int
	linked      map[fsops.FileKey]int
	linkedInUse map[fsops.FileKey]int
	relinked    map[fsops.FileKey]struct{}
	refs        []aliasRef
	incomplete  bool
	fileDirs    map[string]struct{}
}

type aliasRef struct {
	path string
	prev int
	size int64
}

func newFileKeys() *fileKeys {
	return &fileKeys{
		inUse:       make(map[fsops.FileKey]struct{}),
		others:      make(map[fsops.FileKey]int),
		linked:      make(map[fsops.FileKey]int),
		linkedInUse: make(map[fsops.FileKey]int),
		relinked:    make(map[fsops.FileKey]struct{}),
		fileDirs:    make(map[string]struct{}),
	}
}

func (k *fileKeys) addInUse(key fsops.FileKey, path string, nlinks uint64) {
	_, seen := k.inUse[key]
	_, linked := k.linkedInUse[key]
	k.inUse[key] = struct{}{}
	if seen && linked != (nlinks > 1) {
		k.relinked[key] = struct{}{}
	}
	if nlinks > 1 {
		k.chain(k.linkedInUse, key, path)
	}
}

func (k *fileKeys) addOther(key fsops.FileKey, path string, nlinks uint64) int {
	if nlinks > 1 {
		return k.chain(k.linked, key, path)
	}
	return k.chain(k.others, key, path)
}

func (k *fileKeys) chain(chains map[fsops.FileKey]int, key fsops.FileKey, path string) int {
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

func dropAliasOrphans(ctx context.Context, backend fsops.Backend, instance *models.Instance, orphans []OrphanFile, keys *fileKeys) []OrphanFile {
	d := backend.Paths()
	inUse, aliased := keys.inUseAndAliased()
	inUse = append(inUse, keys.linkedAliases(ctx, backend, instance)...)
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
		if drop[i] || (orphans[i].files > 0 && dups[i].files >= orphans[i].files) {
			continue
		}
		orphan := orphans[i]
		orphan.Size -= dups[i].bytes
		out = append(out, orphan)
	}
	return out
}

func (k *fileKeys) inUseAndAliased() (inUse []string, aliased []int) {
	for key, head := range k.others {
		if _, ok := k.inUse[key]; ok {
			inUse = k.paths(inUse, head)
		} else if k.refs[head].prev >= 0 {
			aliased = append(aliased, head)
		}
	}
	return inUse, aliased
}

func (k *fileKeys) linkedAliases(ctx context.Context, backend fsops.Backend, instance *models.Instance) []string {
	type match struct{ others, torrents int }
	var (
		inUse   []string
		matches []match
		dirs    []string
	)
	d := backend.Paths()
	for key, head := range k.linked {
		if _, ok := k.inUse[key]; !ok {
			continue
		}
		torrents, ok := k.linkedInUse[key]
		if _, relinked := k.relinked[key]; !ok || relinked {
			inUse = k.paths(inUse, head)
			continue
		}
		matches = append(matches, match{head, torrents})
		for _, h := range []int{head, torrents} {
			for r := h; r >= 0; r = k.refs[r].prev {
				dirs = append(dirs, d.Dir(k.refs[r].path))
			}
		}
	}
	if len(matches) == 0 {
		return inUse
	}
	ids := readDirIDs(ctx, backend, instance, dirs)
	for _, m := range matches {
		for r := m.others; r >= 0; r = k.refs[r].prev {
			for t := m.torrents; t >= 0; t = k.refs[t].prev {
				if sameEntry(d, ids, k.refs[r].path, k.refs[t].path) {
					inUse = append(inUse, k.refs[r].path)
					break
				}
			}
		}
	}
	return inUse
}

func (k *fileKeys) paths(paths []string, head int) []string {
	for r := head; r >= 0; r = k.refs[r].prev {
		paths = append(paths, k.refs[r].path)
	}
	return paths
}

type aliasDups struct {
	files int
	bytes int64
}

type listedRef struct {
	orphan int
	path   string
	size   int64
}

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

func outermostDiscUnit(d fsops.PathDialect, normUnit string, discRoots map[string]string) (string, bool) {
	outermost := ""
	sep := d.Separator()
	for dir, _, ok := strings.CutLast(normUnit, sep); ok && dir != ""; dir, _, ok = strings.CutLast(dir, sep) {
		if discUnitPath, ok := discRoots[dir]; ok {
			outermost = discUnitPath
		}
	}
	return outermost, outermost != ""
}

func (w *scanWalker) resolveDiscUnits() {
	for du := range w.discUnitsWithOrphan {
		if w.protected.torrentFileIn(du) {
			w.discUnitsInUse[du] = struct{}{}
		}
	}
	for du := range w.discUnitsInUse {
		delete(w.orphanUnits, du)
	}

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

func walkScanRootCollectingDirs(ctx context.Context, root string, protected *protectedPaths,
	gracePeriod time.Duration, backend fsops.Backend, instance *models.Instance,
) ([]OrphanFile, []AbandonedDir, error) {
	orphans, dirs, _, err := walkScanRootWithUnitFilter(ctx, root, protected, gracePeriod, 0, nil, backend, instance, true, nil)
	return orphans, dirs, err
}

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
		for range ch {
		}
	}()
	for entry := range ch {
		if ctx.Err() != nil {
			break
		}
		if entry.Err != nil {
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
		keys.fileDirs[backend.Paths().Dir(entry.Path)] = struct{}{}
		if entry.FileIDErr != nil && (instance == nil || models.FilesystemCapabilitiesOf(instance).Identity) {
			return fmt.Errorf("file identity %s: %w", entry.Path, entry.FileIDErr)
		}
		if key, ok := keys.of(entry, instance); ok {
			keys.addInUse(key, entry.Path, entry.Nlinks)
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
	ownKeys := keys == nil
	if ownKeys {
		keys = newFileKeys()
	}
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
		for range ch {
		}
	}()

	for entry := range ch {
		if ctx.Err() != nil {
			break
		}

		if entry.Err != nil {
			if errors.Is(entry.Err, fs.ErrPermission) && entry.Path != root &&
				!errors.Is(entry.Err, fsops.ErrConnectionLost) {
				continue
			}
			if errors.Is(entry.Err, fs.ErrNotExist) && entry.Path != root {
				continue
			}
			return nil, nil, false, entry.Err
		}

		if entry.IsSymlink || (!entry.IsDir && !entry.Mode.IsRegular()) {
			continue
		}

		if entry.IsDir {
			if w.collectDirs && entry.Path != w.root {
				w.seenDirs[entry.Path] = &AbandonedDir{Path: entry.Path, ModTime: entry.ModTime}
			}
			continue
		}

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
			w.keys.fileDirs[w.d.Dir(path)] = struct{}{}
			if key, ok := w.keys.of(entry, w.instance); ok {
				w.keys.addInUse(key, path, entry.Nlinks)
			}
			continue
		}
		if entry.StatErr != nil {
			continue
		}
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
		orphans = dropAliasOrphans(ctx, backend, instance, orphans, keys)
	}
	return orphans, w.candidateDirs(orphans), w.truncated, ctx.Err()
}

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

func buildDiscCandidatePaths(d fsops.PathDialect, root string, segments []string, markerIndex int, markerSegment string) (candidateAbs, markerAbs string) {
	var unitRel, markerRel string
	if markerIndex == 0 {
		unitRel = markerSegment
		markerRel = markerSegment
	} else {
		unitRel = strings.Join(segments[:markerIndex], d.Separator())
		markerRel = d.Join(unitRel, markerSegment)
	}
	candidateAbs = d.Clean(d.Join(root, unitRel))
	markerAbs = d.Clean(d.Join(root, markerRel))
	return candidateAbs, markerAbs
}

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

func isPathUnderNormalized(d fsops.PathDialect, child, parent string) bool {
	if len(child) <= len(parent) || !strings.HasPrefix(child, parent) {
		return false
	}
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
