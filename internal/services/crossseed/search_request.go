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

// torznabRequestInput is what buildTorznabRequest reads. The lookups that fill
// it run first, so a table test can give each rule its facts with no I/O.
type torznabRequestInput struct {
	torrentName string
	source      namedRelease
	contentInfo ContentTypeInfo
	// externalIDs is nil when no lookup found an ID.
	externalIDs   *models.ExternalIDs
	episodeMap    *models.EpisodeMap
	arrTitles     []string
	tagSourcedIDs bool
	indexerIDs    []int
	query         string
	limit         int
	cacheMode     string
}

// buildTorznabRequest holds the rules for what qui asks a Torznab indexer. The
// normal search and the search for a season group both call it. The normal
// search trims the categories after this step, because the trim reads the
// indexers from the database.
func (m matcher) buildTorznabRequest(in torznabRequestInput) gatherInput {
	release := in.source.release
	rawRelease := m.parseReleaseName(in.torrentName)

	queryRelease := release
	if in.contentInfo.ContentType == "music" {
		// Keyed on the content type, not the parsed type: the file-extension signal forces music
		// on releases whose name parsed as tv or movie, and those need the artist/album re-parse
		// too. Audiobooks are excluded here only; they still take the music query shaping below.
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
		// An absolute-numbered source has an episode but no season, and Torznab
		// ep counts within a season. The map gives ID-capable indexers the
		// seasoned pair; text indexers keep the title-only query. Keyed on the
		// parsed release, not the generated season and episode, so a
		// caller-supplied query still carries the map.
		if release.Series == 0 && release.Episode > 0 && in.episodeMap != nil && in.episodeMap.Absolute == release.Episode {
			req.EpisodeMap = in.episodeMap
		}
	}

	if in.contentInfo.IsMusic {
		m.applyMusicFields(req, rawRelease, in.torrentName)
	}

	if len(in.contentInfo.Categories) > 0 {
		req.Categories = in.contentInfo.Categories

		// Add season/episode info for TV content only if the generated query did not set it
		if !in.contentInfo.IsMusic && queryRelease.Series > 0 && req.Season == nil {
			req.Season = new(queryRelease.Series)

			if queryRelease.Episode > 0 && req.Episode == nil {
				req.Episode = new(queryRelease.Episode)
			}
		}

		// Year hurts TV searches on many indexers; keep it for movies only.
		if release.Year > 0 && in.contentInfo.ContentType != "tv" {
			req.Year = release.Year
		}
	}

	out := gatherInput{req: req, tagSourcedIDs: in.tagSourcedIDs, torrentName: in.torrentName}
	if !req.OmitQueryForIDs || in.tagSourcedIDs {
		var altSeason int
		out.altTitle, altSeason, _ = m.alternateTitleQuery(req.Query, in.source, in.arrTitles)
		if altSeason > 0 {
			out.altTitleSeason = new(altSeason)
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
	req.OmitQueryForIDs = true // Cross-seed: omit q when IDs present
}

func (m matcher) applyMusicFields(req *jackett.TorznabSearchRequest, rawRelease *rls.Release, torrentName string) {
	musicRelease := rawRelease
	if rawRelease.Type != rls.Music || rawRelease.Artist == "" {
		musicRelease = m.parseMusicReleaseFromTorrentName(rawRelease, torrentName)
	}
	req.Artist = musicRelease.Artist
	req.Album = musicRelease.Title // For music, Title represents the album
}
