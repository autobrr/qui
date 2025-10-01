// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type WebhooksHandler struct {
	clientAPIKeyStore *models.ClientAPIKeyStore
	syncManager       *qbittorrent.SyncManager
	instanceStore     *models.InstanceStore
}

func NewWebhooksHandler(
	clientAPIKeyStore *models.ClientAPIKeyStore,
	syncManager *qbittorrent.SyncManager,
	instanceStore *models.InstanceStore,
) *WebhooksHandler {
	return &WebhooksHandler{
		clientAPIKeyStore: clientAPIKeyStore,
		syncManager:       syncManager,
		instanceStore:     instanceStore,
	}
}

type WebhookPreferences struct {
	Enabled                      bool   `json:"enabled"`
	APIKey                       string `json:"api_key"`
	APIKeyID                     int    `json:"api_key_id"`
	InstanceID                   string `json:"instance_id"`
	InstanceName                 string `json:"instance_name"`
	AutorunEnabled               bool   `json:"autorun_enabled"`
	AutorunOnTorrentAddedEnabled bool   `json:"autorun_on_torrent_added_enabled"`
	QuiURL                       string `json:"qui_url"`

	// These fields are not returned by the API, but are used to validate the preferences.
	AutorunProgram               string `json:"-"`
	AutorunOnTorrentAddedProgram string `json:"-"`
}

type Webhook struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	RootPath    string `json:"rootPath"`
	ContentPath string `json:"contentPath"`
	SavePath    string `json:"savePath"`
	NumFiles    string `json:"numFiles"`
	Size        string `json:"size"`
	Tracker     string `json:"tracker"`
	InfoHashV1  string `json:"infoHashV1"`
	InfoHashV2  string `json:"infoHashV2"`
	TorrentId   string `json:"torrentId"`
}

