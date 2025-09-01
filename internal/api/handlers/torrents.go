// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type TorrentsHandler struct {
	syncManager *qbittorrent.SyncManager
}

func NewTorrentsHandler(syncManager *qbittorrent.SyncManager) *TorrentsHandler {
	return &TorrentsHandler{
		syncManager: syncManager,
	}
}

// ListTorrents returns all torrents for an instance with enhanced metadata
func (h *TorrentsHandler) ListTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse query parameters
	sort := "added_on"
	order := "desc"
	search := ""
	sessionID := r.Header.Get("X-Session-ID") // Optional session tracking

	if s := r.URL.Query().Get("sort"); s != "" {
		sort = s
		// Map frontend sort fields to qBittorrent API field names
		switch s {
		case "addedOn":
			sort = "added_on"
		case "dlspeed":
			sort = "dlspeed"
		case "upspeed":
			sort = "upspeed"
			// Add other mappings as needed
		}
	}

	if o := r.URL.Query().Get("order"); o != "" {
		order = o
	}

	if q := r.URL.Query().Get("search"); q != "" {
		search = q
	}

	// Parse filters
	var filters qbittorrent.FilterOptions

	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			log.Warn().Err(err).Msg("Failed to parse filters, ignoring")
		}
	}

	// Convert custom filters to library format
	var torrentFilterOptions qbt.TorrentFilterOptions

	// Handle status filter - take first status if multiple provided
	if len(filters.Status) > 0 {
		status := filters.Status[0]
		switch status {
		case "all":
			torrentFilterOptions.Filter = qbt.TorrentFilterAll
		case "active":
			torrentFilterOptions.Filter = qbt.TorrentFilterActive
		case "inactive":
			torrentFilterOptions.Filter = qbt.TorrentFilterInactive
		case "completed":
			torrentFilterOptions.Filter = qbt.TorrentFilterCompleted
		case "resumed":
			torrentFilterOptions.Filter = qbt.TorrentFilterResumed
		case "paused":
			torrentFilterOptions.Filter = qbt.TorrentFilterPaused
		case "stopped":
			torrentFilterOptions.Filter = qbt.TorrentFilterStopped
		case "stalled":
			torrentFilterOptions.Filter = qbt.TorrentFilterStalled
		case "uploading", "seeding":
			torrentFilterOptions.Filter = qbt.TorrentFilterUploading
		case "stalled_uploading", "stalled_seeding":
			torrentFilterOptions.Filter = qbt.TorrentFilterStalledUploading
		case "downloading":
			torrentFilterOptions.Filter = qbt.TorrentFilterDownloading
		case "stalled_downloading":
			torrentFilterOptions.Filter = qbt.TorrentFilterStalledDownloading
		case "errored", "error":
			torrentFilterOptions.Filter = qbt.TorrentFilterError
		default:
			// Default to all if unknown status
			torrentFilterOptions.Filter = qbt.TorrentFilterAll
		}
	}

	// Handle category filter - take first category if multiple provided
	if len(filters.Categories) > 0 {
		torrentFilterOptions.Category = filters.Categories[0]
	}

	// Handle tag filter - take first tag if multiple provided
	if len(filters.Tags) > 0 {
		torrentFilterOptions.Tag = filters.Tags[0]
	}

	// Note: Tracker filtering is not supported by the library, so we ignore filters.Trackers

	// Debug logging
	log.Debug().
		Str("sort", sort).
		Str("order", order).
		Str("search", search).
		Interface("filters", filters).
		Interface("torrentFilterOptions", torrentFilterOptions).
		Str("sessionID", sessionID).
		Msg("Torrent list request parameters")

	// Get all torrents with search, sorting and filters
	// Backend returns complete dataset, frontend handles virtual scrolling
	response, err := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, sort, order, search, torrentFilterOptions)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	// Data is always fresh from sync manager
	w.Header().Set("X-Data-Source", "fresh")

	RespondJSON(w, http.StatusOK, response)
}

// SyncTorrents returns server statistics for an instance (used by Dashboard)
func (h *TorrentsHandler) SyncTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get server statistics
	mainData, err := h.syncManager.GetServerStats(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get server stats")
		RespondError(w, http.StatusInternalServerError, "Failed to get server stats")
		return
	}

	RespondJSON(w, http.StatusOK, mainData)
}

