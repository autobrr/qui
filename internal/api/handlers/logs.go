// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// LogsHandler handles log-related API endpoints
type LogsHandler struct {
	logCache *qbittorrent.LogCache
}

// NewLogsHandler creates a new logs handler
func NewLogsHandler(logCache *qbittorrent.LogCache) *LogsHandler {
	return &LogsHandler{
		logCache: logCache,
	}
}

// LogResponse represents the API response for logs
type LogResponse struct {
	Logs    any  `json:"logs"`
	Total   int  `json:"total"`
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"hasMore"`
}

// GetMainLogs handles GET /api/instances/{id}/logs/main
func (h *LogsHandler) GetMainLogs(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		log.Error().Err(err).Str("instance_id", instanceIDStr).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	page := 0
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed >= 0 {
			page = parsed
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	search := r.URL.Query().Get("search")

	// Parse log levels
	var levels []int
	if levelsParam := r.URL.Query()["levels[]"]; len(levelsParam) > 0 {
		for _, level := range levelsParam {
			if l, err := strconv.Atoi(level); err == nil {
				levels = append(levels, l)
			}
		}
	} else if levelsParam := r.URL.Query().Get("levels"); levelsParam != "" {
		// Handle comma-separated levels
		for level := range strings.SplitSeq(levelsParam, ",") {
			if l, err := strconv.Atoi(strings.TrimSpace(level)); err == nil {
				levels = append(levels, l)
			}
		}
	}

	// Get logs from cache
	logs, total, err := h.logCache.GetMainLogs(instanceID, page, limit, search, levels)
	if err != nil {
		log.Error().Err(err).Int("instance_id", instanceID).Msg("Failed to get main logs")
		http.Error(w, "Failed to get logs", http.StatusInternalServerError)
		return
	}

	// Prepare response
	response := LogResponse{
		Logs:    logs,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: (page+1)*limit < total,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error().Err(err).Msg("Failed to encode response")
	}
}

// GetPeerLogs handles GET /api/instances/{id}/logs/peers
func (h *LogsHandler) GetPeerLogs(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		log.Error().Err(err).Str("instance_id", instanceIDStr).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	page := 0
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed >= 0 {
			page = parsed
		}
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	search := r.URL.Query().Get("search")

	// Get logs from cache
	logs, total, err := h.logCache.GetPeerLogs(instanceID, page, limit, search)
	if err != nil {
		log.Error().Err(err).Int("instance_id", instanceID).Msg("Failed to get peer logs")
		http.Error(w, "Failed to get logs", http.StatusInternalServerError)
		return
	}

	// Prepare response
	response := LogResponse{
		Logs:    logs,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: (page+1)*limit < total,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error().Err(err).Msg("Failed to encode response")
	}
}
