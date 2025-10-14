// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/publicsuffix"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type TorrentsHandler struct {
	syncManager *qbittorrent.SyncManager
}

const addTorrentMaxFormMemory int64 = 256 << 20 // 256 MiB cap for multi-file uploads

// SortedPeer represents a peer with its key for sorting
type SortedPeer struct {
	Key string `json:"key"`
	qbt.TorrentPeer
}

// SortedPeersResponse wraps the peers response with sorted peers
type SortedPeersResponse struct {
	*qbt.TorrentPeersResponse
	SortedPeers []SortedPeer `json:"sorted_peers,omitempty"`
}

func NewTorrentsHandler(syncManager *qbittorrent.SyncManager) *TorrentsHandler {
	return &TorrentsHandler{
		syncManager: syncManager,
	}
}

// ListTorrents returns paginated torrents for an instance with enhanced metadata
func (h *TorrentsHandler) ListTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse query parameters
	limit := 300 // Default pagination size
	page := 0
	sort := "addedOn"
	order := "desc"
	search := ""
	sessionID := r.Header.Get("X-Session-ID") // Optional session tracking

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 2000 {
			limit = parsed
		}
	}

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed >= 0 {
			page = parsed
		}
	}

	if s := r.URL.Query().Get("sort"); s != "" {
		sort = s
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

	// Debug logging
	log.Debug().
		Str("sort", sort).
		Str("order", order).
		Int("page", page).
		Int("limit", limit).
		Str("search", search).
		Interface("filters", filters).
		Str("sessionID", sessionID).
		Msg("Torrent list request parameters")

	// Calculate offset from page
	offset := page * limit

	// Get torrents with search, sorting and filters
	// The sync manager will handle stale-while-revalidate internally
	response, err := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, limit, offset, sort, order, search, filters)
	if err != nil {
		// Record error for user visibility
		errorStore := h.syncManager.GetErrorStore()
		if recordErr := errorStore.RecordError(r.Context(), instanceID, err); recordErr != nil {
			log.Error().Err(recordErr).Int("instanceID", instanceID).Msg("Failed to record torrent error")
		}

		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	// Data is always fresh from sync manager
	w.Header().Set("X-Data-Source", "fresh")

	RespondJSON(w, http.StatusOK, response)
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
	err = r.ParseMultipartForm(addTorrentMaxFormMemory)
	if err != nil {
		if errors.Is(err, multipart.ErrMessageTooLarge) {
			RespondError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Upload exceeded %d MB limit", addTorrentMaxFormMemory>>20))
			return
		}
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
	Location                 string                     `json:"location,omitempty"`                 // For setLocation action
	TrackerOldURL            string                     `json:"trackerOldURL,omitempty"`            // For editTrackers action
	TrackerNewURL            string                     `json:"trackerNewURL,omitempty"`            // For editTrackers action
	TrackerURLs              string                     `json:"trackerURLs,omitempty"`              // For addTrackers/removeTrackers actions
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
		"toggleAutoTMM", "setShareLimit", "setUploadLimit", "setDownloadLimit", "setLocation",
		"editTrackers", "addTrackers", "removeTrackers",
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
		if req.Filters == nil {
			req.Filters = &qbittorrent.FilterOptions{}
		}

		// Get all torrents matching the current filters and search
		// Use a very large limit to get all torrents (backend will handle this properly)
		response, err := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, 100000, 0, "added_on", "desc", req.Search, *req.Filters)
		if err != nil {
			// Record error for user visibility
			errorStore := h.syncManager.GetErrorStore()
			if recordErr := errorStore.RecordError(r.Context(), instanceID, err); recordErr != nil {
				log.Error().Err(recordErr).Int("instanceID", instanceID).Msg("Failed to record torrent error")
			}

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
		err = h.syncManager.SetTorrentShareLimit(r.Context(), instanceID, targetHashes, req.RatioLimit, req.SeedingTimeLimit, req.InactiveSeedingTimeLimit)
	case "setUploadLimit":
		err = h.syncManager.SetTorrentUploadLimit(r.Context(), instanceID, targetHashes, req.UploadLimit)
	case "setDownloadLimit":
		err = h.syncManager.SetTorrentDownloadLimit(r.Context(), instanceID, targetHashes, req.DownloadLimit)
	case "setLocation":
		if req.Location == "" {
			RespondError(w, http.StatusBadRequest, "Location parameter is required for setLocation action")
			return
		}
		err = h.syncManager.SetLocation(r.Context(), instanceID, targetHashes, req.Location)
	case "editTrackers":
		if req.TrackerOldURL == "" || req.TrackerNewURL == "" {
			RespondError(w, http.StatusBadRequest, "Both trackerOldURL and trackerNewURL are required for editTrackers action")
			return
		}
		err = h.syncManager.BulkEditTrackers(r.Context(), instanceID, targetHashes, req.TrackerOldURL, req.TrackerNewURL)
	case "addTrackers":
		if req.TrackerURLs == "" {
			RespondError(w, http.StatusBadRequest, "TrackerURLs parameter is required for addTrackers action")
			return
		}
		err = h.syncManager.BulkAddTrackers(r.Context(), instanceID, targetHashes, req.TrackerURLs)
	case "removeTrackers":
		if req.TrackerURLs == "" {
			RespondError(w, http.StatusBadRequest, "TrackerURLs parameter is required for removeTrackers action")
			return
		}
		err = h.syncManager.BulkRemoveTrackers(r.Context(), instanceID, targetHashes, req.TrackerURLs)
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

// GetActiveTrackers returns all active tracker domains with their URLs
func (h *TorrentsHandler) GetActiveTrackers(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get active trackers
	trackers, err := h.syncManager.GetActiveTrackers(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get active trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to get active trackers")
		return
	}

	RespondJSON(w, http.StatusOK, trackers)
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

// EditTrackerRequest represents a tracker edit request
type EditTrackerRequest struct {
	OldURL string `json:"oldURL"`
	NewURL string `json:"newURL"`
}

// EditTorrentTracker edits a tracker URL for a specific torrent
func (h *TorrentsHandler) EditTorrentTracker(w http.ResponseWriter, r *http.Request) {
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

	var req EditTrackerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.OldURL == "" || req.NewURL == "" {
		RespondError(w, http.StatusBadRequest, "Both oldURL and newURL are required")
		return
	}

	// Edit tracker
	err = h.syncManager.EditTorrentTracker(r.Context(), instanceID, hash, req.OldURL, req.NewURL)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to edit tracker")
		RespondError(w, http.StatusInternalServerError, "Failed to edit tracker")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// AddTrackerRequest represents a tracker add request
type AddTrackerRequest struct {
	URLs string `json:"urls"` // Newline-separated URLs
}

// AddTorrentTrackers adds trackers to a specific torrent
func (h *TorrentsHandler) AddTorrentTrackers(w http.ResponseWriter, r *http.Request) {
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

	var req AddTrackerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.URLs == "" {
		RespondError(w, http.StatusBadRequest, "URLs are required")
		return
	}

	// Add trackers
	err = h.syncManager.AddTorrentTrackers(r.Context(), instanceID, hash, req.URLs)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to add trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to add trackers")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// RemoveTrackerRequest represents a tracker remove request
type RemoveTrackerRequest struct {
	URLs string `json:"urls"` // Newline-separated URLs
}

// RemoveTorrentTrackers removes trackers from a specific torrent
func (h *TorrentsHandler) RemoveTorrentTrackers(w http.ResponseWriter, r *http.Request) {
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

	var req RemoveTrackerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.URLs == "" {
		RespondError(w, http.StatusBadRequest, "URLs are required")
		return
	}

	// Remove trackers
	err = h.syncManager.RemoveTorrentTrackers(r.Context(), instanceID, hash, req.URLs)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to remove trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to remove trackers")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// RenameTorrent updates the display name for a torrent
func (h *TorrentsHandler) RenameTorrent(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		RespondError(w, http.StatusBadRequest, "Torrent name cannot be empty")
		return
	}

	if err := h.syncManager.RenameTorrent(r.Context(), instanceID, hash, req.Name); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to rename torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to rename torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"message": "Torrent renamed successfully"})
}

// RenameTorrentFile renames a file within a torrent
func (h *TorrentsHandler) RenameTorrentFile(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if strings.TrimSpace(req.OldPath) == "" || strings.TrimSpace(req.NewPath) == "" {
		RespondError(w, http.StatusBadRequest, "Both oldPath and newPath are required")
		return
	}

	if err := h.syncManager.RenameTorrentFile(r.Context(), instanceID, hash, req.OldPath, req.NewPath); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Str("oldPath", req.OldPath).Str("newPath", req.NewPath).Msg("Failed to rename torrent file")
		RespondError(w, http.StatusInternalServerError, "Failed to rename torrent file")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"message": "Torrent file renamed successfully"})
}

// RenameTorrentFolder renames a folder within a torrent
func (h *TorrentsHandler) RenameTorrentFolder(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if strings.TrimSpace(req.OldPath) == "" || strings.TrimSpace(req.NewPath) == "" {
		RespondError(w, http.StatusBadRequest, "Both oldPath and newPath are required")
		return
	}

	if err := h.syncManager.RenameTorrentFolder(r.Context(), instanceID, hash, req.OldPath, req.NewPath); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Str("oldPath", req.OldPath).Str("newPath", req.NewPath).Msg("Failed to rename torrent folder")
		RespondError(w, http.StatusInternalServerError, "Failed to rename torrent folder")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"message": "Torrent folder renamed successfully"})
}

