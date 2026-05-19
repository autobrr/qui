// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prowlarr

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	gojackett "github.com/autobrr/qui/pkg/gojackett"
	"github.com/autobrr/qui/pkg/redact"
)

// Config holds the options for constructing a Client.
type Config struct {
	Host       string
	APIKey     string
	BasicUser  string
	BasicPass  string
	Timeout    int
	HTTPClient *http.Client
	UserAgent  string
	Version    string
}

// TorznabError represents a Torznab error response
type TorznabError struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

// Client provides a minimal Prowlarr API wrapper suitable for Torznab-style access.
type Client struct {
	host       string
	apiKey     string
	basicUser  string
	basicPass  string
	httpClient *http.Client
	userAgent  string
	version    string
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

	ua := strings.TrimSpace(cfg.UserAgent)
	if ua == "" {
		ua = "qui"
	}
	version := strings.TrimSpace(cfg.Version)
	if version != "" && !strings.Contains(ua, version) {
		ua = fmt.Sprintf("%s/%s", ua, version)
	}

	return &Client{
		host:       strings.TrimRight(cfg.Host, "/"),
		apiKey:     cfg.APIKey,
		basicUser:  strings.TrimSpace(cfg.BasicUser),
		basicPass:  strings.TrimSpace(cfg.BasicPass),
		httpClient: client,
		userAgent:  ua,
		version:    version,
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
	Protocol           string `json:"protocol"` // "unknown", "usenet", "torrent"
}

// IndexerDetail represents detailed information about a Prowlarr indexer
type IndexerDetail struct {
	ID                 int            `json:"id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Implementation     string         `json:"implementation"`
	ImplementationName string         `json:"implementationName"`
	Enable             bool           `json:"enable"`
	Fields             []IndexerField `json:"fields"`
}

// IndexerField represents a configuration field for an indexer
type IndexerField struct {
	Order    int    `json:"order"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	Value    any    `json:"value"`
	Type     string `json:"type"`
	Advanced bool   `json:"advanced"`
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
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return rss, fmt.Errorf("prowlarr request failed: %w", redact.URLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return rss, fmt.Errorf("prowlarr returned status %d", resp.StatusCode)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return rss, fmt.Errorf("failed to read prowlarr response: %w", err)
	}

	// Check if the response is an error
	bodyStr := strings.TrimSpace(string(body))
	if strings.HasPrefix(bodyStr, "<error") {
		var torznabErr TorznabError
		if err := xml.Unmarshal(body, &torznabErr); err != nil {
			return rss, fmt.Errorf("failed to decode torznab error response: %w", err)
		}
		return rss, fmt.Errorf("torznab error %s: %s", torznabErr.Code, torznabErr.Message)
	}

	// Decode the RSS response
	if err := xml.Unmarshal(body, &rss); err != nil {
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
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	req.Header.Set("User-Agent", c.userAgent)

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

// GetIndexer retrieves detailed information about a specific indexer from Prowlarr
func (c *Client) GetIndexer(ctx context.Context, indexerID int) (*IndexerDetail, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("prowlarr HTTP client is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	endpoint, err := url.JoinPath(c.host, "api", "v1", "indexer", fmt.Sprintf("%d", indexerID))
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
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query prowlarr: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, fmt.Errorf("prowlarr indexer %d not found (404)", indexerID)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("prowlarr returned %d (unauthorized)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("prowlarr unexpected status %d", resp.StatusCode)
	}

	var payload IndexerDetail
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode prowlarr response: %w", err)
	}

	return &payload, nil
}

// GetTrackerDomains extracts actual tracker domains from Prowlarr indexers
func (c *Client) GetTrackerDomains(ctx context.Context) ([]string, error) {
	indexers, err := c.GetIndexers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get indexers: %w", err)
	}

	var domains []string
	domainMap := make(map[string]bool)

	for _, indexer := range indexers {
		if !indexer.Enable {
			continue
		}

		// Get detailed indexer information to extract the tracker URL
		detail, err := c.GetIndexer(ctx, indexer.ID)
		if err != nil {
			continue // Skip this indexer if we can't get details
		}

		// Extract tracker domain from indexer fields
		domain := ExtractDomainFromIndexerFields(detail.Fields)
		if domain != "" && !domainMap[domain] {
			domainMap[domain] = true
			domains = append(domains, domain)
		}
	}

	return domains, nil
}

// ExtractDomainFromIndexerFields extracts the tracker domain from Prowlarr indexer configuration fields
func ExtractDomainFromIndexerFields(fields []IndexerField) string {
	// Look for common field names that contain the tracker URL
	for _, field := range fields {
		if field.Value == nil {
			continue
		}

		// Convert value to string
		valueStr := fmt.Sprintf("%v", field.Value)
		if valueStr == "" {
			continue
		}

		// Check for common field names that contain URLs
		fieldName := strings.ToLower(field.Name)
		if fieldName == "baseurl" || fieldName == "base_url" || fieldName == "url" || fieldName == "siteurl" || fieldName == "site_url" {
			if domain := extractDomainFromURL(valueStr); domain != "" {
				return domain
			}
		}

		// Also check if the value looks like a URL
		if strings.HasPrefix(valueStr, "http://") || strings.HasPrefix(valueStr, "https://") {
			if domain := extractDomainFromURL(valueStr); domain != "" {
				return domain
			}
		}
	}

	return ""
}

// extractDomainFromURL extracts the domain from a URL string (copied from jackett service)
func extractDomainFromURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	// Parse the URL
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	// Extract hostname
	hostname := u.Hostname()
	if hostname == "" {
		return ""
	}

	// Remove common subdomains
	parts := strings.Split(hostname, ".")
	if len(parts) >= 3 {
		// Remove www, api, etc.
		if parts[0] == "www" || parts[0] == "api" || parts[0] == "tracker" {
			hostname = strings.Join(parts[1:], ".")
		}
	}

	return hostname
}