// AddTorrentRequest represents a request to add a torrent
type AddTorrentRequest struct {
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	StartPaused  bool     `json:"start_paused,omitempty"`
	SkipChecking bool     `json:"skip_checking,omitempty"`
	SavePath     string   `json:"save_path,omitempty"`
}

// AddTorrent adds a new torrent
func (h *TorrentsHandler) AddTorrent(w http.ResponseWriter, r *http.Request) {
	// Set a reasonable timeout for the entire operation
	// With multiple files, we allow 60 seconds total (not per file)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse multipart form
	err = r.ParseMultipartForm(32 << 20) // 32MB max
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Failed to parse form data")
		return
	}

	var torrentFiles [][]byte
	var urls []string

	// Check for torrent files (multiple files supported)
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		fileHeaders := r.MultipartForm.File["torrent"]
		if len(fileHeaders) > 0 {
			for _, fileHeader := range fileHeaders {
				file, err := fileHeader.Open()
				if err != nil {
					log.Error().Err(err).Str("filename", fileHeader.Filename).Msg("Failed to open torrent file")
					continue
				}
				defer file.Close()

				fileContent, err := io.ReadAll(file)
				if err != nil {
					log.Error().Err(err).Str("filename", fileHeader.Filename).Msg("Failed to read torrent file")
					continue
				}
				torrentFiles = append(torrentFiles, fileContent)
			}
		}
	}

	// Check for URLs/magnet links if no files
	if len(torrentFiles) == 0 {
		urlsParam := r.FormValue("urls")
		if urlsParam != "" {
			// Support both comma and newline separated URLs
			urlsParam = strings.ReplaceAll(urlsParam, "\n", ",")
			urls = strings.Split(urlsParam, ",")
		} else {
			RespondError(w, http.StatusBadRequest, "Either torrent files or URLs are required")
			return
		}
	}

	// Parse options from form
	options := make(map[string]string)

	if category := r.FormValue("category"); category != "" {
		options["category"] = category
	}

	if tags := r.FormValue("tags"); tags != "" {
		options["tags"] = tags
	}

	// NOTE: qBittorrent's API does not properly support the start_paused_enabled preference
	// (it gets rejected/ignored when set via app/setPreferences). As a workaround, the frontend
	// now stores this preference in localStorage and applies it when adding torrents.
	// This complex logic attempts to respect qBittorrent's global preference, but since the
	// preference cannot be set via API, this is effectively unused in the current implementation.
	if pausedStr := r.FormValue("paused"); pausedStr != "" {
		requestedPaused := pausedStr == "true"

		// Get current preferences to check start_paused_enabled
		prefs, err := h.syncManager.GetAppPreferences(ctx, instanceID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get preferences for paused check, defaulting to explicit paused setting")
			// If we can't get preferences, apply the requested paused state explicitly
			if requestedPaused {
				options["paused"] = "true"
				options["stopped"] = "true"
			} else {
				options["paused"] = "false"
				options["stopped"] = "false"
			}
		} else {
			// Only set paused options if the requested state differs from the global preference
			globalStartPaused := prefs.StartPausedEnabled
			if requestedPaused != globalStartPaused {
				if requestedPaused {
					options["paused"] = "true"
					options["stopped"] = "true"
				} else {
					options["paused"] = "false"
					options["stopped"] = "false"
				}
			}
			// If requestedPaused == globalStartPaused, don't set paused options
			// This allows qBittorrent's global preference to take effect
		}
	}

	if skipChecking := r.FormValue("skip_checking"); skipChecking == "true" {
		options["skip_checking"] = "true"
	}

	if sequentialDownload := r.FormValue("sequentialDownload"); sequentialDownload == "true" {
		options["sequentialDownload"] = "true"
	}

	if firstLastPiecePrio := r.FormValue("firstLastPiecePrio"); firstLastPiecePrio == "true" {
		options["firstLastPiecePrio"] = "true"
	}

	if upLimit := r.FormValue("upLimit"); upLimit != "" {
		// Convert from KB/s to bytes/s (qBittorrent API expects bytes/s)
		if upLimitInt, err := strconv.ParseInt(upLimit, 10, 64); err == nil && upLimitInt > 0 {
			options["upLimit"] = strconv.FormatInt(upLimitInt*1024, 10)
		}
	}

	if dlLimit := r.FormValue("dlLimit"); dlLimit != "" {
		// Convert from KB/s to bytes/s (qBittorrent API expects bytes/s)
		if dlLimitInt, err := strconv.ParseInt(dlLimit, 10, 64); err == nil && dlLimitInt > 0 {
			options["dlLimit"] = strconv.FormatInt(dlLimitInt*1024, 10)
		}
	}

	if ratioLimit := r.FormValue("ratioLimit"); ratioLimit != "" {
		options["ratioLimit"] = ratioLimit
	}

	if seedingTimeLimit := r.FormValue("seedingTimeLimit"); seedingTimeLimit != "" {
		options["seedingTimeLimit"] = seedingTimeLimit
	}

	if contentLayout := r.FormValue("contentLayout"); contentLayout != "" {
		options["contentLayout"] = contentLayout
	}

	if rename := r.FormValue("rename"); rename != "" {
		options["rename"] = rename
	}

	if savePath := r.FormValue("savepath"); savePath != "" {
		options["savepath"] = savePath
		// When savepath is provided, disable autoTMM
		options["autoTMM"] = "false"
	}

	// Handle autoTMM explicitly if provided
	if autoTMM := r.FormValue("autoTMM"); autoTMM != "" {
		options["autoTMM"] = autoTMM
		// If autoTMM is true, remove savepath to let qBittorrent handle it
		if autoTMM == "true" {
			delete(options, "savepath")
		}
	}

	// Track results for multiple files
	var addedCount int
	var failedCount int
	var lastError error

	// Add torrent(s)
	if len(torrentFiles) > 0 {
		// Add from files
		for i, fileContent := range torrentFiles {
			// Check if context is already cancelled (timeout or client disconnect)
			if ctx.Err() != nil {
				log.Warn().Int("instanceID", instanceID).Msg("Request cancelled, stopping torrent additions")
				break
			}

			if err := h.syncManager.AddTorrent(ctx, instanceID, fileContent, options); err != nil {
				log.Error().Err(err).Int("instanceID", instanceID).Int("fileIndex", i).Msg("Failed to add torrent file")
				failedCount++
				lastError = err
			} else {
				addedCount++
			}
		}
	} else if len(urls) > 0 {
		// Add from URLs
		if err := h.syncManager.AddTorrentFromURLs(ctx, instanceID, urls, options); err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to add torrent from URLs")
			RespondError(w, http.StatusInternalServerError, "Failed to add torrent")
			return
		}
		addedCount = len(urls) // Assume all URLs succeeded for simplicity
	}

	// Check if any torrents failed
	if failedCount > 0 && addedCount == 0 {
		// All failed
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to add all torrents: %v", lastError))
		return
	}

	// Data will be fresh on next request from sync manager
	log.Debug().Int("instanceID", instanceID).Msg("Torrent added - next request will get fresh data from sync manager")

	// Build response message
	var message string
	if failedCount > 0 {
		message = fmt.Sprintf("Added %d torrent(s), %d failed", addedCount, failedCount)
	} else if addedCount > 1 {
		message = fmt.Sprintf("%d torrents added successfully", addedCount)
	} else {
		message = "Torrent added successfully"
	}

	RespondJSON(w, http.StatusCreated, map[string]any{
		"message": message,
		"added":   addedCount,
		"failed":  failedCount,
	})
}

