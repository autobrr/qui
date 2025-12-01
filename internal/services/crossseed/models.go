// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/services/jackett"
)

// CrossSeedRequest represents a request to cross-seed a torrent
type CrossSeedRequest struct {
	// TargetInstanceIDs specifies which instances to cross-seed to
	// If empty, will attempt to cross-seed to all instances
	TargetInstanceIDs []int `json:"target_instance_ids,omitempty"`
	// Tags to apply to the cross-seeded torrent (source-specific tags from settings)
	Tags []string `json:"tags,omitempty"`
	// IgnorePatterns specify files to ignore when matching
	IgnorePatterns []string `json:"ignore_patterns,omitempty"`
	// SkipIfExists if true, skip cross-seeding if torrent already exists on target
	SkipIfExists *bool `json:"skip_if_exists,omitempty"`
	// StartPaused controls whether newly added torrents start paused
	StartPaused *bool `json:"start_paused,omitempty"`
	// TorrentData is the base64-encoded torrent file
	TorrentData string `json:"torrent_data"`
	// Category to apply to the cross-seeded torrent
	Category string `json:"category,omitempty"`
	// IndexerName specifies the name of the indexer for this torrent (used with useCategoryFromIndexer setting)
	IndexerName string `json:"indexer_name,omitempty"`
	// InheritSourceTags controls whether to also copy tags from the matched source torrent.
	InheritSourceTags bool `json:"inherit_source_tags,omitempty"`
	// FindIndividualEpisodes controls whether to find individual episodes when searching with season packs
	// If false (default), season packs will only match with other season packs
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
}

// CrossSeedResponse represents the result of a cross-seed operation
type CrossSeedResponse struct {
	// Results contains per-instance results
	Results []InstanceCrossSeedResult `json:"results"`
	// TorrentInfo contains information about the torrent being cross-seeded
	TorrentInfo *TorrentInfo `json:"torrent_info,omitempty"`
	// Success indicates if any instances were successfully cross-seeded
	Success bool `json:"success"`
}

// InstanceCrossSeedResult represents the result for a single instance
type InstanceCrossSeedResult struct {
	// MatchedTorrent is the existing torrent that matched (if any)
	MatchedTorrent *MatchedTorrent `json:"matched_torrent,omitempty"`
	InstanceName   string          `json:"instance_name"`
	// Status describes the result: "added", "exists", "no_match", "error"
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	InstanceID int    `json:"instance_id"`
	Success    bool   `json:"success"`
}

// MatchedTorrent represents an existing torrent that matches the cross-seed candidate
type MatchedTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

// TorrentInfo contains basic information about the torrent being cross-seeded
type TorrentInfo struct {
	ExcludedIndexers  map[int]string `json:"excluded_indexers,omitempty"` // Indexers excluded by content filtering with reasons
	Files             []TorrentFile  `json:"files,omitempty"`
	SearchCategories  []int          `json:"search_categories,omitempty"`  // Torznab categories required for this search
	RequiredCaps      []string       `json:"required_caps,omitempty"`      // Required indexer capabilities (e.g., "tv-search", "movie-search", "music-search")
	AvailableIndexers []int          `json:"available_indexers,omitempty"` // Indexers available after capability filtering
	FilteredIndexers  []int          `json:"filtered_indexers,omitempty"`  // Indexers available after content filtering
	ContentMatches    []string       `json:"content_matches,omitempty"`    // Existing torrents that match this content
	InstanceName      string         `json:"instance_name,omitempty"`
	Hash              string         `json:"hash,omitempty"`
	Name              string         `json:"name"`
	Category          string         `json:"category,omitempty"`
	ContentType       string         `json:"content_type,omitempty"` // Detected content type: movie, tv, music, audiobook, book, comic, game, app, adult, unknown
	SearchType        string         `json:"search_type,omitempty"`  // Search type to use: tvsearch, movie, music, book, search
	Size              int64          `json:"size"`
	Progress          float64        `json:"progress,omitempty"`
	InstanceID        int            `json:"instance_id,omitempty"`
	TotalFiles        int            `json:"total_files,omitempty"`    // Total files in torrent
	MatchingFiles     int            `json:"matching_files,omitempty"` // Files that match source
	FileCount         int            `json:"file_count"`               // Deprecated: use TotalFiles
	// Async filtering status
	ContentFilteringCompleted bool `json:"content_filtering_completed,omitempty"` // Whether async content filtering has finished
}

