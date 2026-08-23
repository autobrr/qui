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
	"slices"
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

// hardlinkIndexTTL bounds how long an index survives without a full rebuild.
// Torrent set changes are folded in incrementally and do not wait for it, so this
// only has to catch link counts that changed on disk without any torrent being
// added or removed: a manual rm or ln, or a script moving data. Delete decisions
// do not rely on it either, because verifyDeleteCandidates re-reads the disk for
// the candidates before a rule deletes anything.
const hardlinkIndexTTL = 10 * time.Minute

// hardlinkIncrementalChangeRatio is the share of the torrent set that may change
// before an incremental update stops being worthwhile. Past it, the update would
// re-read most torrents anyway, and a full rebuild is the simpler path.
const hardlinkIncrementalChangeRatio = 2

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

	// ScopeByHash maps torrent hash to its hardlink scope (none, torrents_only, outside_qbittorrent, both).
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

// hardlinkBuildState holds the per-torrent scan results plus the link counts derived
// from them. Cross-instance augmentation updates uniquePathCount in place without
// re-scanning, and an incremental update rebuilds the derived counts from
// torrentInfoByHash so it only has to re-read the torrents whose links changed.
type hardlinkBuildState struct {
	globalFileIDMap   map[hardlink.FileID]*fileIDTracker
	seenPaths         map[string]struct{}
	torrentInfoByHash map[string]*torrentFileInfo
}

// linkedFile records one hardlinked file exactly as the disk reported it. Keeping
// these lets an incremental update rebuild the global link counts in memory, so a
// torrent that nothing linked to or from is never re-read.
type linkedFile struct {
	path   string
	fileID hardlink.FileID
	nlink  uint64
}

