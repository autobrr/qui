// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
)

// ArrHandler handles ARR instance management endpoints
type ArrHandler struct {
	instanceStore *models.ArrInstanceStore
	arrService    *arr.Service
}

// NewArrHandler creates a new ARR handler
func NewArrHandler(instanceStore *models.ArrInstanceStore, arrService *arr.Service) *ArrHandler {
	return &ArrHandler{
		instanceStore: instanceStore,
		arrService:    arrService,
	}
}

// arrTestResponse is the response for test endpoints
type arrTestResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ListInstances handles GET /api/arr/instances
func (h *ArrHandler) ListInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	instances, err := h.instanceStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list ARR instances")
		RespondError(w, http.StatusInternalServerError, "Failed to list ARR instances")
		return
	}

	// Mask API keys in the response
	for i := range instances {
		instances[i].APIKeyEncrypted = ""
	}

	RespondJSON(w, http.StatusOK, instances)
}

// GetInstance handles GET /api/arr/instances/{id}
func (h *ArrHandler) GetInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondError(w, http.StatusBadRequest, "Missing instance ID")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx := r.Context()
	instance, err := h.instanceStore.Get(ctx, id)
	if err != nil {
		if err == models.ErrArrInstanceNotFound {
			RespondError(w, http.StatusNotFound, "ARR instance not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to get ARR instance")
		RespondError(w, http.StatusInternalServerError, "Failed to get ARR instance")
		return
	}

	// Mask API key in the response
	instance.APIKeyEncrypted = ""

	RespondJSON(w, http.StatusOK, instance)
}

// arrCreateRequest represents the request to create an ARR instance
type arrCreateRequest struct {
	Type           models.ArrInstanceType `json:"type"`
	Name           string                 `json:"name"`
	BaseURL        string                 `json:"base_url"`
	APIKey         string                 `json:"api_key"`
	Enabled        bool                   `json:"enabled"`
	Priority       int                    `json:"priority"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
}

// CreateInstance handles POST /api/arr/instances
func (h *ArrHandler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var req arrCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode create ARR instance request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.BaseURL == "" {
		RespondError(w, http.StatusBadRequest, "Base URL is required")
		return
	}

	if req.APIKey == "" {
		RespondError(w, http.StatusBadRequest, "API key is required")
		return
	}

	if req.Type != models.ArrInstanceTypeSonarr && req.Type != models.ArrInstanceTypeRadarr {
		RespondError(w, http.StatusBadRequest, "Invalid instance type (must be 'sonarr' or 'radarr')")
		return
	}

	// Default timeout
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 15
	}

	ctx := r.Context()

	instance, err := h.instanceStore.Create(ctx, req.Type, req.Name, req.BaseURL, req.APIKey, req.Enabled, req.Priority, req.TimeoutSeconds)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create ARR instance")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			RespondError(w, http.StatusConflict, "An instance with this URL already exists for this type")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to create ARR instance")
		return
	}

	// Mask API key in response
	instance.APIKeyEncrypted = ""

	RespondJSON(w, http.StatusCreated, instance)
}

// arrUpdateRequest represents the request to update an ARR instance
type arrUpdateRequest struct {
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key,omitempty"` // Optional - only update if provided
	Enabled        *bool  `json:"enabled,omitempty"`
	Priority       *int   `json:"priority,omitempty"`
	TimeoutSeconds *int   `json:"timeout_seconds,omitempty"`
}

