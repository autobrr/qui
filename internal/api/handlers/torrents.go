// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"cmp"
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

	mediainfo "github.com/autobrr/go-mediainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/redact"
	"github.com/autobrr/qui/pkg/torrentname"
)

type torrentAdder interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
	AddTorrentFromURLs(ctx context.Context, instanceID int, urls []string, options map[string]string) (*qbt.TorrentAddResponse, error)
	GetAppPreferences(ctx context.Context, instanceID int) (qbt.AppPreferences, error)
}

type torrentDownloader interface {
	DownloadTorrent(ctx context.Context, req jackett.TorrentDownloadRequest) ([]byte, error)
}

type deleteMatchFinder interface {
	FindDeleteMatches(ctx context.Context, selection map[int][]string) (*crossseed.DeleteMatchesResponse, error)
}

type torrentContentResolver interface {
	GetTorrentFiles(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error)
	GetTorrentProperties(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error)
	GetTorrentRefs(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error)
}

type torrentDeleter interface {
	DeleteTorrents(ctx context.Context, instanceID int, hashes []string, deleteFiles bool, actor qbittorrent.DeleteActor) error
}

type TorrentsHandler struct {
	syncManager   *qbittorrent.SyncManager
	instanceStore *models.InstanceStore

	torrentAdder      torrentAdder
	torrentDownloader torrentDownloader
	contentResolver   torrentContentResolver
	archiveExporter   torrentArchiveExporter
	deleteMatches     deleteMatchFinder
	deleter           torrentDeleter
	installPaths      installPaths
	held              heldTorrents
}

type heldTorrents interface {
	Hold(instanceID int, hash string) error
}

func (h *TorrentsHandler) SetHeldTorrents(held heldTorrents) {
	h.held = held
}

var bulkActionMoves = []string{"delete", "deleteWithFiles", "setLocation", "setCategory", "toggleAutoTMM"}

func (h *TorrentsHandler) respondIfHeld(w http.ResponseWriter, instanceID int, hashes ...string) bool {
	if h.held == nil {
		return false
	}
	for _, list := range hashes {
		for hash := range strings.SplitSeq(list, "|") {
			if err := h.held.Hold(instanceID, hash); err != nil {
				RespondError(w, http.StatusConflict, err.Error())
				return true
			}
		}
	}
	return false
}

func truncateExpr(expr string, maxLen int) string {
	if len(expr) <= maxLen {
		return expr
	}
	return expr[:maxLen-3] + "..."
}

func validateTorrentFilePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, `\`) {
		return errors.New("torrentFilePath must include a filename, not a directory")
	}

	return nil
}

const addTorrentMaxFormMemory int64 = 256 << 20

const addTorrentMaxFormParts = 1000

type SortedPeer struct {
	Key string `json:"key"`
	qbt.TorrentPeer
}

type SortedPeersResponse struct {
	*qbt.TorrentPeersResponse
	FullUpdate  *struct{}    `json:"full_update,omitempty"`
	SortedPeers []SortedPeer `json:"sorted_peers,omitempty"`
}

func NewTorrentsHandler(syncManager *qbittorrent.SyncManager, jackettService *jackett.Service, instanceStore *models.InstanceStore, crossSeedService *crossseed.Service) *TorrentsHandler {
	h := &TorrentsHandler{
		syncManager:     syncManager,
		instanceStore:   instanceStore,
		torrentAdder:    syncManager,
		contentResolver: syncManager,
		archiveExporter: syncManager,
		deleteMatches:   crossSeedService,
		deleter:         syncManager,
	}
	if jackettService != nil {
		h.torrentDownloader = jackettService
	}
	return h
}

func (h *TorrentsHandler) ListTorrents(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	limit := 300
	page := 0
	sort := "added_on"
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

	if q := r.URL.Query().Get("search"); q != "" {
		search = q
	}

	var filters qbittorrent.FilterOptions

	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			log.Warn().Err(err).Msg("Failed to parse filters, ignoring")
		}
	}

	preferParam := strings.TrimSpace(r.URL.Query().Get("prefer"))
	preferCached := strings.EqualFold(preferParam, "stale") ||
		strings.EqualFold(preferParam, "cache") ||
		strings.EqualFold(preferParam, "cached")

	ctx := r.Context()

	logEvent := log.Trace().
		Int("instanceID", instanceID).
		Str("sort", sort).
		Str("order", order).
		Int("page", page).
		Int("limit", limit).
		Str("search", search).
		Bool("preferCached", preferCached).
		Str("sessionID", sessionID)

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

	offset := page * limit

	response, err := h.syncManager.ListTorrents(ctx, qbittorrent.TorrentListView{
		InstanceID:         instanceID,
		Limit:              limit,
		Offset:             offset,
		Sort:               sort,
		Order:              order,
		Search:             search,
		Filters:            filters,
		SidebarCounts:      true,
		CopyGroups:         groupCopiesParam(r),
		UniqueSize:         uniqueSizeParam(r),
		CacheOnly:          preferCached,
		FullPasskeyAccess:  middleware.CanReadTrackerPasskeys(r),
		WithHelperTorrents: true,
	})
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:list") {
			return
		}
		errorStore := h.syncManager.GetErrorStore()
		if recordErr := errorStore.RecordError(ctx, instanceID, err); recordErr != nil {
			log.Error().Err(recordErr).Int("instanceID", instanceID).Msg("Failed to record torrent error")
		}

		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrents")
		return
	}

	switch {
	case response.CacheMetadata != nil && response.CacheMetadata.Source != "":
		w.Header().Set("X-Data-Source", response.CacheMetadata.Source)
	case preferCached:
		w.Header().Set("X-Data-Source", "cache")
	default:
		w.Header().Set("X-Data-Source", "fresh")
	}

	RespondJSON(w, http.StatusOK, response)
}

func groupCopiesParam(r *http.Request) bool {
	groupCopies, _ := strconv.ParseBool(r.URL.Query().Get("groupCopies"))
	return groupCopies
}

func uniqueSizeParam(r *http.Request) bool {
	uniqueSize, _ := strconv.ParseBool(r.URL.Query().Get("uniqueSize"))
	return uniqueSize
}

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
		Hashes         []string                  `json:"hashes"`
		Targets        []BulkActionTarget        `json:"targets"`
		SelectAll      bool                      `json:"selectAll"`
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
	if req.SelectAll && (len(req.Hashes) > 0 || len(req.Targets) > 0) {
		RespondError(w, http.StatusBadRequest, "Cannot specify hashes/targets together with selectAll")
		return
	}

	if req.Field != "name" && req.Field != "hash" && req.Field != "full_path" && req.Field != "tags" && req.Field != "magnet_uri" {
		RespondError(w, http.StatusBadRequest, "Invalid field: must be name, hash, full_path, tags, or magnet_uri")
		return
	}
	if req.Field == "magnet_uri" && !middleware.HasPermission(w, r, models.AreaTorrents, models.LevelWrite) {
		return
	}

	if req.Sort == "" {
		req.Sort = "added_on"
	}
	if req.Order == "" {
		req.Order = "desc"
	}

	if len(req.Targets) > 0 || len(req.Hashes) > 0 {
		targetsByInstance := explicitTargets(instanceID, req.Targets, req.Hashes)

		if instanceID == allInstancesID && len(req.Hashes) > 0 && len(req.Targets) == 0 {
			seenTargets := make(map[int]map[string]struct{})
			requestedHashes := buildExcludeHashSet(req.Hashes)
			torrents, crossErr := h.selectAllTorrents(r, true, allInstancesID, "", "", "", qbittorrent.FilterOptions{}, req.InstanceIDs, nil, nil)
			if crossErr != nil {
				respondTorrentFieldSelectionError(w, crossErr, req.Field, req.InstanceIDs)
				return
			}

			for _, torrent := range torrents {
				if !matchesRequestedHashSet(requestedHashes, torrent.Hash, torrent.InfohashV1, torrent.InfohashV2) {
					continue
				}
				addBulkTarget(targetsByInstance, seenTargets, torrent.InstanceID, resolvedTorrentFieldHash(torrent.Hash, torrent.InfohashV1, torrent.InfohashV2))
			}
		}

		if len(targetsByInstance) == 0 {
			RespondError(w, http.StatusBadRequest, "No torrents match the selection criteria")
			return
		}

		targetInstanceIDs := make([]int, 0, len(targetsByInstance))
		for targetInstanceID := range targetsByInstance {
			targetInstanceIDs = append(targetInstanceIDs, targetInstanceID)
		}
		slices.Sort(targetInstanceIDs)

		values := make([]string, 0, len(flattenTargetHashes(targetsByInstance)))
		requestedCount := 0
		resolvedCount := 0
		for _, targetInstanceID := range targetInstanceIDs {
			torrents, fieldErr := h.syncManager.GetTorrentRefs(r.Context(), targetInstanceID, qbittorrent.TorrentRefFilter{WithHelperTorrents: true})
			if fieldErr != nil {
				if instanceID != allInstancesID {
					if respondIfInstanceDisabled(w, fieldErr, targetInstanceID, "torrents:metadata") {
						return
					}
				}
				log.Error().
					Err(fieldErr).
					Int("instanceID", targetInstanceID).
					Str("field", req.Field).
					Msg("Failed to get cached torrents for explicit field request")
				RespondError(w, http.StatusInternalServerError, "Failed to get torrent field")
				return
			}

			slices.SortFunc(torrents, func(a, b *qbt.Torrent) int {
				return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Hash, b.Hash))
			})
			requestedHashes := buildExcludeHashSet(targetsByInstance[targetInstanceID])
			requestedCount += len(targetsByInstance[targetInstanceID])
			for _, torrent := range torrents {
				if !matchesRequestedHashSet(requestedHashes, torrent.Hash, torrent.InfohashV1, torrent.InfohashV2) {
					continue
				}

				value := torrentFieldValue(req.Field, torrent.Name, torrent.Hash, torrent.InfohashV1, torrent.InfohashV2, torrent.ContentPath, torrent.Tags, torrent.MagnetURI)
				if shouldIncludeTorrentFieldValue(req.Field, value) {
					values = append(values, value)
					resolvedCount++
				}
			}
		}
		if req.Field == "tags" && resolvedCount < requestedCount {
			RespondError(w, http.StatusConflict, "Could not resolve the full tag baseline for the selected torrents")
			return
		}

		RespondJSON(w, http.StatusOK, &qbittorrent.TorrentFieldResponse{
			Values: values,
			Total:  len(values),
		})
		return
	}

	torrents, err := h.selectAllTorrents(r, true, instanceID, req.Sort, req.Order, req.Search, req.Filters, req.InstanceIDs, req.ExcludeHashes, req.ExcludeTargets)
	if err != nil {
		if instanceID != allInstancesID && respondIfInstanceDisabled(w, err, instanceID, "torrents:metadata") {
			return
		}
		respondTorrentFieldSelectionError(w, err, req.Field, req.InstanceIDs)
		return
	}

	values := make([]string, 0, len(torrents))
	for _, torrent := range torrents {
		value := torrentFieldValue(req.Field, torrent.Name, torrent.Hash, torrent.InfohashV1, torrent.InfohashV2, torrent.ContentPath, torrent.Tags, torrent.Torrent.MagnetURI)
		if shouldIncludeTorrentFieldValue(req.Field, value) {
			values = append(values, value)
		}
	}

	RespondJSON(w, http.StatusOK, &qbittorrent.TorrentFieldResponse{
		Values: values,
		Total:  len(values),
	})
}

func torrentFieldValue(field, name, hash, infohashV1, infohashV2, contentPath, tags, magnetURI string) string {
	switch field {
	case "name":
		return strings.TrimSpace(name)
	case "hash":
		return preferredHashValue(&qbt.Torrent{
			Hash:       hash,
			InfohashV1: infohashV1,
			InfohashV2: infohashV2,
		})
	case "full_path":
		return strings.TrimSpace(contentPath)
	case "tags":
		return tags
	case "magnet_uri":
		return strings.TrimSpace(magnetURI)
	default:
		return ""
	}
}

func shouldIncludeTorrentFieldValue(field, value string) bool {
	return field == "tags" || value != ""
}

func resolvedTorrentFieldHash(hash, infohashV1, infohashV2 string) string {
	preferred := preferredHashValue(&qbt.Torrent{
		Hash:       hash,
		InfohashV1: infohashV1,
		InfohashV2: infohashV2,
	})
	if preferred != "" {
		return preferred
	}
	return strings.TrimSpace(hash)
}

func torrentFieldHashVariants(hash, infohashV1, infohashV2 string) []string {
	candidates := []string{
		hash,
		infohashV1,
		infohashV2,
		resolvedTorrentFieldHash(hash, infohashV1, infohashV2),
	}
	seen := make(map[string]struct{}, len(candidates))
	var variants []string
	for _, candidate := range candidates {
		normalized := normalizeHashValue(candidate)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		variants = append(variants, normalized)
	}
	return variants
}

func hasTorrentFieldHash(hash, infohashV1, infohashV2 string) bool {
	return normalizeHashValue(hash) != "" || normalizeHashValue(infohashV1) != "" || normalizeHashValue(infohashV2) != ""
}

func matchesRequestedHashSet(requestedHashes map[string]struct{}, hash, infohashV1, infohashV2 string) bool {
	if len(requestedHashes) == 0 {
		return false
	}
	for _, candidate := range torrentFieldHashVariants(hash, infohashV1, infohashV2) {
		if _, ok := requestedHashes[candidate]; ok {
			return true
		}
	}
	return false
}

func matchesExcludedTargetSet(excludeTargets map[string]struct{}, instanceID int, hash, infohashV1, infohashV2 string) bool {
	if len(excludeTargets) == 0 {
		return false
	}
	for _, candidate := range torrentFieldHashVariants(hash, infohashV1, infohashV2) {
		if _, ok := excludeTargets[fmt.Sprintf("%d:%s", instanceID, candidate)]; ok {
			return true
		}
	}
	return false
}

func respondTorrentFieldSelectionError(w http.ResponseWriter, err error, field string, instanceIDs []int) {
	if errors.Is(err, errPartialResults) {
		log.Warn().Str("field", field).Ints("instanceIDs", instanceIDs).Msg("Cross-instance torrent field returned partial results")
		RespondError(w, http.StatusServiceUnavailable, "Unable to resolve all scoped instances for torrent field request")
		return
	}
	log.Error().Err(err).Str("field", field).Msg("Failed to get torrent field")
	RespondError(w, http.StatusInternalServerError, "Failed to get torrent field")
}

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

	torrents, err := h.syncManager.GetTorrentRefs(r.Context(), instanceID, qbittorrent.TorrentRefFilter{Hashes: req.Hashes})
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:checkDuplicates") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get qBittorrent sync manager")
		RespondError(w, http.StatusInternalServerError, "Failed to check duplicate torrents")
		return
	}

	requested := make(map[string][]string, len(req.Hashes))
	for _, hash := range req.Hashes {
		key := normalizeHashValue(hash)
		if !slices.Contains(requested[key], hash) {
			requested[key] = append(requested[key], hash)
		}
	}

	matches := make([]qbittorrent.DuplicateTorrentMatch, len(torrents))
	for i, torrent := range torrents {
		matches[i] = qbittorrent.DuplicateTorrentMatch{
			Hash:          torrent.Hash,
			InfohashV1:    strings.TrimSpace(torrent.InfohashV1),
			InfohashV2:    strings.TrimSpace(torrent.InfohashV2),
			Name:          torrent.Name,
			MatchedHashes: requested[normalizeHashValue(torrent.Hash)],
		}
	}

	RespondJSON(w, http.StatusOK, struct {
		Duplicates []qbittorrent.DuplicateTorrentMatch `json:"duplicates"`
	}{Duplicates: matches})
}

func (h *TorrentsHandler) AddTorrent(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	err = r.ParseMultipartForm(addTorrentMaxFormMemory)
	if err != nil {
		if errors.Is(err, multipart.ErrMessageTooLarge) {
			RespondError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Upload too large: send at most %d files and fields per request", addTorrentMaxFormParts))
			return
		}
		RespondError(w, http.StatusBadRequest, "Failed to parse form data")
		return
	}

	type uploadedTorrent struct {
		filename string
		content  []byte
	}
	var torrentFiles []uploadedTorrent
	var urls []string

	type fileReadFailure struct {
		filename string
		err      string
	}
	var fileReadFailures []fileReadFailure

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
				torrentFiles = append(torrentFiles, uploadedTorrent{filename: fileHeader.Filename, content: fileContent})
			}
		}
	}

	var indexerID int
	if len(torrentFiles) == 0 {
		urlsParam := r.FormValue("urls")
		if urlsParam != "" {
			urlsParam = strings.ReplaceAll(urlsParam, "\n", ",")
			urls = strings.Split(urlsParam, ",")
		} else {
			RespondError(w, http.StatusBadRequest, "Either torrent files or URLs are required")
			return
		}

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

	options := make(map[string]string)

	if category := r.FormValue("category"); category != "" {
		options["category"] = category
	}

	if tags := r.FormValue("tags"); tags != "" {
		options["tags"] = tags
	}

	if pausedStr := r.FormValue("paused"); pausedStr != "" {
		paused := strconv.FormatBool(pausedStr == "true")
		options["paused"] = paused
		options["stopped"] = paused
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
		if upLimitInt, err := strconv.ParseInt(upLimit, 10, 64); err == nil && upLimitInt > 0 {
			options["upLimit"] = strconv.FormatInt(upLimitInt*1024, 10)
		}
	}

	if dlLimit := r.FormValue("dlLimit"); dlLimit != "" {
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
		options["autoTMM"] = "false"
	}

	if useDownloadPath := r.FormValue("useDownloadPath"); useDownloadPath != "" {
		options["useDownloadPath"] = useDownloadPath
	}

	if downloadPath := r.FormValue("downloadPath"); downloadPath != "" {
		options["downloadPath"] = downloadPath
	}

	if autoTMM := r.FormValue("autoTMM"); autoTMM != "" {
		options["autoTMM"] = autoTMM
		if autoTMM == "true" {
			delete(options, "savepath")
			delete(options, "useDownloadPath")
			delete(options, "downloadPath")
		}
	}

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

	if len(torrentFiles) > 0 {
		for i, torrentFile := range torrentFiles {
			if ctx.Err() != nil {
				log.Warn().Int("instanceID", instanceID).Msg("Request cancelled, stopping torrent additions")
				break
			}

			if _, err := h.torrentAdder.AddTorrent(ctx, instanceID, torrentFile.content, options); err != nil {
				if respondIfInstanceDisabled(w, err, instanceID, "torrents:add") {
					return
				}
				log.Debug().Err(err).Int("instanceID", instanceID).Int("fileIndex", i).Msg("Failed to add torrent file")
				failedFiles = append(failedFiles, failedFile{Filename: torrentFile.filename, Error: err.Error()})
				failedCount++
				lastError = err
			} else {
				addedCount++
			}
		}
		for _, f := range fileReadFailures {
			failedFiles = append(failedFiles, failedFile{Filename: f.filename, Error: f.err})
			failedCount++
		}
	} else if len(urls) > 0 {
		if indexerID > 0 {
			if h.torrentDownloader == nil {
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

				if ctx.Err() != nil {
					log.Warn().Int("instanceID", instanceID).Msg("Request cancelled, stopping torrent additions")
					break
				}

				if strings.HasPrefix(strings.ToLower(url), "magnet:") {
					resp, err := h.torrentAdder.AddTorrentFromURLs(ctx, instanceID, []string{url}, options)
					if err != nil {
						if respondIfInstanceDisabled(w, err, instanceID, "torrents:addFromURLs") {
							return
						}
						log.Debug().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(url)).Msg("Failed to add magnet link")
						failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
						failedCount++
						lastError = err
					} else if err := torrentURLAddFailureError(resp); err != nil {
						log.Debug().
							Int("instanceID", instanceID).
							Str("url", redact.URLString(url)).
							Int64("successCount", resp.SuccessCount).
							Int64("failureCount", resp.FailureCount).
							Int64("pendingCount", resp.PendingCount).
							Msg("qBittorrent reported failed magnet link add")
						failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
						failedCount++
						lastError = err
					} else {
						addedCount++
					}
					continue
				}

				torrentBytes, err := h.torrentDownloader.DownloadTorrent(ctx, jackett.TorrentDownloadRequest{
					IndexerID:   indexerID,
					DownloadURL: url,
				})
				if err != nil {
					var magnetErr *jackett.MagnetDownloadError
					if errors.As(err, &magnetErr) && magnetErr.MagnetURL != "" {
						magnetURL := strings.TrimSpace(magnetErr.MagnetURL)
						resp, err := h.torrentAdder.AddTorrentFromURLs(ctx, instanceID, []string{magnetURL}, options)
						if err != nil {
							if respondIfInstanceDisabled(w, err, instanceID, "torrents:addFromURLs") {
								return
							}
							log.Debug().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(magnetURL)).Msg("Failed to add magnet link from indexer redirect")
							failedURLs = append(failedURLs, failedURL{URL: magnetURL, Error: err.Error()})
							failedCount++
							lastError = err
						} else if err := torrentURLAddFailureError(resp); err != nil {
							log.Debug().
								Int("instanceID", instanceID).
								Str("url", redact.URLString(magnetURL)).
								Int64("successCount", resp.SuccessCount).
								Int64("failureCount", resp.FailureCount).
								Int64("pendingCount", resp.PendingCount).
								Msg("qBittorrent reported failed magnet link from indexer redirect")
							failedURLs = append(failedURLs, failedURL{URL: magnetURL, Error: err.Error()})
							failedCount++
							lastError = err
						} else {
							addedCount++
						}
						continue
					}
					log.Debug().Err(err).Int("indexerID", indexerID).Int("instanceID", instanceID).Str("url", redact.URLString(url)).Msg("Failed to download torrent from indexer")
					failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
					failedCount++
					lastError = err
					continue
				}

				if _, err := h.torrentAdder.AddTorrent(ctx, instanceID, torrentBytes, options); err != nil {
					if respondIfInstanceDisabled(w, err, instanceID, "torrents:add") {
						return
					}
					log.Debug().Err(err).Int("instanceID", instanceID).Int("indexerID", indexerID).Str("url", redact.URLString(url)).Msg("Failed to add downloaded torrent")
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
			var skippedEmpty int
			for _, url := range urls {
				url = strings.TrimSpace(url)
				if url == "" {
					skippedEmpty++
					continue
				}

				if ctx.Err() != nil {
					log.Warn().Int("instanceID", instanceID).Msg("Request cancelled, stopping torrent additions")
					break
				}

				resp, err := h.torrentAdder.AddTorrentFromURLs(ctx, instanceID, []string{url}, options)
				if err != nil {
					if respondIfInstanceDisabled(w, err, instanceID, "torrents:addFromURLs") {
						return
					}
					log.Debug().Err(err).Int("instanceID", instanceID).Str("url", redact.URLString(url)).Msg("Failed to add torrent from URL")
					failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
					failedCount++
					lastError = err
					continue
				}

				if err := torrentURLAddFailureError(resp); err != nil {
					log.Debug().
						Int("instanceID", instanceID).
						Str("url", redact.URLString(url)).
						Int64("successCount", resp.SuccessCount).
						Int64("failureCount", resp.FailureCount).
						Int64("pendingCount", resp.PendingCount).
						Msg("qBittorrent reported failed URL add")
					failedURLs = append(failedURLs, failedURL{URL: url, Error: err.Error()})
					failedCount++
					lastError = err
					continue
				}

				addedCount++
			}
			if skippedEmpty > 0 {
				log.Debug().Int("skippedEmpty", skippedEmpty).Int("instanceID", instanceID).
					Msg("Skipped empty URLs in add torrent request")
			}
		}
	}

	if failedCount > 0 {
		log.Error().Err(lastError).Int("instanceID", instanceID).Int("added", addedCount).Int("failed", failedCount).
			Msg("Failed to add torrents")
	}

	if failedCount > 0 && addedCount == 0 {
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to add all torrents: %v", lastError))
		return
	}

	log.Debug().Int("instanceID", instanceID).Msg("Torrent added - next request will get fresh data from sync manager")

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

func torrentURLAddFailureError(resp *qbt.TorrentAddResponse) error {
	if resp == nil || resp.FailureCount == 0 {
		return nil
	}
	return errors.New("qBittorrent rejected torrent URL")
}

type BulkActionRequest struct {
	Hashes                   []string                   `json:"hashes"`
	Targets                  []BulkActionTarget         `json:"targets,omitempty"`
	Action                   string                     `json:"action"`
	DeleteFiles              bool                       `json:"deleteFiles,omitempty"`
	Tags                     string                     `json:"tags,omitempty"`
	Comment                  string                     `json:"comment,omitempty"`
	Category                 string                     `json:"category,omitempty"`
	Enable                   bool                       `json:"enable,omitempty"`
	SelectAll                bool                       `json:"selectAll,omitempty"`
	InstanceIDs              []int                      `json:"instanceIds,omitempty"`
	Filters                  *qbittorrent.FilterOptions `json:"filters,omitempty"`
	Search                   string                     `json:"search,omitempty"`
	ExcludeHashes            []string                   `json:"excludeHashes,omitempty"`
	ExcludeTargets           []BulkActionTarget         `json:"excludeTargets,omitempty"`
	RatioLimit               float64                    `json:"ratioLimit,omitempty"`
	SeedingTimeLimit         int64                      `json:"seedingTimeLimit,omitempty"`
	InactiveSeedingTimeLimit int64                      `json:"inactiveSeedingTimeLimit,omitempty"`
	ShareLimitAction         string                     `json:"shareLimitAction,omitempty"`
	ShareLimitsMode          string                     `json:"shareLimitsMode,omitempty"`
	UploadLimit              int64                      `json:"uploadLimit,omitempty"`
	DownloadLimit            int64                      `json:"downloadLimit,omitempty"`
	Location                 string                     `json:"location,omitempty"`
	TrackerOldURL            string                     `json:"trackerOldURL,omitempty"`
	TrackerNewURL            string                     `json:"trackerNewURL,omitempty"`
	TrackerURLs              string                     `json:"trackerURLs,omitempty"`
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

var errPartialResults = errors.New("selection read returned partial results")

func explicitTargets(scope int, targets []BulkActionTarget, hashes []string) map[int][]string {
	targetsByInstance := make(map[int][]string)
	seenTargets := make(map[int]map[string]struct{})

	for _, target := range targets {
		targetInstanceID := target.InstanceID
		if targetInstanceID <= 0 {
			if scope == allInstancesID {
				continue
			}
			targetInstanceID = scope
		}
		if scope != allInstancesID && targetInstanceID != scope {
			continue
		}
		addBulkTarget(targetsByInstance, seenTargets, targetInstanceID, target.Hash)
	}

	if scope != allInstancesID {
		for _, hash := range hashes {
			addBulkTarget(targetsByInstance, seenTargets, scope, hash)
		}
	}

	return targetsByInstance
}

func (h *TorrentsHandler) selectAllTorrents(r *http.Request, cacheOnly bool, scope int, sort, order, search string, filters qbittorrent.FilterOptions, instanceIDs []int, excludeHashes []string, excludeTargets []BulkActionTarget) ([]qbittorrent.CrossInstanceTorrentView, error) {
	ctx := r.Context()
	view := qbittorrent.TorrentListView{
		InstanceID:         scope,
		Sort:               sort,
		Order:              order,
		Search:             search,
		Filters:            filters,
		CacheOnly:          cacheOnly,
		FullPasskeyAccess:  middleware.CanReadTrackerPasskeys(r),
		WithHelperTorrents: true,
	}
	var torrents []qbittorrent.CrossInstanceTorrentView
	if scope == allInstancesID {
		view.Unified, view.InstanceIDs = true, instanceIDs
		response, err := h.syncManager.ListTorrents(ctx, view)
		if err != nil {
			return nil, err
		}
		if response.PartialResults {
			return nil, errPartialResults
		}
		torrents = response.CrossInstanceTorrents
	} else {
		response, err := h.syncManager.ListTorrents(ctx, view)
		if err != nil {
			if !errors.Is(err, qbittorrent.ErrInstanceDisabled) {
				if recordErr := h.syncManager.GetErrorStore().RecordError(ctx, scope, err); recordErr != nil {
					log.Error().Err(recordErr).Int("instanceID", scope).Msg("Failed to record torrent error")
				}
			}
			return nil, err
		}
		torrents = make([]qbittorrent.CrossInstanceTorrentView, 0, len(response.Torrents))
		for i := range response.Torrents {
			torrents = append(torrents, qbittorrent.CrossInstanceTorrentView{TorrentView: &response.Torrents[i], InstanceID: scope})
		}
	}

	excludedHashes := buildExcludeHashSet(excludeHashes)
	excludedTargets := buildExcludeTargetSet(excludeTargets)
	return slices.DeleteFunc(torrents, func(torrent qbittorrent.CrossInstanceTorrentView) bool {
		return !hasTorrentFieldHash(torrent.Hash, torrent.InfohashV1, torrent.InfohashV2) || selectionExcluded(excludedHashes, excludedTargets, torrent)
	}), nil
}

func selectionExcluded(excludeHashes, excludeTargets map[string]struct{}, torrent qbittorrent.CrossInstanceTorrentView) bool {
	return matchesRequestedHashSet(excludeHashes, torrent.Hash, torrent.InfohashV1, torrent.InfohashV2) ||
		matchesExcludedTargetSet(excludeTargets, torrent.InstanceID, torrent.Hash, torrent.InfohashV1, torrent.InfohashV2)
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

func appendTargetsFromCrossInstanceTorrents(
	targetsByInstance map[int][]string,
	seen map[int]map[string]struct{},
	torrents []qbittorrent.CrossInstanceTorrentView,
	excludeHashes map[string]struct{},
	excludeTargets map[string]struct{},
) {
	for _, torrent := range torrents {
		if selectionExcluded(excludeHashes, excludeTargets, torrent) {
			continue
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

	normalizedInstanceIDs, instanceIDsErr := normalizeInstanceIDs(req.InstanceIDs)
	if instanceIDsErr != nil {
		RespondError(w, http.StatusBadRequest, instanceIDsErr.Error())
		return
	}
	req.InstanceIDs = normalizedInstanceIDs

	if message := torrentSelectionError(instanceID, req); message != "" {
		RespondError(w, http.StatusBadRequest, message)
		return
	}

	validActions := []string{
		"pause", "resume", "delete", "deleteWithFiles",
		"recheck", "reannounce", "increasePriority", "decreasePriority",
		"topPriority", "bottomPriority", "addTags", "removeTags", "setTags", "setComment", "setCategory",
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

	targetsByInstance, ok := h.resolveTorrentSelection(w, r, instanceID, req)
	if !ok {
		return
	}

	targetInstanceIDs := make([]int, 0, len(targetsByInstance))
	for targetInstanceID := range targetsByInstance {
		targetInstanceIDs = append(targetInstanceIDs, targetInstanceID)
	}
	slices.Sort(targetInstanceIDs)

	if slices.Contains(bulkActionMoves, req.Action) {
		for _, targetInstanceID := range targetInstanceIDs {
			if h.respondIfHeld(w, targetInstanceID, targetsByInstance[targetInstanceID]...) {
				return
			}
		}
	}

	for _, targetInstanceID := range targetInstanceIDs {
		hashes := targetsByInstance[targetInstanceID]
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

func torrentSelectionError(instanceID int, req BulkActionRequest) string {
	if !req.SelectAll && len(req.Hashes) == 0 && len(req.Targets) == 0 {
		return "No torrents selected"
	}
	if req.SelectAll && (len(req.Hashes) > 0 || len(req.Targets) > 0) {
		return "Cannot specify hashes/targets together with selectAll"
	}
	if instanceID == allInstancesID && len(req.Hashes) > 0 && len(req.Targets) == 0 {
		return "targets is required when addressing torrents by hash in the unified scope"
	}
	return ""
}

func (h *TorrentsHandler) resolveTorrentSelection(w http.ResponseWriter, r *http.Request, instanceID int, req BulkActionRequest) (map[int][]string, bool) {
	var targetsByInstance map[int][]string
	if req.SelectAll {
		targetsByInstance = make(map[int][]string)
		if req.Filters == nil {
			req.Filters = &qbittorrent.FilterOptions{}
		}

		torrents, selectErr := h.selectAllTorrents(r, false, instanceID, "added_on", "desc", req.Search, *req.Filters, req.InstanceIDs, req.ExcludeHashes, req.ExcludeTargets)
		if selectErr != nil {
			if instanceID != allInstancesID && respondIfInstanceDisabled(w, selectErr, instanceID, "torrents:selectAll") {
				return nil, false
			}
			if errors.Is(selectErr, errPartialResults) {
				log.Warn().Str("action", req.Action).Ints("instanceIDs", req.InstanceIDs).Msg("SelectAll bulk action aborted due to partial results")
				RespondError(w, http.StatusServiceUnavailable, "Unable to resolve all scoped instances for bulk action")
				return nil, false
			}
			log.Error().Err(selectErr).Int("instanceID", instanceID).Msg("Failed to get torrents for selectAll operation")
			RespondError(w, http.StatusInternalServerError, "Failed to get torrents for bulk action")
			return nil, false
		}

		appendTargetsFromCrossInstanceTorrents(targetsByInstance, make(map[int]map[string]struct{}), torrents, nil, nil)

		log.Debug().
			Int("instanceID", instanceID).
			Int("targetCount", len(torrents)).
			Int("excludedHashes", len(req.ExcludeHashes)).
			Int("excludedTargets", len(req.ExcludeTargets)).
			Str("action", req.Action).
			Msg("SelectAll bulk action")
	} else {
		targetsByInstance = explicitTargets(instanceID, req.Targets, req.Hashes)
	}

	if len(targetsByInstance) == 0 {
		RespondError(w, http.StatusBadRequest, "No torrents match the selection criteria")
		return nil, false
	}

	return targetsByInstance, true
}

func (h *TorrentsHandler) GetDeleteCrossSeeds(w http.ResponseWriter, r *http.Request) {
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
	normalizedInstanceIDs, instanceIDsErr := normalizeInstanceIDs(req.InstanceIDs)
	if instanceIDsErr != nil {
		RespondError(w, http.StatusBadRequest, instanceIDsErr.Error())
		return
	}
	req.InstanceIDs = normalizedInstanceIDs
	if message := torrentSelectionError(instanceID, req); message != "" {
		RespondError(w, http.StatusBadRequest, message)
		return
	}

	targetsByInstance, ok := h.resolveTorrentSelection(w, r, instanceID, req)
	if !ok {
		return
	}

	response, findErr := h.deleteMatches.FindDeleteMatches(r.Context(), targetsByInstance)
	if findErr != nil {
		if r.Context().Err() != nil {
			return
		}
		log.Error().Err(findErr).Int("instanceID", instanceID).Msg("Failed to check cross-seeds for delete")
		RespondError(w, mapCrossSeedErrorStatus(findErr), findErr.Error())
		return
	}

	hideMatchPasskeys(r, response.Matches)
	RespondJSON(w, http.StatusOK, response)
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
		return h.syncManager.SetTags(ctx, instanceID, hashes, req.Tags)
	case "setComment":
		return h.syncManager.SetComment(ctx, instanceID, hashes, req.Comment)
	case "setCategory":
		return h.syncManager.SetCategory(ctx, instanceID, hashes, req.Category)
	case "toggleAutoTMM":
		return h.syncManager.SetAutoTMM(ctx, instanceID, hashes, req.Enable)
	case "forceStart":
		return h.syncManager.SetForceStart(ctx, instanceID, hashes, req.Enable)
	case "setShareLimit":
		return h.syncManager.SetTorrentShareLimit(ctx, instanceID, hashes, req.RatioLimit, req.SeedingTimeLimit, req.InactiveSeedingTimeLimit, req.ShareLimitAction, req.ShareLimitsMode)
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
	case "delete", "deleteWithFiles":
		deleteFiles := req.Action == "deleteWithFiles" || req.DeleteFiles
		return h.deleter.DeleteTorrents(ctx, instanceID, hashes, deleteFiles, qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser})
	default:
		return h.syncManager.BulkAction(ctx, instanceID, hashes, req.Action)
	}
}

func (h *TorrentsHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

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

func (h *TorrentsHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
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

func (h *TorrentsHandler) EditCategory(w http.ResponseWriter, r *http.Request) {
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

func (h *TorrentsHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

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

func (h *TorrentsHandler) GetActiveTrackers(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	trackers, err := h.syncManager.GetActiveTrackers(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getActiveTrackers") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get active trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to get active trackers")
		return
	}

	if !middleware.CanReadTrackerPasskeys(r) {
		for domain, trackerURL := range trackers {
			trackers[domain] = qbittorrent.TrackerOrigin(trackerURL)
		}
	}
	RespondJSON(w, http.StatusOK, trackers)
}

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

func (h *TorrentsHandler) GetTorrentProperties(w http.ResponseWriter, r *http.Request) {
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

func (h *TorrentsHandler) GetTorrentTrackers(w http.ResponseWriter, r *http.Request) {
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

	trackers, err := h.syncManager.GetTorrentTrackers(r.Context(), instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getTrackers") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent trackers")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent trackers")
		return
	}

	if !middleware.CanReadTrackerPasskeys(r) {
		trackers = qbittorrent.TrackersWithoutPasskeys(trackers)
	}
	RespondJSON(w, http.StatusOK, trackers)
}

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

	if !middleware.CanReadTrackerPasskeys(r) {
		for i := range webseeds {
			webseeds[i].URL = qbittorrent.TrackerOrigin(webseeds[i].URL)
		}
	}
	RespondJSON(w, http.StatusOK, webseeds)
}

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

type EditTrackerRequest struct {
	OldURL string `json:"oldURL"`
	NewURL string `json:"newURL"`
}

func (h *TorrentsHandler) EditTorrentTracker(w http.ResponseWriter, r *http.Request) {
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

type AddTrackerRequest struct {
	URLs string `json:"urls"`
}

func (h *TorrentsHandler) AddTorrentTrackers(w http.ResponseWriter, r *http.Request) {
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

type RemoveTrackerRequest struct {
	URLs string `json:"urls"`
}

func (h *TorrentsHandler) RemoveTorrentTrackers(w http.ResponseWriter, r *http.Request) {
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

	if h.respondIfHeld(w, instanceID, hash) {
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

	if h.respondIfHeld(w, instanceID, hash) {
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

	if h.respondIfHeld(w, instanceID, hash) {
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

func (h *TorrentsHandler) GetTorrentPeers(w http.ResponseWriter, r *http.Request) {
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

	peers, err := h.syncManager.GetTorrentPeers(r.Context(), instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getPeers") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent peers")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent peers")
		return
	}

	sortedPeers := make([]SortedPeer, 0, len(peers.Peers))
	for key, peer := range peers.Peers {
		sortedPeers = append(sortedPeers, SortedPeer{
			Key:         key,
			TorrentPeer: peer,
		})
	}

	sort.Slice(sortedPeers, func(i, j int) bool {
		iIsSeeder := sortedPeers[i].Progress == 1.0
		jIsSeeder := sortedPeers[j].Progress == 1.0

		if iIsSeeder != jIsSeeder {
			return iIsSeeder
		}

		if sortedPeers[i].Progress != sortedPeers[j].Progress {
			return sortedPeers[i].Progress > sortedPeers[j].Progress
		}

		if sortedPeers[i].DownSpeed != sortedPeers[j].DownSpeed {
			return sortedPeers[i].DownSpeed > sortedPeers[j].DownSpeed
		}

		if sortedPeers[i].UpSpeed != sortedPeers[j].UpSpeed {
			return sortedPeers[i].UpSpeed > sortedPeers[j].UpSpeed
		}

		return sortedPeers[i].Key < sortedPeers[j].Key
	})

	response := &SortedPeersResponse{
		TorrentPeersResponse: peers,
		SortedPeers:          sortedPeers,
	}

	log.Trace().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Int("peerCount", len(sortedPeers)).
		Msg("Torrent peers response with sorted peers")

	RespondJSON(w, http.StatusOK, response)
}

func (h *TorrentsHandler) GetTorrentFiles(w http.ResponseWriter, r *http.Request) {
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

	refreshParam := strings.TrimSpace(r.URL.Query().Get("refresh"))
	forceRefresh := refreshParam != "" && refreshParam != "0" && !strings.EqualFold(refreshParam, "false")
	ctx := r.Context()
	if forceRefresh {
		ctx = qbittorrent.WithForceFilesRefresh(ctx)
	}

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

type SetTorrentFilePriorityRequest struct {
	Indices  []int `json:"indices"`
	Priority int   `json:"priority"`
}

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
		case errors.Is(err, qbt.ErrTorrentMetadataNotDownloadedYet):
			RespondError(w, http.StatusConflict, "Torrent metadata is not yet available. Try again once metadata has downloaded.")
		default:
			log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to update torrent file priority")
			RespondError(w, http.StatusInternalServerError, "Failed to update torrent file priority")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

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

	if _, err := w.Write(data); err != nil { //nolint:gosec // G705: a .torrent body served as an attachment with nosniff, not markup
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to write torrent export response")
	}
}

func (h *TorrentsHandler) AddPeers(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

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

func (h *TorrentsHandler) BanPeers(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

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
	if err := validateTorrentFilePath(req.TorrentFilePath); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
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

	if !middleware.CanReadTrackerPasskeys(r) {
		for i := range tasks {
			qbittorrent.HideTrackerURLs(tasks[i].Trackers)
			qbittorrent.HideTrackerURLs(tasks[i].URLSeeds)
		}
	}
	RespondJSON(w, http.StatusOK, tasks)
}

func (h *TorrentsHandler) GetActiveTaskCount(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	count := h.syncManager.GetActiveTaskCount(r.Context(), instanceID)
	RespondJSON(w, http.StatusOK, map[string]int{"count": count})
}

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

	filename := taskID + ".torrent"
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(data); err != nil { //nolint:gosec // G705: a .torrent body served as an attachment with nosniff, not markup
		log.Error().Err(err).Int("instanceID", instanceID).Str("taskID", taskID).Msg("Failed to write torrent file response")
	}
}

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

func (h *TorrentsHandler) ListCrossInstanceTorrents(w http.ResponseWriter, r *http.Request) {
	limit := 300
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

	var filters qbittorrent.FilterOptions
	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			log.Warn().Err(err).Msg("Failed to parse filters, ignoring")
		}
	}

	logEvent := log.Trace().
		Str("sort", sort).
		Str("order", order).
		Int("page", page).
		Int("limit", limit).
		Str("search", search)
	if len(instanceIDs) > 0 {
		logEvent = logEvent.Ints("instanceIDs", instanceIDs)
	}

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

	offset := page * limit

	response, err := h.syncManager.ListTorrents(r.Context(), qbittorrent.TorrentListView{
		Unified:            true,
		InstanceIDs:        instanceIDs,
		Limit:              limit,
		Offset:             offset,
		Sort:               sort,
		Order:              order,
		Search:             search,
		Filters:            filters,
		SidebarCounts:      true,
		CopyGroups:         groupCopiesParam(r),
		FullPasskeyAccess:  middleware.CanReadTrackerPasskeys(r),
		WithHelperTorrents: true,
	})
	if err != nil {
		if respondIfInstanceDisabled(w, err, 0, "torrents:listCrossInstance") {
			return
		}
		log.Error().Err(err).Msg("Failed to get cross-instance torrents")
		RespondError(w, http.StatusInternalServerError, "Failed to get cross-instance torrents")
		return
	}

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

	mode, ok := parseDirectoryContentMode(r.URL.Query().Get("mode"))
	if !ok {
		RespondError(w, http.StatusBadRequest, "Invalid mode")
		return
	}

	response, err := h.syncManager.GetDirectoryContentCtx(r.Context(), instanceID, dirPath, mode, withMetadata)
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

func parseDirectoryContentMode(raw string) (qbt.DirectoryContentMode, bool) {
	switch mode := qbt.DirectoryContentMode(strings.TrimSpace(raw)); mode {
	case "":
		return qbt.DirectoryContentDirs, true
	case qbt.DirectoryContentAll, qbt.DirectoryContentDirs, qbt.DirectoryContentFiles:
		return mode, true
	default:
		return "", false
	}
}

func (h *TorrentsHandler) requireContentAccess(w http.ResponseWriter, r *http.Request, instanceID int) bool {
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
	if !models.FilesystemCapabilitiesOf(instance).Content {
		RespondError(w, http.StatusForbidden, "Instance does not have local filesystem access enabled")
		return false
	}
	return true
}

func resolveTorrentFilePath(basePath, relativePath string) (string, error) {
	cleanBase := filepath.Clean(basePath)
	if !filepath.IsAbs(cleanBase) {
		return "", errors.New("base path must be absolute")
	}

	full := filepath.Join(cleanBase, filepath.FromSlash(relativePath))
	cleanFull := filepath.Clean(full)

	rel, err := filepath.Rel(cleanBase, cleanFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	// #nosec G703 -- cleanBase is validated as absolute and constrained by traversal checks above.
	if _, err := os.Lstat(cleanBase); err != nil {
		return "", fmt.Errorf("failed to access base path: %w", err)
	}

	evaluatedBase, err := filepath.EvalSymlinks(cleanBase)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base path symlinks: %w", err)
	}

	// #nosec G703 -- cleanFull is derived from a validated base path and traversal-checked relative input.
	if _, err := os.Lstat(cleanFull); err != nil {
		if os.IsNotExist(err) {
			return cleanFull, nil
		}
		return "", fmt.Errorf("failed to access candidate path: %w", err)
	}

	evaluatedFull, err := filepath.EvalSymlinks(cleanFull)
	if err != nil {
		return "", fmt.Errorf("failed to resolve candidate path symlinks: %w", err)
	}

	rel, err = filepath.Rel(evaluatedBase, evaluatedFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	return evaluatedFull, nil
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

type resolvedTorrentContentFile struct {
	InstanceID   int
	Hash         string
	FileIndex    int
	RelativePath string
	ResolvedPath string
}

func parseTorrentContentFileParams(w http.ResponseWriter, r *http.Request) (int, string, int, bool) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return 0, "", 0, false
	}

	hash := strings.TrimSpace(chi.URLParam(r, "hash"))
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Missing torrent hash")
		return 0, "", 0, false
	}

	fileIndex, err := strconv.Atoi(chi.URLParam(r, "fileIndex"))
	if err != nil || fileIndex < 0 {
		RespondError(w, http.StatusBadRequest, "Invalid file index")
		return 0, "", 0, false
	}

	return instanceID, hash, fileIndex, true
}

func fetchTorrentFilesAndPropsForContentFile(ctx context.Context, resolver torrentContentResolver, instanceID int, hash string, fileIndex int, context string, w http.ResponseWriter) (string, int, *qbt.TorrentProperties, bool) {
	files, err := resolver.GetTorrentFiles(ctx, instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, context) {
			return "", 0, nil, false
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent files")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent files")
		return "", 0, nil, false
	}
	if files == nil {
		RespondError(w, http.StatusNotFound, "Torrent files not found")
		return "", 0, nil, false
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
		return "", 0, nil, false
	}

	props, err := resolver.GetTorrentProperties(ctx, instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, context) {
			return "", 0, nil, false
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent properties")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return "", 0, nil, false
	}
	if props == nil {
		log.Error().Int("instanceID", instanceID).Str("hash", hash).Msg("Torrent properties are nil")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return "", 0, nil, false
	}

	return targetFileName, len(*files), props, true
}

func resolveTorrentContentFilePathOnDisk(ctx context.Context, resolver torrentContentResolver, instanceID int, hash string, props *qbt.TorrentProperties, targetFileName string, filesLen int, w http.ResponseWriter) (string, bool) {
	contentPath := ""
	if torrents, err := resolver.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: []string{hash}}); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("Failed to get torrent content path for fallback resolution")
	} else if len(torrents) > 0 {
		contentPath = torrents[0].ContentPath
	}

	candidates := filePathCandidates(props.SavePath, props.DownloadPath, contentPath, targetFileName, filesLen == 1)
	if len(candidates) == 0 {
		RespondError(w, http.StatusBadRequest, "Invalid file path")
		return "", false
	}

	for _, candidate := range candidates {
		// #nosec G703,G304 -- candidate is constructed from validated base paths via resolveTorrentFilePath.
		f, err := os.Open(candidate)
		if err != nil {
			continue
		}

		stat, err := f.Stat()
		if err != nil {
			if cerr := f.Close(); cerr != nil {
				log.Warn().Err(cerr).Str("candidate", candidate).Msg("Failed to close candidate file after stat error")
			}
			continue
		}
		if stat.IsDir() {
			if cerr := f.Close(); cerr != nil {
				log.Warn().Err(cerr).Str("candidate", candidate).Msg("Failed to close candidate directory handle")
			}
			continue
		}
		if cerr := f.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("candidate", candidate).Msg("Failed to close candidate file")
			continue
		}

		return candidate, true
	}

	RespondError(w, http.StatusNotFound, "File not found on disk")
	return "", false
}

func (h *TorrentsHandler) resolveTorrentContentFile(w http.ResponseWriter, r *http.Request, unavailableMessage, context string) (resolvedTorrentContentFile, bool) {
	instanceID, hash, fileIndex, ok := parseTorrentContentFileParams(w, r)
	if !ok {
		return resolvedTorrentContentFile{}, false
	}

	if !h.requireContentAccess(w, r, instanceID) {
		return resolvedTorrentContentFile{}, false
	}

	if h.contentResolver == nil {
		RespondError(w, http.StatusInternalServerError, unavailableMessage)
		return resolvedTorrentContentFile{}, false
	}

	targetFileName, filesLen, props, ok := fetchTorrentFilesAndPropsForContentFile(r.Context(), h.contentResolver, instanceID, hash, fileIndex, context, w)
	if !ok {
		return resolvedTorrentContentFile{}, false
	}

	resolvedPath, ok := resolveTorrentContentFilePathOnDisk(r.Context(), h.contentResolver, instanceID, hash, props, targetFileName, filesLen, w)
	if !ok {
		return resolvedTorrentContentFile{}, false
	}

	return resolvedTorrentContentFile{
		InstanceID:   instanceID,
		Hash:         hash,
		FileIndex:    fileIndex,
		RelativePath: targetFileName,
		ResolvedPath: resolvedPath,
	}, true
}

func (h *TorrentsHandler) DownloadTorrentContentFile(w http.ResponseWriter, r *http.Request) {
	resolved, ok := h.resolveTorrentContentFile(w, r, "Download service unavailable", "torrents:downloadContentFile")
	if !ok {
		return
	}

	// #nosec G703,G304 -- resolved.ResolvedPath is constructed from validated base paths via resolveTorrentFilePath.
	file, err := os.Open(resolved.ResolvedPath)
	if err != nil {
		RespondError(w, http.StatusNotFound, "File not found on disk")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		RespondError(w, http.StatusNotFound, "File not found on disk")
		return
	}
	if h.isInstallFile(info) {
		RespondError(w, http.StatusForbidden, "qui does not serve its own config or database files")
		return
	}

	filename := filepath.Base(resolved.RelativePath)

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

type torrentFileMediaInfoField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type torrentFileMediaInfoStream struct {
	Kind   string                      `json:"kind"`
	Fields []torrentFileMediaInfoField `json:"fields"`
}

type torrentFileMediaInfoResponse struct {
	FileIndex    int                          `json:"fileIndex"`
	RelativePath string                       `json:"relativePath"`
	Streams      []torrentFileMediaInfoStream `json:"streams"`
	RawJSON      string                       `json:"rawJSON"`
}

type contentPathMediaInfoResponse struct {
	ContentPath   string          `json:"contentPath"`
	SummaryTxt    string          `json:"summaryTxt"`
	MediaInfoJSON json.RawMessage `json:"mediaInfoJson"`
}

func mapReportToMediaInfoStreams(report mediainfo.Report) []torrentFileMediaInfoStream {
	streams := make([]torrentFileMediaInfoStream, 0, 1+len(report.Streams))
	generalFields := make([]torrentFileMediaInfoField, 0, len(report.General.Fields))
	for _, field := range report.General.Fields {
		generalFields = append(generalFields, torrentFileMediaInfoField{Name: field.Name, Value: field.Value})
	}
	streams = append(streams, torrentFileMediaInfoStream{
		Kind:   string(report.General.Kind),
		Fields: generalFields,
	})

	for _, stream := range report.Streams {
		fields := make([]torrentFileMediaInfoField, 0, len(stream.Fields))
		for _, field := range stream.Fields {
			fields = append(fields, torrentFileMediaInfoField{Name: field.Name, Value: field.Value})
		}
		streams = append(streams, torrentFileMediaInfoStream{
			Kind:   string(stream.Kind),
			Fields: fields,
		})
	}

	return streams
}

func normalizeContentPathRelativeInput(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("content path is required")
	}

	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	normalized := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(normalized) {
		return "", errors.New("absolute paths are not allowed")
	}
	if normalized == ".." || strings.HasPrefix(normalized, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	return normalized, nil
}

func contentPathCandidatesFromPreferences(prefs qbt.AppPreferences, relativePath string) []string {
	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{})

	if p, err := resolveTorrentFilePath(prefs.SavePath, relativePath); err == nil {
		candidates = appendUniqueCandidate(candidates, seen, p)
	}

	if prefs.TempPathEnabled {
		if p, err := resolveTorrentFilePath(prefs.TempPath, relativePath); err == nil {
			candidates = appendUniqueCandidate(candidates, seen, p)
		}
	}

	return candidates
}

func findExistingContentFile(candidates []string) (string, bool) {
	for _, candidate := range candidates {
		// #nosec G703,G304 -- candidate is constructed from validated base paths via resolveTorrentFilePath.
		f, err := os.Open(candidate)
		if err != nil {
			continue
		}

		stat, err := f.Stat()
		if err != nil {
			if cerr := f.Close(); cerr != nil {
				log.Warn().Err(cerr).Str("candidate", candidate).Msg("Failed to close candidate file after stat error")
			}
			continue
		}
		if stat.IsDir() {
			if cerr := f.Close(); cerr != nil {
				log.Warn().Err(cerr).Str("candidate", candidate).Msg("Failed to close candidate directory handle")
			}
			continue
		}
		if cerr := f.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("candidate", candidate).Msg("Failed to close candidate file")
			continue
		}

		return candidate, true
	}

	return "", false
}

func (h *TorrentsHandler) GetTorrentFileMediaInfo(w http.ResponseWriter, r *http.Request) {
	resolved, ok := h.resolveTorrentContentFile(w, r, "MediaInfo service unavailable", "torrents:getFileMediaInfo")
	if !ok {
		return
	}

	// #nosec G304 -- resolved.ResolvedPath is constructed from validated base paths via resolveTorrentFilePath.
	report, err := mediainfo.AnalyzeFile(resolved.ResolvedPath, mediainfo.WithParseSpeed(0.5))
	if err != nil {
		log.Error().Err(err).Int("instanceID", resolved.InstanceID).Str("hash", resolved.Hash).Int("fileIndex", resolved.FileIndex).Msg("Failed to analyze file with MediaInfo")
		RespondError(w, http.StatusInternalServerError, "Failed to analyze file")
		return
	}

	rawJSON, err := mediainfo.Render([]mediainfo.Report{report}, mediainfo.OutputJSON)
	if err != nil {
		log.Error().Err(err).Int("instanceID", resolved.InstanceID).Str("hash", resolved.Hash).Int("fileIndex", resolved.FileIndex).Msg("Failed to render MediaInfo JSON")
		RespondError(w, http.StatusInternalServerError, "Failed to render MediaInfo")
		return
	}

	RespondJSON(w, http.StatusOK, torrentFileMediaInfoResponse{
		FileIndex:    resolved.FileIndex,
		RelativePath: resolved.RelativePath,
		Streams:      mapReportToMediaInfoStreams(report),
		RawJSON:      rawJSON,
	})
}

func (h *TorrentsHandler) GetContentPathMediaInfo(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	if !h.requireContentAccess(w, r, instanceID) {
		return
	}

	rawContentPath := strings.TrimSpace(r.URL.Query().Get("contentPath"))
	if rawContentPath == "" {
		rawContentPath = strings.TrimSpace(r.URL.Query().Get("content_path"))
	}

	relativePath, err := normalizeContentPathRelativeInput(rawContentPath)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid content path")
		return
	}

	prefs, err := h.torrentAdder.GetAppPreferences(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "torrents:getContentPathMediaInfo") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences for MediaInfo")
		RespondError(w, http.StatusInternalServerError, "Failed to get app preferences")
		return
	}

	candidates := contentPathCandidatesFromPreferences(prefs, relativePath)
	if len(candidates) == 0 {
		RespondError(w, http.StatusBadRequest, "No content roots configured for instance")
		return
	}

	resolvedPath, ok := findExistingContentFile(candidates)
	if !ok {
		RespondError(w, http.StatusNotFound, "File not found on disk")
		return
	}

	// #nosec G304 -- resolvedPath is constructed from validated base paths via resolveTorrentFilePath.
	report, err := mediainfo.AnalyzeFile(resolvedPath, mediainfo.WithParseSpeed(0.5))
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("contentPath", relativePath).Msg("Failed to analyze file with MediaInfo")
		RespondError(w, http.StatusInternalServerError, "Failed to analyze file")
		return
	}

	summaryTxt, err := mediainfo.Render([]mediainfo.Report{report}, mediainfo.OutputText)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("contentPath", relativePath).Msg("Failed to render MediaInfo summary text")
		RespondError(w, http.StatusInternalServerError, "Failed to render MediaInfo")
		return
	}

	rawJSON, err := mediainfo.Render([]mediainfo.Report{report}, mediainfo.OutputJSON)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("contentPath", relativePath).Msg("Failed to render MediaInfo JSON")
		RespondError(w, http.StatusInternalServerError, "Failed to render MediaInfo")
		return
	}

	RespondJSON(w, http.StatusOK, contentPathMediaInfoResponse{
		ContentPath:   relativePath,
		SummaryTxt:    summaryTxt,
		MediaInfoJSON: json.RawMessage(rawJSON),
	})
}
