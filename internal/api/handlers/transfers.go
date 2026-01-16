// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
)

// TransfersHandler handles transfer API endpoints
type TransfersHandler struct {
	service *transfer.Service
}

// NewTransfersHandler creates a new TransfersHandler
func NewTransfersHandler(service *transfer.Service) *TransfersHandler {
	return &TransfersHandler{service: service}
}

// CreateTransferPayload is the request body for creating a transfer
type CreateTransferPayload struct {
	SourceInstanceID int               `json:"sourceInstanceId"`
	TargetInstanceID int               `json:"targetInstanceId"`
	TorrentHash      string            `json:"torrentHash"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`
	DeleteFromSource *bool             `json:"deleteFromSource,omitempty"`
	PreserveCategory *bool             `json:"preserveCategory,omitempty"`
	PreserveTags     *bool             `json:"preserveTags,omitempty"`
}

// MovePayload is the request body for the move convenience endpoint
type MovePayload struct {
	TargetInstanceID int               `json:"targetInstanceId"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`
	DeleteFromSource *bool             `json:"deleteFromSource,omitempty"`
	PreserveCategory *bool             `json:"preserveCategory,omitempty"`
	PreserveTags     *bool             `json:"preserveTags,omitempty"`
}

// Create handles POST /api/transfers
func (h *TransfersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var payload CreateTransferPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("transfers: failed to decode create payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Apply defaults
	deleteFromSource := true
	if payload.DeleteFromSource != nil {
		deleteFromSource = *payload.DeleteFromSource
	}
	preserveCategory := true
	if payload.PreserveCategory != nil {
		preserveCategory = *payload.PreserveCategory
	}
	preserveTags := true
	if payload.PreserveTags != nil {
		preserveTags = *payload.PreserveTags
	}

	req := &transfer.TransferRequest{
		SourceInstanceID: payload.SourceInstanceID,
		TargetInstanceID: payload.TargetInstanceID,
		TorrentHash:      payload.TorrentHash,
		PathMappings:     payload.PathMappings,
		DeleteFromSource: deleteFromSource,
		PreserveCategory: preserveCategory,
		PreserveTags:     preserveTags,
	}

	t, err := h.service.QueueTransfer(r.Context(), req)
	if err != nil {
		if errors.Is(err, transfer.ErrTransferAlreadyExists) {
			RespondError(w, http.StatusConflict, "Transfer already exists for this torrent")
			return
		}
		log.Error().Err(err).Msg("failed to create transfer")
		RespondError(w, http.StatusInternalServerError, "Failed to create transfer")
		return
	}

	RespondJSON(w, http.StatusCreated, t)
}

// List handles GET /api/transfers
func (h *TransfersHandler) List(w http.ResponseWriter, r *http.Request) {
	opts := transfer.ListOptions{
		Limit:  50,
		Offset: 0,
	}

	// Parse query parameters
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}
	if instanceIDStr := r.URL.Query().Get("instanceId"); instanceIDStr != "" {
		if instanceID, err := strconv.Atoi(instanceIDStr); err == nil && instanceID > 0 {
			opts.InstanceID = &instanceID
		}
	}
	if statesStr := r.URL.Query().Get("states"); statesStr != "" {
		// Parse comma-separated states
		var states []models.TransferState
		for _, s := range splitAndTrim(statesStr, ",") {
			states = append(states, models.TransferState(s))
		}
		opts.States = states
	}

	transfers, err := h.service.ListTransfers(r.Context(), opts)
	if err != nil {
		log.Error().Err(err).Msg("failed to list transfers")
		RespondError(w, http.StatusInternalServerError, "Failed to list transfers")
		return
	}

	RespondJSON(w, http.StatusOK, transfers)
}

// Get handles GET /api/transfers/{id}
func (h *TransfersHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid transfer ID")
		return
	}

	t, err := h.service.GetTransfer(r.Context(), id)
	if err != nil {
		log.Error().Err(err).Int64("id", id).Msg("failed to get transfer")
		RespondError(w, http.StatusNotFound, "Transfer not found")
		return
	}

	RespondJSON(w, http.StatusOK, t)
}

// Cancel handles DELETE /api/transfers/{id}
func (h *TransfersHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid transfer ID")
		return
	}

	if err := h.service.CancelTransfer(r.Context(), id); err != nil {
		if errors.Is(err, transfer.ErrCannotCancel) {
			RespondError(w, http.StatusConflict, "Cannot cancel transfer in current state")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("failed to cancel transfer")
		RespondError(w, http.StatusInternalServerError, "Failed to cancel transfer")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// MoveTorrent handles POST /api/instances/{instanceID}/torrents/{hash}/move
// This is a convenience endpoint that creates a transfer from the given instance
func (h *TransfersHandler) MoveTorrent(w http.ResponseWriter, r *http.Request) {
	sourceInstanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	hash := chi.URLParam(r, "hash")
	if hash == "" {
		RespondError(w, http.StatusBadRequest, "Torrent hash is required")
		return
	}

	var payload MovePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("transfers: failed to decode move payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if payload.TargetInstanceID == 0 {
		RespondError(w, http.StatusBadRequest, "Target instance ID is required")
		return
	}

	// Apply defaults
	deleteFromSource := true
	if payload.DeleteFromSource != nil {
		deleteFromSource = *payload.DeleteFromSource
	}
	preserveCategory := true
	if payload.PreserveCategory != nil {
		preserveCategory = *payload.PreserveCategory
	}
	preserveTags := true
	if payload.PreserveTags != nil {
		preserveTags = *payload.PreserveTags
	}

	req := &transfer.MoveRequest{
		SourceInstanceID: sourceInstanceID,
		TargetInstanceID: payload.TargetInstanceID,
		Hash:             hash,
		PathMappings:     payload.PathMappings,
		DeleteFromSource: deleteFromSource,
		PreserveCategory: preserveCategory,
		PreserveTags:     preserveTags,
	}

	t, err := h.service.MoveTorrent(r.Context(), req)
	if err != nil {
		if errors.Is(err, transfer.ErrTransferAlreadyExists) {
			RespondError(w, http.StatusConflict, "Transfer already exists for this torrent")
			return
		}
		log.Error().Err(err).
			Int("sourceInstance", sourceInstanceID).
			Str("hash", hash).
			Msg("failed to move torrent")
		RespondError(w, http.StatusInternalServerError, "Failed to queue move")
		return
	}

	RespondJSON(w, http.StatusAccepted, t)
}

// splitAndTrim splits a string by separator and trims whitespace
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	for _, p := range splitString(s, sep) {
		p = trimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var parts []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			parts = append(parts, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
