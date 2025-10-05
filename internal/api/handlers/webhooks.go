// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
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

const (
	WebhookTypeTorrentAdded     = "torrent_added"
	WebhookTypeTorrentCompleted = "torrent_completed"
)

var (
	// programFmt is an inline script used to make calls to the qui API from qBittorrent.
	// It needs to be formatted with positional arguments containing:
	//	1. The URL of the qui server (must be reachable by the qBittorrent instance).
	// 	2. The instanceID of the qBittorrent instance.
	// 	3. The API key of the qui server.
	// 	4. The type of webhook.
	programFmt = `curl -X POST -s %s/api/instances/%s/webhooks -H "X-API-Key: %s" -H "Content-Type: application/json" -d {"type":"%s","name":"%%N","category":"%%L","tags":"%%G","contentPath":"%%F","rootPath":"%%R","savePath":"%%D","numFiles":"%%C","size":"%%Z","tracker":"%%T","infoHashV1":"%%I","infoHashV2":"%%J","torrentId":"%%K"}`

	// programRegex is used to parse the program(s) from qBittorrent preferences.
	programRegex = regexp.MustCompile(`curl -X POST -s ([^\s]+)/api/instances/(\d+)/webhooks -H "X-API-Key: ([^\s]+)"`)

	errAPIKeyRequired = errors.New("API key is required")
)

type WebhooksHandler struct {
	webhookStore  *models.WebhookStore
	apiKeyStore   *models.APIKeyStore
	syncManager   *qbittorrent.SyncManager
	instanceStore *models.InstanceStore
}

func NewWebhooksHandler(
	webhookStore *models.WebhookStore,
	apiKeyStore *models.APIKeyStore,
	syncManager *qbittorrent.SyncManager,
	instanceStore *models.InstanceStore,
) *WebhooksHandler {
	return &WebhooksHandler{
		webhookStore:  webhookStore,
		apiKeyStore:   apiKeyStore,
		syncManager:   syncManager,
		instanceStore: instanceStore,
	}
}

