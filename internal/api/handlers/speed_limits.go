// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

type SpeedLimitsHandler struct {
	clientPool  *internalqbittorrent.ClientPool
	syncManager *internalqbittorrent.SyncManager
}

func NewSpeedLimitsHandler(clientPool *internalqbittorrent.ClientPool, syncManager *internalqbittorrent.SyncManager) *SpeedLimitsHandler {
	return &SpeedLimitsHandler{
		clientPool:  clientPool,
		syncManager: syncManager,
	}
}

// SpeedLimitsStatus represents the current speed limits status
type SpeedLimitsStatus struct {
	AlternativeSpeedLimitsEnabled bool  `json:"alternativeSpeedLimitsEnabled"`
	DownloadLimit                 int64 `json:"downloadLimit"`    // Current download limit (0 = unlimited)
	UploadLimit                   int64 `json:"uploadLimit"`      // Current upload limit (0 = unlimited)
	AlternativeDownloadLimit      int64 `json:"altDownloadLimit"` // Alternative download limit
	AlternativeUploadLimit        int64 `json:"altUploadLimit"`   // Alternative upload limit
}

// ToggleResponse represents the response after toggling speed limits
type ToggleResponse struct {
	Success                       bool   `json:"success"`
	AlternativeSpeedLimitsEnabled bool   `json:"alternativeSpeedLimitsEnabled"`
	Message                       string `json:"message,omitempty"`
}

// GetSpeedLimitsStatus godoc
// @Summary Get speed limits status for an instance
// @Description Get current speed limits status including alternative speed limits and current/alternative limits
// @Tags instances
// @Accept json
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Success 200 {object} SpeedLimitsStatus
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/instances/{instanceID}/speed-limits [get]
func (h *SpeedLimitsHandler) GetSpeedLimitsStatus(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		log.Error().Err(err).Str("instanceID", instanceIDStr).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		http.Error(w, "Failed to connect to instance", http.StatusNotFound)
		return
	}

	// Get alternative speed limits mode
	altSpeedEnabled, err := client.GetAlternativeSpeedLimitsModeCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get alternative speed limits mode")
		http.Error(w, "Failed to get speed limits status", http.StatusInternalServerError)
		return
	}

	// Get current global limits
	downloadLimit, err := client.GetGlobalDownloadLimitCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get download limit")
		http.Error(w, "Failed to get download limit", http.StatusInternalServerError)
		return
	}

	uploadLimit, err := client.GetGlobalUploadLimitCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get upload limit")
		http.Error(w, "Failed to get upload limit", http.StatusInternalServerError)
		return
	}

	// Get app preferences to get alternative limits
	prefs, err := client.GetAppPreferencesCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences")
		http.Error(w, "Failed to get app preferences", http.StatusInternalServerError)
		return
	}

	status := SpeedLimitsStatus{
		AlternativeSpeedLimitsEnabled: altSpeedEnabled,
		DownloadLimit:                 downloadLimit,
		UploadLimit:                   uploadLimit,
		AlternativeDownloadLimit:      int64(prefs.AltDlLimit),
		AlternativeUploadLimit:        int64(prefs.AltUpLimit),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Error().Err(err).Msg("Failed to encode speed limits status response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// ToggleSpeedLimits godoc
// @Summary Toggle alternative speed limits for an instance
// @Description Toggle alternative speed limits on/off for a qBittorrent instance
// @Tags instances
// @Accept json
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Success 200 {object} ToggleResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/instances/{instanceID}/speed-limits/toggle [post]
func (h *SpeedLimitsHandler) ToggleSpeedLimits(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		log.Error().Err(err).Str("instanceID", instanceIDStr).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		http.Error(w, "Failed to connect to instance", http.StatusNotFound)
		return
	}

	// Get current state before toggling
	altSpeedEnabledBefore, err := client.GetAlternativeSpeedLimitsModeCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get alternative speed limits mode")
		http.Error(w, "Failed to get current speed limits status", http.StatusInternalServerError)
		return
	}

	// Toggle alternative speed limits
	err = client.ToggleAlternativeSpeedLimitsCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to toggle alternative speed limits")
		http.Error(w, "Failed to toggle speed limits", http.StatusInternalServerError)
		return
	}

	// Get new state after toggling to confirm
	altSpeedEnabledAfter, err := client.GetAlternativeSpeedLimitsModeCtx(ctx)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to verify speed limits state after toggle")
		// Still return success since the toggle command was sent successfully
		altSpeedEnabledAfter = !altSpeedEnabledBefore
	}

	var message string
	if altSpeedEnabledAfter {
		message = "Alternative speed limits enabled"
	} else {
		message = "Alternative speed limits disabled"
	}

	log.Info().Int("instanceID", instanceID).Bool("enabled", altSpeedEnabledAfter).Msg("Speed limits toggled")

	response := ToggleResponse{
		Success:                       true,
		AlternativeSpeedLimitsEnabled: altSpeedEnabledAfter,
		Message:                       message,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error().Err(err).Msg("Failed to encode toggle response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