// UpdateInstance handles PUT /api/arr/instances/{id}
func (h *ArrHandler) UpdateInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondError(w, http.StatusBadRequest, "Missing instance ID")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req arrUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode update ARR instance request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Build update params
	params := &models.ArrInstanceUpdateParams{}

	if req.Name != "" {
		params.Name = &req.Name
	}

	if baseURL := strings.TrimSpace(req.BaseURL); baseURL != "" {
		params.BaseURL = &baseURL
	}

	if req.APIKey != "" {
		params.APIKey = &req.APIKey
	}

	params.Enabled = req.Enabled
	params.Priority = req.Priority
	params.TimeoutSeconds = req.TimeoutSeconds

	ctx := r.Context()
	instance, err := h.instanceStore.Update(ctx, id, params)
	if err != nil {
		if err == models.ErrArrInstanceNotFound {
			RespondError(w, http.StatusNotFound, "ARR instance not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to update ARR instance")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			RespondError(w, http.StatusConflict, "An instance with this URL already exists for this type")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to update ARR instance")
		return
	}

	// Mask API key in response
	instance.APIKeyEncrypted = ""

	RespondJSON(w, http.StatusOK, instance)
}

// DeleteInstance handles DELETE /api/arr/instances/{id}
func (h *ArrHandler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondError(w, http.StatusBadRequest, "Missing instance ID")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx := r.Context()
	if err := h.instanceStore.Delete(ctx, id); err != nil {
		if err == models.ErrArrInstanceNotFound {
			RespondError(w, http.StatusNotFound, "ARR instance not found")
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to delete ARR instance")
		RespondError(w, http.StatusInternalServerError, "Failed to delete ARR instance")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestInstance handles POST /api/arr/instances/{id}/test
func (h *ArrHandler) TestInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondError(w, http.StatusBadRequest, "Missing instance ID")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx := r.Context()
	testErr := h.arrService.TestInstance(ctx, id)

	response := arrTestResponse{
		Success: testErr == nil,
	}

	if testErr != nil {
		response.Error = testErr.Error()
		log.Debug().Err(testErr).Int("id", id).Msg("ARR instance test failed")
	}

	RespondJSON(w, http.StatusOK, response)
}

// arrTestConnectionRequest represents the request to test a connection before saving
type arrTestConnectionRequest struct {
	Type    models.ArrInstanceType `json:"type"`
	BaseURL string                 `json:"base_url"`
	APIKey  string                 `json:"api_key"`
}

// TestConnection handles POST /api/arr/test
func (h *ArrHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	var req arrTestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode test connection request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.BaseURL == "" {
		RespondError(w, http.StatusBadRequest, "Base URL is required")
		return
	}

	if req.APIKey == "" {
		RespondError(w, http.StatusBadRequest, "API key is required")
		return
	}

	if req.Type != models.ArrInstanceTypeSonarr && req.Type != models.ArrInstanceTypeRadarr {
		RespondError(w, http.StatusBadRequest, "Invalid instance type (must be 'sonarr' or 'radarr')")
		return
	}

	ctx := r.Context()
	testErr := h.arrService.TestConnection(ctx, req.BaseURL, req.APIKey, req.Type)

	response := arrTestResponse{
		Success: testErr == nil,
	}

	if testErr != nil {
		response.Error = testErr.Error()
		log.Debug().Err(testErr).
			Str("baseUrl", req.BaseURL).
			Str("type", string(req.Type)).
			Msg("ARR connection test failed")
	}

	RespondJSON(w, http.StatusOK, response)
}

// arrResolveRequest represents a request to resolve a title to external IDs
type arrResolveRequest struct {
	Title       string `json:"title"`
	ContentType string `json:"content_type"` // "movie" or "tv"
}

// Resolve handles POST /api/arr/resolve
func (h *ArrHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	var req arrResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode resolve request")
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Title == "" {
		RespondError(w, http.StatusBadRequest, "Title is required")
		return
	}

	var contentType arr.ContentType
	switch req.ContentType {
	case "movie":
		contentType = arr.ContentTypeMovie
	case "tv":
		contentType = arr.ContentTypeTV
	default:
		RespondError(w, http.StatusBadRequest, "Invalid content type (must be 'movie' or 'tv')")
		return
	}

	ctx := r.Context()
	result, err := h.arrService.DebugResolve(ctx, req.Title, contentType)
	if err != nil {
		log.Error().Err(err).Str("title", req.Title).Msg("Failed to resolve title")
		RespondError(w, http.StatusInternalServerError, "Failed to resolve title")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}
