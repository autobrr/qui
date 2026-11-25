// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"strings"
	"unicode"

	"github.com/autobrr/qui/internal/models"
)

type SystemStatusResponse struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

type SonarrParseResponse struct {
	Title             string                   `json:"title"`
	ParsedEpisodeInfo *SonarrParsedEpisodeInfo `json:"parsedEpisodeInfo"`
	Series            *SonarrSeries            `json:"series"`
	Episodes          []SonarrEpisodeResource  `json:"episodes"`
}

type SonarrParsedEpisodeInfo struct {
	SeriesTitle       string `json:"seriesTitle"`
	SeasonNumber      int    `json:"seasonNumber"`
	EpisodeNumbers    []int  `json:"episodeNumbers"`
	Quality           any    `json:"quality"`
	ReleaseGroup      string `json:"releaseGroup"`
	ReleaseHash       string `json:"releaseHash"`
	IsDaily           bool   `json:"isDaily"`
	IsAbsoluteNumber  bool   `json:"isAbsoluteNumbering"`
	IsPossibleSpecial bool   `json:"isPossibleSpecialEpisode"`
}

type SonarrSeries struct {
	ID              int              `json:"id"`
	Title           string           `json:"title"`
	Year            int              `json:"year"`
	AlternateTitles []AlternateTitle `json:"alternateTitles"`
	TVDbID          int              `json:"tvdbId"`
	TVMazeID        int              `json:"tvMazeId"`
	TMDbID          int              `json:"tmdbId"`
	IMDbID          string           `json:"imdbId"`
}

type SonarrEpisodeResource struct {
	ID                    int  `json:"id"`
	SeasonNumber          int  `json:"seasonNumber"`
	EpisodeNumber         int  `json:"episodeNumber"`
	AbsoluteEpisodeNumber *int `json:"absoluteEpisodeNumber"`
}

type RadarrParseResponse struct {
	Title           string                 `json:"title"`
	ParsedMovieInfo *RadarrParsedMovieInfo `json:"parsedMovieInfo"`
	Movie           *RadarrMovie           `json:"movie"`
}

type RadarrParsedMovieInfo struct {
	MovieTitle   string `json:"movieTitle"`
	Year         int    `json:"year"`
	IMDbID       string `json:"imdbId"`
	TMDbID       int    `json:"tmdbId"`
	Quality      any    `json:"quality"`
	ReleaseGroup string `json:"releaseGroup"`
	ReleaseHash  string `json:"releaseHash"`
}

type RadarrMovie struct {
	ID              int              `json:"id"`
	Title           string           `json:"title"`
	Year            int              `json:"year"`
	OriginalTitle   string           `json:"originalTitle"`
	AlternateTitles []AlternateTitle `json:"alternateTitles"`
	TMDbID          int              `json:"tmdbId"`
	IMDbID          string           `json:"imdbId"`
}

type AlternateTitle struct {
	Title             string `json:"title"`
	SeasonNumber      *int   `json:"seasonNumber"`
	SceneSeasonNumber *int   `json:"sceneSeasonNumber"`
}

func (a AlternateTitle) seasonScoped() bool {
	return (a.SeasonNumber != nil && *a.SeasonNumber >= 0) ||
		(a.SceneSeasonNumber != nil && *a.SceneSeasonNumber >= 0)
}

func (a AlternateTitle) matchesSeason(season int) bool {
	return a.SeasonNumber != nil && *a.SeasonNumber == season
}

type ExternalIDsLookupResult struct {
	IDs        *models.ExternalIDs
	Titles     []string
	EpisodeMap *models.EpisodeMap
	Incomplete bool
}

func (r *SonarrParseResponse) ExtractLookupResult() *ExternalIDsLookupResult {
	if r == nil {
		return nil
	}
	result := lookupResultFromSonarrSeries(r.Series)
	if result != nil {
		result.EpisodeMap = r.episodeMap()
	}
	return result
}

