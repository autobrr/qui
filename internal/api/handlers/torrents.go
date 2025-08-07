package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type TorrentsHandler struct {
	syncManager *qbittorrent.SyncManager
}

func NewTorrentsHandler(syncManager *qbittorrent.SyncManager) *TorrentsHandler {
	return &TorrentsHandler{
		syncManager: syncManager,
	}
}

// ListTorrents returns paginated torrents for an instance
func (h *TorrentsHandler) ListTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse query parameters
	limit := 50
	page := 0
	sort := "addedOn"
	order := "desc"
	search := ""

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
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
		Msg("Torrent list request parameters")

	// Calculate offset from page
	offset := page * limit

	// Get torrents with search, sorting and filters
	response, err := h.syncManager.GetTorrentsWithFilters(r.Context(), instanceID, limit, offset, sort, order, search, filters)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	RespondJSON(w, http.StatusOK, response)
}

// SyncTorrents returns sync updates for an instance
func (h *TorrentsHandler) SyncTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get sync updates
	mainData, err := h.syncManager.GetUpdates(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to sync torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to sync torrents")
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

	// Get torrent file
	file, _, err := r.FormFile("torrent")
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Torrent file is required")
		return
	}
	defer file.Close()

	// Read file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Failed to read torrent file")
		return
	}

	// Parse options from form
	options := make(map[string]string)

	if category := r.FormValue("category"); category != "" {
		options["category"] = category
	}

	if tags := r.FormValue("tags"); tags != "" {
		options["tags"] = tags
	}

	if paused := r.FormValue("paused"); paused == "true" {
		options["paused"] = "true"
	}

	if skipChecking := r.FormValue("skip_checking"); skipChecking == "true" {
		options["skip_checking"] = "true"
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

	// Add torrent
	if err := h.syncManager.AddTorrent(r.Context(), instanceID, fileContent, options); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to add torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to add torrent")
		return
	}

	// Immediately invalidate cache - the next request will get fresh data from qBittorrent
	h.syncManager.InvalidateCache(instanceID)
	log.Debug().Int("instanceID", instanceID).Msg("Cache invalidated after adding torrent")

	RespondJSON(w, http.StatusCreated, map[string]string{
		"message": "Torrent added successfully",
	})
}

// BulkActionRequest represents a bulk action request
type BulkActionRequest struct {
	Hashes      []string `json:"hashes"`
	Action      string   `json:"action"`
	DeleteFiles bool     `json:"deleteFiles,omitempty"` // For delete action
	Tags        string   `json:"tags,omitempty"`        // For tag operations (comma-separated)
	Category    string   `json:"category,omitempty"`    // For category operations
	Enable      bool     `json:"enable,omitempty"`      // For toggleAutoTMM action
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

	// Validate input
	if len(req.Hashes) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrents selected")
		return
	}

	validActions := []string{
		"pause", "resume", "delete", "deleteWithFiles",
		"recheck", "reannounce", "increasePriority", "decreasePriority",
		"topPriority", "bottomPriority", "addTags", "removeTags", "setTags", "setCategory",
		"toggleAutoTMM",
	}

	valid := false
	for _, action := range validActions {
		if req.Action == action {
			valid = true
			break
		}
	}

	if !valid {
		RespondError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	// Perform bulk action based on type
	switch req.Action {
	case "addTags":
		if req.Tags == "" {
			RespondError(w, http.StatusBadRequest, "Tags parameter is required for addTags action")
			return
		}
		err = h.syncManager.AddTags(r.Context(), instanceID, req.Hashes, req.Tags)
	case "removeTags":
		if req.Tags == "" {
			RespondError(w, http.StatusBadRequest, "Tags parameter is required for removeTags action")
			return
		}
		err = h.syncManager.RemoveTags(r.Context(), instanceID, req.Hashes, req.Tags)
	case "setTags":
		if req.Tags == "" {
			RespondError(w, http.StatusBadRequest, "Tags parameter is required for setTags action")
			return
		}
		err = h.syncManager.SetTags(r.Context(), instanceID, req.Hashes, req.Tags)
	case "setCategory":
		err = h.syncManager.SetCategory(r.Context(), instanceID, req.Hashes, req.Category)
	case "toggleAutoTMM":
		err = h.syncManager.SetAutoTMM(r.Context(), instanceID, req.Hashes, req.Enable)
	case "delete":
		// Handle delete with deleteFiles parameter
		action := req.Action
		if req.DeleteFiles {
			action = "deleteWithFiles"
		}
		err = h.syncManager.BulkAction(r.Context(), instanceID, req.Hashes, action)
	default:
		// Handle other standard actions
		err = h.syncManager.BulkAction(r.Context(), instanceID, req.Hashes, req.Action)
	}

	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("action", req.Action).Msg("Failed to perform bulk action")
		RespondError(w, http.StatusInternalServerError, "Failed to perform bulk action")
		return
	}

	// Immediately invalidate cache - the next request will get fresh data from qBittorrent
	h.syncManager.InvalidateCache(instanceID)
	log.Debug().Int("instanceID", instanceID).Str("action", req.Action).Msg("Cache invalidated after bulk action")

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

// GetFilteredTorrents returns filtered torrents
func (h *TorrentsHandler) GetFilteredTorrents(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Build filter options from query parameters
	opts := qbt.TorrentFilterOptions{}

	// Filter (convert string to TorrentFilter type)
	if filter := r.URL.Query().Get("filter"); filter != "" {
		opts.Filter = qbt.TorrentFilter(filter)
	}

	// Category
	if category := r.URL.Query().Get("category"); category != "" {
		opts.Category = category
	}

	// Tag
	if tag := r.URL.Query().Get("tag"); tag != "" {
		opts.Tag = tag
	}

	// Sort
	if sort := r.URL.Query().Get("sort"); sort != "" {
		opts.Sort = sort
	}

	// Reverse
	if reverse := r.URL.Query().Get("reverse"); reverse == "true" {
		opts.Reverse = true
	}

	// Limit
	if l := r.URL.Query().Get("limit"); l != "" {
		if limit, err := strconv.Atoi(l); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}

	// Offset
	if o := r.URL.Query().Get("offset"); o != "" {
		if offset, err := strconv.Atoi(o); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}

	// Get filtered torrents
	response, err := h.syncManager.GetFilteredTorrents(r.Context(), instanceID, opts)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get filtered torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get filtered torrents")
		return
	}

	RespondJSON(w, http.StatusOK, response)
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

// GetTorrentWebSeeds returns web seeds for a specific torrent
func (h *TorrentsHandler) GetTorrentWebSeeds(w http.ResponseWriter, r *http.Request) {
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

	// Get web seeds
	webSeeds, err := h.syncManager.GetTorrentWebSeeds(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent web seeds")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent web seeds")
		return
	}

	RespondJSON(w, http.StatusOK, webSeeds)
}

// GetTorrentCounts returns torrent counts for filter sidebar
func (h *TorrentsHandler) GetTorrentCounts(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get torrent counts
	counts, err := h.syncManager.GetTorrentCounts(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrent counts")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent counts")
		return
	}

	RespondJSON(w, http.StatusOK, counts)
}
