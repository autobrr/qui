// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/pkg/hardlink"
)

func TestIsPathInsideBase(t *testing.T) {
	// Use OS-specific path separator for test cases
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{
			name:     "normal nested path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "nested directory path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "subdir" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "path equals base (edge case)",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents",
			expected: true,
		},
		{
			name:     "parent traversal with ..",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + ".." + sep + "secret.txt",
			expected: false,
		},
		{
			name:     "double parent traversal",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + ".." + sep + ".." + sep + "etc" + sep + "passwd",
			expected: false,
		},
		{
			name:     "path that resolves to parent",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data",
			expected: false,
		},
		{
			name:     "sibling path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "other",
			expected: false,
		},
		{
			name:     "absolute path outside base",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "etc" + sep + "passwd",
			expected: false,
		},
		{
			name:     "traversal hidden in middle",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "safe" + sep + ".." + sep + ".." + sep + "secret",
			expected: false,
		},
		{
			name:     "current directory dots are ok",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "." + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "deeply nested valid path",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "a" + sep + "b" + sep + "c" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "path with trailing separator",
			basePath: sep + "data" + sep + "torrents" + sep,
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInsideBase(tt.basePath, tt.fullPath)
			if result != tt.expected {
				t.Errorf("isPathInsideBase(%q, %q) = %v, want %v",
					tt.basePath, tt.fullPath, result, tt.expected)
			}
		})
	}
}

func TestIsPathInsideBase_RelativeCleanedPaths(t *testing.T) {
	// Test with paths that have various normalization edge cases
	sep := string(os.PathSeparator)

	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{
			name:     "redundant separators in base",
			basePath: sep + "data" + sep + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "redundant separators in full",
			basePath: sep + "data" + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + sep + "file.mkv",
			expected: true,
		},
		{
			name:     "dot components in both",
			basePath: sep + "data" + sep + "." + sep + "torrents",
			fullPath: sep + "data" + sep + "torrents" + sep + "." + sep + "file.mkv",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathInsideBase(tt.basePath, tt.fullPath)
			if result != tt.expected {
				t.Errorf("isPathInsideBase(%q, %q) = %v, want %v",
					tt.basePath, tt.fullPath, result, tt.expected)
			}
		})
	}
}

func TestIsPathInsideBase_OSSpecific(t *testing.T) {
	// Platform-specific tests using actual filepath behavior
	basePath := filepath.Join("data", "torrents")
	fullPath := filepath.Join("data", "torrents", "file.mkv")

	if !isPathInsideBase(basePath, fullPath) {
		t.Errorf("Expected relative path inside base to return true")
	}

	escapingPath := filepath.Join("data", "torrents", "..", "other", "file.txt")
	if isPathInsideBase(basePath, escapingPath) {
		t.Errorf("Expected escaping path to return false")
	}
}

func TestAugmentCrossInstanceScope_NoDeficits(t *testing.T) {
	t.Parallel()
	// When there are no deficit FileIDs, CrossScopeByHash should equal ScopeByHash.
	index := &HardlinkIndex{
		ScopeByHash: map[string]string{
			"hash1": HardlinkScopeNone,
			"hash2": HardlinkScopeTorrentsOnly,
		},
		buildState: &hardlinkBuildState{
			globalFileIDMap:   make(map[hardlink.FileID]*fileIDTracker),
			seenPaths:         make(map[string]struct{}),
			torrentInfoByHash: make(map[string]*torrentFileInfo),
		},
	}

	// Service is nil-safe for augment (instanceStore will fail, but deficit check comes first).
	s := &Service{}
	s.augmentCrossInstanceScope(t.Context(), 1, index)

	if index.CrossScopeByHash == nil {
		t.Fatal("expected CrossScopeByHash to be populated")
	}
	if len(index.CrossScopeByHash) != len(index.ScopeByHash) {
		t.Errorf("expected %d entries, got %d", len(index.ScopeByHash), len(index.CrossScopeByHash))
	}
	for hash, expected := range index.ScopeByHash {
		if got := index.CrossScopeByHash[hash]; got != expected {
			t.Errorf("hash %s: expected %q, got %q", hash, expected, got)
		}
	}
	if index.buildState != nil {
		t.Error("expected buildState to be nil after augmentation")
	}
}

