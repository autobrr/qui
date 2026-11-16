// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/transfer"
)

type TransfersHandler struct {
	service *transfer.Service
}

func NewTransfersHandler(service *transfer.Service) *TransfersHandler {
	return &TransfersHandler{service: service}
}

type startTransferRequest struct {
	SourceInstanceID int            `json:"sourceInstanceId"`
	Hash             string         `json:"hash"`
	TargetInstanceID int            `json:"targetInstanceId"`
	SavePath         string         `json:"savePath"`
	Category         string         `json:"category"`
	Tags             []string       `json:"tags"`
	After            transfer.After `json:"after"`
	LeaveStopped     bool           `json:"leaveStopped"`
}

type startTransferResponse struct {
	ID int64 `json:"id"`
}

// Start starts the Transfer of one torrent. It answers before the copy
// starts.
// POST /api/transfers
func (h *TransfersHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req startTransferRequest
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.Hash) == "" || req.SourceInstanceID <= 0 || req.TargetInstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "sourceInstanceId, hash, and targetInstanceId are required")
		return
	}

	id, err := h.service.Start(r.Context(), transfer.Request{
		SourceInstanceID: req.SourceInstanceID,
		Hash:             req.Hash,
		TargetInstanceID: req.TargetInstanceID,
		Placement:        transfer.Placement{SavePath: req.SavePath, Category: req.Category, Tags: req.Tags},
		Options:          transfer.Options{After: req.After, LeaveStopped: req.LeaveStopped},
		StartedBy:        transfer.StartedByUser,
	})
	_, refused := errors.AsType[*transfer.Refusal](err)
	switch {
	case refused, errors.Is(err, transfer.ErrClaimed):
		RespondError(w, http.StatusConflict, err.Error())
	case err != nil:
		log.Error().Err(err).Msg("transfer: failed to start the Transfer")
		RespondError(w, http.StatusInternalServerError, "Failed to start the Transfer")
	default:
		RespondJSON(w, http.StatusAccepted, startTransferResponse{ID: id})
	}
}