// BulkActionRequest represents a bulk action request
type BulkActionRequest struct {
	Hashes                   []string                   `json:"hashes"`
	Action                   string                     `json:"action"`
	DeleteFiles              bool                       `json:"deleteFiles,omitempty"`              // For delete action
	Tags                     string                     `json:"tags,omitempty"`                     // For tag operations (comma-separated)
	Category                 string                     `json:"category,omitempty"`                 // For category operations
	Enable                   bool                       `json:"enable,omitempty"`                   // For toggleAutoTMM action
	SelectAll                bool                       `json:"selectAll,omitempty"`                // When true, apply to all torrents matching filters
	Filters                  *qbittorrent.FilterOptions `json:"filters,omitempty"`                  // Filters to apply when selectAll is true
	Search                   string                     `json:"search,omitempty"`                   // Search query when selectAll is true
	ExcludeHashes            []string                   `json:"excludeHashes,omitempty"`            // Hashes to exclude when selectAll is true
	RatioLimit               float64                    `json:"ratioLimit,omitempty"`               // For setShareLimit action
	SeedingTimeLimit         int64                      `json:"seedingTimeLimit,omitempty"`         // For setShareLimit action
	InactiveSeedingTimeLimit int64                      `json:"inactiveSeedingTimeLimit,omitempty"` // For setShareLimit action
	UploadLimit              int64                      `json:"uploadLimit,omitempty"`              // For setUploadLimit action (KB/s)
	DownloadLimit            int64                      `json:"downloadLimit,omitempty"`            // For setDownloadLimit action (KB/s)
}

