// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"iter"
	"path"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

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

func (s *Service) deriveSearchSourceRelease(ctx context.Context, instanceID int, torrent *qbt.Torrent) namedRelease {
	files, err := s.getTorrentFilesCached(ctx, instanceID, torrent.Hash)
	if err != nil {
		files = nil
	}
	read, _ := s.readSearchSource(ctx, torrent, files)
	return read
}

func nameNeedsFiles(release *rls.Release) bool {
	if release.Episode != 0 {
		return false
	}
	seasonFolder := release.Title == "" && release.Series > 0
	seasonlessBatch := release.Type == rls.Movie && release.Series == 0 && release.Year == 0
	return seasonFolder || seasonlessBatch
}

func viewTorrents(views []qbittorrent.CrossInstanceTorrentView) iter.Seq[*qbt.Torrent] {
	return func(yield func(*qbt.Torrent) bool) {
		for i := range views {
			if views[i].Torrent != nil && !yield(views[i].Torrent) {
				return
			}
		}
	}
}

func (s *Service) readSearchSource(ctx context.Context, torrent *qbt.Torrent, files qbt.TorrentFiles) (namedRelease, ContentTypeInfo) {
	return s.readRelease(torrent.Name, files, readPolicy{
		mapContent: func(contentInfo ContentTypeInfo) ContentTypeInfo {
			return s.applyCategoryMappingRule(ctx, torrent, contentInfo)
		},
	})
}

func (s *Service) readApplyTarget(name string, files qbt.TorrentFiles, replaySearch bool) namedRelease {
	read, _ := s.readRelease(name, files, readPolicy{applyTarget: true, replaySearch: replaySearch})
	return read
}

type readPolicy struct {
	mapContent   func(ContentTypeInfo) ContentTypeInfo
	applyTarget  bool
	replaySearch bool
}

func (s *Service) readRelease(name string, files qbt.TorrentFiles, policy readPolicy) (namedRelease, ContentTypeInfo) {
	read := s.matcher().readName(name)
	read.release = s.seasonFolderRelease(read.release, files)
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

func (s *Service) seasonFolderRelease(parsed *rls.Release, files qbt.TorrentFiles) *rls.Release {
	if parsed.Title != "" || parsed.Series == 0 || parsed.Episode != 0 {
		return parsed
	}
	var fileRelease *rls.Release
	var largestSize int64
	for _, file := range files {
		if fileRelease != nil && file.Size <= largestSize {
			continue
		}
		release := s.matcher().parseFileRelease(path.Base(file.Name))
		if release.Title != "" && release.Series == parsed.Series {
			fileRelease, largestSize = release, file.Size
		}
	}
	if fileRelease == nil {
		return parsed
	}
	borrowed := *fileRelease
	borrowed.Type = rls.Series
	borrowed.Episode = 0
	borrowed.Sum = ""
	return &borrowed
}

func (m matcher) readName(name string) namedRelease {
	return namedRelease{release: m.parseReleaseName(name), rawName: name}
}

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

func inferredSeasonNumeral(release *rls.Release) int {
	if release == nil || release.Series > 0 || release.Episode > 0 || isYearBearingMovieRelease(release) {
		return 0
	}
	if season, _ := readTitleNumeral(release.Title); season >= 2 && season <= 10 {
		return season
	}
	return 0
}

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
	case bestEpisodeCount+len(absoluteEpisodes) >= 2:
		return bestSeries, 0, true, true
	case bestEpisodeCount == 1:
		for ep := range bySeries[bestSeries].episodes {
			return bestSeries, ep, false, true
		}
	}

	if bestFileCount >= 2 {
		return bestSeries, 0, true, true
	}

	return bestSeries, 0, false, true
}

func isYearBearingMovieRelease(release *rls.Release) bool {
	return release != nil && release.Type == rls.Movie && release.Year > 0
}

func (s *Service) readCatalogRelease(name string, files qbt.TorrentFiles, family rls.Type) (namedRelease, error) {
	if family == rls.Unknown || catalogFileFamily(files) != family {
		return namedRelease{}, errors.New("unproven file content family")
	}
	read := s.matcher().readName(name)
	read.tagOrigin = read.release
	derived := rls.Release{Type: family,
		Artist:     read.release.Artist,
		Title:      read.release.Title,
		Subtitle:   read.release.Subtitle,
		Alt:        read.release.Alt,
		Platform:   read.release.Platform,
		Arch:       read.release.Arch,
		Source:     read.release.Source,
		Resolution: read.release.Resolution,
		FrameRate:  read.release.FrameRate,
		Collection: read.release.Collection,
		Year:       read.release.Year,
		Month:      read.release.Month,
		Day:        read.release.Day,
		YearEnd:    read.release.YearEnd,
		Version:    read.release.Version,
		Disc:       read.release.Disc,
		Codec:      read.release.Codec,
		HDR:        read.release.HDR,
		BitDepth:   read.release.BitDepth,
		Audio:      read.release.Audio,
		Channels:   read.release.Channels,
		Other:      read.release.Other,
		Cut:        read.release.Cut,
		Edition:    read.release.Edition,
		Language:   read.release.Language,
		Size:       read.release.Size,
		Region:     read.release.Region,
		Container:  read.release.Container,
		Genre:      read.release.Genre,
		ID:         read.release.ID,
		Group:      read.release.Group,
		Meta:       read.release.Meta,
		Site:       read.release.Site,
		Sum:        read.release.Sum,
		Pass:       read.release.Pass,
		Req:        read.release.Req,
		Ext:        read.release.Ext,
	}

	read.release = &derived
	read.seasonNumeral = 0
	return read, nil
}
