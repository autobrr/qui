// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"time"
)

// TorznabSearchRequest represents a general Torznab search request
type TorznabSearchRequest struct {
	// Query is the search term
	Query string `json:"query"`
	// ReleaseName is the original full release name (for debugging/logging)
	ReleaseName string `json:"release_name,omitempty"`
	// Categories to search
	Categories []int `json:"categories,omitempty"`
	// IMDbID for movies/shows (optional)
	IMDbID string `json:"imdb_id,omitempty"`
	// TVDbID for TV shows (optional)
	TVDbID string `json:"tvdb_id,omitempty"`
	// TMDbID for movies (optional, from ARR lookup)
	TMDbID int `json:"tmdb_id,omitempty"`
	// TVMazeID for TV shows (optional, from ARR lookup)
	TVMazeID int `json:"tvmaze_id,omitempty"`
	// Year for movies/shows/music (optional)
	Year int `json:"year,omitempty"`
	// Season for TV shows (optional)
	Season *int `json:"season,omitempty"`
	// Episode for TV shows (optional)
	Episode *int `json:"episode,omitempty"`
	// Artist for music searches (optional)
	Artist string `json:"artist,omitempty"`
	// Album for music searches (optional)
	Album string `json:"album,omitempty"`
	// Limit the number of results
	Limit int `json:"limit,omitempty"`
	// Offset for pagination
	Offset int `json:"offset,omitempty"`
	// IndexerIDs to search (empty = all enabled indexers)
	IndexerIDs []int `json:"indexer_ids,omitempty"`
	// CacheMode controls cache behaviour (""=default, "bypass" = skip cache)
	CacheMode string `json:"cache_mode,omitempty"`
	// OmitQueryForIDs when true, omits the q parameter if IDs are present (for cross-seed ID-driven searches)
	OmitQueryForIDs bool `json:"-"`
	// SkipHistory prevents recording this search in the history buffer
	SkipHistory bool `json:"-"`
	// OnComplete is called when a search job for an indexer completes
	OnComplete func(jobID uint64, indexerID int, err error) `json:"-"`
	// OnAllComplete is called when all search jobs complete with the final results
	OnAllComplete func(*SearchResponse, error) `json:"-"`
}
type SearchResponse struct {
	Results []SearchResult       `json:"results"`
	Total   int                  `json:"total"`
	Cache   *SearchCacheMetadata `json:"cache,omitempty"`
	Partial bool                 `json:"partial,omitempty"`
	// JobID identifies this search for outcome tracking (cross-seed)
	JobID uint64 `json:"jobId,omitempty"`
}

// SearchCacheMetadata describes how the response was sourced.
type SearchCacheMetadata struct {
	Hit       bool       `json:"hit"`
	Scope     string     `json:"scope"`
	Source    string     `json:"source"`
	CachedAt  time.Time  `json:"cachedAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	LastUsed  *time.Time `json:"lastUsed,omitempty"`
}

// SearchResult represents a single search result from Jackett
type SearchResult struct {
	// Indexer name
	Indexer string `json:"indexer"`
	// Indexer identifier
	IndexerID int `json:"indexer_id"`
	// Title of the release
	Title string `json:"title"`
	// Download URL for the torrent
	DownloadURL string `json:"download_url"`
	// Info URL (details page)
	InfoURL string `json:"info_url,omitempty"`
	// Size in bytes
	Size int64 `json:"size"`
	// Seeders count
	Seeders int `json:"seeders"`
	// Leechers count
	Leechers int `json:"leechers"`
	// Category ID
	CategoryID int `json:"category_id"`
	// Category name
	CategoryName string `json:"category_name"`
	// Published date
	PublishDate time.Time `json:"publish_date"`
	// Download volume factor (0.0 = free, 1.0 = normal)
	DownloadVolumeFactor float64 `json:"download_volume_factor"`
	// Upload volume factor
	UploadVolumeFactor float64 `json:"upload_volume_factor"`
	// GUID (unique identifier)
	GUID string `json:"guid"`
	// InfoHashV1 if available
	InfoHashV1 string `json:"infohash_v1,omitempty"`
	// InfoHashV2 if available
	InfoHashV2 string `json:"infohash_v2,omitempty"`
	// IMDb ID if available
	IMDbID string `json:"imdb_id,omitempty"`
	// TVDb ID if available
	TVDbID string `json:"tvdb_id,omitempty"`
	// Source parsed from release name (e.g., "WEB-DL", "BluRay", "HDTV")
	Source string `json:"source,omitempty"`
	// Collection/streaming service parsed from release name (e.g., "AMZN", "NF", "HULU", "MAX")
	Collection string `json:"collection,omitempty"`
	// Release group parsed from release name
	Group string `json:"group,omitempty"`
}

// IndexersResponse represents the list of available indexers
type IndexersResponse struct {
	Indexers []IndexerInfo `json:"indexers"`
}

// IndexerInfo represents information about a Jackett indexer
type IndexerInfo struct {
	// ID of the indexer
	ID string `json:"id"`
	// Name of the indexer
	Name string `json:"name"`
	// Description
	Description string `json:"description,omitempty"`
	// Type (public, semi-private, private)
	Type string `json:"type"`
	// Configured (whether the indexer is configured)
	Configured bool `json:"configured"`
	// Supported categories
	Categories []CategoryInfo `json:"categories,omitempty"`
}

// CategoryInfo represents a Torznab category
type CategoryInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Torznab category constants
const (
	// Movies
	CategoryMovies   = 2000
	CategoryMoviesSD = 2030
	CategoryMoviesHD = 2040
	CategoryMovies4K = 2045
	CategoryMovies3D = 2050

	// TV
	CategoryTV            = 5000
	CategoryTVSD          = 5030
	CategoryTVHD          = 5040
	CategoryTV4K          = 5045
	CategoryTVSport       = 5060
	CategoryTVAnime       = 5070
	CategoryTVDocumentary = 5080

	// XXX
	CategoryXXX         = 6000
	CategoryXXXDVD      = 6010
	CategoryXXXWMV      = 6020
	CategoryXXXXviD     = 6030
	CategoryXXXx264     = 6040
	CategoryXXXPack     = 6050
	CategoryXXXImageSet = 6060
	CategoryXXXOther    = 6070

	// Audio
	CategoryAudio = 3000

	// PC
	CategoryPC = 4000

	// Books
	CategoryBooks       = 7000
	CategoryBooksEbook  = 7020
	CategoryBooksComics = 7030

	CacheModeDefault = ""
	CacheModeBypass  = "bypass"
)