// GetTorrentFiles returns files information for a specific torrent
func (h *TorrentsHandler) GetTorrentPeers(w http.ResponseWriter, r *http.Request) {
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

	// Get peers (backend handles incremental updates internally)
	peers, err := h.syncManager.GetTorrentPeers(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent peers")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent peers")
		return
	}

	// Create sorted peers array
	sortedPeers := make([]SortedPeer, 0, len(peers.Peers))
	for key, peer := range peers.Peers {
		sortedPeers = append(sortedPeers, SortedPeer{
			Key:         key,
			TorrentPeer: peer,
		})
	}

	// Sort peers: seeders first (progress = 1.0), then by download speed, then upload speed
	sort.Slice(sortedPeers, func(i, j int) bool {
		// Seeders (100% progress) always come first
		iIsSeeder := sortedPeers[i].Progress == 1.0
		jIsSeeder := sortedPeers[j].Progress == 1.0

		if iIsSeeder != jIsSeeder {
			return iIsSeeder // Seeders first
		}

		// Then sort by progress (higher progress first)
		if sortedPeers[i].Progress != sortedPeers[j].Progress {
			return sortedPeers[i].Progress > sortedPeers[j].Progress
		}

		// Then by download speed (active downloading peers)
		if sortedPeers[i].DownSpeed != sortedPeers[j].DownSpeed {
			return sortedPeers[i].DownSpeed > sortedPeers[j].DownSpeed
		}

		// Then by upload speed
		if sortedPeers[i].UpSpeed != sortedPeers[j].UpSpeed {
			return sortedPeers[i].UpSpeed > sortedPeers[j].UpSpeed
		}

		// Finally by IP for stable sorting
		return sortedPeers[i].IP < sortedPeers[j].IP
	})

	// Create response with sorted peers
	response := &SortedPeersResponse{
		TorrentPeersResponse: peers,
		SortedPeers:          sortedPeers,
	}

	// Debug logging
	log.Trace().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Int("peerCount", len(sortedPeers)).
		Msg("Torrent peers response with sorted peers")

	RespondJSON(w, http.StatusOK, response)
}

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

