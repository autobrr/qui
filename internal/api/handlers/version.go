// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/autobrr/qui/pkg/version"
)

// latestReleaseProvider exposes the cached latest release discovered by the
// update service. It is satisfied by *update.Service and kept intentionally
// small so the version handler can be exercised without a live update check.
type latestReleaseProvider interface {
	GetLatestRelease(ctx context.Context) *version.Release
}

type VersionHandler struct {
	updateService  latestReleaseProvider
	currentVersion string
}

func NewVersionHandler(updateService latestReleaseProvider, currentVersion string) *VersionHandler {
	return &VersionHandler{
		updateService:  updateService,
		currentVersion: currentVersion,
	}
}

type LatestVersionResponse struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name,omitempty"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

// VersionResponse reports the version qui is currently running plus whether a
// newer release has been detected. Intended for monitoring tools (e.g. Argus)
// that track the deployed version of a service.
type VersionResponse struct {
	Version         string `json:"version"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// GetVersion returns the version qui is currently running. When the update
// service has detected a newer release, LatestVersion and UpdateAvailable are
// populated from the cached result; otherwise UpdateAvailable is false and
// LatestVersion is omitted.
func (h *VersionHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	response := VersionResponse{
		Version: h.currentVersion,
	}

	if release := h.updateService.GetLatestRelease(r.Context()); release != nil {
		response.LatestVersion = release.TagName
		response.UpdateAvailable = true
	}

	RespondJSON(w, http.StatusOK, response)
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
	_ = json.NewEncoder(w).Encode(response)
}
