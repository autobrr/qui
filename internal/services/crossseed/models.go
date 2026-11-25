// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"maps"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/services/jackett"
)

const (
	matchTypeContentPath = "content_path"
	matchTypeName        = "name"
	matchTypeRelease     = "release"
	matchTypeHardlink    = "hardlink"
	matchTypeReflink     = "reflink"
)

type CrossSeedRequest struct {
	TorrentData                  string   `json:"torrent_data"`
	TargetInstanceIDs            []int    `json:"target_instance_ids,omitempty"`
	Category                     string   `json:"category,omitempty"`
	Tags                         []string `json:"tags,omitempty"`
	StartPaused                  *bool    `json:"start_paused,omitempty"`
	InheritSourceTags            bool     `json:"inherit_source_tags,omitempty"`
	IndexerName                  string   `json:"indexer_name,omitempty"`
	FindIndividualEpisodes       bool     `json:"find_individual_episodes,omitempty"`
	SkipAutoResume               bool     `json:"skip_auto_resume,omitempty"`
	SkipRecheck                  bool     `json:"skip_recheck,omitempty"`
	SkipPieceBoundarySafetyCheck bool     `json:"skip_piece_boundary_safety_check,omitempty"`
	ManualTargetHash             string   `json:"manual_target_hash,omitempty"`

	sourceFilter   sourceFilter
	SearchDecision searchDecisionProvenance `json:"-"`
}

type CrossSeedResponse struct {
	Success         bool                      `json:"success"`
	Results         []InstanceCrossSeedResult `json:"results"`
	TorrentInfo     *TorrentInfo              `json:"torrent_info,omitempty"`
	titleRescueUsed bool
	seasonPackTried bool
}

type InstanceCrossSeedResult struct {
	InstanceID            int             `json:"instance_id"`
	InstanceName          string          `json:"instance_name"`
	Success               bool            `json:"success"`
	Status                string          `json:"status"`
	Message               string          `json:"message,omitempty"`
	MatchedTorrent        *MatchedTorrent `json:"matched_torrent,omitempty"`
	CrossSeedInstanceID   int             `json:"cross_seed_instance_id,omitzero"`
	CrossSeedInstanceName string          `json:"cross_seed_instance_name,omitempty"`
	partialPoolPending    bool
}

type MatchedTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	Size     int64   `json:"size"`
}

type TorrentInfo struct {
	InstanceID        int             `json:"instance_id,omitempty"`
	InstanceName      string          `json:"instance_name,omitempty"`
	Hash              string          `json:"hash,omitempty"`
	Name              string          `json:"name"`
	Category          string          `json:"category,omitempty"`
	Size              int64           `json:"size"`
	Progress          float64         `json:"progress,omitempty"`
	TotalFiles        int             `json:"total_files,omitempty"`
	MatchingFiles     int             `json:"matching_files,omitempty"`
	FileCount         int             `json:"file_count"` // Deprecated: use TotalFiles
	Files             []TorrentFile   `json:"files,omitempty"`
	ContentType       string          `json:"content_type,omitempty"`
	SearchType        string          `json:"search_type,omitempty"`
	SearchCategories  []int           `json:"search_categories,omitempty"`
	RequiredCaps      []string        `json:"required_caps,omitempty"`
	AvailableIndexers []int           `json:"available_indexers,omitempty"`
	FilteredIndexers  []int           `json:"filtered_indexers,omitempty"`
	ExcludedIndexers  map[int]string  `json:"excluded_indexers,omitempty"`
	ContentMatches    []string        `json:"content_matches,omitempty"`
	GazellePlan       []GazelleLookup `json:"gazellePlan,omitzero"`
	DiscLayout        bool            `json:"disc_layout,omitempty"`
	DiscMarker        string          `json:"disc_marker,omitempty"`
}

type TorrentFile struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
}

type FindCandidatesRequest struct {
	TorrentName            string       `json:"torrent_name"`
	TargetRelease          namedRelease `json:"-"`
	SourceIndexer          string       `json:"source_indexer,omitempty"`
	TargetInstanceIDs      []int        `json:"target_instance_ids,omitempty"`
	FindIndividualEpisodes bool         `json:"find_individual_episodes,omitempty"`
	ManualTargetHash       string       `json:"manual_target_hash,omitempty"`

	sourceFilter   sourceFilter
	SearchDecision searchDecisionProvenance `json:"-"`
}

