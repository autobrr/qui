// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"fmt"
	"strconv"
	"time"

	gojackett "github.com/kylesanderson/go-jackett"
)

// Client wraps the go-jackett client
type Client struct {
	client *gojackett.Client
}

// NewClient creates a new Jackett client
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		client: gojackett.NewClient(gojackett.Config{
			Host:   baseURL,
			APIKey: apiKey,
		}),
	}
}

// Result represents a single search result (simplified format)
type Result struct {
	Tracker              string
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
}

// SearchAll searches across all indexers using the "all" endpoint
func (c *Client) SearchAll(params map[string]string) ([]Result, error) {
	return c.Search("all", params)
}

// SearchDirect searches a direct Torznab endpoint (not through Jackett/Prowlarr aggregator)
// Uses the native SearchDirectCtx method from go-jackett library
func (c *Client) SearchDirect(params map[string]string) ([]Result, error) {
	query := params["q"]
	
	// Use go-jackett's native SearchDirect method
	rss, err := c.client.SearchDirectCtx(context.Background(), query, params)
	if err != nil {
		return nil, fmt.Errorf("direct search failed: %w", err)
	}

	return c.convertRssToResults(rss), nil
}

// Search performs a search on a specific indexer or "all"
func (c *Client) Search(indexer string, params map[string]string) ([]Result, error) {
	// Use go-jackett library to perform the search
	rss, err := c.client.GetTorrentsCtx(context.Background(), indexer, params)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	return c.convertRssToResults(rss), nil
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

		// Parse torznab attributes
		for _, attr := range item.Attr {
			switch attr.Name {
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

		results = append(results, result)
	}

	return results
}

// JackettIndexer represents an indexer from Jackett's indexer list
type JackettIndexer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Configured  bool   `json:"configured"`
}

// DiscoverJackettIndexers discovers all configured indexers from a Jackett instance
func DiscoverJackettIndexers(baseURL, apiKey string) ([]JackettIndexer, error) {
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

	// Convert to our JackettIndexer format
	indexers := make([]JackettIndexer, 0, len(indexersResp.Indexer))
	for _, idx := range indexersResp.Indexer {
		indexers = append(indexers, JackettIndexer{
			ID:          idx.ID,
			Name:        idx.Title,
			Description: idx.Description,
			Type:        idx.Type,
			Configured:  idx.Configured == "true",
		})
	}

	return indexers, nil
}

// GetCapabilitiesDirect gets capabilities from a direct Torznab endpoint
func (c *Client) GetCapabilitiesDirect() (*gojackett.Indexers, error) {
	indexers, err := c.client.GetCapsDirectCtx(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get capabilities: %w", err)
	}
	return &indexers, nil
}
