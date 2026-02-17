// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
)

// hardlinkIndexTTL is the cache TTL for hardlink indices.
const hardlinkIndexTTL = 2 * time.Minute

// HardlinkIndex is a cached, single-build index of hardlink duplicate groups.
// It enables O(1) lookups for hardlink expansion and FREE_SPACE projection dedupe.
type HardlinkIndex struct {
	// SignatureByHash maps torrent hash to its hardlink signature (hex-encoded sha256 of sorted FileIDs).
	// Only torrents with all files hard-linked within qBittorrent ("safe" duplicates) are included.
	SignatureByHash map[string]string

	// GroupBySignature maps signature to list of torrent hashes sharing that signature.
	// Only contains groups with 2+ members (actual duplicates).
	GroupBySignature map[string][]string

	// ScopeByHash maps torrent hash to its hardlink scope (none, torrents_only, outside_qbittorrent).
	// Used for HARDLINK_SCOPE condition evaluation.
	ScopeByHash map[string]string

	// builtAt is when this index was built.
	builtAt time.Time

	// digest identifies the torrent set used to build this index.
	digest string
}

// hardlinkIndexCache stores cached indices per instance.
type hardlinkIndexCache struct {
	mu      sync.RWMutex
	indices map[int]*HardlinkIndex
	sf      singleflight.Group
}

var globalHardlinkIndexCache = &hardlinkIndexCache{
	indices: make(map[int]*HardlinkIndex),
}

// GetHardlinkIndex returns a cached or freshly built hardlink index for the given instance.
// The index is cached for 2 minutes and invalidated when the torrent set changes.
func (s *Service) GetHardlinkIndex(ctx context.Context, instanceID int, torrents []qbt.Torrent) *HardlinkIndex {
	if s == nil || s.syncManager == nil {
		return nil
	}

	// Compute digest of current torrent set
	currentDigest := computeTorrentSetDigest(torrents)

	// Check cache
	globalHardlinkIndexCache.mu.RLock()
	cached := globalHardlinkIndexCache.indices[instanceID]
	globalHardlinkIndexCache.mu.RUnlock()

	if cached != nil && time.Since(cached.builtAt) < hardlinkIndexTTL && cached.digest == currentDigest {
		return cached
	}

	// Build index with singleflight to prevent duplicate builds.
	// Include digest in key so concurrent calls with different torrent sets don't share results.
	key := strconv.Itoa(instanceID) + ":" + currentDigest
	result, err, _ := globalHardlinkIndexCache.sf.Do(key, func() (any, error) {
		return s.buildHardlinkIndex(ctx, instanceID, torrents, currentDigest), nil
	})
	if err != nil {
		return nil
	}

	idx, ok := result.(*HardlinkIndex)
	if !ok {
		return nil
	}

	// Validate digest matches (paranoid check for edge cases)
	if idx.digest != currentDigest {
		// Rebuild with correct digest
		return s.buildHardlinkIndex(ctx, instanceID, torrents, currentDigest)
	}
	return idx
}

// computeTorrentSetDigest creates a digest identifying the current torrent set.
// Changes in hash or save path invalidate the cache.
func computeTorrentSetDigest(torrents []qbt.Torrent) string {
	if len(torrents) == 0 {
		return ""
	}

	// Sort by (hash, savePath) without string concatenation allocation
	indices := make([]int, len(torrents))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		ti, tj := &torrents[indices[i]], &torrents[indices[j]]
		if ti.Hash != tj.Hash {
			return ti.Hash < tj.Hash
		}
		return ti.SavePath < tj.SavePath
	})

	// Hash sorted torrents directly (avoids intermediate string concatenation)
	h := sha256.New()
	for _, idx := range indices {
		t := &torrents[idx]
		io.WriteString(h, t.Hash) //nolint:errcheck // hash.Hash.Write never returns error
		h.Write([]byte{0})
		io.WriteString(h, t.SavePath) //nolint:errcheck // hash.Hash.Write never returns error
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16] // Use first 16 chars for compactness
}

// fileIDTracker holds file identity and link count tracking during index build.
type fileIDTracker struct {
	nlink           uint64
	uniquePathCount int
}

