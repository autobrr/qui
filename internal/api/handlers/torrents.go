// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/redact"
	"github.com/autobrr/qui/pkg/torrentname"
)

// torrentAdder is the interface for adding torrents (used for testing)
type torrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error
	AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error
	GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error)
}

// torrentDownloader is the interface for downloading torrents from indexers (used for testing)
type torrentDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

type torrentContentResolver interface {
	GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error)
	GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	GetTorrents(ctx context.Context, instanceID int, options qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
}

type TorrentsHandler struct {
	syncManager    *qbittorrent.SyncManager
	jackettService *jackett.Service
	instanceStore  *models.InstanceStore
	// Testing interfaces - when set, these are used instead of the concrete types
	torrentAdder      torrentAdder
	torrentDownloader torrentDownloader
	contentResolver   torrentContentResolver
}

// truncateExpr truncates long filter expressions for cleaner logging
func truncateExpr(expr string, maxLen int) string {
	if len(expr) <= maxLen {
		return expr
	}
	return expr[:maxLen-3] + "..."
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

func NewTorrentsHandler(syncManager *qbittorrent.SyncManager, jackettService *jackett.Service, instanceStore *models.InstanceStore) *TorrentsHandler {
	return &TorrentsHandler{
		syncManager:    syncManager,
		jackettService: jackettService,
		instanceStore:  instanceStore,
	}
}

// NewTorrentsHandlerForTesting creates a TorrentsHandler with mock interfaces for testing
func NewTorrentsHandlerForTesting(adder torrentAdder, downloader torrentDownloader) *TorrentsHandler {
	return &TorrentsHandler{
		torrentAdder:      adder,
		torrentDownloader: downloader,
	}
}

// addTorrent wraps the torrent addition to support both production and test modes
func (h *TorrentsHandler) addTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) error {
	if h.torrentAdder != nil {
		return h.torrentAdder.AddTorrent(ctx, instanceID, fileContent, options)
	}
	return h.syncManager.AddTorrent(ctx, instanceID, fileContent, options)
}

// addTorrentFromURLs wraps URL-based torrent addition to support both production and test modes
func (h *TorrentsHandler) addTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) error {
	if h.torrentAdder != nil {
		return h.torrentAdder.AddTorrentFromURLs(ctx, instanceID, urls, options)
	}
	return h.syncManager.AddTorrentFromURLs(ctx, instanceID, urls, options)
}

// getAppPreferences wraps preferences retrieval to support both production and test modes
func (h *TorrentsHandler) getAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error) {
	if h.torrentAdder != nil {
		return h.torrentAdder.GetAppPreferences(ctx, instanceID)
	}
	return h.syncManager.GetAppPreferences(ctx, instanceID)
}

// downloadTorrent wraps torrent download to support both production and test modes
func (h *TorrentsHandler) downloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
	if h.torrentDownloader != nil {
		return h.torrentDownloader.DownloadTorrent(ctx, req)
	}
	return h.jackettService.DownloadTorrent(ctx, req)
}

// hasJackettService checks if jackett service is available (either real or mock)
func (h *TorrentsHandler) hasJackettService() bool {
	return h.jackettService != nil || h.torrentDownloader != nil
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
	sort := "added_on"
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

	// Debug logging with truncated expression to prevent log bloat
	logEvent := log.Debug().
		Int("instanceID", instanceID).
		Str("sort", sort).
		Str("order", order).
		Int("page", page).
		Int("limit", limit).
		Str("search", search).
		Str("sessionID", sessionID)

	// Log filters but truncate long expressions
	if filters.Expr != "" {
		logEvent = logEvent.Str("expr", truncateExpr(filters.Expr, 150))
	}
	if len(filters.Status) > 0 {
		logEvent = logEvent.Strs("status", filters.Status)
	}
	if len(filters.Categories) > 0 {
		logEvent = logEvent.Strs("categories", filters.Categories)
	}
	if len(filters.Tags) > 0 {
		logEvent = logEvent.Strs("tags", filters.Tags)
	}

	logEvent.Msg("Torrent list request parameters")

	// Calculate offset from page
	offset := page * limit

	// Get torrents with search, sorting and filters
	// The sync manager will handle stale-while-revalidate internally
	response, err := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, limit, offset, sort, order, search, filters)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:list") {
			return
		}
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

