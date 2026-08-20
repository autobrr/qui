// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/httphelpers"
)

const (
	defaultTimeout   = 15 * time.Second
	defaultUserAgent = "qui/1.0"
)

// Client is an HTTP client for communicating with Sonarr/Radarr v3 API
type Client struct {
	instanceType models.ArrInstanceType
	baseURL      string
	apiKey       string
	basicUser    string
	basicPass    string
	httpClient   *http.Client
	timeout      time.Duration
}

// NewClient creates a new ARR API client
func NewClient(baseURL, apiKey string, basicUsername, basicPassword *string, instanceType models.ArrInstanceType, timeoutSeconds int) *Client {
	timeout := defaultTimeout
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	return &Client{
		instanceType: instanceType,
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		basicUser:    strings.TrimSpace(stringOrEmpty(basicUsername)),
		basicPass:    strings.TrimSpace(stringOrEmpty(basicPassword)),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// Ping tests connectivity to the ARR instance via GET /api/v3/system/status
func (c *Client) Ping(ctx context.Context) error {
	endpoint := c.baseURL + "/api/v3/system/status"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req) //nolint:bodyclose // closed by DrainAndClose
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer httphelpers.DrainAndClose(resp)

	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("authentication failed: invalid API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var status SystemStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Validate we got a valid response with app name
	if status.AppName == "" {
		return fmt.Errorf("invalid response: missing appName")
	}

	return nil
}

// ParseTitle calls the parse endpoint to resolve a title to external IDs
// For Sonarr: GET /api/v3/parse?title=<title>
// For Radarr: GET /api/v3/parse?title=<title>
func (c *Client) ParseTitle(ctx context.Context, title string) (*models.ExternalIDs, error) {
	result, err := c.ParseTitleLookupResult(ctx, title)
	if result == nil {
		return nil, err
	}
	return result.IDs, err
}

// ParseTitleLookupResult calls the parse endpoint to resolve a title to IDs and ARR title aliases.
func (c *Client) ParseTitleLookupResult(ctx context.Context, title string) (*ExternalIDsLookupResult, error) {
	endpoint := c.baseURL + "/api/v3/parse"

	// Build URL with query parameter
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint URL: %w", err)
	}
	q := u.Query()
	q.Set("title", title)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req) //nolint:bodyclose // closed by DrainAndClose
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httphelpers.DrainAndClose(resp)

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("authentication failed: invalid API key")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// Parse based on instance type
	switch c.instanceType {
	case models.ArrInstanceTypeSonarr:
		return c.parseSonarrResponse(ctx, resp.Body)
	case models.ArrInstanceTypeRadarr:
		return c.parseRadarrResponse(ctx, resp.Body)
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", c.instanceType)
	}
}

// LookupByTerm queries the ARR title-search endpoint (Radarr /api/v3/movie/lookup,
// Sonarr /api/v3/series/lookup) and returns IDs/titles from the best-matching
// candidate, or nil when no candidate matches term (and year, when known).
func (c *Client) LookupByTerm(ctx context.Context, term string, year int) (*ExternalIDsLookupResult, error) {
	params := url.Values{}
	params.Set("term", term)
	switch c.instanceType {
	case models.ArrInstanceTypeRadarr:
		var movies []RadarrMovie
		if err := c.getJSON(ctx, "/api/v3/movie/lookup", params, &movies); err != nil {
			return nil, err
		}
		return lookupResultFromRadarrMovie(selectRadarrLookupMatch(term, year, movies)), nil
	case models.ArrInstanceTypeSonarr:
		var series []SonarrSeries
		if err := c.getJSON(ctx, "/api/v3/series/lookup", params, &series); err != nil {
			return nil, err
		}
		return c.sonarrSeriesLookupResult(ctx, selectSonarrLookupMatch(term, year, series)), nil
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", c.instanceType)
	}
}

func (c *Client) sonarrSeriesLookupResult(ctx context.Context, series *SonarrSeries) *ExternalIDsLookupResult {
	result := lookupResultFromSonarrSeries(series)
	if series == nil || series.ID <= 0 {
		return result
	}

	fullSeries, err := c.sonarrSeriesByID(ctx, series.ID)
	if err != nil {
		return result
	}

	return mergeLookupResults(result, lookupResultFromSonarrSeries(fullSeries))
}

func (c *Client) sonarrSeriesByID(ctx context.Context, id int) (*SonarrSeries, error) {
	var series SonarrSeries
	if err := c.getJSON(ctx, fmt.Sprintf("/api/v3/series/%d", id), nil, &series); err != nil {
		return nil, err
	}
	return &series, nil
}

func (c *Client) radarrMovieLookupResult(ctx context.Context, parseResp *RadarrParseResponse) *ExternalIDsLookupResult {
	if parseResp == nil {
		return nil
	}

	result := parseResp.ExtractLookupResult()
	if parseResp.Movie == nil || parseResp.Movie.ID <= 0 {
		return result
	}

	fullMovie, err := c.radarrMovieByID(ctx, parseResp.Movie.ID)
	if err != nil {
		return result
	}

	return mergeLookupResults(result, lookupResultFromRadarrMovie(fullMovie))
}

