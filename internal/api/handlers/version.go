// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/autobrr/qui/internal/update"
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
	TagName     string `json:"tag_name"`
	Name        string `json:"name,omitempty"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

func (h *VersionHandler) GetLatestVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	release := h.updateService.GetLatestRelease(ctx)
	if release == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	response := LatestVersionResponse{
		TagName:     release.TagName,
		HTMLURL:     release.HTMLURL,
		PublishedAt: release.PublishedAt.Format("2006-01-02T15:04:05Z"),
	}

	if release.Name != nil {
		response.Name = *release.Name
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
