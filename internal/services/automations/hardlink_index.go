// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/pkg/hardlink"
)

const hardlinkIndexTTL = 10 * time.Minute

const hardlinkIncrementalChangeRatio = 2

type HardlinkIndex struct {
	SignatureByHash map[string]string

	GroupBySignature map[string][]string

	DeleteSafeSignatureByHash map[string]string

	DeleteSafeGroupBySignature map[string][]string

	ScopeByHash map[string]string

	CrossScopeByHash map[string]string

	builtAt time.Time

	digest string

	routedDigest string

	buildState *hardlinkBuildState

	crossFileIDMap map[fsops.FileKey]*fileIDTracker

	crossScopeMu sync.Mutex
}

type hardlinkBuildState struct {
	instance          *models.Instance
	globalFileIDMap   map[fsops.FileKey]*fileIDTracker
	seenPaths         map[string]struct{}
	torrentInfoByHash map[string]*torrentFileInfo
	routedFiles       []linkedFile
}

func (state *hardlinkBuildState) key(id hardlink.FileID) (fsops.FileKey, bool) {
	return fsops.FileKeyOf(id, state.instance)
}

func (state *hardlinkBuildState) hasRoutedLink(info *torrentFileInfo) bool {
	return slices.ContainsFunc(info.linkedFiles, func(lf linkedFile) bool {
		key, ok := state.key(lf.fileID)
		tracker := state.globalFileIDMap[key]
		return ok && tracker != nil && tracker.routed
	})
}

type linkedFile struct {
	path   string
	fileID hardlink.FileID
	nlink  uint64
}

type torrentFileInfo struct {
	savePath       string
	fileIDs        []hardlink.FileID
	linkedFiles    []linkedFile
	allAccessible  bool
	hasHardlinks   bool
	hasInvalidPath bool
}

type hardlinkIndexCache struct {
	mu      sync.RWMutex
	indices map[int]*HardlinkIndex
	sf      singleflight.Group
}

var globalHardlinkIndexCache = &hardlinkIndexCache{
	indices: make(map[int]*HardlinkIndex),
}

func (s *Service) GetHardlinkIndex(ctx context.Context, instanceID int, torrents []*qbt.Torrent) *HardlinkIndex {
	if s == nil || s.filesReader == nil {
		return nil
	}

	var routed *crossseedroute.RoutedTorrents
	if s.crossSeedRoutes != nil {
		var err error
		if routed, err = s.crossSeedRoutes.ListRouted(ctx, s.routedTorrents, instanceID); err != nil {
			logRoutedUnavailable(instanceID, err)
			return &HardlinkIndex{}
		}
	}

	currentDigest := computeTorrentSetDigest(torrents)
	var routedDigest string
	if routed != nil {
		routedDigest = computeTorrentSetDigest(torrentRefs(routed.Torrents))
	}

	globalHardlinkIndexCache.mu.RLock()
	cached := globalHardlinkIndexCache.indices[instanceID]
	globalHardlinkIndexCache.mu.RUnlock()

	fresh := cached != nil && time.Since(cached.builtAt) < hardlinkIndexTTL && cached.routedDigest == routedDigest
	if fresh && cached.digest == currentDigest {
		return cached
	}

	buildCtx := qbittorrent.WithFilesUntilChangedOrStale(ctx)

	key := strconv.Itoa(instanceID) + ":" + currentDigest + ":" + routedDigest
	result, err, _ := globalHardlinkIndexCache.sf.Do(key, func() (any, error) {
		if fresh {
			if updated := s.updateHardlinkIndex(buildCtx, instanceID, cached, torrents, currentDigest); updated != nil {
				return updated, nil
			}
		}
		return s.buildHardlinkIndex(buildCtx, instanceID, torrents, currentDigest, routed, routedDigest), nil
	})
	if err != nil {
		return nil
	}

	idx, ok := result.(*HardlinkIndex)
	if !ok {
		return nil
	}

	if idx.digest != currentDigest || idx.routedDigest != routedDigest {
		return s.buildHardlinkIndex(buildCtx, instanceID, torrents, currentDigest, routed, routedDigest)
	}
	return idx
}