// GetTorrentField returns field values for torrents matching the given filters.
// Used for select all copy operations (Copy Name, Copy Hash, Copy Full Path).
func (h *TorrentsHandler) GetTorrentField(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Field          string                    `json:"field"`
		Sort           string                    `json:"sort"`
		Order          string                    `json:"order"`
		Search         string                    `json:"search"`
		Filters        qbittorrent.FilterOptions `json:"filters"`
		InstanceIDs    []int                     `json:"instanceIds"`
		ExcludeHashes  []string                  `json:"excludeHashes"`
		ExcludeTargets []BulkActionTarget        `json:"excludeTargets"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	normalizedInstanceIDs, instanceIDsErr := normalizeInstanceIDs(req.InstanceIDs)
	if instanceIDsErr != nil {
		RespondError(w, http.StatusBadRequest, instanceIDsErr.Error())
		return
	}
	req.InstanceIDs = normalizedInstanceIDs

	if len(req.ExcludeHashes) > 512 {
		RespondError(w, http.StatusBadRequest, "Too many exclude hashes provided (maximum 512)")
		return
	}

	if req.Field != "name" && req.Field != "hash" && req.Field != "full_path" {
		RespondError(w, http.StatusBadRequest, "Invalid field: must be name, hash, or full_path")
		return
	}

	if req.Sort == "" {
		req.Sort = "added_on"
	}
	if req.Order == "" {
		req.Order = "desc"
	}

	if instanceID == allInstancesID {
		response, err := h.syncManager.GetCrossInstanceTorrentsWithFilters(
			r.Context(),
			0,
			0,
			req.Sort,
			req.Order,
			req.Search,
			req.Filters,
			req.InstanceIDs,
		)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Str("field", req.Field).Msg("Failed to get cross-instance torrent field")
			RespondError(w, http.StatusInternalServerError, "Failed to get torrent field")
			return
		}

		excludeHashes := buildExcludeHashSet(req.ExcludeHashes)
		excludeTargets := buildExcludeTargetSet(req.ExcludeTargets)
		values := make([]string, 0, len(response.CrossInstanceTorrents))
		for _, torrent := range response.CrossInstanceTorrents {
			normalized := normalizeHashValue(torrent.Hash)
			if normalized == "" {
				continue
			}
			if excludeHashes != nil {
				if _, skip := excludeHashes[normalized]; skip {
					continue
				}
			}
			if excludeTargets != nil {
				key := fmt.Sprintf("%d:%s", torrent.InstanceID, normalized)
				if _, skip := excludeTargets[key]; skip {
					continue
				}
			}

			var value string
			switch req.Field {
			case "name":
				value = strings.TrimSpace(torrent.Name)
			case "hash":
				value = preferredCrossInstanceHashValue(torrent)
			case "full_path":
				value = fullPathValue(torrent.SavePath, torrent.Name)
			}

			if value != "" {
				values = append(values, value)
			}
		}

		RespondJSON(w, http.StatusOK, &qbittorrent.TorrentFieldResponse{
			Values: values,
			Total:  len(values),
		})
		return
	}

	fieldResponse, err := h.syncManager.GetTorrentField(r.Context(), instanceID, req.Field, req.Sort, req.Order, req.Search, req.Filters, req.ExcludeHashes)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:metadata") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("field", req.Field).Msg("Failed to get torrent field")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent field")
		return
	}

	RespondJSON(w, http.StatusOK, fieldResponse)
}

// CheckDuplicates validates if any of the provided hashes already exist in qBittorrent.
func (h *TorrentsHandler) CheckDuplicates(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req struct {
		Hashes []string `json:"hashes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Hashes) == 0 {
		RespondJSON(w, http.StatusOK, struct {
			Duplicates []qbittorrent.DuplicateTorrentMatch `json:"duplicates"`
		}{Duplicates: []qbittorrent.DuplicateTorrentMatch{}})
		return
	}

	if len(req.Hashes) > 512 {
		RespondError(w, http.StatusBadRequest, "Too many hashes provided (maximum 512)")
		return
	}

	syncManager, err := h.syncManager.GetQBittorrentSyncManager(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:checkDuplicates") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get qBittorrent sync manager")
		RespondError(w, http.StatusInternalServerError, "Failed to check duplicate torrents")
		return
	}

	torrents := syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: req.Hashes})

	matches := make([]qbittorrent.DuplicateTorrentMatch, len(torrents))
	for i, torrent := range torrents {
		matches[i] = qbittorrent.DuplicateTorrentMatch{
			Hash:          torrent.Hash,
			InfohashV1:    strings.TrimSpace(torrent.InfohashV1),
			InfohashV2:    strings.TrimSpace(torrent.InfohashV2),
			Name:          torrent.Name,
			MatchedHashes: []string{torrent.Hash},
		}
	}

	RespondJSON(w, http.StatusOK, struct {
		Duplicates []qbittorrent.DuplicateTorrentMatch `json:"duplicates"`
	}{Duplicates: matches})
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

	// Track file processing failures for response
	type fileReadFailure struct {
		filename string
		err      string
	}
	var fileReadFailures []fileReadFailure

	// Check for torrent files (multiple files supported)
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		fileHeaders := r.MultipartForm.File["torrent"]
		if len(fileHeaders) > 0 {
			for _, fileHeader := range fileHeaders {
				file, err := fileHeader.Open()
				if err != nil {
					log.Warn().Err(err).Str("filename", fileHeader.Filename).Msg("Failed to open torrent file")
					fileReadFailures = append(fileReadFailures, fileReadFailure{filename: fileHeader.Filename, err: "Failed to open file"})
					continue
				}
				defer file.Close()

				fileContent, err := io.ReadAll(file)
				if err != nil {
					log.Warn().Err(err).Str("filename", fileHeader.Filename).Msg("Failed to read torrent file")
					fileReadFailures = append(fileReadFailures, fileReadFailure{filename: fileHeader.Filename, err: "Failed to read file"})
					continue
				}
				torrentFiles = append(torrentFiles, fileContent)
			}
		}
	}

	// Check for URLs/magnet links if no files
	var indexerID int
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

		// Parse indexer_id if provided (for downloading torrent from indexer)
		if indexerIDStr := r.FormValue("indexer_id"); indexerIDStr != "" {
			var err error
			indexerID, err = strconv.Atoi(indexerIDStr)
			if err != nil {
				log.Error().Err(err).Str("indexer_id", indexerIDStr).Msg("Invalid indexer_id provided")
				RespondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid indexer_id: %q is not a valid integer", indexerIDStr))
				return
			}
			if indexerID <= 0 {
				log.Error().Int("indexer_id", indexerID).Msg("Invalid indexer_id: must be positive")
				RespondError(w, http.StatusBadRequest, "Invalid indexer_id: must be a positive integer")
				return
			}
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
		prefs, err := h.getAppPreferences(ctx, instanceID)
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

	// useDownloadPath and downloadPath are not officially documented by the qBittorrent API, but are defined here:
	// https://github.com/qbittorrent/qBittorrent/blob/f68bc3fef9a64e2fa81225c4661b713a10017dee/src/webui/api/torrentscontroller.cpp#L1019-L1020
	if useDownloadPath := r.FormValue("useDownloadPath"); useDownloadPath != "" {
		options["useDownloadPath"] = useDownloadPath
	}

	if downloadPath := r.FormValue("downloadPath"); downloadPath != "" {
		options["downloadPath"] = downloadPath
	}

	// Handle autoTMM explicitly if provided
	if autoTMM := r.FormValue("autoTMM"); autoTMM != "" {
		options["autoTMM"] = autoTMM
		// If autoTMM is true, remove savepath to let qBittorrent handle it
		if autoTMM == "true" {
			delete(options, "savepath")
			delete(options, "useDownloadPath")
			delete(options, "downloadPath")
		}
	}

	// Track results for multiple files/URLs
	var addedCount int
	var failedCount int
	var lastError error
	type failedURL struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	var failedURLs []failedURL
	type failedFile struct {
		Filename string `json:"filename"`
		Error    string `json:"error"`
	}
	var failedFiles []failedFile

	// Add torrent(s)
	if len(torrentFiles) > 0 {
		// Add from files
		for i, fileContent := range torrentFiles {
			// Check if context is already cancelled (timeout or client disconnect)
			if ctx.Err() != nil {
				log.Warn().Int("instanceID", instanceID).Msg("Request cancelled, stopping torrent additions")
				break
			}

			if err := h.addTorrent(ctx, instanceID, fileContent, options); err != nil {
				if respondIfInstanceDisabled(w, err, instanceID, "torrents:add") {
					return
				}
				log.Error().Err(err).Int("instanceID", instanceID).Int("fileIndex", i).Msg("Failed to add torrent file")
				failedFiles = append(failedFiles, failedFile{Filename: fmt.Sprintf("file_%d", i), Error: err.Error()})
				failedCount++
				lastError = err
			} else {
				addedCount++
			}
		}
		// Include file read failures in the count and response
		for _, f := range fileReadFailures {
			failedFiles = append(failedFiles, failedFile{Filename: f.filename, Error: f.err})
			failedCount++
		}
	} else if len(urls) > 0 {
		// Add from URLs
		// If indexer_id is provided, download torrent files from the indexer first
		// (needed for remote qBittorrent instances that can't reach the indexer)
		if indexerID > 0 {
			if !h.hasJackettService() {
				log.Error().Int("indexerID", indexerID).Int("instanceID", instanceID).
					Msg("Indexer download requested but jackett service is not available")
				RespondError(w, http.StatusServiceUnavailable,
					"Indexer service is not available. Configure an indexer or remove indexer_id to use direct URL method.")
				return
			}
			var skippedEmpty int
			for _, url := range urls {
				url = strings.TrimSpace(url)
				if url == "" {
					skippedEmpty++
					continue
				}

				// Check if context is already cancelled
				if ctx.Err() != nil {
					log.Warn().Int("instanceID", instanceID).Msg("Request cancelled, stopping torrent additions")
					break
				}

				// Magnet links can be added directly to qBittorrent
				if strings.HasPrefix(strings.ToLower(url), "magnet:") {
					if err := h.addTorrentFromURLs(ctx, instanceID, []string{url}, options); err != nil {
						if respondIfInstanceDisabled(w, err, instanceID, "torrents:addFromURLs") {
							return
						}
						log.Error().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(url)).Msg("Failed to add magnet link")
						failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
						failedCount++
						lastError = err
					} else {
						addedCount++
					}
					continue
				}

				// Download torrent file from indexer
				torrentBytes, err := h.downloadTorrent(ctx, jackett.TorrentDownloadRequest{
					IndexerID:   indexerID,
					DownloadURL: url,
				})
				if err != nil {
					var magnetErr *jackett.MagnetDownloadError
					if errors.As(err, &magnetErr) && magnetErr.MagnetURL != "" {
						magnetURL := strings.TrimSpace(magnetErr.MagnetURL)
						if err := h.addTorrentFromURLs(ctx, instanceID, []string{magnetURL}, options); err != nil {
							if respondIfInstanceDisabled(w, err, instanceID, "torrents:addFromURLs") {
								return
							}
							log.Error().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(magnetURL)).Msg("Failed to add magnet link from indexer redirect")
							failedURLs = append(failedURLs, failedURL{URL: magnetURL, Error: err.Error()})
							failedCount++
							lastError = err
						} else {
							addedCount++
						}
						continue
					}
					log.Error().Err(err).Int("indexerID", indexerID).Int("instanceID", instanceID).Str("url", redact.URLString(url)).Msg("Failed to download torrent from indexer")
					failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
					failedCount++
					lastError = err
					continue
				}

				// Add torrent from downloaded file content
				if err := h.addTorrent(ctx, instanceID, torrentBytes, options); err != nil {
					if respondIfInstanceDisabled(w, err, instanceID, "torrents:add") {
						return
					}
					log.Error().Err(err).Int("instanceID", instanceID).Int("indexerID", indexerID).Str("url", redact.URLString(url)).Msg("Failed to add downloaded torrent")
					failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
					failedCount++
					lastError = err
				} else {
					addedCount++
				}
			}
			if skippedEmpty > 0 {
				log.Debug().Int("skippedEmpty", skippedEmpty).Int("instanceID", instanceID).
					Msg("Skipped empty URLs in add torrent request")
			}
		} else {
			// No indexer_id - use URL method directly
			// (works for local qBittorrent instances or magnet links)
			if err := h.addTorrentFromURLs(ctx, instanceID, urls, options); err != nil {
				if respondIfInstanceDisabled(w, err, instanceID, "torrents:addFromURLs") {
					return
				}
				log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to add torrent from URLs")
				RespondError(w, http.StatusInternalServerError, "Failed to add torrent")
				return
			}
			addedCount = len(urls) // Assume all URLs succeeded for simplicity
		}
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

	response := map[string]any{
		"message": message,
		"added":   addedCount,
		"failed":  failedCount,
	}
	if len(failedURLs) > 0 {
		response["failedURLs"] = failedURLs
	}
	if len(failedFiles) > 0 {
		response["failedFiles"] = failedFiles
	}
	RespondJSON(w, http.StatusCreated, response)
}

