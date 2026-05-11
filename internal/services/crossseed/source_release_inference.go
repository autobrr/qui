// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path"
	"strings"

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

// deriveTVSearchName chooses the best TV release folder name for search inference.
// It prefers ContentPath's folder, then a shared top-level file folder, then torrent.Name.
func deriveTVSearchName(torrent *qbt.Torrent, files qbt.TorrentFiles) string {
	if torrent == nil {
		return ""
	}

	if name := folderNameFromContentPath(torrent.ContentPath, torrent.SavePath); name != "" {
		return name
	}

	if name := commonTopLevelFolder(files); name != "" {
		return name
	}

	return strings.TrimSpace(torrent.Name)
}

// folderNameFromContentPath returns the basename of ContentPath when it points at a folder.
// It returns empty for special paths, paths equal to savePath, or names that look like files.
func folderNameFromContentPath(contentPath, savePath string) string {
	cleaned := cleanTorrentPath(contentPath)
	if cleaned == "" || cleaned == "." || cleaned == "/" {
		return ""
	}
	if strings.EqualFold(cleaned, cleanTorrentPath(savePath)) {
		return ""
	}

	name := path.Base(cleaned)
	if name == "." || name == "/" || name == "" || isLikelyTorrentFileName(name) {
		return ""
	}

	return strings.TrimSpace(name)
}

// commonTopLevelFolder returns the single shared top-level folder for all nested files.
// It returns empty when files are flat, lack a shared folder, or disagree on the top folder.
func commonTopLevelFolder(files qbt.TorrentFiles) string {
	var folder string
	for _, file := range files {
		name := cleanTorrentPath(file.Name)
		if name == "" || name == "." || !strings.Contains(name, "/") {
			continue
		}

		current := strings.TrimSpace(strings.SplitN(name, "/", 2)[0])
		if current == "" {
			continue
		}
		if folder == "" {
			folder = current
			continue
		}
		if folder != current {
			return ""
		}
	}

	return folder
}

func cleanTorrentPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	return path.Clean(value)
}

// isLikelyTorrentFileName reports whether name has a common payload or sidecar extension.
// It treats video, subtitle, metadata/image, archive, and split-RAR extensions as files.
func isLikelyTorrentFileName(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".mkv", ".mp4", ".avi", ".mov", ".wmv", ".m4v", ".ts", ".m2ts",
		".srt", ".ass", ".ssa", ".sub", ".idx",
		".nfo", ".sfv", ".txt", ".jpg", ".jpeg", ".png",
		".rar", ".zip", ".7z", ".r00", ".r01":
		return true
	default:
		return false
	}
}