func logRoutedUnavailable(instanceID int, err error) {
	log.Warn().Err(err).Int("instanceID", instanceID).
		Msg("automations: cannot read the routed torrents of the Cross-seed instance, hardlink scope stays unknown for this run")
}

func computeTorrentSetDigest(torrents []*qbt.Torrent) string {
	if len(torrents) == 0 {
		return ""
	}

	indices := make([]int, len(torrents))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		ti, tj := torrents[indices[i]], torrents[indices[j]]
		if ti.Hash != tj.Hash {
			return ti.Hash < tj.Hash
		}
		return ti.SavePath < tj.SavePath
	})

	h := sha256.New()
	for _, idx := range indices {
		t := torrents[idx]
		io.WriteString(h, t.Hash) //nolint:errcheck // hash.Hash.Write never returns error
		h.Write([]byte{0})
		io.WriteString(h, t.SavePath) //nolint:errcheck // hash.Hash.Write never returns error
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

type fileIDTracker struct {
	nlink           uint64
	uniquePathCount int
	routed          bool
}

func scanTorrentFiles(ctx context.Context, backend fsops.Backend, torrent qbt.Torrent, files qbt.TorrentFiles) *torrentFileInfo {
	info := &torrentFileInfo{
		savePath:      torrent.SavePath,
		fileIDs:       make([]hardlink.FileID, 0, len(files)),
		allAccessible: true,
	}

	d := backend.Paths()
	if torrent.SavePath == "" || !d.IsAbs(torrent.SavePath) {
		info.allAccessible = false
		info.hasInvalidPath = true
		return info
	}

	for _, f := range files {
		if ctx.Err() != nil {
			info.allAccessible = false
			return info
		}

		fullPath, ok := buildFullPath(d, torrent.SavePath, f.Name)
		if !ok || !isPathInsideBase(d, torrent.SavePath, fullPath) {
			info.allAccessible = false
			info.hasInvalidPath = true
			continue
		}

		lstatInfo, err := backend.Lstat(ctx, fullPath)
		if err != nil {
			if f.Priority == 0 && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			info.allAccessible = false
			continue
		}
		if !lstatInfo.Mode.IsRegular() {
			continue
		}

		fileID := lstatInfo.FileID
		nlink := lstatInfo.Nlinks
		if fileID.IsZero() {
			info.allAccessible = false
			continue
		}

		info.fileIDs = append(info.fileIDs, fileID)
		if nlink > 1 {
			info.hasHardlinks = true
			info.linkedFiles = append(info.linkedFiles, linkedFile{path: fullPath, fileID: fileID, nlink: nlink})
		}
	}

	return info
}

func deriveLinkCounts(instance *models.Instance, torrentInfoByHash map[string]*torrentFileInfo, routedFiles []linkedFile) *hardlinkBuildState {
	globalFileIDMap := make(map[fsops.FileKey]*fileIDTracker)
	seenPaths := make(map[string]struct{})

	for _, info := range torrentInfoByHash {
		for _, lf := range info.linkedFiles {
			key, ok := fsops.FileKeyOf(lf.fileID, instance)
			if !ok {
				continue
			}
			tracker := globalFileIDMap[key]
			if tracker == nil {
				tracker = &fileIDTracker{nlink: lf.nlink}
				globalFileIDMap[key] = tracker
			} else if lf.nlink > tracker.nlink {
				tracker.nlink = lf.nlink
			}

			if _, seen := seenPaths[lf.path]; !seen {
				seenPaths[lf.path] = struct{}{}
				tracker.uniquePathCount++
			}
		}
	}

	for _, lf := range routedFiles {
		key, ok := fsops.FileKeyOf(lf.fileID, instance)
		tracker := globalFileIDMap[key]
		if !ok || tracker == nil {
			continue
		}
		tracker.routed = true
		if _, seen := seenPaths[lf.path]; !seen {
			seenPaths[lf.path] = struct{}{}
			tracker.uniquePathCount++
		}
	}

	return &hardlinkBuildState{
		instance:          instance,
		globalFileIDMap:   globalFileIDMap,
		seenPaths:         seenPaths,
		torrentInfoByHash: torrentInfoByHash,
		routedFiles:       routedFiles,
	}
}

func (s *Service) updateHardlinkIndex(ctx context.Context, instanceID int, cached *HardlinkIndex, torrents []*qbt.Torrent, digest string) *HardlinkIndex {
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

	torrentByHash := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentByHash[torrents[i].Hash] = torrents[i]
	}

	rescan, staleFileIDs := planTorrentRescan(previous, torrentByHash)

	if len(rescan)*hardlinkIncrementalChangeRatio > len(torrents) {
		return nil
	}

	scanned, ok := s.scanHashes(ctx, instanceID, torrentByHash, rescan)
	if !ok {
		return nil
	}
	for _, info := range scanned {
		for _, fileID := range info.fileIDs {
			if key, ok := previous.key(fileID); ok {
				staleFileIDs[key] = struct{}{}
			}
		}
	}

	sharing := planSharingRescan(previous, torrentByHash, rescan, staleFileIDs)

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

	index := &HardlinkIndex{digest: digest, routedDigest: cached.routedDigest, builtAt: cached.builtAt}
	stats := index.applyLinkState(deriveLinkCounts(previous.instance, torrentInfoByHash, previous.routedFiles))

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

func scopeAfterRescan(info *torrentFileInfo, instance *models.Instance, fileIDMap map[fsops.FileKey]*fileIDTracker) string {
	if info == nil || fileIDMap == nil {
		return ""
	}
	if !info.allAccessible || len(info.fileIDs) == 0 {
		return ""
	}

	hasInside, hasOutside := false, false
	for _, lf := range info.linkedFiles {
		uniquePaths := 1
		key, ok := fsops.FileKeyOf(lf.fileID, instance)
		if tracker := fileIDMap[key]; ok && tracker != nil && tracker.uniquePathCount > 1 {
			uniquePaths = tracker.uniquePathCount
		}

		if uniquePaths > 1 {
			hasInside = true
		}
		if lf.nlink > uint64(uniquePaths) {
			hasOutside = true
		}
		if hasInside && hasOutside {
			break
		}
	}

	return hardlinkScope(hasInside, hasOutside)
}

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

func (s *Service) verifyDeleteCandidates(ctx context.Context, instanceID int, index *HardlinkIndex, torrentByHash map[string]*qbt.Torrent, candidates map[string]hardlinkUse) map[string]string {
	if len(candidates) == 0 {
		return nil
	}
	hashes := slices.Collect(maps.Keys(candidates))
	if index == nil {
		blocked := make(map[string]string, len(hashes))
		for _, hash := range hashes {
			blocked[hash] = "hardlink scope unknown"
		}
		return blocked
	}

	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("candidates", len(hashes)).
			Msg("automations: failed to re-read delete candidates, holding the deletions")
		blocked := make(map[string]string, len(hashes))
		for _, hash := range hashes {
			blocked[hash] = "file list unavailable"
		}
		return blocked
	}

	backend, _, err := s.backendPool.Require(ctx, instanceID, models.CapabilityTrustedIdentity)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("candidates", len(hashes)).
			Msg("automations: failed to get backend to re-read delete candidates, holding the deletions")
		blocked := make(map[string]string, len(hashes))
		for _, hash := range hashes {
			blocked[hash] = "filesystem backend unavailable"
		}
		return blocked
	}

	index.crossScopeMu.Lock()
	var singleCounts map[fsops.FileKey]*fileIDTracker
	var instance *models.Instance
	if index.buildState != nil {
		singleCounts, instance = index.buildState.globalFileIDMap, index.buildState.instance
	}
	crossScopes, crossCounts := index.CrossScopeByHash, index.crossFileIDMap
	index.crossScopeMu.Unlock()

	var blocked map[string]string
	for hash, use := range candidates {
		var info *torrentFileInfo
		if files, present := filesByHash[hash]; present {
			info = scanTorrentFiles(ctx, backend, *torrentByHash[hash], files)
		}
		check := func(scopes map[string]string, counts map[fsops.FileKey]*fileIDTracker) string {
			expected, known := scopes[hash]
			if !known {
				return "hardlink scope unknown"
			}
			actual := scopeAfterRescan(info, instance, counts)
			switch actual {
			case expected:
				return ""
			case "":
				return "files no longer readable"
			default:
				return "hardlink scope changed from " + expected + " to " + actual
			}
		}

		var reason string
		if use.single {
			reason = check(index.ScopeByHash, singleCounts)
		}
		if reason == "" && use.cross {
			reason = check(crossScopes, crossCounts)
		}
		if reason == "" {
			continue
		}
		if blocked == nil {
			blocked = make(map[string]string)
		}
		blocked[hash] = reason
	}

	return blocked
}

