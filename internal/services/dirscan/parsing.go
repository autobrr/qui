// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/moistari/rls"

	"github.com/autobrr/qui/pkg/releases"
)

// TRaSHMetadata holds database IDs extracted from TRaSH Guides naming conventions.
// See: https://trash-guides.info/
type TRaSHMetadata struct {
	TMDbID  int    // From {tmdb-345691}
	IMDbID  string // From {imdb-tt1234567}
	TVDbID  int    // From [tvdb-123] or [tvdbid-123]
	Edition string // From {edition-Extended}
}

// trashIDPatterns matches TRaSH Guides naming conventions.
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

// SearcheeMetadata combines TRaSH IDs with rls parsed release metadata.
type SearcheeMetadata struct {
	// Original name before any processing
	OriginalName string

	// CleanedName has TRaSH IDs removed (suitable for rls parsing)
	CleanedName string

	// TRaSH IDs extracted from the name
	TRaSH TRaSHMetadata

	// Release metadata from rls parsing
	Release *rls.Release

	// Derived search fields
	Title   string // Cleaned title for search queries
	Year    int    // Year if detected
	Season  *int   // Season number for TV
	Episode *int   // Episode number for TV

	// Content type hints
	IsTV    bool
	IsMovie bool
	IsMusic bool
}

// Parser handles name parsing with TRaSH ID extraction and rls integration.
type Parser struct {
	rlsParser *releases.Parser
}

// NewParser creates a new name parser.
func NewParser(rlsParser *releases.Parser) *Parser {
	if rlsParser == nil {
		rlsParser = releases.NewDefaultParser()
	}
	return &Parser{rlsParser: rlsParser}
}

// Parse extracts TRaSH IDs and parses release metadata from a searchee name.
func (p *Parser) Parse(name string) *SearcheeMetadata {
	meta := &SearcheeMetadata{
		OriginalName: name,
	}

	// Extract TRaSH IDs and clean the name
	meta.CleanedName, meta.TRaSH = extractTRaSHMetadata(name)

	// Parse with rls
	meta.Release = p.rlsParser.Parse(meta.CleanedName)

	// Extract useful fields from rls result
	p.populateFromRelease(meta)

	return meta
}

// extractTRaSHMetadata extracts TRaSH Guides IDs from a name and returns the cleaned name.
func extractTRaSHMetadata(name string) (cleaned string, meta TRaSHMetadata) {
	cleaned = name

	// Extract TMDb ID
	if matches := trashIDPatterns.tmdb.FindStringSubmatch(name); len(matches) > 1 {
		if id, err := strconv.Atoi(matches[1]); err == nil {
			meta.TMDbID = id
		}
		cleaned = trashIDPatterns.tmdb.ReplaceAllString(cleaned, "")
	}

	// Extract IMDb ID
	if matches := trashIDPatterns.imdb.FindStringSubmatch(name); len(matches) > 1 {
		meta.IMDbID = matches[1]
		cleaned = trashIDPatterns.imdb.ReplaceAllString(cleaned, "")
	}

	// Extract TVDb ID (try both patterns)
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

	// Extract edition
	if matches := trashIDPatterns.edition.FindStringSubmatch(name); len(matches) > 1 {
		meta.Edition = matches[1]
		cleaned = trashIDPatterns.edition.ReplaceAllString(cleaned, "")
	}

	// Clean up extra spaces
	cleaned = cleanExtraSpaces(cleaned)

	return cleaned, meta
}

// cleanExtraSpaces removes duplicate spaces and trims.
func cleanExtraSpaces(s string) string {
	// Replace multiple spaces with single space
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// populateFromRelease extracts useful fields from the rls.Release.
func (p *Parser) populateFromRelease(meta *SearcheeMetadata) {
	if meta.Release == nil {
		return
	}

	r := meta.Release

	// Get the title
	meta.Title = r.Title
	if meta.Title == "" {
		meta.Title = meta.CleanedName
	}

	// Year
	meta.Year = r.Year

	// Season/Episode detection
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

	// Content type hints from rls
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

// HasExternalIDs returns true if any external database IDs are available.
func (m *SearcheeMetadata) HasExternalIDs() bool {
	return m.TRaSH.TMDbID > 0 || m.TRaSH.IMDbID != "" || m.TRaSH.TVDbID > 0
}

// GetIMDbID returns the IMDb ID in a format suitable for Torznab search.
// Returns empty string if not available.
func (m *SearcheeMetadata) GetIMDbID() string {
	return m.TRaSH.IMDbID
}

// GetTVDbID returns the TVDb ID if available, 0 otherwise.
func (m *SearcheeMetadata) GetTVDbID() int {
	return m.TRaSH.TVDbID
}

// GetTMDbID returns the TMDb ID if available, 0 otherwise.
func (m *SearcheeMetadata) GetTMDbID() int {
	return m.TRaSH.TMDbID
}

// SetExternalIDs updates the metadata with external IDs from arr lookup.
// Only sets IDs that are not already present from TRaSH naming.
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

// seasonFolderPattern matches season folder names like "Season 01", "Season 1", "Specials".
var seasonFolderPattern = regexp.MustCompile(`(?i)^(?:season\s*(\d+)|specials?)$`)

// ParseSeasonFolder attempts to parse a folder name as a season folder.
// Returns the season number (0 for Specials) and true if it matches, or 0 and false if not.
func ParseSeasonFolder(name string) (season int, ok bool) {
	matches := seasonFolderPattern.FindStringSubmatch(name)
	if len(matches) == 0 {
		return 0, false
	}

	// "Specials" case - no capture group
	if matches[1] == "" {
		return 0, true
	}

	// Season number case
	season, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}

	return season, true
}
