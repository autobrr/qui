// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"sync/atomic"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/update"
)

type restartRequester interface {
	Request() error
}

// SystemHandler serves the Restart and Self-update actions.
type SystemHandler struct {
	availability update.Availability
	restarter    restartRequester
	// busy is the one lock over every system action. A successful action ends
	// in a Restart, so only a failed action releases it.
	busy atomic.Bool
}

func NewSystemHandler(availability update.Availability, restarter restartRequester) *SystemHandler {
	return &SystemHandler{availability: availability, restarter: restarter}
}

// Restart checks the binary and returns 202. The serve loop then runs the
// graceful shutdown and restarts qui in place.
func (h *SystemHandler) Restart(w http.ResponseWriter, _ *http.Request) {
	if !h.availability.Restart {
		RespondError(w, http.StatusForbidden, "Restart is not available")
		return
	}
	if !h.busy.CompareAndSwap(false, true) {
		RespondError(w, http.StatusConflict, "An update or restart is already running")
		return
	}
	if err := h.restarter.Request(); err != nil {
		h.busy.Store(false)
		log.Error().Err(err).Msg("refused restart")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
