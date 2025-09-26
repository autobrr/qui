// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/services/trackericons"
)

// TrackerIconProvider defines the behaviour required to serve tracker icons.
type TrackerIconProvider interface {
	GetIcon(ctx context.Context, host, trackerURL string) (string, error)
}

// TrackerIconHandler serves cached tracker favicons via the API.
type TrackerIconHandler struct {
	service TrackerIconProvider
}

// NewTrackerIconHandler constructs a new handler for tracker icons.
func NewTrackerIconHandler(service TrackerIconProvider) *TrackerIconHandler {
	return &TrackerIconHandler{service: service}
}

// GetTrackerIcon resolves or fetches a tracker icon and streams it back to the client.
func (h *TrackerIconHandler) GetTrackerIcon(w http.ResponseWriter, r *http.Request) {
	tracker := strings.TrimSpace(chi.URLParam(r, "tracker"))
	if tracker == "" {
		http.Error(w, "tracker parameter is required", http.StatusBadRequest)
		return
	}

	trackerURL := r.URL.Query().Get("url")

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	iconPath, err := h.service.GetIcon(ctx, tracker, trackerURL)
	if err != nil {
		switch {
		case errors.Is(err, trackericons.ErrInvalidTrackerHost):
			http.Error(w, "invalid tracker host", http.StatusBadRequest)
		case errors.Is(err, trackericons.ErrIconNotFound):
			http.NotFound(w, r)
		case errors.Is(err, context.DeadlineExceeded):
			http.Error(w, "tracker icon fetch timed out", http.StatusGatewayTimeout)
		case errors.Is(err, context.Canceled):
			http.Error(w, "tracker icon fetch canceled", http.StatusRequestTimeout)
		default:
			http.Error(w, "failed to fetch tracker icon", http.StatusBadGateway)
		}
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, iconPath)
}
