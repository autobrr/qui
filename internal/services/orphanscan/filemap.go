// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"strings"
	"sync"

	"github.com/autobrr/qui/internal/fsops"
)

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

// Has checks if a normalized path exists in the map.
func (m *TorrentFileMap) Has(path string) bool {
	m.mu.RLock()
	_, ok := m.paths[path]
	m.mu.RUnlock()
	return ok
}

// HasAnyInDir reports whether at least one torrent file exists at or below dirPath.
// The input must be normalized with fsops.NormalizePath.
func (m *TorrentFileMap) HasAnyInDir(dirPath string) bool {
	m.mu.RLock()
	_, ok := m.dirs[dirPath]
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

// canonicalizeHash matches SyncManager's internal hash normalization.
func canonicalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}