func (r *SonarrParseResponse) episodeMap() *models.EpisodeMap {
	if len(r.Episodes) != 1 || r.Episodes[0].AbsoluteEpisodeNumber == nil {
		return nil
	}
	episode := r.Episodes[0]
	return &models.EpisodeMap{
		Season:   episode.SeasonNumber,
		Episode:  episode.EpisodeNumber,
		Absolute: *episode.AbsoluteEpisodeNumber,
	}
}

func externalIDsFromSonarrSeries(series *SonarrSeries) *models.ExternalIDs {
	if series == nil {
		return nil
	}
	ids := &models.ExternalIDs{}

	if series.TVDbID > 0 {
		ids.TVDbID = series.TVDbID
	}
	if series.TVMazeID > 0 {
		ids.TVMazeID = series.TVMazeID
	}
	if series.TMDbID > 0 {
		ids.TMDbID = series.TMDbID
	}
	if series.IMDbID != "" && series.IMDbID != "0" {
		ids.IMDbID = series.IMDbID
	}

	if ids.IsEmpty() {
		return nil
	}

	return ids
}

func lookupResultFromSonarrSeries(series *SonarrSeries) *ExternalIDsLookupResult {
	if series == nil {
		return nil
	}

	ids := externalIDsFromSonarrSeries(series)
	titles := titlesFromSeries(series)
	if ids == nil && len(titles) == 0 {
		return nil
	}

	return &ExternalIDsLookupResult{
		IDs:    ids,
		Titles: titles,
	}
}

func (r *RadarrParseResponse) ExtractLookupResult() *ExternalIDsLookupResult {
	if r == nil {
		return nil
	}

	ids := externalIDsFromRadarrMovie(r.Movie)
	if ids == nil {
		ids = &models.ExternalIDs{}
	}

	if r.ParsedMovieInfo != nil {
		if ids.TMDbID == 0 && r.ParsedMovieInfo.TMDbID > 0 {
			ids.TMDbID = r.ParsedMovieInfo.TMDbID
		}
		if ids.IMDbID == "" && r.ParsedMovieInfo.IMDbID != "" && r.ParsedMovieInfo.IMDbID != "0" {
			ids.IMDbID = r.ParsedMovieInfo.IMDbID
		}
	}

	if ids.IsEmpty() {
		ids = nil
	}

	titles := titlesFromMovie(r.Movie)
	if ids == nil && len(titles) == 0 {
		return nil
	}

	return &ExternalIDsLookupResult{
		IDs:    ids,
		Titles: titles,
	}
}

func lookupResultFromRadarrMovie(movie *RadarrMovie) *ExternalIDsLookupResult {
	if movie == nil {
		return nil
	}

	ids := externalIDsFromRadarrMovie(movie)
	titles := titlesFromMovie(movie)
	if ids == nil && len(titles) == 0 {
		return nil
	}

	return &ExternalIDsLookupResult{
		IDs:    ids,
		Titles: titles,
	}
}

func externalIDsFromRadarrMovie(movie *RadarrMovie) *models.ExternalIDs {
	if movie == nil {
		return nil
	}
	ids := &models.ExternalIDs{}

	if movie.TMDbID > 0 {
		ids.TMDbID = movie.TMDbID
	}
	if movie.IMDbID != "" && movie.IMDbID != "0" {
		ids.IMDbID = movie.IMDbID
	}

	if ids.IsEmpty() {
		return nil
	}

	return ids
}

func titlesFromSeries(series *SonarrSeries) []string {
	if series == nil {
		return nil
	}

	titles := make([]string, 0, 1+len(series.AlternateTitles))
	addUniqueTitle(&titles, series.Title)
	for _, alternate := range series.AlternateTitles {
		addUniqueTitle(&titles, alternate.Title)
	}
	return titles
}

func titlesForSeason(series *SonarrSeries, season int) []string {
	if series == nil {
		return nil
	}
	titles := make([]string, 0, 1+len(series.AlternateTitles))
	addUniqueTitle(&titles, series.Title)
	for _, alternate := range series.AlternateTitles {
		if alternate.seasonScoped() && !alternate.matchesSeason(season) {
			continue
		}
		addUniqueTitle(&titles, alternate.Title)
	}
	return titles
}

