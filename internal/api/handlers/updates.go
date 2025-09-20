// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"

	"github.com/autobrr/qui/pkg/version"
	"github.com/go-chi/chi/v5"
)

type updateService interface {
	CheckUpdates(ctx context.Context)
	GetLatestRelease(ctx context.Context) *version.Release
}

// UpdateHandler exposes update check endpoints to the frontend.
type UpdateHandler struct {
	service updateService
}

// NewUpdateHandler constructs an UpdateHandler.
func NewUpdateHandler(service updateService) *UpdateHandler {
	return &UpdateHandler{service: service}
}

// RegisterRoutes configures update routes under /updates.
func (h *UpdateHandler) RegisterRoutes(r chi.Router) {
	r.Route("/updates", func(r chi.Router) {
		r.Get("/latest", h.getLatest)
		r.Get("/check", h.checkUpdates)
	})
}

func (h *UpdateHandler) getLatest(w http.ResponseWriter, r *http.Request) {
	latest := h.service.GetLatestRelease(r.Context())
	if latest == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	RespondJSON(w, http.StatusOK, latest)
}

func (h *UpdateHandler) checkUpdates(w http.ResponseWriter, r *http.Request) {
	h.service.CheckUpdates(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
