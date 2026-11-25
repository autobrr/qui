// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strconv"
	"strings"

	"github.com/autobrr/rls"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

type torznabRequestInput struct {
	torrentName   string
	source        namedRelease
	contentInfo   ContentTypeInfo
	externalIDs   *models.ExternalIDs
	episodeMap    *models.EpisodeMap
	arrTitles     []string
	tagSourcedIDs bool
	indexerIDs    []int
	query         string
	limit         int
	cacheMode     string
}

func (m matcher) buildTorznabRequest(in torznabRequestInput) GatherInput {
	release := in.source.release
	rawRelease := m.parseReleaseName(in.torrentName)

	queryRelease := release
	if in.contentInfo.ContentType == "music" {
		queryRelease = m.parseMusicReleaseFromTorrentName(rawRelease, in.torrentName)
	}

	req := &jackett.TorznabSearchRequest{
		Query:            strings.TrimSpace(in.query),
		ReleaseName:      in.torrentName,
		Limit:            effectiveTorznabCrossSeedSearchLimit(in.limit),
		IndexerIDs:       in.indexerIDs,
		CacheMode:        in.cacheMode,
		ReturnAllResults: true,
	}
	if req.Query == "" {
		generated := BuildTorznabQuery(in.torrentName, queryRelease, in.contentInfo.IsMusic)
		req.Query = generated.Query
		req.Season = generated.Season
		req.Episode = generated.Episode
	}

	if in.externalIDs != nil {
		applyExternalIDs(req, in.externalIDs)
		if release.Series == 0 && release.Episode > 0 && in.episodeMap != nil && in.episodeMap.Absolute == release.Episode {
			req.EpisodeMap = in.episodeMap
		}
	}

	if in.contentInfo.IsMusic {
		m.applyMusicFields(req, rawRelease, in.torrentName)
	}

	if len(in.contentInfo.Categories) > 0 {
		req.Categories = in.contentInfo.Categories

		if !in.contentInfo.IsMusic && queryRelease.Series > 0 && req.Season == nil {
			req.Season = new(queryRelease.Series)

			if queryRelease.Episode > 0 && req.Episode == nil {
				req.Episode = new(queryRelease.Episode)
			}
		}

		if release.Year > 0 && in.contentInfo.ContentType != "tv" {
			req.Year = release.Year
		}
	}

	out := GatherInput{Req: req, TagSourcedIDs: in.tagSourcedIDs, TorrentName: in.torrentName, LogPrefix: "[CROSSSEED-SEARCH]"}
	if in.contentInfo.ContentType == "tv" {
		out.TVSourceYear = release.Year
	}
	if !req.OmitQueryForIDs || in.tagSourcedIDs {
		var altSeason int
		out.AltTitle, altSeason, _ = m.alternateTitleQuery(req.Query, in.source, in.arrTitles)
		if altSeason > 0 {
			out.AltTitleSeason = new(altSeason)
		}
	}
	return out
}

func applyExternalIDs(req *jackett.TorznabSearchRequest, ids *models.ExternalIDs) {
	if ids.IMDbID != "" {
		req.IMDbID = ids.IMDbID
	}
	if ids.TVDbID > 0 {
		req.TVDbID = strconv.Itoa(ids.TVDbID)
	}
	if ids.TMDbID > 0 {
		req.TMDbID = ids.TMDbID
	}
	if ids.TVMazeID > 0 {
		req.TVMazeID = ids.TVMazeID
	}
	req.OmitQueryForIDs = true
}

func (m matcher) applyMusicFields(req *jackett.TorznabSearchRequest, rawRelease *rls.Release, torrentName string) {
	musicRelease := rawRelease
	if rawRelease.Type != rls.Music || rawRelease.Artist == "" {
		musicRelease = m.parseMusicReleaseFromTorrentName(rawRelease, torrentName)
	}
	req.Artist = musicRelease.Artist
	req.Album = musicRelease.Title
}
