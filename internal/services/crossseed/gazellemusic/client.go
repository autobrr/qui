// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package gazellemusic implements gzlx-style cross-seed matching against Gazelle trackers
// (OPS/RED) using their JSON APIs.
package gazellemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// sharedTransport enables connection pooling across clients.
var sharedTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	t.ForceAttemptHTTP2 = true
	return t
}()

type TrackerSpec struct {
	Host       string
	RateLimit  int
	RatePeriod int
	SourceFlag string
}

var KnownTrackers = map[string]TrackerSpec{
	"redacted.sh": {
		Host:       "redacted.sh",
		RateLimit:  10,
		RatePeriod: 10,
		SourceFlag: "RED",
	},
	"orpheus.network": {
		Host:       "orpheus.network",
		RateLimit:  5,
		RatePeriod: 10,
		SourceFlag: "OPS",
	},
}

// TrackerToSite maps announce tracker hosts to their API site hosts.
var TrackerToSite = map[string]string{
	"flacsfor.me":    "redacted.sh",
	"home.opsfet.ch": "orpheus.network",
}

type AjaxResponse struct {
	Status   string          `json:"status"`
	Response json.RawMessage `json:"response"`
	Error    string          `json:"error"`
}

type TorrentResponse struct {
	Group   TorrentGroup   `json:"group"`
	Torrent TorrentDetails `json:"torrent"`
}

type TorrentGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type TorrentDetails struct {
	ID       int64  `json:"id"`
	InfoHash string `json:"infoHash"`
	Size     int64  `json:"size"`
	FileList string `json:"fileList"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type SearchResult struct {
	GroupID   FlexInt         `json:"groupId"`
	GroupName string          `json:"groupName"`
	Artist    string          `json:"artist"`
	Torrents  []SearchTorrent `json:"torrents"`
}

type SearchTorrent struct {
	TorrentID FlexInt `json:"torrentId"`
	Size      int64   `json:"size"`
}

// FlexInt handles JSON fields that can be either string or number.
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := parseInt64(s)
		if err != nil {
			return err
		}
		*f = FlexInt(parsed)
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into FlexInt", string(data))
}

type TorrentSearchResult struct {
	TorrentID int64
	GroupID   int64
	Size      int64
	Title     string
	InfoHash  string
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	limiter    *rate.Limiter
	host       string
	spec       TrackerSpec
}

func NewClient(serverURL, apiKey string) (*Client, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	host := parsed.Host
	spec, ok := KnownTrackers[host]
	if !ok {
		return nil, fmt.Errorf("unsupported gazelle host: %s", host)
	}
	limiter := rate.NewLimiter(rate.Every(time.Duration(spec.RatePeriod)*time.Second/time.Duration(spec.RateLimit)), 1)

	return &Client{
		baseURL: serverURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: sharedTransport,
		},
		limiter: limiter,
		host:    host,
		spec:    spec,
	}, nil
}

func (c *Client) Host() string       { return c.host }
func (c *Client) SourceFlag() string { return c.spec.SourceFlag }

func (c *Client) request(ctx context.Context, method, endpoint string, params url.Values) ([]byte, int, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, fmt.Errorf("rate limit wait failed: %w", err)
	}
	reqURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(c.baseURL, "/"), endpoint)
	if len(params) > 0 {
		reqURL = fmt.Sprintf("%s?%s", reqURL, params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request for %s: %w", endpoint, err)
	}
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("User-Agent", "qui/1.0 (gazellemusic)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request to %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response from %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		// Keep body text; callers may log a short snippet.
		return body, resp.StatusCode, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return body, resp.StatusCode, nil
}

func (c *Client) ajax(ctx context.Context, action string, params url.Values) (*AjaxResponse, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("action", action)
	body, _, err := c.request(ctx, http.MethodGet, "ajax.php", params)
	if err != nil {
		return nil, err
	}
	var resp AjaxResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("API error: %s", resp.Error)
	}
	return &resp, nil
}

func (c *Client) SearchByHash(ctx context.Context, hash string) (*TorrentSearchResult, error) {
	params := url.Values{}
	params.Set("hash", strings.ToUpper(hash))

	resp, err := c.ajax(ctx, "torrent", params)
	if err != nil {
		// Gazelle uses "bad parameters" for not-found. Treat as miss.
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "bad id parameter") ||
			strings.Contains(lower, "bad parameters") ||
			strings.Contains(lower, "bad hash parameter") {
			log.Trace().Str("hash", hash).Str("site", c.host).Msg("gazelle: no match by hash")
			return nil, nil
		}
		return nil, err
	}

	var torrentResp TorrentResponse
	if err := json.Unmarshal(resp.Response, &torrentResp); err != nil {
		return nil, err
	}

	return &TorrentSearchResult{
		TorrentID: torrentResp.Torrent.ID,
		GroupID:   torrentResp.Group.ID,
		Size:      torrentResp.Torrent.Size,
		Title:     torrentResp.Group.Name,
		InfoHash:  torrentResp.Torrent.InfoHash,
	}, nil
}

func (c *Client) SearchByFilename(ctx context.Context, filename string) ([]TorrentSearchResult, error) {
	params := url.Values{}
	params.Set("filelist", filename)

	resp, err := c.ajax(ctx, "browse", params)
	if err != nil {
		return nil, err
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(resp.Response, &searchResp); err != nil {
		return nil, err
	}

	results := make([]TorrentSearchResult, 0, 64)
	for _, r := range searchResp.Results {
		for _, t := range r.Torrents {
			results = append(results, TorrentSearchResult{
				TorrentID: int64(t.TorrentID),
				GroupID:   int64(r.GroupID),
				Size:      t.Size,
				Title:     r.GroupName,
			})
		}
	}
	return results, nil
}

func (c *Client) GetTorrent(ctx context.Context, torrentID int64) (*TorrentResponse, error) {
	params := url.Values{}
	params.Set("id", strconv.FormatInt(torrentID, 10))

	resp, err := c.ajax(ctx, "torrent", params)
	if err != nil {
		return nil, err
	}

	var torrentResp TorrentResponse
	if err := json.Unmarshal(resp.Response, &torrentResp); err != nil {
		return nil, err
	}
	return &torrentResp, nil
}

func (c *Client) DownloadTorrent(ctx context.Context, torrentID int64) ([]byte, error) {
	params := url.Values{}
	params.Set("action", "download")
	params.Set("id", strconv.FormatInt(torrentID, 10))

	body, _, err := c.request(ctx, http.MethodGet, "ajax.php", params)
	if err != nil {
		return nil, err
	}

	// Robust validation: accept any key ordering; ensure bencoded dict with torrent keys.
	if !looksLikeTorrentPayload(body) {
		var ajaxErr AjaxResponse
		if json.Unmarshal(body, &ajaxErr) == nil && ajaxErr.Error != "" {
			return nil, fmt.Errorf("download failed: %s", ajaxErr.Error)
		}
		return nil, fmt.Errorf("downloaded data appears invalid (size=%d)", len(body))
	}
	return body, nil
}

func looksLikeTorrentPayload(body []byte) bool {
	if len(body) == 0 || body[0] != 'd' {
		return false
	}

	decoded, err := decodeBencode(body)
	if err != nil {
		return false
	}

	m, ok := decoded.(map[string]any)
	if !ok {
		return false
	}

	// "info" is required for .torrent files; it's specific enough to reject most non-torrent payloads.
	info, ok := m["info"]
	if !ok {
		return false
	}

	// Ensure "info" is a dictionary.
	_, ok = info.(map[string]any)
	return ok
}
