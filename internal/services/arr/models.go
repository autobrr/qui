// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"github.com/autobrr/qui/internal/models"
)

// SystemStatusResponse represents the response from /api/v3/system/status (both Sonarr and Radarr)
type SystemStatusResponse struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

// SonarrParseResponse represents the response from Sonarr's /api/v3/parse endpoint
type SonarrParseResponse struct {
	Title             string                   `json:"title"`
	ParsedEpisodeInfo *SonarrParsedEpisodeInfo `json:"parsedEpisodeInfo"`
	Series            *SonarrSeries            `json:"series"`
}

// SonarrParsedEpisodeInfo contains parsed episode information from Sonarr
type SonarrParsedEpisodeInfo struct {
	SeriesTitle       string `json:"seriesTitle"`
	SeasonNumber      int    `json:"seasonNumber"`
	EpisodeNumbers    []int  `json:"episodeNumbers"`
	AbsoluteEpisode   int    `json:"absoluteEpisodeNumber"`
	Quality           any    `json:"quality"`
	ReleaseGroup      string `json:"releaseGroup"`
	ReleaseHash       string `json:"releaseHash"`
	IsDaily           bool   `json:"isDaily"`
	IsAbsoluteNumber  bool   `json:"isAbsoluteNumbering"`
	IsPossibleSpecial bool   `json:"isPossibleSpecialEpisode"`
}

// SonarrSeries represents a series in Sonarr (contains external IDs)
type SonarrSeries struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	TVDbID   int    `json:"tvdbId"`
	TVMazeID int    `json:"tvMazeId"`
	TMDbID   int    `json:"tmdbId"`
	IMDbID   string `json:"imdbId"`
}

// RadarrParseResponse represents the response from Radarr's /api/v3/parse endpoint
type RadarrParseResponse struct {
	Title           string                 `json:"title"`
	ParsedMovieInfo *RadarrParsedMovieInfo `json:"parsedMovieInfo"`
	Movie           *RadarrMovie           `json:"movie"`
}

// RadarrParsedMovieInfo contains parsed movie information from Radarr
// Note: parsedMovieInfo can contain IDs even when movie is nil (extracted from release name)
type RadarrParsedMovieInfo struct {
	MovieTitle   string `json:"movieTitle"`
	Year         int    `json:"year"`
	IMDbID       string `json:"imdbId"`
	TMDbID       int    `json:"tmdbId"`
	Quality      any    `json:"quality"`
	ReleaseGroup string `json:"releaseGroup"`
	ReleaseHash  string `json:"releaseHash"`
}

// RadarrMovie represents a movie in Radarr (contains external IDs)
type RadarrMovie struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	TMDbID int    `json:"tmdbId"`
	IMDbID string `json:"imdbId"`
}

// ExtractExternalIDs extracts external IDs from a Sonarr parse response
func (r *SonarrParseResponse) ExtractExternalIDs() *models.ExternalIDs {
	if r.Series == nil {
		return nil
	}

	ids := &models.ExternalIDs{}

	// Extract IDs, treating 0 as "not present"
	if r.Series.TVDbID > 0 {
		ids.TVDbID = r.Series.TVDbID
	}
	if r.Series.TVMazeID > 0 {
		ids.TVMazeID = r.Series.TVMazeID
	}
	if r.Series.TMDbID > 0 {
		ids.TMDbID = r.Series.TMDbID
	}
	if r.Series.IMDbID != "" && r.Series.IMDbID != "0" {
		ids.IMDbID = r.Series.IMDbID
	}

	if ids.IsEmpty() {
		return nil
	}

	return ids
}

// ExtractExternalIDs extracts external IDs from a Radarr parse response
func (r *RadarrParseResponse) ExtractExternalIDs() *models.ExternalIDs {
	ids := &models.ExternalIDs{}

	// First try to get IDs from the matched movie (most reliable)
	if r.Movie != nil {
		if r.Movie.TMDbID > 0 {
			ids.TMDbID = r.Movie.TMDbID
		}
		if r.Movie.IMDbID != "" && r.Movie.IMDbID != "0" {
			ids.IMDbID = r.Movie.IMDbID
		}
	}

	// If movie is nil or missing IDs, try parsedMovieInfo (can have IDs from release name)
	if r.ParsedMovieInfo != nil {
		if ids.TMDbID == 0 && r.ParsedMovieInfo.TMDbID > 0 {
			ids.TMDbID = r.ParsedMovieInfo.TMDbID
		}
		if ids.IMDbID == "" && r.ParsedMovieInfo.IMDbID != "" && r.ParsedMovieInfo.IMDbID != "0" {
			ids.IMDbID = r.ParsedMovieInfo.IMDbID
		}
	}

	if ids.IsEmpty() {
		return nil
	}

	return ids
}
