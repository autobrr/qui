// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"sort"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// CrossMatchNeeds specifies which cross-match sets to compute.
type CrossMatchNeeds struct {
	SameExists   bool
	SameSeeding  bool
	SameTags     bool
	OtherExists  bool
	OtherSeeding bool
}

// CrossMatchResult contains all cross-match sets for a given instance.
type CrossMatchResult struct {
	SameInstanceExists   map[string]struct{}
	SameInstanceSeeding  map[string]struct{}
	OtherInstanceExists  map[string]struct{}
	OtherInstanceSeeding map[string]struct{}
	// SameInstanceTagsByHash maps a source hash to the sorted raw Tags strings of
	// its same-instance cross-seed matches (self excluded). A hash is present only
	// when at least one match carries a non-empty tag string.
	SameInstanceTagsByHash map[string][]string
}

// candidateEntry holds pre-computed data for a candidate torrent.
type candidateEntry struct {
	hash    string
	tags    string
	seeding bool
}

// releaseCandidate extends candidateEntry with parsed release metadata for rls matching.
type releaseCandidate struct {
	candidateEntry
	name    string
	release *rls.Release
}

// matchIndex holds pre-built indexes for efficient batch matching
// of source torrents against candidate torrents.
type matchIndex struct {
	byContentPath map[string][]candidateEntry       // key: normalizedContentPath (non-ambiguous only)
	byName        map[string][]candidateEntry       // key: normalizeLowerTrim(name)
	byReleaseKey  map[releaseKey][]releaseCandidate // for release metadata matching
}

// BuildCrossMatchSets builds sets of torrent hashes that have cross-seeds
// on the same instance and/or other instances, using the same matching strategies
// as "Filter Cross-Seeds": content path, exact name, and release metadata matching.
// All data comes from the SyncManager cache — no API calls to qBittorrent.
func (s *Service) BuildCrossMatchSets(ctx context.Context, currentInstanceID int, needs CrossMatchNeeds) *CrossMatchResult {
	result := &CrossMatchResult{}
	needsSame := needs.SameExists || needs.SameSeeding || needs.SameTags
	needsOther := needs.OtherExists || needs.OtherSeeding

	if !needsSame && !needsOther {
		return result
	}

	// Get current instance torrents (needed for both same and other matching)
	currentTorrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, currentInstanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", currentInstanceID).Msg("crossseed: failed to get current instance torrents")
		return result
	}

	// Build same-instance index and match
	if needsSame {
		sameIdx := s.buildIndexFromViews(currentTorrents, needs.SameSeeding)
		if sameIdx != nil {
			result.SameInstanceExists = make(map[string]struct{})
			if needs.SameSeeding {
				result.SameInstanceSeeding = make(map[string]struct{})
			}
			if needs.SameTags {
				result.SameInstanceTagsByHash = make(map[string][]string)
			}
			for i := range currentTorrents {
				if ctx.Err() != nil {
					break
				}
				source := currentTorrents[i].Torrent
				if matched, matchSeeding, memberTags := s.matchAgainstIndex(source, sameIdx, true, needs.SameTags); matched {
					result.SameInstanceExists[source.Hash] = struct{}{}
					if needs.SameSeeding && matchSeeding {
						result.SameInstanceSeeding[source.Hash] = struct{}{}
					}
					if needs.SameTags && len(memberTags) > 0 {
						result.SameInstanceTagsByHash[source.Hash] = memberTags
					}
				}
			}
		}
	}

	// Build other-instance index and match
	if needsOther {
		instances, err := s.instanceStore.List(ctx)
		if err != nil {
			log.Error().Err(err).Msg("crossseed: failed to list instances for cross-instance matching")
			return result
		}

		otherIdx := s.buildOtherInstanceIndex(ctx, instances, currentInstanceID, needs.OtherSeeding)
		if otherIdx != nil {
			result.OtherInstanceExists = make(map[string]struct{})
			if needs.OtherSeeding {
				result.OtherInstanceSeeding = make(map[string]struct{})
			}
			for i := range currentTorrents {
				if ctx.Err() != nil {
					break
				}
				source := currentTorrents[i].Torrent
				if matched, matchSeeding, _ := s.matchAgainstIndex(source, otherIdx, false, false); matched {
					result.OtherInstanceExists[source.Hash] = struct{}{}
					if needs.OtherSeeding && matchSeeding {
						result.OtherInstanceSeeding[source.Hash] = struct{}{}
					}
				}
			}
		}
	}

	return result
}

// buildIndexFromViews builds a matchIndex from CrossInstanceTorrentViews.
func (s *Service) buildIndexFromViews(views []qbittorrent.CrossInstanceTorrentView, needsSeeding bool) *matchIndex {
	if len(views) == 0 {
		return nil
	}

	idx := &matchIndex{
		byContentPath: make(map[string][]candidateEntry),
		byName:        make(map[string][]candidateEntry),
		byReleaseKey:  make(map[releaseKey][]releaseCandidate),
	}

	for i := range views {
		view := &views[i]
		s.indexView(idx, view, needsSeeding)
	}

	return idx
}

