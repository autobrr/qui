// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gojackett "github.com/autobrr/qui/pkg/gojackett"

	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/prowlarr"
	"github.com/autobrr/qui/pkg/stringutils"
)

const maxTorrentDownloadBytes int64 = 16 << 20 // 16 MiB safety limit for torrent blobs

// Client wraps the Torznab backend client implementation
type Client struct {
	backend    models.TorznabBackend
	baseURL    string
	apiKey     string
	jackett    *gojackett.Client
	prowlarr   *prowlarr.Client
	httpClient *http.Client
	timeout    time.Duration
}

// NewClient creates a new Torznab client for the desired backend
func NewClient(baseURL, apiKey string, backend models.TorznabBackend, timeoutSeconds int) *Client {
	if backend == "" {
		backend = models.TorznabBackendJackett
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	c := &Client{
		backend: backend,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}

	switch backend {
	case models.TorznabBackendProwlarr:
		httpClient := &http.Client{Timeout: c.timeout}
		c.httpClient = httpClient
		c.prowlarr = prowlarr.NewClient(prowlarr.Config{
			Host:       baseURL,
			APIKey:     apiKey,
			Timeout:    timeoutSeconds,
			HTTPClient: httpClient,
			UserAgent:  buildinfo.UserAgent,
			Version:    buildinfo.Version,
		})
	case models.TorznabBackendNative:
		c.jackett = gojackett.NewClient(gojackett.Config{
			Host:       baseURL,
			APIKey:     apiKey,
			Timeout:    timeoutSeconds,
			DirectMode: true,
		})
	default: // jackett + fallback
		c.jackett = gojackett.NewClient(gojackett.Config{
			Host:    baseURL,
			APIKey:  apiKey,
			Timeout: timeoutSeconds,
		})
	}

	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout}
	}

	return c
}

// Result represents a single search result (simplified format)
type Result struct {
	Tracker              string
	IndexerID            int
	Title                string
	Link                 string
	Details              string
	GUID                 string
	PublishDate          time.Time
	Category             string
	Size                 int64
	Seeders              int
	Peers                int
	DownloadVolumeFactor float64
	UploadVolumeFactor   float64
	Imdb                 string
	// Attributes stores every Torznab attribute with lowercase keys from RSS item attr entries (see convertRssToResults normalization).
	Attributes map[string]string
}

// SearchAll searches across all indexers when supported by the backend
func (c *Client) SearchAll(ctx context.Context, params map[string]string) ([]Result, error) {
	switch c.backend {
	case models.TorznabBackendJackett:
		return c.Search(ctx, "all", params)
	case models.TorznabBackendNative:
		return c.SearchDirect(ctx, params)
	default:
		return nil, fmt.Errorf("search all not supported for backend %s", c.backend)
	}
}

// SearchDirect searches a direct Torznab endpoint (not through Jackett/Prowlarr aggregator)
// Uses the native SearchDirectCtx method from go-jackett library
func (c *Client) SearchDirect(ctx context.Context, params map[string]string) ([]Result, error) {
	if c.jackett == nil {
		return nil, fmt.Errorf("direct search not supported for backend %s", c.backend)
	}
	query := params["q"]

	if ctx == nil {
		ctx = context.Background()
	}

	rss, err := c.jackett.SearchDirectCtx(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("direct search failed: %w", err)
	}

	return c.convertRssToResults(rss), nil
}

// Search performs a search on a specific indexer or "all"
func (c *Client) Search(ctx context.Context, indexer string, params map[string]string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	switch c.backend {
	case models.TorznabBackendProwlarr:
		return c.searchProwlarr(ctx, indexer, params)
	case models.TorznabBackendNative:
		return c.SearchDirect(ctx, params)
	default:
		if c.jackett == nil {
			return nil, fmt.Errorf("jackett client not configured for backend %s", c.backend)
		}
		rss, err := c.jackett.GetTorrentsCtx(ctx, indexer, params)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}
		return c.convertRssToResults(rss), nil
	}
}

