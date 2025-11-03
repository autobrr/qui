// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	qbt "github.com/autobrr/go-qbittorrent"
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
	// Tags to apply to the cross-seeded torrent
	Tags []string `json:"tags,omitempty"`
	// SkipIfExists if true, skip cross-seeding if torrent already exists on target
	SkipIfExists bool `json:"skip_if_exists,omitempty"`
}

// CrossSeedResponse represents the result of a cross-seed operation
type CrossSeedResponse struct {
	// Success indicates if any instances were successfully cross-seeded
	Success bool `json:"success"`
	// Results contains per-instance results
	Results []InstanceCrossSeedResult `json:"results"`
	// TorrentInfo contains information about the torrent being cross-seeded
	TorrentInfo *TorrentInfo `json:"torrent_info,omitempty"`
}

// InstanceCrossSeedResult represents the result for a single instance
type InstanceCrossSeedResult struct {
	InstanceID   int    `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Success      bool   `json:"success"`
	// Status describes the result: "added", "exists", "no_match", "error"
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// MatchedTorrent is the existing torrent that matched (if any)
	MatchedTorrent *MatchedTorrent `json:"matched_torrent,omitempty"`
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
	InstanceID    int           `json:"instance_id,omitempty"`
	InstanceName  string        `json:"instance_name,omitempty"`
	Hash          string        `json:"hash,omitempty"`
	Name          string        `json:"name"`
	Size          int64         `json:"size"`
	Progress      float64       `json:"progress,omitempty"`
	TotalFiles    int           `json:"total_files,omitempty"`    // Total files in torrent
	MatchingFiles int           `json:"matching_files,omitempty"` // Files that match source
	FileCount     int           `json:"file_count"`               // Deprecated: use TotalFiles
	Files         []TorrentFile `json:"files,omitempty"`
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
	// IgnorePatterns are file patterns to ignore when matching (e.g., "*.srt", "*sample*.mkv")
	IgnorePatterns []string `json:"ignore_patterns,omitempty"`
	// TargetInstanceIDs specifies which instances to search for EXISTING torrents with matching files
	// If empty, will search all instances
	TargetInstanceIDs []int `json:"target_instance_ids,omitempty"`
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
}
