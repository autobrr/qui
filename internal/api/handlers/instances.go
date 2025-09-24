// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

type InstancesHandler struct {
	instanceStore *models.InstanceStore
	clientPool    *internalqbittorrent.ClientPool
	syncManager   *internalqbittorrent.SyncManager
}

func NewInstancesHandler(instanceStore *models.InstanceStore, clientPool *internalqbittorrent.ClientPool, syncManager *internalqbittorrent.SyncManager) *InstancesHandler {
	return &InstancesHandler{
		instanceStore: instanceStore,
		clientPool:    clientPool,
		syncManager:   syncManager,
	}
}

func (h *InstancesHandler) buildInstanceResponsesParallel(ctx context.Context, instances []*models.Instance) []InstanceResponse {
	if len(instances) == 0 {
		return []InstanceResponse{}
	}

	type result struct {
		index    int
		response InstanceResponse
	}
	resultCh := make(chan result, len(instances))

	for i, instance := range instances {
		go func(index int, inst *models.Instance) {
			response := h.buildInstanceResponse(ctx, inst)
			resultCh <- result{index: index, response: response}
		}(i, instance)
	}

	responses := make([]InstanceResponse, len(instances))
	for i := range len(instances) {
		select {
		case res := <-resultCh:
			responses[res.index] = res.response
		case <-ctx.Done():
			// Handle context cancellation gracefully
			responses[i] = InstanceResponse{
				ID:                 instances[i].ID,
				Name:               instances[i].Name,
				Host:               instances[i].Host,
				Username:           instances[i].Username,
				BasicUsername:      instances[i].BasicUsername,
				TLSSkipVerify:      instances[i].TLSSkipVerify,
				Connected:          false,
				HasDecryptionError: false,
			}
		}
	}

	return responses
}

// buildInstanceResponse creates a consistent response for an instance
func (h *InstancesHandler) buildInstanceResponse(ctx context.Context, instance *models.Instance) InstanceResponse {
	// Use cached connection status only, do not test connection synchronously
	client, _ := h.clientPool.GetClientOffline(ctx, instance.ID)
	healthy := client != nil && client.IsHealthy()

	decryptionErrorInstances := h.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instance.ID)

	response := InstanceResponse{
		ID:                 instance.ID,
		Name:               instance.Name,
		Host:               instance.Host,
		Username:           instance.Username,
		BasicUsername:      instance.BasicUsername,
		TLSSkipVerify:      instance.TLSSkipVerify,
		Connected:          healthy,
		HasDecryptionError: hasDecryptionError,
	}

	// Fetch recent errors for disconnected instances
	if !healthy {
		errorStore := h.clientPool.GetErrorStore()
		recentErrors, err := errorStore.GetRecentErrors(ctx, instance.ID, 5)
		if err != nil {
			log.Error().Err(err).Int("instanceID", instance.ID).Msg("Failed to get recent errors")
		} else {
			response.RecentErrors = recentErrors
		}
	}

	return response
}

// buildQuickInstanceResponse creates a response without testing connection
func (h *InstancesHandler) buildQuickInstanceResponse(instance *models.Instance) InstanceResponse {
	return InstanceResponse{
		ID:                 instance.ID,
		Name:               instance.Name,
		Host:               instance.Host,
		Username:           instance.Username,
		BasicUsername:      instance.BasicUsername,
		TLSSkipVerify:      instance.TLSSkipVerify,
		Connected:          false, // Will be updated asynchronously
		HasDecryptionError: false,
	}
}

// testConnectionAsync tests connection in background and updates cache
func (h *InstancesHandler) testConnectionAsync(instanceID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Debug().Int("instanceID", instanceID).Msg("Testing connection asynchronously")

	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Async connection test failed")
		return
	}

	if err := client.HealthCheck(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Async health check failed")
		return
	}

	log.Debug().Int("instanceID", instanceID).Msg("Async connection test succeeded")
}

// CreateInstanceRequest represents a request to create a new instance
type CreateInstanceRequest struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Username      string  `json:"username"`
	Password      string  `json:"password"`
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
	TLSSkipVerify bool    `json:"tlsSkipVerify,omitempty"`
}

