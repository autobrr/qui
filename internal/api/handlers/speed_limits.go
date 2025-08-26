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

// SetSpeedLimitsRequest represents a request to set custom speed limits
type SetSpeedLimitsRequest struct {
	DownloadLimit                 *int64 `json:"downloadLimit,omitempty"`                 // Global download limit (0 = unlimited)
	UploadLimit                   *int64 `json:"uploadLimit,omitempty"`                   // Global upload limit (0 = unlimited)
	AlternativeDownloadLimit      *int64 `json:"altDownloadLimit,omitempty"`              // Alternative download limit
	AlternativeUploadLimit        *int64 `json:"altUploadLimit,omitempty"`                // Alternative upload limit
	AlternativeSpeedLimitsEnabled *bool  `json:"alternativeSpeedLimitsEnabled,omitempty"` // Enable/disable alternative limits
}

// SetSpeedLimitsResponse represents the response after setting speed limits
type SetSpeedLimitsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
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

// SetSpeedLimits godoc
// @Summary Set custom speed limits for an instance
// @Description Set custom global and/or alternative speed limits for a qBittorrent instance
// @Tags instances
// @Accept json
// @Produce json
// @Param instanceID path int true "Instance ID"
// @Param request body SetSpeedLimitsRequest true "Speed limits configuration"
// @Success 200 {object} SetSpeedLimitsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/instances/{instanceID}/speed-limits [put]
func (h *SpeedLimitsHandler) SetSpeedLimits(w http.ResponseWriter, r *http.Request) {
	instanceIDStr := chi.URLParam(r, "instanceID")
	instanceID, err := strconv.Atoi(instanceIDStr)
	if err != nil {
		log.Error().Err(err).Str("instanceID", instanceIDStr).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	var req SetSpeedLimitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		http.Error(w, "Failed to connect to instance", http.StatusNotFound)
		return
	}

	// Set global download limit if provided
	if req.DownloadLimit != nil {
		err = client.SetGlobalDownloadLimitCtx(ctx, *req.DownloadLimit)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Int64("limit", *req.DownloadLimit).Msg("Failed to set global download limit")
			http.Error(w, "Failed to set download limit", http.StatusInternalServerError)
			return
		}
		log.Info().Int("instanceID", instanceID).Int64("limit", *req.DownloadLimit).Msg("Set global download limit")
	}

	// Set global upload limit if provided
	if req.UploadLimit != nil {
		err = client.SetGlobalUploadLimitCtx(ctx, *req.UploadLimit)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Int64("limit", *req.UploadLimit).Msg("Failed to set global upload limit")
			http.Error(w, "Failed to set upload limit", http.StatusInternalServerError)
			return
		}
		log.Info().Int("instanceID", instanceID).Int64("limit", *req.UploadLimit).Msg("Set global upload limit")
	}

	// Set alternative limits if provided
	if req.AlternativeDownloadLimit != nil || req.AlternativeUploadLimit != nil {
		prefsMap := make(map[string]any)

		if req.AlternativeDownloadLimit != nil {
			prefsMap["alt_dl_limit"] = int(*req.AlternativeDownloadLimit)
			log.Info().Int("instanceID", instanceID).Int64("limit", *req.AlternativeDownloadLimit).Msg("Setting alternative download limit")
		}

		if req.AlternativeUploadLimit != nil {
			prefsMap["alt_up_limit"] = int(*req.AlternativeUploadLimit)
			log.Info().Int("instanceID", instanceID).Int64("limit", *req.AlternativeUploadLimit).Msg("Setting alternative upload limit")
		}

		err = client.SetPreferencesCtx(ctx, prefsMap)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to set alternative speed limits")
			http.Error(w, "Failed to set alternative speed limits", http.StatusInternalServerError)
			return
		}
	}

	// Toggle alternative speed limits mode if provided
	if req.AlternativeSpeedLimitsEnabled != nil {
		currentMode, err := client.GetAlternativeSpeedLimitsModeCtx(ctx)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get current alternative speed limits mode")
			http.Error(w, "Failed to get current mode", http.StatusInternalServerError)
			return
		}

		// Only toggle if the requested state is different from current state
		if *req.AlternativeSpeedLimitsEnabled != currentMode {
			err = client.ToggleAlternativeSpeedLimitsCtx(ctx)
			if err != nil {
				log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to toggle alternative speed limits")
				http.Error(w, "Failed to toggle alternative speed limits", http.StatusInternalServerError)
				return
			}
			log.Info().Int("instanceID", instanceID).Bool("enabled", *req.AlternativeSpeedLimitsEnabled).Msg("Toggled alternative speed limits")
		}
	}

	response := SetSpeedLimitsResponse{
		Success: true,
		Message: "Speed limits updated successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error().Err(err).Msg("Failed to encode set speed limits response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