func (c *Client) searchProwlarr(ctx context.Context, indexerID string, params map[string]string) ([]Result, error) {
	if c.prowlarr == nil {
		return nil, fmt.Errorf("prowlarr client not configured")
	}

	rss, err := c.prowlarr.SearchIndexer(ctx, indexerID, params)
	if err != nil {
		return nil, fmt.Errorf("prowlarr search failed: %w", err)
	}

	return c.convertRssToResults(rss), nil
}

// FetchCaps retrieves the Torznab caps document for the configured backend/indexer.
func (c *Client) FetchCaps(ctx context.Context, indexerID string) (*torznabCaps, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	switch c.backend {
	case models.TorznabBackendJackett:
		return c.fetchCapsFromJackett(ctx, indexerID)
	case models.TorznabBackendProwlarr:
		return c.fetchCapsFromProwlarr(ctx, indexerID)
	case models.TorznabBackendNative:
		return c.fetchCapsFromNative(ctx)
	default:
		return nil, fmt.Errorf("caps not supported for backend %s", c.backend)
	}
}

func (c *Client) fetchCapsFromJackett(ctx context.Context, indexerID string) (*torznabCaps, error) {
	baseRoot := strings.TrimRight(c.baseURL, "/")
	trimmedID := strings.Trim(strings.TrimSpace(indexerID), "/")

	const jackettIndexerPrefix = "/api/v2.0/indexers/"
	if strings.Contains(baseRoot, jackettIndexerPrefix) {
		parts := strings.SplitN(baseRoot, jackettIndexerPrefix, 2)
		baseRoot = strings.TrimRight(parts[0], "/")
		if trimmedID == "" && len(parts) == 2 {
			remainder := parts[1]
			if idx := strings.Index(remainder, "/"); idx != -1 {
				remainder = remainder[:idx]
			}
			trimmedID = strings.Trim(remainder, "/")
		}
	}

	if trimmedID == "" {
		return nil, fmt.Errorf("jackett indexer identifier is required for caps fetch")
	}

	endpoint, err := url.JoinPath(baseRoot, "api", "v2.0", "indexers", trimmedID, "results", "torznab", "api")
	if err != nil {
		return nil, fmt.Errorf("build jackett caps url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build jackett caps request: %w", err)
	}
	query := req.URL.Query()
	query.Set("t", "caps")
	if c.apiKey != "" {
		query.Set("apikey", c.apiKey)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jackett caps request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("jackett caps returned status %d", resp.StatusCode)
	}

	return parseTorznabCaps(resp.Body)
}

func (c *Client) fetchCapsFromProwlarr(ctx context.Context, indexerID string) (*torznabCaps, error) {
	trimmed := strings.TrimSpace(indexerID)
	if trimmed == "" {
		return nil, fmt.Errorf("prowlarr indexer identifier is required for caps fetch")
	}

	endpoint, err := url.JoinPath(strings.TrimRight(c.baseURL, "/"), "api", "v1", "indexer", trimmed, "newznab")
	if err != nil {
		return nil, fmt.Errorf("build prowlarr caps url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build prowlarr caps request: %w", err)
	}
	query := req.URL.Query()
	query.Set("t", "caps")
	if c.apiKey != "" {
		query.Set("apikey", c.apiKey)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr caps request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("prowlarr caps returned status %d", resp.StatusCode)
	}

	return parseTorznabCaps(resp.Body)
}