// torrentFileInfo tracks per-torrent file identity data during hardlink index build.
type torrentFileInfo struct {
	savePath       string // the save path this scan used; a change invalidates the scan
	fileIDs        []hardlink.FileID
	linkedFiles    []linkedFile // files seen with nlink > 1, in scan order
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

// GetHardlinkIndex returns a cached, incrementally updated, or freshly built hardlink
// index for the given instance. A change to the torrent set is folded in by re-reading
// only the torrents whose links it can have changed; the full rebuild runs when the
// index ages past hardlinkIndexTTL, when too much of the set changed at once, or when
// no previous scan is available to build on.
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

	fresh := cached != nil && time.Since(cached.builtAt) < hardlinkIndexTTL
	if fresh && cached.digest == currentDigest {
		return cached
	}

	// Build index with singleflight to prevent duplicate builds. The digest keys the
	// call so concurrent callers looking at the same torrent set share one build.
	key := strconv.Itoa(instanceID) + ":" + currentDigest
	result, err, _ := globalHardlinkIndexCache.sf.Do(key, func() (any, error) {
		if fresh {
			if updated := s.updateHardlinkIndex(ctx, instanceID, cached, torrents, currentDigest); updated != nil {
				return updated, nil
			}
		}
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

// scanTorrentFiles reads one torrent's files off disk and records what it found.
// A torrent that could not be fully inspected keeps allAccessible false, which
// leaves its scope unknown rather than guessing at "not hardlinked".
func scanTorrentFiles(torrent qbt.Torrent, files qbt.TorrentFiles) *torrentFileInfo {
	info := &torrentFileInfo{
		savePath:      torrent.SavePath,
		fileIDs:       make([]hardlink.FileID, 0, len(files)),
		allAccessible: true,
	}

	// Reject empty or non-absolute save paths to prevent Lstat on unintended locations.
	if torrent.SavePath == "" || !filepath.IsAbs(torrent.SavePath) {
		info.allAccessible = false
		info.hasInvalidPath = true
		return info
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
			if f.Priority == 0 && os.IsNotExist(err) {
				continue
			}
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
			// Only track hard-linked files. Files with nlink == 1 cannot have outside
			// links, so leaving them out keeps the retained state small.
			info.hasHardlinks = true
			info.linkedFiles = append(info.linkedFiles, linkedFile{path: fullPath, fileID: fileID, nlink: nlink})
		}
	}

	return info
}

// deriveLinkCounts rebuilds the global link counts from the per-torrent scans. It
// touches no disk, so an incremental update can recompute every count after
// re-reading only the torrents whose links actually changed.
//
// Scans taken at different moments can disagree on a file's link count. The higher
// count wins, because it is the one that reports links outside the torrent set, and
// that answer excludes the torrent from the delete-safe groups.
func deriveLinkCounts(torrentInfoByHash map[string]*torrentFileInfo) *hardlinkBuildState {
	globalFileIDMap := make(map[hardlink.FileID]*fileIDTracker)
	seenPaths := make(map[string]struct{})

	for _, info := range torrentInfoByHash {
		for _, lf := range info.linkedFiles {
			tracker := globalFileIDMap[lf.fileID]
			if tracker == nil {
				tracker = &fileIDTracker{nlink: lf.nlink}
				globalFileIDMap[lf.fileID] = tracker
			} else if lf.nlink > tracker.nlink {
				tracker.nlink = lf.nlink
			}

			if _, seen := seenPaths[lf.path]; !seen {
				seenPaths[lf.path] = struct{}{}
				tracker.uniquePathCount++
			}
		}
	}

	return &hardlinkBuildState{
		globalFileIDMap:   globalFileIDMap,
		seenPaths:         seenPaths,
		torrentInfoByHash: torrentInfoByHash,
	}
}

// updateHardlinkIndex folds a torrent set change into the previous index instead of
// rescanning every torrent. Adding or removing one torrent changes the link counts of
// the torrents that share its files and of nothing else, so only those are re-read.
//
// Returns nil when the update cannot be trusted or would not pay for itself, leaving
// the caller to run a full build: no previous scan to build on, a fetch failure, or
// more than a fraction of the set changed at once.
func (s *Service) updateHardlinkIndex(ctx context.Context, instanceID int, cached *HardlinkIndex, torrents []qbt.Torrent, digest string) *HardlinkIndex {
	if cached == nil || len(torrents) == 0 {
		return nil
	}

	cached.crossScopeMu.Lock()
	previous := cached.buildState
	cached.crossScopeMu.Unlock()
	if previous == nil {
		return nil
	}

	startTime := time.Now()

	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentByHash[torrents[i].Hash] = torrents[i]
	}

	rescan, staleFileIDs := planTorrentRescan(previous.torrentInfoByHash, torrentByHash)

	// Bail before reading anything when the changes alone already exceed the budget,
	// so a mass addition does not read half the library off disk twice.
	if len(rescan)*hardlinkIncrementalChangeRatio > len(torrents) {
		return nil
	}

	// Torrents that arrived raise the link count on every file they hardlink to, so
	// the holders of those files need a second look. Their identities are only known
	// once the new torrents have been read, hence the two passes.
	scanned, ok := s.scanHashes(ctx, instanceID, torrentByHash, rescan)
	if !ok {
		return nil
	}
	for _, info := range scanned {
		for _, fileID := range info.fileIDs {
			staleFileIDs[fileID] = struct{}{}
		}
	}

	sharing := planSharingRescan(previous.torrentInfoByHash, torrentByHash, rescan, staleFileIDs)

	if (len(rescan)+len(sharing))*hardlinkIncrementalChangeRatio > len(torrents) {
		return nil
	}

	sharingScanned, ok := s.scanHashes(ctx, instanceID, torrentByHash, sharing)
	if !ok {
		return nil
	}

	torrentInfoByHash := make(map[string]*torrentFileInfo, len(torrents))
	for hash, info := range previous.torrentInfoByHash {
		if _, stillPresent := torrentByHash[hash]; stillPresent {
			torrentInfoByHash[hash] = info
		}
	}
	maps.Copy(torrentInfoByHash, scanned)
	maps.Copy(torrentInfoByHash, sharingScanned)

	// Deriving the counts from every retained scan discards whatever cross-instance
	// augmentation added to the previous state, which is why CrossScopeByHash starts
	// nil again and callers that need it re-augment.
	index := &HardlinkIndex{digest: digest}
	stats := index.applyLinkState(deriveLinkCounts(torrentInfoByHash))
	index.builtAt = time.Now()

	globalHardlinkIndexCache.mu.Lock()
	globalHardlinkIndexCache.indices[instanceID] = index
	globalHardlinkIndexCache.mu.Unlock()

	log.Debug().
		Int("instanceID", instanceID).
		Int("totalTorrents", len(torrents)).
		Int("rescanned", len(rescan)).
		Int("resharedRescanned", len(sharing)).
		Int("removed", len(previous.torrentInfoByHash)-len(torrentInfoByHash)+len(rescan)).
		Int("scopeComputed", len(index.ScopeByHash)).
		Int("deleteSafeDuplicateTorrents", len(index.DeleteSafeSignatureByHash)).
		Int("outsideLinks", stats.outsideLinks).
		Int("inaccessible", stats.inaccessible).
		Dur("buildTime", time.Since(startTime)).
		Msg("automations: hardlink index updated")

	return index
}

// scopeAfterRescan recomputes one torrent's hardlink scope from a fresh disk read,
// reusing the index's count of how many paths in the torrent set point at each file.
// It returns an empty scope when the torrent could not be fully inspected.
//
// A file the index has never seen counts as held by this torrent alone, so any extra
// link on it reads as a link outside the client. That is the answer that keeps the
// torrent out of the delete-safe groups.
func (idx *HardlinkIndex) scopeAfterRescan(info *torrentFileInfo) string {
	if idx == nil || info == nil {
		return ""
	}
	if !info.allAccessible || len(info.fileIDs) == 0 {
		return ""
	}

	// Cross-instance augmentation raises uniquePathCount in place, and a preview
	// request can run it while a scheduled apply verifies its delete candidates.
	idx.crossScopeMu.Lock()
	defer idx.crossScopeMu.Unlock()
	if idx.buildState == nil {
		return ""
	}

	// Same inside/outside partition as scopeForTorrent, or a "both" torrent could
	// never verify equal to its indexed scope and its delete would always be vetoed.
	hasInside, hasOutside := false, false
	for _, lf := range info.linkedFiles {
		uniquePaths := 1
		if tracker := idx.buildState.globalFileIDMap[lf.fileID]; tracker != nil && tracker.uniquePathCount > 1 {
			uniquePaths = tracker.uniquePathCount
		}

		if uniquePaths > 1 {
			hasInside = true
		}
		if lf.nlink > uint64(uniquePaths) { //nolint:gosec // uniquePaths is always positive
			hasOutside = true
		}
		if hasInside && hasOutside {
			break
		}
	}

	return hardlinkScope(hasInside, hasOutside)
}

// hardlinkScope names the partition cell for the two independent link facts.
func hardlinkScope(hasInside, hasOutside bool) string {
	switch {
	case hasInside && hasOutside:
		return HardlinkScopeBoth
	case hasOutside:
		return HardlinkScopeOutsideQBitTorrent
	case hasInside:
		return HardlinkScopeTorrentsOnly
	default:
		return HardlinkScopeNone
	}
}

// verifyDeleteCandidates re-reads the given torrents' files and returns the hashes whose
// hardlink scope no longer matches the index the rule decided on. Callers must not delete
// those: between the index build and now, links were added or removed on disk, or the
// files stopped being readable, and the rule's answer no longer describes them.
//
// This is the one place that must not trust the index. The index is allowed to lag by
// hardlinkIndexTTL for changes nothing reports, which is harmless for tagging and not
// harmless for deleting.
func (s *Service) verifyDeleteCandidates(ctx context.Context, instanceID int, index *HardlinkIndex, torrentByHash map[string]qbt.Torrent, hashes []string) map[string]string {
	if index == nil || len(hashes) == 0 {
		return nil
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		// Every candidate becomes unverifiable, and unverifiable must not be deleted.
		log.Warn().Err(err).Int("instanceID", instanceID).Int("candidates", len(hashes)).
			Msg("automations: failed to re-read delete candidates, holding the deletions")
		blocked := make(map[string]string, len(hashes))
		for _, hash := range hashes {
			blocked[hash] = "file list unavailable"
		}
		return blocked
	}

	var blocked map[string]string
	for _, hash := range hashes {
		expected, known := index.ScopeByHash[hash]
		if !known {
			// The rule already treated this torrent's scope as unknown, so there is no
			// hardlink answer to invalidate.
			continue
		}

		files, present := filesByHash[hash]
		actual := ""
		if present {
			actual = index.scopeAfterRescan(scanTorrentFiles(torrentByHash[hash], files))
		}

		if actual == expected {
			continue
		}

		if blocked == nil {
			blocked = make(map[string]string)
		}
		if actual == "" {
			blocked[hash] = "files no longer readable"
			continue
		}
		blocked[hash] = "hardlink scope changed from " + expected + " to " + actual
	}

	return blocked
}

// planTorrentRescan decides which torrents an incremental update must read off disk,
// and which files it can no longer trust the link counts for.
//
// A torrent that left takes its link counts with it: the files it held may have lost a
// link, or kept the link and lost the torrent that explained it. Either way the torrents
// still holding those files need a fresh read. A torrent whose save path moved points at
// different files now, so its previous scan is as stale as a removal plus an addition.
func planTorrentRescan(previous map[string]*torrentFileInfo, torrentByHash map[string]qbt.Torrent) (map[string]struct{}, map[hardlink.FileID]struct{}) {
	rescan := make(map[string]struct{})
	staleFileIDs := make(map[hardlink.FileID]struct{})

	for hash, info := range previous {
		torrent, stillPresent := torrentByHash[hash]
		if stillPresent && torrent.SavePath == info.savePath {
			continue
		}
		if stillPresent {
			rescan[hash] = struct{}{}
		}
		for _, fileID := range info.fileIDs {
			staleFileIDs[fileID] = struct{}{}
		}
	}

	for hash := range torrentByHash {
		if _, scanned := previous[hash]; !scanned {
			rescan[hash] = struct{}{}
		}
	}

	return rescan, staleFileIDs
}

// planSharingRescan returns the torrents that hold one of the untrusted files and are
// not already being re-read. These are the ones whose scope can flip without anything
// happening to them directly.
func planSharingRescan(
	previous map[string]*torrentFileInfo,
	torrentByHash map[string]qbt.Torrent,
	rescan map[string]struct{},
	staleFileIDs map[hardlink.FileID]struct{},
) map[string]struct{} {
	sharing := make(map[string]struct{})

	for hash, info := range previous {
		if _, alreadyScanned := rescan[hash]; alreadyScanned {
			continue
		}
		if _, stillPresent := torrentByHash[hash]; !stillPresent {
			continue
		}
		for _, fileID := range info.fileIDs {
			if _, stale := staleFileIDs[fileID]; stale {
				sharing[hash] = struct{}{}
				break
			}
		}
	}

	return sharing
}

// scanHashes reads the given torrents' files off disk. It reports false when the file
// lists could not be fetched, which must abandon the incremental update rather than
// leave those torrents on their previous scan: a torrent silently kept at stale link
// counts is exactly what an incremental update must not produce.
func (s *Service) scanHashes(ctx context.Context, instanceID int, torrentByHash map[string]qbt.Torrent, hashes map[string]struct{}) (map[string]*torrentFileInfo, bool) {
	if len(hashes) == 0 {
		return nil, true
	}

	list := slices.Collect(maps.Keys(hashes))
	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, list)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("hashes", len(list)).
			Msg("automations: failed to fetch files for hardlink index update, falling back to a full build")
		return nil, false
	}

	scanned := make(map[string]*torrentFileInfo, len(list))
	for _, hash := range list {
		files, present := filesByHash[hash]
		if !present {
			// A torrent with no file list has unknown links. Recording it as inspected
			// would claim "no hardlinks" for it, so leave it unknown instead.
			scanned[hash] = &torrentFileInfo{savePath: torrentByHash[hash].SavePath}
			continue
		}
		scanned[hash] = scanTorrentFiles(torrentByHash[hash], files)
	}

	return scanned, true
}