func (c *Client) radarrMovieByID(ctx context.Context, id int) (*RadarrMovie, error) {
	var movie RadarrMovie
	if err := c.getJSON(ctx, fmt.Sprintf("/api/v3/movie/%d", id), nil, &movie); err != nil {
		return nil, err
	}
	return &movie, nil
}

func mergeLookupResults(base, hydrated *ExternalIDsLookupResult) *ExternalIDsLookupResult {
	if base == nil {
		return hydrated
	}
	if hydrated == nil {
		return base
	}

	result := &ExternalIDsLookupResult{
		IDs:    mergeExternalIDs(base.IDs, hydrated.IDs),
		Titles: append([]string(nil), base.Titles...),
	}
	for _, title := range hydrated.Titles {
		addUniqueTitle(&result.Titles, title)
	}
	if result.IDs == nil && len(result.Titles) == 0 {
		return nil
	}
	return result
}

func mergeExternalIDs(base, hydrated *models.ExternalIDs) *models.ExternalIDs {
	if base == nil {
		return hydrated
	}
	if hydrated == nil {
		return base
	}

	ids := *base
	if hydrated.TVDbID > 0 {
		ids.TVDbID = hydrated.TVDbID
	}
	if hydrated.TVMazeID > 0 {
		ids.TVMazeID = hydrated.TVMazeID
	}
	if hydrated.TMDbID > 0 {
		ids.TMDbID = hydrated.TMDbID
	}
	if hydrated.IMDbID != "" && hydrated.IMDbID != "0" {
		ids.IMDbID = hydrated.IMDbID
	}
	if ids.IsEmpty() {
		return nil
	}
	return &ids
}

// ParseSonarrTitle returns the full Sonarr parse response for TV lookups that need the series ID.
func (c *Client) ParseSonarrTitle(ctx context.Context, title string) (*SonarrParseResponse, error) {
	if c.instanceType != models.ArrInstanceTypeSonarr {
		return nil, fmt.Errorf("unsupported instance type for Sonarr parse: %s", c.instanceType)
	}

	var parseResp SonarrParseResponse
	params := url.Values{}
	params.Set("title", title)
	if err := c.getJSON(ctx, "/api/v3/parse", params, &parseResp); err != nil {
		if strings.Contains(err.Error(), "failed to decode response") {
			return nil, fmt.Errorf("failed to decode Sonarr parse response: %w", err)
		}
		return nil, err
	}

	return &parseResp, nil
}

// GetSonarrSeasonEpisodes fetches episodes for a specific Sonarr series season.
func (c *Client) GetSonarrSeasonEpisodes(ctx context.Context, seriesID, seasonNumber int) ([]SonarrEpisodeResource, error) {
	if c.instanceType != models.ArrInstanceTypeSonarr {
		return nil, fmt.Errorf("unsupported instance type for Sonarr episodes: %s", c.instanceType)
	}

	var episodes []SonarrEpisodeResource
	params := url.Values{}
	params.Set("seriesId", strconv.Itoa(seriesID))
	params.Set("seasonNumber", strconv.Itoa(seasonNumber))
	if err := c.getJSON(ctx, "/api/v3/episode", params, &episodes); err != nil {
		if strings.Contains(err.Error(), "failed to decode response") {
			return nil, fmt.Errorf("failed to decode Sonarr episode response: %w", err)
		}
		return nil, err
	}

	return episodes, nil
}

func (c *Client) getJSON(ctx context.Context, path string, params url.Values, target any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("failed to parse endpoint URL: %w", err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req) //nolint:bodyclose // closed by DrainAndClose
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer httphelpers.DrainAndClose(resp)

	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("authentication failed: invalid API key")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// parseSonarrResponse parses a Sonarr parse response and extracts external IDs
func (c *Client) parseSonarrResponse(ctx context.Context, body io.Reader) (*ExternalIDsLookupResult, error) {
	var parseResp SonarrParseResponse
	if err := json.NewDecoder(body).Decode(&parseResp); err != nil {
		return nil, fmt.Errorf("failed to decode Sonarr parse response: %w", err)
	}

	return c.sonarrSeriesLookupResult(ctx, parseResp.Series), nil
}

// parseRadarrResponse parses a Radarr parse response and extracts external IDs
func (c *Client) parseRadarrResponse(ctx context.Context, body io.Reader) (*ExternalIDsLookupResult, error) {
	var parseResp RadarrParseResponse
	if err := json.NewDecoder(body).Decode(&parseResp); err != nil {
		return nil, fmt.Errorf("failed to decode Radarr parse response: %w", err)
	}

	return c.radarrMovieLookupResult(ctx, &parseResp), nil
}

// setHeaders sets the required headers for ARR API requests
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "application/json")
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
