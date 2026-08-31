// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/crossseed"
)

// ManualMatchProposalsRequest asks for ranked Manual match target proposals.
type ManualMatchProposalsRequest struct {
	InstanceID int `json:"instance_id"`
	// TorrentData is the base64-encoded uploaded .torrent file.
	TorrentData string `json:"torrent_data"`
	// TargetHash, when set, is always included in the proposals with its
	// computed overlap, even at zero overlap.
	TargetHash string `json:"target_hash,omitempty"`
}

// ManualMatchApplyRequest applies a Manual match through the cross-seed pipeline.
type ManualMatchApplyRequest struct {
	InstanceID  int      `json:"instance_id"`
	TorrentData string   `json:"torrent_data"`
	TargetHash  string   `json:"target_hash"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	StartPaused *bool    `json:"start_paused,omitempty"`
}

// ManualMatchProposals godoc
// @Summary Rank Manual match target proposals for an uploaded torrent
// @Description Ranks same-instance torrents by file-size overlap with the uploaded torrent and returns dialog prefill values.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body ManualMatchProposalsRequest true "Uploaded torrent and instance"
// @Success 200 {object} crossseed.ManualMatchProposalsResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/manual/proposals [post]
func (h *CrossSeedHandler) ManualMatchProposals(w http.ResponseWriter, r *http.Request) {
	var req ManualMatchProposalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.InstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instance_id must be a positive integer")
		return
	}
	if strings.TrimSpace(req.TorrentData) == "" {
		RespondError(w, http.StatusBadRequest, "torrent_data is required")
		return
	}

	resp, err := h.service.ManualMatchProposalsFromBase64(r.Context(), req.InstanceID, req.TorrentData, req.TargetHash)
	if err != nil {
		log.Error().Err(err).Int("instanceID", req.InstanceID).Msg("Failed to build manual match proposals")
		RespondError(w, mapCrossSeedErrorStatus(err), err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}

// ManualMatchApply godoc
// @Summary Apply a Manual match cross-seed against a chosen target torrent
// @Description Adds the uploaded torrent through the cross-seed pipeline, pinned to the user-chosen target. The recheck arbitrates a wrong pick.
// @Tags cross-seed
// @Accept json
// @Produce json
// @Param request body ManualMatchApplyRequest true "Uploaded torrent, instance, and chosen target"
// @Success 200 {object} crossseed.CrossSeedResponse
// @Failure 400 {object} httphelpers.ErrorResponse
// @Failure 500 {object} httphelpers.ErrorResponse
// @Security ApiKeyAuth
// @Router /api/cross-seed/manual/apply [post]
func (h *CrossSeedHandler) ManualMatchApply(w http.ResponseWriter, r *http.Request) {
	var req ManualMatchApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.InstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "instance_id must be a positive integer")
		return
	}
	if strings.TrimSpace(req.TorrentData) == "" {
		RespondError(w, http.StatusBadRequest, "torrent_data is required")
		return
	}
	if strings.TrimSpace(req.TargetHash) == "" {
		RespondError(w, http.StatusBadRequest, "target_hash is required")
		return
	}

	crossReq := &crossseed.CrossSeedRequest{
		TorrentData:       req.TorrentData,
		TargetInstanceIDs: []int{req.InstanceID},
		ManualTargetHash:  req.TargetHash,
		Category:          req.Category,
		Tags:              req.Tags,
		StartPaused:       req.StartPaused,
	}
	// Full pipeline parity with the other interactive flows: honor the saved
	// tag-inheritance and piece-boundary preferences. SkipRecheck is not
	// propagated because the recheck is the arbiter of a Manual match.
	if settings, err := h.service.GetAutomationSettings(r.Context()); err == nil && settings != nil {
		crossReq.InheritSourceTags = settings.InheritSourceTags
		crossReq.SkipPieceBoundarySafetyCheck = settings.SkipPieceBoundarySafetyCheck
	} else if err != nil {
		log.Warn().Err(err).Msg("Manual match apply: failed to load automation settings, using defaults")
	}

	resp, err := h.service.CrossSeed(r.Context(), crossReq)
	if err != nil {
		log.Error().Err(err).Int("instanceID", req.InstanceID).Str("targetHash", req.TargetHash).Msg("Manual match apply failed")
		RespondError(w, mapCrossSeedErrorStatus(err), err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}