// UpdateInstanceRequest represents a request to update an instance
type UpdateInstanceRequest struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Username      string  `json:"username"`
	Password      string  `json:"password,omitempty"` // Optional for updates
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
	TLSSkipVerify *bool   `json:"tlsSkipVerify,omitempty"`
}

// InstanceResponse represents an instance in API responses
type InstanceResponse struct {
	ID                 int                    `json:"id"`
	Name               string                 `json:"name"`
	Host               string                 `json:"host"`
	Username           string                 `json:"username"`
	BasicUsername      *string                `json:"basicUsername,omitempty"`
	TLSSkipVerify      bool                   `json:"tlsSkipVerify"`
	Connected          bool                   `json:"connected"`
	HasDecryptionError bool                   `json:"hasDecryptionError"`
	RecentErrors       []models.InstanceError `json:"recentErrors,omitempty"`
}

// TestConnectionResponse represents connection test results
type TestConnectionResponse struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// DeleteInstanceResponse represents delete operation result
type DeleteInstanceResponse struct {
	Message string `json:"message"`
}

// ListInstances returns all instances
func (h *InstancesHandler) ListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	response := h.buildInstanceResponsesParallel(r.Context(), instances)

	RespondJSON(w, http.StatusOK, response)
}

// CreateInstance creates a new instance
func (h *InstancesHandler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var req CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input
	if req.Name == "" || req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Name and host are required")
		return
	}

	// Create instance
	instance, err := h.instanceStore.Create(r.Context(), req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword, req.TLSSkipVerify)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create instance")
		RespondError(w, http.StatusInternalServerError, "Failed to create instance")
		return
	}

	// Return quickly without testing connection
	response := h.buildQuickInstanceResponse(instance)

	// Test connection asynchronously
	go h.testConnectionAsync(instance.ID)

	RespondJSON(w, http.StatusCreated, response)
}

// UpdateInstance updates an existing instance
func (h *InstancesHandler) UpdateInstance(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req UpdateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate input
	if req.Name == "" || req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Name and host are required")
		return
	}

	// Fetch existing instance to handle redacted values
	existingInstance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to fetch existing instance")
		RespondError(w, http.StatusInternalServerError, "Failed to fetch instance")
		return
	}

	// Handle redacted password - if redacted, use existing password
	if req.Password != "" && domain.IsRedactedString(req.Password) {
		req.Password = existingInstance.PasswordEncrypted
	}

	// Handle redacted basic password - if redacted, use existing basic password
	if req.BasicPassword != nil && *req.BasicPassword != "" && domain.IsRedactedString(*req.BasicPassword) {
		req.BasicPassword = existingInstance.BasicPasswordEncrypted
	}

	// Update instance
	instance, err := h.instanceStore.Update(r.Context(), instanceID, req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword, req.TLSSkipVerify)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to update instance")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance")
		return
	}

	// Remove old client from pool to force reconnection
	h.clientPool.RemoveClient(instanceID)

	// Return quickly without testing connection
	response := h.buildQuickInstanceResponse(instance)

	// Test connection asynchronously
	go h.testConnectionAsync(instance.ID)

	RespondJSON(w, http.StatusOK, response)
}

// DeleteInstance deletes an instance
func (h *InstancesHandler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Delete instance
	if err := h.instanceStore.Delete(r.Context(), instanceID); err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to delete instance")
		RespondError(w, http.StatusInternalServerError, "Failed to delete instance")
		return
	}

	// Remove client from pool
	h.clientPool.RemoveClient(instanceID)

	response := DeleteInstanceResponse{
		Message: "Instance deleted successfully",
	}
	RespondJSON(w, http.StatusOK, response)
}

// TestConnection tests the connection to an instance
func (h *InstancesHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Try to get client (this will create connection if needed)
	client, err := h.clientPool.GetClient(r.Context(), instanceID)
	if err != nil {
		response := TestConnectionResponse{
			Connected: false,
			Error:     err.Error(),
		}
		RespondJSON(w, http.StatusOK, response)
		return
	}

	// Perform health check
	if err := client.HealthCheck(r.Context()); err != nil {
		response := TestConnectionResponse{
			Connected: false,
			Error:     err.Error(),
		}
		RespondJSON(w, http.StatusOK, response)
		return
	}

	response := TestConnectionResponse{
		Connected: true,
		Message:   "Connection successful",
	}
	RespondJSON(w, http.StatusOK, response)
}
