// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
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
	if inferredIsPack && derived.Series > 0 {
		derived.Episode = 0
		return &derived
	}

	if derived.Series > 0 && derived.Episode == 0 && inferredEpisode > 0 {
		derived.Episode = inferredEpisode
	}

	return &derived
}

func (s *Service) inferTVSeriesEpisodeFromFiles(torrentRelease *rls.Release, files qbt.TorrentFiles) (series, episode int, isPack, ok bool) {
	normalizer := s.stringNormalizer
	if normalizer == nil {
		normalizer = stringutils.NewDefaultNormalizer()
	}

	type seriesInfo struct {
		filesSeen int
		episodes  map[int]struct{}
	}

	bySeries := make(map[int]*seriesInfo)
	for _, file := range files {
		if shouldIgnoreFile(file.Name, normalizer) {
			continue
		}

		fileRelease := s.releaseCache.Parse(file.Name)
		fileRelease = enrichReleaseFromTorrent(fileRelease, torrentRelease)
		if fileRelease.Series <= 0 {
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
