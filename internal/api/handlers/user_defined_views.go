// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/httphelpers"
)

type UserDefinedViewHandler struct {
	userDefinedViewStore *models.UserDefinedViewStore
	instanceStore        *models.InstanceStore
	basePath             string
}

func NewUserDefinedViewHandler(userDefinedViewStore *models.UserDefinedViewStore, instanceStore *models.InstanceStore, baseURL string) *UserDefinedViewHandler {
	return &UserDefinedViewHandler{
		userDefinedViewStore: userDefinedViewStore,
		instanceStore:        instanceStore,
		basePath:             httphelpers.NormalizeBasePath(baseURL),
	}
}

// Create handles POST /api/instances/{instanceId}/user-defined-views
func (h *UserDefinedViewHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateUserDefinedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode create user defined view request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "View name is required", http.StatusBadRequest)
		return
	}

	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Verify instance exists
	_, err = h.instanceStore.Get(ctx, instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			http.Error(w, "Instance not found", http.StatusBadRequest)
			return
		}
		log.Error().Err(err).Int("instanceId", instanceID).Msg("Failed to get instance")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create the view
	create := models.UserDefinedViewCreate{
		InstanceID:        instanceID,
		Name:              req.Name,
		Status:            req.Filters.Status,
		Categories:        req.Filters.Categories,
		Tags:              req.Filters.Tags,
		Trackers:          req.Filters.Trackers,
		ExcludeStatus:     req.Filters.ExcludeStatus,
		ExcludeCategories: req.Filters.ExcludeCategories,
		ExcludeTags:       req.Filters.ExcludeTags,
		ExcludeTrackers:   req.Filters.ExcludeTrackers,
	}
	err = h.userDefinedViewStore.Create(ctx, create)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create user defined view")
		http.Error(w, "Failed to create user defined view", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// List handles GET /api/instances/{instanceId}/user-defined-views
func (h *UserDefinedViewHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Get user-defined views
	views, err := h.userDefinedViewStore.List(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get user-defined view")
		http.Error(w, "Failed to get user-defined view", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(views)
}

// Update handles PUT /api/instances/{instanceId}/user-defined-views/{viewID}
func (h *UserDefinedViewHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	viewID, err := strconv.Atoi(chi.URLParam(r, "viewID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid view ID")
		return
	}

	var req UpdateUserDefinedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode update user defined view request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update the view properties
	update := models.UserDefinedViewUpdate{
		Status:            req.Filters.Status,
		Categories:        req.Filters.Categories,
		Tags:              req.Filters.Tags,
		Trackers:          req.Filters.Trackers,
		ExcludeStatus:     req.Filters.ExcludeStatus,
		ExcludeCategories: req.Filters.ExcludeCategories,
		ExcludeTags:       req.Filters.ExcludeTags,
		ExcludeTrackers:   req.Filters.ExcludeTrackers,
	}
	err = h.userDefinedViewStore.Update(ctx, viewID, update)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create user defined view")
		http.Error(w, "Failed to create user defined view", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete handles DELETE /api/instances/{instanceId}/user-defined-views/{viewID}
func (h *UserDefinedViewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get IDs from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}
	viewID, err := strconv.Atoi(chi.URLParam(r, "viewID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid view ID")
		return
	}

	if err = h.userDefinedViewStore.Delete(ctx, instanceID, viewID); err != nil {
		if errors.Is(err, models.ErrUserDefinedViewNotFound) {
			http.Error(w, "User-defined view not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("viewID", viewID).Msg("Failed to delete user-defined view")
		http.Error(w, "Failed to delete user-defined view", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type ListUserDefinedViewsResponse []UserDefinedView
type UserDefinedView struct {
	ID      int          `json:"id"`
	Name    string       `json:"name"`
	Filters FilterFields `json:"filters"`
}

type CreateUserDefinedViewRequest struct {
	Name    string       `json:"name"`
	Filters FilterFields `json:"filters"`
}

type UpdateUserDefinedViewRequest struct {
	Name    string       `json:"name"`
	Filters FilterFields `json:"filters"`
}

type FilterFields struct {
	Status            []string `json:"status"`
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	Trackers          []string `json:"trackers"`
	ExcludeStatus     []string `json:"excludeStatus"`
	ExcludeCategories []string `json:"excludeCategories"`
	ExcludeTags       []string `json:"excludeTags"`
	ExcludeTrackers   []string `json:"excludeTrackers"`
}
