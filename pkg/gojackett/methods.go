// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"

	"github.com/autobrr/qui/pkg/redact"
)

func (c *Client) GetIndexers() (Indexers, error) {
	return c.GetIndexersCtx(context.Background())
}

func (c *Client) GetIndexersCtx(ctx context.Context) (Indexers, error) {
	opts := map[string]string{
		"t":          "indexers",
		"configured": "true",
	}

	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var ind Indexers
	resp, err := c.getCtx(ctx, "all/results/torznab/api", opts)
	if err != nil {
		return ind, errors.Wrap(err, "all endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&ind)
	return ind, err
}

func (c *Client) GetTorrents(indexer string, opts map[string]string) (Rss, error) {
	return c.GetTorrentsCtx(context.Background(), indexer, opts)
}

func (c *Client) GetTorrentsCtx(ctx context.Context, indexer string, opts map[string]string) (Rss, error) {
	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	resp, err := c.getCtx(ctx, indexer+"/results/torznab/api", opts)
	if err != nil {
		return rss, errors.Wrap(err, indexer+" endpoint error")
	}

	defer drainAndClose(resp.Body)

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return rss, errors.Wrap(err, "failed to read response")
	}

	// Check if the response is an error
	bodyStr := strings.TrimSpace(string(body))
	if strings.HasPrefix(bodyStr, "<error") {
		var torznabErr TorznabError
		if err := xml.Unmarshal(body, &torznabErr); err != nil {
			return rss, errors.Wrap(err, "failed to decode torznab error response")
		}
		return rss, fmt.Errorf("torznab error %s: %s", torznabErr.Code, torznabErr.Message)
	}

	// Decode the RSS response
	err = xml.Unmarshal(body, &rss)
	return rss, err
}

func (c *Client) GetEnclosure(enclosure string) ([]byte, error) {
	return c.GetEnclosureCtx(context.Background(), enclosure)
}

func (c *Client) GetEnclosureCtx(ctx context.Context, enclosure string) ([]byte, error) {
	resp, err := c.getRawCtx(ctx, enclosure)
	if err != nil {
		return nil, errors.Wrap(err, redact.URLString(enclosure))
	}

	defer drainAndClose(resp.Body)

	return io.ReadAll(resp.Body)
}

// SearchDirect performs a direct search against a tracker's torznab API
// This is useful for trackers that expose their own torznab endpoints
// Example: client configured with Host="https://www.morethantv.me/api/torznab" and DirectMode=true
func (c *Client) SearchDirect(query string, opts map[string]string) (Rss, error) {
	return c.SearchDirectCtx(context.Background(), query, opts)
}

// SearchDirectCtx performs a direct search against a tracker's torznab API with context
func (c *Client) SearchDirectCtx(ctx context.Context, query string, opts map[string]string) (Rss, error) {
	if opts == nil {
		opts = make(map[string]string)
	}

	// Set search query
	opts["t"] = "search"
	if query != "" {
		opts["q"] = query
	}

	// Add API key if configured
	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	resp, err := c.getCtx(ctx, "", opts)
	if err != nil {
		return rss, errors.Wrap(err, "direct search endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&rss)
	return rss, err
}

// GetCapsDirect retrieves the capabilities of a direct tracker torznab API
func (c *Client) GetCapsDirect() (Indexers, error) {
	return c.GetCapsDirectCtx(context.Background())
}

// GetCapsDirectCtx retrieves the capabilities of a direct tracker torznab API with context
func (c *Client) GetCapsDirectCtx(ctx context.Context) (Indexers, error) {
	opts := map[string]string{
		"t": "caps",
	}

	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var ind Indexers
	resp, err := c.getCtx(ctx, "", opts)
	if err != nil {
		return ind, errors.Wrap(err, "direct caps endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&ind)
	return ind, err
}

// TVSearchOptions represents parameters for TV show searches
type TVSearchOptions struct {
	Query    string // Free text query
	TVDBID   string // TheTVDB ID
	TVMazeID string // TVMaze ID
	RageID   string // TVRage ID (deprecated but still supported)
	Season   string // Season number
	Episode  string // Episode number
	IMDBID   string // IMDB ID (some indexers support this for TV)
	Limit    string // Number of results to return
	Offset   string // Number of results to skip
	Category string // Comma-separated category IDs (defaults to TV categories)
	Extended string // Set to "1" to include all extended attributes
}

// TVSearch performs a TV-specific search with the given options
func (c *Client) TVSearch(opts TVSearchOptions) (Rss, error) {
	return c.TVSearchCtx(context.Background(), opts)
}

// TVSearchCtx performs a TV-specific search with context
func (c *Client) TVSearchCtx(ctx context.Context, opts TVSearchOptions) (Rss, error) {
	params := map[string]string{
		"t": string(SearchTypeTV),
	}

	if opts.Query != "" {
		params["q"] = opts.Query
	}
	if opts.TVDBID != "" {
		params["tvdbid"] = opts.TVDBID
	}
	if opts.TVMazeID != "" {
		params["tvmazeid"] = opts.TVMazeID
	}
	if opts.RageID != "" {
		params["rid"] = opts.RageID
	}
	if opts.Season != "" {
		params["season"] = opts.Season
	}
	if opts.Episode != "" {
		params["ep"] = opts.Episode
	}
	if opts.IMDBID != "" {
		params["imdbid"] = opts.IMDBID
	}
	if opts.Limit != "" {
		params["limit"] = opts.Limit
	}
	if opts.Offset != "" {
		params["offset"] = opts.Offset
	}
	if opts.Category != "" {
		params["cat"] = opts.Category
	}
	if opts.Extended != "" {
		params["extended"] = opts.Extended
	}

	if len(c.cfg.APIKey) != 0 {
		params["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	endpoint := ""
	if !c.cfg.DirectMode {
		endpoint = "all/results/torznab/api"
	}

	resp, err := c.getCtx(ctx, endpoint, params)
	if err != nil {
		return rss, errors.Wrap(err, "tv search endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&rss)
	return rss, err
}

// MovieSearchOptions represents parameters for movie searches
type MovieSearchOptions struct {
	Query    string // Free text query
	IMDBID   string // IMDB ID (with or without tt prefix)
	TMDBID   string // The Movie Database ID
	Genre    string // Genre filter
	Year     string // Release year
	Limit    string // Number of results to return
	Offset   string // Number of results to skip
	Category string // Comma-separated category IDs (defaults to Movie categories)
	Extended string // Set to "1" to include all extended attributes
}

// MovieSearch performs a movie-specific search with the given options
func (c *Client) MovieSearch(opts MovieSearchOptions) (Rss, error) {
	return c.MovieSearchCtx(context.Background(), opts)
}

// MovieSearchCtx performs a movie-specific search with context
func (c *Client) MovieSearchCtx(ctx context.Context, opts MovieSearchOptions) (Rss, error) {
	params := map[string]string{
		"t": string(SearchTypeMovie),
	}

	if opts.Query != "" {
		params["q"] = opts.Query
	}
	if opts.IMDBID != "" {
		params["imdbid"] = opts.IMDBID
	}
	if opts.TMDBID != "" {
		params["tmdbid"] = opts.TMDBID
	}
	if opts.Genre != "" {
		params["genre"] = opts.Genre
	}
	if opts.Year != "" {
		params["year"] = opts.Year
	}
	if opts.Limit != "" {
		params["limit"] = opts.Limit
	}
	if opts.Offset != "" {
		params["offset"] = opts.Offset
	}
	if opts.Category != "" {
		params["cat"] = opts.Category
	}
	if opts.Extended != "" {
		params["extended"] = opts.Extended
	}

	if len(c.cfg.APIKey) != 0 {
		params["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	endpoint := ""
	if !c.cfg.DirectMode {
		endpoint = "all/results/torznab/api"
	}

	resp, err := c.getCtx(ctx, endpoint, params)
	if err != nil {
		return rss, errors.Wrap(err, "movie search endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&rss)
	return rss, err
}

// MusicSearchOptions represents parameters for music searches
type MusicSearchOptions struct {
	Query    string // Free text query
	Artist   string // Artist name
	Album    string // Album name
	Label    string // Record label
	Track    string // Track name
	Year     string // Release year
	Genre    string // Genre filter
	Limit    string // Number of results to return
	Offset   string // Number of results to skip
	Category string // Comma-separated category IDs (defaults to Audio categories)
	Extended string // Set to "1" to include all extended attributes
}

// MusicSearch performs a music-specific search with the given options
func (c *Client) MusicSearch(opts MusicSearchOptions) (Rss, error) {
	return c.MusicSearchCtx(context.Background(), opts)
}

// MusicSearchCtx performs a music-specific search with context
func (c *Client) MusicSearchCtx(ctx context.Context, opts MusicSearchOptions) (Rss, error) {
	params := map[string]string{
		"t": string(SearchTypeMusic),
	}

	if opts.Query != "" {
		params["q"] = opts.Query
	}
	if opts.Artist != "" {
		params["artist"] = opts.Artist
	}
	if opts.Album != "" {
		params["album"] = opts.Album
	}
	if opts.Label != "" {
		params["label"] = opts.Label
	}
	if opts.Track != "" {
		params["track"] = opts.Track
	}
	if opts.Year != "" {
		params["year"] = opts.Year
	}
	if opts.Genre != "" {
		params["genre"] = opts.Genre
	}
	if opts.Limit != "" {
		params["limit"] = opts.Limit
	}
	if opts.Offset != "" {
		params["offset"] = opts.Offset
	}
	if opts.Category != "" {
		params["cat"] = opts.Category
	}
	if opts.Extended != "" {
		params["extended"] = opts.Extended
	}

	if len(c.cfg.APIKey) != 0 {
		params["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	endpoint := ""
	if !c.cfg.DirectMode {
		endpoint = "all/results/torznab/api"
	}

	resp, err := c.getCtx(ctx, endpoint, params)
	if err != nil {
		return rss, errors.Wrap(err, "music search endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&rss)
	return rss, err
}

// BookSearchOptions represents parameters for book searches
type BookSearchOptions struct {
	Query    string // Free text query
	Author   string // Author name
	Title    string // Book title
	Year     string // Publication year
	Genre    string // Genre filter
	Limit    string // Number of results to return
	Offset   string // Number of results to skip
	Category string // Comma-separated category IDs (defaults to Book categories)
	Extended string // Set to "1" to include all extended attributes
}

// BookSearch performs a book-specific search with the given options
func (c *Client) BookSearch(opts BookSearchOptions) (Rss, error) {
	return c.BookSearchCtx(context.Background(), opts)
}

// BookSearchCtx performs a book-specific search with context
func (c *Client) BookSearchCtx(ctx context.Context, opts BookSearchOptions) (Rss, error) {
	params := map[string]string{
		"t": string(SearchTypeBook),
	}

	if opts.Query != "" {
		params["q"] = opts.Query
	}
	if opts.Author != "" {
		params["author"] = opts.Author
	}
	if opts.Title != "" {
		params["title"] = opts.Title
	}
	if opts.Year != "" {
		params["year"] = opts.Year
	}
	if opts.Genre != "" {
		params["genre"] = opts.Genre
	}
	if opts.Limit != "" {
		params["limit"] = opts.Limit
	}
	if opts.Offset != "" {
		params["offset"] = opts.Offset
	}
	if opts.Category != "" {
		params["cat"] = opts.Category
	}
	if opts.Extended != "" {
		params["extended"] = opts.Extended
	}

	if len(c.cfg.APIKey) != 0 {
		params["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	endpoint := ""
	if !c.cfg.DirectMode {
		endpoint = "all/results/torznab/api"
	}

	resp, err := c.getCtx(ctx, endpoint, params)
	if err != nil {
		return rss, errors.Wrap(err, "book search endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&rss)
	return rss, err
}
