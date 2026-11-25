// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"net/http"

	"github.com/autobrr/qui/internal/update"
	"github.com/autobrr/qui/pkg/version"
)

type latestReleaseProvider interface {
	GetLatestRelease(ctx context.Context) *version.Release
}

type VersionHandler struct {
	updateService  latestReleaseProvider
	currentVersion string
	availability   update.Availability
}

func NewVersionHandler(updateService latestReleaseProvider, currentVersion string, availability update.Availability) *VersionHandler {
	return &VersionHandler{
		updateService:  updateService,
		currentVersion: currentVersion,
		availability:   availability,
	}
}

type LatestVersionResponse struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name,omitempty"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

type VersionResponse struct {
	Version                     string `json:"version"`
	LatestVersion               string `json:"latestVersion,omitempty"`
	UpdateAvailable             bool   `json:"updateAvailable"`
	SelfUpdate                  bool   `json:"selfUpdate"`
	SelfUpdateUnavailableReason string `json:"selfUpdateUnavailableReason"`
	Restart                     bool   `json:"restart"`
}

func (h *VersionHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	response := VersionResponse{
		Version:                     h.currentVersion,
		SelfUpdate:                  h.availability.SelfUpdate,
		SelfUpdateUnavailableReason: h.availability.SelfUpdateUnavailableReason,
		Restart:                     h.availability.Restart,
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

	RespondJSON(w, http.StatusOK, response)
}
