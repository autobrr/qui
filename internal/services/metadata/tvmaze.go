// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const tvmazeBaseURL = "https://api.tvmaze.com"

type tvmazeProvider struct {
	client *http.Client
}

func newTVMazeProvider() *tvmazeProvider {
	return &tvmazeProvider{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type tvmazeShow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tvmazeEpisode struct {
	Season  int `json:"season"`
	Number  int `json:"number"`
	Runtime int `json:"runtime"`
}

// EpisodesInSeason searches for a show by title and counts episodes in the given season.
func (p *tvmazeProvider) EpisodesInSeason(ctx context.Context, title string, seasonNumber int) (int, error) {
	showID, err := p.searchShow(ctx, title)
	if err != nil {
		// Retry with normalized title on failure.
		normalized := normalizeTitle(title)
		if normalized != title {
			log.Debug().Str("original", title).Str("normalized", normalized).Msg("tvmaze: retrying with normalized title")

			showID, err = p.searchShow(ctx, normalized)
			if err != nil {
				return 0, fmt.Errorf("tvmaze search failed for %q: %w", title, err)
			}
		} else {
			return 0, fmt.Errorf("tvmaze search failed for %q: %w", title, err)
		}
	}

	return p.countEpisodes(ctx, showID, seasonNumber)
}

func (p *tvmazeProvider) searchShow(ctx context.Context, title string) (int, error) {
	u := fmt.Sprintf("%s/singlesearch/shows?q=%s", tvmazeBaseURL, url.QueryEscape(title))

	body, err := p.doGet(ctx, u)
	if err != nil {
		return 0, err
	}

	var show tvmazeShow
	if err := json.Unmarshal(body, &show); err != nil {
		return 0, fmt.Errorf("tvmaze: decode show response: %w", err)
	}

	return show.ID, nil
}

func (p *tvmazeProvider) countEpisodes(ctx context.Context, showID, seasonNumber int) (int, error) {
	u := fmt.Sprintf("%s/shows/%d/episodes", tvmazeBaseURL, showID)

	body, err := p.doGet(ctx, u)
	if err != nil {
		return 0, err
	}

	var episodes []tvmazeEpisode
	if err := json.Unmarshal(body, &episodes); err != nil {
		return 0, fmt.Errorf("tvmaze: decode episodes response: %w", err)
	}

	count := 0
	for _, ep := range episodes {
		if ep.Season == seasonNumber {
			count++
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("tvmaze: no episodes found for season %d", seasonNumber)
	}

	return count, nil
}

func (p *tvmazeProvider) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return nil, fmt.Errorf("tvmaze: rate limited (retry-after: %s)", retryAfter)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("tvmaze: not found (404)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tvmaze: unexpected status %d", resp.StatusCode)
	}

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: read response: %w", err)
	}

	return body, nil
}

// normalizeTitle strips year suffixes, quality tags, and other noise from torrent titles.
var titleCleanupPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\s*\(\d{4}\)\s*$`),                            // (2024)
	regexp.MustCompile(`\s+\d{4}\s*$`),                                // trailing year
	regexp.MustCompile(`(?i)\s+(720|1080|2160)p.*$`),                  // quality info
	regexp.MustCompile(`(?i)\s+(hdtv|webrip|bluray|web-dl|webdl).*$`), // source info
}

func normalizeTitle(title string) string {
	result := strings.TrimSpace(title)
	for _, re := range titleCleanupPatterns {
		result = re.ReplaceAllString(result, "")
	}
	return strings.TrimSpace(result)
}

// readBody reads the full response body with a size limit.
func readBody(resp *http.Response) ([]byte, error) {
	// 2 MB limit to avoid unbounded reads.
	const maxBody = 2 << 20

	limited := http.MaxBytesReader(nil, resp.Body, maxBody)

	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}
