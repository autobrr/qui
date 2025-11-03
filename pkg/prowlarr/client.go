// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prowlarr

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	gojackett "github.com/kylesanderson/go-jackett"
)

// Config holds the options for constructing a Client.
type Config struct {
	Host       string
	APIKey     string
	Timeout    int
	HTTPClient *http.Client
}

// Client provides a minimal Prowlarr API wrapper suitable for Torznab-style access.
type Client struct {
	host       string
	apiKey     string
	httpClient *http.Client
}

// NewClient constructs a new Client using the provided configuration.
func NewClient(cfg Config) *Client {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	return &Client{
		host:       strings.TrimRight(cfg.Host, "/"),
		apiKey:     cfg.APIKey,
		httpClient: client,
	}
}

// Indexer represents a configured Prowlarr indexer returned by the API.
type Indexer struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Implementation     string `json:"implementation"`
	ImplementationName string `json:"implementationName"`
	Enable             bool   `json:"enable"`
}

// SearchIndexer performs a Torznab search via the specified Prowlarr indexer ID.
func (c *Client) SearchIndexer(ctx context.Context, indexerID string, params map[string]string) (gojackett.Rss, error) {
	var rss gojackett.Rss

	if strings.TrimSpace(indexerID) == "" {
		return rss, fmt.Errorf("prowlarr indexer ID is required")
	}
	if c.httpClient == nil {
		return rss, fmt.Errorf("prowlarr HTTP client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	query := url.Values{}
	for key, value := range params {
		if strings.TrimSpace(value) == "" {
			continue
		}
		query.Set(key, value)
	}

	if query.Get("t") == "" {
		query.Set("t", "search")
	}
	if c.apiKey != "" {
		query.Set("apikey", c.apiKey)
	}

	endpoint, err := url.JoinPath(c.host, "api", "v1", "indexer", strings.TrimSpace(indexerID), "newznab")
	if err != nil {
		return rss, fmt.Errorf("failed to build prowlarr endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return rss, fmt.Errorf("failed to build prowlarr request: %w", err)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return rss, fmt.Errorf("prowlarr request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return rss, fmt.Errorf("prowlarr returned status %d", resp.StatusCode)
	}

	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return rss, fmt.Errorf("failed to decode prowlarr response: %w", err)
	}

	return rss, nil
}

// GetIndexers retrieves all configured indexers from the Prowlarr instance.
func (c *Client) GetIndexers(ctx context.Context) ([]Indexer, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("prowlarr HTTP client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	endpoint, err := url.JoinPath(c.host, "api", "v1", "indexer")
	if err != nil {
		return nil, fmt.Errorf("failed to build prowlarr endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build prowlarr request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query prowlarr: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, fmt.Errorf("prowlarr endpoint not found (404)")
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("prowlarr returned %d (unauthorized)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("prowlarr unexpected status %d", resp.StatusCode)
	}

	var payload []Indexer
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode prowlarr response: %w", err)
	}

	return payload, nil
}