// ExportTorrent streams the .torrent file for a specific torrent
func (h *TorrentsHandler) ExportTorrent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := strings.TrimSpace(chi.URLParam(r, "hash"))
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	data, suggestedName, trackerDomain, err := h.syncManager.ExportTorrent(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to export torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to export torrent")
		return
	}

	filename := sanitizeTorrentExportFilename(suggestedName, hash, trackerDomain, hash)

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		log.Warn().Str("filename", filename).Msg("Falling back to quoted Content-Disposition header")
		disposition = fmt.Sprintf("attachment; filename=%q", filename)
	}

	if len(data) > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to write torrent export response")
	}
}

// AddPeers adds peers to torrents
func (h *TorrentsHandler) AddPeers(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse request body
	var req struct {
		Hashes []string `json:"hashes"`
		Peers  []string `json:"peers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Hashes) == 0 || len(req.Peers) == 0 {
		RespondError(w, http.StatusBadRequest, "Hashes and peers are required")
		return
	}

	// Add peers
	err = h.syncManager.AddPeersToTorrents(r.Context(), instanceID, req.Hashes, req.Peers)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to add peers to torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to add peers")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// BanPeers bans peers permanently
func (h *TorrentsHandler) BanPeers(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse request body
	var req struct {
		Peers []string `json:"peers"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Peers) == 0 {
		RespondError(w, http.StatusBadRequest, "Peers are required")
		return
	}

	// Ban peers
	err = h.syncManager.BanPeers(r.Context(), instanceID, req.Peers)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to ban peers")
		RespondError(w, http.StatusInternalServerError, "Failed to ban peers")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

const (
	// keep filenames comfortably under the 255 byte limit common across modern filesystems
	maxExportFilenameBytes = 240
	shortTorrentHashLength = 5
	torrentFileExtension   = ".torrent"
)

// truncateUTF8 preserves valid rune boundaries while capping the returned string to maxBytes.
func truncateUTF8(input string, maxBytes int) string {
	if len(input) <= maxBytes {
		return input
	}

	cut := 0
	for cut < len(input) {
		_, size := utf8.DecodeRuneInString(input[cut:])
		if size <= 0 || cut+size > maxBytes {
			break
		}
		cut += size
	}

	return input[:cut]
}

func sanitizeTorrentExportFilename(name, fallback, trackerDomain, hash string) string {
	trimmed := strings.TrimSpace(name)
	alt := strings.TrimSpace(fallback)

	if trimmed == "" {
		trimmed = alt
	}

	if trimmed == "" {
		trimmed = "torrent"
	}

	sanitized := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		case 0:
			return -1
		}

		if r < 32 || r == 127 {
			return -1
		}

		return r
	}, trimmed)

	sanitized = strings.Trim(sanitized, " .")
	if sanitized == "" {
		sanitized = "torrent"
	}

	trackerTag := trackerTagFromDomain(trackerDomain)
	shortHash := shortTorrentHash(hash)

	prefix := ""
	if trackerTag != "" {
		prefix = "[" + trackerTag + "] "
	}

	suffix := ""
	if shortHash != "" {
		suffix = " - " + shortHash
	}

	coreBudget := max(maxExportFilenameBytes-len(torrentFileExtension), 0)

	allowedBytes := coreBudget - len(prefix) - len(suffix)
	if allowedBytes < 1 {
		prefix = ""
		allowedBytes = coreBudget - len(suffix)
		if allowedBytes < 1 {
			suffix = ""
			allowedBytes = coreBudget
			if allowedBytes < 1 {
				allowedBytes = 0
			}
		}
	}

	sanitized = truncateUTF8(sanitized, allowedBytes)
	if sanitized == "" {
		sanitized = "torrent"
	}

	filename := prefix + sanitized + suffix
	if !strings.HasSuffix(strings.ToLower(filename), torrentFileExtension) {
		filename += torrentFileExtension
	}

	return filename
}

func trackerTagFromDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}

	domain = strings.TrimSuffix(domain, ".")
	domain = strings.TrimPrefix(domain, "www.")

	base := domain
	if registrable, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		base = registrable
	}

	if idx := strings.IndexRune(base, '.'); idx != -1 {
		base = base[:idx]
	}

	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}

	// Domain labels should already be safe, but guard against unexpected characters
	var builder strings.Builder
	for _, r := range base {
		switch {
		case unicode.IsLetter(r):
			builder.WriteRune(unicode.ToLower(r))
		case unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '-':
			builder.WriteRune(r)
		}
	}

	tag := strings.Trim(builder.String(), "-")
	return tag
}

func shortTorrentHash(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(shortTorrentHashLength)
	for i := 0; i < len(hash) && builder.Len() < shortTorrentHashLength; i++ {
		c := hash[i]
		switch {
		case '0' <= c && c <= '9':
			builder.WriteByte(c)
		case 'a' <= c && c <= 'f':
			builder.WriteByte(c)
		case 'A' <= c && c <= 'F':
			builder.WriteByte(c + ('a' - 'A'))
		}
	}

	return builder.String()
}

// CreateTorrent creates a new torrent file from source path
func (h *TorrentsHandler) CreateTorrent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req qbt.TorrentCreationParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.SourcePath == "" {
		RespondError(w, http.StatusBadRequest, "sourcePath is required")
		return
	}

	resp, err := h.syncManager.CreateTorrent(r.Context(), instanceID, req)
	if err != nil {
		if errors.Is(err, qbt.ErrTorrentCreationTooManyActiveTasks) {
			RespondError(w, http.StatusConflict, "Too many active torrent creation tasks")
			return
		}
		if errors.Is(err, qbt.ErrUnsupportedVersion) {
			RespondError(w, http.StatusBadRequest, "Torrent creation requires qBittorrent v5.0.0 or later. Please upgrade your qBittorrent instance.")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to create torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to create torrent")
		return
	}

	RespondJSON(w, http.StatusCreated, resp)
}

