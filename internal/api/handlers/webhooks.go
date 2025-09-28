// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/autobrr/qui/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type WebhooksHandler struct {
	instanceStore *models.InstanceStore
}

func NewWebhooksHandler(instanceStore *models.InstanceStore) *WebhooksHandler {
	return &WebhooksHandler{
		instanceStore: instanceStore,
	}
}

type Webhook struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	RootPath    string `json:"rootPath"`
	ContentPath string `json:"contentPath"`
	SavePath    string `json:"savePath"`
	NumFiles    int    `json:"numFiles"`
	Size        int64  `json:"size"`
	Tracker     string `json:"tracker"`
	InfoHashV1  string `json:"infoHashV1"`
	InfoHashV2  string `json:"infoHashV2"`
	TorrentId   string `json:"torrentId"`
}

func (h *WebhooksHandler) PostWebhook(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		log.Error().Err(err).Msg("Invalid instance ID")
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	var webhook *Webhook
	if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Invalid request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Handle webhook
	// 1. Get webhook preferences from the database
	// 2. Trigger webhook
	// 3. ??
	// 4. Return response

	log.Info().Int("instanceID", instanceID).Interface("webhook", webhook).Msg("Received webhook")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(webhook); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to encode webhook response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
