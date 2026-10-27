// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"fmt"
	"sync"

	"github.com/autobrr/qui/internal/fsops"
)

// protectedPaths answers, for one phase of a run, whether a path is a Protected
// path: a file in a torrent, a folder that holds a file in a torrent, a path that
// an ignore path covers, or a folder that holds an ignore path. Callers pass raw
// paths in the backend's dialect; normalization for these questions happens only
// here.
type protectedPaths struct {
	d     fsops.PathDialect
	files *TorrentFileMap // nil: no torrent owns anything
	// ignorePaths keep the user's casing for the walker's exact-match prune;
	// normIgnorePaths are the folded keys every match compares against.
	ignorePaths     []string
	normIgnorePaths []string
	// scanRoots are the run's roots, folded like the delete's scan root
	// boundary compares them.
	scanRoots []string
}

// newProtectedPaths validates and normalizes ignorePaths once. files can be nil.
func newProtectedPaths(d fsops.PathDialect, ignorePaths []string, files *TorrentFileMap) (*protectedPaths, error) {
	cleaned, err := NormalizeIgnorePaths(d, ignorePaths)
	if err != nil {
		return nil, err
	}
	norm := make([]string, len(cleaned))
	for i, p := range cleaned {
		norm[i] = fsops.NormalizePath(d, p)
	}
	return &protectedPaths{d: d, files: files, ignorePaths: cleaned, normIgnorePaths: norm}, nil
}

// setScanRoots records the run's scan roots, so a walk from an outer root
// treats a nested one as it treats its own.
func (p *protectedPaths) setScanRoots(roots []string) {
	p.scanRoots = make([]string, len(roots))
	for i, root := range roots {
		p.scanRoots[i] = fsops.NormalizePath(p.d, root)
	}
}

// holdsScanRoot reports whether dir is one of the run's scan roots or holds
// one.
func (p *protectedPaths) holdsScanRoot(dir string) bool {
	n := fsops.NormalizePath(p.d, dir)
	for _, root := range p.scanRoots {
		if root == n || isPathUnderNormalized(p.d, root, n) {
			return true
		}
	}
	return false
}

// torrentFile reports whether a torrent owns the file at path.
func (p *protectedPaths) torrentFile(path string) bool {
	return p.files != nil && p.files.Has(path)
}

// torrentFileIn reports whether a torrent owns a file at or below dir.
func (p *protectedPaths) torrentFileIn(dir string) bool {
	return p.files != nil && p.files.HasAnyInDir(dir)
}

// ignored reports whether an ignore path covers path. The boundary check keeps
// /data/foo from covering /data/foobar.
func (p *protectedPaths) ignored(path string) bool {
	n := fsops.NormalizePath(p.d, path)
	for _, ignore := range p.normIgnorePaths {
		if n == ignore || isPathUnderNormalized(p.d, n, ignore) {
			return true
		}
	}
	return false
}

// ignoredTorrentDirs returns the ignore paths that are or hold a torrent's
// files. No walk enters an ignore path, so without a walk of its own a bind
// mount alias of such a file elsewhere would be listed. Any other ignore path
// is not walked, since ignored paths are often unmounted (#2483), and a
// filesystem root already ignores every path, so nothing can be listed.
func (p *protectedPaths) ignoredTorrentDirs() []string {
	var dirs []string
	for _, ignore := range p.ignorePaths {
		if !isAbsoluteFilesystemRoot(p.d, ignore) && (p.torrentFile(ignore) || p.torrentFileIn(ignore)) {
			dirs = append(dirs, ignore)
		}
	}
	return dirs
}

// ignoredOrHoldsIgnored also protects a folder that holds an ignore path, so a
// delete of that folder cannot take the ignored content with it.
func (p *protectedPaths) ignoredOrHoldsIgnored(path string) bool {
	n := fsops.NormalizePath(p.d, path)
	for _, ignore := range p.normIgnorePaths {
		if n == ignore || isPathUnderNormalized(p.d, n, ignore) || isPathUnderNormalized(p.d, ignore, n) {
			return true
		}
	}
	return false
}

// NormalizeIgnorePaths validates and normalizes ignore paths in d, the dialect of
// the instance's backend.
// All paths must be absolute. The result is stored in settings and shown in the
// UI, so it keeps the casing the user typed; matching case-folds on both sides
// (see protectedPaths).
func NormalizeIgnorePaths(d fsops.PathDialect, paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned := d.Clean(p)
		if !d.IsAbs(cleaned) {
			return nil, fmt.Errorf("ignore path must be absolute: %s", p)
		}
		result = append(result, fsops.CleanPath(d, cleaned))
	}
	return result, nil
}

// TorrentFileMap is a thread-safe set of file paths belonging to torrents.
type TorrentFileMap struct {
	d     fsops.PathDialect
	paths map[string]struct{}
	dirs  map[string]struct{}
	mu    sync.RWMutex
}

// NewTorrentFileMap creates an empty map of paths in d, its backend's dialect.
func NewTorrentFileMap(d fsops.PathDialect) *TorrentFileMap {
	return &TorrentFileMap{
		d:     d,
		paths: make(map[string]struct{}),
		dirs:  make(map[string]struct{}),
	}
}

// Add adds a normalized path to the map.
func (m *TorrentFileMap) Add(path string) {
	n := fsops.NormalizePath(m.d, path)
	// Track the file itself.
	m.mu.Lock()
	m.paths[n] = struct{}{}
	// Track all ancestor directories so we can quickly answer "is any torrent file under this dir?".
	// This keeps lookup O(1) and avoids expensive prefix scans.
	dir := m.d.Dir(n)
	for dir != "." && dir != m.d.Separator() {
		m.dirs[dir] = struct{}{}
		parent := m.d.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	m.mu.Unlock()
}

// Has reports whether path is a file in the map.
func (m *TorrentFileMap) Has(path string) bool {
	n := fsops.NormalizePath(m.d, path)
	m.mu.RLock()
	_, ok := m.paths[n]
	m.mu.RUnlock()
	return ok
}

// HasAnyInDir reports whether at least one torrent file exists at or below dirPath.
func (m *TorrentFileMap) HasAnyInDir(dirPath string) bool {
	n := fsops.NormalizePath(m.d, dirPath)
	m.mu.RLock()
	_, ok := m.dirs[n]
	m.mu.RUnlock()
	return ok
}

// Len returns the number of paths in the map.
func (m *TorrentFileMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.paths)
}

// MergeFrom unions other into m. Peers' maps are built with the scanning
// backend, so both maps' keys share one dialect.
// Returns the number of file paths newly added to m.
func (m *TorrentFileMap) MergeFrom(other *TorrentFileMap) int {
	if other == nil {
		return 0
	}

	other.mu.RLock()
	defer other.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	added := 0
	for p := range other.paths {
		if _, exists := m.paths[p]; !exists {
			m.paths[p] = struct{}{}
			added++
		}
	}
	for d := range other.dirs {
		m.dirs[d] = struct{}{}
	}
	return added
}
