// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/httphelpers"
)

type ClientAPIKeysHandler struct {
	clientAPIKeyStore *models.ClientAPIKeyStore
	instanceStore     *models.InstanceStore
	basePath          string
}

func NewClientAPIKeysHandler(clientAPIKeyStore *models.ClientAPIKeyStore, instanceStore *models.InstanceStore, baseURL string) *ClientAPIKeysHandler {
	return &ClientAPIKeysHandler{
		clientAPIKeyStore: clientAPIKeyStore,
		instanceStore:     instanceStore,
		basePath:          httphelpers.NormalizeBasePath(baseURL),
	}
}

type CreateClientAPIKeyRequest struct {
	ClientName string `json:"clientName"`
	InstanceID int    `json:"instanceId"`
}

type CreateClientAPIKeyResponse struct {
	Key          string               `json:"key"`
	ClientAPIKey *models.ClientAPIKey `json:"clientApiKey"`
	Instance     *models.Instance     `json:"instance,omitempty"`
	ProxyURL     string               `json:"proxyUrl"`
}

type ClientAPIKeyWithInstance struct {
	*models.ClientAPIKey
	Instance *models.Instance `json:"instance"`
}

// CreateClientAPIKey handles POST /api/client-api-keys
func (h *ClientAPIKeysHandler) CreateClientAPIKey(w http.ResponseWriter, r *http.Request) {
	var req CreateClientAPIKeyRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	// Validate required fields
	if req.ClientName == "" {
		RespondError(w, http.StatusBadRequest, "Client name is required")
		return
	}

	if req.InstanceID == 0 {
		RespondError(w, http.StatusBadRequest, "Instance ID is required")
		return
	}

	// Verify instance exists
	ctx := r.Context()
	instance, err := h.instanceStore.Get(ctx, req.InstanceID)
	if err != nil {
		if err == models.ErrInstanceNotFound {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceId", req.InstanceID).Msg("Failed to get instance")
		RespondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Create the client API key
	rawKey, clientAPIKey, err := h.clientAPIKeyStore.Create(ctx, req.ClientName, req.InstanceID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create client API key")
		RespondError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	// Generate proxy URL (base-aware)
	proxyURL := httphelpers.JoinBasePath(h.basePath, "/proxy/"+rawKey)

	response := CreateClientAPIKeyResponse{
		Key:          rawKey,
		ClientAPIKey: clientAPIKey,
		Instance:     instance,
		ProxyURL:     proxyURL,
	}

	RespondJSON(w, http.StatusOK, response)
}

// ListClientAPIKeys handles GET /api/client-api-keys
func (h *ClientAPIKeysHandler) ListClientAPIKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all client API keys
	clientAPIKeys, err := h.clientAPIKeyStore.GetAll(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get client API keys")
		RespondError(w, http.StatusInternalServerError, "Failed to get API keys")
		return
	}

	// Enrich with instance information
	var enrichedKeys []*ClientAPIKeyWithInstance
	for _, key := range clientAPIKeys {
		instance, err := h.instanceStore.Get(ctx, key.InstanceID)
		if err != nil {
			// Log error but continue - instance might have been deleted
			log.Warn().Err(err).Int("instanceId", key.InstanceID).Int("keyId", key.ID).
				Msg("Failed to get instance for client API key")
			enrichedKeys = append(enrichedKeys, &ClientAPIKeyWithInstance{
				ClientAPIKey: key,
				Instance:     nil, // Will be null in JSON
			})
			continue
		}

		enrichedKeys = append(enrichedKeys, &ClientAPIKeyWithInstance{
			ClientAPIKey: key,
			Instance:     instance,
		})
	}

	RespondJSON(w, http.StatusOK, enrichedKeys)
}

// DeleteClientAPIKey handles DELETE /api/client-api-keys/{id}
func (h *ClientAPIKeysHandler) DeleteClientAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := ParseIntParam(w, r, "id", "API key ID")
	if !ok {
		return
	}

	ctx := r.Context()
	if err := h.clientAPIKeyStore.Delete(ctx, id); err != nil {
		if err == models.ErrClientAPIKeyNotFound {
			RespondError(w, http.StatusNotFound, "API key not found")
			return
		}
		log.Error().Err(err).Int("keyId", id).Msg("Failed to delete client API key")
		RespondError(w, http.StatusInternalServerError, "Failed to delete API key")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