// BulkActionRequest represents a bulk action request
type BulkActionRequest struct {
	Hashes                   []string                   `json:"hashes"`
	Targets                  []BulkActionTarget         `json:"targets,omitempty"` // Optional explicit instance/hash targets (used by unified view)
	Action                   string                     `json:"action"`
	DeleteFiles              bool                       `json:"deleteFiles,omitempty"`              // For delete action
	Tags                     string                     `json:"tags,omitempty"`                     // For tag operations (comma-separated)
	Category                 string                     `json:"category,omitempty"`                 // For category operations
	Enable                   bool                       `json:"enable,omitempty"`                   // For toggleAutoTMM action
	SelectAll                bool                       `json:"selectAll,omitempty"`                // When true, apply to all torrents matching filters
	InstanceIDs              []int                      `json:"instanceIds,omitempty"`              // Optional unified instance scope
	Filters                  *qbittorrent.FilterOptions `json:"filters,omitempty"`                  // Filters to apply when selectAll is true
	Search                   string                     `json:"search,omitempty"`                   // Search query when selectAll is true
	ExcludeHashes            []string                   `json:"excludeHashes,omitempty"`            // Hashes to exclude when selectAll is true
	ExcludeTargets           []BulkActionTarget         `json:"excludeTargets,omitempty"`           // Optional explicit targets to exclude when selectAll is true
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

type BulkActionTarget struct {
	InstanceID int    `json:"instanceId"`
	Hash       string `json:"hash"`
}

const allInstancesID = 0

func normalizeHashValue(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func addBulkTarget(targetsByInstance map[int][]string, seen map[int]map[string]struct{}, instanceID int, hash string) {
	if instanceID <= 0 {
		return
	}

	normalized := normalizeHashValue(hash)
	if normalized == "" {
		return
	}

	if seen[instanceID] == nil {
		seen[instanceID] = make(map[string]struct{})
	}

	if _, exists := seen[instanceID][normalized]; exists {
		return
	}

	seen[instanceID][normalized] = struct{}{}
	targetsByInstance[instanceID] = append(targetsByInstance[instanceID], strings.TrimSpace(hash))
}

func buildExcludeHashSet(excludeHashes []string) map[string]struct{} {
	if len(excludeHashes) == 0 {
		return nil
	}

	result := make(map[string]struct{}, len(excludeHashes))
	for _, hash := range excludeHashes {
		normalized := normalizeHashValue(hash)
		if normalized != "" {
			result[normalized] = struct{}{}
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func buildExcludeTargetSet(excludeTargets []BulkActionTarget) map[string]struct{} {
	if len(excludeTargets) == 0 {
		return nil
	}

	result := make(map[string]struct{}, len(excludeTargets))
	for _, target := range excludeTargets {
		normalized := normalizeHashValue(target.Hash)
		if target.InstanceID <= 0 || normalized == "" {
			continue
		}
		result[fmt.Sprintf("%d:%s", target.InstanceID, normalized)] = struct{}{}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func normalizeInstanceIDs(instanceIDs []int) ([]int, error) {
	if len(instanceIDs) == 0 {
		return nil, nil
	}

	seen := make(map[int]struct{}, len(instanceIDs))
	normalized := make([]int, 0, len(instanceIDs))

	for _, instanceID := range instanceIDs {
		if instanceID <= 0 {
			return nil, errors.New("instanceIds must contain positive instance IDs")
		}
		if _, exists := seen[instanceID]; exists {
			return nil, errors.New("instanceIds must not contain duplicates")
		}
		seen[instanceID] = struct{}{}
		normalized = append(normalized, instanceID)
	}

	slices.Sort(normalized)
	return normalized, nil
}

func parseInstanceIDsParam(raw string) ([]int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ",")
	instanceIDs := make([]int, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}

		instanceID, err := strconv.Atoi(value)
		if err != nil {
			return nil, errors.New("invalid instanceIds query parameter")
		}
		instanceIDs = append(instanceIDs, instanceID)
	}

	return normalizeInstanceIDs(instanceIDs)
}

func shouldResolveCrossInstanceHashes(instanceID int, req BulkActionRequest) bool {
	return instanceID == allInstancesID && len(req.Hashes) > 0 && len(req.Targets) == 0
}

func appendTargetsFromCrossInstanceTorrents(
	targetsByInstance map[int][]string,
	seen map[int]map[string]struct{},
	torrents []qbittorrent.CrossInstanceTorrentView,
	excludeHashes map[string]struct{},
	excludeTargets map[string]struct{},
) {
	for _, torrent := range torrents {
		normalized := normalizeHashValue(torrent.Hash)
		if normalized == "" {
			continue
		}
		if excludeHashes != nil {
			if _, skip := excludeHashes[normalized]; skip {
				continue
			}
		}
		if excludeTargets != nil {
			key := fmt.Sprintf("%d:%s", torrent.InstanceID, normalized)
			if _, skip := excludeTargets[key]; skip {
				continue
			}
		}
		addBulkTarget(targetsByInstance, seen, torrent.InstanceID, torrent.Hash)
	}
}

func preferredHashValue(torrent *qbt.Torrent) string {
	infoHashV1 := strings.TrimSpace(torrent.InfohashV1)
	if infoHashV1 != "" {
		return strings.ToUpper(infoHashV1)
	}

	hash := strings.TrimSpace(torrent.Hash)
	infoHashV2 := strings.TrimSpace(torrent.InfohashV2)
	if hash != "" && (infoHashV2 == "" || !strings.EqualFold(infoHashV2, hash)) {
		return strings.ToUpper(hash)
	}
	if infoHashV2 != "" {
		return strings.ToUpper(infoHashV2)
	}

	return ""
}

func preferredCrossInstanceHashValue(torrent qbittorrent.CrossInstanceTorrentView) string {
	if torrent.TorrentView == nil || torrent.Torrent == nil {
		return ""
	}
	return preferredHashValue(torrent.Torrent)
}

func fullPathValue(savePath, name string) string {
	normalizedSavePath := strings.ReplaceAll(strings.TrimSpace(savePath), "\\", "/")
	trimmedName := strings.TrimSpace(name)
	if normalizedSavePath == "" || trimmedName == "" {
		return ""
	}

	if strings.HasSuffix(normalizedSavePath, "/") {
		return normalizedSavePath + trimmedName
	}

	return normalizedSavePath + "/" + trimmedName
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

	// Validate input - either specific hashes/targets or selectAll mode
	if !req.SelectAll && len(req.Hashes) == 0 && len(req.Targets) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrents selected")
		return
	}

	if req.SelectAll && (len(req.Hashes) > 0 || len(req.Targets) > 0) {
		RespondError(w, http.StatusBadRequest, "Cannot specify hashes/targets together with selectAll")
		return
	}

	validActions := []string{
		"pause", "resume", "delete", "deleteWithFiles",
		"recheck", "reannounce", "increasePriority", "decreasePriority",
		"topPriority", "bottomPriority", "addTags", "removeTags", "setTags", "setCategory",
		"toggleAutoTMM", "forceStart", "setShareLimit", "setUploadLimit", "setDownloadLimit", "setLocation",
		"editTrackers", "addTrackers", "removeTrackers", "toggleSequentialDownload",
	}

	valid := slices.Contains(validActions, req.Action)

	if !valid {
		RespondError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	if validationErr := validateBulkActionRequest(req); validationErr != nil {
		RespondError(w, http.StatusBadRequest, validationErr.Error())
		return
	}

	targetsByInstance := make(map[int][]string)
	seenTargets := make(map[int]map[string]struct{})

	if req.SelectAll {
		if req.Filters == nil {
			req.Filters = &qbittorrent.FilterOptions{}
		}

		excludeHashes := buildExcludeHashSet(req.ExcludeHashes)
		excludeTargets := buildExcludeTargetSet(req.ExcludeTargets)

		if instanceID == allInstancesID {
			response, crossErr := h.syncManager.GetCrossInstanceTorrentsWithFilters(
				r.Context(),
				0,
				0,
				"added_on",
				"desc",
				req.Search,
				*req.Filters,
				req.InstanceIDs,
			)
			if crossErr != nil {
				log.Error().Err(crossErr).Msg("Failed to get cross-instance torrents for selectAll operation")
				RespondError(w, http.StatusInternalServerError, "Failed to get torrents for bulk action")
				return
			}
			appendTargetsFromCrossInstanceTorrents(
				targetsByInstance,
				seenTargets,
				response.CrossInstanceTorrents,
				excludeHashes,
				excludeTargets,
			)

			log.Debug().
				Int("instanceID", instanceID).
				Int("totalFound", len(response.CrossInstanceTorrents)).
				Int("excludedHashes", len(req.ExcludeHashes)).
				Int("excludedTargets", len(req.ExcludeTargets)).
				Int("targetCount", len(flattenTargetHashes(targetsByInstance))).
				Str("action", req.Action).
				Msg("SelectAll cross-instance bulk action")
		} else {
			// Use a very large limit to get all torrents (backend will handle this properly)
			response, listErr := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, 100000, 0, "added_on", "desc", req.Search, *req.Filters)
			if listErr != nil {
				if respondIfInstanceDisabled(w, listErr, instanceID, "torrents:selectAll") {
					return
				}
				// Record error for user visibility
				errorStore := h.syncManager.GetErrorStore()
				if recordErr := errorStore.RecordError(r.Context(), instanceID, listErr); recordErr != nil {
					log.Error().Err(recordErr).Int("instanceID", instanceID).Msg("Failed to record torrent error")
				}

				log.Error().Err(listErr).Int("instanceID", instanceID).Msg("Failed to get torrents for selectAll operation")
				RespondError(w, http.StatusInternalServerError, "Failed to get torrents for bulk action")
				return
			}

			for _, torrent := range response.Torrents {
				normalized := normalizeHashValue(torrent.Hash)
				if normalized == "" {
					continue
				}
				if excludeHashes != nil {
					if _, skip := excludeHashes[normalized]; skip {
						continue
					}
				}
				addBulkTarget(targetsByInstance, seenTargets, instanceID, torrent.Hash)
			}

			log.Debug().
				Int("instanceID", instanceID).
				Int("totalFound", len(response.Torrents)).
				Int("excluded", len(req.ExcludeHashes)).
				Int("targetCount", len(targetsByInstance[instanceID])).
				Str("action", req.Action).
				Msg("SelectAll bulk action")
		}
	} else {
		for _, target := range req.Targets {
			targetInstanceID := target.InstanceID
			if targetInstanceID <= 0 {
				if instanceID == allInstancesID {
					continue
				}
				targetInstanceID = instanceID
			}
			if instanceID != allInstancesID && targetInstanceID != instanceID {
				continue
			}
			addBulkTarget(targetsByInstance, seenTargets, targetInstanceID, target.Hash)
		}

		if len(req.Hashes) > 0 {
			// Explicit targets are authoritative in unified scope; only resolve hashes when
			// targets are not provided (backward-compatible payloads).
			if shouldResolveCrossInstanceHashes(instanceID, req) {
				requestedHashes := buildExcludeHashSet(req.Hashes)
				response, crossErr := h.syncManager.GetCrossInstanceTorrentsWithFilters(
					r.Context(),
					0,
					0,
					"",
					"",
					"",
					qbittorrent.FilterOptions{},
					req.InstanceIDs,
				)
				if crossErr != nil {
					log.Error().Err(crossErr).Msg("Failed to resolve hash targets for cross-instance bulk action")
					RespondError(w, http.StatusInternalServerError, "Failed to get torrents for bulk action")
					return
				}

				for _, torrent := range response.CrossInstanceTorrents {
					normalized := normalizeHashValue(torrent.Hash)
					if requestedHashes == nil {
						continue
					}
					if _, ok := requestedHashes[normalized]; !ok {
						continue
					}
					addBulkTarget(targetsByInstance, seenTargets, torrent.InstanceID, torrent.Hash)
				}
			} else if instanceID != allInstancesID {
				for _, hash := range req.Hashes {
					addBulkTarget(targetsByInstance, seenTargets, instanceID, hash)
				}
			}
		}
	}

	if len(targetsByInstance) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrents match the selection criteria")
		return
	}

	for targetInstanceID, hashes := range targetsByInstance {
		if len(hashes) == 0 {
			continue
		}

		actionErr := h.executeBulkActionForInstance(r.Context(), targetInstanceID, req, hashes)
		if actionErr != nil {
			if instanceID != allInstancesID {
				if respondIfInstanceDisabled(w, actionErr, targetInstanceID, "torrents:bulkAction") {
					return
				}
			}
			log.Error().
				Err(actionErr).
				Int("instanceID", targetInstanceID).
				Str("action", req.Action).
				Int("hashCount", len(hashes)).
				Msg("Failed to perform bulk action")
			RespondError(w, http.StatusInternalServerError, "Failed to perform bulk action")
			return
		}
	}

	log.Debug().
		Int("instanceID", instanceID).
		Str("action", req.Action).
		Int("instanceCount", len(targetsByInstance)).
		Int("targetCount", len(flattenTargetHashes(targetsByInstance))).
		Msg("Bulk action completed with optimistic cache update")

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Bulk action completed successfully",
	})
}

func flattenTargetHashes(targetsByInstance map[int][]string) []string {
	if len(targetsByInstance) == 0 {
		return nil
	}

	total := 0
	for _, hashes := range targetsByInstance {
		total += len(hashes)
	}

	flat := make([]string, 0, total)
	for _, hashes := range targetsByInstance {
		flat = append(flat, hashes...)
	}

	return flat
}

func validateBulkActionRequest(req BulkActionRequest) error {
	switch req.Action {
	case "addTags", "removeTags":
		if strings.TrimSpace(req.Tags) == "" {
			return errors.New("tags parameter is required")
		}
	case "setLocation":
		if strings.TrimSpace(req.Location) == "" {
			return errors.New("location parameter is required")
		}
	case "editTrackers":
		if strings.TrimSpace(req.TrackerOldURL) == "" || strings.TrimSpace(req.TrackerNewURL) == "" {
			return errors.New("both trackerOldURL and trackerNewURL are required")
		}
	case "addTrackers", "removeTrackers":
		if strings.TrimSpace(req.TrackerURLs) == "" {
			return errors.New("trackerURLs parameter is required")
		}
	}

	return nil
}

func (h *TorrentsHandler) executeBulkActionForInstance(ctx context.Context, instanceID int, req BulkActionRequest, hashes []string) error {
	switch req.Action {
	case "addTags":
		if req.Tags == "" {
			return errors.New("tags parameter is required for addTags action")
		}
		return h.syncManager.AddTags(ctx, instanceID, hashes, req.Tags)
	case "removeTags":
		if req.Tags == "" {
			return errors.New("tags parameter is required for removeTags action")
		}
		return h.syncManager.RemoveTags(ctx, instanceID, hashes, req.Tags)
	case "setTags":
		// allow empty tags to clear all tags from torrents
		return h.syncManager.SetTags(ctx, instanceID, hashes, req.Tags)
	case "setCategory":
		return h.syncManager.SetCategory(ctx, instanceID, hashes, req.Category)
	case "toggleAutoTMM":
		return h.syncManager.SetAutoTMM(ctx, instanceID, hashes, req.Enable)
	case "forceStart":
		return h.syncManager.SetForceStart(ctx, instanceID, hashes, req.Enable)
	case "setShareLimit":
		return h.syncManager.SetTorrentShareLimit(ctx, instanceID, hashes, req.RatioLimit, req.SeedingTimeLimit, req.InactiveSeedingTimeLimit)
	case "setUploadLimit":
		return h.syncManager.SetTorrentUploadLimit(ctx, instanceID, hashes, req.UploadLimit)
	case "setDownloadLimit":
		return h.syncManager.SetTorrentDownloadLimit(ctx, instanceID, hashes, req.DownloadLimit)
	case "setLocation":
		if req.Location == "" {
			return errors.New("location parameter is required for setLocation action")
		}
		return h.syncManager.SetLocation(ctx, instanceID, hashes, req.Location)
	case "editTrackers":
		if req.TrackerOldURL == "" || req.TrackerNewURL == "" {
			return errors.New("both trackerOldURL and trackerNewURL are required for editTrackers action")
		}
		return h.syncManager.BulkEditTrackers(ctx, instanceID, hashes, req.TrackerOldURL, req.TrackerNewURL)
	case "addTrackers":
		if req.TrackerURLs == "" {
			return errors.New("trackerURLs parameter is required for addTrackers action")
		}
		return h.syncManager.BulkAddTrackers(ctx, instanceID, hashes, req.TrackerURLs)
	case "removeTrackers":
		if req.TrackerURLs == "" {
			return errors.New("trackerURLs parameter is required for removeTrackers action")
		}
		return h.syncManager.BulkRemoveTrackers(ctx, instanceID, hashes, req.TrackerURLs)
	case "delete":
		action := req.Action
		if req.DeleteFiles {
			action = "deleteWithFiles"
		}
		return h.syncManager.BulkAction(ctx, instanceID, hashes, action)
	default:
		return h.syncManager.BulkAction(ctx, instanceID, hashes, req.Action)
	}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getCategories") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:createCategory") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:editCategory") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:removeCategories") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getTags") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getActiveTrackers") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:createTags") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:deleteTags") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getProperties") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getTrackers") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent trackers")
		return
	}

	RespondJSON(w, http.StatusOK, trackers)
}