func (c *Client) fetchCapsFromNative(ctx context.Context) (*torznabCaps, error) {
	endpoint := strings.TrimRight(c.baseURL, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("native torznab endpoint not configured")
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse native torznab endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("t", "caps")
	if c.apiKey != "" {
		query.Set("apikey", c.apiKey)
	}
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build native caps request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("native caps request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("native caps returned status %d", resp.StatusCode)
	}

	return parseTorznabCaps(resp.Body)
}

// Download retrieves the raw torrent bytes for the provided download URL.
func (c *Client) Download(ctx context.Context, downloadURL string) ([]byte, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return nil, fmt.Errorf("download URL is required")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Normalise relative URLs
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		downloadURL = strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(downloadURL, "/")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Accept", "application/x-bittorrent, application/octet-stream")
	req.Header.Set("User-Agent", buildinfo.UserAgent)

	// Ensure API key is present for backends that require it
	if c.apiKey != "" && !strings.Contains(downloadURL, "apikey=") {
		query := req.URL.Query()
		query.Set("apikey", c.apiKey)
		req.URL.RawQuery = query.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrent download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("torrent download returned status %d", resp.StatusCode)
	}

	limitedReader := io.LimitReader(resp.Body, maxTorrentDownloadBytes+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read torrent body: %w", err)
	}
	if int64(len(data)) > maxTorrentDownloadBytes {
		return nil, fmt.Errorf("torrent download exceeded %d bytes limit", maxTorrentDownloadBytes)
	}

	return data, nil
}

// convertRssToResults converts go-jackett RSS response to our Result format
func (c *Client) convertRssToResults(rss gojackett.Rss) []Result {
	results := make([]Result, 0, len(rss.Channel.Item))
	// Intern the tracker name once since it's the same for all results
	tracker := stringutils.Intern(rss.Channel.Title)
	for _, item := range rss.Channel.Item {
		result := Result{
			Tracker:              tracker,
			Title:                item.Title,
			Link:                 item.Enclosure.URL,
			Details:              item.Comments,
			GUID:                 item.Guid,
			Category:             "", // Categories are in item.Category array
			Size:                 0,
			DownloadVolumeFactor: 1.0,
			UploadVolumeFactor:   1.0,
		}

		// Parse size
		if size, err := strconv.ParseInt(item.Size, 10, 64); err == nil {
			result.Size = size
		}

		// Parse pub date
		if item.PubDate != "" {
			if t, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
				result.PublishDate = t
			} else if t, err := time.Parse(time.RFC1123, item.PubDate); err == nil {
				result.PublishDate = t
			}
		}

		// Set first category if available (intern since categories repeat)
		if len(item.Category) > 0 {
			result.Category = stringutils.Intern(item.Category[0])
		}

		// Parse torznab attributes into a lookup map with interned keys/values
		attrMap := make(map[string]string, len(item.Attr))
		for _, attr := range item.Attr {
			name := stringutils.InternNormalized(attr.Name)
			if name == "" {
				continue
			}
			// Intern values since many are repeated (category names, factors, etc.)
			attrMap[name] = stringutils.Intern(attr.Value)
			switch name {
			case "seeders":
				if v, err := strconv.Atoi(attr.Value); err == nil {
					result.Seeders = v
				}
			case "peers":
				if v, err := strconv.Atoi(attr.Value); err == nil {
					result.Peers = v
				}
			case "downloadvolumefactor":
				if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
					result.DownloadVolumeFactor = v
				}
			case "uploadvolumefactor":
				if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
					result.UploadVolumeFactor = v
				}
			case "imdb":
				result.Imdb = stringutils.Intern(attr.Value)
			}
		}
		result.Attributes = attrMap

		results = append(results, result)
	}

	return results
}

// JackettIndexer represents an indexer from Jackett's indexer list
type JackettIndexer struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Type        string                `json:"type"`
	Configured  bool                  `json:"configured"`
	Backend     models.TorznabBackend `json:"backend"`
	Caps        []string              `json:"caps,omitempty"`
}

