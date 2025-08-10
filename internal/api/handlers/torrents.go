package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// TorrentResponse represents a torrent response with metadata
type TorrentResponse struct {
	Torrents   []qbt.Torrent           `json:"torrents"`
	Total      int                     `json:"total"`
	Stats      *TorrentStats           `json:"stats,omitempty"`
	Counts     *TorrentCounts          `json:"counts,omitempty"`
	Categories map[string]qbt.Category `json:"categories,omitempty"`
	Tags       []string                `json:"tags,omitempty"`
	HasMore    bool                    `json:"hasMore"`
	SessionID  string                  `json:"sessionId,omitempty"`
}

// TorrentStats represents aggregated torrent statistics
type TorrentStats struct {
	Total              int `json:"total"`
	Downloading        int `json:"downloading"`
	Seeding            int `json:"seeding"`
	Paused             int `json:"paused"`
	Error              int `json:"error"`
	Checking           int `json:"checking"`
	TotalDownloadSpeed int `json:"totalDownloadSpeed"`
	TotalUploadSpeed   int `json:"totalUploadSpeed"`
}

// TorrentCounts represents torrent counts by status
type TorrentCounts struct {
	All         int `json:"all"`
	Downloading int `json:"downloading"`
	Seeding     int `json:"seeding"`
	Completed   int `json:"completed"`
	Paused      int `json:"paused"`
	Active      int `json:"active"`
	Inactive    int `json:"inactive"`
	Resumed     int `json:"resumed"`
	Stalled     int `json:"stalled"`
	Error       int `json:"error"`
}

type TorrentsHandler struct {
	syncManager *qbittorrent.SyncManager
}

func NewTorrentsHandler(syncManager *qbittorrent.SyncManager) *TorrentsHandler {
	return &TorrentsHandler{
		syncManager: syncManager,
	}
}

// getClient helper method to get client for an instance
func (h *TorrentsHandler) getClient(ctx context.Context, instanceID int) (*qbt.Client, error) {
	return h.syncManager.GetClientManager().GetClient(ctx, instanceID)
}

// calculateStats calculates torrent statistics from a list of torrents
func (h *TorrentsHandler) calculateStats(torrents []qbt.Torrent) *TorrentStats {
	stats := &TorrentStats{}
	stats.Total = len(torrents)

	for _, torrent := range torrents {
		switch strings.ToLower(string(torrent.State)) {
		case "downloading", "metadl", "stalledDL", "forcedDL", "queuedDL":
			stats.Downloading++
			stats.TotalDownloadSpeed += int(torrent.DlSpeed)
		case "uploading", "stalledUP", "forcedUP", "queuedUP":
			stats.Seeding++
		case "pausedDL", "pausedUP":
			stats.Paused++
		case "error", "missingFiles":
			stats.Error++
		case "checkingDL", "checkingUP", "checkingResumeData":
			stats.Checking++
		}

		stats.TotalUploadSpeed += int(torrent.UpSpeed)
	}

	return stats
}

// calculateCounts calculates torrent counts by status
func (h *TorrentsHandler) calculateCounts(torrents []qbt.Torrent) *TorrentCounts {
	counts := &TorrentCounts{}
	counts.All = len(torrents)

	for _, torrent := range torrents {
		state := strings.ToLower(string(torrent.State))

		switch state {
		case "downloading", "metadl", "stalledDL", "forcedDL", "queuedDL":
			counts.Downloading++
		case "uploading", "stalledUP", "forcedUP", "queuedUP":
			counts.Seeding++
		case "pausedDL", "pausedUP":
			counts.Paused++
		case "error", "missingFiles":
			counts.Error++
		}

		if torrent.Progress >= 1.0 {
			counts.Completed++
		}

		if torrent.DlSpeed > 0 || torrent.UpSpeed > 0 {
			counts.Active++
		} else {
			counts.Inactive++
		}

		if strings.Contains(state, "stalled") {
			counts.Stalled++
		}

		if !strings.Contains(state, "paused") {
			counts.Resumed++
		}
	}

	return counts
}

