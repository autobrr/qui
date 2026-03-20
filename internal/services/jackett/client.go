// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	gojackett "github.com/autobrr/qui/pkg/gojackett"

	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/prowlarr"
	"github.com/autobrr/qui/pkg/redact"
)

const maxTorrentDownloadBytes int64 = 16 << 20 // 16 MiB safety limit for torrent blobs

// DownloadError represents an HTTP error during torrent download.
// It preserves the status code for rate-limit detection and retry logic.
type DownloadError struct {
	StatusCode int
	URL        string
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("torrent download from %s returned status %d", e.URL, e.StatusCode)
}

func (e *DownloadError) Is(target error) bool {
	_, ok := target.(*DownloadError)
	return ok
}

// MagnetDownloadError indicates that the download endpoint redirected to a magnet URL.
// Callers should add the magnet directly to the torrent client.
type MagnetDownloadError struct {
	MagnetURL string
}

func (e *MagnetDownloadError) Error() string {
	return fmt.Sprintf("torrent download redirected to magnet link: %s", e.MagnetURL)
}

// IsRateLimited returns true if this error indicates rate limiting (HTTP 429).
func (e *DownloadError) IsRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests
}

// Client wraps the Torznab backend client implementation
type Client struct {
	backend    models.TorznabBackend
	baseURL    string
	apiKey     string
	basicUser  string
	basicPass  string
	jackett    *gojackett.Client
	prowlarr   *prowlarr.Client
	httpClient *http.Client
	timeout    time.Duration
}