// TorrentFile represents a file in the torrent
type TorrentFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Index int    `json:"index"`
}

// FindCandidatesRequest represents a request to find cross-seed candidates
// Use case: "I have a torrent NAME (just a string) - which existing torrents already have matching files?"
type FindCandidatesRequest struct {
	// IgnorePatterns are file patterns to ignore when matching (e.g., "*.srt", "*sample*.mkv")
	IgnorePatterns []string `json:"ignore_patterns,omitempty"`
	// TargetInstanceIDs specifies which instances to search for EXISTING torrents with matching files
	// If empty, will search all instances
	TargetInstanceIDs []int `json:"target_instance_ids,omitempty"`
	// TorrentName is the title/name of the torrent you want to add (just a string, torrent doesn't exist yet)
	TorrentName string `json:"torrent_name"`
	// SourceIndexer optionally records where the request originated (e.g., automation feed indexer)
	SourceIndexer string `json:"source_indexer,omitempty"`
	// FindIndividualEpisodes controls whether to find individual episodes when searching with season packs
	// If false (default), season packs will only match with other season packs
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
}

// FindCandidatesResponse represents potential cross-seed candidates
// SourceTorrent: The NEW torrent you want to add
// Candidates: EXISTING torrents across your instances that have the files needed by the source
// Multiple candidates may be returned because:
//   - You may have multiple single episodes that collectively provide a season pack's files
//   - Different quality/group versions may exist across instances
//   - You can choose which existing torrent(s) to use as the data source
type FindCandidatesResponse struct {
	SourceTorrent *TorrentInfo         `json:"source_torrent"`
	Candidates    []CrossSeedCandidate `json:"candidates"`
}

// FindCandidatesResponseV2 represents potential cross-seed candidates (simplified format)
type FindCandidatesResponseV2 struct {
	SourceTorrent TorrentInfo   `json:"source_torrent"`
	Candidates    []TorrentInfo `json:"candidates"`
}

// CrossSeedCandidate represents EXISTING torrents that can provide data for cross-seeding
// Each candidate is an existing torrent in your client that has files matching what the new torrent needs
// There may be multiple candidates because:
//   - Multiple episodes can collectively provide a season pack
//   - The same content may exist in different qualities/groups across instances
type CrossSeedCandidate struct {
	// Torrents: The EXISTING torrents in this instance that have matching files
	// Multiple torrents may be listed because they can collectively or individually provide the needed data
	Torrents     []qbt.Torrent `json:"torrents"`
	InstanceName string        `json:"instance_name"`
	// MatchType indicates the type of match:
	//   "exact" - 100% duplicate files (same paths and sizes)
	//   "partial-in-pack" - new torrent's files are found within existing season pack
	//   "partial-contains" - new torrent is a season pack containing existing episode(s)
	//   "size" - total size matches but structure differs
	MatchType  string `json:"match_type"`
	InstanceID int    `json:"instance_id"`
}

// TorrentSearchOptions controls how the service searches for cross-seed matches for an existing torrent.
type TorrentSearchOptions struct {
	// IndexerIDs restricts the search to specific Torznab indexers.
	IndexerIDs []int `json:"indexer_ids,omitempty"`
	// Optional override for the search query; defaults to the torrent name.
	Query string `json:"query,omitempty"`
	// CacheMode forces cache behaviour when querying Torznab ("" = default, "bypass" = skip cache)
	CacheMode string `json:"cache_mode,omitempty"`
	// Limit controls how many results are returned (after filtering). Defaults to 20.
	Limit int `json:"limit,omitempty"`
	// FindIndividualEpisodes controls whether to find individual episodes when searching with season packs
	// If false (default), season packs will only match with other season packs
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
}

