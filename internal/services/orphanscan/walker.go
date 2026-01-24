// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
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
// Only files are returned as orphans - directories are cleaned up separately after file deletion.
func walkScanRoot(ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int) ([]OrphanFile, bool, error) {
	return walkScanRootWithUnitFilter(ctx, root, tfm, ignorePaths, gracePeriod, maxFiles, nil)
}

// walkScanRootDiscUnits walks a directory tree and returns only disc-layout orphan units.
// This is intended for diagnostics/local tests to avoid materializing a huge orphan list.
func walkScanRootDiscUnits(
	ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxUnits int,
) ([]OrphanFile, bool, error) {
	return walkScanRootWithUnitFilter(ctx, root, tfm, ignorePaths, gracePeriod, maxUnits, func(_ string, isDiscUnit bool) bool {
		return isDiscUnit
	})
}

type scanWalker struct {
	ctx         context.Context
	root        string
	tfm         *TorrentFileMap
	ignorePaths []string
	gracePeriod time.Duration
	maxFiles    int
	unitFilter  func(unitPath string, isDiscUnit bool) bool

	orphanUnits    map[string]*OrphanFile
	discUnitsInUse map[string]struct{}
	discUnitCache  map[string]discUnitDecision
	discUnitPaths  map[string]struct{}
	seenInodes     map[inodeKey]struct{}
	truncated      bool
}

func newScanWalker(
	ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
) *scanWalker {
	return &scanWalker{
		ctx:            ctx,
		root:           root,
		tfm:            tfm,
		ignorePaths:    ignorePaths,
		gracePeriod:    gracePeriod,
		maxFiles:       maxFiles,
		unitFilter:     unitFilter,
		orphanUnits:    make(map[string]*OrphanFile),
		discUnitsInUse: make(map[string]struct{}),
		discUnitCache:  make(map[string]discUnitDecision),
		discUnitPaths:  make(map[string]struct{}),
		seenInodes:     make(map[inodeKey]struct{}),
	}
}

type inodeKey struct {
	dev uint64
	ino uint64
}

func (w *scanWalker) shouldSkipDuplicate(info fs.FileInfo) bool {
	key, nlink, ok := inodeKeyFromInfo(info)
	if !ok || nlink > 1 {
		return false
	}
	if _, exists := w.seenInodes[key]; exists {
		return true
	}
	w.seenInodes[key] = struct{}{}
	return false
}

func (w *scanWalker) walk(path string, d fs.DirEntry, walkErr error) error {
	if err := w.checkCanceled(); err != nil {
		return err
	}
	if walkErr != nil {
		// WalkDir can call the callback with a nil DirEntry (e.g. root stat failure),
		// so we must handle errors before touching d.
		return w.handleWalkErr(walkErr)
	}
	if d == nil {
		return nil
	}
	if d.Type()&fs.ModeSymlink != 0 {
		return nil
	}
	if d.IsDir() {
		return w.handleDir(path, d)
	}
	return w.handleFile(path, d)
}

func (w *scanWalker) checkCanceled() error {
	select {
	case <-w.ctx.Done():
		return fmt.Errorf("walk canceled: %w", w.ctx.Err())
	default:
		return nil
	}
}

func (w *scanWalker) handleWalkErr(walkErr error) error {
	if walkErr == nil {
		return nil
	}
	if os.IsPermission(walkErr) {
		return nil
	}
	return walkErr
}

func (w *scanWalker) handleDir(path string, d fs.DirEntry) error {
	if isIgnoredPath(path, w.ignorePaths) {
		return fs.SkipDir
	}
	if isIgnoredOrphanDirName(d.Name()) {
		return fs.SkipDir
	}
	return nil
}

