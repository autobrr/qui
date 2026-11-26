// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"fmt"
	"sync"

	"github.com/autobrr/qui/internal/fsops"
)

type protectedPaths struct {
	d               fsops.PathDialect
	files           *TorrentFileMap
	ignorePaths     []string
	normIgnorePaths []string
	scanRoots       []string
	inUse           *inUseIDs
}

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

func (p *protectedPaths) setScanRoots(roots []string) {
	p.scanRoots = make([]string, len(roots))
	for i, root := range roots {
		p.scanRoots[i] = fsops.NormalizePath(p.d, root)
	}
}

func (p *protectedPaths) holdsScanRoot(dir string) bool {
	n := fsops.NormalizePath(p.d, dir)
	for _, root := range p.scanRoots {
		if root == n || isPathUnderNormalized(p.d, root, n) {
			return true
		}
	}
	return false
}

func (p *protectedPaths) torrentFile(path string) bool {
	return p.files != nil && p.files.Has(path)
}

func (p *protectedPaths) torrentFileIn(dir string) bool {
	return p.files != nil && p.files.HasAnyInDir(dir)
}

func (p *protectedPaths) ignored(path string) bool {
	n := fsops.NormalizePath(p.d, path)
	for _, ignore := range p.normIgnorePaths {
		if n == ignore || isPathUnderNormalized(p.d, n, ignore) {
			return true
		}
	}
	return false
}

func (p *protectedPaths) ignoredTorrentDirs() []string {
	var dirs []string
	for _, ignore := range p.ignorePaths {
		if !isAbsoluteFilesystemRoot(p.d, ignore) && (p.torrentFile(ignore) || p.torrentFileIn(ignore)) {
			dirs = append(dirs, ignore)
		}
	}
	return dirs
}

func (p *protectedPaths) ignoredOrHoldsIgnored(path string) bool {
	n := fsops.NormalizePath(p.d, path)
	for _, ignore := range p.normIgnorePaths {
		if n == ignore || isPathUnderNormalized(p.d, n, ignore) || isPathUnderNormalized(p.d, ignore, n) {
			return true
		}
	}
	return false
}

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

type TorrentFileMap struct {
	d     fsops.PathDialect
	paths map[string]struct{}
	dirs  map[string]struct{}
	mu    sync.RWMutex
}

func NewTorrentFileMap(d fsops.PathDialect) *TorrentFileMap {
	return &TorrentFileMap{
		d:     d,
		paths: make(map[string]struct{}),
		dirs:  make(map[string]struct{}),
	}
}

func (m *TorrentFileMap) Add(path string) {
	n := fsops.NormalizePath(m.d, path)
	m.mu.Lock()
	m.paths[n] = struct{}{}
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

func (m *TorrentFileMap) Has(path string) bool {
	n := fsops.NormalizePath(m.d, path)
	m.mu.RLock()
	_, ok := m.paths[n]
	m.mu.RUnlock()
	return ok
}

func (m *TorrentFileMap) HasAnyInDir(dirPath string) bool {
	n := fsops.NormalizePath(m.d, dirPath)
	m.mu.RLock()
	_, ok := m.dirs[n]
	m.mu.RUnlock()
	return ok
}

func (m *TorrentFileMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.paths)
}

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