// GetTorrentWebSeeds returns the web seeds (HTTP sources) for a torrent
func (h *TorrentsHandler) GetTorrentWebSeeds(w http.ResponseWriter, r *http.Request) {
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

	webseeds, err := h.syncManager.GetTorrentWebSeeds(r.Context(), instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getWebSeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent web seeds")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent web seeds")
		return
	}

	RespondJSON(w, http.StatusOK, webseeds)
}

// GetTorrentPieceStates returns the download state of each piece for a torrent.
// States: 0 = not downloaded, 1 = downloading, 2 = downloaded
//
//nolint:dupl // Handler pattern is intentionally similar to other torrent detail handlers
func (h *TorrentsHandler) GetTorrentPieceStates(w http.ResponseWriter, r *http.Request) {
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

	pieceStates, err := h.syncManager.GetTorrentPieceStates(r.Context(), instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getPieceStates") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent piece states")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent piece states")
		return
	}

	RespondJSON(w, http.StatusOK, pieceStates)
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:editTracker") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:addTrackers") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:removeTrackers") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:rename") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:renameFile") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:renameFolder") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getPeers") {
			return
		}
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

	// Optional cache bypass for callers that need the freshest file list (e.g., rename dialogs)
	refreshParam := strings.TrimSpace(r.URL.Query().Get("refresh"))
	forceRefresh := refreshParam != "" && refreshParam != "0" && !strings.EqualFold(refreshParam, "false")
	ctx := r.Context()
	if forceRefresh {
		ctx = qbittorrent.WithForceFilesRefresh(ctx)
	}

	// Get files
	files, err := h.syncManager.GetTorrentFiles(ctx, instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getFiles") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent files")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent files")
		return
	}

	if files == nil {
		RespondError(w, http.StatusNotFound, "Torrent files not found")
		return
	}

	RespondJSON(w, http.StatusOK, files)
}

