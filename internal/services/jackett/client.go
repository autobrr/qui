// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog/log"

	gojackett "github.com/autobrr/qui/pkg/gojackett"

	"github.com/autobrr/qui/internal/buildinfo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/prowlarr"
	"github.com/autobrr/qui/pkg/redact"
)

const maxTorrentDownloadBytes int64 = 16 << 20

var ErrInvalidTorrentPayload = errors.New("torrent download returned a web page or other non-torrent response")

type DownloadError struct {
	StatusCode int
	URL        string
	RetryAfter string
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("torrent download from %s returned status %d", e.URL, e.StatusCode)
}

func (e *DownloadError) HTTPStatusCode() int {
	return e.StatusCode
}

func (e *DownloadError) RetryAfterHeader() string {
	return e.RetryAfter
}

type responseError struct {
	Operation  string
	StatusCode int
	RetryAfter string
}

func (e *responseError) Error() string {
	return fmt.Sprintf("%s returned status %d", e.Operation, e.StatusCode)
}

func (e *responseError) HTTPStatusCode() int {
	return e.StatusCode
}

func (e *responseError) RetryAfterHeader() string {
	return e.RetryAfter
}

func newHTTPResponseError(operation string, resp *http.Response) error {
	return &responseError{
		Operation:  operation,
		StatusCode: resp.StatusCode,
		RetryAfter: resp.Header.Get("Retry-After"),
	}
}

type MagnetDownloadError struct {
	MagnetURL string
}

func (e *MagnetDownloadError) Error() string {
	return "torrent download redirected to magnet link: " + redact.URLString(e.MagnetURL)
}

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

const defaultIndexerTimeoutSeconds = 30

func NewClient(baseURL, apiKey string, basicUsername, basicPassword *string, backend models.TorznabBackend, timeoutSeconds int) *Client {
	if backend == "" {
		backend = models.TorznabBackendJackett
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultIndexerTimeoutSeconds
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
	default:
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
	SearchIMDbID         string
	SearchTVDbID         string
	SearchTMDbID         int
	Attributes           map[string]string
}

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
		return nil, errors.New("prowlarr client not configured")
	}

	rss, err := c.prowlarr.SearchIndexer(ctx, indexerID, params)
	if err != nil {
		return nil, fmt.Errorf("prowlarr search failed: %w", err)
	}

	return c.convertRssToResults(rss), nil
}

func (c *Client) FetchCaps(ctx context.Context, indexerID string) (*TorznabCaps, error) {
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

func (c *Client) fetchCapsFromJackett(ctx context.Context, indexerID string) (*TorznabCaps, error) {
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
		return nil, errors.New("jackett indexer identifier is required for caps fetch")
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
		return nil, newHTTPResponseError("jackett caps", resp)
	}

	return parseTorznabCapsResponse(resp.Body, resp.Header.Get("Retry-After"))
}

func (c *Client) fetchCapsFromProwlarr(ctx context.Context, indexerID string) (*TorznabCaps, error) {
	trimmed := strings.TrimSpace(indexerID)
	if trimmed == "" {
		return nil, errors.New("prowlarr indexer identifier is required for caps fetch")
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
		return nil, newHTTPResponseError("prowlarr caps", resp)
	}

	return parseTorznabCapsResponse(resp.Body, resp.Header.Get("Retry-After"))
}

func (c *Client) fetchCapsFromNative(ctx context.Context) (*TorznabCaps, error) {
	endpoint := strings.TrimRight(c.baseURL, "/")
	if endpoint == "" {
		return nil, errors.New("native torznab endpoint not configured")
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
		return nil, newHTTPResponseError("native caps", resp)
	}

	return parseTorznabCapsResponse(resp.Body, resp.Header.Get("Retry-After"))
}