// GetAllWebhooks returns the webhook preferences for all instances.
func (h *WebhooksHandler) GetAllWebhooks(w http.ResponseWriter, r *http.Request) {
	instances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get instances")
		http.Error(w, "Failed to get instances", http.StatusInternalServerError)
		return
	}

	response := make([]*WebhookPreferences, 0)
	for _, instance := range instances {
		key, err := h.clientAPIKeyStore.GetByInstanceIDAndIsWebhook(r.Context(), instance.ID)
		// No webhook API key found for this instance, so no webhooks are enabled.
		if err == models.ErrClientAPIKeyNotFound {
			response = append(response, &WebhookPreferences{
				Enabled:                      false,
				InstanceID:                   strconv.Itoa(instance.ID),
				InstanceName:                 instance.Name,
				AutorunEnabled:               false,
				AutorunOnTorrentAddedEnabled: false,
				QuiURL:                       "",
			})
			continue
		}
		if err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to get webhook api key")
			http.Error(w, "Failed to get webhook api key", http.StatusInternalServerError)
			return
		}

		prefs, err := h.syncManager.GetAppPreferences(r.Context(), instance.ID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to get app preferences")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		webhookPreferences := getWebhookPreferences(prefs)
		if webhookPreferences == nil {
			// Instance has valid webhook API key, but no webhook preferences are set.
			response = append(response, &WebhookPreferences{
				Enabled:      false,
				APIKeyID:     key.ID,
				InstanceID:   strconv.Itoa(instance.ID),
				InstanceName: instance.Name,
			})
			continue
		}

		webhookPreferences.APIKeyID = key.ID
		webhookPreferences.Enabled = true
		webhookPreferences.InstanceID = strconv.Itoa(instance.ID)
		webhookPreferences.InstanceName = instance.Name
		response = append(response, webhookPreferences)

	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error().Err(err).Msg("Failed to encode webhooks response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// UpdateWebhookPreferences updates the webhook preferences for an instance.
func (h *WebhooksHandler) UpdateWebhookPreferences(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	var webhookPreferences WebhookPreferences
	if err := json.NewDecoder(r.Body).Decode(&webhookPreferences); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	qbitPrefs, err := validateWebhookPreferences(webhookPreferences)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to validate webhook preferences")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// NOTE: qBittorrent's app/setPreferences API does not properly support all preferences.
	// Specifically, start_paused_enabled gets rejected/ignored. The frontend now handles
	// this preference via localStorage as a workaround.
	if err := h.syncManager.SetAppPreferences(r.Context(), instanceID, qbitPrefs); err != nil {
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

	updatedWebhookPrefs := getWebhookPreferences(updatedPrefs)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updatedWebhookPrefs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode updated preferences response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// PostWebhook receives a webhook from qBittorrent and processes it.
func (h *WebhooksHandler) PostWebhook(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	webhook := Webhook{}
	if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Info().Int("instanceID", instanceID).Interface("webhook", webhook).Msg("Decoded webhook")

	// TODO: Handle webhook
	// 1. Get webhook preferences from the database
	// 2. Trigger webhook
	// 3. ??
	// 4. Return response

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(webhook); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode webhook response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// getWebhookPreferences validates the qbit preferences and returns the webhook preferences.
// It could be that the user has custom programs not set by qui,
// so this function checks if the programs follow the correct qui URL format.
// If yes, then the webhook preferences are returned.
func getWebhookPreferences(prefs qbt.AppPreferences) *WebhookPreferences {
	re := regexp.MustCompile(`curl -X POST -s ([^\s]+)/api/instances/(\d+)/webhooks -H "X-API-Key: ([^\s]+)"`)

	if prefs.AutorunProgram == "" && prefs.AutorunOnTorrentAddedProgram == "" {
		return nil
	}

	webhookPreferences := &WebhookPreferences{}

	if prefs.AutorunEnabled {
		matches := re.FindStringSubmatch(prefs.AutorunProgram)
		if len(matches) == 4 {
			webhookPreferences.AutorunEnabled = true
			webhookPreferences.QuiURL = matches[1]
			webhookPreferences.InstanceID = matches[2]
			webhookPreferences.APIKey = matches[3]
		}
	}

	if prefs.AutorunOnTorrentAddedEnabled {
		matches := re.FindStringSubmatch(prefs.AutorunOnTorrentAddedProgram)
		if len(matches) == 4 {
			webhookPreferences.AutorunOnTorrentAddedEnabled = true
			webhookPreferences.QuiURL = matches[1]
			webhookPreferences.InstanceID = matches[2]
			webhookPreferences.APIKey = matches[3]
		}
	}

	return webhookPreferences
}

// validateWebhookPreferences performs sanity checks on the webhook preferences.
// Namely, checks if the quiURL is valid when the webhook preferences are enabled.
// If everything is ok, the program(s) is/are added and the preferences are returned.
func validateWebhookPreferences(prefs WebhookPreferences) (map[string]any, error) {
	if prefs.AutorunEnabled && prefs.QuiURL == "" {
		return nil, fmt.Errorf("quiURL is required when autorun_enabled is true")
	}

	if prefs.AutorunOnTorrentAddedEnabled && prefs.QuiURL == "" {
		return nil, fmt.Errorf("quiURL is required when autorun_on_torrent_added_enabled is true")
	}

	if prefs.QuiURL == "" {
		return nil, fmt.Errorf("quiURL is required")
	}

	if _, err := url.Parse(prefs.QuiURL); err != nil {
		return nil, fmt.Errorf("quiURL '%s' must be a valid URL: %w", prefs.QuiURL, err)
	}

	// program is an inline script used to make calls to the qui API from qBittorrent.
	// It needs to be formatted with positional arguments containing:
	//	1. The URL of the qui server (must be reachable by the qBittorrent instance).
	// 	2. The instanceID of the qBittorrent instance.
	program := fmt.Sprintf(`curl -X POST -s %s/api/instances/%s/webhooks -H "X-API-Key: %s" -H "Content-Type: application/json" -d {"name":"$N","category":"$L","tags":"$G","contentPath":"$F","rootPath":"$R","savePath":"$D","numFiles":"$C","size":"$Z","tracker":"$T","infoHashV1":"$I","infoHashV2":"$J","torrentId":"$K"}`,
		prefs.QuiURL,
		prefs.InstanceID,
		prefs.APIKey,
	)

	if prefs.AutorunEnabled {
		prefs.AutorunProgram = program
	}
	if prefs.AutorunOnTorrentAddedEnabled {
		prefs.AutorunOnTorrentAddedProgram = program
	}

	return map[string]any{
		"autorun_enabled":                  prefs.AutorunEnabled,
		"autorun_program":                  prefs.AutorunProgram,
		"autorun_on_torrent_added_enabled": prefs.AutorunOnTorrentAddedEnabled,
		"autorun_on_torrent_added_program": prefs.AutorunOnTorrentAddedProgram,
	}, nil
}