type FindCandidatesResponse struct {
	SourceTorrent               *TorrentInfo         `json:"source_torrent"`
	Candidates                  []CrossSeedCandidate `json:"candidates"`
	seasonPackEpisodeCandidates bool
}

type FindCandidatesResponseV2 struct {
	SourceTorrent TorrentInfo   `json:"source_torrent"`
	Candidates    []TorrentInfo `json:"candidates"`
}

type CrossSeedCandidate struct {
	filePairs    []catalogFilePair
	InstanceID   int            `json:"instance_id"`
	InstanceName string         `json:"instance_name"`
	Torrents     []*qbt.Torrent `json:"torrents"`
	MatchType    string         `json:"match_type"`
	titleRescue  bool
	route        *candidateRoute
}

type TorrentSearchOptions struct {
	Query                  string   `json:"query,omitempty"`
	Limit                  int      `json:"limit,omitempty"`
	IndexerIDs             []int    `json:"indexer_ids,omitempty"`
	FindIndividualEpisodes bool     `json:"find_individual_episodes,omitempty"`
	CacheMode              string   `json:"cache_mode,omitempty"`
	DisableTorznab         bool     `json:"disable_torznab,omitempty"`
	GazelleSites           []string `json:"gazelle_sites,omitempty"`
	SkipGazelle            bool     `json:"-"`
	RescueTitleMismatches  bool     `json:"-"`
	TitleRescueResultLimit int      `json:"-"`
}

type TorrentSearchResult struct {
	Indexer              string                   `json:"indexer"`
	IndexerID            int                      `json:"indexer_id"`
	Title                string                   `json:"title"`
	DownloadURL          string                   `json:"download_url"`
	InfoURL              string                   `json:"info_url,omitempty"`
	Size                 int64                    `json:"size"`
	Seeders              int                      `json:"seeders"`
	Leechers             int                      `json:"leechers"`
	CategoryID           int                      `json:"category_id"`
	CategoryName         string                   `json:"category_name"`
	PublishDate          string                   `json:"publish_date"`
	DownloadVolumeFactor float64                  `json:"download_volume_factor"`
	UploadVolumeFactor   float64                  `json:"upload_volume_factor"`
	GUID                 string                   `json:"guid"`
	InfoHashV1           string                   `json:"infohash_v1,omitempty"`
	InfoHashV2           string                   `json:"infohash_v2,omitempty"`
	IMDbID               string                   `json:"imdb_id,omitempty"`
	TVDbID               string                   `json:"tvdb_id,omitempty"`
	MatchReason          string                   `json:"match_reason,omitempty"`
	MatchScore           float64                  `json:"match_score"`
	SearchDecision       searchDecisionProvenance `json:"-"`
}

const (
	QueryDegradedARRLookupFailed = "arr_lookup_failed"
	QueryDegradedARRNoIDs        = "arr_no_ids"
)

type TorrentSearchResponse struct {
	SourceTorrent     TorrentInfo                  `json:"source_torrent"`
	Results           []TorrentSearchResult        `json:"results"`
	Cache             *jackett.SearchCacheMetadata `json:"cache,omitempty"`
	Partial           bool                         `json:"partial,omitempty"`
	QueryDegraded     string                       `json:"query_degraded,omitempty"`
	GazelleLookups    []GazelleLookup              `json:"gazelleLookups"`
	TorznabFailed     bool                         `json:"torznab_failed,omitzero"`
	torznabErr        error
	JobID             uint64 `json:"jobId,omitempty"`
	CoveredIndexerIDs []int  `json:"-"`
	TorznabAnswered   bool   `json:"-"`
	torznabRequested  bool
	DecisionTrace     *SearchDecisionTrace `json:"decisionTrace,omitempty"`
}

type TorrentSearchSelection struct {
	IndexerID   int    `json:"indexer_id"`
	Indexer     string `json:"indexer"`
	DownloadURL string `json:"download_url"`
	Title       string `json:"title"`
	GUID        string `json:"guid,omitempty"`
}

type ApplyTorrentSearchRequest struct {
	Selections             []TorrentSearchSelection `json:"selections"`
	UseTag                 bool                     `json:"use_tag"`
	TagName                string                   `json:"tag_name,omitempty"`
	StartPaused            *bool                    `json:"start_paused,omitempty"`
	FindIndividualEpisodes bool                     `json:"find_individual_episodes,omitempty"`
}

