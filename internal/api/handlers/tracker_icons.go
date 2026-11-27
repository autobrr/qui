// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"
	"time"
)

type TrackerIconProvider interface {
	GetIcon(ctx context.Context, host, trackerURL string) (string, error)
	ListIcons(ctx context.Context) (map[string]string, error)
}

type TrackerIconHandler struct {
	service TrackerIconProvider
}

func NewTrackerIconHandler(service TrackerIconProvider) *TrackerIconHandler {
	return &TrackerIconHandler{service: service}
}

func (h *TrackerIconHandler) GetTrackerIcons(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	icons, err := h.service.ListIcons(ctx)
	if err != nil {
		http.Error(w, "failed to list tracker icons", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	RespondJSON(w, r, http.StatusOK, icons)
}