// buildHardlinkIndex constructs a fresh hardlink index by scanning all torrent files once.
// The complexity is inherent to the single-pass algorithm that avoids multiple filesystem scans.
//
//nolint:gocognit,gocyclo,funlen,revive // complexity is inherent to the single-pass design
func (s *Service) buildHardlinkIndex(ctx context.Context, instanceID int, torrents []qbt.Torrent, digest string) *HardlinkIndex {
	startTime := time.Now()
	index := &HardlinkIndex{
		SignatureByHash:  make(map[string]string),
		GroupBySignature: make(map[string][]string),
		ScopeByHash:      make(map[string]string),
		digest:           digest,
		// builtAt is set at the end of a successful build to avoid TTL issues with slow builds
	}

	if len(torrents) == 0 {
		index.builtAt = time.Now()
		globalHardlinkIndexCache.mu.Lock()
		globalHardlinkIndexCache.indices[instanceID] = index
		globalHardlinkIndexCache.mu.Unlock()
		return index
	}

	// Fetch file lists for all torrents in one batch
	hashes := make([]string, 0, len(torrents))
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for i := range torrents {
		hashes = append(hashes, torrents[i].Hash)
		torrentByHash[torrents[i].Hash] = torrents[i]
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for hardlink index build")

		// Don't cache on context cancellation/deadline - a canceled request shouldn't poison the cache
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return index
		}

		index.builtAt = time.Now()
		globalHardlinkIndexCache.mu.Lock()
		globalHardlinkIndexCache.indices[instanceID] = index
		globalHardlinkIndexCache.mu.Unlock()
		return index
	}

	// Note: partial results (missing hashes) are handled implicitly; torrents with missing file lists
	// will have unknown hardlink scope and won't participate in expansion.

	// Phase 1: Single pass to collect FileID info across all files
	// Track: fileID -> {nlink, uniquePathCount}
	globalFileIDMap := make(map[hardlink.FileID]*fileIDTracker)
	seenPaths := make(map[string]struct{})
	torrentsInvalidPaths := 0

	// Also track per-torrent: which FileIDs it contains and whether all files are accessible
	type torrentFileInfo struct {
		fileIDs        []hardlink.FileID
		allAccessible  bool
		hasHardlinks   bool // At least one file has nlink > 1
		hasInvalidPath bool // At least one file path escapes save path
	}
	torrentInfoByHash := make(map[string]*torrentFileInfo)

	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		info := &torrentFileInfo{
			fileIDs:       make([]hardlink.FileID, 0, len(files)),
			allAccessible: true,
		}
		torrentInfoByHash[hash] = info

		for _, f := range files {
			fullPath := buildFullPath(torrent.SavePath, f.Name)

			// Reject paths that escape the torrent's save path to prevent malicious
			// torrent metadata from causing Lstat on arbitrary filesystem locations.
			if !isPathInsideBase(torrent.SavePath, fullPath) {
				info.allAccessible = false
				info.hasInvalidPath = true
				continue
			}

			fi, err := os.Lstat(fullPath)
			if err != nil {
				info.allAccessible = false
				continue
			}
			if !fi.Mode().IsRegular() {
				continue
			}

			fileID, nlink, err := hardlink.GetFileID(fi, fullPath)
			if err != nil {
				info.allAccessible = false
				continue
			}

			info.fileIDs = append(info.fileIDs, fileID)
			if nlink > 1 {
				info.hasHardlinks = true

				// Only track global fileID info for hard-linked files (nlink > 1).
				// Files with nlink == 1 can't have outside links, so skip them to save memory.
				tracker := globalFileIDMap[fileID]
				if tracker == nil {
					tracker = &fileIDTracker{nlink: nlink}
					globalFileIDMap[fileID] = tracker
				}
				// Count unique paths pointing to this fileID
				if _, seen := seenPaths[fullPath]; !seen {
					seenPaths[fullPath] = struct{}{}
					tracker.uniquePathCount++
				}
			}
		}

		if info.hasInvalidPath {
			torrentsInvalidPaths++
		}
	}

	// Phase 2: Compute scope and signature for each torrent
	torrentsWithOutsideLinks := 0
	torrentsInaccessible := 0

	for hash, info := range torrentInfoByHash {
		// If we couldn't inspect all files, treat scope as "unknown" by not adding to map.
		// This ensures HARDLINK_SCOPE conditions never match for partially-inspected torrents.
		if !info.allAccessible || len(info.fileIDs) == 0 {
			// Only count as inaccessible if not already counted as invalid path
			if !info.hasInvalidPath {
				torrentsInaccessible++
			}
			continue
		}

		// Determine scope
		scope := HardlinkScopeNone
		hasOutsideLinks := false

		for _, fileID := range info.fileIDs {
			tracker := globalFileIDMap[fileID]
			if tracker == nil {
				continue
			}
			if tracker.nlink <= 1 {
				continue // Not hard-linked
			}

			// File has hardlinks
			if tracker.nlink > uint64(tracker.uniquePathCount) { //nolint:gosec // uniquePathCount is always positive
				// Links exist outside the torrent set
				scope = HardlinkScopeOutsideQBitTorrent
				hasOutsideLinks = true
				break
			}
			scope = HardlinkScopeTorrentsOnly
		}

		index.ScopeByHash[hash] = scope

		// Only include in duplicate index if:
		// 1. Has hardlinks (otherwise not a duplicate candidate)
		// 2. No outside links (safe for expansion)
		if !info.hasHardlinks {
			continue // Not a hardlink duplicate candidate
		}
		if hasOutsideLinks {
			torrentsWithOutsideLinks++
			continue
		}

		// Compute signature: sha256 of sorted FileIDs
		sig := computeFileIDSignature(info.fileIDs)
		index.SignatureByHash[hash] = sig
		index.GroupBySignature[sig] = append(index.GroupBySignature[sig], hash)
	}

	// Remove singleton groups (not actual duplicates)
	for sig, hashes := range index.GroupBySignature {
		if len(hashes) < 2 {
			delete(index.GroupBySignature, sig)
			delete(index.SignatureByHash, hashes[0])
		}
	}

	// Set builtAt at the end of successful build (not start) to avoid TTL issues with slow builds
	index.builtAt = time.Now()

	// Cache the index
	globalHardlinkIndexCache.mu.Lock()
	globalHardlinkIndexCache.indices[instanceID] = index
	globalHardlinkIndexCache.mu.Unlock()

	log.Debug().
		Int("instanceID", instanceID).
		Int("totalTorrents", len(torrents)).
		Int("scopeComputed", len(index.ScopeByHash)).
		Int("duplicateGroups", len(index.GroupBySignature)).
		Int("duplicateTorrents", len(index.SignatureByHash)).
		Int("outsideLinks", torrentsWithOutsideLinks).
		Int("inaccessible", torrentsInaccessible).
		Int("invalidPaths", torrentsInvalidPaths).
		Dur("buildTime", time.Since(startTime)).
		Msg("automations: hardlink index built")

	return index
}