func seasonLookupTitles(series *SonarrSeries, lookupTitle string, season int) []string {
	if series == nil {
		return nil
	}
	normalized := normalizeTitleWords(lookupTitle)
	seriesTitle := normalizeTitleWords(series.Title)
	for _, alternate := range series.AlternateTitles {
		if !alternate.seasonScoped() || alternate.matchesSeason(season) {
			continue
		}
		alias := normalizeTitleWords(alternate.Title)
		if alias == "" || alias == seriesTitle {
			continue
		}
		if normalized == alias || strings.HasPrefix(normalized, alias+" ") {
			return nil
		}
	}
	return titlesForSeason(series, season)
}

func lookupYearMatches(releaseYear, candidateYear int) bool {
	if releaseYear <= 0 || candidateYear <= 0 {
		return true
	}
	diff := releaseYear - candidateYear
	return diff >= -1 && diff <= 1
}

func selectRadarrLookupMatch(term string, year int, movies []RadarrMovie) *RadarrMovie {
	normalized := normalizeTitleWords(term)
	if normalized == "" {
		return nil
	}
	var exactInLibrary, exactMatches, inLibrary, matches []*RadarrMovie
	for i := range movies {
		movie := &movies[i]
		if normalizeTitleWords(movie.Title) != normalized && normalizeTitleWords(movie.OriginalTitle) != normalized {
			continue
		}
		if !lookupYearMatches(year, movie.Year) {
			continue
		}
		switch {
		case year > 0 && movie.Year == year && movie.ID > 0:
			exactInLibrary = append(exactInLibrary, movie)
		case year > 0 && movie.Year == year:
			exactMatches = append(exactMatches, movie)
		case movie.ID > 0:
			inLibrary = append(inLibrary, movie)
		default:
			matches = append(matches, movie)
		}
	}
	if len(exactInLibrary) > 0 {
		return exactInLibrary[0]
	}
	if len(exactMatches) > 0 {
		return exactMatches[0]
	}
	if len(inLibrary) == 1 || (len(inLibrary) > 1 && year > 0) {
		return inLibrary[0]
	}
	if len(inLibrary) > 1 {
		return nil
	}
	if len(matches) == 1 || (len(matches) > 1 && year > 0) {
		return matches[0]
	}
	return nil
}

func selectSonarrLookupMatch(term string, year int, series []SonarrSeries) *SonarrSeries {
	normalized := normalizeTitleWords(term)
	if normalized == "" {
		return nil
	}
	var exactInLibrary, exactMatches, inLibrary, matches []*SonarrSeries
	for i := range series {
		candidate := &series[i]
		if normalizeTitleWords(candidate.Title) != normalized {
			continue
		}
		if !lookupYearMatches(year, candidate.Year) {
			continue
		}
		switch {
		case year > 0 && candidate.Year == year && candidate.ID > 0:
			exactInLibrary = append(exactInLibrary, candidate)
		case year > 0 && candidate.Year == year:
			exactMatches = append(exactMatches, candidate)
		case candidate.ID > 0:
			inLibrary = append(inLibrary, candidate)
		default:
			matches = append(matches, candidate)
		}
	}
	if len(exactInLibrary) > 0 {
		return exactInLibrary[0]
	}
	if len(exactMatches) > 0 {
		return exactMatches[0]
	}
	if len(inLibrary) == 1 || (len(inLibrary) > 1 && year > 0) {
		return inLibrary[0]
	}
	if len(inLibrary) > 1 {
		return nil
	}
	if len(matches) == 1 || (len(matches) > 1 && year > 0) {
		return matches[0]
	}
	return nil
}

func normalizeTitleWords(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func titlesFromMovie(movie *RadarrMovie) []string {
	if movie == nil {
		return nil
	}

	titles := make([]string, 0, 2+len(movie.AlternateTitles))
	addUniqueTitle(&titles, movie.Title)
	addUniqueTitle(&titles, movie.OriginalTitle)
	for _, alternate := range movie.AlternateTitles {
		addUniqueTitle(&titles, alternate.Title)
	}
	return titles
}

func addUniqueTitle(titles *[]string, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	for _, existing := range *titles {
		if strings.EqualFold(existing, title) {
			return
		}
	}
	*titles = append(*titles, title)
}
