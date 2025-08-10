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
	Status     map[string]int `json:"status"`
	Categories map[string]int `json:"categories"`
	Tags       map[string]int `json:"tags"`
	Trackers   map[string]int `json:"trackers"`
	Total      int            `json:"total"`
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
		switch string(torrent.State) {
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

// calculateCounts calculates torrent counts by status (legacy function - now we use calculateDetailedCounts)
func (h *TorrentsHandler) calculateCounts(torrents []qbt.Torrent) *TorrentCounts {
	counts := &TorrentCounts{
		Status:     make(map[string]int),
		Categories: make(map[string]int),
		Tags:       make(map[string]int),
		Trackers:   make(map[string]int),
		Total:      len(torrents),
	}

	// Initialize status counts
	counts.Status["all"] = len(torrents)
	counts.Status["downloading"] = 0
	counts.Status["seeding"] = 0
	counts.Status["completed"] = 0
	counts.Status["paused"] = 0
	counts.Status["active"] = 0
	counts.Status["inactive"] = 0
	counts.Status["resumed"] = 0
	counts.Status["stalled"] = 0
	counts.Status["error"] = 0

	for _, torrent := range torrents {
		state := string(torrent.State)

		switch state {
		case "downloading", "metadl", "stalledDL", "forcedDL", "queuedDL":
			counts.Status["downloading"]++
		case "uploading", "stalledUP", "forcedUP", "queuedUP":
			counts.Status["seeding"]++
		case "pausedDL", "pausedUP":
			counts.Status["paused"]++
		case "error", "missingFiles":
			counts.Status["error"]++
		}

		if torrent.Progress >= 1.0 {
			counts.Status["completed"]++
		}

		if torrent.DlSpeed > 0 || torrent.UpSpeed > 0 {
			counts.Status["active"]++
		} else {
			counts.Status["inactive"]++
		}

		if strings.Contains(state, "stalled") {
			counts.Status["stalled"]++
		}

		if !strings.Contains(state, "paused") {
			counts.Status["resumed"]++
		}
	}

	return counts
}

// calculateDetailedCounts calculates detailed counts for filter sidebar
func (h *TorrentsHandler) calculateDetailedCounts(torrents []qbt.Torrent) map[string]int {
	detailedCounts := make(map[string]int)

	// Count by status filters (matching frontend logic)
	statusFilters := []string{"all", "downloading", "seeding", "completed", "paused", "active", "inactive", "resumed", "stalled", "stalled_uploading", "stalled_downloading", "errored", "checking", "moving"}
	for _, status := range statusFilters {
		filtered := h.filterTorrentsByStatus(torrents, status)
		detailedCounts["status:"+status] = len(filtered)
	}

	// Count by categories
	categoryMap := make(map[string]int)
	for _, torrent := range torrents {
		category := torrent.Category
		if category == "" {
			categoryMap[""] += 1 // Uncategorized
		} else {
			categoryMap[category] += 1
		}
	}
	for category, count := range categoryMap {
		detailedCounts["category:"+category] = count
	}

	// Count by tags
	tagMap := make(map[string]int)
	untaggedCount := 0
	for _, torrent := range torrents {
		if torrent.Tags == "" {
			untaggedCount++
		} else {
			tags := strings.Split(torrent.Tags, ",")
			for _, tag := range tags {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagMap[tag] += 1
				}
			}
		}
	}
	detailedCounts["tag:"] = untaggedCount // Untagged
	for tag, count := range tagMap {
		detailedCounts["tag:"+tag] = count
	}

	// Count by trackers
	trackerMap := make(map[string]int)
	for _, torrent := range torrents {
		tracker := torrent.Tracker
		if tracker == "" {
			trackerMap[""] += 1 // No tracker
		} else {
			// Extract domain from tracker URL for grouping
			if strings.Contains(tracker, "://") {
				parts := strings.Split(tracker, "://")
				if len(parts) > 1 {
					domain := strings.Split(parts[1], "/")[0]
					trackerMap[domain] += 1
				}
			} else {
				trackerMap[tracker] += 1
			}
		}
	}
	for tracker, count := range trackerMap {
		detailedCounts["tracker:"+tracker] = count
	}

	return detailedCounts
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
	offset := 0
	sort := "addedOn"
	order := "desc"
	search := ""
	sessionID := r.Header.Get("X-Session-ID")

	// Parse filters from JSON if provided
	var filters struct {
		Status     []string `json:"status"`
		Categories []string `json:"categories"`
		Tags       []string `json:"tags"`
		Trackers   []string `json:"trackers"`
	}

	if filtersParam := r.URL.Query().Get("filters"); filtersParam != "" {
		if err := json.Unmarshal([]byte(filtersParam), &filters); err != nil {
			log.Error().Err(err).Str("filters", filtersParam).Msg("Failed to parse filters JSON")
		}
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 2000 {
			limit = parsed
		}
	}

	// Support both offset and page for backwards compatibility
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	} else if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed >= 0 {
			offset = parsed * limit // Convert page to offset
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

	// Get all data from sync manager
	syncData, err := h.syncManager.GetMainData(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to sync data")
		RespondError(w, http.StatusInternalServerError, "Failed to sync data")
		return
	}

	// Extract torrents from sync data
	allTorrents := make([]qbt.Torrent, 0, len(syncData.Torrents))
	if syncData != nil {
		if syncData.Torrents != nil {
			for _, torrent := range syncData.Torrents {
				allTorrents = append(allTorrents, torrent)
			}
		}
	}

	// Apply search filter if provided
	filteredTorrents := allTorrents
	if search != "" {
		filteredTorrents = h.filterTorrentsBySearch(allTorrents, search)
	}

	// Apply status filters if provided
	if len(filters.Status) > 0 {
		filteredTorrents = h.filterTorrentsByStatuses(filteredTorrents, filters.Status)
	}

	// Apply category filters if provided
	if len(filters.Categories) > 0 {
		filteredTorrents = h.filterTorrentsByCategories(filteredTorrents, filters.Categories)
	}

	// Apply tag filters if provided
	if len(filters.Tags) > 0 {
		filteredTorrents = h.filterTorrentsByTags(filteredTorrents, filters.Tags)
	}

	// Apply tracker filters if provided
	if len(filters.Trackers) > 0 {
		filteredTorrents = h.filterTorrentsByTrackers(filteredTorrents, filters.Trackers)
	}

	// Calculate stats and counts
	stats := h.calculateStats(filteredTorrents)

	// Calculate detailed counts for filter sidebar (using all torrents, not filtered ones)
	detailedCounts := h.calculateDetailedCounts(allTorrents)

	// Convert flat counts to structured format expected by frontend
	structuredCounts := &TorrentCounts{
		Status:     make(map[string]int),
		Categories: make(map[string]int),
		Tags:       make(map[string]int),
		Trackers:   make(map[string]int),
		Total:      len(allTorrents),
	}

	for key, count := range detailedCounts {
		if strings.HasPrefix(key, "status:") {
			statusKey := strings.TrimPrefix(key, "status:")
			structuredCounts.Status[statusKey] = count
		} else if strings.HasPrefix(key, "category:") {
			categoryKey := strings.TrimPrefix(key, "category:")
			structuredCounts.Categories[categoryKey] = count
		} else if strings.HasPrefix(key, "tag:") {
			tagKey := strings.TrimPrefix(key, "tag:")
			structuredCounts.Tags[tagKey] = count
		} else if strings.HasPrefix(key, "tracker:") {
			trackerKey := strings.TrimPrefix(key, "tracker:")
			structuredCounts.Trackers[trackerKey] = count
		}
	}

	// Get categories and tags from sync data
	var categories map[string]qbt.Category
	var tags []string
	if syncData != nil {
		categories = syncData.Categories
		tags = syncData.Tags
	}

	// Sort torrents
	h.sortTorrents(filteredTorrents, sort, order)

	// Apply pagination
	var paginatedTorrents []qbt.Torrent
	if offset < len(filteredTorrents) {
		end := offset + limit
		if end > len(filteredTorrents) {
			end = len(filteredTorrents)
		}
		paginatedTorrents = filteredTorrents[offset:end]
	}

	response := &TorrentResponse{
		Torrents:   paginatedTorrents,
		Total:      len(filteredTorrents),
		Stats:      stats,
		Counts:     structuredCounts,
		Categories: categories,
		Tags:       tags,
		HasMore:    offset+limit < len(filteredTorrents),
		SessionID:  sessionID,
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
	syncData, err := h.syncManager.GetMainData(r.Context(), instanceID)
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

// filterTorrentsByStatus filters torrents by status
func (h *TorrentsHandler) filterTorrentsByStatus(torrents []qbt.Torrent, status string) []qbt.Torrent {
	if status == "" {
		return torrents
	}

	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		state := string(torrent.State)

		// Match frontend status filter logic
		switch status {
		case "all":
			filtered = append(filtered, torrent)
		case "downloading":
			if state == "downloading" || state == "metaDL" || state == "stalledDL" || state == "forcedDL" || state == "queuedDL" {
				filtered = append(filtered, torrent)
			}
		case "seeding":
			if state == "uploading" || state == "stalledUP" || state == "forcedUP" || state == "queuedUP" {
				filtered = append(filtered, torrent)
			}
		case "completed":
			if torrent.Progress >= 1.0 {
				filtered = append(filtered, torrent)
			}
		case "paused":
			if state == "pausedDL" || state == "pausedUP" || state == "stoppedDL" || state == "stoppedUP" {
				filtered = append(filtered, torrent)
			}
		case "active":
			if torrent.DlSpeed > 0 || torrent.UpSpeed > 0 {
				filtered = append(filtered, torrent)
			}
		case "inactive":
			if torrent.DlSpeed == 0 && torrent.UpSpeed == 0 {
				filtered = append(filtered, torrent)
			}
		case "resumed":
			if !strings.Contains(state, "paused") && !strings.Contains(state, "stopped") {
				filtered = append(filtered, torrent)
			}
		case "stalled":
			if state == "stalledDL" || state == "stalledUP" {
				filtered = append(filtered, torrent)
			}
		case "stalled_uploading":
			if state == "stalledUP" {
				filtered = append(filtered, torrent)
			}
		case "stalled_downloading":
			if state == "stalledDL" {
				filtered = append(filtered, torrent)
			}
		case "errored":
			if state == "error" || state == "missingFiles" {
				filtered = append(filtered, torrent)
			}
		case "checking":
			if state == "checkingDL" || state == "checkingUP" || state == "checkingResumeData" {
				filtered = append(filtered, torrent)
			}
		case "moving":
			if state == "moving" {
				filtered = append(filtered, torrent)
			}
		default:
			// Direct state match for any other status
			if state == status {
				filtered = append(filtered, torrent)
			}
		}
	}

	return filtered
}

// filterTorrentsByCategory filters torrents by category
func (h *TorrentsHandler) filterTorrentsByCategory(torrents []qbt.Torrent, category string) []qbt.Torrent {
	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		// Handle empty category (uncategorized)
		if category == "" && torrent.Category == "" {
			filtered = append(filtered, torrent)
		} else if category != "" && torrent.Category == category {
			filtered = append(filtered, torrent)
		}
	}

	return filtered
}