type WebhookPreferences struct {
	Enabled                      bool   `json:"enabled"`
	InstanceID                   string `json:"instance_id"`
	APIKeyID                     int    `json:"api_key_id"`
	InstanceName                 string `json:"instance_name"`
	AutorunEnabled               bool   `json:"autorun_enabled"`
	AutorunOnTorrentAddedEnabled bool   `json:"autorun_on_torrent_added_enabled"`
	QuiURL                       string `json:"qui_url"`

	rawApiKey string `json:"-"`
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
		webhook, err := h.webhookStore.GetByInstanceID(r.Context(), instance.ID)
		if err == models.ErrWebhookNotFound {
			response = append(response, &WebhookPreferences{
				Enabled:                      false,
				InstanceID:                   strconv.Itoa(instance.ID),
				InstanceName:                 instance.Name,
				APIKeyID:                     0,
				AutorunEnabled:               false,
				AutorunOnTorrentAddedEnabled: false,
				QuiURL:                       "",
			})
			continue
		}
		if err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to get webhook")
			http.Error(w, "Failed to get webhook", http.StatusInternalServerError)
			return
		}

		response = append(response, &WebhookPreferences{
			Enabled:                      webhook.Enabled,
			InstanceID:                   strconv.Itoa(instance.ID),
			InstanceName:                 instance.Name,
			APIKeyID:                     webhook.APIKeyID,
			AutorunEnabled:               webhook.AutorunEnabled,
			AutorunOnTorrentAddedEnabled: webhook.AutorunOnTorrentAddedEnabled,
			QuiURL:                       webhook.QuiURL,
		})
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

	var preferences WebhookPreferences
	if err := json.NewDecoder(r.Body).Decode(&preferences); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Make sure the API Key still exists.
	_, err = h.apiKeyStore.GetByID(r.Context(), preferences.APIKeyID)
	if errors.Is(err, models.ErrAPIKeyNotFound) {
		// If it doesn't, create it.
		rawKey, apiKey, err := h.apiKeyStore.Create(r.Context(), preferences.rawApiKey)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to create API key")
			http.Error(w, "Failed to create API key", http.StatusInternalServerError)
			return
		}
		preferences.APIKeyID = apiKey.ID
		preferences.rawApiKey = rawKey
	} else if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get API key")
		http.Error(w, "Failed to get API key", http.StatusInternalServerError)
		return
	}

	_, err = h.webhookStore.Upsert(r.Context(),
		instanceID,
		preferences.APIKeyID,
		preferences.Enabled,
		preferences.AutorunEnabled,
		preferences.AutorunOnTorrentAddedEnabled,
		preferences.QuiURL,
	)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to upsert webhook")
		http.Error(w, "Failed to upsert webhook", http.StatusInternalServerError)
		return
	}

	// If we don't have the raw API key, retrieve it from the preferences.
	// This can happen when the user is updating an existing webhook.
	// No need to retrieve the API key if the webhook is being disabled.
	if preferences.rawApiKey == "" && preferences.Enabled {
		// qBittorrent doesn't return programs when they are not enabled.
		// So we need to set the programs to true and then get the preferences again.
		err = h.syncManager.SetAppPreferences(r.Context(), instanceID, map[string]any{
			"autorun_enabled":                  true,
			"autorun_on_torrent_added_enabled": true,
		})
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to set app preferences")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		prefs, err := h.syncManager.GetAppPreferences(r.Context(), instanceID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get app preferences")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		webhookPreferences := getWebhookPreferences(prefs)
		if webhookPreferences == nil {
			log.Error().Int("instanceID", instanceID).Msg("Failed to get webhook preferences")
			http.Error(w, "Failed to get webhook preferences", http.StatusInternalServerError)
			return
		}
		preferences.rawApiKey = webhookPreferences.rawApiKey
	}

	qbitPrefs, err := validateWebhookPreferences(preferences)
	if errors.Is(err, errAPIKeyRequired) {
		// If we get to this point and we don't have an API key, delete it.
		// This can happen if the user has manually altered the program in qBittorrent preferences.
		err = h.apiKeyStore.Delete(r.Context(), preferences.APIKeyID)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to delete API key")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, errAPIKeyRequired.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to validate webhook preferences")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.syncManager.SetAppPreferences(r.Context(), instanceID, qbitPrefs); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to set app preferences")
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	webhookPreferences := &WebhookPreferences{}

	if prefs.AutorunEnabled {
		matches := programRegex.FindStringSubmatch(prefs.AutorunProgram)
		if len(matches) == 4 {
			webhookPreferences.AutorunEnabled = true
			webhookPreferences.QuiURL = matches[1]
			webhookPreferences.InstanceID = matches[2]
			webhookPreferences.rawApiKey = matches[3]
		}
	}

	if prefs.AutorunOnTorrentAddedEnabled {
		matches := programRegex.FindStringSubmatch(prefs.AutorunOnTorrentAddedProgram)
		if len(matches) == 4 {
			webhookPreferences.AutorunOnTorrentAddedEnabled = true
			webhookPreferences.QuiURL = matches[1]
			webhookPreferences.InstanceID = matches[2]
			webhookPreferences.rawApiKey = matches[3]
		}
	}

	return webhookPreferences
}

// validateWebhookPreferences performs sanity checks on the webhook preferences.
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

	if prefs.Enabled && prefs.rawApiKey == "" {
		return nil, errAPIKeyRequired
	}

	qbitPrefs := map[string]any{}
	if prefs.AutorunEnabled {
		// Don't set program if the webhook is disabled
		if prefs.Enabled {
			qbitPrefs["autorun_program"] = fmt.Sprintf(
				programFmt, prefs.QuiURL, prefs.InstanceID, prefs.rawApiKey, WebhookTypeTorrentCompleted)
		}
		qbitPrefs["autorun_enabled"] = prefs.AutorunEnabled && prefs.Enabled
	}
	if prefs.AutorunOnTorrentAddedEnabled {
		// Don't set program if the webhook is disabled
		if prefs.Enabled {
			qbitPrefs["autorun_on_torrent_added_program"] = fmt.Sprintf(
				programFmt, prefs.QuiURL, prefs.InstanceID, prefs.rawApiKey, WebhookTypeTorrentAdded)
		}
		qbitPrefs["autorun_on_torrent_added_enabled"] = prefs.AutorunOnTorrentAddedEnabled && prefs.Enabled
	}

	return qbitPrefs, nil
}
