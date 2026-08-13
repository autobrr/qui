// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// TorrentFileMap is a thread-safe set of file paths belonging to torrents.
type TorrentFileMap struct {
	paths map[string]struct{}
	dirs  map[string]struct{}
	mu    sync.RWMutex
}

// NewTorrentFileMap creates a new empty TorrentFileMap.
func NewTorrentFileMap() *TorrentFileMap {
	return &TorrentFileMap{
		paths: make(map[string]struct{}),
		dirs:  make(map[string]struct{}),
	}
}

// Add adds a normalized path to the map.
func (m *TorrentFileMap) Add(path string) {
	n := normalizePath(path)
	// Track the file itself.
	m.mu.Lock()
	m.paths[n] = struct{}{}
	// Track all ancestor directories so we can quickly answer "is any torrent file under this dir?".
	// This keeps lookup O(1) and avoids expensive prefix scans.
	dir := filepath.Dir(n)
	for dir != "." && dir != string(filepath.Separator) {
		m.dirs[dir] = struct{}{}
		parent := filepath.Dir(dir)
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
// The input must be normalized with normalizePath.
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

// MergeFrom unions other into m.
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

// cleanPath cleans a path without changing its casing.
// Uses filepath.Clean (OS-specific separators) and NFC unicode normalization to
// avoid mismatches between canonically-equivalent strings (e.g. composed vs
// decomposed forms on some platforms).
// Tradeoff: canonically-equivalent names collapse to one key, so byte-distinct
// NFC/NFD twins on normalization-sensitive filesystems are treated as one path.
// Use this when the result is shown to the user or stored; use normalizePath
// when the result is only compared against another path.
func cleanPath(path string) string {
	p := filepath.Clean(path)
	// On Unix, paths can contain arbitrary bytes (not always valid UTF-8).
	// Avoid normalizing invalid UTF-8 to prevent replacing bytes with U+FFFD.
	if !utf8.ValidString(p) {
		return p
	}
	if !norm.NFC.IsNormalString(p) {
		p = norm.NFC.String(p)
	}
	return p
}

// normalizePath cleans a path and case-folds it for consistent comparison.
//
// Case-folding is unconditional because runtime.GOOS cannot answer whether the
// scanned filesystem is case-insensitive: macOS APFS and Windows volumes are
// case-insensitive by default, and Docker bind-mounts them into a Linux
// container, where qBittorrent can report two save paths that differ only by
// case for one physical directory.
//
// Invalid UTF-8 is folded too. strings.ToLower and encoding/json replace a bad
// byte the same way, so the fold is what lets a name read from disk match the
// same name after a round trip through the qBittorrent API. Do not add a
// utf8.ValidString guard here: an owned file whose name carries a non-UTF-8
// byte is then reported as an orphan and deleted.
//
// ToLower can turn an NFC string into a non-NFC one (H + U+0331 becomes U+1E96),
// so the folded result is cleaned again.
//
// ponytail: names that differ only by case, or only in which invalid bytes they
// carry, collapse to one key. That direction is safe (a missed orphan, never a
// deleted owned file). Upgrade path if it is ever reported: keep a folded
// secondary index and confirm hits with os.SameFile.
//
// The result is for comparison only. It must never reach an os.* call or the
// database, because on a case-sensitive filesystem it can name a different file.
func normalizePath(path string) string {
	return cleanPath(strings.ToLower(cleanPath(path)))
}

// canonicalizeHash matches SyncManager's internal hash normalization.
func canonicalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}
