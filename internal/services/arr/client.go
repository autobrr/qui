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
		return c.parseRadarrResponse(resp.Body)
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", c.instanceType)
	}
}

// QueueExternalIDs resolves external IDs from ARR queue records by download client ID.
func (c *Client) QueueExternalIDs(ctx context.Context, downloadID string) (*models.ExternalIDs, error) {
	result, err := c.QueueLookupResult(ctx, downloadID)
	if result == nil {
		return nil, err
	}
	return result.IDs, err
}

// QueueLookupResult resolves external IDs and title aliases from ARR queue records by download client ID.
func (c *Client) QueueLookupResult(ctx context.Context, downloadID string) (*ExternalIDsLookupResult, error) {
	switch c.instanceType {
	case models.ArrInstanceTypeSonarr:
		return c.sonarrQueueExternalIDs(ctx, downloadID)
	case models.ArrInstanceTypeRadarr:
		return c.radarrQueueExternalIDs(ctx, downloadID)
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", c.instanceType)
	}
}

// HistoryExternalIDs resolves external IDs from ARR history records by download client ID.
func (c *Client) HistoryExternalIDs(ctx context.Context, downloadID string) (*models.ExternalIDs, error) {
	result, err := c.HistoryLookupResult(ctx, downloadID)
	if result == nil {
		return nil, err
	}
	return result.IDs, err
}

// HistoryLookupResult resolves external IDs and title aliases from ARR history records by download client ID.
func (c *Client) HistoryLookupResult(ctx context.Context, downloadID string) (*ExternalIDsLookupResult, error) {
	switch c.instanceType {
	case models.ArrInstanceTypeSonarr:
		return c.sonarrHistoryExternalIDs(ctx, downloadID)
	case models.ArrInstanceTypeRadarr:
		return c.radarrHistoryExternalIDs(ctx, downloadID)
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", c.instanceType)
	}
}

func (c *Client) sonarrQueueExternalIDs(ctx context.Context, downloadID string) (*ExternalIDsLookupResult, error) {
	// Queue lookup intentionally checks only the first page: it is a best-effort downloadID match for ARR titles before history/parse fallback.
	var queue SonarrQueueResponse
	if err := c.getJSON(ctx, "/api/v3/queue", sonarrQueueParams(), &queue); err != nil {
		return nil, err
	}

	for _, item := range queue.Records {
		if downloadIDsEqual(item.DownloadID, downloadID) {
			if result := c.sonarrSeriesLookupResult(ctx, item.Series); result != nil && result.IDs != nil {
				return result, nil
			}
		}
	}
	return nil, nil
}

func (c *Client) radarrQueueExternalIDs(ctx context.Context, downloadID string) (*ExternalIDsLookupResult, error) {
	var queue RadarrQueueResponse
	if err := c.getJSON(ctx, "/api/v3/queue", radarrQueueParams(), &queue); err != nil {
		return nil, err
	}

	for _, item := range queue.Records {
		if downloadIDsEqual(item.DownloadID, downloadID) {
			if result := lookupResultFromRadarrMovie(item.Movie); result != nil && result.IDs != nil {
				return result, nil
			}
		}
	}
	return nil, nil
}

func (c *Client) sonarrHistoryExternalIDs(ctx context.Context, downloadID string) (*ExternalIDsLookupResult, error) {
	var history SonarrHistoryResponse
	if err := c.getJSON(ctx, "/api/v3/history", sonarrHistoryParams(downloadID), &history); err != nil {
		return nil, err
	}
	return c.sonarrHistoryIDs(ctx, history.Records, downloadID), nil
}

func (c *Client) radarrHistoryExternalIDs(ctx context.Context, downloadID string) (*ExternalIDsLookupResult, error) {
	var history RadarrHistoryResponse
	if err := c.getJSON(ctx, "/api/v3/history", radarrHistoryParams(downloadID), &history); err != nil {
		return nil, err
	}
	return radarrHistoryIDs(history.Records, downloadID), nil
}

func (c *Client) sonarrHistoryIDs(ctx context.Context, records []SonarrHistoryItem, downloadID string) *ExternalIDsLookupResult {
	var fallback *ExternalIDsLookupResult

	for _, item := range records {
		if downloadIDsEqual(item.DownloadID, downloadID) {
			result := c.sonarrSeriesLookupResult(ctx, item.Series)
			if result == nil || result.IDs == nil {
				continue
			}
			if usefulSonarrHistoryEvent(item.EventType) {
				return result
			}
			if fallback == nil {
				fallback = result
			}
		}
	}
	return fallback
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

func radarrHistoryIDs(records []RadarrHistoryItem, downloadID string) *ExternalIDsLookupResult {
	for _, item := range records {
		if downloadIDsEqual(item.DownloadID, downloadID) && usefulRadarrHistoryEvent(item.EventType) {
			if result := lookupResultFromRadarrMovie(item.Movie); result != nil && result.IDs != nil {
				return result
			}
		}
	}
	for _, item := range records {
		if downloadIDsEqual(item.DownloadID, downloadID) {
			if result := lookupResultFromRadarrMovie(item.Movie); result != nil && result.IDs != nil {
				return result
			}
		}
	}
	return nil
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

func sonarrQueueParams() url.Values {
	params := url.Values{}
	params.Set("includeSeries", "true")
	params.Set("includeEpisode", "true")
	params.Set("pageSize", "100")
	return params
}

func radarrQueueParams() url.Values {
	params := url.Values{}
	params.Set("includeMovie", "true")
	params.Set("pageSize", "100")
	return params
}

func sonarrHistoryParams(downloadID string) url.Values {
	params := historyParams(downloadID)
	params.Set("includeSeries", "true")
	return params
}

func radarrHistoryParams(downloadID string) url.Values {
	params := historyParams(downloadID)
	params.Set("includeMovie", "true")
	return params
}

func historyParams(downloadID string) url.Values {
	params := url.Values{}
	params.Set("downloadId", downloadID)
	params.Set("pageSize", "100")
	params.Set("sortKey", "date")
	params.Set("sortDirection", "descending")
	return params
}

func downloadIDsEqual(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
}

func usefulSonarrHistoryEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "grabbed", "downloadFolderImported", "seriesFolderImported":
		return true
	default:
		return false
	}
}

func usefulRadarrHistoryEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "grabbed", "downloadFolderImported", "movieFolderImported":
		return true
	default:
		return false
	}
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
func (c *Client) parseRadarrResponse(body io.Reader) (*ExternalIDsLookupResult, error) {
	var parseResp RadarrParseResponse
	if err := json.NewDecoder(body).Decode(&parseResp); err != nil {
		return nil, fmt.Errorf("failed to decode Radarr parse response: %w", err)
	}

	return parseResp.ExtractLookupResult(), nil
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

// InstanceType returns the ARR instance type this client is configured for
func (c *Client) InstanceType() models.ArrInstanceType {
	return c.instanceType
}

// BaseURL returns the base URL this client is configured for
func (c *Client) BaseURL() string {
	return c.baseURL
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