// SetTorrentFilePriorityRequest represents a request to update torrent file priorities.
type SetTorrentFilePriorityRequest struct {
	Indices  []int `json:"indices"`
	Priority int   `json:"priority"`
}

// SetTorrentFilePriority updates the download priority for one or more files in a torrent.
func (h *TorrentsHandler) SetTorrentFilePriority(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if strings.TrimSpace(hash) == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	var req SetTorrentFilePriorityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Indices) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one file index must be provided")
		return
	}

	if req.Priority < 0 || req.Priority > 7 {
		RespondError(w, http.StatusBadRequest, "Priority must be between 0 and 7")
		return
	}

	if err := h.syncManager.SetTorrentFilePriority(r.Context(), instanceID, hash, req.Indices, req.Priority); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:setFilePriority") {
			return
		}
		switch {
		case errors.Is(err, qbt.ErrInvalidPriority):
			RespondError(w, http.StatusBadRequest, "Invalid priority or file indices")
		case errors.Is(err, qbt.ErrTorrentMetdataNotDownloadedYet):
			RespondError(w, http.StatusConflict, "Torrent metadata is not yet available. Try again once metadata has downloaded.")
		default:
			log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to update torrent file priority")
			RespondError(w, http.StatusInternalServerError, "Failed to update torrent file priority")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:export") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to export torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to export torrent")
		return
	}

	filename := torrentname.SanitizeExportFilename(suggestedName, hash, trackerDomain, hash)

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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:addPeers") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:banPeers") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to ban peers")
		RespondError(w, http.StatusInternalServerError, "Failed to ban peers")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"success": true})
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:create") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getCreationStatus") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:downloadCreationFile") {
			return
		}
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
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:deleteCreationTask") {
			return
		}
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