func planTorrentRescan(previous *hardlinkBuildState, torrentByHash map[string]*qbt.Torrent) (map[string]struct{}, map[fsops.FileKey]struct{}) {
	rescan := make(map[string]struct{})
	staleFileIDs := make(map[fsops.FileKey]struct{})

	for hash, info := range previous.torrentInfoByHash {
		torrent, stillPresent := torrentByHash[hash]
		if stillPresent && torrent.SavePath == info.savePath {
			continue
		}
		if stillPresent {
			rescan[hash] = struct{}{}
		}
		for _, fileID := range info.fileIDs {
			if key, ok := previous.key(fileID); ok {
				staleFileIDs[key] = struct{}{}
			}
		}
	}

	for hash := range torrentByHash {
		if _, scanned := previous.torrentInfoByHash[hash]; !scanned {
			rescan[hash] = struct{}{}
		}
	}

	return rescan, staleFileIDs
}

func planSharingRescan(
	previous *hardlinkBuildState,
	torrentByHash map[string]*qbt.Torrent,
	rescan map[string]struct{},
	staleFileIDs map[fsops.FileKey]struct{},
) map[string]struct{} {
	sharing := make(map[string]struct{})

	for hash, info := range previous.torrentInfoByHash {
		if _, alreadyScanned := rescan[hash]; alreadyScanned {
			continue
		}
		if _, stillPresent := torrentByHash[hash]; !stillPresent {
			continue
		}
		for _, fileID := range info.fileIDs {
			key, ok := previous.key(fileID)
			if _, stale := staleFileIDs[key]; ok && stale {
				sharing[hash] = struct{}{}
				break
			}
		}
	}

	return sharing
}