// ListTorrents returns paginated torrents for an instance with enhanced metadata
func (h *TorrentsHandler) ListTorrents(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse query parameters
	limit := 500
	page := 0
	sort := "addedOn"
	order := "desc"
	search := ""
	sessionID := r.Header.Get("X-Session-ID")

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

	if s := r.URL.Query().Get("search"); s != "" {
		search = s
	}

	// Get client
	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	// Get all torrents directly from client
	allTorrents, err := client.GetTorrentsCtx(r.Context(), qbt.TorrentFilterOptions{})
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	// Apply search filter if provided
	filteredTorrents := allTorrents
	if search != "" {
		filteredTorrents = h.filterTorrentsBySearch(allTorrents, search)
	}

	// Calculate stats and counts
	stats := h.calculateStats(filteredTorrents)
	counts := h.calculateCounts(filteredTorrents)

	// Sort torrents
	h.sortTorrents(filteredTorrents, sort, order)

	// Apply pagination
	offset := page * limit
	var paginatedTorrents []qbt.Torrent
	if offset < len(filteredTorrents) {
		end := offset + limit
		if end > len(filteredTorrents) {
			end = len(filteredTorrents)
		}
		paginatedTorrents = filteredTorrents[offset:end]
	}

	response := &TorrentResponse{
		Torrents:  paginatedTorrents,
		Total:     len(filteredTorrents),
		Stats:     stats,
		Counts:    counts,
		HasMore:   offset+limit < len(filteredTorrents),
		SessionID: sessionID,
	}

	RespondJSON(w, http.StatusOK, response)
}

// SyncTorrents returns sync data
func (h *TorrentsHandler) SyncTorrents(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Sync to get latest data using our sync manager
	syncData, err := h.syncManager.Sync(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to sync data")
		RespondError(w, http.StatusInternalServerError, "Failed to sync data")
		return
	}

	// Return the sync data
	if syncData == nil {
		RespondError(w, http.StatusInternalServerError, "No sync data available")
		return
	}

	RespondJSON(w, http.StatusOK, syncData)
}

// filterTorrentsBySearch filters torrents by search term
func (h *TorrentsHandler) filterTorrentsBySearch(torrents []qbt.Torrent, search string) []qbt.Torrent {
	if search == "" {
		return torrents
	}

	search = strings.ToLower(search)
	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		if strings.Contains(strings.ToLower(torrent.Name), search) {
			filtered = append(filtered, torrent)
		}
	}

	return filtered
}

// sortTorrents sorts torrents by the specified field and order
func (h *TorrentsHandler) sortTorrents(torrents []qbt.Torrent, sortBy, order string) {
	// Implementation would go here - for now just keep original order
	// This would include sorting by various fields like name, size, addedOn, etc.
}

// AddTorrent adds a new torrent
func (h *TorrentsHandler) AddTorrent(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get client for direct operations
	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
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

	// Check for torrent files
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
			urlsParam = strings.ReplaceAll(urlsParam, "\n", ",")
			urls = strings.Split(urlsParam, ",")
		} else {
			RespondError(w, http.StatusBadRequest, "Either torrent files or URLs are required")
			return
		}
	}

	// Parse options
	options := make(map[string]string)
	if category := r.FormValue("category"); category != "" {
		options["category"] = category
	}
	if tags := r.FormValue("tags"); tags != "" {
		options["tags"] = tags
	}
	if savePath := r.FormValue("save_path"); savePath != "" {
		options["savepath"] = savePath
	}
	if startPaused := r.FormValue("start_paused"); startPaused == "true" {
		options["paused"] = "true"
	}
	if skipChecking := r.FormValue("skip_checking"); skipChecking == "true" {
		options["skip_checking"] = "true"
	}

	// Add torrents
	if len(torrentFiles) > 0 {
		for _, fileContent := range torrentFiles {
			if err := client.AddTorrentFromFileCtx(ctx, string(fileContent), options); err != nil {
				log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to add torrent file")
				RespondError(w, http.StatusInternalServerError, "Failed to add torrent file")
				return
			}
		}
	} else {
		for _, url := range urls {
			if err := client.AddTorrentFromUrlCtx(ctx, url, options); err != nil {
				log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to add torrent URL")
				RespondError(w, http.StatusInternalServerError, "Failed to add torrent URL")
				return
			}
		}
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Torrent(s) added successfully"})
}