// TorrentSearchResult represents an indexer search result that appears to match the seeded torrent.
type TorrentSearchResult struct {
	Indexer              string  `json:"indexer"`
	Title                string  `json:"title"`
	DownloadURL          string  `json:"download_url"`
	InfoURL              string  `json:"info_url,omitempty"`
	CategoryName         string  `json:"category_name"`
	PublishDate          string  `json:"publish_date"`
	GUID                 string  `json:"guid"`
	IMDbID               string  `json:"imdb_id,omitempty"`
	TVDbID               string  `json:"tvdb_id,omitempty"`
	MatchReason          string  `json:"match_reason,omitempty"`
	Size                 int64   `json:"size"`
	DownloadVolumeFactor float64 `json:"download_volume_factor"`
	UploadVolumeFactor   float64 `json:"upload_volume_factor"`
	MatchScore           float64 `json:"match_score"`
	IndexerID            int     `json:"indexer_id"`
	Seeders              int     `json:"seeders"`
	Leechers             int     `json:"leechers"`
	CategoryID           int     `json:"category_id"`
}

// TorrentSearchResponse bundles the seeded torrent information with potential cross-seed matches.
type TorrentSearchResponse struct {
	SourceTorrent TorrentInfo                  `json:"source_torrent"`
	Results       []TorrentSearchResult        `json:"results"`
	Cache         *jackett.SearchCacheMetadata `json:"cache,omitempty"`
	// JobID identifies this search for outcome tracking (cross-seed)
	JobID   uint64 `json:"jobId,omitempty"`
	Partial bool   `json:"partial,omitempty"`
}

// TorrentSearchSelection represents a user-selected search result that should be added for cross-seeding.
type TorrentSearchSelection struct {
	Indexer     string `json:"indexer"`
	DownloadURL string `json:"download_url"`
	Title       string `json:"title"`
	GUID        string `json:"guid,omitempty"`
	IndexerID   int    `json:"indexer_id"`
}

// ApplyTorrentSearchRequest describes the payload used when adding torrents found via cross-seed search.
type ApplyTorrentSearchRequest struct {
	Selections  []TorrentSearchSelection `json:"selections"`
	StartPaused *bool                    `json:"start_paused,omitempty"`
	TagName     string                   `json:"tag_name,omitempty"`
	UseTag      bool                     `json:"use_tag"`
	// FindIndividualEpisodes ensures manual apply reuses season packs for single episodes when enabled.
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
}

// TorrentSearchAddResult summarises a single add attempt from a search selection.
type TorrentSearchAddResult struct {
	InstanceResults []InstanceCrossSeedResult `json:"instance_results,omitempty"`
	Title           string                    `json:"title"`
	Indexer         string                    `json:"indexer"`
	TorrentName     string                    `json:"torrent_name,omitempty"`
	Error           string                    `json:"error,omitempty"`
	Success         bool                      `json:"success"`
}

// ApplyTorrentSearchResponse aggregates the results of adding multiple search selections.
type ApplyTorrentSearchResponse struct {
	Results []TorrentSearchAddResult `json:"results"`
}

// AsyncIndexerFilteringState represents the state of async indexer filtering operations
type AsyncIndexerFilteringState struct {
	sync.RWMutex          `json:"-"`
	ExcludedIndexers      map[int]string `json:"excluded_indexers,omitempty"`
	CapabilityIndexers    []int          `json:"capability_indexers,omitempty"`
	FilteredIndexers      []int          `json:"filtered_indexers,omitempty"`
	ContentMatches        []string       `json:"content_matches,omitempty"`
	Error                 string         `json:"error,omitempty"`
	CapabilitiesCompleted bool           `json:"capabilities_completed"`
	ContentCompleted      bool           `json:"content_completed"`
}

