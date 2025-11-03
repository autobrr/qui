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
type FindCandidatesRequest struct {
	// SourceInstanceID is the instance to search for potential cross-seeds
	SourceInstanceID int `json:"source_instance_id"`
	// TorrentHash is the hash of the torrent to find candidates for (deprecated: use TorrentName)
	TorrentHash string `json:"torrent_hash,omitempty"`
	// TorrentName is the name of the torrent to find candidates for
	TorrentName string `json:"torrent_name,omitempty"`
	// IgnorePatterns are file patterns to ignore when matching (e.g., "*.srt", "*sample*.mkv")
	IgnorePatterns []string `json:"ignore_patterns,omitempty"`
	// TargetInstanceIDs specifies which instances to search for candidates
	// If empty, will search all instances except source
	TargetInstanceIDs []int `json:"target_instance_ids,omitempty"`
}

// FindCandidatesResponse represents potential cross-seed candidates (original format)
type FindCandidatesResponse struct {
	SourceTorrent *TorrentInfo         `json:"source_torrent"`
	Candidates    []CrossSeedCandidate `json:"candidates"`
}

// FindCandidatesResponseV2 represents potential cross-seed candidates (simplified format)
type FindCandidatesResponseV2 struct {
	SourceTorrent TorrentInfo   `json:"source_torrent"`
	Candidates    []TorrentInfo `json:"candidates"`
}

// CrossSeedCandidate represents a potential torrent to cross-seed
type CrossSeedCandidate struct {
	InstanceID   int           `json:"instance_id"`
	InstanceName string        `json:"instance_name"`
	Torrents     []qbt.Torrent `json:"torrents"`
	// MatchType indicates the type of match:
	//   "exact" - 100% duplicate files (same paths and sizes)
	//   "partial-in-pack" - source episode(s) found within candidate season pack
	//   "partial-contains" - source season pack contains candidate episode(s)
	//   "size" - total size matches but structure differs
	MatchType string `json:"match_type"`
}
