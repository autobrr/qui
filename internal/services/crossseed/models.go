// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"maps"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/services/jackett"
)

// Local match type constants for determineLocalMatchType.
const (
	matchTypeContentPath = "content_path"
	matchTypeName        = "name"
	matchTypeRelease     = "release"
	matchTypeHardlink    = "hardlink"
	matchTypeReflink     = "reflink"
)

// CrossSeedRequest represents a request to cross-seed a torrent
type CrossSeedRequest struct {
	// TorrentData is the base64-encoded torrent file
	TorrentData string `json:"torrent_data"`
	// TargetInstanceIDs specifies which instances to cross-seed to
	// If empty, will attempt to cross-seed to all instances
	TargetInstanceIDs []int `json:"target_instance_ids,omitempty"`
	// Category to apply to the cross-seeded torrent
	Category string `json:"category,omitempty"`
	// Tags to apply to the cross-seeded torrent (source-specific tags from settings)
	Tags []string `json:"tags,omitempty"`
	// SkipIfExists if true, skip cross-seeding if torrent already exists on target
	SkipIfExists *bool `json:"skip_if_exists,omitempty"`
	// StartPaused controls whether newly added torrents start paused
	StartPaused *bool `json:"start_paused,omitempty"`
	// InheritSourceTags controls whether to also copy tags from the matched source torrent.
	InheritSourceTags bool `json:"inherit_source_tags,omitempty"`
	// IndexerName specifies the name of the indexer for this torrent (used with useCategoryFromIndexer setting)
	IndexerName string `json:"indexer_name,omitempty"`
	// FindIndividualEpisodes enables episode-aware matching for season packs. When true,
	// a season pack source can match individual episode candidates (useful for finding
	// episodes to seed within a pack). However, applying a season pack cross-seed is
	// rejected when the only available match is a single-episode torrent, preventing
	// incomplete "season pack from episode" outcomes.
	// If false (default), season packs will only match with other season packs.
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
	// SkipAutoResume prevents automatic resume after hash check when true.
	// Default behavior (false) resumes torrents after verification completes.
	SkipAutoResume bool `json:"skip_auto_resume,omitempty"`
	// SkipRecheck skips matches that require a recheck because of file layout or
	// an exact-size season, episode, title, or release-group relaxation.
	SkipRecheck bool `json:"skip_recheck,omitempty"`
	// SkipPieceBoundarySafetyCheck bypasses the piece boundary safety check that prevents
	// corruption when extra files share pieces with content. Risky: may corrupt existing seeded data.
	SkipPieceBoundarySafetyCheck bool `json:"skip_piece_boundary_safety_check,omitempty"`

	// SourceFilterCategories filters candidate torrents to only those in these categories.
	// Used by RSS automation to respect RSSSourceCategories setting.
	// Internal-only, not exposed via JSON API.
	SourceFilterCategories []string `json:"-"`
	// SourceFilterTags filters candidate torrents to only those with at least one of these tags.
	// Internal-only, not exposed via JSON API.
	SourceFilterTags []string `json:"-"`
	// SourceFilterExcludeCategories excludes candidate torrents in these categories.
	// Internal-only, not exposed via JSON API.
	SourceFilterExcludeCategories []string `json:"-"`
	// SourceFilterExcludeTags excludes candidate torrents with any of these tags.
	// Internal-only, not exposed via JSON API.
	SourceFilterExcludeTags []string `json:"-"`
	// SearchDecision is private provenance carried only by cached search results.
	// It binds apply to the source torrent and every relaxation search admitted.
	SearchDecision searchDecisionProvenance `json:"-"`
}

// CrossSeedResponse represents the result of a cross-seed operation
type CrossSeedResponse struct {
	// Success indicates if any instances were successfully cross-seeded
	Success bool `json:"success"`
	// Results contains per-instance results
	Results []InstanceCrossSeedResult `json:"results"`
	// TorrentInfo contains information about the torrent being cross-seeded
	TorrentInfo *TorrentInfo `json:"torrent_info,omitempty"`
	// titleRescueUsed reports that the internal torrent title still differed.
	// The search result stays private because clients cannot grant this bypass.
	titleRescueUsed bool
}