func (w *scanWalker) handleFile(path string, d fs.DirEntry) error {
	if isIgnoredPath(path, w.ignorePaths) {
		return nil
	}

	unitPath, isDiscUnit := discOrphanUnitWithContext(w.root, path, w.tfm, w.discUnitCache, w.ignorePaths)
	normPath := normalizePath(path)
	if w.tfm.Has(normPath) {
		w.markInUse(unitPath, isDiscUnit)
		if info, infoErr := d.Info(); infoErr == nil {
			w.shouldSkipDuplicate(info)
		}
		return nil
	}
	if isIgnoredOrphanFileName(d.Name()) {
		return nil
	}

	info, infoErr := d.Info()
	if infoErr != nil {
		return nil //nolint:nilerr // best-effort scan: ignore stat failures
	}
	if time.Since(info.ModTime()) < w.gracePeriod {
		return nil
	}
	if w.shouldSkipDuplicate(info) {
		return nil
	}
	if isDiscUnit {
		if w.isDiscUnitInUse(unitPath) {
			return nil
		}
		w.discUnitPaths[unitPath] = struct{}{}
	}
	if w.unitFilter != nil && !w.unitFilter(unitPath, isDiscUnit) {
		return nil
	}

	return w.addOrUpdateUnit(unitPath, info)
}

func (w *scanWalker) markInUse(unitPath string, isDiscUnit bool) {
	if !isDiscUnit {
		return
	}
	w.discUnitsInUse[unitPath] = struct{}{}
	delete(w.orphanUnits, unitPath)
}

func (w *scanWalker) isDiscUnitInUse(unitPath string) bool {
	_, ok := w.discUnitsInUse[unitPath]
	return ok
}

func (w *scanWalker) addOrUpdateUnit(unitPath string, info fs.FileInfo) error {
	entry, exists := w.orphanUnits[unitPath]
	if !exists {
		if w.maxFiles > 0 && len(w.orphanUnits) >= w.maxFiles {
			w.truncated = true
			return fs.SkipAll
		}
		entry = &OrphanFile{Path: unitPath, Status: FileStatusPending}
		w.orphanUnits[unitPath] = entry
	}

	entry.Size += info.Size()
	if entry.ModifiedAt.IsZero() || info.ModTime().After(entry.ModifiedAt) {
		entry.ModifiedAt = info.ModTime()
	}
	return nil
}

func containingDiscUnit(normUnit string, discRoots map[string]string) (string, bool) {
	for normDisc, discUnitPath := range discRoots {
		if normUnit == normDisc {
			continue
		}
		if isPathUnderNormalized(normUnit, normDisc) {
			return discUnitPath, true
		}
	}
	return "", false
}