// GetTorrentCreationStatus gets status of torrent creation tasks
func (h *TorrentsHandler) GetTorrentCreationStatus(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	taskID := r.URL.Query().Get("taskID")

	tasks, err := h.syncManager.GetTorrentCreationStatus(r.Context(), instanceID, taskID)
	if err != nil {
		if errors.Is(err, qbt.ErrTorrentCreationTaskNotFound) {
			RespondError(w, http.StatusNotFound, "Torrent creation task not found")
			return
		}
		if errors.Is(err, qbt.ErrUnsupportedVersion) {
			RespondError(w, http.StatusBadRequest, "Torrent creation requires qBittorrent v5.0.0 or later. Please upgrade your qBittorrent instance.")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrent creation status")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent creation status")
		return
	}

	RespondJSON(w, http.StatusOK, tasks)
}

// GetActiveTaskCount returns the number of active torrent creation tasks
// This is a lightweight endpoint optimized for polling the badge count
func (h *TorrentsHandler) GetActiveTaskCount(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	count := h.syncManager.GetActiveTaskCount(r.Context(), instanceID)
	RespondJSON(w, http.StatusOK, map[string]int{"count": count})
}

// DownloadTorrentCreationFile downloads the torrent file for a completed task
func (h *TorrentsHandler) DownloadTorrentCreationFile(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondError(w, http.StatusBadRequest, "Task ID is required")
		return
	}

	data, err := h.syncManager.GetTorrentCreationFile(r.Context(), instanceID, taskID)
	if err != nil {
		if errors.Is(err, qbt.ErrTorrentCreationTaskNotFound) {
			RespondError(w, http.StatusNotFound, "Torrent creation task not found")
			return
		}
		if errors.Is(err, qbt.ErrTorrentCreationUnfinished) {
			RespondError(w, http.StatusConflict, "Torrent creation is still in progress")
			return
		}
		if errors.Is(err, qbt.ErrTorrentCreationFailed) {
			RespondError(w, http.StatusConflict, "Torrent creation failed")
			return
		}
		if errors.Is(err, qbt.ErrUnsupportedVersion) {
			RespondError(w, http.StatusBadRequest, "Torrent creation requires qBittorrent v5.0.0 or later. Please upgrade your qBittorrent instance.")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("taskID", taskID).Msg("Failed to download torrent file")
		RespondError(w, http.StatusInternalServerError, "Failed to download torrent file")
		return
	}

	filename := fmt.Sprintf("%s.torrent", taskID)
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("taskID", taskID).Msg("Failed to write torrent file response")
	}
}