// ListCrossInstanceTorrents returns torrents from all instances matching the filter expression
func (h *TorrentsHandler) ListCrossInstanceTorrents(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	limit := 300 // Default pagination size
	page := 0
	sort := "added_on"
	order := "desc"
	search := ""

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

	instanceIDs, instanceIDsErr := parseInstanceIDsParam(r.URL.Query().Get("instanceIds"))
	if instanceIDsErr != nil {
		RespondError(w, http.StatusBadRequest, instanceIDsErr.Error())
		return
	}

	// Parse filters
	var filters qbittorrent.FilterOptions
	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			log.Warn().Err(err).Msg("Failed to parse filters, ignoring")
		}
	}

	// Debug logging with truncated expression to prevent log bloat
	logEvent := log.Debug().
		Str("sort", sort).
		Str("order", order).
		Int("page", page).
		Int("limit", limit).
		Str("search", search)
	if len(instanceIDs) > 0 {
		logEvent = logEvent.Ints("instanceIDs", instanceIDs)
	}

	// Log filters but truncate long expressions
	if filters.Expr != "" {
		logEvent = logEvent.Str("expr", truncateExpr(filters.Expr, 150))
	}
	if len(filters.Status) > 0 {
		logEvent = logEvent.Strs("status", filters.Status)
	}
	if len(filters.Categories) > 0 {
		logEvent = logEvent.Strs("categories", filters.Categories)
	}
	if len(filters.Tags) > 0 {
		logEvent = logEvent.Strs("tags", filters.Tags)
	}

	logEvent.Msg("Cross-instance torrent list request parameters")

	// Calculate offset from page
	offset := page * limit

	// Get torrents from all instances with the filter expression
	response, err := h.syncManager.GetCrossInstanceTorrentsWithFilters(r.Context(), limit, offset, sort, order, search, filters, instanceIDs)
	if err != nil {
		// Note: Cross-instance queries don't have a single instanceID, so we pass 0 for logging purposes
		if respondIfInstanceDisabled(w, err, 0, "torrents:listCrossInstance") {
			return
		}
		log.Error().Err(err).Msg("Failed to get cross-instance torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get cross-instance torrents")
		return
	}

	w.Header().Set("X-Data-Source", "fresh")
	RespondJSON(w, http.StatusOK, response)
}