func (s *Service) scanHashes(ctx context.Context, instanceID int, torrentByHash map[string]*qbt.Torrent, hashes map[string]struct{}) (map[string]*torrentFileInfo, bool) {
	if len(hashes) == 0 {
		return nil, true
	}

	list := slices.Collect(maps.Keys(hashes))
	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, list)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("hashes", len(list)).
			Msg("automations: failed to fetch files for hardlink index update, falling back to a full build")
		return nil, false
	}

	backend, _, err := s.backendPool.Require(ctx, instanceID, models.CapabilityTrustedIdentity)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("hashes", len(list)).
			Msg("automations: failed to get backend for hardlink index update, falling back to a full build")
		return nil, false
	}

	scanned := make(map[string]*torrentFileInfo, len(list))
	for _, hash := range list {
		if ctx.Err() != nil {
			return nil, false
		}
		files, present := filesByHash[hash]
		if !present {
			scanned[hash] = &torrentFileInfo{savePath: torrentByHash[hash].SavePath}
			continue
		}
		scanned[hash] = scanTorrentFiles(ctx, backend, *torrentByHash[hash], files)
	}
	if ctx.Err() != nil {
		return nil, false
	}

	return scanned, true
}

func (s *Service) buildHardlinkIndex(ctx context.Context, instanceID int, torrents []*qbt.Torrent, digest string, routed *crossseedroute.RoutedTorrents, routedDigest string) *HardlinkIndex {
	startTime := time.Now()
	index := &HardlinkIndex{
		SignatureByHash:            make(map[string]string),
		GroupBySignature:           make(map[string][]string),
		DeleteSafeSignatureByHash:  make(map[string]string),
		DeleteSafeGroupBySignature: make(map[string][]string),
		ScopeByHash:                make(map[string]string),
		digest:                     digest,
		routedDigest:               routedDigest,
	}

	if len(torrents) == 0 {
		index.builtAt = time.Now()
		globalHardlinkIndexCache.mu.Lock()
		globalHardlinkIndexCache.indices[instanceID] = index
		globalHardlinkIndexCache.mu.Unlock()
		return index
	}

	backend, instance, err := s.backendPool.Require(ctx, instanceID, models.CapabilityTrustedIdentity)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("automations: failed to get backend for hardlink index")
		index.builtAt = time.Now()
		return index
	}

	hashes := make([]string, 0, len(torrents))
	torrentByHash := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		hashes = append(hashes, torrents[i].Hash)
		torrentByHash[torrents[i].Hash] = torrents[i]
	}

	filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).
			Msg("automations: failed to fetch files for hardlink index build")

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return index
		}

		index.builtAt = time.Now()
		globalHardlinkIndexCache.mu.Lock()
		globalHardlinkIndexCache.indices[instanceID] = index
		globalHardlinkIndexCache.mu.Unlock()
		return index
	}

	torrentInfoByHash := make(map[string]*torrentFileInfo, len(filesByHash))
	for hash, files := range filesByHash {
		if ctx.Err() != nil {
			return index
		}
		torrentInfoByHash[hash] = scanTorrentFiles(ctx, backend, *torrentByHash[hash], files)
	}
	if ctx.Err() != nil {
		return index
	}

	var routedFiles []linkedFile
	if routed != nil {
		if err := routed.LoadFiles(ctx, s.routedTorrents); err != nil {
			logRoutedUnavailable(instanceID, err)
			return index
		}
		for _, torrent := range routed.Torrents {
			routedFiles = append(routedFiles, scanTorrentFiles(ctx, backend, torrent, routed.Files[torrent.Hash]).linkedFiles...)
		}
		if ctx.Err() != nil {
			return index
		}
	}

	stats := index.applyLinkState(deriveLinkCounts(instance, torrentInfoByHash, routedFiles))

	index.builtAt = time.Now()

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