// InstanceCrossSeedResult represents the result for a single instance
type InstanceCrossSeedResult struct {
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Success      bool   `json:"success"`
	// Status describes the result (examples: "added", "exists", "no_match", "size_mismatch", "content_mismatch", "error"); this list is not exhaustive and additional statuses may be used.
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// MatchedTorrent is the existing torrent that matched (if any)
	MatchedTorrent *MatchedTorrent `json:"matched_torrent,omitempty"`
	// partialPoolPending keeps pool-only reporting out of ordinary search results.
	partialPoolPending bool
}

// MatchedTorrent represents an existing torrent that matches the cross-seed candidate
type MatchedTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	Size     int64   `json:"size"`
}

// TorrentInfo contains basic information about the torrent being cross-seeded
type TorrentInfo struct {
	InstanceID       int           `json:"instance_id,omitempty"`
	InstanceName     string        `json:"instance_name,omitempty"`
	Hash             string        `json:"hash,omitempty"`
	Name             string        `json:"name"`
	Category         string        `json:"category,omitempty"`
	Size             int64         `json:"size"`
	Progress         float64       `json:"progress,omitempty"`
	TotalFiles       int           `json:"total_files,omitempty"`    // Total files in torrent
	MatchingFiles    int           `json:"matching_files,omitempty"` // Files that match source
	FileCount        int           `json:"file_count"`               // Deprecated: use TotalFiles
	Files            []TorrentFile `json:"files,omitempty"`
	ContentType      string        `json:"content_type,omitempty"`      // Detected content type: movie, tv, music, audiobook, book, comic, game, app, adult, unknown
	SearchType       string        `json:"search_type,omitempty"`       // Search type to use: tvsearch, movie, music, book, search
	SearchCategories []int         `json:"search_categories,omitempty"` // Torznab categories required for this search
	RequiredCaps     []string      `json:"required_caps,omitempty"`     // Required indexer capabilities (e.g., "tv-search", "movie-search", "music-search")
	// Pre-filtering information for UI context menu
	AvailableIndexers []int          `json:"available_indexers,omitempty"` // Indexers available after capability filtering
	FilteredIndexers  []int          `json:"filtered_indexers,omitempty"`  // Indexers available after content filtering
	ExcludedIndexers  map[int]string `json:"excluded_indexers,omitempty"`  // Indexers excluded by content filtering with reasons
	ContentMatches    []string       `json:"content_matches,omitempty"`    // Existing torrents that match this content
	// Async filtering status
	ContentFilteringCompleted bool `json:"content_filtering_completed,omitempty"` // Whether async content filtering has finished
	// Disc layout detection
	DiscLayout bool   `json:"disc_layout,omitempty"` // True if this torrent contains disc-based media (Blu-ray/DVD)
	DiscMarker string `json:"disc_marker,omitempty"` // The marker directory name (e.g., "BDMV" or "VIDEO_TS") if DiscLayout is true
}

// TorrentFile represents a file in the torrent
type TorrentFile struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
}