// BulkAction performs bulk operations on torrents
func (h *TorrentsHandler) BulkAction(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req BulkActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input - either specific hashes or selectAll mode
	if !req.SelectAll && len(req.Hashes) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrents selected")
		return
	}

	if req.SelectAll && len(req.Hashes) > 0 {
		RespondError(w, http.StatusBadRequest, "Cannot specify both hashes and selectAll")
		return
	}

	validActions := []string{
		"pause", "resume", "delete", "deleteWithFiles",
		"recheck", "reannounce", "increasePriority", "decreasePriority",
		"topPriority", "bottomPriority", "addTags", "removeTags", "setTags", "setCategory",
		"toggleAutoTMM", "setShareLimit", "setUploadLimit", "setDownloadLimit",
	}

	valid := slices.Contains(validActions, req.Action)

	if !valid {
		RespondError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	// If selectAll is true, get all torrent hashes matching the filters
	var targetHashes []string
	if req.SelectAll {
		// Default to empty filters if not provided
		var torrentFilterOptions qbt.TorrentFilterOptions
		if req.Filters != nil {
			// Convert custom filters to library format
			filters := *req.Filters

			// Handle status filter - take first status if multiple provided
			if len(filters.Status) > 0 {
				status := filters.Status[0]
				switch status {
				case "all":
					torrentFilterOptions.Filter = qbt.TorrentFilterAll
				case "active":
					torrentFilterOptions.Filter = qbt.TorrentFilterActive
				case "inactive":
					torrentFilterOptions.Filter = qbt.TorrentFilterInactive
				case "completed":
					torrentFilterOptions.Filter = qbt.TorrentFilterCompleted
				case "resumed":
					torrentFilterOptions.Filter = qbt.TorrentFilterResumed
				case "paused":
					torrentFilterOptions.Filter = qbt.TorrentFilterPaused
				case "stopped":
					torrentFilterOptions.Filter = qbt.TorrentFilterStopped
				case "stalled":
					torrentFilterOptions.Filter = qbt.TorrentFilterStalled
				case "uploading", "seeding":
					torrentFilterOptions.Filter = qbt.TorrentFilterUploading
				case "stalled_uploading", "stalled_seeding":
					torrentFilterOptions.Filter = qbt.TorrentFilterStalledUploading
				case "downloading":
					torrentFilterOptions.Filter = qbt.TorrentFilterDownloading
				case "stalled_downloading":
					torrentFilterOptions.Filter = qbt.TorrentFilterStalledDownloading
				case "errored", "error":
					torrentFilterOptions.Filter = qbt.TorrentFilterError
				default:
					// Default to all if unknown status
					torrentFilterOptions.Filter = qbt.TorrentFilterAll
				}
			}

			// Handle category filter - take first category if multiple provided
			if len(filters.Categories) > 0 {
				torrentFilterOptions.Category = filters.Categories[0]
			}

			// Handle tag filter - take first tag if multiple provided
			if len(filters.Tags) > 0 {
				torrentFilterOptions.Tag = filters.Tags[0]
			}

			// Note: Tracker filtering is not supported by the library
		}

		// Get all torrents matching the current filters and search
		// Backend returns all data, no pagination needed
		response, err := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, "added_on", "desc", req.Search, torrentFilterOptions)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents for selectAll operation")
			RespondError(w, http.StatusInternalServerError, "Failed to get torrents for bulk action")
			return
		}

		// Extract all hashes and filter out excluded ones
		excludeSet := make(map[string]bool)
		for _, hash := range req.ExcludeHashes {
			excludeSet[hash] = true
		}

		for _, torrent := range response.Torrents {
			if !excludeSet[torrent.Hash] {
				targetHashes = append(targetHashes, torrent.Hash)
			}
		}

		log.Debug().Int("instanceID", instanceID).Int("totalFound", len(response.Torrents)).Int("excluded", len(req.ExcludeHashes)).Int("targetCount", len(targetHashes)).Str("action", req.Action).Msg("SelectAll bulk action")
	} else {
		targetHashes = req.Hashes
	}

	if len(targetHashes) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrents match the selection criteria")
		return
	}

	// Perform bulk action based on type
	switch req.Action {
	case "addTags":
		if req.Tags == "" {
			RespondError(w, http.StatusBadRequest, "Tags parameter is required for addTags action")
			return
		}
		err = h.syncManager.AddTags(r.Context(), instanceID, targetHashes, req.Tags)
	case "removeTags":
		if req.Tags == "" {
			RespondError(w, http.StatusBadRequest, "Tags parameter is required for removeTags action")
			return
		}
		err = h.syncManager.RemoveTags(r.Context(), instanceID, targetHashes, req.Tags)
	case "setTags":
		// allow empty tags to clear all tags from torrents
		err = h.syncManager.SetTags(r.Context(), instanceID, targetHashes, req.Tags)
	case "setCategory":
		err = h.syncManager.SetCategory(r.Context(), instanceID, targetHashes, req.Category)
	case "toggleAutoTMM":
		err = h.syncManager.SetAutoTMM(r.Context(), instanceID, targetHashes, req.Enable)
	case "setShareLimit":
		err = h.syncManager.SetTorrentShareLimit(r.Context(), instanceID, req.Hashes, req.RatioLimit, req.SeedingTimeLimit, req.InactiveSeedingTimeLimit)
	case "setUploadLimit":
		err = h.syncManager.SetTorrentUploadLimit(r.Context(), instanceID, req.Hashes, req.UploadLimit)
	case "setDownloadLimit":
		err = h.syncManager.SetTorrentDownloadLimit(r.Context(), instanceID, req.Hashes, req.DownloadLimit)
	case "delete":
		// Handle delete with deleteFiles parameter
		action := req.Action
		if req.DeleteFiles {
			action = "deleteWithFiles"
		}
		err = h.syncManager.BulkAction(r.Context(), instanceID, targetHashes, action)
	default:
		// Handle other standard actions
		err = h.syncManager.BulkAction(r.Context(), instanceID, targetHashes, req.Action)
	}

	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("action", req.Action).Msg("Failed to perform bulk action")
		RespondError(w, http.StatusInternalServerError, "Failed to perform bulk action")
		return
	}

	log.Debug().Int("instanceID", instanceID).Str("action", req.Action).Msg("Bulk action completed with optimistic cache update")

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Bulk action completed successfully",
	})
}

