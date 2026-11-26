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

type freeSpaceLedger struct {
	startBytes       int64
	projectsDeletes  bool
	deleteMode       string
	includeHardlinks bool
	freedBytes       int64
	countedPaths     map[string]int64
	countedMembers   map[string]struct{}
}

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

func (s *Service) freeSpaceLedgers(ctx context.Context, instance *models.Instance, rules []*models.Automation, eligibleView bool) (map[int]*freeSpaceLedger, error) {
	return s.buildFreeSpaceLedgers(ctx, instance, rules, eligibleView, s.usageUnder)
}

func (s *Service) buildFreeSpaceLedgers(ctx context.Context, instance *models.Instance, rules []*models.Automation, eligibleView bool,
	usageUnder func(context.Context, *models.Instance, string) (int64, error)) (map[int]*freeSpaceLedger, error) {
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
		allowance := rule.FreeSpaceSource != nil && rule.FreeSpaceSource.Type == models.FreeSpaceSourceAllowance
		startBytes, read := startBySource[sourceKey]
		if !read {
			var err error
			if allowance {
				startBytes, err = usageUnder(ctx, instance, rule.FreeSpaceSource.Path)
			} else {
				startBytes, err = s.freeSpaceBytes(ctx, instance, rule.FreeSpaceSource)
			}
			if err != nil {
				log.Error().Err(err).Int("instanceID", instance.ID).Str("sourceKey", sourceKey).Msg("automations: failed to get free space for source")
				return nil, fmt.Errorf("failed to get free space for source %s: %w", sourceKey, err)
			}
			startBySource[sourceKey] = startBytes
		}
		if allowance {
			startBytes = rule.FreeSpaceSource.AllowanceBytes - startBytes
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

func (l *freeSpaceLedger) projects() bool {
	return l != nil && l.projectsDeletes
}

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

func (l *freeSpaceLedger) count(torrent qbt.Torrent, crossSeeds *crossSeedGroups, evalCtx *EvalContext) bool {
	if !l.projects() {
		return true
	}
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
	l.freedBytes += torrent.Size
	return true
}

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

func (l *freeSpaceLedger) countCrossSeedGroup(torrent qbt.Torrent, crossSeeds *crossSeedGroups, evalCtx *EvalContext) bool {
	if _, counted := l.countedMembers[torrent.Hash]; counted {
		return true
	}

	torrents := evalCtx.torrentsByHash
	if torrents == nil {
		trigger := torrent
		torrents = make(map[string]*qbt.Torrent, len(crossSeeds.members(torrent.Hash))+1)
		torrents[torrent.Hash] = &trigger
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
