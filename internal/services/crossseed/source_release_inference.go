// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/stringutils"
)

// deriveSourceReleaseForSearch enhances parsed torrent metadata with information inferred
// from actual files, primarily to recover season/episode structure when the torrent name
// doesn't include it (common for anime season packs).
func (s *Service) deriveSourceReleaseForSearch(sourceRelease *rls.Release, files qbt.TorrentFiles) *rls.Release {
	if sourceRelease == nil || len(files) == 0 || s == nil || s.releaseCache == nil {
		return sourceRelease
	}

	inferredSeries, inferredEpisode, inferredIsPack, ok := s.inferTVSeriesEpisodeFromFiles(sourceRelease, files)
	if !ok {
		return sourceRelease
	}

	derived := *sourceRelease
	if derived.Series == 0 && inferredSeries > 0 {
		derived.Series = inferredSeries
	}

	// Trust file structure when it indicates a season pack.
	if inferredIsPack {
		derived.Type = rls.Series
		derived.Episode = 0
		return &derived
	}

	if derived.Episode == 0 && inferredEpisode > 0 {
		derived.Episode = inferredEpisode
	}
	if inferredEpisode > 0 {
		derived.Type = rls.Episode
	}

	return &derived
}

func (s *Service) selectSourceReleaseForSearch(sourceRelease, contentDetectionRelease *rls.Release, files qbt.TorrentFiles, contentInfo ContentTypeInfo) *rls.Release {
	if contentInfo.ContentType != "tv" {
		return sourceRelease
	}

	baseRelease := sourceRelease
	if isTVRelease(contentDetectionRelease) {
		baseRelease = contentDetectionRelease
	}

	searchRelease := s.deriveSourceReleaseForSearch(baseRelease, files)
	if isTVSeasonPack(searchRelease) {
		return mergeSeasonPackSearchStructure(sourceRelease, searchRelease)
	}

	return searchRelease
}

func mergeSeasonPackSearchStructure(sourceRelease, inferredRelease *rls.Release) *rls.Release {
	if sourceRelease == nil || inferredRelease == nil {
		return inferredRelease
	}

	merged := *sourceRelease
	merged.Type = rls.Series
	merged.Series = inferredRelease.Series
	merged.Episode = 0
	return &merged
}

// deriveSearchSourceRelease recovers the same category-aware public view and
// selected-file provenance used by search.
func (s *Service) deriveSearchSourceRelease(ctx context.Context, instanceID int, torrent *qbt.Torrent, parsed *rls.Release) namedRelease {
	view := namedRelease{release: parsed, rawName: torrent.Name}
	files, err := s.getTorrentFilesCached(ctx, instanceID, torrent.Hash)
	if err != nil {
		return view
	}
	return s.searchSourceReleaseViewFromFiles(ctx, torrent, parsed, files)
}

// searchSourceReleaseViewFromFiles reconstructs the existing torrent view used
// by search and cached-decision replay. Category routing is part of that view,
// so both stages derive the same TV structure.
func (s *Service) searchSourceReleaseViewFromFiles(ctx context.Context, torrent *qbt.Torrent, parsed *rls.Release, files qbt.TorrentFiles) namedRelease {
	view, _ := s.searchSourceReleaseViewAndContentInfo(ctx, torrent, parsed, files)
	return view
}

func (s *Service) searchSourceReleaseViewAndContentInfo(ctx context.Context, torrent *qbt.Torrent, parsed *rls.Release, files qbt.TorrentFiles) (namedRelease, ContentTypeInfo) {
	contentDetectionRelease, usedFile := s.selectContentDetectionRelease(torrent.Name, parsed, files)
	contentInfo := s.applyCategoryMappingRule(ctx, torrent, DetermineContentTypeWithFiles(contentDetectionRelease, files))
	view := s.buildReleaseView(torrent.Name, parsed, contentDetectionRelease, usedFile, files, contentInfo, releaseViewPolicy{
		useDerivedTV: true,
	})
	return view, contentInfo
}

