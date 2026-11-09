// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// freeSpaceLedger is the count of one rule that reads FREE_SPACE: the free space of the
// source of the rule at the start of the run, and the Projected freed bytes of the rule.
type freeSpaceLedger struct {
	startBytes int64
	// projectsDeletes is true for a delete rule whose Condition reads FREE_SPACE, outside
	// the "eligible" preview view.
	projectsDeletes  bool
	deleteMode       string
	includeHardlinks bool
	freedBytes       int64
	// countedPaths holds the largest size counted for each file path of a plain
	// delete-with-files rule.
	countedPaths map[string]int64
	// countedMembers holds the cross-seed members counted by an include-cross-seeds rule.
	countedMembers map[string]struct{}
}

// newFreeSpaceLedger makes the ledger of the rule. In the "eligible" preview view the
// ledger counts nothing, so the Condition keeps the free space at the start.
func newFreeSpaceLedger(rule *models.Automation, startBytes int64, eligibleView bool) *freeSpaceLedger {
	ledger := &freeSpaceLedger{startBytes: startBytes}
	if eligibleView || rule.Conditions == nil {
		return ledger
	}
	deleteAction := rule.Conditions.Delete
	if deleteAction == nil || !deleteAction.Enabled || !ConditionUsesField(deleteAction.Condition, FieldFreeSpace) {
		return ledger
	}
	ledger.projectsDeletes = true
	ledger.deleteMode = getDeleteMode(rule)
	ledger.includeHardlinks = deleteAction.IncludeHardlinks
	ledger.countedPaths = make(map[string]int64)
	ledger.countedMembers = make(map[string]struct{})
	return ledger
}

// freeSpaceLedgers makes one ledger for each rule that reads FREE_SPACE in an enabled
// Action Condition or in its sort, keyed by rule ID. It reads the free space of each
// source one time. A rule that reads FREE_SPACE needs the instance.
func (s *Service) freeSpaceLedgers(ctx context.Context, instance *models.Instance, rules []*models.Automation, eligibleView bool) (map[int]*freeSpaceLedger, error) {
	ledgers := make(map[int]*freeSpaceLedger)
	startBySource := make(map[string]int64)
	for _, rule := range rules {
		if !ruleUsesCondition(rule, FieldFreeSpace) {
			continue
		}
		if instance == nil {
			return nil, errors.New("failed to get free space: the instance did not load")
		}
		sourceKey := GetFreeSpaceSourceKey(rule.FreeSpaceSource)
		startBytes, read := startBySource[sourceKey]
		if !read {
			var err error
			startBytes, err = s.freeSpaceBytes(ctx, instance, rule.FreeSpaceSource)
			if err != nil {
				log.Error().Err(err).Int("instanceID", instance.ID).Str("sourceKey", sourceKey).Msg("automations: failed to get free space for source")
				return nil, fmt.Errorf("failed to get free space for source %s: %w", sourceKey, err)
			}
			startBySource[sourceKey] = startBytes
		}
		ledgers[rule.ID] = newFreeSpaceLedger(rule, startBytes, eligibleView)
	}
	return ledgers, nil
}

func (l *freeSpaceLedger) startFreeSpace() int64 {
	return l.startBytes
}

func (l *freeSpaceLedger) projectedFreeSpace() int64 {
	return l.startBytes + l.freedBytes
}

func (l *freeSpaceLedger) projectedFreedBytes() int64 {
	if l == nil {
		return 0
	}
	return l.freedBytes
}

// projects reports whether the ledger counts the Delete triggers of its rule.
func (l *freeSpaceLedger) projects() bool {
	return l != nil && l.projectsDeletes
}

// hardlinks returns the hardlink index that the ledger follows when it counts a cross-seed
// group, or nil when it follows none.
func (l *freeSpaceLedger) hardlinks(evalCtx *EvalContext) *HardlinkIndex {
	if !l.projects() || l.deleteMode != DeleteModeWithFilesIncludeCrossSeeds || !l.includeHardlinks {
		return nil
	}
	return evalCtx.hardlinkIndex
}

func (l *freeSpaceLedger) reset() {
	if l == nil {
		return
	}
	l.freedBytes = 0
	clear(l.countedPaths)
	clear(l.countedMembers)
}

// count adds the bytes that the delete of one Delete trigger frees. A Shared file counts
// one time. It returns false when it cannot estimate the bytes of the torrent, and then
// counts nothing. A ledger that does not project counts nothing and returns true.
func (l *freeSpaceLedger) count(torrent qbt.Torrent, crossSeeds *crossSeedGroups, evalCtx *EvalContext) bool {
	if !l.projects() {
		return true
	}
	// Keep-files mode, and preserve mode when a Shared file stays, free nothing.
	if !deleteFreesSpace(l.deleteMode, torrent, crossSeeds, evalCtx.CrossSeedFilesByHash) {
		return true
	}
	switch l.deleteMode {
	case DeleteModeWithFilesPreserveCrossSeeds:
		if _, keep := otherInstanceKeepsFiles(evalCtx, torrent.Hash); keep {
			return true
		}
	case DeleteModeWithFilesIncludeCrossSeeds:
		return l.countCrossSeedGroup(torrent, crossSeeds, evalCtx)
	case DeleteModeWithFiles:
		if len(crossSeeds.members(torrent.Hash)) > 0 {
			return l.countFiles(torrent, evalCtx.CrossSeedFilesByHash[torrent.Hash])
		}
	}
	// The torrent shares no file: preserve mode verified that above, and a
	// file delete outside a group has nothing to share.
	l.freedBytes += torrent.Size
	return true
}

// countFiles counts each file path of the torrent one time, at its largest size.
func (l *freeSpaceLedger) countFiles(torrent qbt.Torrent, files qbt.TorrentFiles) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		path := resolvedTorrentFilePath(torrent, file.Name)
		if counted := l.countedPaths[path]; file.Size > counted {
			l.freedBytes += file.Size - counted
			l.countedPaths[path] = file.Size
		}
	}
	return true
}

// countCrossSeedGroup counts the verified group of the torrent one time, with its
// hardlink copies when the rule includes hardlinks.
func (l *freeSpaceLedger) countCrossSeedGroup(torrent qbt.Torrent, crossSeeds *crossSeedGroups, evalCtx *EvalContext) bool {
	if _, counted := l.countedMembers[torrent.Hash]; counted {
		return true
	}

	torrents := evalCtx.torrentsByHash
	if torrents == nil {
		torrents = make(map[string]qbt.Torrent, len(crossSeeds.members(torrent.Hash))+1)
		torrents[torrent.Hash] = torrent
		for _, member := range crossSeeds.members(torrent.Hash) {
			torrents[member.Hash] = member
		}
	}
	hardlinks := l.hardlinks(evalCtx)
	expanded, ok := deleteCrossSeedGroup(torrent, torrents, crossSeeds, hardlinks,
		l.countedMembers, cachedTorrentFilesFetcher(evalCtx.CrossSeedFilesByHash))
	if !ok {
		return false
	}
	// The selection keeps the files of the whole group, so the group frees nothing.
	if _, keep := groupKeepsFilesForOtherInstances(evalCtx, slices.Collect(maps.Keys(expanded.expandedSet))); keep {
		maps.Copy(l.countedMembers, expanded.expandedSet)
		return true
	}
	bytes, ok := verifiedCrossSeedFileBytes(torrents, expanded.expandedSet, evalCtx.CrossSeedFilesByHash, hardlinks)
	if !ok {
		return false
	}
	maps.Copy(l.countedMembers, expanded.expandedSet)
	l.freedBytes += bytes
	return true
}
