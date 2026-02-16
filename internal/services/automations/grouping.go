// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	groupKeyContentPath       = "contentPath"
	groupKeySavePath          = "savePath"
	groupKeyEffectiveName     = "effectiveName"
	groupKeyContentType       = "contentType"
	groupKeyTrackerDomain     = "tracker"
	groupKeyRlsSource         = "rlsSource"
	groupKeyRlsResolution     = "rlsResolution"
	groupKeyRlsCodec          = "rlsCodec"
	groupKeyRlsHDR            = "rlsHDR"
	groupKeyRlsAudio          = "rlsAudio"
	groupKeyRlsChannels       = "rlsChannels"
	groupKeyRlsGroup          = "rlsGroup"
	groupKeyHardlinkSignature = "hardlinkSignature"
)

const (
	groupAmbiguousVerifyOverlap = "verify_overlap"
	groupAmbiguousSkip          = "skip"
)

// Built-in group IDs.
const (
	GroupCrossSeedContentPath     = "cross_seed_content_path"
	GroupCrossSeedContentSavePath = "cross_seed_content_save_path"
	GroupReleaseItem              = "release_item"
	GroupTrackerReleaseItem       = "tracker_release_item"
	GroupHardlinkSignature        = "hardlink_signature"
)

type groupIndex struct {
	groupID string

	keyByHash     map[string]string
	hashesByKey   map[string][]string
	sizeByHash    map[string]int
	ambiguousKeys map[string]struct{}
}

func (g *groupIndex) KeyForHash(hash string) string {
	if g == nil || g.keyByHash == nil {
		return ""
	}
	return g.keyByHash[hash]
}

func (g *groupIndex) MembersForHash(hash string) []string {
	if g == nil || g.hashesByKey == nil {
		return nil
	}
	key := g.KeyForHash(hash)
	if key == "" {
		return nil
	}
	return g.hashesByKey[key]
}

func (g *groupIndex) SizeForHash(hash string) int {
	if g == nil || g.sizeByHash == nil {
		return 0
	}
	return g.sizeByHash[hash]
}

func (g *groupIndex) IsAmbiguousForHash(hash string) bool {
	if g == nil || g.ambiguousKeys == nil {
		return false
	}
	key := g.KeyForHash(hash)
	if key == "" {
		return false
	}
	_, ok := g.ambiguousKeys[key]
	return ok
}

func activateRuleGrouping(evalCtx *EvalContext, rule *models.Automation, torrents []qbt.Torrent, sm *qbittorrent.SyncManager) {
	if evalCtx == nil {
		return
	}
	evalCtx.ActiveRuleID = 0
	evalCtx.activeGroupIndex = nil

	if rule == nil || rule.Conditions == nil || rule.Conditions.Grouping == nil {
		return
	}
	groupID := strings.TrimSpace(rule.Conditions.Grouping.DefaultGroupID)
	if groupID == "" {
		return
	}

	evalCtx.ActiveRuleID = rule.ID
	evalCtx.activeGroupIndex = getOrBuildGroupIndexForRule(evalCtx, rule, groupID, torrents, sm)
}

func getOrBuildGroupIndexForRule(evalCtx *EvalContext, rule *models.Automation, groupID string, torrents []qbt.Torrent, sm *qbittorrent.SyncManager) *groupIndex {
	if evalCtx == nil || rule == nil || groupID == "" {
		return nil
	}
	if evalCtx.groupIndexCache == nil {
		evalCtx.groupIndexCache = make(map[int]map[string]*groupIndex)
	}
	if evalCtx.groupIndexCache[rule.ID] == nil {
		evalCtx.groupIndexCache[rule.ID] = make(map[string]*groupIndex)
	}
	if cached := evalCtx.groupIndexCache[rule.ID][groupID]; cached != nil {
		return cached
	}

	var def *models.GroupDefinition
	if rule.Conditions != nil && rule.Conditions.Grouping != nil {
		def = findGroupDefinition(rule.Conditions.Grouping, groupID)
	}
	if def == nil {
		def = builtinGroupDefinition(groupID)
	}
	if def == nil {
		return nil
	}

	idx := buildGroupIndex(groupID, def, torrents, sm, evalCtx)
	evalCtx.groupIndexCache[rule.ID][groupID] = idx
	return idx
}

func findGroupDefinition(cfg *models.GroupingConfig, id string) *models.GroupDefinition {
	if cfg == nil || id == "" {
		return nil
	}
	for i := range cfg.Groups {
		if strings.EqualFold(strings.TrimSpace(cfg.Groups[i].ID), id) {
			return &cfg.Groups[i]
		}
	}
	return nil
}