// cloneLocked assumes the caller has already acquired at least a read lock.
func (s *AsyncIndexerFilteringState) cloneLocked() *AsyncIndexerFilteringState {
	if s == nil {
		return nil
	}
	clone := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: s.CapabilitiesCompleted,
		ContentCompleted:      s.ContentCompleted,
		Error:                 s.Error,
	}
	if len(s.CapabilityIndexers) > 0 {
		clone.CapabilityIndexers = append([]int(nil), s.CapabilityIndexers...)
	}
	if len(s.FilteredIndexers) > 0 {
		clone.FilteredIndexers = append([]int(nil), s.FilteredIndexers...)
	}
	if len(s.ContentMatches) > 0 {
		clone.ContentMatches = append([]string(nil), s.ContentMatches...)
	}
	if len(s.ExcludedIndexers) > 0 {
		clone.ExcludedIndexers = make(map[int]string, len(s.ExcludedIndexers))
		for id, reason := range s.ExcludedIndexers {
			clone.ExcludedIndexers[id] = reason
		}
	}
	return clone
}

// Clone creates a snapshot copy of the filtering state using a read lock.
func (s *AsyncIndexerFilteringState) Clone() *AsyncIndexerFilteringState {
	if s == nil {
		return nil
	}
	s.RLock()
	defer s.RUnlock()
	return s.cloneLocked()
}

// AsyncTorrentAnalysis represents the result of async torrent analysis with filtering state
type AsyncTorrentAnalysis struct {
	TorrentInfo    *TorrentInfo                `json:"torrent_info"`
	FilteringState *AsyncIndexerFilteringState `json:"filtering_state"`
}

// WebhookCheckRequest represents a request from autobrr to check if a release can be cross-seeded.
// The torrentName is parsed using the rls library to extract all metadata, so only the name is required.
type WebhookCheckRequest struct {
	// InstanceIDs optionally limits the scan to the requested instances; omit or pass an empty array to search all instances.
	InstanceIDs []int `json:"instanceIds,omitempty"`
	// FindIndividualEpisodes overrides the default behavior when matching season packs vs episodes.
	// When omitted, qui uses the automation setting; when set, this explicitly forces the behavior.
	FindIndividualEpisodes *bool `json:"findIndividualEpisodes,omitempty"`
	// TorrentName is the release name as announced (required)
	TorrentName string `json:"torrentName"`
	// Size is the total torrent size in bytes (optional - enables size validation if provided)
	Size uint64 `json:"size,omitempty"`
}

// WebhookCheckMatch represents a matched torrent in an instance
type WebhookCheckMatch struct {
	InstanceName string  `json:"instanceName"`
	TorrentHash  string  `json:"torrentHash"`
	TorrentName  string  `json:"torrentName"`
	MatchType    string  `json:"matchType"` // "metadata", "exact", "size"
	SizeDiff     float64 `json:"sizeDiff,omitempty"`
	Progress     float64 `json:"progress"`
	InstanceID   int     `json:"instanceId"`
}

// WebhookCheckResponse represents the response to a webhook check request
type WebhookCheckResponse struct {
	Matches        []WebhookCheckMatch `json:"matches"`
	Recommendation string              `json:"recommendation"` // "download" or "skip"
	CanCrossSeed   bool                `json:"canCrossSeed"`
}

// AutobrrApplyRequest represents autobrr pushing a torrent directly to qui for application.
type AutobrrApplyRequest struct {
	// InstanceIDs optionally scopes the apply request to specific instances; omit or pass an empty array to target all matches.
	InstanceIDs    []int    `json:"instanceIds,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	IgnorePatterns []string `json:"ignorePatterns,omitempty"`
	StartPaused    *bool    `json:"startPaused,omitempty"`
	SkipIfExists   *bool    `json:"skipIfExists,omitempty"`
	// FindIndividualEpisodes overrides the automation-level episode matching behavior when set.
	FindIndividualEpisodes *bool  `json:"findIndividualEpisodes,omitempty"`
	TorrentData            string `json:"torrentData"`
	Category               string `json:"category,omitempty"`
}