// buildOtherInstanceIndex builds a matchIndex from all torrents on other instances.
func (s *Service) buildOtherInstanceIndex(ctx context.Context, instances []*models.Instance, currentInstanceID int, needsSeeding bool) *matchIndex {
	idx := &matchIndex{
		byContentPath: make(map[string][]candidateEntry),
		byName:        make(map[string][]candidateEntry),
		byReleaseKey:  make(map[releaseKey][]releaseCandidate),
	}

	hasEntries := false

	for _, inst := range instances {
		if inst.ID == currentInstanceID || !inst.IsActive {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		views, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", inst.ID).Msg("crossseed: skipping instance for cross-instance matching")
			continue
		}

		for i := range views {
			s.indexView(idx, &views[i], needsSeeding)
			hasEntries = true
		}
	}

	if !hasEntries {
		return nil
	}
	return idx
}

// indexView adds a single torrent view to all indexes.
func (s *Service) indexView(idx *matchIndex, view *qbittorrent.CrossInstanceTorrentView, needsSeeding bool) {
	entry := candidateEntry{
		hash:    normalizeHash(view.Hash),
		tags:    view.Tags,
		seeding: needsSeeding && isTorrentViewSeeding(view),
	}

	// Index by content path (non-ambiguous only)
	contentPath := normalizePathForComparison(view.ContentPath)
	savePath := normalizePathForComparison(view.SavePath)
	if contentPath != "" && contentPath != savePath {
		idx.byContentPath[contentPath] = append(idx.byContentPath[contentPath], entry)
	}

	// Index by normalized name
	name := normalizeLowerTrim(view.Name)
	if name != "" {
		idx.byName[name] = append(idx.byName[name], entry)
	}

	// Index by release key for rls matching
	release := s.parseReleaseName(view.Name)
	title := normalizeLowerTrim(release.Title)
	if title != "" {
		key := makeReleaseKey(release)
		idx.byReleaseKey[key] = append(idx.byReleaseKey[key], releaseCandidate{
			candidateEntry: entry,
			name:           view.Name,
			release:        release,
		})
	}
}

// matchAgainstIndex checks if a source torrent matches any candidate in the index.
// When excludeSelf is true, candidates with the same hash as the source are skipped
// (used for same-instance matching where the source torrent is in the index).
// When collectTags is true, the non-empty raw Tags strings of every distinct match
// are returned sorted; a candidate matching via several strategies counts once.
// Returns (matched, anyMatchSeeding, memberTags).
func (s *Service) matchAgainstIndex(source *qbt.Torrent, idx *matchIndex, excludeSelf, collectTags bool) (matched bool, matchSeeding bool, memberTags []string) {
	sourceHash := normalizeHash(source.Hash)

	var seen map[string]struct{}
	if collectTags {
		seen = make(map[string]struct{})
	}
	record := func(c candidateEntry) {
		matched = true
		if c.seeding {
			matchSeeding = true
		}
		if !collectTags {
			return
		}
		if _, dup := seen[c.hash]; dup {
			return
		}
		seen[c.hash] = struct{}{}
		if c.tags != "" {
			memberTags = append(memberTags, c.tags)
		}
	}

	// Strategy 1: Content path match (non-ambiguous)
	contentPath := normalizePathForComparison(source.ContentPath)
	savePath := normalizePathForComparison(source.SavePath)
	if contentPath != "" && contentPath != savePath {
		if candidates, ok := idx.byContentPath[contentPath]; ok {
			for _, c := range candidates {
				if excludeSelf && c.hash == sourceHash {
					continue
				}
				record(c)
			}
		}
	}

	// Strategy 2: Exact name match
	name := normalizeLowerTrim(source.Name)
	if name != "" {
		if candidates, ok := idx.byName[name]; ok {
			for _, c := range candidates {
				if excludeSelf && c.hash == sourceHash {
					continue
				}
				record(c)
			}
		}
	}

	// Strategy 3: Release metadata match
	sourceRelease := s.parseReleaseName(source.Name)
	sourceTitle := normalizeLowerTrim(sourceRelease.Title)
	if sourceTitle != "" {
		key := makeReleaseKey(sourceRelease)
		if candidates, ok := idx.byReleaseKey[key]; ok {
			for _, c := range candidates {
				if excludeSelf && c.hash == sourceHash {
					continue
				}
				match, reason := s.releasesMatchWithReason(sourceRelease, c.release, false)
				logReleaseMatchDecision(
					source.Name,
					c.name,
					false,
					sourceRelease,
					c.release,
					match,
					reason,
					"crossseed: release metadata candidate evaluated",
				)
				if match {
					record(c.candidateEntry)
				}
			}
		}
	}

	sort.Strings(memberTags)
	return matched, matchSeeding, memberTags
}

// isTorrentViewSeeding checks if a CrossInstanceTorrentView is in a seeding state.
func isTorrentViewSeeding(view *qbittorrent.CrossInstanceTorrentView) bool {
	if view == nil || view.TorrentView == nil || view.Torrent == nil || view.Progress < 1.0 {
		return false
	}
	switch view.State {
	case qbt.TorrentStateUploading, qbt.TorrentStateStalledUp,
		qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp,
		qbt.TorrentStateForcedUp:
		return true
	case qbt.TorrentStateError, qbt.TorrentStateMissingFiles,
		qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedUp,
		qbt.TorrentStateAllocating, qbt.TorrentStateDownloading,
		qbt.TorrentStateMetaDl, qbt.TorrentStatePausedDl,
		qbt.TorrentStateStoppedDl, qbt.TorrentStateQueuedDl,
		qbt.TorrentStateStalledDl, qbt.TorrentStateCheckingDl,
		qbt.TorrentStateForcedDl, qbt.TorrentStateCheckingResumeData,
		qbt.TorrentStateMoving, qbt.TorrentStateUnknown:
		return false
	}
	return false
}
