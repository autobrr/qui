// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/discscan"
)

// DiscScanHandler starts, lists, and cancels BDInfo scans of Discs.
type DiscScanHandler struct {
	service     *discscan.Service
	store       *models.DiscScanStore
	resolver    torrentContentResolver
	backendPool *fsops.Pool
}

func NewDiscScanHandler(service *discscan.Service, store *models.DiscScanStore, resolver torrentContentResolver, backendPool *fsops.Pool) *DiscScanHandler {
	return &DiscScanHandler{service: service, store: store, resolver: resolver, backendPool: backendPool}
}

type discScanStartRequest struct {
	DiscPath string `json:"discPath"`
	Force    bool   `json:"force"`
}

// Start queues a Disc scan or returns the cached report.
// POST /api/instances/{instanceID}/torrents/{hash}/disc-scans
func (h *DiscScanHandler) Start(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	hash := strings.TrimSpace(chi.URLParam(r, "hash"))
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Missing torrent hash")
		return
	}

	var req discScanStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	discPath, ok := normalizeDiscPath(req.DiscPath)
	if !ok {
		RespondError(w, http.StatusBadRequest, "Invalid disc path")
		return
	}

	ctx := r.Context()
	backend, err := h.backendPool.GetBackend(ctx, instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("discscan: failed to get filesystem backend")
		RespondError(w, http.StatusInternalServerError, "Failed to look up instance")
		return
	}

	files, err := h.resolver.GetTorrentFiles(ctx, instanceID, hash)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "discscan:start") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("discscan: failed to get torrent files")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent files")
		return
	}
	if files == nil || len(*files) == 0 {
		RespondError(w, http.StatusNotFound, "Torrent files not found")
		return
	}
	wantDir, ok := discKind(*files, discPath)
	if !ok {
		RespondError(w, http.StatusBadRequest, "Path is not a Disc")
		return
	}

	props, err := h.resolver.GetTorrentProperties(ctx, instanceID, hash)
	if err != nil || props == nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("discscan: failed to get torrent properties")
		RespondError(w, http.StatusInternalServerError, "Failed to get torrent properties")
		return
	}
	contentPath := ""
	if torrents, err := h.resolver.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: []string{hash}}); err == nil && len(torrents) > 0 {
		contentPath = torrents[0].ContentPath
	}

	// The save path Stat is the access gate: the noop backend of an instance
	// without filesystem access fails it with ErrNoFilesystemAccess.
	if _, err := backend.Stat(ctx, props.SavePath); err != nil {
		if errors.Is(err, fsops.ErrNoFilesystemAccess) {
			RespondError(w, http.StatusForbidden, "Instance does not have filesystem access")
			return
		}
		RespondError(w, http.StatusNotFound, "Disc not found on disk")
		return
	}

	var resolved string
	for _, candidate := range filePathCandidates(props.SavePath, props.DownloadPath, contentPath, discPath, len(*files) == 1) {
		info, err := backend.Stat(ctx, candidate)
		if err != nil || info.IsDir != wantDir {
			continue
		}
		resolved = candidate
		break
	}
	if resolved == "" {
		RespondError(w, http.StatusNotFound, "Disc not found on disk")
		return
	}

	run, err := h.service.Enqueue(ctx, instanceID, hash, discPath, resolved, req.Force)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("discscan: failed to queue scan")
		RespondError(w, http.StatusInternalServerError, "Failed to queue disc scan")
		return
	}
	RespondJSON(w, http.StatusOK, run)
}

// ListForTorrent returns the newest run per Disc path of one torrent.
// GET /api/instances/{instanceID}/torrents/{hash}/disc-scans
func (h *DiscScanHandler) ListForTorrent(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}
	hash := strings.TrimSpace(chi.URLParam(r, "hash"))
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Missing torrent hash")
		return
	}

	runs, err := h.store.ListNewestForTorrent(r.Context(), instanceID, hash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("discscan: failed to list runs")
		RespondError(w, http.StatusInternalServerError, "Failed to list disc scans")
		return
	}
	RespondJSON(w, http.StatusOK, runs)
}

// Get returns one run.
// GET /api/instances/{instanceID}/disc-scans/{runID}
func (h *DiscScanHandler) Get(w http.ResponseWriter, r *http.Request) {
	instanceID, runID, ok := parseDiscScanParams(w, r)
	if !ok {
		return
	}

	run, err := h.store.GetByInstance(r.Context(), instanceID, runID)
	if err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("discscan: failed to get run")
		RespondError(w, http.StatusInternalServerError, "Failed to get disc scan")
		return
	}
	if run == nil {
		RespondError(w, http.StatusNotFound, "Disc scan not found")
		return
	}
	RespondJSON(w, http.StatusOK, run)
}

// Cancel stops a pending or scanning run.
// POST /api/instances/{instanceID}/disc-scans/{runID}/cancel
func (h *DiscScanHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	instanceID, runID, ok := parseDiscScanParams(w, r)
	if !ok {
		return
	}

	run, err := h.service.Cancel(r.Context(), instanceID, runID)
	switch {
	case errors.Is(err, discscan.ErrRunNotFound):
		RespondError(w, http.StatusNotFound, "Disc scan not found")
		return
	case errors.Is(err, discscan.ErrRunFinished):
		RespondError(w, http.StatusConflict, "Disc scan already finished")
		return
	case err != nil:
		log.Error().Err(err).Int64("runID", runID).Msg("discscan: failed to cancel run")
		RespondError(w, http.StatusInternalServerError, "Failed to cancel disc scan")
		return
	}
	RespondJSON(w, http.StatusOK, run)
}

func parseDiscScanParams(w http.ResponseWriter, r *http.Request) (int, int64, bool) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return 0, 0, false
	}
	runID, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil || runID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return 0, 0, false
	}
	return instanceID, runID, true
}

// normalizeDiscPath returns the slash-form, torrent-relative Disc path. "." is
// the torrent root, for a torrent whose files start with BDMV/. Absolute paths
// (POSIX, drive letter, UNC) and traversal are rejected on every OS.
func normalizeDiscPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	cleaned := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	switch {
	case strings.HasPrefix(cleaned, "/"), cleaned == "..", strings.HasPrefix(cleaned, "../"):
		return "", false
	case len(cleaned) >= 2 && cleaned[1] == ':':
		return "", false
	}
	return cleaned, true
}

// discKind reports whether discPath names a Disc in the torrent: an .iso file
// in the file list, or a folder that holds a BDMV folder. isDir tells which.
// This is the BDMV half of crossseed.isDiscLayoutTorrent, applied to one
// folder instead of the whole torrent; VIDEO_TS is not a Disc (no DVD reports).
func discKind(files qbt.TorrentFiles, discPath string) (isDir bool, ok bool) {
	prefix := discPath + "/"
	if discPath == "." {
		prefix = ""
	}
	for _, f := range files {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if name == discPath && strings.EqualFold(path.Ext(name), ".iso") {
			return false, true
		}
		if strings.HasPrefix(name, prefix+"BDMV/") {
			return true, true
		}
	}
	return false, false
}