// computeFileIDSignature creates a compact signature from a list of FileIDs.
func computeFileIDSignature(fileIDs []hardlink.FileID) string {
	if len(fileIDs) == 0 {
		return ""
	}

	// Sort FileIDs for stability using the platform-agnostic Less method
	sorted := make([]hardlink.FileID, len(fileIDs))
	copy(sorted, fileIDs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Less(sorted[j])
	})

	// Hash the sorted FileIDs using WriteToHash to avoid per-file allocations
	h := sha256.New()
	for _, fid := range sorted {
		fid.WriteToHash(h)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// isPathInsideBase checks if fullPath is safely contained within basePath.
// Returns true if fullPath is inside basePath, false if it escapes (e.g., via ".." traversal).
// This prevents malicious torrent metadata from causing Lstat on arbitrary paths.
func isPathInsideBase(basePath, fullPath string) bool {
	// Clean both paths to resolve any . or .. components
	cleanBase := filepath.Clean(basePath)
	cleanFull := filepath.Clean(fullPath)

	// Get relative path from base to full
	rel, err := filepath.Rel(cleanBase, cleanFull)
	if err != nil {
		return false
	}

	// Check if the relative path escapes the base:
	// - ".." means direct parent traversal
	// - Paths starting with "../" traverse upward
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}

	return true
}

// GetHardlinkCopies returns torrent hashes that share the same physical files as the trigger.
// Uses O(1) lookup via the cached index. Returns nil if trigger has no hardlink duplicates.
func (idx *HardlinkIndex) GetHardlinkCopies(triggerHash string) []string {
	if idx == nil {
		return nil
	}

	sig, ok := idx.SignatureByHash[triggerHash]
	if !ok {
		return nil
	}

	group := idx.GroupBySignature[sig]
	if len(group) <= 1 {
		return nil
	}

	copies := make([]string, 0, len(group)-1)
	for _, h := range group {
		if h != triggerHash {
			copies = append(copies, h)
		}
	}
	return copies
}

// GetHardlinkScope returns the hardlink scope for a torrent (none, torrents_only, outside_qbittorrent).
// Returns empty string if the scope is unknown (torrent not in index, files inaccessible, etc.).
func (idx *HardlinkIndex) GetHardlinkScope(hash string) string {
	if idx == nil {
		return ""
	}
	if scope, ok := idx.ScopeByHash[hash]; ok {
		return scope
	}
	return ""
}

// InvalidateHardlinkIndex removes the cached index for an instance.
// Call this when torrents are added/removed to force a rebuild on next access.
func InvalidateHardlinkIndex(instanceID int) {
	globalHardlinkIndexCache.mu.Lock()
	delete(globalHardlinkIndexCache.indices, instanceID)
	globalHardlinkIndexCache.mu.Unlock()
}

// ClearHardlinkIndexCache clears all cached indices.
// Useful for tests or when global settings change.
func ClearHardlinkIndexCache() {
	globalHardlinkIndexCache.mu.Lock()
	globalHardlinkIndexCache.indices = make(map[int]*HardlinkIndex)
	globalHardlinkIndexCache.mu.Unlock()
}

// Ensure syncManager implements the required interface
var _ interface {
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
} = (*qbittorrent.SyncManager)(nil)
