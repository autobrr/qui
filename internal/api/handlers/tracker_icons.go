// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/autobrr/qui/internal/config"
)

// TrackerIconProvider defines the behaviour required to serve tracker icons.
type TrackerIconProvider interface {
	GetIcon(ctx context.Context, host, trackerURL string) (string, error)
	ListIcons(ctx context.Context) (map[string]string, error)
}

// TrackerIconManager extends TrackerIconProvider with configuration control.
type TrackerIconManager interface {
	TrackerIconProvider
	FetchEnabled() bool
	SetFetchEnabled(enabled bool)
}

// TrackerIconHandler serves cached tracker favicons via the API.
type TrackerIconHandler struct {
	service TrackerIconManager
	config  *config.AppConfig
}

// NewTrackerIconHandler constructs a new handler for tracker icons.
func NewTrackerIconHandler(service TrackerIconManager, cfg *config.AppConfig) *TrackerIconHandler {
	return &TrackerIconHandler{
		service: service,
		config:  cfg,
	}
}

// GetTrackerIcons returns all cached tracker icons as a JSON map.
func (h *TrackerIconHandler) GetTrackerIcons(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	icons, err := h.service.ListIcons(ctx)
	if err != nil {
		http.Error(w, "failed to list tracker icons", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	if err := json.NewEncoder(w).Encode(icons); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

type trackerIconSettingsResponse struct {
	FetchEnabled bool `json:"fetchEnabled"`
}

type trackerIconSettingsRequest struct {
	FetchEnabled bool `json:"fetchEnabled"`
}

// GetTrackerIconSettings returns the current tracker icon configuration flags.
func (h *TrackerIconHandler) GetTrackerIconSettings(w http.ResponseWriter, _ *http.Request) {
	if h == nil || h.service == nil {
		RespondError(w, http.StatusInternalServerError, "tracker icon service not available")
		return
	}

	RespondJSON(w, http.StatusOK, trackerIconSettingsResponse{
		FetchEnabled: h.service.FetchEnabled(),
	})
}

// UpdateTrackerIconSettings updates the tracker icon configuration flags.
func (h *TrackerIconHandler) UpdateTrackerIconSettings(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		RespondError(w, http.StatusInternalServerError, "tracker icon service not available")
		return
	}

	var req trackerIconSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if h.config != nil {
		if err := h.config.UpdateTrackerIconsFetchEnabled(req.FetchEnabled); err != nil {
			RespondError(w, http.StatusInternalServerError, "failed to update tracker icon settings")
			return
		}
	}

	h.service.SetFetchEnabled(req.FetchEnabled)

	RespondJSON(w, http.StatusOK, trackerIconSettingsResponse{
		FetchEnabled: h.service.FetchEnabled(),
	})
}
