// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
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
	// Includes all inspected torrents with hardlinks and is used for hardlink_signature grouping.
	SignatureByHash map[string]string

	// GroupBySignature maps signature to list of torrent hashes sharing that signature.
	// Only contains groups with 2+ members (actual duplicates).
	GroupBySignature map[string][]string

	// DeleteSafeSignatureByHash maps torrent hash to its hardlink signature for torrents whose
	// hardlinks stay fully inside qBittorrent. Used by delete/include-hardlinks expansion and
	// FREE_SPACE dedupe so destructive paths never follow outside links.
	DeleteSafeSignatureByHash map[string]string

	// DeleteSafeGroupBySignature maps signature to list of delete-safe torrent hashes sharing it.
	// Only contains groups with 2+ members.
	DeleteSafeGroupBySignature map[string][]string

	// ScopeByHash maps torrent hash to its hardlink scope (none, torrents_only, outside_qbittorrent).
	// Used for HARDLINK_SCOPE condition evaluation.
	ScopeByHash map[string]string

	// CrossScopeByHash maps torrent hash to its cross-instance hardlink scope.
	// Considers files from ALL instances with HasLocalFilesystemAccess when resolving
	// whether "outside" links are on other qBittorrent instances or truly external.
	// Used for HARDLINK_SCOPE_CROSS condition evaluation. Nil until Phase 2 runs.
	// Access requires holding crossScopeMu.
	CrossScopeByHash map[string]string

	// builtAt is when this index was built.
	builtAt time.Time

	// digest identifies the torrent set used to build this index.
	digest string

	// buildState holds intermediate data from Phase 1 that Phase 2 (cross-instance
	// augmentation) needs. Retained until cross-scope is computed or the index is replaced.
	// Access requires holding crossScopeMu.
	buildState *hardlinkBuildState

	// crossScopeMu protects CrossScopeByHash and buildState from concurrent access.
	// augmentCrossInstanceScope and finalizeCrossScope must be called with this held.
	// On success, CrossScopeByHash is set and buildState freed.
	// On failure (e.g. context cancellation), CrossScopeByHash stays nil and
	// buildState is retained so the next caller can retry.
	crossScopeMu sync.Mutex
}

// hardlinkBuildState holds intermediate state from the single-instance scan so that
// cross-instance augmentation can update uniquePathCount without re-scanning.
type hardlinkBuildState struct {
	globalFileIDMap   map[hardlink.FileID]*fileIDTracker
	seenPaths         map[string]struct{}
	torrentInfoByHash map[string]*torrentFileInfo
}