// applyTargetReleaseViewFromFiles builds the downloaded candidate's view. A
// cached search may need file-derived TV structure, but an explicit info.name
// group remains authoritative and selected-file tags stay as veto evidence.
func (s *Service) applyTargetReleaseViewFromFiles(name string, parsed *rls.Release, files qbt.TorrentFiles, replaySearch bool) namedRelease {
	contentDetectionRelease, usedFile := s.selectContentDetectionRelease(name, parsed, files)
	contentInfo := DetermineContentTypeWithFiles(contentDetectionRelease, files)
	return s.buildReleaseView(name, parsed, contentDetectionRelease, usedFile, files, contentInfo, releaseViewPolicy{
		useDerivedTV:             !isTVRelease(parsed) || replaySearch,
		preserveExplicitRawGroup: true,
	})
}

type releaseViewPolicy struct {
	useDerivedTV             bool
	preserveExplicitRawGroup bool
}

func (s *Service) buildReleaseView(
	name string,
	parsed *rls.Release,
	contentDetectionRelease *rls.Release,
	usedFile bool,
	files qbt.TorrentFiles,
	contentInfo ContentTypeInfo,
	policy releaseViewPolicy,
) namedRelease {
	view := namedRelease{release: parsed, rawName: name}
	if len(files) == 0 {
		return view
	}

	if usedFile {
		view.tagOrigin = contentDetectionRelease
	}
	derived := s.selectSourceReleaseForSearch(parsed, contentDetectionRelease, files, contentInfo)
	if !policy.useDerivedTV || !isTVRelease(derived) {
		return view
	}
	if policy.preserveExplicitRawGroup && releaseHasExplicitGroupTag(parsed) {
		preservedIdentity := *derived
		preservedIdentity.Group = parsed.Group
		preservedIdentity.Site = parsed.Site
		view.release = &preservedIdentity
		return view
	}
	view.release = derived
	return view
}

func (s *Service) inferTVSeriesEpisodeFromFiles(torrentRelease *rls.Release, files qbt.TorrentFiles) (series, episode int, isPack, ok bool) {
	normalizer := s.stringNormalizer
	if normalizer == nil {
		normalizer = stringutils.DefaultNormalizer
	}

	type seriesInfo struct {
		filesSeen int
		episodes  map[int]struct{}
	}

	bySeries := make(map[int]*seriesInfo)
	absoluteEpisodes := make(map[int]struct{})
	seasonlessEpisodeFiles := 0
	for _, file := range files {
		if shouldIgnoreFile(file.Name, normalizer) {
			continue
		}

		fileRelease := s.releaseCache.Parse(file.Name)
		fileRelease = enrichReleaseFromTorrent(fileRelease, torrentRelease)
		if fileRelease.Series <= 0 {
			if fileRelease.Episode > 0 {
				seasonlessEpisodeFiles++
				absoluteEpisodes[fileRelease.Episode] = struct{}{}
			}
			continue
		}

		info := bySeries[fileRelease.Series]
		if info == nil {
			info = &seriesInfo{episodes: make(map[int]struct{})}
			bySeries[fileRelease.Series] = info
		}
		info.filesSeen++
		if fileRelease.Episode > 0 {
			info.episodes[fileRelease.Episode] = struct{}{}
		}
	}

	bestSeries := 0
	bestEpisodeCount := 0
	bestFileCount := 0
	for sNum, info := range bySeries {
		epCount := len(info.episodes)
		if epCount > bestEpisodeCount || (epCount == bestEpisodeCount && info.filesSeen > bestFileCount) {
			bestSeries = sNum
			bestEpisodeCount = epCount
			bestFileCount = info.filesSeen
		}
	}

	if bestSeries == 0 {
		if isYearBearingMovieRelease(torrentRelease) {
			return 0, 0, false, false
		}

		// Multiple seasonless episode files indicate a pack even if parsing
		// collapses them to the same absolute episode number.
		if seasonlessEpisodeFiles >= 2 {
			return 0, 0, true, true
		}
		if len(absoluteEpisodes) == 1 {
			for ep := range absoluteEpisodes {
				return 0, ep, false, true
			}
		}
		return 0, 0, false, false
	}

	switch {
	case bestEpisodeCount >= 2:
		return bestSeries, 0, true, true
	case bestEpisodeCount == 1:
		for ep := range bySeries[bestSeries].episodes {
			return bestSeries, ep, false, true
		}
	}

	// If rls detected a season but couldn't extract episode numbers, treat multiple
	// relevant files as a season pack.
	if bestFileCount >= 2 {
		return bestSeries, 0, true, true
	}

	return bestSeries, 0, false, true
}

func isYearBearingMovieRelease(release *rls.Release) bool {
	return release != nil && release.Type == rls.Movie && release.Year > 0
}
