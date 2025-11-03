// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is a simple Jackett/Torznab API client
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewClient creates a new Jackett client
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// TorznabFeed represents the XML response from a Torznab API
type TorznabFeed struct {
	XMLName xml.Name       `xml:"rss"`
	Channel TorznabChannel `xml:"channel"`
}

// TorznabChannel represents the channel element in Torznab XML
type TorznabChannel struct {
	Title       string        `xml:"title"`
	Description string        `xml:"description"`
	Items       []TorznabItem `xml:"item"`
}

// TorznabItem represents a single result item
type TorznabItem struct {
	Title      string           `xml:"title"`
	GUID       string           `xml:"guid"`
	Link       string           `xml:"link"`
	Comments   string           `xml:"comments"`
	PubDate    string           `xml:"pubDate"`
	Size       int64            `xml:"size"`
	Category   string           `xml:"category"`
	Enclosure  TorznabEnclosure `xml:"enclosure"`
	Attributes []TorznabAttr    `xml:"attr"`
}

// TorznabEnclosure represents the enclosure element (download link)
type TorznabEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

// TorznabAttr represents Torznab attributes
type TorznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// TorznabCaps represents the capabilities response
type TorznabCaps struct {
	XMLName    xml.Name          `xml:"caps"`
	Server     TorznabServer     `xml:"server"`
	Searching  TorznabSearching  `xml:"searching"`
	Categories []TorznabCategory `xml:"categories>category"`
}

// TorznabServer represents server information
type TorznabServer struct {
	Title   string `xml:"title,attr"`
	Version string `xml:"version,attr"`
}

// TorznabSearching represents search capabilities
type TorznabSearching struct {
	Search      TorznabSearchType `xml:"search"`
	TVSearch    TorznabSearchType `xml:"tv-search"`
	MovieSearch TorznabSearchType `xml:"movie-search"`
}

// TorznabSearchType represents a search type capability
type TorznabSearchType struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

// TorznabCategory represents a category
type TorznabCategory struct {
	ID          int               `xml:"id,attr"`
	Name        string            `xml:"name,attr"`
	Description string            `xml:"description,attr"`
	Subcats     []TorznabCategory `xml:"subcat"`
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

// Category represents a simplified category
type Category struct {
	ID   int
	Name string
}

// SearchAll searches across all indexers using the "all" endpoint
func (c *Client) SearchAll(params url.Values) ([]Result, error) {
	return c.Search("all", params)
}

// Search performs a search on a specific indexer or "all"
func (c *Client) Search(indexer string, params url.Values) ([]Result, error) {
	// Build URL: /api/v2.0/indexers/{indexer}/results/torznab/api
	searchURL := fmt.Sprintf("%s/api/v2.0/indexers/%s/results/torznab/api", c.baseURL, indexer)

	// Add API key
	if params == nil {
		params = url.Values{}
	}
	params.Set("apikey", c.apiKey)
	params.Set("t", "search") // Torznab search type

	fullURL := fmt.Sprintf("%s?%s", searchURL, params.Encode())

	// Make HTTP request
	resp, err := c.client.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jackett returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse XML response
	var feed TorznabFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("failed to parse XML response: %w", err)
	}

	// Convert to Result format
	results := make([]Result, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		result := Result{
			Tracker:              feed.Channel.Title,
			Title:                item.Title,
			Link:                 item.Enclosure.URL,
			Details:              item.Comments,
			GUID:                 item.GUID,
			Category:             item.Category,
			Size:                 item.Size,
			DownloadVolumeFactor: 1.0,
			UploadVolumeFactor:   1.0,
		}

		// Parse pub date
		if item.PubDate != "" {
			if t, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
				result.PublishDate = t
			} else if t, err := time.Parse(time.RFC1123, item.PubDate); err == nil {
				result.PublishDate = t
			}
		}

		// Parse attributes
		for _, attr := range item.Attributes {
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

	return results, nil
}

// GetCaps retrieves the capabilities of an indexer
func (c *Client) GetCaps(indexer string) (*TorznabCaps, error) {
	// Build URL: /api/v2.0/indexers/{indexer}/results/torznab/api?t=caps
	capsURL := fmt.Sprintf("%s/api/v2.0/indexers/%s/results/torznab/api?t=caps&apikey=%s", 
		c.baseURL, indexer, c.apiKey)

	// Make HTTP request
	resp, err := c.client.Get(capsURL)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jackett returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse XML response
	var caps TorznabCaps
	if err := xml.NewDecoder(resp.Body).Decode(&caps); err != nil {
		return nil, fmt.Errorf("failed to parse caps XML: %w", err)
	}

	return &caps, nil
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
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Build URL: /api/v2.0/indexers/all/results/torznab/api?t=indexers
	// Note: Jackett doesn't have a dedicated indexers endpoint in the standard API
	// We'll use the /api/v2.0/indexers endpoint which is Jackett-specific
	indexersURL := fmt.Sprintf("%s/api/v2.0/indexers?configured=true", baseURL)

	req, err := http.NewRequest("GET", indexersURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add API key header for Jackett API
	req.Header.Set("X-Api-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jackett returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var indexers []JackettIndexer
	if err := json.NewDecoder(resp.Body).Decode(&indexers); err != nil {
		return nil, fmt.Errorf("failed to parse indexers JSON: %w", err)
	}

	return indexers, nil
}