func (w *scanWalker) mergeSuppressedUnitsIntoDiscUnits() {
	if len(w.discUnitPaths) == 0 {
		return
	}

	discRoots := make(map[string]string, len(w.discUnitPaths))
	for du := range w.discUnitPaths {
		discRoots[normalizePath(du)] = du
	}

	for unit, entry := range w.orphanUnits {
		discUnitPath, ok := containingDiscUnit(normalizePath(unit), discRoots)
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

func walkScanRootWithUnitFilter(
	ctx context.Context, root string, tfm *TorrentFileMap,
	ignorePaths []string, gracePeriod time.Duration, maxFiles int,
	unitFilter func(unitPath string, isDiscUnit bool) bool,
) ([]OrphanFile, bool, error) {
	w := newScanWalker(ctx, root, tfm, ignorePaths, gracePeriod, maxFiles, unitFilter)
	err := filepath.WalkDir(root, w.walk)
	return w.orphans(), w.truncated, err
}

// discOrphanUnit detects whether a file path belongs to a disc-layout folder.
// If so, it returns the deletion unit path (directory) that should represent the disc.
//
// Rules:
//   - Detects BDMV and VIDEO_TS directory markers (case-insensitive) anywhere in the path.
//   - Prefers the parent directory above the marker as the unit root.
//   - If the marker is directly under the scan root, the unit becomes the marker directory itself
//     (to avoid attempting to delete the scan root).
func discOrphanUnit(scanRoot, filePath string, cache map[string]discUnitDecision) (unitPath string, ok bool) {
	// Backwards-compatible wrapper (used only by local diagnostic code).
	return discOrphanUnitWithContext(scanRoot, filePath, nil, cache, nil)
}

// findDiscMarker scans path segments for a disc-layout marker (BDMV, VIDEO_TS).
// Returns the marker index, actual on-disk segment name, and uppercase marker.
func findDiscMarker(segments []string) (markerIndex int, markerSegment, markerUpper string, found bool) {
	for i, seg := range segments {
		segUpper := strings.ToUpper(seg)
		if slices.Contains(discLayoutMarkers, segUpper) {
			return i, seg, segUpper, true
		}
	}
	return -1, "", "", false
}

// buildDiscCandidatePaths builds the candidate parent and marker absolute paths.
func buildDiscCandidatePaths(root string, segments []string, markerIndex int, markerSegment string) (candidateAbs, markerAbs string) {
	var unitRel, markerRel string
	if markerIndex == 0 {
		unitRel = markerSegment
		markerRel = markerSegment
	} else {
		unitRel = filepath.Join(segments[:markerIndex]...)
		if unitRel == "." || unitRel == "" {
			unitRel = markerSegment
			markerRel = markerSegment
		} else {
			markerRel = filepath.Join(unitRel, markerSegment)
		}
	}
	candidateAbs = filepath.Clean(filepath.Join(root, unitRel))
	markerAbs = filepath.Clean(filepath.Join(root, markerRel))
	return candidateAbs, markerAbs
}

// chooseDiscUnit decides whether to use the parent folder, marker folder, or disable grouping.
func chooseDiscUnit(candidateAbs, markerAbs, markerUpper string, tfm *TorrentFileMap, ignorePaths []string) discUnitDecision {
	parentProtected := len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(candidateAbs, ignorePaths)
	markerProtected := len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(markerAbs, ignorePaths)

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
	if discParentIsSafeDiscRoot(candidateAbs, markerUpper, tfm) {
		return discUnitDecision{chosenUnit: candidateAbs}
	}
	return discUnitDecision{chosenUnit: markerAbs}
}

// discOrphanUnitWithContext detects whether a file path belongs to a disc-layout folder.
// If so, it returns the deletion unit path (directory) that should represent the disc.
//
// Note: sibling orphan files are suppressed at the end of the scan if a parent disc unit is chosen.
// Respects ignorePaths to prevent grouping that would delete ignored content.
func discRelativeSegments(root, path string) ([]string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, false
	}

	relDir := filepath.Dir(rel)
	if relDir == "." {
		return nil, false
	}

	return strings.Split(relDir, string(filepath.Separator)), true
}

func discUnitFromParentMarker(
	originalPath, candidateAbs, markerAbs, markerUpper string,
	tfm *TorrentFileMap,
	unitCache map[string]discUnitDecision,
	ignorePaths []string,
) (unitPath string, ok bool) {
	key := normalizePath(candidateAbs) + "|" + markerUpper
	if unitCache != nil {
		if decision, ok := unitCache[key]; ok {
			if decision.disableGrouping {
				return originalPath, false
			}
			return decision.chosenUnit, true
		}
	}

	decision := chooseDiscUnit(candidateAbs, markerAbs, markerUpper, tfm, ignorePaths)
	if unitCache != nil {
		unitCache[key] = decision
	}
	if decision.disableGrouping {
		return originalPath, false
	}
	return decision.chosenUnit, true
}

func discOrphanUnitWithContext(scanRoot, filePath string, tfm *TorrentFileMap, unitCache map[string]discUnitDecision, ignorePaths []string) (unitPath string, ok bool) {
	root := filepath.Clean(scanRoot)
	path := filepath.Clean(filePath)

	segments, ok := discRelativeSegments(root, path)
	if !ok {
		return path, false
	}

	markerIndex, markerSegment, markerUpper, found := findDiscMarker(segments)
	if !found {
		return path, false
	}

	candidateAbs, markerAbs := buildDiscCandidatePaths(root, segments, markerIndex, markerSegment)
	if candidateAbs == root {
		return markerAbs, true
	}

	if markerIndex > 0 {
		return discUnitFromParentMarker(path, candidateAbs, markerAbs, markerUpper, tfm, unitCache, ignorePaths)
	}

	if len(ignorePaths) > 0 && isPathProtectedByIgnorePaths(markerAbs, ignorePaths) {
		return path, false
	}

	return markerAbs, true
}

// isPathUnderNormalized checks if child is strictly under parent.
// Both paths must already be normalized via normalizePath.
func isPathUnderNormalized(child, parent string) bool {
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

func discParentIsPureDiscRoot(parentAbs, marker string) bool {
	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		return false
	}

	allowedDirs, allowedFiles := discAllowedNames(marker)
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

func discParentIsSafeDiscRoot(parentAbs, marker string, tfm *TorrentFileMap) bool {
	if tfm == nil {
		return discParentIsPureDiscRoot(parentAbs, marker)
	}

	entries, err := os.ReadDir(parentAbs)
	if err != nil {
		return false
	}

	allowedDirs, allowedFiles := discAllowedNames(marker)
	for _, e := range entries {
		nameUpper := strings.ToUpper(e.Name())
		full := filepath.Join(parentAbs, e.Name())
		if e.IsDir() {
			if _, ok := allowedDirs[nameUpper]; ok {
				continue
			}
			if tfm.HasAnyInDir(normalizePath(full)) {
				return false
			}
			continue
		}
		if _, ok := allowedFiles[nameUpper]; ok {
			continue
		}
		if tfm.Has(normalizePath(full)) {
			return false
		}
	}

	return true
}

// isIgnoredPath checks if path matches any ignore prefix with boundary safety.
// Ensures /data/foo doesn't match /data/foobar (requires separator after prefix).
// Uses normalizePath for consistent comparison across platforms (handles Windows casing).
func isIgnoredPath(path string, ignorePaths []string) bool {
	normPath := normalizePath(path)
	for _, prefix := range ignorePaths {
		normPrefix := normalizePath(prefix)
		if normPath == normPrefix {
			return true
		}
		if strings.HasPrefix(normPath, normPrefix) {
			// Ensure match is at path boundary
			if len(normPath) > len(normPrefix) && normPath[len(normPrefix)] == filepath.Separator {
				return true
			}
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

func isIgnoredOrphanDirName(name string) bool {
	for _, exact := range ignoredOrphanDirNames {
		if strings.EqualFold(name, exact) {
			return true
		}
	}
	for _, prefix := range ignoredOrphanDirNamePrefixes {
		if hasPrefixFold(name, prefix) {
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
func isPathProtectedByIgnorePaths(path string, ignorePaths []string) bool {
	// Check if the path itself is ignored
	if isIgnoredPath(path, ignorePaths) {
		return true
	}

	// Check if any ignore path is a descendant of this path
	normPath := normalizePath(path)
	for _, ignorePath := range ignorePaths {
		normIgnore := normalizePath(ignorePath)
		// Skip if ignore path is the same as the target
		if normIgnore == normPath {
			continue
		}
		// Check if ignore path is under this path (boundary-safe)
		if strings.HasPrefix(normIgnore, normPath) {
			if len(normIgnore) > len(normPath) && normIgnore[len(normPath)] == filepath.Separator {
				return true
			}
		}
	}
	return false
}

// NormalizeIgnorePaths validates and normalizes ignore paths.
// All paths must be absolute. Uses normalizePath for consistency (Windows casing).
func NormalizeIgnorePaths(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned := filepath.Clean(p)
		if !filepath.IsAbs(cleaned) {
			return nil, fmt.Errorf("ignore path must be absolute: %s", p)
		}
		result = append(result, normalizePath(cleaned))
	}
	return result, nil
}
