// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"

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

// deriveSearchSourceRelease reads a library torrent as search reads it, with
// its files when they can be read.
func (s *Service) deriveSearchSourceRelease(ctx context.Context, instanceID int, torrent *qbt.Torrent) namedRelease {
	files, err := s.getTorrentFilesCached(ctx, instanceID, torrent.Hash)
	if err != nil {
		files = nil
	}
	read, _ := s.readSearchSource(ctx, torrent, files)
	return read
}

// readSearchSource reads the torrent that search looks up. Category routing is
// part of the reading, so search and cached-decision replay derive the same TV
// structure.
func (s *Service) readSearchSource(ctx context.Context, torrent *qbt.Torrent, files qbt.TorrentFiles) (namedRelease, ContentTypeInfo) {
	return s.readRelease(torrent.Name, files, readPolicy{
		mapContent: func(contentInfo ContentTypeInfo) ContentTypeInfo {
			return s.applyCategoryMappingRule(ctx, torrent, contentInfo)
		},
	})
}

// readApplyTarget reads the torrent that apply adds. A cached search may need
// file-derived TV structure, but an explicit info.name group remains
// authoritative and selected-file tags stay as veto evidence.
func (s *Service) readApplyTarget(name string, files qbt.TorrentFiles, replaySearch bool) namedRelease {
	read, _ := s.readRelease(name, files, readPolicy{applyTarget: true, replaySearch: replaySearch})
	return read
}

// readPolicy says how a Match path reads the files of a release.
type readPolicy struct {
	// mapContent applies the category mapping rule of the search source.
	mapContent func(ContentTypeInfo) ContentTypeInfo
	// applyTarget reads the torrent that apply adds. Trackers retitle listings
	// but keep the original info.name, so a TV name keeps its own structure
	// unless apply replays a search, and an explicit info.name group stays.
	applyTarget  bool
	replaySearch bool
}

// readRelease is the one reader of a release name: it returns the Read release
// (GLOSSARY.md). files, when known, give the season, episode, and pack. The
// content type is returned only when the files were read or the policy maps
// content.
func (s *Service) readRelease(name string, files qbt.TorrentFiles, policy readPolicy) (namedRelease, ContentTypeInfo) {
	read := s.matcher().readName(name)
	parsed := read.release
	if policy.applyTarget && isTVRelease(parsed) && !policy.replaySearch {
		files = nil
	}
	if len(files) == 0 && policy.mapContent == nil {
		return read, ContentTypeInfo{}
	}

	contentDetectionRelease, usedFile := s.selectContentDetectionRelease(name, parsed, files)
	contentInfo := DetermineContentTypeWithFiles(contentDetectionRelease, files)
	if policy.mapContent != nil {
		contentInfo = policy.mapContent(contentInfo)
	}
	if len(files) == 0 {
		return read, contentInfo
	}

	if usedFile {
		read.tagOrigin = contentDetectionRelease
	}
	derived := s.selectSourceReleaseForSearch(parsed, contentDetectionRelease, files, contentInfo)
	if !isTVRelease(derived) {
		return read, contentInfo
	}
	read.seasonNumeral = inferredSeasonNumeral(derived)
	read.release = derived
	if policy.applyTarget && releaseHasExplicitGroupTag(parsed) {
		preservedIdentity := *derived
		preservedIdentity.Group = parsed.Group
		preservedIdentity.Site = parsed.Site
		read.release = &preservedIdentity
	}
	return read, contentInfo
}

// readName reads a release name without its files.
func (m matcher) readName(name string) namedRelease {
	return namedRelease{release: m.parseReleaseName(name), rawName: name}
}

// titleKeys returns the Title keys of a Read release (GLOSSARY.md), each once.
func (m matcher) titleKeys(read namedRelease) []string {
	var buf [8]string
	titles := append(m.appendReleaseTitles(buf[:0], read.release, read.rawName), numeralFreeTitle(read.release))
	keys := make([]string, 0, len(titles))
	for _, title := range titles {
		if title == "" {
			continue
		}
		if key := stringutils.NormalizeForMatching(title); key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// numeralFreeTitle is the title without its Season numeral, or "". A name that
// parses as a yearless movie also counts, because the matcher reads the numeral
// of a bare "Title III" against a seasoned other side.
func numeralFreeTitle(release *rls.Release) string {
	if title := titleWithoutSeasonNumeral(release); title != "" {
		return title
	}
	if inferredSeasonNumeral(release) == 0 {
		return ""
	}
	_, title := readTitleNumeral(release.Title)
	return title
}

// inferredSeasonNumeral reads a last-word II to X as the season of a pack
// that has no season anywhere else: "Kaiju Squad 100 II" files carry only the
// numeral, and trackers list the pack as S02. A larger numeral, or a lone I, is
// more often part of the real title than a season. An episode keeps its
// absolute number, so qui never invents an S02E03 from "II - 03". A movie with
// a year, such as "Rocky III 1982", is a sequel and not a season.
func inferredSeasonNumeral(release *rls.Release) int {
	if release == nil || release.Series > 0 || release.Episode > 0 || isYearBearingMovieRelease(release) {
		return 0
	}
	if season, _ := readTitleNumeral(release.Title); season >= 2 && season <= 10 {
		return season
	}
	return 0
}

// withTitleNumeralSeason gives a release with only a title numeral the season
// of the other release, when the numeral names that season. The copy lives for
// one pair: a numeral that names another season leaves the release seasonless.
func withTitleNumeralSeason(release, other *rls.Release) *rls.Release {
	if other == nil || other.Series == 0 || inferredSeasonNumeral(release) != other.Series {
		return release
	}
	withSeason := *release
	withSeason.Series = other.Series
	return &withSeason
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

	// Read each file name before the torrent's season is copied in, so the
	// seasonless files take their numbering scheme from seasonlessFilesPerSeason.
	fileReleases := make([]*rls.Release, 0, len(files))
	lowestSeasonlessEpisode := 0
	for _, file := range files {
		if shouldIgnoreFile(file.Name, normalizer) {
			continue
		}
		fileRelease := s.matcher().parseFileRelease(file.Name)
		if ep := absoluteEpisode(fileRelease); ep > 0 && (lowestSeasonlessEpisode == 0 || ep < lowestSeasonlessEpisode) {
			lowestSeasonlessEpisode = ep
		}
		fileReleases = append(fileReleases, fileRelease)
	}
	perSeason := lowestSeasonlessEpisode > 0 && seasonlessFilesPerSeason(torrentRelease, lowestSeasonlessEpisode)

	bySeries := make(map[int]*seriesInfo)
	absoluteEpisodes := make(map[int]struct{})
	seasonlessEpisodeFiles := 0
	for _, fileRelease := range fileReleases {
		if perSeason || absoluteEpisode(fileRelease) == 0 {
			fileRelease = enrichReleaseFromTorrent(fileRelease, torrentRelease)
		}
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

	// Seasonless files in the absolute scheme are episodes of the pack too.
	switch {
	case bestEpisodeCount+len(absoluteEpisodes) >= 2:
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
