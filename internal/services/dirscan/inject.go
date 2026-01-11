// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/internal/services/jackett"
)

// Injector handles downloading and injecting torrents into qBittorrent.
type Injector struct {
	jackettService JackettDownloader
	syncManager    TorrentAdder
}

// JackettDownloader is the interface for downloading torrent files.
type JackettDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

// TorrentAdder is the interface for adding torrents to qBittorrent.
type TorrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
}

// NewInjector creates a new injector.
func NewInjector(jackettService JackettDownloader, syncManager TorrentAdder) *Injector {
	return &Injector{
		jackettService: jackettService,
		syncManager:    syncManager,
	}
}

// InjectRequest contains parameters for injecting a torrent.
type InjectRequest struct {
	// InstanceID is the target qBittorrent instance.
	InstanceID int

	// Searchee that was matched.
	Searchee *Searchee

	// SearchResult that matched the searchee.
	SearchResult *jackett.SearchResult

	// SavePath is the path where qBittorrent should save the torrent.
	// This should point to the parent directory of the searchee.
	SavePath string

	// QbitPathPrefix is an optional path prefix to apply for container path mapping.
	// If set, replaces the searchee's path prefix for qBittorrent injection.
	QbitPathPrefix string

	// Category to assign to the torrent.
	Category string

	// Tags to assign to the torrent.
	Tags []string

	// StartPaused adds the torrent in paused state.
	StartPaused bool
}

// InjectResult contains the result of an injection attempt.
type InjectResult struct {
	// Success is true if the torrent was added successfully.
	Success bool

	// TorrentHash is the hash of the added torrent.
	TorrentHash string

	// Error message if injection failed.
	ErrorMessage string
}

// Inject downloads a torrent and injects it into qBittorrent.
func (i *Injector) Inject(ctx context.Context, req *InjectRequest) (*InjectResult, error) {
	result := &InjectResult{}

	// Download the torrent
	downloadReq := jackett.TorrentDownloadRequest{
		IndexerID:   req.SearchResult.IndexerID,
		DownloadURL: req.SearchResult.DownloadURL,
		GUID:        req.SearchResult.GUID,
	}

	torrentBytes, err := i.jackettService.DownloadTorrent(ctx, downloadReq)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to download torrent: %v", err)
		return result, fmt.Errorf("download torrent: %w", err)
	}

	// Calculate the save path
	savePath := i.calculateSavePath(req)

	// Build add options
	options := i.buildAddOptions(req, savePath)

	// Add the torrent to qBittorrent
	if err := i.syncManager.AddTorrent(ctx, req.InstanceID, torrentBytes, options); err != nil {
		result.ErrorMessage = fmt.Sprintf("failed to add torrent: %v", err)
		return result, fmt.Errorf("add torrent: %w", err)
	}

	result.Success = true
	return result, nil
}

// calculateSavePath determines the save path for the torrent.
func (i *Injector) calculateSavePath(req *InjectRequest) string {
	// Start with the provided save path or derive from searchee
	savePath := req.SavePath
	if savePath == "" {
		// Use the parent directory of the searchee path
		savePath = filepath.Dir(req.Searchee.Path)
	}

	// Apply path prefix mapping for containers
	if req.QbitPathPrefix != "" {
		savePath = applyPathMapping(savePath, filepath.Dir(req.Searchee.Path), req.QbitPathPrefix)
	}

	return savePath
}

// applyPathMapping replaces the original path prefix with the qBittorrent path prefix.
// Example:
//
//	original: /data/usenet/completed/Movie.Name/
//	searcheePath: /data/usenet/completed
//	qbitPrefix: /downloads/completed
//	result: /downloads/completed/Movie.Name/
func applyPathMapping(savePath, searcheePath, qbitPrefix string) string {
	// If savePath starts with searcheePath, replace that portion with qbitPrefix
	if suffix, found := strings.CutPrefix(savePath, searcheePath); found {
		return qbitPrefix + suffix
	}
	// If no match, just use the qbitPrefix as-is
	return qbitPrefix
}

// buildAddOptions builds the options map for adding a torrent.
func (i *Injector) buildAddOptions(req *InjectRequest, savePath string) map[string]string {
	options := make(map[string]string)

	// Disable autoTMM to use our explicit save path
	options["autoTMM"] = "false"

	// Set the save path
	options["savepath"] = savePath

	// Use Original layout to match existing file structure
	options["contentLayout"] = "Original"

	// Set category if provided
	if req.Category != "" {
		options["category"] = req.Category
	}

	// Set tags if provided
	if len(req.Tags) > 0 {
		options["tags"] = strings.Join(req.Tags, ",")
	}

	// Set paused state
	if req.StartPaused {
		//nolint:goconst // standard qBittorrent API values
		options["paused"] = "true"
		options["stopped"] = "true"
	}

	// Skip hash checking since we expect files to already exist
	// This significantly speeds up injection for large files
	options["skip_checking"] = "true"

	return options
}

// InjectBatch injects multiple torrents.
// Returns results for each injection attempt.
func (i *Injector) InjectBatch(ctx context.Context, requests []*InjectRequest) []*InjectResult {
	results := make([]*InjectResult, len(requests))

	for idx, req := range requests {
		result, err := i.Inject(ctx, req)
		if err != nil {
			// Error is already captured in result.ErrorMessage
			results[idx] = result
			continue
		}
		results[idx] = result
	}

	return results
}