// FindCandidatesRequest represents a request to find cross-seed candidates
// Use case: "I have a torrent NAME (just a string) - which existing torrents already have matching files?"
type FindCandidatesRequest struct {
	// TorrentName is the title/name of the torrent you want to add (just a string, torrent doesn't exist yet)
	TorrentName string `json:"torrent_name"`
	// TargetRelease keeps the public release fields, raw name, and selected-file
	// tag provenance together so apply cannot accidentally drop one origin.
	TargetRelease namedRelease `json:"-"`
	// SourceIndexer optionally records where the request originated (e.g., automation feed indexer)
	SourceIndexer string `json:"source_indexer,omitempty"`
	// TargetInstanceIDs specifies which instances to search for EXISTING torrents with matching files
	// If empty, will search all instances
	TargetInstanceIDs []int `json:"target_instance_ids,omitempty"`
	// FindIndividualEpisodes enables episode-aware matching for season packs. When true,
	// a season pack source can match individual episode candidates (useful for finding
	// episodes to seed within a pack). However, applying a season pack cross-seed is
	// rejected when the only available match is a single-episode torrent, preventing
	// incomplete "season pack from episode" outcomes.
	// If false (default), season packs will only match with other season packs.
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`

	// Source filters - used to restrict which existing torrents are considered as candidates.
	// These are applied when fetching torrents (if no pre-built snapshot is provided).
	// Internal-only, not exposed via JSON API.

	// SourceFilterCategories filters candidate torrents to only those in these categories.
	SourceFilterCategories []string `json:"-"`
	// SourceFilterTags filters candidate torrents to only those with at least one of these tags.
	SourceFilterTags []string `json:"-"`
	// SourceFilterExcludeCategories excludes candidate torrents in these categories.
	SourceFilterExcludeCategories []string `json:"-"`
	// SourceFilterExcludeTags excludes candidate torrents with any of these tags.
	SourceFilterExcludeTags []string `json:"-"`
	// SearchDecision binds a cached decision to the source torrent and limits
	// apply replay to exactly the relaxations search admitted.
	SearchDecision searchDecisionProvenance `json:"-"`
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
	// seasonPackEpisodeCandidates reports that the target is a season pack and the
	// library holds same-title episodes that were excluded as direct candidates.
	// Trigger signal for the season-pack assembly diversion; internal only.
	seasonPackEpisodeCandidates bool
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
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	// Torrents: The EXISTING torrents in this instance that have matching files
	// Multiple torrents may be listed because they can collectively or individually provide the needed data
	Torrents []qbt.Torrent `json:"torrents"`
	// MatchType indicates the type of match:
	//   "exact" - 100% duplicate files (same paths and sizes)
	//   "partial-in-pack" - new torrent's files are found within existing season pack
	//   "partial-contains" - new torrent is a season pack containing existing episode(s)
	//   "size" - total size matches but structure differs
	MatchType string `json:"match_type"`
	// titleRescue binds the title bypass to the exact local source torrent.
	titleRescue bool
}

// TorrentSearchOptions controls how the service searches for cross-seed matches for an existing torrent.
type TorrentSearchOptions struct {
	// Optional override for the search query; defaults to the torrent name.
	Query string `json:"query,omitempty"`
	// Limit controls how many results are requested and returned (after filtering). Defaults to 100.
	Limit int `json:"limit,omitempty"`
	// IndexerIDs restricts the search to specific Torznab indexers.
	IndexerIDs []int `json:"indexer_ids,omitempty"`
	// FindIndividualEpisodes enables episode-aware matching for season packs. When true,
	// a season pack source can match individual episode candidates (useful for finding
	// episodes to seed within a pack). However, applying a season pack cross-seed is
	// rejected when the only available match is a single-episode torrent, preventing
	// incomplete "season pack from episode" outcomes.
	// If false (default), season packs will only match with other season packs.
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
	// CacheMode forces cache behaviour when querying Torznab ("" = default, "bypass" = skip cache)
	CacheMode string `json:"cache_mode,omitempty"`
	// DisableTorznab skips all Torznab search stages while still allowing Gazelle matching.
	DisableTorznab bool `json:"disable_torznab,omitempty"`
	// SkipGazelle disables Gazelle pre-search in mixed search mode.
	// Internal-only (not exposed in API payloads).
	SkipGazelle bool `json:"-"`
	// RescueTitleMismatches admits exact-size results when only the title differs.
	// The service reads this value from saved settings.
	RescueTitleMismatches bool `json:"-"`
	// TitleRescueResultLimit limits rescue results returned to an interactive client.
	TitleRescueResultLimit int `json:"-"`
}

// TorrentSearchResult represents an indexer search result that appears to match the seeded torrent.
type TorrentSearchResult struct {
	Indexer              string  `json:"indexer"`
	IndexerID            int     `json:"indexer_id"`
	Title                string  `json:"title"`
	DownloadURL          string  `json:"download_url"`
	InfoURL              string  `json:"info_url,omitempty"`
	Size                 int64   `json:"size"`
	Seeders              int     `json:"seeders"`
	Leechers             int     `json:"leechers"`
	CategoryID           int     `json:"category_id"`
	CategoryName         string  `json:"category_name"`
	PublishDate          string  `json:"publish_date"`
	DownloadVolumeFactor float64 `json:"download_volume_factor"`
	UploadVolumeFactor   float64 `json:"upload_volume_factor"`
	GUID                 string  `json:"guid"`
	InfoHashV1           string  `json:"infohash_v1,omitempty"`
	InfoHashV2           string  `json:"infohash_v2,omitempty"`
	IMDbID               string  `json:"imdb_id,omitempty"`
	TVDbID               string  `json:"tvdb_id,omitempty"`
	MatchReason          string  `json:"match_reason,omitempty"`
	MatchScore           float64 `json:"match_score"`
	// SearchDecision is retained only in the in-memory search result cache.
	SearchDecision searchDecisionProvenance `json:"-"`
}

// Reasons for TorrentSearchResponse.QueryDegraded. No reason is reported when
// ARR is not configured or the content type has no ARR lookup: that is normal
// steady state, not a degradation of this search.
const (
	// QueryDegradedARRLookupFailed means the ARR external-ID lookup errored.
	QueryDegradedARRLookupFailed = "arr_lookup_failed"
	// QueryDegradedARRNoIDs means ARR responded but returned no usable IDs.
	QueryDegradedARRNoIDs = "arr_no_ids"
)

// TorrentSearchResponse bundles the seeded torrent information with potential cross-seed matches.
type TorrentSearchResponse struct {
	SourceTorrent TorrentInfo                  `json:"source_torrent"`
	Results       []TorrentSearchResult        `json:"results"`
	Cache         *jackett.SearchCacheMetadata `json:"cache,omitempty"`
	Partial       bool                         `json:"partial,omitempty"`
	// QueryDegraded is set when the ARR external-ID lookup could not supply IDs
	// and the Torznab search fell back to a title-only text query.
	QueryDegraded string `json:"query_degraded,omitempty"`
	// JobID identifies this search for outcome tracking (cross-seed)
	JobID uint64 `json:"jobId,omitempty"`
	// CoveredIndexerIDs lists the Torznab indexers that answered every search
	// pass of this request (primary plus any zero-result retries). Used to
	// stamp per-indexer search history; an indexer missing here was rate
	// limited or failed a pass and stays eligible for the next run.
	CoveredIndexerIDs []int `json:"-"`
}

// TorrentSearchSelection represents a user-selected search result that should be added for cross-seeding.
type TorrentSearchSelection struct {
	IndexerID   int    `json:"indexer_id"`
	Indexer     string `json:"indexer"`
	DownloadURL string `json:"download_url"`
	Title       string `json:"title"`
	GUID        string `json:"guid,omitempty"`
}

// ApplyTorrentSearchRequest describes the payload used when adding torrents found via cross-seed search.
type ApplyTorrentSearchRequest struct {
	Selections  []TorrentSearchSelection `json:"selections"`
	UseTag      bool                     `json:"use_tag"`
	TagName     string                   `json:"tag_name,omitempty"`
	StartPaused *bool                    `json:"start_paused,omitempty"`
	// FindIndividualEpisodes controls episode-aware behaviour when searching with season packs.
	FindIndividualEpisodes bool `json:"find_individual_episodes,omitempty"`
}

// TorrentSearchAddResult summarises a single add attempt from a search selection.
type TorrentSearchAddResult struct {
	Title           string                    `json:"title"`
	Indexer         string                    `json:"indexer"`
	TorrentName     string                    `json:"torrent_name,omitempty"`
	InfoHash        string                    `json:"info_hash,omitempty"`
	Success         bool                      `json:"success"`
	InstanceResults []InstanceCrossSeedResult `json:"instance_results,omitempty"`
	Error           string                    `json:"error,omitempty"`
}

// ApplyTorrentSearchResponse aggregates the results of adding multiple search selections.
type ApplyTorrentSearchResponse struct {
	Results []TorrentSearchAddResult `json:"results"`
}

// LocalMatchesResponse contains torrents from all instances that match a source torrent.
type LocalMatchesResponse struct {
	Matches []LocalMatch `json:"matches"`
}

// LocalMatch represents a torrent that matches the source across instances.
type LocalMatch struct {
	InstanceID    int     `json:"instance_id"`
	InstanceName  string  `json:"instance_name"`
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Size          int64   `json:"size"`
	Progress      float64 `json:"progress"`
	SavePath      string  `json:"save_path"`
	ContentPath   string  `json:"content_path"`
	Category      string  `json:"category"`
	Tags          string  `json:"tags"`
	State         string  `json:"state"`
	Tracker       string  `json:"tracker"`
	TrackerHealth string  `json:"tracker_health,omitempty"`
	MatchType     string  `json:"match_type"` // "content_path", "hardlink", "reflink", "name", "release"
}

// AsyncIndexerFilteringState represents the state of async indexer filtering operations
type AsyncIndexerFilteringState struct {
	sync.RWMutex          `json:"-"`
	CapabilitiesCompleted bool           `json:"capabilities_completed"`
	ContentCompleted      bool           `json:"content_completed"`
	CapabilityIndexers    []int          `json:"capability_indexers,omitempty"`
	FilteredIndexers      []int          `json:"filtered_indexers,omitempty"`
	ExcludedIndexers      map[int]string `json:"excluded_indexers,omitempty"`
	ContentMatches        []string       `json:"content_matches,omitempty"`
	Error                 string         `json:"error,omitempty"`

	rejectedContentCandidates map[string]contentPrefilterRejectedTorrent
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
		maps.Copy(clone.ExcludedIndexers, s.ExcludedIndexers)
	}
	if len(s.rejectedContentCandidates) > 0 {
		clone.rejectedContentCandidates = make(map[string]contentPrefilterRejectedTorrent, len(s.rejectedContentCandidates))
		maps.Copy(clone.rejectedContentCandidates, s.rejectedContentCandidates)
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
	// TorrentName is the release name as announced (required)
	TorrentName string `json:"torrentName"`
	// InstanceIDs optionally limits the scan to the requested instances; omit or pass an empty array to search all instances.
	InstanceIDs []int `json:"instanceIds,omitempty"`
	// Size is the total torrent size in bytes (optional - enables size validation if provided)
	Size uint64 `json:"size,omitempty"`
	// Indexer is autobrr's stable indexer identifier (for example "hdb").
	// Used to apply tracker-specific webhook matching rules.
	Indexer string `json:"indexer,omitempty"`
	// FindIndividualEpisodes overrides the default behavior when matching season packs vs episodes.
	// When omitted, qui uses the automation setting; when set, this explicitly forces the behavior.
	FindIndividualEpisodes *bool `json:"findIndividualEpisodes,omitempty"`
}

// WebhookCheckMatch represents a matched torrent in an instance
type WebhookCheckMatch struct {
	InstanceID   int     `json:"instanceId"`
	InstanceName string  `json:"instanceName"`
	TorrentHash  string  `json:"torrentHash"`
	TorrentName  string  `json:"torrentName"`
	MatchType    string  `json:"matchType"` // "metadata", "exact", "size"
	SizeDiff     float64 `json:"sizeDiff,omitempty"`
	Progress     float64 `json:"progress"`
}

// WebhookCheckResponse represents the response to a webhook check request
type WebhookCheckResponse struct {
	CanCrossSeed   bool                `json:"canCrossSeed"`
	Matches        []WebhookCheckMatch `json:"matches"`
	Recommendation string              `json:"recommendation"` // "download" or "skip"
}

// AutobrrApplyRequest represents autobrr pushing a torrent directly to qui for application.
type AutobrrApplyRequest struct {
	TorrentData string `json:"torrentData"`
	// TorrentName is the original announcement name. It is distinct from the
	// downloaded metainfo info.name used for replay validation.
	TorrentName string `json:"torrentName,omitempty"`
	// InstanceIDs optionally scopes the apply request to specific instances; omit or pass an empty array to target all matches.
	InstanceIDs  []int    `json:"instanceIds,omitempty"`
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	StartPaused  *bool    `json:"startPaused,omitempty"`
	SkipIfExists *bool    `json:"skipIfExists,omitempty"`
	// FindIndividualEpisodes overrides the automation-level episode matching behavior when set.
	FindIndividualEpisodes *bool `json:"findIndividualEpisodes,omitempty"`
	// Indexer is autobrr's stable indexer identifier (for example "hdb").
	// Used when "Use indexer name as category" mode is enabled because webhook applies
	// cannot derive tracker identity from the torrent file itself.
	Indexer string `json:"indexer,omitempty"`
}

// --- Season-pack webhook DTOs ---

// SeasonPackCheckRequest represents a request to check whether a season pack
// can be reconstructed from existing individual episodes.
type SeasonPackCheckRequest struct {
	TorrentName string `json:"torrentName"`
	TorrentData string `json:"torrentData"`
	InstanceIDs []int  `json:"instanceIds,omitempty"`
	Indexer     string `json:"indexer,omitempty"`
}

// SeasonPackCheckMatch describes per-instance episode coverage.
type SeasonPackCheckMatch struct {
	InstanceID      int     `json:"instanceId"`
	MatchedEpisodes int     `json:"matchedEpisodes"`
	TotalEpisodes   int     `json:"totalEpisodes"`
	Coverage        float64 `json:"coverage"`
}

// SeasonPackCheckResponse is the response to a season-pack check request.
type SeasonPackCheckResponse struct {
	Ready            bool                   `json:"ready"`
	Reason           string                 `json:"reason,omitempty"`
	Message          string                 `json:"message,omitempty"`
	Matches          []SeasonPackCheckMatch `json:"matches,omitempty"`
	ThresholdSkipped bool                   `json:"thresholdSkipped,omitempty"`
}

// SeasonPackApplyRequest represents a request to apply (add) a season pack torrent
// using hardlinked/reflinked episode data.
type SeasonPackApplyRequest struct {
	TorrentName string `json:"torrentName"`
	TorrentData string `json:"torrentData"`
	InstanceIDs []int  `json:"instanceIds,omitempty"`
	Indexer     string `json:"indexer,omitempty"`
	// autonomous marks internal requests originating from qui itself (RSS/automation
	// diversion) rather than the webhook. Gated by SeasonPackAutomationEnabled
	// instead of SeasonPackEnabled. Not settable via JSON by design.
	autonomous bool
}

// SeasonPackApplyResponse is the result of a season-pack apply attempt.
type SeasonPackApplyResponse struct {
	Applied         bool    `json:"applied"`
	Reason          string  `json:"reason,omitempty"`
	Message         string  `json:"message,omitempty"`
	InstanceID      int     `json:"instanceId,omitempty"`
	MatchedEpisodes int     `json:"matchedEpisodes,omitempty"`
	TotalEpisodes   int     `json:"totalEpisodes,omitempty"`
	Coverage        float64 `json:"coverage,omitempty"`
	LinkMode        string  `json:"linkMode,omitempty"`
}