// filterTorrentsByTag filters torrents by tag
func (h *TorrentsHandler) filterTorrentsByTag(torrents []qbt.Torrent, tag string) []qbt.Torrent {
	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		// Handle empty tag (untagged)
		if tag == "" && torrent.Tags == "" {
			filtered = append(filtered, torrent)
		} else if tag != "" && torrent.Tags != "" {
			// Split tags and check if the requested tag is present
			tags := strings.Split(torrent.Tags, ",")
			for _, t := range tags {
				if strings.TrimSpace(t) == tag {
					filtered = append(filtered, torrent)
					break
				}
			}
		}
	}

	return filtered
}

// filterTorrentsByTracker filters torrents by tracker
func (h *TorrentsHandler) filterTorrentsByTracker(torrents []qbt.Torrent, tracker string) []qbt.Torrent {
	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		// Handle empty tracker
		if tracker == "" && torrent.Tracker == "" {
			filtered = append(filtered, torrent)
		} else if tracker != "" && strings.Contains(strings.ToLower(torrent.Tracker), strings.ToLower(tracker)) {
			filtered = append(filtered, torrent)
		}
	}

	return filtered
}

// filterTorrentsByStatuses filters torrents by multiple statuses (OR logic)
func (h *TorrentsHandler) filterTorrentsByStatuses(torrents []qbt.Torrent, statuses []string) []qbt.Torrent {
	if len(statuses) == 0 {
		return torrents
	}

	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		for _, status := range statuses {
			statusFiltered := h.filterTorrentsByStatus([]qbt.Torrent{torrent}, status)
			if len(statusFiltered) > 0 {
				filtered = append(filtered, torrent)
				break // Found a match, no need to check other statuses for this torrent
			}
		}
	}

	return filtered
}