type TorrentSearchAddResult struct {
	Title           string                    `json:"title"`
	Indexer         string                    `json:"indexer"`
	TorrentName     string                    `json:"torrent_name,omitempty"`
	InfoHash        string                    `json:"info_hash,omitempty"`
	Success         bool                      `json:"success"`
	InstanceResults []InstanceCrossSeedResult `json:"instance_results,omitempty"`
	Error           string                    `json:"error,omitempty"`
}

type ApplyTorrentSearchResponse struct {
	Results []TorrentSearchAddResult `json:"results"`
}

type LocalMatchesResponse struct {
	Matches []LocalMatch `json:"matches"`
}

const (
	UncheckedOffline  = "offline"
	UncheckedDisabled = "disabled"
)

type DeleteMatchesResponse struct {
	Matches   []LocalMatch        `json:"matches"`
	Unchecked []UncheckedInstance `json:"unchecked_instances"`
}

type UncheckedInstance struct {
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Reason       string `json:"reason"`
}

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
	MatchType     string  `json:"match_type"`
}

type AsyncIndexerFilteringState struct {
	sync.RWMutex          `json:"-"`
	CapabilitiesCompleted bool           `json:"capabilities_completed"`
	ContentCompleted      bool           `json:"content_completed"`
	CapabilityIndexers    []int          `json:"capability_indexers,omitempty"`
	FilteredIndexers      []int          `json:"filtered_indexers,omitempty"`
	ExcludedIndexers      map[int]string `json:"excluded_indexers,omitempty"`
	ContentMatches        []string       `json:"content_matches,omitempty"`
	Error                 string         `json:"error,omitempty"`

	contentType string
	torznabErr  error

	rejectedContentCandidates map[string]contentPrefilterRejectedTorrent
}

func (s *AsyncIndexerFilteringState) cloneLocked() *AsyncIndexerFilteringState {
	if s == nil {
		return nil
	}
	clone := &AsyncIndexerFilteringState{
		CapabilitiesCompleted: s.CapabilitiesCompleted,
		ContentCompleted:      s.ContentCompleted,
		Error:                 s.Error,
		contentType:           s.contentType,
		torznabErr:            s.torznabErr,
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

func (s *AsyncIndexerFilteringState) Clone() *AsyncIndexerFilteringState {
	if s == nil {
		return nil
	}
	s.RLock()
	defer s.RUnlock()
	return s.cloneLocked()
}

type AsyncTorrentAnalysis struct {
	TorrentInfo    *TorrentInfo                `json:"torrent_info"`
	FilteringState *AsyncIndexerFilteringState `json:"filtering_state"`
}

type WebhookCheckRequest struct {
	TorrentName            string `json:"torrentName"`
	InstanceIDs            []int  `json:"instanceIds,omitempty"`
	Size                   uint64 `json:"size,omitempty"`
	Indexer                string `json:"indexer,omitempty"`
	FindIndividualEpisodes *bool  `json:"findIndividualEpisodes,omitempty"`
}

type WebhookCheckMatch struct {
	InstanceID   int     `json:"instanceId"`
	InstanceName string  `json:"instanceName"`
	TorrentHash  string  `json:"torrentHash"`
	TorrentName  string  `json:"torrentName"`
	MatchType    string  `json:"matchType"`
	SizeDiff     float64 `json:"sizeDiff,omitempty"`
	Progress     float64 `json:"progress"`
}

type WebhookCheckResponse struct {
	CanCrossSeed   bool                `json:"canCrossSeed"`
	Matches        []WebhookCheckMatch `json:"matches"`
	Recommendation string              `json:"recommendation"`
	SeasonPack     *WebhookSeasonPack  `json:"seasonPack,omitempty"`
}

type WebhookSeasonPack struct {
	InstanceID       int     `json:"instanceId"`
	MatchedEpisodes  int     `json:"matchedEpisodes"`
	TotalEpisodes    int     `json:"totalEpisodes"`
	Coverage         float64 `json:"coverage"`
	ThresholdSkipped bool    `json:"thresholdSkipped"`
}

type AutobrrApplyRequest struct {
	TorrentData            string   `json:"torrentData"`
	TorrentName            string   `json:"torrentName,omitempty"`
	InstanceIDs            []int    `json:"instanceIds,omitempty"`
	Category               string   `json:"category,omitempty"`
	Tags                   []string `json:"tags,omitempty"`
	StartPaused            *bool    `json:"startPaused,omitempty"`
	FindIndividualEpisodes *bool    `json:"findIndividualEpisodes,omitempty"`
	Indexer                string   `json:"indexer,omitempty"`
}
