// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"time"
)

// CrossSeedSearchRequest represents a request to search for cross-seeds via Jackett
type CrossSeedSearchRequest struct {
	// Query is the search term (e.g., series name, movie title)
	Query string `json:"query"`
	// Season for TV shows (optional)
	Season *int `json:"season,omitempty"`
	// Episode for TV shows (optional)
	Episode *int `json:"episode,omitempty"`
	// Year for movies or yearly releases (optional)
	Year *int `json:"year,omitempty"`
	// Month for daily shows (optional)
	Month *int `json:"month,omitempty"`
	// Day for daily shows (optional)
	Day *int `json:"day,omitempty"`
	// IMDbID for movies/shows (optional)
	IMDbID string `json:"imdb_id,omitempty"`
	// TVDbID for TV shows (optional)
	TVDbID string `json:"tvdb_id,omitempty"`
	// Categories to search (will be auto-detected if not provided)
	Categories []int `json:"categories,omitempty"`
	// Limit the number of results
	Limit int `json:"limit,omitempty"`
	// Offset for pagination
	Offset int `json:"offset,omitempty"`
}

// TorznabSearchRequest represents a general Torznab search request
type TorznabSearchRequest struct {
	// Query is the search term
	Query string `json:"query"`
	// Categories to search
	Categories []int `json:"categories,omitempty"`
	// IMDbID for movies/shows (optional)
	IMDbID string `json:"imdb_id,omitempty"`
	// TVDbID for TV shows (optional)
	TVDbID string `json:"tvdb_id,omitempty"`
	// Season for TV shows (optional)
	Season *int `json:"season,omitempty"`
	// Episode for TV shows (optional)
	Episode *int `json:"episode,omitempty"`
	// Limit the number of results
	Limit int `json:"limit,omitempty"`
	// Offset for pagination
	Offset int `json:"offset,omitempty"`
	// IndexerIDs to search (empty = all enabled indexers)
	IndexerIDs []int `json:"indexer_ids,omitempty"`
}

// SearchResponse represents the response from a Torznab search
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Total   int            `json:"total"`
}

// SearchResult represents a single search result from Jackett
type SearchResult struct {
	// Indexer name
	Indexer string `json:"indexer"`
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
	// IMDb ID if available
	IMDbID string `json:"imdb_id,omitempty"`
	// TVDb ID if available
	TVDbID string `json:"tvdb_id,omitempty"`
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
)

// ContentType represents the type of content being searched
type ContentType int

const (
	ContentTypeUnknown ContentType = iota
	ContentTypeMovie
	ContentTypeTVShow
	ContentTypeTVDaily
	ContentTypeTVAnime
	ContentTypeXXX
	ContentTypeAudio
	ContentTypeBooks
	ContentTypePC
)

// DetectContentType attempts to detect the content type from the search parameters
func DetectContentType(req *CrossSeedSearchRequest) ContentType {
	// XXX content is usually identified by specific categories or patterns
	// For now, we'll rely on category hints if provided

	// If we have episode info, it's TV
	if req.Episode != nil && *req.Episode > 0 {
		return ContentTypeTVShow
	}

	// If we have season but no episode, could be season pack
	if req.Season != nil && *req.Season > 0 {
		return ContentTypeTVShow
	}

	// Daily shows have year/month/day
	if req.Year != nil && req.Month != nil && req.Day != nil {
		return ContentTypeTVDaily
	}

	// If we have year but no month/day, likely a movie
	if req.Year != nil && req.Month == nil && req.Day == nil {
		return ContentTypeMovie
	}

	// If we have TVDbID, it's TV
	if req.TVDbID != "" {
		return ContentTypeTVShow
	}

	// If we have IMDbID, could be movie or TV
	if req.IMDbID != "" {
		// Can't determine definitively, will use query context
		return ContentTypeUnknown
	}

	return ContentTypeUnknown
}

// GetCategoriesForContentType returns the appropriate Torznab categories for a content type
func GetCategoriesForContentType(contentType ContentType) []int {
	switch contentType {
	case ContentTypeMovie:
		return []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K}
	case ContentTypeTVShow, ContentTypeTVDaily:
		return []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K}
	case ContentTypeTVAnime:
		return []int{CategoryTVAnime}
	case ContentTypeXXX:
		return []int{CategoryXXX, CategoryXXXDVD, CategoryXXXx264, CategoryXXXPack}
	case ContentTypeAudio:
		return []int{CategoryAudio}
	case ContentTypeBooks:
		return []int{CategoryBooks, CategoryBooksEbook, CategoryBooksComics}
	case ContentTypePC:
		return []int{CategoryPC}
	default:
		// Return common categories
		return []int{CategoryMovies, CategoryTV}
	}
}
