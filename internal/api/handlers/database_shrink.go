// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/dbshrink"
)

type DatabaseShrinkHandler struct {
	service *dbshrink.Service
}

func NewDatabaseShrinkHandler(service *dbshrink.Service) *DatabaseShrinkHandler {
	return &DatabaseShrinkHandler{service: service}
}

// Status returns the size of the database file, the free disk space, the need
// of a shrink, and the outcome of the shrink at the last start.
// GET /api/database/shrink
func (h *DatabaseShrinkHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.Status(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("database shrink: failed to read the status")
		RespondError(w, http.StatusInternalServerError, "Failed to read the database shrink status")
		return
	}
	RespondJSON(w, http.StatusOK, status)
}

type databaseShrinkResponse struct {
	Restarting bool `json:"restarting"`
}

// Request stores the "shrink pending" mark and restarts qui. Where Restart is
// not available, the shrink runs at the next start.
// POST /api/database/shrink
func (h *DatabaseShrinkHandler) Request(w http.ResponseWriter, r *http.Request) {
	restarting, err := h.service.Request(r.Context())
	switch {
	case errors.Is(err, dbshrink.ErrUnsupported), errors.Is(err, dbshrink.ErrTooLittleDisk), errors.Is(err, dbshrink.ErrRestartBusy):
		RespondError(w, http.StatusConflict, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("database shrink: failed to request the shrink")
		RespondError(w, http.StatusInternalServerError, err.Error())
	default:
		RespondJSON(w, http.StatusAccepted, databaseShrinkResponse{Restarting: restarting})
	}
}

// DismissResult forgets the outcome of the last shrink after the web UI showed it.
// DELETE /api/database/shrink/result
func (h *DatabaseShrinkHandler) DismissResult(w http.ResponseWriter, _ *http.Request) {
	h.service.DismissResult()
	w.WriteHeader(http.StatusNoContent)
}
