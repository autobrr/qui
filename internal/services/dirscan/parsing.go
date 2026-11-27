// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/autobrr/rls"

	"github.com/autobrr/qui/pkg/releases"
)

type TRaSHMetadata struct {
	TMDbID  int
	IMDbID  string
	TVDbID  int
	Edition string
}

var trashIDPatterns = struct {
	tmdb    *regexp.Regexp
	imdb    *regexp.Regexp
	tvdb    *regexp.Regexp
	tvdbid  *regexp.Regexp
	edition *regexp.Regexp
}{
	tmdb:    regexp.MustCompile(`\{tmdb-(\d+)\}`),
	imdb:    regexp.MustCompile(`\{imdb-(tt\d+)\}`),
	tvdb:    regexp.MustCompile(`\[tvdb-?(\d+)\]`),
	tvdbid:  regexp.MustCompile(`\[tvdbid-(\d+)\]`),
	edition: regexp.MustCompile(`\{edition-([^}]+)\}`),
}

var spaceRe = regexp.MustCompile(`\s+`)

type SearcheeMetadata struct {
	OriginalName string

	CleanedName string

	TRaSH TRaSHMetadata

	Release *rls.Release

	Title   string
	Year    int
	Season  *int
	Episode *int

	IsTV    bool
	IsMovie bool
	IsMusic bool
}

type Parser struct {
	rlsParser *releases.Parser
}

func NewParser(rlsParser *releases.Parser) *Parser {
	if rlsParser == nil {
		rlsParser = releases.NewDefaultParser()
	}
	return &Parser{rlsParser: rlsParser}
}

func (p *Parser) Parse(name string) *SearcheeMetadata {
	meta := &SearcheeMetadata{
		OriginalName: name,
	}

	meta.CleanedName, meta.TRaSH = extractTRaSHMetadata(name)

	meta.Release = p.rlsParser.Parse(meta.CleanedName)

	p.populateFromRelease(meta)

	return meta
}

func extractTRaSHMetadata(name string) (cleaned string, meta TRaSHMetadata) {
	cleaned = name

	if matches := trashIDPatterns.tmdb.FindStringSubmatch(name); len(matches) > 1 {
		if id, err := strconv.Atoi(matches[1]); err == nil {
			meta.TMDbID = id
		}
		cleaned = trashIDPatterns.tmdb.ReplaceAllString(cleaned, "")
	}

	if matches := trashIDPatterns.imdb.FindStringSubmatch(name); len(matches) > 1 {
		meta.IMDbID = matches[1]
		cleaned = trashIDPatterns.imdb.ReplaceAllString(cleaned, "")
	}

	if matches := trashIDPatterns.tvdb.FindStringSubmatch(name); len(matches) > 1 {
		if id, err := strconv.Atoi(matches[1]); err == nil {
			meta.TVDbID = id
		}
		cleaned = trashIDPatterns.tvdb.ReplaceAllString(cleaned, "")
	} else if matches := trashIDPatterns.tvdbid.FindStringSubmatch(name); len(matches) > 1 {
		if id, err := strconv.Atoi(matches[1]); err == nil {
			meta.TVDbID = id
		}
		cleaned = trashIDPatterns.tvdbid.ReplaceAllString(cleaned, "")
	}

	if matches := trashIDPatterns.edition.FindStringSubmatch(name); len(matches) > 1 {
		meta.Edition = matches[1]
		cleaned = trashIDPatterns.edition.ReplaceAllString(cleaned, "")
	}

	cleaned = cleanExtraSpaces(cleaned)

	return cleaned, meta
}

func cleanExtraSpaces(s string) string {
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func (p *Parser) populateFromRelease(meta *SearcheeMetadata) {
	if meta.Release == nil {
		return
	}

	r := meta.Release

	meta.Title = r.Title
	if meta.Title == "" {
		meta.Title = meta.CleanedName
	}

	meta.Year = r.Year

	if r.Series > 0 {
		season := r.Series
		meta.Season = &season
		meta.IsTV = true
	}
	if r.Episode > 0 {
		episode := r.Episode
		meta.Episode = &episode
		meta.IsTV = true
	}

	if r.Type == rls.Series || r.Type == rls.Episode {
		meta.IsTV = true
	}
	if r.Type == rls.Movie {
		meta.IsMovie = true
	}
	if r.Type == rls.Music {
		meta.IsMusic = true
	}
}

func (m *SearcheeMetadata) HasExternalIDs() bool {
	return m.TRaSH.TMDbID > 0 || m.TRaSH.IMDbID != "" || m.TRaSH.TVDbID > 0
}

func (m *SearcheeMetadata) GetIMDbID() string {
	return m.TRaSH.IMDbID
}

func (m *SearcheeMetadata) GetTVDbID() int {
	return m.TRaSH.TVDbID
}

func (m *SearcheeMetadata) GetTMDbID() int {
	return m.TRaSH.TMDbID
}

func (m *SearcheeMetadata) SetExternalIDs(imdbID string, tmdbID, tvdbID int) {
	if m.TRaSH.IMDbID == "" && imdbID != "" {
		m.TRaSH.IMDbID = imdbID
	}
	if m.TRaSH.TMDbID == 0 && tmdbID > 0 {
		m.TRaSH.TMDbID = tmdbID
	}
	if m.TRaSH.TVDbID == 0 && tvdbID > 0 {
		m.TRaSH.TVDbID = tvdbID
	}
}
