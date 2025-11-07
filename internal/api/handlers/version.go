// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/autobrr/qui/internal/update"
	"github.com/rs/zerolog/log"
)

type VersionHandler struct {
	updateService *update.Service
}

func NewVersionHandler(updateService *update.Service) *VersionHandler {
	return &VersionHandler{
		updateService: updateService,
	}
}

type LatestVersionResponse struct {
	TagName             string `json:"tag_name"`
	Name                string `json:"name,omitempty"`
	Body                string `json:"body,omitempty"`
	HTMLURL             string `json:"html_url"`
	PublishedAt         string `json:"published_at"`
	SelfUpdateSupported bool   `json:"self_update_supported"`
}

func (h *VersionHandler) GetLatestVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	release := h.updateService.GetLatestRelease(ctx)
	if release == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	response := LatestVersionResponse{
		TagName:             release.TagName,
		HTMLURL:             release.HTMLURL,
		PublishedAt:         release.PublishedAt.Format("2006-01-02T15:04:05Z"),
		SelfUpdateSupported: h.updateService.CanSelfUpdate(),
	}

	if release.Name != nil {
		response.Name = *release.Name
	}

	if release.Body != nil {
		response.Body = *release.Body
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// TriggerSelfUpdate downloads the latest release and schedules a restart when supported.
func (h *VersionHandler) TriggerSelfUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := context.WithoutCancel(r.Context())

	if !h.updateService.CanSelfUpdate() {
		http.Error(w, update.ErrSelfUpdateUnsupported.Error(), http.StatusBadRequest)
		return
	}

	if err := h.updateService.RunSelfUpdate(ctx); err != nil {
		if errors.Is(err, update.ErrSelfUpdateUnsupported) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		log.Error().Err(err).Msg("failed to run self-update")
		http.Error(w, "failed to run self-update", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Update installed. qui will restart shortly.",
	})

	go func() {
		time.Sleep(2 * time.Second)
		log.Info().Msg("restarting process after self-update")

		// Get the executable path
		execPath, err := os.Executable()
		if err != nil {
			log.Error().Err(err).Msg("failed to get executable path for restart")
			os.Exit(1)
			return
		}

		// Get the executable path, resolving any symlinks
		execPath, err = exec.LookPath(execPath)
		if err != nil {
			log.Error().Err(err).Msg("failed to resolve executable path")
			os.Exit(1)
			return
		}

		// Restart the process with the same arguments
		err = syscall.Exec(execPath, os.Args, os.Environ())
		if err != nil {
			log.Error().Err(err).Msg("failed to restart process after update")
			os.Exit(1)
		}
	}()
}