type indexStats struct {
	outsideLinks int
	inaccessible int
	invalidPaths int
}

func (idx *HardlinkIndex) applyLinkState(state *hardlinkBuildState) indexStats {
	var stats indexStats

	idx.SignatureByHash = make(map[string]string)
	idx.GroupBySignature = make(map[string][]string)
	idx.DeleteSafeSignatureByHash = make(map[string]string)
	idx.DeleteSafeGroupBySignature = make(map[string][]string)
	idx.ScopeByHash = computeScopeMap(state)

	for hash, info := range state.torrentInfoByHash {
		if info.hasInvalidPath {
			stats.invalidPaths++
		}

		scope, inspected := idx.ScopeByHash[hash]
		if !inspected {
			if !info.hasInvalidPath {
				stats.inaccessible++
			}
			continue
		}

		hasOutsideLinks := scope == HardlinkScopeOutsideQBitTorrent || scope == HardlinkScopeBoth

		if !info.hasHardlinks {
			continue
		}

		if hasOutsideLinks {
			stats.outsideLinks++
		}

		sig := computeFileIDSignature(info.fileIDs)
		idx.SignatureByHash[hash] = sig
		idx.GroupBySignature[sig] = append(idx.GroupBySignature[sig], hash)
		if !hasOutsideLinks && !state.hasRoutedLink(info) {
			idx.DeleteSafeSignatureByHash[hash] = sig
			idx.DeleteSafeGroupBySignature[sig] = append(idx.DeleteSafeGroupBySignature[sig], hash)
		}
	}

	pruneSingletonHardlinkGroups(idx.SignatureByHash, idx.GroupBySignature)
	pruneSingletonHardlinkGroups(idx.DeleteSafeSignatureByHash, idx.DeleteSafeGroupBySignature)

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

func computeFileIDSignature(fileIDs []hardlink.FileID) string {
	if len(fileIDs) == 0 {
		return ""
	}

	sorted := make([]hardlink.FileID, len(fileIDs))
	copy(sorted, fileIDs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Less(sorted[j])
	})

	h := sha256.New()
	for _, fid := range sorted {
		fid.WriteToHash(h)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func isPathInsideBase(d fsops.PathDialect, basePath, fullPath string) bool {
	cleanBase := d.Clean(basePath)
	cleanFull := d.Clean(fullPath)

	rel, err := d.Rel(cleanBase, cleanFull)
	if err != nil {
		return false
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+d.Separator()) {
		return false
	}

	return true
}

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

func (s *Service) augmentCrossInstanceScope(ctx context.Context, instanceID int, index *HardlinkIndex) {
	if index == nil || index.buildState == nil {
		return
	}
	state := index.buildState
	phase2Start := time.Now()

	augmented := collectDeficitFileIDs(state)
	deficitSet := maps.Clone(augmented)

	if len(deficitSet) == 0 {
		index.finalizeCrossScope(instanceID, "no deficits")
		return
	}

	if s.instanceStore == nil || s.filesReader == nil {
		log.Warn().Int("instanceID", instanceID).
			Msg("automations: instanceStore or files reader unavailable for cross-scope, falling back to single-instance scope")
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

	if ctx.Err() != nil {
		log.Warn().Int("instanceID", instanceID).
			Msg("automations: cross-instance scope aborted (context cancelled), will retry on next run")
		return
	}

	if stats.skipped > 0 || (stats.lstatCalls >= maxCrossInstanceLstatCalls && len(deficitSet) > 0) {
		log.Warn().Int("instanceID", instanceID).
			Int("skippedInstances", stats.skipped).
			Int("lstatCalls", stats.lstatCalls).
			Int("remainingDeficits", len(deficitSet)).
			Msg("automations: cross-instance scope incomplete, will retry on next run")
		return
	}

	crossCounts := maps.Clone(state.globalFileIDMap)
	maps.Copy(crossCounts, augmented)
	index.crossFileIDMap = crossCounts
	index.CrossScopeByHash = computeScopeMap(&hardlinkBuildState{
		instance:          state.instance,
		globalFileIDMap:   crossCounts,
		torrentInfoByHash: state.torrentInfoByHash,
	})

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

func collectDeficitFileIDs(state *hardlinkBuildState) map[fsops.FileKey]*fileIDTracker {
	deficitSet := make(map[fsops.FileKey]*fileIDTracker)
	for fileID, tracker := range state.globalFileIDMap {
		if tracker.nlink > uint64(tracker.uniquePathCount) {
			deficitSet[fileID] = &fileIDTracker{nlink: tracker.nlink, uniquePathCount: tracker.uniquePathCount}
		}
	}
	return deficitSet
}

func (idx *HardlinkIndex) finalizeCrossScope(instanceID int, reason string) {
	idx.CrossScopeByHash = make(map[string]string, len(idx.ScopeByHash))
	maps.Copy(idx.CrossScopeByHash, idx.ScopeByHash)
	idx.crossFileIDMap = idx.buildState.globalFileIDMap
	if reason != "" {
		log.Debug().Int("instanceID", instanceID).
			Msgf("automations: cross-instance scope matches single-instance (%s)", reason)
	}
}

func (s *Service) listCrossScopeInstances(ctx context.Context, instanceID int) ([]int, error) {
	instances, err := s.instanceStore.List(ctx)
	if err != nil {
		return nil, err
	}
	var result []int
	for _, inst := range instances {
		if inst.ID != instanceID && inst.IsActive && models.FilesystemCapabilitiesOf(inst).TrustedIdentity {
			result = append(result, inst.ID)
		}
	}
	return result, nil
}

type crossScanStats struct {
	scanned, skipped int
	deficitBefore    int
	lstatCalls       int
	lstatErrors      int
}

const maxCrossInstanceLstatCalls = 500_000

func (s *Service) scanOtherInstancesForDeficits(
	ctx context.Context,
	instanceID int,
	otherInstances []int,
	deficitSet map[fsops.FileKey]*fileIDTracker,
	state *hardlinkBuildState,
) crossScanStats {
	stats := crossScanStats{deficitBefore: len(deficitSet)}
	crossSeen := make(map[string]struct{})

	for _, otherID := range otherInstances {
		if ctx.Err() != nil || len(deficitSet) == 0 || stats.lstatCalls >= maxCrossInstanceLstatCalls {
			break
		}

		backend, otherInstance, backendErr := s.backendPool.Require(ctx, otherID, models.CapabilityTrustedIdentity)
		if backendErr != nil {
			log.Warn().Err(backendErr).Int("instanceID", instanceID).Int("otherInstanceID", otherID).
				Msg("automations: failed to get backend for cross-scope scan, skipping instance")
			stats.skipped++
			continue
		}
		d := backend.Paths()

		views, err := s.filesReader.GetCachedInstanceTorrents(ctx, otherID)
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

		filesByHash, err := s.filesReader.GetTorrentFilesBatch(ctx, otherID, otherHashes)
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
			if savePath == "" || !d.IsAbs(savePath) {
				continue
			}

			for _, f := range files {
				if len(deficitSet) == 0 || stats.lstatCalls >= maxCrossInstanceLstatCalls {
					break
				}

				fullPath, ok := buildFullPath(d, savePath, f.Name)
				if !ok || !isPathInsideBase(d, savePath, fullPath) {
					continue
				}
				if _, seen := state.seenPaths[fullPath]; seen {
					continue
				}
				if _, seen := crossSeen[fullPath]; seen {
					continue
				}

				stats.lstatCalls++

				lstatInfo, err := backend.Lstat(ctx, fullPath)
				if err != nil {
					stats.lstatErrors++
					continue
				}
				if !lstatInfo.Mode.IsRegular() {
					continue
				}

				fileID := lstatInfo.FileID
				if fileID.IsZero() {
					stats.lstatErrors++
					continue
				}

				key, ok := fsops.FileKeyOf(fileID, otherInstance)
				tracker, isDeficit := deficitSet[key]
				if !ok || !isDeficit {
					continue
				}

				crossSeen[fullPath] = struct{}{}
				tracker.uniquePathCount++

				if tracker.nlink <= uint64(tracker.uniquePathCount) {
					delete(deficitSet, key)
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

func computeScopeMap(state *hardlinkBuildState) map[string]string {
	result := make(map[string]string, len(state.torrentInfoByHash))
	for hash, info := range state.torrentInfoByHash {
		if !info.allAccessible || len(info.fileIDs) == 0 {
			continue
		}
		result[hash] = scopeForTorrent(info, state)
	}
	return result
}

func scopeForTorrent(info *torrentFileInfo, state *hardlinkBuildState) string {
	hasInside, hasOutside := false, false
	for _, fileID := range info.fileIDs {
		key, ok := state.key(fileID)
		tracker := state.globalFileIDMap[key]
		if !ok || tracker == nil || tracker.nlink <= 1 {
			continue
		}
		if tracker.uniquePathCount > 1 {
			hasInside = true
		}
		if tracker.nlink > uint64(tracker.uniquePathCount) {
			hasOutside = true
		}
		if hasInside && hasOutside {
			break
		}
	}
	return hardlinkScope(hasInside, hasOutside)
}
