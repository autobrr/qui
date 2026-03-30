// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	tvdbBaseURL       = "https://api4.thetvdb.com/v4"
	tvdbTokenBuffer   = 24 * time.Hour
	tvdbTokenLifetime = 30 * 24 * time.Hour // TVDB tokens are valid ~30 days
)

type tvdbProvider struct {
	apiKey string
	pin    string

	token       string
	tokenExpiry time.Time
	mu          sync.Mutex

	client *http.Client
}

func newTVDBProvider(apiKey, pin string) *tvdbProvider {
	return &tvdbProvider{
		apiKey: apiKey,
		pin:    pin,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// EpisodesInSeason searches for a series on TVDB and returns the episode count for a season.
func (p *tvdbProvider) EpisodesInSeason(ctx context.Context, title string, seasonNumber int) (int, error) {
	if err := p.ensureToken(ctx); err != nil {
		return 0, fmt.Errorf("tvdb auth: %w", err)
	}

	seriesID, err := p.searchSeries(ctx, title)
	if err != nil {
		return 0, fmt.Errorf("tvdb search for %q: %w", title, err)
	}

	return p.countEpisodes(ctx, seriesID, seasonNumber)
}

func (p *tvdbProvider) ensureToken(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && time.Now().Before(p.tokenExpiry.Add(-tvdbTokenBuffer)) {
		return nil
	}

	return p.login(ctx)
}

func (p *tvdbProvider) login(ctx context.Context) error {
	payload := map[string]string{"apikey": p.apiKey}
	if p.pin != "" {
		payload["pin"] = p.pin
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("tvdb: marshal login payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tvdbBaseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tvdb: create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("tvdb: login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tvdb: login returned status %d", resp.StatusCode)
	}

	respBody, err := readBody(resp)
	if err != nil {
		return fmt.Errorf("tvdb: read login response: %w", err)
	}

	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("tvdb: decode login response: %w", err)
	}

	if result.Data.Token == "" {
		return errors.New("tvdb: empty token in login response")
	}

	p.token = result.Data.Token
	p.tokenExpiry = time.Now().Add(tvdbTokenLifetime)

	log.Debug().Msg("tvdb: authenticated successfully")

	return nil
}

type tvdbSearchResult struct {
	Data []struct {
		TVDBID   string `json:"tvdb_id"`
		Name     string `json:"name"`
		ObjectID string `json:"objectID"`
	} `json:"data"`
}

func (p *tvdbProvider) searchSeries(ctx context.Context, title string) (string, error) {
	u := fmt.Sprintf("%s/search?query=%s&type=series", tvdbBaseURL, url.QueryEscape(title))

	body, err := p.doAuthGet(ctx, u)
	if err != nil {
		return "", err
	}

	var result tvdbSearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("tvdb: decode search response: %w", err)
	}

	if len(result.Data) == 0 {
		return "", fmt.Errorf("tvdb: no series found for %q", title)
	}

	id := result.Data[0].TVDBID
	if id == "" {
		id = result.Data[0].ObjectID
	}

	return id, nil
}

type tvdbEpisodesResult struct {
	Data struct {
		Episodes []struct {
			SeasonNumber int `json:"seasonNumber"`
			Number       int `json:"number"`
		} `json:"episodes"`
	} `json:"data"`
}

func (p *tvdbProvider) countEpisodes(ctx context.Context, seriesID string, seasonNumber int) (int, error) {
	u := fmt.Sprintf("%s/series/%s/episodes/default?page=0&season=%d", tvdbBaseURL, seriesID, seasonNumber)

	body, err := p.doAuthGet(ctx, u)
	if err != nil {
		return 0, err
	}

	var result tvdbEpisodesResult
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("tvdb: decode episodes response: %w", err)
	}

	count := len(result.Data.Episodes)
	if count == 0 {
		return 0, fmt.Errorf("tvdb: no episodes found for season %d", seasonNumber)
	}

	return count, nil
}

func (p *tvdbProvider) doAuthGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("tvdb: create request: %w", err)
	}

	p.mu.Lock()
	req.Header.Set("Authorization", "Bearer "+p.token)
	p.mu.Unlock()

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tvdb: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("tvdb: rate limited (429)")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("tvdb: unauthorized, token may be expired")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tvdb: unexpected status %d", resp.StatusCode)
	}

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("tvdb: read response: %w", err)
	}

	return body, nil
}
