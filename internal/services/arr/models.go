// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"strings"
	"unicode"

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
	ID              int              `json:"id"`
	Title           string           `json:"title"`
	Year            int              `json:"year"`
	AlternateTitles []AlternateTitle `json:"alternateTitles"`
	TVDbID          int              `json:"tvdbId"`
	TVMazeID        int              `json:"tvMazeId"`
	TMDbID          int              `json:"tmdbId"`
	IMDbID          string           `json:"imdbId"`
}

// SonarrEpisodeResource represents the subset of Sonarr episode fields needed for season counts.
type SonarrEpisodeResource struct {
	ID            int `json:"id"`
	SeasonNumber  int `json:"seasonNumber"`
	EpisodeNumber int `json:"episodeNumber"`
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
	ID              int              `json:"id"`
	Title           string           `json:"title"`
	Year            int              `json:"year"`
	OriginalTitle   string           `json:"originalTitle"`
	AlternateTitles []AlternateTitle `json:"alternateTitles"`
	TMDbID          int              `json:"tmdbId"`
	IMDbID          string           `json:"imdbId"`
}

// AlternateTitle represents an ARR alternate title. SeasonNumber/SceneSeasonNumber scope
// the title to a specific season when present; Sonarr uses null or -1 for series-wide
// titles. Radarr movies never set these (movies have no seasons).
type AlternateTitle struct {
	Title             string `json:"title"`
	SeasonNumber      *int   `json:"seasonNumber"`
	SceneSeasonNumber *int   `json:"sceneSeasonNumber"`
}

// seasonScoped reports whether the alternate title names only one season (e.g.
// "Show 2nd Season") rather than the whole series. Sonarr uses null or -1 for
// series-wide titles.
func (a AlternateTitle) seasonScoped() bool {
	return (a.SeasonNumber != nil && *a.SeasonNumber >= 0) ||
		(a.SceneSeasonNumber != nil && *a.SceneSeasonNumber >= 0)
}

// matchesSeason reports whether a season-scoped alternate title names the given season.
// Only SeasonNumber (Sonarr's numbering) counts: SceneSeasonNumber lives in the
// release-label domain, where a "Show 2nd Season" alias typically carries
// sceneSeasonNumber 1, so scene equality would bridge wrong-season aliases onto the
// pack (a season-2 alias kept for a season-1 lookup). An alias scoped only by scene
// season therefore matches no season, since its Sonarr season is unknown.
func (a AlternateTitle) matchesSeason(season int) bool {
	return a.SeasonNumber != nil && *a.SeasonNumber == season
}

// ExternalIDsLookupResult contains ARR IDs plus ARR-provided titles for the same content.
type ExternalIDsLookupResult struct {
	IDs    *models.ExternalIDs
	Titles []string
}

// ExtractExternalIDs extracts external IDs from a Sonarr parse response
func (r *SonarrParseResponse) ExtractExternalIDs() *models.ExternalIDs {
	result := r.ExtractLookupResult()
	if result == nil {
		return nil
	}
	return result.IDs
}

// ExtractLookupResult extracts external IDs and title aliases from a Sonarr parse response.
func (r *SonarrParseResponse) ExtractLookupResult() *ExternalIDsLookupResult {
	if r == nil {
		return nil
	}
	return lookupResultFromSonarrSeries(r.Series)
}

func externalIDsFromSonarrSeries(series *SonarrSeries) *models.ExternalIDs {
	if series == nil {
		return nil
	}
	ids := &models.ExternalIDs{}

	// Extract IDs, treating 0 as "not present"
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

// ExtractExternalIDs extracts external IDs from a Radarr parse response
func (r *RadarrParseResponse) ExtractExternalIDs() *models.ExternalIDs {
	result := r.ExtractLookupResult()
	if result == nil {
		return nil
	}
	return result.IDs
}

// ExtractLookupResult extracts external IDs and title aliases from a Radarr parse response.
func (r *RadarrParseResponse) ExtractLookupResult() *ExternalIDsLookupResult {
	if r == nil {
		return nil
	}

	ids := externalIDsFromRadarrMovie(r.Movie)
	if ids == nil {
		ids = &models.ExternalIDs{}
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

// titlesForSeason returns the series title plus the alternate titles usable for the
// given season: series-wide aliases (romaji/english/abbreviated) and any alias scoped
// to that season. Aliases scoped to a DIFFERENT season are dropped so, e.g., a
// "Show 2nd Season" alias cannot bridge season-2 episodes onto a season-1 pack.
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

// seasonLookupTitles returns titlesForSeason, unless the looked-up release title is
// itself a season-scoped alias whose Sonarr season is not the one the release labels
// (e.g. a "Show 2nd Season" pack labeled S01). The pack's numbering is then alias-local,
// and expanding to the canonical title would bridge wrong-season canonical locals onto
// it, so alias expansion is suppressed and matching falls back to literal titles.
//
// An alias that normalizes to the series title itself (XEM scene mappings often carry
// the bare canonical title per season, and normalization strips the "♪♪"/"∬"-style
// punctuation distinguishing sequel titles) carries no season signal: every pack of the
// show is prefixed by it, so treating it as an alias-titled pack would suppress alias
// matching for all canonically-titled packs.
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

// lookupYearMatches reports whether a candidate's year is compatible with the
// release's parsed year. Unknown years on either side cannot disprove a match;
// ±1 absorbs premiere-date vs release-date drift.
func lookupYearMatches(releaseYear, candidateYear int) bool {
	if releaseYear <= 0 || candidateYear <= 0 {
		return true
	}
	diff := releaseYear - candidateYear
	return diff >= -1 && diff <= 1
}

// selectRadarrLookupMatch returns the lookup candidate whose title (or original
// title) matches term after normalization and whose year is compatible with the
// release's parsed year; nil when none match. Same-title remakes are the wrong-bind
// hazard here: with a year both sides, the year decides; with no year, prefer the
// in-library candidate (nonzero id), and refuse an ambiguous multi-candidate set
// rather than gamble on lookup order — a wrong pick would be cached for 30 days.
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
	// An exact-year candidate always beats a ±1 one: the tolerance exists for
	// premiere-date drift of the same work, not to let an adjacent-year remake
	// win on library membership.
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
		return nil // two same-title library entries and no year to pick between them
	}
	if len(matches) == 1 || (len(matches) > 1 && year > 0) {
		return matches[0]
	}
	return nil
}

// selectSonarrLookupMatch is selectRadarrLookupMatch for series, matching on title only.
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
		return nil // two same-title library entries and no year to pick between them
	}
	if len(matches) == 1 || (len(matches) > 1 && year > 0) {
		return matches[0]
	}
	return nil
}

// normalizeTitleWords lowercases and reduces a title (or release name) to space-separated
// alphanumeric words, so "Oshi.no.Ko.2nd.Season.S01..." prefix-matches "Oshi no Ko 2nd Season".
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