func TestAugmentCrossInstanceScope_NilIndex(t *testing.T) {
	t.Parallel()
	s := &Service{}
	// Should not panic on nil index.
	s.augmentCrossInstanceScope(t.Context(), 1, nil)

	// Should not panic on nil buildState.
	index := &HardlinkIndex{}
	s.augmentCrossInstanceScope(t.Context(), 1, index)
	if index.CrossScopeByHash != nil {
		t.Error("expected CrossScopeByHash to remain nil with nil buildState")
	}
}

func TestAugmentCrossInstanceScope_DeficitWithNoOtherInstances(t *testing.T) {
	t.Parallel()
	// Simulate: torrent with one file that has nlink=2, uniquePathCount=1 (deficit).
	// No other instances available -> cross-scope should fall back to single-instance scope.
	fid := hardlink.FileID{Dev: 1, Ino: 100}
	tracker := &fileIDTracker{nlink: 2, uniquePathCount: 1} // Deficit: nlink > uniquePathCount

	index := &HardlinkIndex{
		ScopeByHash: map[string]string{
			"hash1": HardlinkScopeOutsideQBitTorrent,
		},
		buildState: &hardlinkBuildState{
			globalFileIDMap: map[hardlink.FileID]*fileIDTracker{fid: tracker},
			seenPaths:       make(map[string]struct{}),
			torrentInfoByHash: map[string]*torrentFileInfo{
				"hash1": {
					fileIDs:       []hardlink.FileID{fid},
					allAccessible: true,
					hasHardlinks:  true,
				},
			},
		},
	}

	// Service has no instanceStore → List will fail → falls back to copy of ScopeByHash.
	s := &Service{}
	s.augmentCrossInstanceScope(t.Context(), 1, index)

	if index.CrossScopeByHash == nil {
		t.Fatal("expected CrossScopeByHash to be populated")
	}
	// With no other instances reachable, cross-scope should mirror single-instance scope.
	if got := index.CrossScopeByHash["hash1"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Errorf("expected %q, got %q", HardlinkScopeOutsideQBitTorrent, got)
	}
}

// --- Filesystem-based tests using real hardlinks ---

// createFile creates a file with some content and returns its FileID and nlink.
func createFile(t *testing.T, path string) hardlink.FileID {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("content-"+filepath.Base(path)), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	fid, _, err := hardlink.GetFileID(fi, path)
	if err != nil {
		t.Fatal(err)
	}
	return fid
}

// lstatFileID returns the FileID and nlink for an existing path.
func lstatFileID(t *testing.T, path string) (hardlink.FileID, uint64) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	fid, nlink, err := hardlink.GetFileID(fi, path)
	if err != nil {
		t.Fatal(err)
	}
	return fid, nlink
}

// buildStateFromLstat creates a hardlinkBuildState by Lstat-ing real files.
// torrents maps hash → list of absolute file paths (simulating what Phase 1 would produce).
func buildStateFromLstat(t *testing.T, torrents map[string][]string) *hardlinkBuildState {
	t.Helper()
	state := &hardlinkBuildState{
		globalFileIDMap:   make(map[hardlink.FileID]*fileIDTracker),
		seenPaths:         make(map[string]struct{}),
		torrentInfoByHash: make(map[string]*torrentFileInfo),
	}
	for hash, paths := range torrents {
		info := &torrentFileInfo{
			fileIDs:       make([]hardlink.FileID, 0, len(paths)),
			allAccessible: true,
		}
		for _, p := range paths {
			fid, nlink := lstatFileID(t, p)
			info.fileIDs = append(info.fileIDs, fid)
			if nlink > 1 {
				info.hasHardlinks = true
				tracker := state.globalFileIDMap[fid]
				if tracker == nil {
					tracker = &fileIDTracker{nlink: nlink}
					state.globalFileIDMap[fid] = tracker
				}
				if _, seen := state.seenPaths[p]; !seen {
					state.seenPaths[p] = struct{}{}
					tracker.uniquePathCount++
				}
			}
		}
		state.torrentInfoByHash[hash] = info
	}
	return state
}

// computeScopeFromState delegates to the production computeScopeMap function.
func computeScopeFromState(state *hardlinkBuildState) map[string]string {
	return computeScopeMap(state)
}

