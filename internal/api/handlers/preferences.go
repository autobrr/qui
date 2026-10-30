// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/prefcatalog"
)

type PreferencesHandler struct {
	syncManager *qbittorrent.SyncManager
}

func NewPreferencesHandler(syncManager *qbittorrent.SyncManager) *PreferencesHandler {
	return &PreferencesHandler{
		syncManager: syncManager,
	}
}

// GetPreferences returns the preference catalog view of the instance preferences.
func (h *PreferencesHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	h.respondWithView(w, r, instanceID, "preferences:get")
}

// UpdatePreferences writes the changed preference keys and returns the new view.
func (h *PreferencesHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	var prefs map[string]any
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.syncManager.SetAppPreferences(r.Context(), instanceID, prefs); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:set") {
			return
		}
		if errors.Is(err, prefcatalog.ErrInvalidPatch) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to set app preferences")
		http.Error(w, "Preferences save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.respondWithView(w, r, instanceID, "preferences:getUpdated")
}

// SendTestEmail asks qBittorrent to send a test email. Success means that qBittorrent queued it.
func (h *PreferencesHandler) SendTestEmail(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	if err := h.syncManager.SendTestEmail(r.Context(), instanceID); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:testEmail") {
			return
		}
		if errors.Is(err, qbt.ErrUnsupportedVersion) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to send test email")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetNetworkBinding returns the network interfaces and addresses that qBittorrent can bind to.
func (h *PreferencesHandler) GetNetworkBinding(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	binding, err := h.syncManager.GetNetworkBinding(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:networkBinding") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get network binding")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(binding); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode network binding")
	}
}

func (h *PreferencesHandler) respondWithView(w http.ResponseWriter, r *http.Request, instanceID int, operation string) {
	view, err := h.syncManager.GetPreferenceView(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, operation) {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get preference view")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(view); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode preference view")
	}
}

// GetAlternativeSpeedLimitsMode returns the current alternative speed limits mode
func (h *PreferencesHandler) GetAlternativeSpeedLimitsMode(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	enabled, err := h.syncManager.GetAlternativeSpeedLimitsMode(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:getAltSpeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get alternative speed limits mode")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled}); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode alternative speed limits mode response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// ToggleAlternativeSpeedLimits toggles alternative speed limits on/off
func (h *PreferencesHandler) ToggleAlternativeSpeedLimits(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	if err := h.syncManager.ToggleAlternativeSpeedLimits(r.Context(), instanceID); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:toggleAltSpeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to toggle alternative speed limits")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the new state
	enabled, err := h.syncManager.GetAlternativeSpeedLimitsMode(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:getAltSpeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get updated alternative speed limits mode")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled}); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode toggle response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