// filterTorrentsByCategories filters torrents by multiple categories (OR logic)
func (h *TorrentsHandler) filterTorrentsByCategories(torrents []qbt.Torrent, categories []string) []qbt.Torrent {
	if len(categories) == 0 {
		return torrents
	}

	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		for _, category := range categories {
			categoryFiltered := h.filterTorrentsByCategory([]qbt.Torrent{torrent}, category)
			if len(categoryFiltered) > 0 {
				filtered = append(filtered, torrent)
				break // Found a match, no need to check other categories for this torrent
			}
		}
	}

	return filtered
}

// filterTorrentsByTags filters torrents by multiple tags (OR logic)
func (h *TorrentsHandler) filterTorrentsByTags(torrents []qbt.Torrent, tags []string) []qbt.Torrent {
	if len(tags) == 0 {
		return torrents
	}

	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		for _, tag := range tags {
			tagFiltered := h.filterTorrentsByTag([]qbt.Torrent{torrent}, tag)
			if len(tagFiltered) > 0 {
				filtered = append(filtered, torrent)
				break // Found a match, no need to check other tags for this torrent
			}
		}
	}

	return filtered
}

// filterTorrentsByTrackers filters torrents by multiple trackers (OR logic)
func (h *TorrentsHandler) filterTorrentsByTrackers(torrents []qbt.Torrent, trackers []string) []qbt.Torrent {
	if len(trackers) == 0 {
		return torrents
	}

	var filtered []qbt.Torrent

	for _, torrent := range torrents {
		for _, tracker := range trackers {
			trackerFiltered := h.filterTorrentsByTracker([]qbt.Torrent{torrent}, tracker)
			if len(trackerFiltered) > 0 {
				filtered = append(filtered, torrent)
				break // Found a match, no need to check other trackers for this torrent
			}
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
	syncData, err := h.syncManager.GetMainData(r.Context(), instanceID)
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
	syncData, err := h.syncManager.GetMainData(r.Context(), instanceID)
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