// Individual torrent actions

// DeleteTorrent deletes a single torrent
func (h *TorrentsHandler) DeleteTorrent(w http.ResponseWriter, r *http.Request) {
	// Get instance ID and hash from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	// Check if files should be deleted
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"

	action := "delete"
	if deleteFiles {
		action = "deleteWithFiles"
	}

	// Delete torrent
	if err := h.syncManager.BulkAction(r.Context(), instanceID, []string{hash}, action); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to delete torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to delete torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Torrent deleted successfully",
	})
}

// PauseTorrent pauses a single torrent
func (h *TorrentsHandler) PauseTorrent(w http.ResponseWriter, r *http.Request) {
	// Get instance ID and hash from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	// Pause torrent
	if err := h.syncManager.BulkAction(r.Context(), instanceID, []string{hash}, "pause"); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to pause torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to pause torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Torrent paused successfully",
	})
}

// ResumeTorrent resumes a single torrent
func (h *TorrentsHandler) ResumeTorrent(w http.ResponseWriter, r *http.Request) {
	// Get instance ID and hash from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	// Resume torrent
	if err := h.syncManager.BulkAction(r.Context(), instanceID, []string{hash}, "resume"); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to resume torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to resume torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Torrent resumed successfully",
	})
}

// GetCategories returns all categories
func (h *TorrentsHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get categories
	categories, err := h.syncManager.GetCategories(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get categories")
		RespondError(w, http.StatusInternalServerError, "Failed to get categories")
		return
	}

	RespondJSON(w, http.StatusOK, categories)
}