func (h *TorrentsHandler) GetDirectoryContent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	dirPath := strings.TrimSpace(r.URL.Query().Get("dirPath"))
	if dirPath == "" {
		RespondError(w, http.StatusBadRequest, "Invalid directory path")
		return
	}

	withMetadata := false
	if raw := strings.TrimSpace(r.URL.Query().Get("withMetadata")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid withMetadata")
			return
		}
		withMetadata = parsed
	}

	response, err := h.syncManager.GetDirectoryContentCtx(r.Context(), instanceID, dirPath, withMetadata)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getDirectoryContent") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get directory contents")
		RespondError(w, http.StatusInternalServerError, "Failed to get directory contents")
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

// requireLocalAccess checks that the instance has local filesystem access enabled.
func (h *TorrentsHandler) requireLocalAccess(w http.ResponseWriter, r *http.Request, instanceID int) bool {
	if h.instanceStore == nil {
		log.Error().Msg("Instance store not configured")
		RespondError(w, http.StatusInternalServerError, "Instance store not configured")
		return false
	}

	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return false
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to look up instance")
		RespondError(w, http.StatusInternalServerError, "Failed to look up instance")
		return false
	}
	if instance == nil {
		RespondError(w, http.StatusNotFound, "Instance not found")
		return false
	}
	if !instance.HasLocalFilesystemAccess {
		RespondError(w, http.StatusForbidden, "Instance does not have local filesystem access enabled")
		return false
	}
	return true
}

