// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
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
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	prefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(prefs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode preferences response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// UpdatePreferences updates specific preference fields
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

	prefs, err = validateAutoRunPreferences(prefs, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to validate auto run preferences")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// NOTE: qBittorrent's app/setPreferences API does not properly support all preferences.
	// Specifically, start_paused_enabled gets rejected/ignored. The frontend now handles
	// this preference via localStorage as a workaround.
	if err := h.syncManager.SetAppPreferences(r.Context(), instanceID, prefs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to set app preferences")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated preferences
	updatedPrefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get updated preferences")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updatedPrefs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode updated preferences response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
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
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to toggle alternative speed limits")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the new state
	enabled, err := h.syncManager.GetAlternativeSpeedLimitsMode(r.Context(), instanceID)
	if err != nil {
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

// validateAutoRunPreferences performs sanity checks on the auto run preferences.
// Namely, checks if the quiURL is valid when the auto run preferences are enabled.
// If everything is ok, the program is added to the preferences and preferences are returned.
func validateAutoRunPreferences(prefs map[string]any, instanceID int) (map[string]any, error) {
	if prefs["autorun_enabled"] == true && prefs["qui_url"] == "" {
		return nil, fmt.Errorf("quiURL is required when autorun_enabled is true")
	}

	if prefs["autorun_on_torrent_added_enabled"] == true && prefs["qui_url"] == "" {
		return nil, fmt.Errorf("quiURL is required when autorun_on_torrent_added_enabled is true")
	}

	// No quirURL provided and no auto run preferences are enabled, return the preferences as is.
	if prefs["qui_url"] == "" {
		return prefs, nil
	}

	// TODO: assuming qui_url is saved and returned by the syncManager.
	quiURL := prefs["qui_url"].(string)
	if _, err := url.Parse(quiURL); err != nil {
		return nil, fmt.Errorf("quiURL '%s' must be a valid URL: %w", quiURL, err)
	}

	// program is an inline script used to make calls to the qui API from qBittorrent.
	// It needs to be formatted with positional arguments containing:
	//	1. The URL of the qui server (must be reachable by the qBittorrent instance).
	// 	2. The instanceID of the qBittorrent instance.
	// TODO: add API key to the request.
	program := fmt.Sprintf(`curl -s %s/api/instances/%v/webhooks \
		-H "Content-Type: application/json" \
		-d "{
			\"name\":\"$N\",
			\"category\":\"$L\",
			\"tags\":\"$G\",
			\"contentPath\":\"$F\",
			\"rootPath\":\"$R\",
			\"savePath\":\"$D\",
			\"numFiles\":\"$C\",
			\"size\":\"$Z\",
			\"tracker\":\"$T\",
			\"infoHashV1\":\"$I\",
			\"infoHashV2\":\"$J\",
			\"torrentId\":\"$K\"
		}"`,
		quiURL,
		instanceID,
	)

	if prefs["autorun_enabled"] == true {
		prefs["autorun_program"] = program
	}
	if prefs["autorun_on_torrent_added_enabled"] == true {
		prefs["autorun_on_torrent_added_program"] = program
	}

	return prefs, nil
}
