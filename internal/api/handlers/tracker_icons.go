// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// TrackerIconProvider defines the behaviour required to serve tracker icons.
type TrackerIconProvider interface {
	GetIcon(ctx context.Context, host, trackerURL string) (string, error)
	ListIcons(ctx context.Context) (map[string]string, error)
}

// TrackerIconHandler serves cached tracker favicons via the API.
type TrackerIconHandler struct {
	service TrackerIconProvider
}

// NewTrackerIconHandler constructs a new handler for tracker icons.
func NewTrackerIconHandler(service TrackerIconProvider) *TrackerIconHandler {
	return &TrackerIconHandler{service: service}
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
