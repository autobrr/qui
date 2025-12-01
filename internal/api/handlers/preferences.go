// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type PreferencesHandler struct {
	syncManager *qbittorrent.SyncManager
}

func NewPreferencesHandler(syncManager *qbittorrent.SyncManager) *PreferencesHandler {
	return &PreferencesHandler{
		syncManager: syncManager,
	}
}

// GetPreferences returns app preferences for an instance
// TODO: The go-qbittorrent library is missing network interface list endpoints:
// - /api/v2/app/networkInterfaceList (to get available network interfaces)
// - /api/v2/app/networkInterfaceAddressList (to get addresses for an interface)
// These are needed to properly populate network interface dropdowns like the official WebUI.
// For now, current_network_interface and current_interface_address show actual values but
// cannot be configured with proper dropdown selections.
func (h *PreferencesHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := ParseInstanceID(w, r)
	if !ok {
		return
	}

	prefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:get") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, prefs)
}

// UpdatePreferences updates specific preference fields
func (h *PreferencesHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := ParseInstanceID(w, r)
	if !ok {
		return
	}

	var prefs map[string]any
	if !DecodeJSON(w, r, &prefs) {
		return
	}

	// NOTE: qBittorrent's app/setPreferences API does not properly support all preferences.
	// Specifically, start_paused_enabled gets rejected/ignored. The frontend now handles
	// this preference via localStorage as a workaround.
	if err := h.syncManager.SetAppPreferences(r.Context(), instanceID, prefs); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:set") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to set app preferences")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return updated preferences
	updatedPrefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:getUpdated") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get updated preferences")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, updatedPrefs)
}

// GetAlternativeSpeedLimitsMode returns the current alternative speed limits mode
func (h *PreferencesHandler) GetAlternativeSpeedLimitsMode(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := ParseInstanceID(w, r)
	if !ok {
		return
	}

	enabled, err := h.syncManager.GetAlternativeSpeedLimitsMode(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:getAltSpeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get alternative speed limits mode")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

// ToggleAlternativeSpeedLimits toggles alternative speed limits on/off
func (h *PreferencesHandler) ToggleAlternativeSpeedLimits(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := ParseInstanceID(w, r)
	if !ok {
		return
	}

	if err := h.syncManager.ToggleAlternativeSpeedLimits(r.Context(), instanceID); err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:toggleAltSpeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to toggle alternative speed limits")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return the new state
	enabled, err := h.syncManager.GetAlternativeSpeedLimitsMode(r.Context(), instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "preferences:getAltSpeeds") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get updated alternative speed limits mode")
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}