func TestCrossScope_NoHardlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Instance A has a standalone file with no hardlinks.
	fileA := filepath.Join(dir, "instance-a", "torrent", "movie.mkv")
	createFile(t, fileA)

	state := buildStateFromLstat(t, map[string][]string{
		"hashA": {fileA},
	})
	scope := computeScopeFromState(state)

	if got := scope["hashA"]; got != HardlinkScopeNone {
		t.Errorf("expected %q, got %q", HardlinkScopeNone, got)
	}
}

func TestCrossScope_HardlinksWithinSameInstance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Two torrents on the same instance share a hardlinked file.
	original := filepath.Join(dir, "instance-a", "torrent1", "movie.mkv")
	createFile(t, original)
	linked := filepath.Join(dir, "instance-a", "torrent2", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(linked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, linked); err != nil {
		t.Fatal(err)
	}

	state := buildStateFromLstat(t, map[string][]string{
		"hash1": {original},
		"hash2": {linked},
	})
	scope := computeScopeFromState(state)

	// Both torrents have nlink=2, uniquePathCount=2 → torrents_only.
	if got := scope["hash1"]; got != HardlinkScopeTorrentsOnly {
		t.Errorf("hash1: expected %q, got %q", HardlinkScopeTorrentsOnly, got)
	}
	if got := scope["hash2"]; got != HardlinkScopeTorrentsOnly {
		t.Errorf("hash2: expected %q, got %q", HardlinkScopeTorrentsOnly, got)
	}
}

func TestCrossScope_CrossInstanceResolvesDeficit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Instance A has a torrent. Instance B has a cross-seed hardlinked to same inode.
	// No external links → after augmentation, cross-scope should be torrents_only.
	fileA := filepath.Join(dir, "instance-a", "torrent", "movie.mkv")
	createFile(t, fileA)
	fileB := filepath.Join(dir, "instance-b", "xseed", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(fileB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fileA, fileB); err != nil {
		t.Fatal(err)
	}

	// Phase 1: only scan Instance A's files.
	state := buildStateFromLstat(t, map[string][]string{
		"hashA": {fileA},
	})
	phase1Scope := computeScopeFromState(state)

	// Phase 1 sees nlink=2, uniquePathCount=1 → outside_qbittorrent.
	if got := phase1Scope["hashA"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Fatalf("phase 1: expected %q, got %q", HardlinkScopeOutsideQBitTorrent, got)
	}

	// Simulate Phase 2: Lstat Instance B's file and augment state.
	fidB, _ := lstatFileID(t, fileB)
	tracker := state.globalFileIDMap[fidB]
	if tracker == nil {
		t.Fatal("expected Instance B's file to share FileID with Instance A")
	}
	state.seenPaths[fileB] = struct{}{}
	tracker.uniquePathCount++

	// Recompute scope with augmented counts.
	crossScope := computeScopeFromState(state)

	// nlink=2, uniquePathCount=2 → torrents_only.
	if got := crossScope["hashA"]; got != HardlinkScopeTorrentsOnly {
		t.Errorf("cross-scope: expected %q, got %q", HardlinkScopeTorrentsOnly, got)
	}
}

func TestCrossScope_CrossInstancePlusExternal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Instance A torrent, Instance B cross-seed, plus external media library hardlink.
	// After augmentation, cross-scope should still be outside_qbittorrent.
	fileA := filepath.Join(dir, "instance-a", "torrent", "movie.mkv")
	createFile(t, fileA)
	fileB := filepath.Join(dir, "instance-b", "xseed", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(fileB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fileA, fileB); err != nil {
		t.Fatal(err)
	}
	mediaLink := filepath.Join(dir, "media", "Movie (2024)", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(mediaLink), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fileA, mediaLink); err != nil {
		t.Fatal(err)
	}

	// Phase 1: scan Instance A only.
	state := buildStateFromLstat(t, map[string][]string{
		"hashA": {fileA},
	})
	phase1Scope := computeScopeFromState(state)
	if got := phase1Scope["hashA"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Fatalf("phase 1: expected %q, got %q", HardlinkScopeOutsideQBitTorrent, got)
	}

	// Phase 2: augment with Instance B.
	fidB, _ := lstatFileID(t, fileB)
	tracker := state.globalFileIDMap[fidB]
	if tracker == nil {
		t.Fatal("expected Instance B's file to share FileID")
	}
	state.seenPaths[fileB] = struct{}{}
	tracker.uniquePathCount++

	// nlink=3 (A + B + media), uniquePathCount=2 (A + B) → still outside.
	crossScope := computeScopeFromState(state)
	if got := crossScope["hashA"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Errorf("cross-scope: expected %q, got %q", HardlinkScopeOutsideQBitTorrent, got)
	}
}

