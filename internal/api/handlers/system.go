// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/update"
)

type restartRequester interface {
	Request() error
}

type selfUpdater interface {
	Install(ctx context.Context, tag string) (update.Result, error)
}

// SystemHandler serves the Restart and Self-update actions.
type SystemHandler struct {
	availability update.Availability
	restarter    restartRequester
	updater      selfUpdater
	// busy is the one lock over every system action. A successful action ends
	// in a Restart, so only a failed action releases it.
	busy atomic.Bool
}

func NewSystemHandler(availability update.Availability, restarter restartRequester, updater selfUpdater) *SystemHandler {
	return &SystemHandler{availability: availability, restarter: restarter, updater: updater}
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

// SelfUpdateRequest carries the tag that the dialog showed.
type SelfUpdateRequest struct {
	Version string `json:"version"`
}

// Update installs the requested release, returns 200, then restarts qui. On an
// error the binary and the process do not change.
func (h *SystemHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !h.availability.SelfUpdate {
		RespondError(w, http.StatusForbidden, "Self-update is not available")
		return
	}
	var req SelfUpdateRequest
	err := json.UnmarshalRead(r.Body, &req)
	if err == nil {
		// Strict: go-selfupdate matches the tag as an exact string, so a loose
		// "1.31" would pass here and then miss every release.
		_, err = semver.StrictNewVersion(strings.TrimPrefix(req.Version, "v"))
	}
	if err != nil {
		RespondError(w, http.StatusBadRequest, "version must be a release tag, such as v1.31.0")
		return
	}
	if !h.busy.CompareAndSwap(false, true) {
		RespondError(w, http.StatusConflict, "An update or restart is already running")
		return
	}

	result, err := h.updater.Install(r.Context(), req.Version)
	if err != nil {
		h.busy.Store(false)
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, update.ErrReleaseNotFound):
			status = http.StatusNotFound
		case errors.Is(err, update.ErrNotNewer):
			status = http.StatusConflict
		case errors.Is(err, update.ErrSwap):
			status = http.StatusInternalServerError
		}
		log.Error().Err(err).Str("version", req.Version).Msg("self-update failed")
		RespondError(w, status, err.Error())
		return
	}

	log.Info().Str("version", result.Version).Str("backupError", result.BackupError).Msg("self-update installed, restarting")
	RespondJSON(w, http.StatusOK, result)
	// The graceful shutdown waits for this response to finish.
	if err := h.restarter.Request(); err != nil {
		h.busy.Store(false)
		log.Error().Err(err).Msg("self-update installed, but the restart was refused")
	}
}