// NewClient creates a new Torznab client for the desired backend
func NewClient(baseURL, apiKey string, basicUsername, basicPassword *string, backend models.TorznabBackend, timeoutSeconds int) *Client {
	if backend == "" {
		backend = models.TorznabBackendJackett
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	c := &Client{
		backend:   backend,
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		basicUser: strings.TrimSpace(stringOrEmpty(basicUsername)),
		basicPass: strings.TrimSpace(stringOrEmpty(basicPassword)),
		timeout:   time.Duration(timeoutSeconds) * time.Second,
	}

	switch backend {
	case models.TorznabBackendProwlarr:
		httpClient := &http.Client{Timeout: c.timeout}
		c.httpClient = httpClient
		c.prowlarr = prowlarr.NewClient(prowlarr.Config{
			Host:       baseURL,
			APIKey:     apiKey,
			BasicUser:  c.basicUser,
			BasicPass:  c.basicPass,
			Timeout:    timeoutSeconds,
			HTTPClient: httpClient,
			UserAgent:  buildinfo.UserAgent,
			Version:    buildinfo.Version,
		})
	case models.TorznabBackendNative:
		c.jackett = gojackett.NewClient(gojackett.Config{
			Host:       baseURL,
			APIKey:     apiKey,
			BasicUser:  c.basicUser,
			BasicPass:  c.basicPass,
			Timeout:    timeoutSeconds,
			DirectMode: true,
		})
	default: // jackett + fallback
		c.jackett = gojackett.NewClient(gojackett.Config{
			Host:      baseURL,
			APIKey:    apiKey,
			BasicUser: c.basicUser,
			BasicPass: c.basicPass,
			Timeout:   timeoutSeconds,
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
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	query := req.URL.Query()
	query.Set("t", "caps")
	if c.apiKey != "" {
		query.Set("apikey", c.apiKey)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jackett caps request failed: %w", redact.URLError(err))
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
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	query := req.URL.Query()
	query.Set("t", "caps")
	if c.apiKey != "" {
		query.Set("apikey", c.apiKey)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr caps request failed: %w", redact.URLError(err))
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
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("native caps request failed: %w", redact.URLError(err))
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

	checkRedirect := c.httpClient.CheckRedirect
	redirectClient := *c.httpClient
	redirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if strings.EqualFold(req.URL.Scheme, "magnet") {
			return http.ErrUseLastResponse
		}
		if checkRedirect != nil {
			return checkRedirect(req, via)
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Accept", "application/x-bittorrent, application/octet-stream")
	req.Header.Set("User-Agent", buildinfo.UserAgent)
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}

	// Ensure API key is present for backends that require it
	if c.apiKey != "" && !strings.Contains(downloadURL, "apikey=") {
		query := req.URL.Query()
		query.Set("apikey", c.apiKey)
		req.URL.RawQuery = query.Encode()
	}

	resp, err := redirectClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrent download failed: %w", redact.URLError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		location := strings.TrimSpace(resp.Header.Get("Location"))
		if strings.HasPrefix(strings.ToLower(location), "magnet:") {
			return nil, &MagnetDownloadError{MagnetURL: location}
		}
		return nil, &DownloadError{StatusCode: resp.StatusCode, URL: redact.URLString(downloadURL)}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &DownloadError{StatusCode: resp.StatusCode, URL: redact.URLString(downloadURL)}
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

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// convertRssToResults converts go-jackett RSS response to our Result format
func (c *Client) convertRssToResults(rss gojackett.Rss) []Result {
	results := make([]Result, 0, len(rss.Channel.Item))
	for _, item := range rss.Channel.Item {
		result := Result{
			Tracker:              rss.Channel.Title,
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

		// Set first category if available
		if len(item.Category) > 0 {
			result.Category = item.Category[0]
		}

		// Parse torznab attributes into a lookup map
		attrMap := make(map[string]string, len(item.Attr))
		for _, attr := range item.Attr {
			name := strings.ToLower(strings.TrimSpace(attr.Name))
			if name == "" {
				continue
			}
			attrMap[name] = attr.Value
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
				result.Imdb = attr.Value
			}
		}
		result.Attributes = attrMap

		results = append(results, result)
	}

	return results
}

// JackettIndexer represents an indexer from Jackett's indexer list
type JackettIndexer struct {
	ID          string                          `json:"id"`
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	Type        string                          `json:"type"`
	Configured  bool                            `json:"configured"`
	Backend     models.TorznabBackend           `json:"backend"`
	Caps        []string                        `json:"caps,omitempty"`
	Categories  []models.TorznabIndexerCategory `json:"categories,omitempty"`
}

// DiscoveryResult contains discovered indexers and any warnings from partial failures
type DiscoveryResult struct {
	Indexers []JackettIndexer `json:"indexers"`
	Warnings []string         `json:"warnings,omitempty"`
}

// DiscoverJackettIndexers discovers all configured indexers from a Jackett instance.
// The context is used to cancel in-flight capability fetches if the request is cancelled.
// Returns a DiscoveryResult containing indexers and any warnings about partial failures.
func DiscoverJackettIndexers(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string) (DiscoveryResult, error) {
	if ctx == nil {
		log.Warn().Msg("DiscoverJackettIndexers called with nil context - this is a programming error")
		ctx = context.Background()
	}
	if baseURL = strings.TrimSpace(baseURL); baseURL == "" {
		return DiscoveryResult{Indexers: []JackettIndexer{}}, fmt.Errorf("base url is required")
	}

	jackettIndexers, failedIDs, jackettErr := discoverJackettIndexers(ctx, baseURL, apiKey, basicUsername, basicPassword)
	if jackettErr == nil {
		var warnings []string
		if len(failedIDs) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d indexer(s) failed capability fetch - sync manually later", len(failedIDs)))
		}
		return DiscoveryResult{Indexers: jackettIndexers, Warnings: warnings}, nil
	}

	prowlarrClient := prowlarr.NewClient(prowlarr.Config{
		Host:      baseURL,
		APIKey:    apiKey,
		BasicUser: strings.TrimSpace(stringOrEmpty(basicUsername)),
		BasicPass: strings.TrimSpace(stringOrEmpty(basicPassword)),
		Timeout:   15,
		UserAgent: buildinfo.UserAgent,
		Version:   buildinfo.Version,
	})

	pIndexers, prowlarrErr := prowlarrClient.GetIndexers(ctx)
	if prowlarrErr == nil {
		// First pass: build indexer list and collect IDs for parallel caps fetch
		indexers := make([]JackettIndexer, 0, len(pIndexers))
		indexerIDs := make([]string, 0, len(pIndexers))

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

			indexers = append(indexers, indexer)
			indexerIDs = append(indexerIDs, indexer.ID)
		}

		// Fetch capabilities and categories for all indexers in parallel with retries
		capsMap, failedIDs := fetchCapsParallel(ctx, baseURL, apiKey, basicUsername, basicPassword, models.TorznabBackendProwlarr, indexerIDs)

		// Apply capabilities and categories to indexers
		for i := range indexers {
			if caps, ok := capsMap[indexers[i].ID]; ok {
				indexers[i].Caps = caps.Capabilities
				indexers[i].Categories = caps.Categories
			}
		}

		var warnings []string
		if len(failedIDs) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d indexer(s) failed capability fetch - sync manually later", len(failedIDs)))
		}
		return DiscoveryResult{Indexers: indexers, Warnings: warnings}, nil
	}

	return DiscoveryResult{Indexers: []JackettIndexer{}}, fmt.Errorf("jackett discovery failed: %v; prowlarr discovery failed: %w", jackettErr, prowlarrErr)
}

func discoverJackettIndexers(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string) ([]JackettIndexer, []string, error) {
	// Use the go-jackett library
	client := gojackett.NewClient(gojackett.Config{
		Host:      baseURL,
		APIKey:    apiKey,
		BasicUser: strings.TrimSpace(stringOrEmpty(basicUsername)),
		BasicPass: strings.TrimSpace(stringOrEmpty(basicPassword)),
	})

	// Get all configured indexers
	indexersResp, err := client.GetIndexersCtx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get indexers: %w", err)
	}

	// First pass: build indexer list and collect IDs for parallel caps fetch
	indexers := make([]JackettIndexer, 0, len(indexersResp.Indexer))
	configuredIDs := make([]string, 0, len(indexersResp.Indexer))

	for _, idx := range indexersResp.Indexer {
		indexer := JackettIndexer{
			ID:          idx.ID,
			Name:        idx.Title,
			Description: idx.Description,
			Type:        idx.Type,
			Configured:  idx.Configured == "true",
			Backend:     models.TorznabBackendJackett,
		}

		indexers = append(indexers, indexer)

		// Only fetch caps for configured indexers
		if indexer.Configured {
			configuredIDs = append(configuredIDs, idx.ID)
		}
	}

	// Fetch capabilities and categories for all configured indexers in parallel with retries
	capsMap, failedIDs := fetchCapsParallel(ctx, baseURL, apiKey, basicUsername, basicPassword, models.TorznabBackendJackett, configuredIDs)

	// Apply capabilities and categories to indexers
	for i := range indexers {
		if caps, ok := capsMap[indexers[i].ID]; ok {
			indexers[i].Caps = caps.Capabilities
			indexers[i].Categories = caps.Categories
		}
	}

	return indexers, failedIDs, nil
}

// capsFetchResult holds the result of a parallel caps fetch
type capsFetchResult struct {
	indexerID string
	caps      *torznabCaps
	err       error
}

// fetchCapsParallel fetches capabilities and categories for multiple indexers concurrently with retries.
// Returns a map of indexerID -> torznabCaps and a slice of failed indexer IDs.
// Failed fetches are logged but don't fail the overall operation.
// The parent context is used to cancel all in-flight requests if the caller's context is cancelled.
func fetchCapsParallel(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string, backend models.TorznabBackend, indexerIDs []string) (map[string]*torznabCaps, []string) {
	if len(indexerIDs) == 0 {
		return nil, nil
	}

	const (
		maxConcurrent = 10 // Max parallel requests to avoid overwhelming the server
		maxRetries    = 2  // Number of retries per indexer
		retryDelay    = 500 * time.Millisecond
		fetchTimeout  = 15 * time.Second
	)

	results := make(map[string]*torznabCaps)
	resultsChan := make(chan capsFetchResult, len(indexerIDs))

	// Semaphore to limit concurrency
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup
	for _, id := range indexerIDs {
		wg.Add(1)
		go func(indexerID string) {
			defer wg.Done()

			// Check if parent context is already cancelled
			select {
			case <-ctx.Done():
				resultsChan <- capsFetchResult{indexerID: indexerID, err: ctx.Err()}
				return
			default:
			}

			// Acquire semaphore with cancellation support
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				resultsChan <- capsFetchResult{indexerID: indexerID, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			caps, err := fetchCapsWithRetry(ctx, baseURL, apiKey, basicUsername, basicPassword, backend, indexerID, maxRetries, retryDelay, fetchTimeout)
			resultsChan <- capsFetchResult{
				indexerID: indexerID,
				caps:      caps,
				err:       err,
			}
		}(id)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var failedIDs []string
	for result := range resultsChan {
		if result.err != nil {
			failedIDs = append(failedIDs, result.indexerID)
			log.Warn().
				Err(result.err).
				Str("indexer_id", result.indexerID).
				Str("backend", string(backend)).
				Msg("Failed to fetch capabilities for indexer during discovery")
			continue
		}
		if result.caps != nil {
			results[result.indexerID] = result.caps
		}
	}

	if len(failedIDs) > 0 {
		log.Warn().
			Int("failed", len(failedIDs)).
			Int("total", len(indexerIDs)).
			Int("succeeded", len(results)).
			Msg("Some indexers failed capability fetch during discovery - they can be synced manually later")
	}

	return results, failedIDs
}

// fetchCapsWithRetry attempts to fetch capabilities with retries and exponential backoff.
// The parent context is used as the base for per-attempt timeouts, allowing cancellation.
func fetchCapsWithRetry(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string, backend models.TorznabBackend, indexerID string, maxRetries int, retryDelay, timeout time.Duration) (*torznabCaps, error) {
	client := NewClient(baseURL, apiKey, basicUsername, basicPassword, backend, int(timeout.Seconds()))

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s...
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay * time.Duration(1<<(attempt-1))):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		caps, err := client.FetchCaps(attemptCtx, indexerID)
		cancel() // Cancel immediately after use to avoid context leak in loop

		if err == nil && caps != nil {
			return caps, nil
		}
		lastErr = err

		// Check if parent context was cancelled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("caps response was empty after %d attempts", maxRetries+1)
	} else {
		lastErr = fmt.Errorf("caps fetch failed after %d attempts: %w", maxRetries+1, lastErr)
	}
	return nil, lastErr
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
