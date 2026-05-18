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
		return c.parseSonarrResponse(resp.Body)
	case models.ArrInstanceTypeRadarr:
		return c.parseRadarrResponse(resp.Body)
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", c.instanceType)
	}
}

// ParseSonarrTitle returns the full Sonarr parse response for TV lookups that need the series ID.
func (c *Client) ParseSonarrTitle(ctx context.Context, title string) (*SonarrParseResponse, error) {
	if c.instanceType != models.ArrInstanceTypeSonarr {
		return nil, fmt.Errorf("unsupported instance type for Sonarr parse: %s", c.instanceType)
	}

	endpoint := c.baseURL + "/api/v3/parse"
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

	var parseResp SonarrParseResponse
	if err := json.NewDecoder(resp.Body).Decode(&parseResp); err != nil {
		return nil, fmt.Errorf("failed to decode Sonarr parse response: %w", err)
	}

	return &parseResp, nil
}

// GetSonarrSeasonEpisodes fetches episodes for a specific Sonarr series season.
func (c *Client) GetSonarrSeasonEpisodes(ctx context.Context, seriesID, seasonNumber int) ([]SonarrEpisodeResource, error) {
	if c.instanceType != models.ArrInstanceTypeSonarr {
		return nil, fmt.Errorf("unsupported instance type for Sonarr episodes: %s", c.instanceType)
	}

	endpoint := c.baseURL + "/api/v3/episode"
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint URL: %w", err)
	}
	q := u.Query()
	q.Set("seriesId", strconv.Itoa(seriesID))
	q.Set("seasonNumber", strconv.Itoa(seasonNumber))
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

	var episodes []SonarrEpisodeResource
	if err := json.NewDecoder(resp.Body).Decode(&episodes); err != nil {
		return nil, fmt.Errorf("failed to decode Sonarr episode response: %w", err)
	}

	return episodes, nil
}

// parseSonarrResponse parses a Sonarr parse response and extracts external IDs
func (c *Client) parseSonarrResponse(body io.Reader) (*models.ExternalIDs, error) {
	var parseResp SonarrParseResponse
	if err := json.NewDecoder(body).Decode(&parseResp); err != nil {
		return nil, fmt.Errorf("failed to decode Sonarr parse response: %w", err)
	}

	return parseResp.ExtractExternalIDs(), nil
}

// parseRadarrResponse parses a Radarr parse response and extracts external IDs
func (c *Client) parseRadarrResponse(body io.Reader) (*models.ExternalIDs, error) {
	var parseResp RadarrParseResponse
	if err := json.NewDecoder(body).Decode(&parseResp); err != nil {
		return nil, fmt.Errorf("failed to decode Radarr parse response: %w", err)
	}

	return parseResp.ExtractExternalIDs(), nil
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