// BulkActionRequest represents a bulk action request
type BulkActionRequest struct {
	Hashes   []string `json:"hashes"`
	Action   string   `json:"action"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Enable   bool     `json:"enable,omitempty"`
}

// BulkAction performs bulk actions on torrents
func (h *TorrentsHandler) BulkAction(w http.ResponseWriter, r *http.Request) {
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

	if len(req.Hashes) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrent hashes provided")
		return
	}

	// Get client for direct operations
	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	// Perform the action
	switch req.Action {
	case "pause":
		err = client.PauseCtx(r.Context(), req.Hashes)
	case "resume":
		err = client.ResumeCtx(r.Context(), req.Hashes)
	case "delete":
		err = client.DeleteTorrentsCtx(r.Context(), req.Hashes, false)
	case "forceDelete":
		err = client.DeleteTorrentsCtx(r.Context(), req.Hashes, true)
	case "recheck":
		err = client.RecheckCtx(r.Context(), req.Hashes)
	case "reannounce":
		err = client.ReAnnounceTorrentsCtx(r.Context(), req.Hashes)
	case "increasePrio":
		err = client.IncreasePriorityCtx(r.Context(), req.Hashes)
	case "decreasePrio":
		err = client.DecreasePriorityCtx(r.Context(), req.Hashes)
	case "topPrio":
		err = client.SetMaxPriorityCtx(r.Context(), req.Hashes)
	case "bottomPrio":
		err = client.SetMinPriorityCtx(r.Context(), req.Hashes)
	case "addTags":
		if len(req.Tags) > 0 {
			err = client.AddTagsCtx(r.Context(), req.Hashes, strings.Join(req.Tags, ","))
		}
	case "removeTags":
		if len(req.Tags) > 0 {
			err = client.RemoveTagsCtx(r.Context(), req.Hashes, strings.Join(req.Tags, ","))
		}
	case "setTags":
		if len(req.Tags) > 0 {
			err = client.SetTags(r.Context(), req.Hashes, strings.Join(req.Tags, ","))
		}
	case "setCategory":
		err = client.SetCategoryCtx(r.Context(), req.Hashes, req.Category)
	case "setAutoTMM":
		err = client.SetAutoManagementCtx(r.Context(), req.Hashes, req.Enable)
	default:
		RespondError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("action", req.Action).Msg("Failed to perform bulk action")
		RespondError(w, http.StatusInternalServerError, "Failed to perform action")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Action completed successfully"})
}

// GetCategories returns categories using sync manager
func (h *TorrentsHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Sync to get latest data using our sync manager
	syncData, err := h.syncManager.Sync(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to sync data")
		RespondError(w, http.StatusInternalServerError, "Failed to sync data")
		return
	}

	// Get categories from sync data
	categories := syncData.Categories
	RespondJSON(w, http.StatusOK, categories)
}

// GetTags returns tags using sync manager
func (h *TorrentsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Sync to get latest data using our sync manager
	syncData, err := h.syncManager.Sync(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to sync data")
		RespondError(w, http.StatusInternalServerError, "Failed to sync data")
		return
	}

	// Get tags from sync data
	tags := syncData.Tags
	RespondJSON(w, http.StatusOK, tags)
}

// CreateCategoryRequest represents a category creation request
type CreateCategoryRequest struct {
	Name     string `json:"name"`
	SavePath string `json:"save_path"`
}

// CreateCategory creates a new category
func (h *TorrentsHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Category name is required")
		return
	}

	// Get client for direct operations
	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	if err := client.CreateCategoryCtx(r.Context(), req.Name, req.SavePath); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("category", req.Name).Msg("Failed to create category")
		RespondError(w, http.StatusInternalServerError, "Failed to create category")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Category created successfully"})
}

// EditCategory edits an existing category
func (h *TorrentsHandler) EditCategory(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Category name is required")
		return
	}

	// Get client for direct operations
	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	if err := client.EditCategoryCtx(r.Context(), req.Name, req.SavePath); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("category", req.Name).Msg("Failed to edit category")
		RespondError(w, http.StatusInternalServerError, "Failed to edit category")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Category updated successfully"})
}

// GetFilteredTorrents returns torrents based on filters
func (h *TorrentsHandler) GetFilteredTorrents(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get query parameters
	category := r.URL.Query().Get("category")
	tag := r.URL.Query().Get("tag")
	filter := r.URL.Query().Get("filter")
	search := r.URL.Query().Get("search")

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	opts := qbt.TorrentFilterOptions{}
	if category != "" {
		opts.Category = category
	}
	if tag != "" {
		opts.Tag = tag
	}
	if filter != "" {
		opts.Filter = qbt.TorrentFilter(filter)
	}

	torrents, err := client.GetTorrentsCtx(r.Context(), opts)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	// Filter by search if provided
	if search != "" {
		torrents = h.filterTorrentsBySearch(torrents, search)
	}

	response := TorrentResponse{
		Torrents: torrents,
		Total:    len(torrents),
		HasMore:  false,
	}

	RespondJSON(w, http.StatusOK, response)
}

// GetTorrentCounts returns torrent counts by status
func (h *TorrentsHandler) GetTorrentCounts(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	torrents, err := client.GetTorrentsCtx(r.Context(), qbt.TorrentFilterOptions{})
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	counts := h.calculateCounts(torrents)
	RespondJSON(w, http.StatusOK, counts)
}

// DeleteTorrent deletes a single torrent
func (h *TorrentsHandler) DeleteTorrent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	err = client.DeleteTorrentsCtx(r.Context(), []string{hash}, deleteFiles)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to delete torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to delete torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Torrent deleted successfully"})
}

// PauseTorrent pauses a single torrent
func (h *TorrentsHandler) PauseTorrent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	err = client.PauseCtx(r.Context(), []string{hash})
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to pause torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to pause torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Torrent paused successfully"})
}

// ResumeTorrent resumes a single torrent
func (h *TorrentsHandler) ResumeTorrent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	err = client.ResumeCtx(r.Context(), []string{hash})
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to resume torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to resume torrent")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Torrent resumed successfully"})
}

// GetTorrentProperties returns properties for a specific torrent
func (h *TorrentsHandler) GetTorrentProperties(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	properties, err := client.GetTorrentPropertiesCtx(r.Context(), hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent properties")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return
	}

	RespondJSON(w, http.StatusOK, properties)
}

// GetTorrentTrackers returns trackers for a specific torrent
func (h *TorrentsHandler) GetTorrentTrackers(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	trackers, err := client.GetTorrentTrackersCtx(r.Context(), hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent trackers")
		return
	}

	RespondJSON(w, http.StatusOK, trackers)
}

// GetTorrentFiles returns files for a specific torrent
func (h *TorrentsHandler) GetTorrentFiles(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	files, err := client.GetFilesInformationCtx(r.Context(), hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent files")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent files")
		return
	}

	RespondJSON(w, http.StatusOK, files)
}

// GetTorrentWebSeeds returns web seeds for a specific torrent
func (h *TorrentsHandler) GetTorrentWebSeeds(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Hash is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	webSeeds, err := client.GetTorrentsWebSeedsCtx(r.Context(), hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent web seeds")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent web seeds")
		return
	}

	RespondJSON(w, http.StatusOK, webSeeds)
}

// RemoveCategories removes multiple categories
func (h *TorrentsHandler) RemoveCategories(w http.ResponseWriter, r *http.Request) {
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
		RespondError(w, http.StatusBadRequest, "Categories list is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	err = client.RemoveCategoriesCtx(r.Context(), req.Categories)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Interface("categories", req.Categories).Msg("Failed to remove categories")
		RespondError(w, http.StatusInternalServerError, "Failed to remove categories")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Categories removed successfully"})
}

// CreateTags creates new tags
func (h *TorrentsHandler) CreateTags(w http.ResponseWriter, r *http.Request) {
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
		RespondError(w, http.StatusBadRequest, "Tags list is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	err = client.CreateTagsCtx(r.Context(), req.Tags)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Interface("tags", req.Tags).Msg("Failed to create tags")
		RespondError(w, http.StatusInternalServerError, "Failed to create tags")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Tags created successfully"})
}

// DeleteTags deletes existing tags
func (h *TorrentsHandler) DeleteTags(w http.ResponseWriter, r *http.Request) {
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
		RespondError(w, http.StatusBadRequest, "Tags list is required")
		return
	}

	client, err := h.getClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		RespondError(w, http.StatusInternalServerError, "Failed to get client")
		return
	}

	err = client.DeleteTagsCtx(r.Context(), req.Tags)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Interface("tags", req.Tags).Msg("Failed to delete tags")
		RespondError(w, http.StatusInternalServerError, "Failed to delete tags")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Tags deleted successfully"})
}
