// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"slices"
	"time"

	"github.com/autobrr/qui/internal/models"
)

type TorznabSearchRequest struct {
	Query                   string                                       `json:"query"`
	ReleaseName             string                                       `json:"release_name,omitempty"`
	Categories              []int                                        `json:"categories,omitempty"`
	IMDbID                  string                                       `json:"imdb_id,omitempty"`
	TVDbID                  string                                       `json:"tvdb_id,omitempty"`
	TMDbID                  int                                          `json:"tmdb_id,omitempty"`
	TVMazeID                int                                          `json:"tvmaze_id,omitempty"`
	Year                    int                                          `json:"year,omitempty"`
	Season                  *int                                         `json:"season,omitempty"`
	Episode                 *int                                         `json:"episode,omitempty"`
	EpisodeMap              *models.EpisodeMap                           `json:"-"`
	Artist                  string                                       `json:"artist,omitempty"`
	Album                   string                                       `json:"album,omitempty"`
	Limit                   int                                          `json:"limit,omitempty"`
	Offset                  int                                          `json:"offset,omitempty"`
	IndexerIDs              []int                                        `json:"indexer_ids,omitempty"`
	CacheMode               string                                       `json:"cache_mode,omitempty"`
	OmitQueryForIDs         bool                                         `json:"-"`
	SkipHistory             bool                                         `json:"-"`
	SkipCachePersist        bool                                         `json:"-"`
	ReturnAllResults        bool                                         `json:"-"`
	MinimumExecutionTimeout time.Duration                                `json:"-"`
	OnComplete              func(jobID uint64, indexerID int, err error) `json:"-"`
	OnAllComplete           func(*SearchResponse, error)                 `json:"-"`
}
type SearchResponse struct {
	Results             []SearchResult       `json:"results"`
	Total               int                  `json:"total"`
	Cache               *SearchCacheMetadata `json:"cache,omitempty"`
	Partial             bool                 `json:"partial,omitempty"`
	JobID               uint64               `json:"jobId,omitempty"`
	RequestedIndexerIDs []int                `json:"-"`
	CoveredIndexerIDs   []int                `json:"-"`
}

func (r *SearchResponse) FullyCovered() bool {
	if r == nil {
		return false
	}
	for _, id := range r.RequestedIndexerIDs {
		if !slices.Contains(r.CoveredIndexerIDs, id) {
			return false
		}
	}
	return true
}

type SearchCacheMetadata struct {
	Hit       bool       `json:"hit"`
	Scope     string     `json:"scope"`
	Source    string     `json:"source"`
	CachedAt  time.Time  `json:"cachedAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	LastUsed  *time.Time `json:"lastUsed,omitempty"`
}

type SearchResult struct {
	Indexer              string    `json:"indexer"`
	IndexerID            int       `json:"indexer_id"`
	Title                string    `json:"title"`
	DownloadURL          string    `json:"download_url"`
	InfoURL              string    `json:"info_url,omitempty"`
	Size                 int64     `json:"size"`
	Seeders              int       `json:"seeders"`
	Leechers             int       `json:"leechers"`
	CategoryID           int       `json:"category_id"`
	CategoryName         string    `json:"category_name"`
	PublishDate          time.Time `json:"publish_date"`
	DownloadVolumeFactor float64   `json:"download_volume_factor"`
	UploadVolumeFactor   float64   `json:"upload_volume_factor"`
	GUID                 string    `json:"guid"`
	InfoHashV1           string    `json:"infohash_v1,omitempty"`
	InfoHashV2           string    `json:"infohash_v2,omitempty"`
	IMDbID               string    `json:"imdb_id,omitempty"`
	TVDbID               string    `json:"tvdb_id,omitempty"`
	TMDbID               string    `json:"tmdb_id,omitempty"`
	SearchIMDbID         string    `json:"search_imdb_id,omitempty"`
	SearchTVDbID         string    `json:"search_tvdb_id,omitempty"`
	SearchTMDbID         int       `json:"search_tmdb_id,omitempty"`
	Source               string    `json:"source,omitempty"`
	Collection           string    `json:"collection,omitempty"`
	Group                string    `json:"group,omitempty"`
}

type IndexersResponse struct {
	Indexers []IndexerInfo `json:"indexers"`
}

type IndexerInfo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Type        string         `json:"type"`
	Configured  bool           `json:"configured"`
	Categories  []CategoryInfo `json:"categories,omitempty"`
}

type CategoryInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

const (
	CategoryMovies   = 2000
	CategoryMoviesSD = 2030
	CategoryMoviesHD = 2040
	CategoryMovies4K = 2045
	CategoryMovies3D = 2050

	CategoryTV            = 5000
	CategoryTVSD          = 5030
	CategoryTVHD          = 5040
	CategoryTV4K          = 5045
	CategoryTVSport       = 5060
	CategoryTVAnime       = 5070
	CategoryTVDocumentary = 5080

	CategoryXXX         = 6000
	CategoryXXXDVD      = 6010
	CategoryXXXWMV      = 6020
	CategoryXXXXviD     = 6030
	CategoryXXXx264     = 6040
	CategoryXXXPack     = 6050
	CategoryXXXImageSet = 6060
	CategoryXXXOther    = 6070

	CategoryAudio = 3000

	CategoryPC = 4000

	CategoryBooks       = 7000
	CategoryBooksEbook  = 7020
	CategoryBooksComics = 7030

	CacheModeDefault = ""
	CacheModeBypass  = "bypass"
)
