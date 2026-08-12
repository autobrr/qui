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

// deriveSearchSourceTVRelease recovers TV structure from a torrent's files the
// same way search does, for names that parse as non-TV (bracket-anime packs
// carry season/episode markers only in file names). Returns nil when the files
// don't establish a TV release, so callers keep the raw parse.
func (s *Service) deriveSearchSourceTVRelease(ctx context.Context, instanceID int, hash, name string, parsed *rls.Release) *rls.Release {
	files, err := s.getTorrentFilesCached(ctx, instanceID, hash)
	if err != nil {
		return nil
	}
	return s.deriveTVReleaseFromFiles(name, parsed, files)
}

// deriveTVReleaseFromFiles is the file-list core of deriveSearchSourceTVRelease
// for callers that already hold the files (e.g. apply, which has the incoming
// torrent's metainfo). Returns nil when the files don't establish a TV release.
func (s *Service) deriveTVReleaseFromFiles(name string, parsed *rls.Release, files qbt.TorrentFiles) *rls.Release {
	if len(files) == 0 {
		return nil
	}

	contentDetectionRelease, _ := s.selectContentDetectionRelease(name, parsed, files)
	contentInfo := DetermineContentTypeWithFiles(contentDetectionRelease, files)
	derived := s.selectSourceReleaseForSearch(parsed, contentDetectionRelease, files, contentInfo)
	if !isTVRelease(derived) {
		return nil
	}
	return derived
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