// CreateCategory creates a new category
func (h *TorrentsHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Name     string `json:"name"`
		SavePath string `json:"savePath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Category name is required")
		return
	}

	if err := h.syncManager.CreateCategory(r.Context(), instanceID, req.Name, req.SavePath); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to create category")
		RespondError(w, http.StatusInternalServerError, "Failed to create category")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]string{
		"message": "Category created successfully",
	})
}

// EditCategory edits an existing category
func (h *TorrentsHandler) EditCategory(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Name     string `json:"name"`
		SavePath string `json:"savePath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Category name is required")
		return
	}

	if err := h.syncManager.EditCategory(r.Context(), instanceID, req.Name, req.SavePath); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to edit category")
		RespondError(w, http.StatusInternalServerError, "Failed to edit category")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Category updated successfully",
	})
}

// RemoveCategories removes categories
func (h *TorrentsHandler) RemoveCategories(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Categories []string `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Categories) == 0 {
		RespondError(w, http.StatusBadRequest, "No categories provided")
		return
	}

	if err := h.syncManager.RemoveCategories(r.Context(), instanceID, req.Categories); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to remove categories")
		RespondError(w, http.StatusInternalServerError, "Failed to remove categories")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Categories removed successfully",
	})
}

// GetTags returns all tags
func (h *TorrentsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get tags
	tags, err := h.syncManager.GetTags(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get tags")
		RespondError(w, http.StatusInternalServerError, "Failed to get tags")
		return
	}

	RespondJSON(w, http.StatusOK, tags)
}

// CreateTags creates new tags
func (h *TorrentsHandler) CreateTags(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Tags) == 0 {
		RespondError(w, http.StatusBadRequest, "No tags provided")
		return
	}

	if err := h.syncManager.CreateTags(r.Context(), instanceID, req.Tags); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to create tags")
		RespondError(w, http.StatusInternalServerError, "Failed to create tags")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]string{
		"message": "Tags created successfully",
	})
}

// DeleteTags deletes tags
func (h *TorrentsHandler) DeleteTags(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Tags) == 0 {
		RespondError(w, http.StatusBadRequest, "No tags provided")
		return
	}

	if err := h.syncManager.DeleteTags(r.Context(), instanceID, req.Tags); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to delete tags")
		RespondError(w, http.StatusInternalServerError, "Failed to delete tags")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Tags deleted successfully",
	})
}

// GetTorrentProperties returns detailed properties for a specific torrent
func (h *TorrentsHandler) GetTorrentProperties(w http.ResponseWriter, r *http.Request) {
	// Get instance ID and hash from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	// Get properties
	properties, err := h.syncManager.GetTorrentProperties(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent properties")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return
	}

	RespondJSON(w, http.StatusOK, properties)
}

// GetTorrentTrackers returns trackers for a specific torrent
func (h *TorrentsHandler) GetTorrentTrackers(w http.ResponseWriter, r *http.Request) {
	// Get instance ID and hash from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	// Get trackers
	trackers, err := h.syncManager.GetTorrentTrackers(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent trackers")
		return
	}

	RespondJSON(w, http.StatusOK, trackers)
}

// GetTorrentFiles returns files information for a specific torrent
func (h *TorrentsHandler) GetTorrentFiles(w http.ResponseWriter, r *http.Request) {
	// Get instance ID and hash from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	// Get files
	files, err := h.syncManager.GetTorrentFiles(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent files")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent files")
		return
	}

	RespondJSON(w, http.StatusOK, files)
}