// DeleteTorrentCreationTask deletes a torrent creation task
func (h *TorrentsHandler) DeleteTorrentCreationTask(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondError(w, http.StatusBadRequest, "Task ID is required")
		return
	}

	err = h.syncManager.DeleteTorrentCreationTask(r.Context(), instanceID, taskID)
	if err != nil {
		if errors.Is(err, qbt.ErrTorrentCreationTaskNotFound) {
			RespondError(w, http.StatusNotFound, "Torrent creation task not found")
			return
		}
		if errors.Is(err, qbt.ErrUnsupportedVersion) {
			RespondError(w, http.StatusBadRequest, "Torrent creation requires qBittorrent v5.0.0 or later. Please upgrade your qBittorrent instance.")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("taskID", taskID).Msg("Failed to delete torrent creation task")
		RespondError(w, http.StatusInternalServerError, "Failed to delete torrent creation task")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"message": "Torrent creation task deleted successfully"})
}

// GetEconomyAnalysis returns the complete economy analysis for an instance
func (h *TorrentsHandler) GetEconomyAnalysis(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse pagination parameters
	page := 1
	pageSize := 25 // Increased default for better performance

	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if pageSizeStr := r.URL.Query().Get("pageSize"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	// Parse sorting parameters
	sortField := r.URL.Query().Get("sort")
	sortDesc := false
	if sortOrder := r.URL.Query().Get("order"); sortOrder == "desc" {
		sortDesc = true
	}

	// Parse filters
	var filters qbittorrent.FilterOptions
	if filtersStr := r.URL.Query().Get("filters"); filtersStr != "" {
		if err := json.Unmarshal([]byte(filtersStr), &filters); err != nil {
			log.Warn().Err(err).Msg("Failed to parse filters, ignoring")
		}
	}

	// Get economy analysis
	analysis, err := h.syncManager.GetEconomyAnalysisWithPaginationAndSorting(r.Context(), instanceID, page, pageSize, sortField, sortDesc, filters)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get economy analysis")
		RespondError(w, http.StatusInternalServerError, "Failed to get economy analysis")
		return
	}

	RespondJSON(w, http.StatusOK, analysis)
}

// GetEconomyStats returns aggregated economy statistics for an instance
func (h *TorrentsHandler) GetEconomyStats(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get economy stats
	stats, err := h.syncManager.GetEconomyStats(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get economy stats")
		RespondError(w, http.StatusInternalServerError, "Failed to get economy stats")
		return
	}

	RespondJSON(w, http.StatusOK, stats)
}

// GetTopValuableTorrents returns the most valuable torrents by economy score
func (h *TorrentsHandler) GetTopValuableTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse limit parameter
	limit := 20 // Default limit
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	// Get top valuable torrents
	torrents, err := h.syncManager.GetTopValuableTorrents(r.Context(), instanceID, limit)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get top valuable torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get top valuable torrents")
		return
	}

	RespondJSON(w, http.StatusOK, torrents)
}
