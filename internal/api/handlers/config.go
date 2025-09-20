// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/update"
	"github.com/go-chi/chi/v5"
)

// ConfigHandler exposes application configuration for the frontend.
type ConfigHandler struct {
	cfg     *config.AppConfig
	version string
	updates *update.Service
}

// ConfigResponse represents the configuration payload returned to clients.
type ConfigResponse struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	BaseURL         string `json:"base_url"`
	LogLevel        string `json:"log_level"`
	LogPath         string `json:"log_path"`
	CheckForUpdates bool   `json:"check_for_updates"`
	Version         string `json:"version"`
}

// ConfigUpdateRequest represents supported configuration updates from the UI.
type ConfigUpdateRequest struct {
	CheckForUpdates *bool `json:"check_for_updates"`
}

// NewConfigHandler creates a ConfigHandler instance.
func NewConfigHandler(cfg *config.AppConfig, version string, updates *update.Service) *ConfigHandler {
	return &ConfigHandler{cfg: cfg, version: version, updates: updates}
}

// RegisterRoutes wires handler routes under /config.
func (h *ConfigHandler) RegisterRoutes(r chi.Router) {
	r.Route("/config", func(r chi.Router) {
		r.Get("/", h.getConfig)
		r.Patch("/", h.updateConfig)
	})
}

func (h *ConfigHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	RespondJSON(w, http.StatusOK, ConfigResponse{
		Host:            h.cfg.Config.Host,
		Port:            h.cfg.Config.Port,
		BaseURL:         h.cfg.Config.BaseURL,
		LogLevel:        h.cfg.Config.LogLevel,
		LogPath:         h.cfg.Config.LogPath,
		CheckForUpdates: h.cfg.Config.CheckForUpdates,
		Version:         h.version,
	})
}

func (h *ConfigHandler) updateConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CheckForUpdates != nil {
		h.cfg.Config.CheckForUpdates = *req.CheckForUpdates
		if h.updates != nil {
			h.updates.SetEnabled(*req.CheckForUpdates)
		}
	}

	if err := h.cfg.UpdateConfig(); err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
