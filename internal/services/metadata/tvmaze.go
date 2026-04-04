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

// ErrShowNotFound is returned when a show lookup yields no results (HTTP 404 or empty response).
var ErrShowNotFound = errors.New("show not found")

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
		// Only retry with normalized title when the show wasn't found.
		// For transient errors (rate limits, timeouts, 5xx) return immediately.
		if !errors.Is(err, ErrShowNotFound) {
			return 0, fmt.Errorf("tvmaze search failed for %q: %w", title, err)
		}

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
	// /shows/:id/episodes excludes specials by default (unlike /seasons/:id/episodes).
	// Stream-decode to avoid buffering the full response for shows with many seasons.
	u := fmt.Sprintf("%s/shows/%d/episodes", tvmazeBaseURL, showID)

	resp, err := p.doGetResponse(ctx, u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Cap reads at 2 MiB to match readBody's limit and fail fast on pathological responses.
	const maxBody = 2 << 20
	bounded := io.LimitReader(resp.Body, maxBody)

	count, err := streamCountSeasonEpisodes(bounded, seasonNumber)
	if err != nil {
		return 0, fmt.Errorf("tvmaze: decode episodes: %w", err)
	}

	if count == 0 {
		return 0, fmt.Errorf("tvmaze: no episodes found for season %d", seasonNumber)
	}

	return count, nil
}

// doGetResponse performs a GET request and returns the raw response (caller must close body).
func (p *tvmazeProvider) doGetResponse(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: request failed: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfter := resp.Header.Get("Retry-After")
		return nil, fmt.Errorf("tvmaze: rate limited (retry-after: %s)", retryAfter)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("tvmaze: %w", ErrShowNotFound)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("tvmaze: unexpected status %d", resp.StatusCode)
	}

	return resp, nil
}

func (p *tvmazeProvider) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	resp, err := p.doGetResponse(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: read response: %w", err)
	}

	return body, nil
}

// normalizeTitle strips year suffixes, quality tags, and other noise from torrent titles.
// titleCleanupPatterns strips parenthesized years, quality tags, and source
// noise from torrent titles. Bare trailing years (e.g. "Show 2024") are NOT
// stripped because they are ambiguous -- could be part of the title (e.g.
// "1923", "Yellowstone 1923"). Parenthesized years like "(2024)" are a clear
// torrent convention and safe to remove.
var titleCleanupPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\s*\(\d{4}\)\s*$`),                            // (2024)
	regexp.MustCompile(`(?i)\s+(720|1080|2160)p.*$`),                  // quality info
	regexp.MustCompile(`(?i)\s+(hdtv|webrip|bluray|web-dl|webdl).*$`), // source info
}

func normalizeTitle(title string) string {
	result := strings.TrimSpace(title)
	for {
		prev := result
		for _, re := range titleCleanupPatterns {
			result = strings.TrimSpace(re.ReplaceAllString(result, ""))
		}
		if result == prev {
			break
		}
	}
	return result
}

// streamCountSeasonEpisodes counts episodes matching the given season number
// by streaming the JSON array, avoiding full deserialization into memory.
func streamCountSeasonEpisodes(r io.Reader, seasonNumber int) (int, error) {
	dec := json.NewDecoder(r)

	// Expect opening bracket.
	t, err := dec.Token()
	if err != nil {
		return 0, fmt.Errorf("expected array start: %w", err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != '[' {
		return 0, fmt.Errorf("expected '[', got %v", t)
	}

	type episodeKey struct {
		Season int `json:"season"`
	}

	count := 0
	for dec.More() {
		var ep episodeKey
		if err := dec.Decode(&ep); err != nil {
			return 0, fmt.Errorf("decode element: %w", err)
		}
		if ep.Season == seasonNumber {
			count++
		}
	}

	// Require valid closing bracket to reject truncated responses.
	t, err = dec.Token()
	if err != nil {
		return 0, fmt.Errorf("expected array end: %w", err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != ']' {
		return 0, fmt.Errorf("expected ']', got %v", t)
	}

	return count, nil
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