// resolveTorrentFilePath joins basePath with relativePath and validates against
// directory traversal. Returns the cleaned absolute path or an error.
func resolveTorrentFilePath(basePath, relativePath string) (string, error) {
	full := filepath.Join(basePath, filepath.FromSlash(relativePath))
	cleanBase := filepath.Clean(basePath)
	cleanFull := filepath.Clean(full)

	rel, err := filepath.Rel(cleanBase, cleanFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}
	return cleanFull, nil
}

func appendUniqueCandidate(candidates []string, seen map[string]struct{}, candidate string) []string {
	if candidate == "" {
		return candidates
	}
	cleanCandidate := filepath.Clean(candidate)
	if !filepath.IsAbs(cleanCandidate) {
		return candidates
	}
	if _, ok := seen[cleanCandidate]; ok {
		return candidates
	}
	seen[cleanCandidate] = struct{}{}
	return append(candidates, cleanCandidate)
}

// filePathCandidates returns resolved absolute paths to try, preferring
// contentPath, then savePath, then downloadPath.
func filePathCandidates(savePath, downloadPath, contentPath, relativePath string, singleFile bool) []string {
	var candidates []string
	seen := make(map[string]struct{})
	if contentPath != "" {
		cleanContentPath := filepath.Clean(filepath.FromSlash(contentPath))
		if singleFile {
			candidates = appendUniqueCandidate(candidates, seen, cleanContentPath)
			parent := filepath.Dir(cleanContentPath)
			if p, err := resolveTorrentFilePath(parent, relativePath); err == nil {
				candidates = appendUniqueCandidate(candidates, seen, p)
			}
		} else if p, err := resolveTorrentFilePath(cleanContentPath, relativePath); err == nil {
			candidates = appendUniqueCandidate(candidates, seen, p)
		}
	}
	if savePath != "" {
		if p, err := resolveTorrentFilePath(savePath, relativePath); err == nil {
			candidates = appendUniqueCandidate(candidates, seen, p)
		}
	}
	if downloadPath != "" {
		if p, err := resolveTorrentFilePath(downloadPath, relativePath); err == nil {
			candidates = appendUniqueCandidate(candidates, seen, p)
		}
	}
	return candidates
}

// DownloadTorrentContentFile serves a single file from a torrent's content on disk.
// GET /api/instances/{instanceID}/torrents/{hash}/files/{fileIndex}/download
func (h *TorrentsHandler) DownloadTorrentContentFile(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Missing torrent hash")
		return
	}

	fileIndex, err := strconv.Atoi(chi.URLParam(r, "fileIndex"))
	if err != nil || fileIndex < 0 {
		RespondError(w, http.StatusBadRequest, "Invalid file index")
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	// Get file list and find target file by index
	var resolver torrentContentResolver
	switch {
	case h.contentResolver != nil:
		resolver = h.contentResolver
	case h.syncManager != nil:
		resolver = h.syncManager
	default:
		RespondError(w, http.StatusInternalServerError, "Download service unavailable")
		return
	}

	files, err := resolver.GetTorrentFiles(r.Context(), instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:downloadContentFile") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent files")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent files")
		return
	}
	if files == nil {
		RespondError(w, http.StatusNotFound, "Torrent files not found")
		return
	}

	var targetFileName string
	found := false
	for _, f := range *files {
		if f.Index == fileIndex {
			targetFileName = f.Name
			found = true
			break
		}
	}
	if !found {
		RespondError(w, http.StatusNotFound, "File index not found in torrent")
		return
	}

	// Get torrent properties for save/download paths
	props, err := resolver.GetTorrentProperties(r.Context(), instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:downloadContentFile") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent properties")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return
	}
	if props == nil {
		log.Error().Int("instanceID", instanceID).Str("hash", hash).Msg("Torrent properties are nil")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return
	}

	contentPath := ""
	if torrents, err := resolver.GetTorrents(r.Context(), instanceID, qbt.TorrentFilterOptions{Hashes: []string{hash}}); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent content path for fallback resolution")
	} else if len(torrents) > 0 {
		contentPath = torrents[0].ContentPath
	}

	candidates := filePathCandidates(props.SavePath, props.DownloadPath, contentPath, targetFileName, len(*files) == 1)
	if len(candidates) == 0 {
		RespondError(w, http.StatusBadRequest, "Invalid file path")
		return
	}

	// Try each candidate path until we find the file
	var file *os.File
	var info os.FileInfo
	for _, candidate := range candidates {
		// #nosec G703,G304 -- candidate is constructed from validated base paths via resolveTorrentFilePath.
		f, err := os.Open(candidate)
		if err != nil {
			continue
		}

		stat, err := f.Stat()
		if err != nil {
			_ = f.Close()
			continue
		}
		if stat.IsDir() {
			_ = f.Close()
			continue
		}

		file = f
		info = stat
		break
	}
	if file == nil {
		RespondError(w, http.StatusNotFound, "File not found on disk")
		return
	}
	defer file.Close()

	filename := filepath.Base(targetFileName)

	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		disposition = fmt.Sprintf("attachment; filename=%q", filename)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, info.ModTime(), file)
}