// DiscoverJackettIndexers discovers all configured indexers from a Jackett instance
func DiscoverJackettIndexers(baseURL, apiKey string) ([]JackettIndexer, error) {
	if baseURL = strings.TrimSpace(baseURL); baseURL == "" {
		return nil, fmt.Errorf("base url is required")
	}

	jackettIndexers, jackettErr := discoverJackettIndexers(baseURL, apiKey)
	if jackettErr == nil {
		return jackettIndexers, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	prowlarrClient := prowlarr.NewClient(prowlarr.Config{
		Host:      baseURL,
		APIKey:    apiKey,
		Timeout:   15,
		UserAgent: buildinfo.UserAgent,
		Version:   buildinfo.Version,
	})

	pIndexers, prowlarrErr := prowlarrClient.GetIndexers(ctx)
	if prowlarrErr == nil {
		indexers := make([]JackettIndexer, 0, len(pIndexers))
		for _, idx := range pIndexers {
			// Skip disabled indexers
			if !idx.Enable {
				continue
			}
			// Skip non-torrent indexers (Usenet/Newznab)
			if idx.Protocol != "torrent" {
				continue
			}

			description := idx.Description
			if description == "" {
				description = idx.ImplementationName
			}

			backendType := idx.Implementation
			if backendType == "" {
				backendType = idx.ImplementationName
			}

			indexer := JackettIndexer{
				ID:          strconv.Itoa(idx.ID),
				Name:        idx.Name,
				Description: description,
				Type:        backendType,
				Configured:  true, // Always true since we skip disabled indexers above
				Backend:     models.TorznabBackendProwlarr,
			}

			// Try to fetch capabilities
			if caps := fetchCapabilitiesForDiscovery(baseURL, apiKey, models.TorznabBackendProwlarr, strconv.Itoa(idx.ID)); caps != nil {
				indexer.Caps = caps
			}

			indexers = append(indexers, indexer)
		}
		return indexers, nil
	}

	return nil, fmt.Errorf("jackett discovery failed: %v; prowlarr discovery failed: %w", jackettErr, prowlarrErr)
}

func discoverJackettIndexers(baseURL, apiKey string) ([]JackettIndexer, error) {
	// Use the go-jackett library
	client := gojackett.NewClient(gojackett.Config{
		Host:   baseURL,
		APIKey: apiKey,
	})

	// Get all configured indexers
	indexersResp, err := client.GetIndexersCtx(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get indexers: %w", err)
	}

	// Convert to our JackettIndexer format and optionally fetch capabilities
	indexers := make([]JackettIndexer, 0, len(indexersResp.Indexer))
	for _, idx := range indexersResp.Indexer {
		indexer := JackettIndexer{
			ID:          idx.ID,
			Name:        idx.Title,
			Description: idx.Description,
			Type:        idx.Type,
			Configured:  idx.Configured == "true",
			Backend:     models.TorznabBackendJackett,
		}

		// Try to fetch capabilities for configured indexers
		if indexer.Configured {
			if caps := fetchCapabilitiesForDiscovery(baseURL, apiKey, models.TorznabBackendJackett, idx.ID); caps != nil {
				indexer.Caps = caps
			}
		}

		indexers = append(indexers, indexer)
	}

	return indexers, nil
}

// fetchCapabilitiesForDiscovery tries to fetch capabilities for an indexer during discovery
// Returns nil if capabilities cannot be fetched (to avoid failing the entire discovery process)
func fetchCapabilitiesForDiscovery(baseURL, apiKey string, backend models.TorznabBackend, indexerID string) []string {
	// Create a client with a short timeout for discovery
	client := NewClient(baseURL, apiKey, backend, 10) // 10 second timeout

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	caps, err := client.FetchCaps(ctx, indexerID)
	if err != nil {
		// Don't fail discovery if we can't fetch caps - just log a debug message
		// This could happen if the indexer is down, misconfigured, etc.
		return nil
	}

	if caps != nil {
		return caps.Capabilities
	}

	return nil
}

// GetCapabilitiesDirect gets capabilities from a direct Torznab endpoint
func (c *Client) GetCapabilitiesDirect() (*gojackett.Indexers, error) {
	if c.jackett == nil {
		return nil, fmt.Errorf("capabilities not supported for backend %s", c.backend)
	}
	indexers, err := c.jackett.GetCapsDirectCtx(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get capabilities: %w", err)
	}
	return &indexers, nil
}