// buildHardlinkIndex constructs a fresh hardlink index by scanning all torrent files once.
// The complexity is inherent to the single-pass algorithm that avoids multiple filesystem scans.
//
//nolint:gocognit,gocyclo,funlen,revive // complexity is inherent to the single-pass design
func (s *Service) buildHardlinkIndex(ctx context.Context, instanceID int, torrents []qbt.Torrent, digest string) *HardlinkIndex {
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

	// Phase 1: read every torrent's files off disk, then derive the link counts.
	torrentInfoByHash := make(map[string]*torrentFileInfo, len(filesByHash))
	for hash, files := range filesByHash {
		torrentInfoByHash[hash] = scanTorrentFiles(torrentByHash[hash], files)
	}

	// Phase 2: derive scope, signatures and groups from the scan results.
	stats := index.applyLinkState(deriveLinkCounts(torrentInfoByHash))

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
		Int("outsideLinks", stats.outsideLinks).
		Int("inaccessible", stats.inaccessible).
		Int("invalidPaths", stats.invalidPaths).
		Dur("buildTime", time.Since(startTime)).
		Msg("automations: hardlink index built")

	return index
}

// indexStats counts what the derived maps left out, for the build log line.
type indexStats struct {
	outsideLinks int
	inaccessible int
	invalidPaths int
}

