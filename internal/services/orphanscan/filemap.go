// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"path/filepath"
	"strings"
	"sync"
)

// TorrentFileMap is a thread-safe set of file paths belonging to torrents.
type TorrentFileMap struct {
	paths map[string]struct{}
	mu    sync.RWMutex
}

// NewTorrentFileMap creates a new empty TorrentFileMap.
func NewTorrentFileMap() *TorrentFileMap {
	return &TorrentFileMap{
		paths: make(map[string]struct{}),
	}
}

// Add adds a normalized path to the map.
func (m *TorrentFileMap) Add(path string) {
	m.mu.Lock()
	m.paths[path] = struct{}{}
	m.mu.Unlock()
}

// Has checks if a normalized path exists in the map.
func (m *TorrentFileMap) Has(path string) bool {
	m.mu.RLock()
	_, ok := m.paths[path]
	m.mu.RUnlock()
	return ok
}

// Len returns the number of paths in the map.
func (m *TorrentFileMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.paths)
}

// normalizePath cleans and normalizes a path for consistent comparison.
// Uses filepath.Clean (OS-specific separators). No case-folding.
func normalizePath(path string) string {
	return filepath.Clean(path)
}

// canonicalizeHash matches SyncManager's internal hash normalization.
func canonicalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}
