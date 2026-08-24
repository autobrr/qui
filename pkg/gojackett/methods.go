// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"
)

func (c *Client) GetIndexersCtx(ctx context.Context) (Indexers, error) {
	opts := map[string]string{
		"t":          "indexers",
		"configured": "true",
	}

	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var ind Indexers
	resp, err := c.getCtx(ctx, "all/results/torznab/api", opts)
	if err != nil {
		return ind, errors.Wrap(err, "all endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&ind)
	return ind, err
}

func (c *Client) GetTorrentsCtx(ctx context.Context, indexer string, opts map[string]string) (Rss, error) {
	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	resp, err := c.getCtx(ctx, indexer+"/results/torznab/api", opts)
	if err != nil {
		return rss, errors.Wrap(err, indexer+" endpoint error")
	}

	defer drainAndClose(resp.Body)

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return rss, errors.Wrap(err, "failed to read response")
	}

	// Check if the response is an error
	bodyStr := strings.TrimSpace(string(body))
	if strings.HasPrefix(bodyStr, "<error") {
		var torznabErr TorznabError
		if err := xml.Unmarshal(body, &torznabErr); err != nil {
			return rss, errors.Wrap(err, "failed to decode torznab error response")
		}
		return rss, fmt.Errorf("torznab error %s: %s", torznabErr.Code, torznabErr.Message)
	}

	// Decode the RSS response
	err = xml.Unmarshal(body, &rss)
	return rss, err
}

// SearchDirectCtx performs a direct search against a tracker's torznab API with context.
func (c *Client) SearchDirectCtx(ctx context.Context, query string, opts map[string]string) (Rss, error) {
	if opts == nil {
		opts = make(map[string]string)
	}

	opts["t"] = "search"
	if query != "" {
		opts["q"] = query
	}

	if len(c.cfg.APIKey) != 0 {
		opts["apikey"] = c.cfg.APIKey
	}

	var rss Rss
	resp, err := c.getCtx(ctx, "", opts)
	if err != nil {
		return rss, errors.Wrap(err, "direct search endpoint error")
	}

	defer drainAndClose(resp.Body)

	err = xml.NewDecoder(resp.Body).Decode(&rss)
	return rss, err
}