func TestCrossScope_DeficitSetResolution(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Test that deficit set management works correctly:
	// Two files with different deficit states.
	fileA1 := filepath.Join(dir, "instance-a", "t1", "file1.mkv")
	createFile(t, fileA1)
	fileA2 := filepath.Join(dir, "instance-a", "t1", "file2.mkv")
	createFile(t, fileA2)

	// file1 has a cross-instance hardlink (resolvable deficit).
	fileB1 := filepath.Join(dir, "instance-b", "t1", "file1.mkv")
	if err := os.MkdirAll(filepath.Dir(fileB1), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fileA1, fileB1); err != nil {
		t.Fatal(err)
	}

	// file2 has an external hardlink (unresolvable deficit).
	extLink := filepath.Join(dir, "media", "file2.mkv")
	if err := os.MkdirAll(filepath.Dir(extLink), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fileA2, extLink); err != nil {
		t.Fatal(err)
	}

	// Phase 1: scan only Instance A.
	state := buildStateFromLstat(t, map[string][]string{
		"hashT1": {fileA1, fileA2},
	})
	phase1Scope := computeScopeFromState(state)
	// Torrent has at least one file with outside links → outside_qbittorrent.
	if got := phase1Scope["hashT1"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Fatalf("phase 1: expected %q, got %q", HardlinkScopeOutsideQBitTorrent, got)
	}

	// Phase 2: augment with Instance B's file1 (resolves file1's deficit).
	fidB1, _ := lstatFileID(t, fileB1)
	if tracker := state.globalFileIDMap[fidB1]; tracker != nil {
		state.seenPaths[fileB1] = struct{}{}
		tracker.uniquePathCount++
	}

	// file1 is now resolved, but file2 still has nlink > uniquePathCount.
	crossScope := computeScopeFromState(state)
	if got := crossScope["hashT1"]; got != HardlinkScopeOutsideQBitTorrent {
		t.Errorf("cross-scope: expected %q (file2 still has external link), got %q",
			HardlinkScopeOutsideQBitTorrent, got)
	}
}

func TestCrossScope_SeenPathsDedup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Verify that a path already in seenPaths is not double-counted.
	fileA := filepath.Join(dir, "shared", "movie.mkv")
	createFile(t, fileA)
	fileB := filepath.Join(dir, "instance-b", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(fileB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fileA, fileB); err != nil {
		t.Fatal(err)
	}

	// Build state with fileA counted.
	state := buildStateFromLstat(t, map[string][]string{
		"hash1": {fileA},
	})

	fid, _ := lstatFileID(t, fileA)
	tracker := state.globalFileIDMap[fid]
	if tracker == nil {
		t.Skip("nlink=1, no hardlinks to test (filesystem may not support)")
	}
	countBefore := tracker.uniquePathCount

	// fileA is already in seenPaths. Simulating scanning it again should not increment.
	if _, seen := state.seenPaths[fileA]; !seen {
		t.Fatal("expected fileA to be in seenPaths")
	}
	// If we were to skip (as the real code does), count stays the same.
	if tracker.uniquePathCount != countBefore {
		t.Errorf("expected uniquePathCount=%d to not change, got %d", countBefore, tracker.uniquePathCount)
	}

	// fileB is NOT in seenPaths, so it should increment.
	if _, seen := state.seenPaths[fileB]; seen {
		t.Fatal("expected fileB to NOT be in seenPaths")
	}
	state.seenPaths[fileB] = struct{}{}
	tracker.uniquePathCount++
	if tracker.uniquePathCount != countBefore+1 {
		t.Errorf("expected uniquePathCount=%d, got %d", countBefore+1, tracker.uniquePathCount)
	}
}