// applyLinkState fills the index's derived maps from a completed scan and adopts
// the state as its own. Every map is rebuilt from scratch, so it is equally valid
// after a full scan or after an incremental one that re-read a handful of torrents.
func (idx *HardlinkIndex) applyLinkState(state *hardlinkBuildState) indexStats {
	var stats indexStats

	idx.SignatureByHash = make(map[string]string)
	idx.GroupBySignature = make(map[string][]string)
	idx.DeleteSafeSignatureByHash = make(map[string]string)
	idx.DeleteSafeGroupBySignature = make(map[string][]string)
	// Torrents that could not be fully inspected are left out, so their scope reads as
	// unknown and HARDLINK_SCOPE conditions never match them.
	idx.ScopeByHash = computeScopeMap(state)

	for hash, info := range state.torrentInfoByHash {
		if info.hasInvalidPath {
			stats.invalidPaths++
		}

		scope, inspected := idx.ScopeByHash[hash]
		if !inspected {
			// Only count as inaccessible if not already counted as invalid path
			if !info.hasInvalidPath {
				stats.inaccessible++
			}
			continue
		}

		hasOutsideLinks := scope == HardlinkScopeOutsideQBitTorrent || scope == HardlinkScopeBoth

		// Only include in duplicate index if:
		// 1. Has hardlinks (otherwise not a duplicate candidate)
		if !info.hasHardlinks {
			continue // Not a hardlink duplicate candidate
		}

		if hasOutsideLinks {
			stats.outsideLinks++
		}

		// Compute signature once; feed broad grouping map plus delete-safe subset.
		sig := computeFileIDSignature(info.fileIDs)
		idx.SignatureByHash[hash] = sig
		idx.GroupBySignature[sig] = append(idx.GroupBySignature[sig], hash)
		if !hasOutsideLinks {
			idx.DeleteSafeSignatureByHash[hash] = sig
			idx.DeleteSafeGroupBySignature[sig] = append(idx.DeleteSafeGroupBySignature[sig], hash)
		}
	}

	pruneSingletonHardlinkGroups(idx.SignatureByHash, idx.GroupBySignature)
	pruneSingletonHardlinkGroups(idx.DeleteSafeSignatureByHash, idx.DeleteSafeGroupBySignature)

	// The state is always retained: an incremental update needs the per-torrent scan
	// results to avoid re-reading torrents whose links did not change, and cross-instance
	// augmentation needs the link counts. It costs a few MB for a 5000-torrent instance.
	idx.buildState = state

	return stats
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

// GetHardlinkScope returns the hardlink scope for a torrent (none, torrents_only, outside_qbittorrent, both).
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

	// Same for other incomplete scans: peer instances that failed to answer, or an
	// exhausted Lstat budget with deficits left (unscanned links indistinguishable
	// from external ones). A scope computed from incomplete evidence could match
	// HARDLINK_SCOPE_CROSS conditions wrongly; a nil CrossScopeByHash means those
	// conditions never match (unknown-safe).
	if stats.skipped > 0 || (stats.lstatCalls >= maxCrossInstanceLstatCalls && len(deficitSet) > 0) {
		log.Warn().Int("instanceID", instanceID).
			Int("skippedInstances", stats.skipped).
			Int("lstatCalls", stats.lstatCalls).
			Int("remainingDeficits", len(deficitSet)).
			Msg("automations: cross-instance scope incomplete, will retry on next run")
		return
	}

	// Recompute scope for all torrents using augmented counts. The state stays so the
	// next torrent set change can update incrementally; the counts this scan raised are
	// discarded there, because deriveLinkCounts rebuilds them from the per-torrent scans.
	index.CrossScopeByHash = computeScopeMap(state)

	var countNone, countTorrentsOnly, countOutside, countBoth int
	for _, scope := range index.CrossScopeByHash {
		switch scope {
		case HardlinkScopeNone:
			countNone++
		case HardlinkScopeTorrentsOnly:
			countTorrentsOnly++
		case HardlinkScopeOutsideQBitTorrent:
			countOutside++
		case HardlinkScopeBoth:
			countBoth++
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
		Int("crossScopeBoth", countBoth).
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

// finalizeCrossScope copies ScopeByHash to CrossScopeByHash.
// Used when Phase 2 cannot or does not need to scan other instances.
func (idx *HardlinkIndex) finalizeCrossScope(instanceID int, reason string) {
	idx.CrossScopeByHash = make(map[string]string, len(idx.ScopeByHash))
	maps.Copy(idx.CrossScopeByHash, idx.ScopeByHash)
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
		result[hash] = scopeForTorrent(info, state.globalFileIDMap)
	}
	return result
}

// scopeForTorrent partitions a torrent into none/torrents_only/outside_qbittorrent/both
// by checking each file for links inside the torrent set (inode shared by 2+ set paths)
// and outside it (nlink exceeds the paths accounted for in the set).
func scopeForTorrent(info *torrentFileInfo, fileIDMap map[hardlink.FileID]*fileIDTracker) string {
	hasInside, hasOutside := false, false
	for _, fileID := range info.fileIDs {
		tracker := fileIDMap[fileID]
		if tracker == nil || tracker.nlink <= 1 {
			continue // Not hard-linked
		}
		if tracker.uniquePathCount > 1 {
			hasInside = true
		}
		if tracker.nlink > uint64(tracker.uniquePathCount) { //nolint:gosec // uniquePathCount is always positive
			hasOutside = true
		}
		if hasInside && hasOutside {
			break
		}
	}
	return hardlinkScope(hasInside, hasOutside)
}

// Ensure syncManager implements the required interface
var _ interface {
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
} = (*qbittorrent.SyncManager)(nil)