func builtinGroupDefinition(id string) *models.GroupDefinition {
	switch id {
	case GroupCrossSeedContentPath:
		return &models.GroupDefinition{
			ID:                    id,
			Keys:                  []string{groupKeyContentPath},
			AmbiguousPolicy:       groupAmbiguousVerifyOverlap,
			MinFileOverlapPercent: 90,
		}
	case GroupCrossSeedContentSavePath:
		return &models.GroupDefinition{
			ID:                    id,
			Keys:                  []string{groupKeyContentPath, groupKeySavePath},
			AmbiguousPolicy:       groupAmbiguousVerifyOverlap,
			MinFileOverlapPercent: 90,
		}
	case GroupReleaseItem:
		return &models.GroupDefinition{
			ID:   id,
			Keys: []string{groupKeyContentType, groupKeyEffectiveName},
		}
	case GroupTrackerReleaseItem:
		return &models.GroupDefinition{
			ID:   id,
			Keys: []string{groupKeyTrackerDomain, groupKeyContentType, groupKeyEffectiveName},
		}
	case GroupHardlinkSignature:
		return &models.GroupDefinition{
			ID:   id,
			Keys: []string{groupKeyHardlinkSignature},
		}
	default:
		return nil
	}
}

func buildGroupIndex(groupID string, def *models.GroupDefinition, torrents []qbt.Torrent, sm *qbittorrent.SyncManager, evalCtx *EvalContext) *groupIndex {
	idx := &groupIndex{
		groupID:       groupID,
		keyByHash:     make(map[string]string),
		hashesByKey:   make(map[string][]string),
		sizeByHash:    make(map[string]int),
		ambiguousKeys: make(map[string]struct{}),
	}

	keys := def.Keys
	for i := range torrents {
		t := torrents[i]
		key, ok := buildGroupKey(keys, t, sm, evalCtx)
		if !ok || key == "" {
			continue
		}
		idx.keyByHash[t.Hash] = key
		idx.hashesByKey[key] = append(idx.hashesByKey[key], t.Hash)

		// Mark ambiguity for contentPath-based groups when ContentPath == SavePath.
		if containsKey(keys, groupKeyContentPath) && normalizePath(t.ContentPath) == normalizePath(t.SavePath) {
			idx.ambiguousKeys[key] = struct{}{}
		}
	}

	for key, hashes := range idx.hashesByKey {
		// Stable order for deterministic behavior.
		sort.Strings(hashes)
		idx.hashesByKey[key] = hashes
		for _, h := range hashes {
			idx.sizeByHash[h] = len(hashes)
		}
	}

	return idx
}

func buildGroupKey(keys []string, t qbt.Torrent, sm *qbittorrent.SyncManager, evalCtx *EvalContext) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch k {
		case groupKeyContentPath:
			v := normalizePath(t.ContentPath)
			if v == "" {
				return "", false
			}
			parts = append(parts, v)
		case groupKeySavePath:
			v := normalizePath(t.SavePath)
			if v == "" {
				return "", false
			}
			parts = append(parts, v)
		case groupKeyEffectiveName:
			v := strings.TrimSpace(torrentEffectiveName(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyContentType:
			v := strings.TrimSpace(torrentContentType(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyTrackerDomain:
			if sm == nil {
				return "", false
			}
			domains := collectTrackerDomains(t, sm)
			if len(domains) == 0 {
				return "", false
			}
			parts = append(parts, strings.ToLower(domains[0]))
		case groupKeyRlsSource:
			v := strings.TrimSpace(torrentRlsSource(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyRlsResolution:
			v := strings.TrimSpace(torrentRlsResolution(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyRlsCodec:
			v := strings.TrimSpace(torrentRlsCodec(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyRlsHDR:
			v := strings.TrimSpace(torrentRlsHDR(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyRlsAudio:
			v := strings.TrimSpace(torrentRlsAudio(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyRlsChannels:
			v := strings.TrimSpace(torrentRlsChannels(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyRlsGroup:
			v := strings.TrimSpace(torrentRlsGroup(t, evalCtx))
			if v == "" {
				return "", false
			}
			parts = append(parts, strings.ToLower(v))
		case groupKeyHardlinkSignature:
			if evalCtx == nil || evalCtx.HardlinkSignatureByHash == nil {
				return "", false
			}
			v := strings.TrimSpace(evalCtx.HardlinkSignatureByHash[t.Hash])
			if v == "" {
				return "", false
			}
			parts = append(parts, v)
		default:
			return "", false
		}
	}

	return strings.Join(parts, "|"), true
}

func containsKey(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
