// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/crossseed"
)

type ManualMatchProposalsRequest struct {
	InstanceID  int    `json:"instance_id"`
	TorrentData string `json:"torrent_data"`
	TargetHash  string `json:"target_hash,omitempty"`
}

type ManualMatchApplyRequest struct {
	InstanceID  int      `json:"instance_id"`
	TorrentData string   `json:"torrent_data"`
	TargetHash  string   `json:"target_hash"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (h *CrossSeedHandler) ManualAssembleCheck(w http.ResponseWriter, r *http.Request) {
	var req crossseed.ManualAssembleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	resp, err := h.service.CheckManualAssemble(r.Context(), &req)
	if err != nil {
		RespondError(w, mapCrossSeedErrorStatus(err), err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}

func (h *CrossSeedHandler) ManualAssembleApply(w http.ResponseWriter, r *http.Request) {
	var req crossseed.ManualAssembleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	resp, err := h.service.ApplyManualAssemble(context.WithoutCancel(r.Context()), &req)
	if err != nil {
		RespondError(w, mapCrossSeedErrorStatus(err), err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}

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
	}
	ctx := context.WithoutCancel(r.Context())
	settings, err := h.service.GetAutomationSettings(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Manual match apply: failed to load automation settings")
		RespondError(w, http.StatusInternalServerError, "Failed to load cross-seed settings")
		return
	}
	crossReq.InheritSourceTags = settings.InheritSourceTags
	crossReq.SkipPieceBoundarySafetyCheck = settings.SkipPieceBoundarySafetyCheck

	resp, err := h.service.CrossSeed(ctx, crossReq)
	if err != nil {
		log.Error().Err(err).Int("instanceID", req.InstanceID).Str("targetHash", req.TargetHash).Msg("Manual match apply failed")
		RespondError(w, mapCrossSeedErrorStatus(err), err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, resp)
}