func (c *Client) Download(ctx context.Context, downloadURL string) ([]byte, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return nil, errors.New("download URL is required")
	}

	if ctx == nil {
		ctx = context.Background()
	}

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
		return nil, errors.New("build download request: the download link is not a valid URL")
	}
	req.Header.Set("Accept", "application/x-bittorrent, application/octet-stream")
	req.Header.Set("User-Agent", buildinfo.UserAgent)
	if c.basicUser != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}

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
		return nil, &DownloadError{StatusCode: resp.StatusCode, URL: redact.URLString(downloadURL), RetryAfter: resp.Header.Get("Retry-After")}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &DownloadError{StatusCode: resp.StatusCode, URL: redact.URLString(downloadURL), RetryAfter: resp.Header.Get("Retry-After")}
	}

	limitedReader := io.LimitReader(resp.Body, maxTorrentDownloadBytes+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read torrent body: %w", err)
	}
	if int64(len(data)) > maxTorrentDownloadBytes {
		return nil, fmt.Errorf("torrent download exceeded %d bytes limit", maxTorrentDownloadBytes)
	}
	mi, parseErr := metainfo.Load(bytes.NewReader(data))
	if parseErr == nil {
		_, parseErr = mi.UnmarshalInfo()
	}
	if parseErr != nil {
		return nil, fmt.Errorf("%w (Content-Type: %q)", ErrInvalidTorrentPayload, resp.Header.Get("Content-Type"))
	}

	return data, nil
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (c *Client) convertRssToResults(rss gojackett.Rss) []Result {
	results := make([]Result, 0, len(rss.Channel.Item))
	for _, item := range rss.Channel.Item {
		result := Result{
			Tracker:              rss.Channel.Title,
			Title:                item.Title,
			Link:                 item.Enclosure.URL,
			Details:              item.Comments,
			GUID:                 item.GUID,
			Category:             "",
			Size:                 0,
			DownloadVolumeFactor: 1.0,
			UploadVolumeFactor:   1.0,
		}

		if size, err := strconv.ParseInt(item.Size, 10, 64); err == nil {
			result.Size = size
		}

		if item.PubDate != "" {
			if t, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
				result.PublishDate = t
			} else if t, err := time.Parse(time.RFC1123, item.PubDate); err == nil {
				result.PublishDate = t
			}
		}

		if len(item.Category) > 0 {
			result.Category = item.Category[0]
		}

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

type DiscoveryResult struct {
	Indexers []JackettIndexer `json:"indexers"`
	Warnings []string         `json:"warnings,omitempty"`
}

func DiscoverJackettIndexers(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string) (DiscoveryResult, error) {
	if ctx == nil {
		log.Warn().Msg("DiscoverJackettIndexers called with nil context - this is a programming error")
		ctx = context.Background()
	}
	if baseURL = strings.TrimSpace(baseURL); baseURL == "" {
		return DiscoveryResult{Indexers: []JackettIndexer{}}, errors.New("base url is required")
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
		indexers := make([]JackettIndexer, 0, len(pIndexers))
		indexerIDs := make([]string, 0, len(pIndexers))

		for _, idx := range pIndexers {
			if !idx.Enable {
				continue
			}
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
				Configured:  true,
				Backend:     models.TorznabBackendProwlarr,
			}

			indexers = append(indexers, indexer)
			indexerIDs = append(indexerIDs, indexer.ID)
		}

		capsMap, failedIDs := fetchCapsParallel(ctx, baseURL, apiKey, basicUsername, basicPassword, models.TorznabBackendProwlarr, indexerIDs)

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

	return DiscoveryResult{Indexers: []JackettIndexer{}}, fmt.Errorf("jackett discovery failed: %w; prowlarr discovery failed: %w", jackettErr, prowlarrErr)
}

func discoverJackettIndexers(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string) ([]JackettIndexer, []string, error) {
	client := gojackett.NewClient(gojackett.Config{
		Host:      baseURL,
		APIKey:    apiKey,
		BasicUser: strings.TrimSpace(stringOrEmpty(basicUsername)),
		BasicPass: strings.TrimSpace(stringOrEmpty(basicPassword)),
	})

	indexersResp, err := client.GetIndexersCtx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get indexers: %w", err)
	}

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

		if indexer.Configured {
			configuredIDs = append(configuredIDs, idx.ID)
		}
	}

	capsMap, failedIDs := fetchCapsParallel(ctx, baseURL, apiKey, basicUsername, basicPassword, models.TorznabBackendJackett, configuredIDs)

	for i := range indexers {
		if caps, ok := capsMap[indexers[i].ID]; ok {
			indexers[i].Caps = caps.Capabilities
			indexers[i].Categories = caps.Categories
		}
	}

	return indexers, failedIDs, nil
}

type capsFetchResult struct {
	indexerID string
	caps      *TorznabCaps
	err       error
}

func fetchCapsParallel(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string, backend models.TorznabBackend, indexerIDs []string) (map[string]*TorznabCaps, []string) {
	if len(indexerIDs) == 0 {
		return nil, nil
	}

	const (
		maxConcurrent = 10
		maxRetries    = 2
		retryDelay    = 500 * time.Millisecond
		fetchTimeout  = 15 * time.Second
	)

	results := make(map[string]*TorznabCaps)
	resultsChan := make(chan capsFetchResult, len(indexerIDs))

	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup
	for _, id := range indexerIDs {
		wg.Add(1)
		go func(indexerID string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				resultsChan <- capsFetchResult{indexerID: indexerID, err: ctx.Err()}
				return
			default:
			}

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

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

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

func fetchCapsWithRetry(ctx context.Context, baseURL, apiKey string, basicUsername, basicPassword *string, backend models.TorznabBackend, indexerID string, maxRetries int, retryDelay, timeout time.Duration) (*TorznabCaps, error) {
	client := NewClient(baseURL, apiKey, basicUsername, basicPassword, backend, int(timeout.Seconds()))

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay * time.Duration(1<<(attempt-1))):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		caps, err := client.FetchCaps(attemptCtx, indexerID)
		cancel()

		if err == nil && caps != nil {
			return caps, nil
		}
		lastErr = err
		var responseErr interface{ HTTPStatusCode() int }
		if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusTooManyRequests {
			return nil, err
		}

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