// torrentFileInfo tracks per-torrent file identity data during hardlink index build.
type torrentFileInfo struct {
	fileIDs        []hardlink.FileID
	allAccessible  bool
	hasHardlinks   bool // at least one file has nlink > 1
	hasInvalidPath bool // at least one file path escapes save path
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
// When needsCrossScope is true, the index retains intermediate build state so that
// augmentCrossInstanceScope can later augment it without re-scanning the current instance.
func (s *Service) GetHardlinkIndex(ctx context.Context, instanceID int, torrents []qbt.Torrent, needsCrossScope bool) *HardlinkIndex {
	if s == nil || s.syncManager == nil {
		return nil
	}

	// Compute digest of current torrent set
	currentDigest := computeTorrentSetDigest(torrents)

	// Check cache
	globalHardlinkIndexCache.mu.RLock()
	cached := globalHardlinkIndexCache.indices[instanceID]
	globalHardlinkIndexCache.mu.RUnlock()

	cacheValid := cached != nil && time.Since(cached.builtAt) < hardlinkIndexTTL && cached.digest == currentDigest
	if cacheValid && needsCrossScope {
		// Check cross-scope fields under the per-index mutex to avoid racing with
		// a concurrent augmentCrossInstanceScope that writes these fields.
		cached.crossScopeMu.Lock()
		needsRebuild := cached.CrossScopeByHash == nil && cached.buildState == nil
		cached.crossScopeMu.Unlock()
		if needsRebuild {
			cacheValid = false
		}
	}
	if cacheValid {
		return cached
	}

	// Build index with singleflight to prevent duplicate builds.
	// Include digest and cross-scope flag in key so concurrent calls with different
	// scope requirements don't collide.
	crossFlag := "0"
	if needsCrossScope {
		crossFlag = "1"
	}
	key := strconv.Itoa(instanceID) + ":" + currentDigest + ":" + crossFlag
	result, err, _ := globalHardlinkIndexCache.sf.Do(key, func() (any, error) {
		return s.buildHardlinkIndex(ctx, instanceID, torrents, currentDigest, needsCrossScope), nil
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
		return s.buildHardlinkIndex(ctx, instanceID, torrents, currentDigest, needsCrossScope)
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
func (s *Service) buildHardlinkIndex(ctx context.Context, instanceID int, torrents []qbt.Torrent, digest string, retainBuildState bool) *HardlinkIndex {
	startTime := time.Now()
	index := &HardlinkIndex{
		SignatureByHash:            make(map[string]string),
		GroupBySignature:           make(map[string][]string),
		DeleteSafeSignatureByHash:  make(map[string]string),
		DeleteSafeGroupBySignature: make(map[string][]string),
		ScopeByHash:                make(map[string]string),
		digest:                     digest,
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

	torrentInfoByHash := make(map[string]*torrentFileInfo)

	for hash, files := range filesByHash {
		torrent := torrentByHash[hash]
		info := &torrentFileInfo{
			fileIDs:       make([]hardlink.FileID, 0, len(files)),
			allAccessible: true,
		}
		torrentInfoByHash[hash] = info

		// Reject empty or non-absolute save paths to prevent Lstat on unintended locations.
		if torrent.SavePath == "" || !filepath.IsAbs(torrent.SavePath) {
			info.allAccessible = false
			info.hasInvalidPath = true
			torrentsInvalidPaths++
			continue
		}

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
		if !info.hasHardlinks {
			continue // Not a hardlink duplicate candidate
		}

		if hasOutsideLinks {
			torrentsWithOutsideLinks++
		}

		// Compute signature once; feed broad grouping map plus delete-safe subset.
		sig := computeFileIDSignature(info.fileIDs)
		index.SignatureByHash[hash] = sig
		index.GroupBySignature[sig] = append(index.GroupBySignature[sig], hash)
		if !hasOutsideLinks {
			index.DeleteSafeSignatureByHash[hash] = sig
			index.DeleteSafeGroupBySignature[sig] = append(index.DeleteSafeGroupBySignature[sig], hash)
		}
	}

	pruneSingletonHardlinkGroups(index.SignatureByHash, index.GroupBySignature)
	pruneSingletonHardlinkGroups(index.DeleteSafeSignatureByHash, index.DeleteSafeGroupBySignature)

	// Retain build state only when cross-instance augmentation (Phase 2) may be needed.
	// This avoids keeping globalFileIDMap/seenPaths/torrentInfoByHash in memory for
	// runs that only use HARDLINK_SCOPE or includeHardlinks.
	if retainBuildState {
		index.buildState = &hardlinkBuildState{
			globalFileIDMap:   globalFileIDMap,
			seenPaths:         seenPaths,
			torrentInfoByHash: torrentInfoByHash,
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
		Int("groupingDuplicateGroups", len(index.GroupBySignature)).
		Int("groupingDuplicateTorrents", len(index.SignatureByHash)).
		Int("deleteSafeDuplicateGroups", len(index.DeleteSafeGroupBySignature)).
		Int("deleteSafeDuplicateTorrents", len(index.DeleteSafeSignatureByHash)).
		Int("outsideLinks", torrentsWithOutsideLinks).
		Int("inaccessible", torrentsInaccessible).
		Int("invalidPaths", torrentsInvalidPaths).
		Dur("buildTime", time.Since(startTime)).
		Msg("automations: hardlink index built")

	return index
}

func pruneSingletonHardlinkGroups(signatureByHash map[string]string, groupsBySignature map[string][]string) {
	for sig, hashes := range groupsBySignature {
		if len(hashes) >= 2 {
			continue
		}
		delete(groupsBySignature, sig)
		if len(hashes) == 1 {
			delete(signatureByHash, hashes[0])
		}
	}
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

	sig, ok := idx.DeleteSafeSignatureByHash[triggerHash]
	if !ok {
		return nil
	}

	group := idx.DeleteSafeGroupBySignature[sig]
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

// GetHardlinkCrossScope returns the cross-instance hardlink scope for a torrent.
// Returns empty string if cross-scope has not been computed or is unknown.
// Safe for concurrent use; acquires crossScopeMu internally.
func (idx *HardlinkIndex) GetHardlinkCrossScope(hash string) string {
	if idx == nil {
		return ""
	}
	idx.crossScopeMu.Lock()
	scopeMap := idx.CrossScopeByHash
	idx.crossScopeMu.Unlock()
	if scopeMap == nil {
		return ""
	}
	if scope, ok := scopeMap[hash]; ok {
		return scope
	}
	return ""
}

// augmentCrossInstanceScope runs Phase 2 of the hardlink index: scanning files from other
// instances to determine whether "outside" hardlinks point to other qBittorrent instances
// or to truly external paths (media libraries, import dirs, etc.).
//
// It only Lstats files from other instances that might resolve deficit FileIDs (where
// nlink > uniquePathCount after the single-instance scan). Scanning stops early once all
// deficits are resolved.
func (s *Service) augmentCrossInstanceScope(ctx context.Context, instanceID int, index *HardlinkIndex) {
	if index == nil || index.buildState == nil {
		return
	}
	state := index.buildState
	phase2Start := time.Now()

	deficitSet := collectDeficitFileIDs(state)

	if len(deficitSet) == 0 {
		index.finalizeCrossScope(instanceID, "no deficits")
		return
	}

	if s.instanceStore == nil || s.syncManager == nil {
		log.Warn().Int("instanceID", instanceID).
			Msg("automations: instanceStore or syncManager unavailable for cross-scope, falling back to single-instance scope")
		index.finalizeCrossScope(instanceID, "")
		return
	}

	otherInstances, err := s.listCrossScopeInstances(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to list instances for cross-scope, falling back to single-instance scope")
		index.finalizeCrossScope(instanceID, "")
		return
	}
	if len(otherInstances) == 0 {
		index.finalizeCrossScope(instanceID, "no other instances with local access")
		return
	}

	stats := s.scanOtherInstancesForDeficits(ctx, instanceID, otherInstances, deficitSet, state)

	// If context was cancelled during scanning, don't cache partial results.
	// Leave CrossScopeByHash nil and retain buildState so the next caller can retry.
	if ctx.Err() != nil {
		log.Warn().Int("instanceID", instanceID).
			Msg("automations: cross-instance scope aborted (context cancelled), will retry on next run")
		return
	}

	// Recompute scope for all torrents using augmented counts.
	index.CrossScopeByHash = computeScopeMap(state)
	index.buildState = nil

	var countNone, countTorrentsOnly, countOutside int
	for _, scope := range index.CrossScopeByHash {
		switch scope {
		case HardlinkScopeNone:
			countNone++
		case HardlinkScopeTorrentsOnly:
			countTorrentsOnly++
		case HardlinkScopeOutsideQBitTorrent:
			countOutside++
		}
	}

	log.Debug().
		Int("instanceID", instanceID).
		Int("otherInstancesScanned", stats.scanned).
		Int("otherInstancesSkipped", stats.skipped).
		Int("deficitFileIDsBefore", stats.deficitBefore).
		Int("deficitFileIDsAfter", len(deficitSet)).
		Int("crossScopeNone", countNone).
		Int("crossScopeTorrentsOnly", countTorrentsOnly).
		Int("crossScopeOutside", countOutside).
		Int("lstatCalls", stats.lstatCalls).
		Int("lstatErrors", stats.lstatErrors).
		Dur("crossScopeBuildTime", time.Since(phase2Start)).
		Msg("automations: cross-instance hardlink scope computed")
}

// collectDeficitFileIDs returns FileIDs where nlink > uniquePathCount.
func collectDeficitFileIDs(state *hardlinkBuildState) map[hardlink.FileID]*fileIDTracker {
	deficitSet := make(map[hardlink.FileID]*fileIDTracker)
	for fileID, tracker := range state.globalFileIDMap {
		if tracker.nlink > uint64(tracker.uniquePathCount) { //nolint:gosec // uniquePathCount is always positive
			deficitSet[fileID] = tracker
		}
	}
	return deficitSet
}

// finalizeCrossScope copies ScopeByHash to CrossScopeByHash and frees build state.
// Used when Phase 2 cannot or does not need to scan other instances.
func (idx *HardlinkIndex) finalizeCrossScope(instanceID int, reason string) {
	idx.CrossScopeByHash = make(map[string]string, len(idx.ScopeByHash))
	maps.Copy(idx.CrossScopeByHash, idx.ScopeByHash)
	idx.buildState = nil
	if reason != "" {
		log.Debug().Int("instanceID", instanceID).
			Msgf("automations: cross-instance scope matches single-instance (%s)", reason)
	}
}

// listCrossScopeInstances returns IDs of other active instances with local filesystem access.
func (s *Service) listCrossScopeInstances(ctx context.Context, instanceID int) ([]int, error) {
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		return nil, err
	}
	var result []int
	for _, inst := range instances {
		if inst.ID != instanceID && inst.IsActive && inst.HasLocalFilesystemAccess {
			result = append(result, inst.ID)
		}
	}
	return result, nil
}

// crossScanStats holds counters from the cross-instance scan loop.
type crossScanStats struct {
	scanned, skipped int
	deficitBefore    int
	lstatCalls       int
	lstatErrors      int
}

// maxCrossInstanceLstatCalls limits the total Lstat calls during cross-instance scanning
// to prevent excessive filesystem operations from misconfigured qBittorrent instances.
const maxCrossInstanceLstatCalls = 500_000

// scanOtherInstancesForDeficits Lstats files from other instances to resolve deficit FileIDs.
// Modifies deficitSet and state.seenPaths/globalFileIDMap in place.
//
//nolint:gocognit // early-exit checks at multiple loop levels are inherent to the scanning pattern
func (s *Service) scanOtherInstancesForDeficits(
	ctx context.Context,
	instanceID int,
	otherInstances []int,
	deficitSet map[hardlink.FileID]*fileIDTracker,
	state *hardlinkBuildState,
) crossScanStats {
	stats := crossScanStats{deficitBefore: len(deficitSet)}

	for _, otherID := range otherInstances {
		if ctx.Err() != nil || len(deficitSet) == 0 || stats.lstatCalls >= maxCrossInstanceLstatCalls {
			break
		}

		views, err := s.syncManager.GetCachedInstanceTorrents(ctx, otherID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Int("otherInstanceID", otherID).
				Msg("automations: failed to get torrents for cross-scope, skipping instance")
			stats.skipped++
			continue
		}

		otherHashes := make([]string, 0, len(views))
		savePaths := make(map[string]string, len(views))
		for _, v := range views {
			otherHashes = append(otherHashes, v.Hash)
			savePaths[v.Hash] = v.SavePath
		}

		filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, otherID, otherHashes)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Int("otherInstanceID", otherID).
				Msg("automations: failed to get files for cross-scope, skipping instance")
			stats.skipped++
			continue
		}

		stats.scanned++

		for hash, files := range filesByHash {
			if len(deficitSet) == 0 || stats.lstatCalls >= maxCrossInstanceLstatCalls {
				break
			}

			savePath := savePaths[hash]
			if savePath == "" || !filepath.IsAbs(savePath) {
				continue
			}

			for _, f := range files {
				if len(deficitSet) == 0 || stats.lstatCalls >= maxCrossInstanceLstatCalls {
					break
				}

				fullPath := buildFullPath(savePath, f.Name)
				if !isPathInsideBase(savePath, fullPath) {
					continue
				}
				if _, seen := state.seenPaths[fullPath]; seen {
					continue
				}

				stats.lstatCalls++

				fi, err := os.Lstat(fullPath)
				if err != nil {
					stats.lstatErrors++
					continue
				}
				if !fi.Mode().IsRegular() {
					continue
				}

				fileID, _, err := hardlink.GetFileID(fi, fullPath)
				if err != nil {
					stats.lstatErrors++
					continue
				}

				tracker, isDeficit := deficitSet[fileID]
				if !isDeficit {
					continue
				}

				state.seenPaths[fullPath] = struct{}{}
				tracker.uniquePathCount++

				if tracker.nlink <= uint64(tracker.uniquePathCount) { //nolint:gosec // uniquePathCount is always positive
					delete(deficitSet, fileID)
				}
			}
		}
	}

	if stats.lstatCalls >= maxCrossInstanceLstatCalls {
		log.Warn().
			Int("instanceID", instanceID).
			Int("lstatCalls", stats.lstatCalls).
			Int("remainingDeficits", len(deficitSet)).
			Msg("automations: cross-instance Lstat budget exhausted, some deficits may be unresolved")
	}

	return stats
}

// computeScopeMap computes hardlink scope for each torrent from the current build state.
func computeScopeMap(state *hardlinkBuildState) map[string]string {
	result := make(map[string]string, len(state.torrentInfoByHash))
	for hash, info := range state.torrentInfoByHash {
		if !info.allAccessible || len(info.fileIDs) == 0 {
			continue
		}
		scope := HardlinkScopeNone
		for _, fileID := range info.fileIDs {
			tracker := state.globalFileIDMap[fileID]
			if tracker == nil || tracker.nlink <= 1 {
				continue
			}
			if tracker.nlink > uint64(tracker.uniquePathCount) { //nolint:gosec // uniquePathCount is always positive
				scope = HardlinkScopeOutsideQBitTorrent
				break
			}
			scope = HardlinkScopeTorrentsOnly
		}
		result[hash] = scope
	}
	return result
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