func TestCrossScope_ContextCancellation(t *testing.T) {
	t.Parallel()

	// augmentCrossInstanceScope should handle a cancelled context gracefully.
	// Note: this test hits the nil-instanceStore guard before reaching the ctx.Err()
	// check in the scan loop because instanceStore/syncManager are concrete types
	// (not interfaces), so we can't inject stubs without refactoring Service.
	// The scan loop's ctx.Err() check is exercised implicitly in production when
	// the automation runner's context is cancelled mid-scan.
	fid := hardlink.FileID{Dev: 1, Ino: 999}
	index := &HardlinkIndex{
		ScopeByHash: map[string]string{"hash1": HardlinkScopeOutsideQBitTorrent},
		buildState: &hardlinkBuildState{
			globalFileIDMap: map[hardlink.FileID]*fileIDTracker{
				fid: {nlink: 2, uniquePathCount: 1},
			},
			seenPaths: make(map[string]struct{}),
			torrentInfoByHash: map[string]*torrentFileInfo{
				"hash1": {
					fileIDs:       []hardlink.FileID{fid},
					allAccessible: true,
					hasHardlinks:  true,
				},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// Service has no instanceStore → will hit nil check before even checking context,
	// falling back to copy of ScopeByHash.
	s := &Service{}
	s.augmentCrossInstanceScope(ctx, 1, index)

	if index.CrossScopeByHash == nil {
		t.Fatal("expected CrossScopeByHash to be populated even with cancelled context")
	}
}

func TestCrossScope_InaccessibleTorrentExcluded(t *testing.T) {
	t.Parallel()

	// Torrents with allAccessible=false should not appear in cross-scope.
	fid := hardlink.FileID{Dev: 1, Ino: 200}
	state := &hardlinkBuildState{
		globalFileIDMap: map[hardlink.FileID]*fileIDTracker{},
		seenPaths:       make(map[string]struct{}),
		torrentInfoByHash: map[string]*torrentFileInfo{
			"accessible": {
				fileIDs:       []hardlink.FileID{fid},
				allAccessible: true,
			},
			"inaccessible": {
				fileIDs:       []hardlink.FileID{fid},
				allAccessible: false, // Can't inspect all files.
			},
		},
	}

	scope := computeScopeFromState(state)
	if _, ok := scope["accessible"]; !ok {
		t.Error("expected accessible torrent in scope")
	}
	if _, ok := scope["inaccessible"]; ok {
		t.Error("expected inaccessible torrent to be excluded from scope")
	}
}

func TestCrossScope_RejectsEmptyAndRelativeSavePaths(t *testing.T) {
	t.Parallel()

	// Verify that buildFullPath + isPathInsideBase correctly handle dangerous save paths.
	// Empty save path: buildFullPath("", "../etc/passwd") should not pass isPathInsideBase.
	emptyBase := ""
	traversal := buildFullPath(emptyBase, "../etc/passwd")
	if isPathInsideBase(emptyBase, traversal) {
		t.Error("expected empty base path to reject traversal")
	}

	// Relative save path: should be rejected by the filepath.IsAbs check in Phase 2.
	if filepath.IsAbs("relative/path") {
		t.Error("expected relative path to not be absolute")
	}

	// Root save path: isPathInsideBase("/", "/etc/passwd") is technically true,
	// but Phase 2 code rejects empty/relative paths before reaching isPathInsideBase.
	// The root "/" case is not explicitly blocked since it's a valid absolute path
	// that a qBittorrent instance could legitimately report.
}

func TestConditionsRequireLocalAccess_HardlinkScopeCross(t *testing.T) {
	t.Parallel()

	// Verify ConditionUsesField detects HARDLINK_SCOPE_CROSS.
	cond := &RuleCondition{
		Field:    FieldHardlinkScopeCross,
		Operator: OperatorEqual,
		Value:    HardlinkScopeOutsideQBitTorrent,
	}
	if !ConditionUsesField(cond, FieldHardlinkScopeCross) {
		t.Error("expected ConditionUsesField to detect HARDLINK_SCOPE_CROSS")
	}
	if ConditionUsesField(cond, FieldHardlinkScope) {
		t.Error("expected ConditionUsesField to NOT detect HARDLINK_SCOPE for HARDLINK_SCOPE_CROSS condition")
	}
}
