// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/reannounce"
)

type InstancesHandler struct {
	instanceStore   *models.InstanceStore
	reannounceStore *models.InstanceReannounceStore
	reannounceCache *reannounce.SettingsCache
	clientPool      *internalqbittorrent.ClientPool
	syncManager     *internalqbittorrent.SyncManager
	reannounceSvc   *reannounce.Service
}

func NewInstancesHandler(instanceStore *models.InstanceStore, reannounceStore *models.InstanceReannounceStore, reannounceCache *reannounce.SettingsCache, clientPool *internalqbittorrent.ClientPool, syncManager *internalqbittorrent.SyncManager, svc *reannounce.Service) *InstancesHandler {
	return &InstancesHandler{
		instanceStore:   instanceStore,
		reannounceStore: reannounceStore,
		reannounceCache: reannounceCache,
		clientPool:      clientPool,
		syncManager:     syncManager,
		reannounceSvc:   svc,
	}
}

// GetInstanceCapabilities returns lightweight capability metadata for an instance.
func (h *InstancesHandler) GetInstanceCapabilities(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	client, err := h.clientPool.GetClientOffline(ctx, instanceID)
	if err != nil {
		client, err = h.clientPool.GetClientWithTimeout(ctx, instanceID, 15*time.Second)
		if err != nil {
			if respondIfInstanceDisabled(w, err, instanceID, "instances:getCapabilities") {
				return
			}
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for capabilities")
			RespondError(w, http.StatusServiceUnavailable, "Failed to load instance capabilities")
			return
		}
	}

	if client.GetWebAPIVersion() == "" {
		if err := client.RefreshCapabilities(ctx); err != nil {
			log.Error().
				Err(err).
				Int("instanceID", instanceID).
				Msg("Unable to refresh qBittorrent capabilities during request")
		}
	}

	capabilities := NewInstanceCapabilitiesResponse(client)
	RespondJSON(w, http.StatusOK, capabilities)
}

// GetReannounceActivity returns recent reannounce events for an instance.
func (h *InstancesHandler) GetReannounceActivity(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}
	if h.reannounceSvc == nil {
		RespondJSON(w, http.StatusOK, []reannounce.ActivityEvent{})
		return
	}
	limitParam := strings.TrimSpace(r.URL.Query().Get("limit"))
	var limit int
	if limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	events := h.reannounceSvc.GetActivity(instanceID, limit)
	if events == nil {
		events = []reannounce.ActivityEvent{}
	}
	normalized := make([]reannounce.ActivityEvent, len(events))
	for i, event := range events {
		event.Hash = strings.ToLower(event.Hash)
		normalized[i] = event
	}
	RespondJSON(w, http.StatusOK, normalized)
}

// GetReannounceCandidates returns torrents that currently fall within the
// reannounce monitoring scope and either have tracker problems or are still
// waiting for their initial tracker contact.
func (h *InstancesHandler) GetReannounceCandidates(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}
	if h.reannounceSvc == nil {
		RespondJSON(w, http.StatusOK, []reannounce.MonitoredTorrent{})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	candidates := h.reannounceSvc.GetMonitoredTorrents(ctx, instanceID)
	if candidates == nil {
		candidates = []reannounce.MonitoredTorrent{}
	}

	normalized := make([]reannounce.MonitoredTorrent, len(candidates))
	for i, candidate := range candidates {
		candidate.Hash = strings.ToLower(candidate.Hash)
		normalized[i] = candidate
	}

	RespondJSON(w, http.StatusOK, normalized)
}

// GetTransferInfo returns lightweight transfer stats for an instance.
func (h *InstancesHandler) GetTransferInfo(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		if respondIfInstanceDisabled(w, err, instanceID, "instances:getTransferInfo") {
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client for transfer info")
		RespondError(w, http.StatusServiceUnavailable, "Failed to load transfer info")
		return
	}

	info, err := client.GetTransferInfoCtx(ctx)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get transfer info")
		RespondError(w, http.StatusInternalServerError, "Failed to get transfer info")
		return
	}

	RespondJSON(w, http.StatusOK, info)
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
	for i := range instances {
		select {
		case res := <-resultCh:
			responses[res.index] = res.response
		case <-ctx.Done():
			// Handle context cancellation gracefully
			responses[i] = InstanceResponse{
				ID:                       instances[i].ID,
				Name:                     instances[i].Name,
				Host:                     instances[i].Host,
				Username:                 instances[i].Username,
				BasicUsername:            instances[i].BasicUsername,
				TLSSkipVerify:            instances[i].TLSSkipVerify,
				HasLocalFilesystemAccess: instances[i].HasLocalFilesystemAccess,
				UseHardlinks:             instances[i].UseHardlinks,
				HardlinkBaseDir:          instances[i].HardlinkBaseDir,
				HardlinkDirPreset:        instances[i].HardlinkDirPreset,
				UseReflinks:              instances[i].UseReflinks,
				Connected:                false,
				HasDecryptionError:       false,
				SortOrder:                instances[i].SortOrder,
				IsActive:                 instances[i].IsActive,
				ReannounceSettings:       payloadFromModel(models.DefaultInstanceReannounceSettings(instances[i].ID)),
				ConnectionStatus: func(active bool) string {
					if !active {
						return "disabled"
					}
					return ""
				}(instances[i].IsActive),
			}
		}
	}

	return responses
}

// buildInstanceResponse creates a consistent response for an instance
func (h *InstancesHandler) buildInstanceResponse(ctx context.Context, instance *models.Instance) InstanceResponse {
	// Use cached connection status only, do not test connection synchronously
	client, _ := h.clientPool.GetClientOffline(ctx, instance.ID)
	healthy := client != nil && client.IsHealthy() && instance.IsActive

	var connectionStatus string
	if !instance.IsActive {
		connectionStatus = "disabled"
	} else if client != nil {
		if status := strings.TrimSpace(client.GetCachedConnectionStatus()); status != "" {
			connectionStatus = strings.ToLower(status)
		}
	}

	decryptionErrorInstances := h.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instance.ID)

	response := InstanceResponse{
		ID:                       instance.ID,
		Name:                     instance.Name,
		Host:                     instance.Host,
		Username:                 instance.Username,
		BasicUsername:            instance.BasicUsername,
		TLSSkipVerify:            instance.TLSSkipVerify,
		HasLocalFilesystemAccess: instance.HasLocalFilesystemAccess,
		UseHardlinks:             instance.UseHardlinks,
		HardlinkBaseDir:          instance.HardlinkBaseDir,
		HardlinkDirPreset:        instance.HardlinkDirPreset,
		UseReflinks:              instance.UseReflinks,
		FallbackToRegularMode:    instance.FallbackToRegularMode,
		Connected:                healthy,
		HasDecryptionError:       hasDecryptionError,
		ConnectionStatus:         connectionStatus,
		SortOrder:                instance.SortOrder,
		IsActive:                 instance.IsActive,
	}

	response.ReannounceSettings = h.getReannounceSettingsPayload(ctx, instance.ID)

	// Fetch recent errors for disconnected instances
	if instance.IsActive && !healthy {
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
	connectionStatus := ""
	if !instance.IsActive {
		connectionStatus = "disabled"
	}
	return InstanceResponse{
		ID:                       instance.ID,
		Name:                     instance.Name,
		Host:                     instance.Host,
		Username:                 instance.Username,
		BasicUsername:            instance.BasicUsername,
		TLSSkipVerify:            instance.TLSSkipVerify,
		HasLocalFilesystemAccess: instance.HasLocalFilesystemAccess,
		UseHardlinks:             instance.UseHardlinks,
		HardlinkBaseDir:          instance.HardlinkBaseDir,
		HardlinkDirPreset:        instance.HardlinkDirPreset,
		UseReflinks:              instance.UseReflinks,
		FallbackToRegularMode:    instance.FallbackToRegularMode,
		Connected:                false, // Will be updated asynchronously
		HasDecryptionError:       false,
		SortOrder:                instance.SortOrder,
		IsActive:                 instance.IsActive,
		ConnectionStatus:         connectionStatus,
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

func (h *InstancesHandler) loadReannounceSettings(ctx context.Context, instanceID int) *models.InstanceReannounceSettings {
	if h.reannounceStore == nil {
		return models.DefaultInstanceReannounceSettings(instanceID)
	}
	settings, err := h.reannounceStore.Get(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to load reannounce settings")
		return models.DefaultInstanceReannounceSettings(instanceID)
	}
	return settings
}

func (h *InstancesHandler) getReannounceSettingsPayload(ctx context.Context, instanceID int) InstanceReannounceSettingsPayload {
	if h.reannounceCache != nil {
		if cached := h.reannounceCache.Get(instanceID); cached != nil {
			return payloadFromModel(cached)
		}
	}
	return payloadFromModel(h.loadReannounceSettings(ctx, instanceID))
}

func (h *InstancesHandler) persistReannounceSettings(ctx context.Context, instanceID int, payload *InstanceReannounceSettingsPayload) (*models.InstanceReannounceSettings, error) {
	desired := payload.toModel(instanceID, nil)
	if h.reannounceStore == nil {
		if h.reannounceCache != nil {
			h.reannounceCache.Replace(desired)
		}
		return desired, nil
	}
	saved, err := h.reannounceStore.Upsert(ctx, desired)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to persist reannounce settings")
		return nil, err
	}
	if h.reannounceCache != nil {
		h.reannounceCache.Replace(saved)
	}
	return saved, nil
}

// CreateInstanceRequest represents a request to create a new instance
type CreateInstanceRequest struct {
	Name                     string                             `json:"name"`
	Host                     string                             `json:"host"`
	Username                 string                             `json:"username"`
	Password                 string                             `json:"password"`
	BasicUsername            *string                            `json:"basicUsername,omitempty"`
	BasicPassword            *string                            `json:"basicPassword,omitempty"`
	TLSSkipVerify            bool                               `json:"tlsSkipVerify,omitempty"`
	HasLocalFilesystemAccess *bool                              `json:"hasLocalFilesystemAccess,omitempty"`
	ReannounceSettings       *InstanceReannounceSettingsPayload `json:"reannounceSettings,omitempty"`
}

// UpdateInstanceRequest represents a request to update an instance
type UpdateInstanceRequest struct {
	Name                     string                             `json:"name"`
	Host                     string                             `json:"host"`
	Username                 string                             `json:"username"`
	Password                 string                             `json:"password,omitempty"` // Optional for updates
	BasicUsername            *string                            `json:"basicUsername,omitempty"`
	BasicPassword            *string                            `json:"basicPassword,omitempty"`
	TLSSkipVerify            *bool                              `json:"tlsSkipVerify,omitempty"`
	HasLocalFilesystemAccess *bool                              `json:"hasLocalFilesystemAccess,omitempty"`
	UseHardlinks             *bool                              `json:"useHardlinks,omitempty"`
	HardlinkBaseDir          *string                            `json:"hardlinkBaseDir,omitempty"`
	HardlinkDirPreset        *string                            `json:"hardlinkDirPreset,omitempty"`
	UseReflinks              *bool                              `json:"useReflinks,omitempty"`
	FallbackToRegularMode    *bool                              `json:"fallbackToRegularMode,omitempty"`
	ReannounceSettings       *InstanceReannounceSettingsPayload `json:"reannounceSettings,omitempty"`
}

type UpdateInstanceStatusRequest struct {
	IsActive bool `json:"isActive"`
}

// InstanceResponse represents an instance in API responses
type InstanceResponse struct {
	ID                       int                               `json:"id"`
	Name                     string                            `json:"name"`
	Host                     string                            `json:"host"`
	Username                 string                            `json:"username"`
	BasicUsername            *string                           `json:"basicUsername,omitempty"`
	TLSSkipVerify            bool                              `json:"tlsSkipVerify"`
	HasLocalFilesystemAccess bool                              `json:"hasLocalFilesystemAccess"`
	UseHardlinks             bool                              `json:"useHardlinks"`
	HardlinkBaseDir          string                            `json:"hardlinkBaseDir"`
	HardlinkDirPreset        string                            `json:"hardlinkDirPreset"`
	UseReflinks              bool                              `json:"useReflinks"`
	FallbackToRegularMode    bool                              `json:"fallbackToRegularMode"`
	Connected                bool                              `json:"connected"`
	HasDecryptionError       bool                              `json:"hasDecryptionError"`
	RecentErrors             []models.InstanceError            `json:"recentErrors,omitempty"`
	ConnectionStatus         string                            `json:"connectionStatus,omitempty"`
	SortOrder                int                               `json:"sortOrder"`
	IsActive                 bool                              `json:"isActive"`
	ReannounceSettings       InstanceReannounceSettingsPayload `json:"reannounceSettings"`
}

// InstanceReannounceSettingsPayload carries tracker monitoring config.
type InstanceReannounceSettingsPayload struct {
	Enabled                   bool     `json:"enabled"`
	InitialWaitSeconds        int      `json:"initialWaitSeconds"`
	ReannounceIntervalSeconds int      `json:"reannounceIntervalSeconds"`
	MaxAgeSeconds             int      `json:"maxAgeSeconds"`
	MaxRetries                int      `json:"maxRetries"`
	Aggressive                bool     `json:"aggressive"`
	MonitorAll                bool     `json:"monitorAll"`
	ExcludeCategories         bool     `json:"excludeCategories"`
	Categories                []string `json:"categories"`
	ExcludeTags               bool     `json:"excludeTags"`
	Tags                      []string `json:"tags"`
	ExcludeTrackers           bool     `json:"excludeTrackers"`
	Trackers                  []string `json:"trackers"`
}

// TestConnectionResponse represents connection test results
type TestConnectionResponse struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (p *InstanceReannounceSettingsPayload) toModel(instanceID int, base *models.InstanceReannounceSettings) *models.InstanceReannounceSettings {
	var target *models.InstanceReannounceSettings
	if base != nil {
		clone := *base
		target = &clone
	} else {
		target = models.DefaultInstanceReannounceSettings(instanceID)
	}
	if p == nil {
		target.InstanceID = instanceID
		return target
	}
	target.InstanceID = instanceID
	target.Enabled = p.Enabled
	target.InitialWaitSeconds = p.InitialWaitSeconds
	target.ReannounceIntervalSeconds = p.ReannounceIntervalSeconds
	target.MaxAgeSeconds = p.MaxAgeSeconds
	target.MaxRetries = p.MaxRetries
	target.Aggressive = p.Aggressive
	target.MonitorAll = p.MonitorAll
	target.ExcludeCategories = p.ExcludeCategories
	target.Categories = append([]string{}, p.Categories...)
	target.ExcludeTags = p.ExcludeTags
	target.Tags = append([]string{}, p.Tags...)
	target.ExcludeTrackers = p.ExcludeTrackers
	target.Trackers = append([]string{}, p.Trackers...)
	return target
}

func payloadFromModel(settings *models.InstanceReannounceSettings) InstanceReannounceSettingsPayload {
	if settings == nil {
		settings = models.DefaultInstanceReannounceSettings(0)
	}
	return InstanceReannounceSettingsPayload{
		Enabled:                   settings.Enabled,
		InitialWaitSeconds:        settings.InitialWaitSeconds,
		ReannounceIntervalSeconds: settings.ReannounceIntervalSeconds,
		MaxAgeSeconds:             settings.MaxAgeSeconds,
		MaxRetries:                settings.MaxRetries,
		Aggressive:                settings.Aggressive,
		MonitorAll:                settings.MonitorAll,
		ExcludeCategories:         settings.ExcludeCategories,
		Categories:                append([]string{}, settings.Categories...),
		ExcludeTags:               settings.ExcludeTags,
		Tags:                      append([]string{}, settings.Tags...),
		ExcludeTrackers:           settings.ExcludeTrackers,
		Trackers:                  append([]string{}, settings.Trackers...),
	}
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

// UpdateInstanceOrder updates the display order for all instances
func (h *InstancesHandler) UpdateInstanceOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceIDs []int `json:"instanceIds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.InstanceIDs) == 0 {
		RespondError(w, http.StatusBadRequest, "instanceIds must not be empty")
		return
	}

	instances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances for reorder")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	if len(req.InstanceIDs) != len(instances) {
		RespondError(w, http.StatusBadRequest, "instanceIds must include all instances")
		return
	}

	validIDs := make(map[int]struct{}, len(instances))
	for _, inst := range instances {
		validIDs[inst.ID] = struct{}{}
	}

	seen := make(map[int]struct{}, len(req.InstanceIDs))
	for _, id := range req.InstanceIDs {
		if _, ok := validIDs[id]; !ok {
			RespondError(w, http.StatusBadRequest, "instanceIds contains an unknown instance")
			return
		}
		if _, ok := seen[id]; ok {
			RespondError(w, http.StatusBadRequest, "instanceIds must not contain duplicates")
			return
		}
		seen[id] = struct{}{}
	}

	if err := h.instanceStore.UpdateOrder(r.Context(), req.InstanceIDs); err != nil {
		log.Error().Err(err).Msg("Failed to update instance order")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance order")
		return
	}

	updatedInstances, err := h.instanceStore.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances after reorder")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	response := h.buildInstanceResponsesParallel(r.Context(), updatedInstances)
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
	instance, err := h.instanceStore.Create(r.Context(), req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword, req.TLSSkipVerify, req.HasLocalFilesystemAccess)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create instance")
		RespondError(w, http.StatusInternalServerError, "Failed to create instance")
		return
	}

	settings, err := h.persistReannounceSettings(r.Context(), instance.ID, req.ReannounceSettings)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to save reannounce settings")
		return
	}

	// Return quickly without testing connection
	response := h.buildQuickInstanceResponse(instance)
	response.ReannounceSettings = payloadFromModel(settings)

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

	// Validate hardlink/reflink settings
	effectiveLocalAccess := existingInstance.HasLocalFilesystemAccess
	if req.HasLocalFilesystemAccess != nil {
		effectiveLocalAccess = *req.HasLocalFilesystemAccess
	}
	effectiveUseHardlinks := existingInstance.UseHardlinks
	if req.UseHardlinks != nil {
		effectiveUseHardlinks = *req.UseHardlinks
	}
	effectiveUseReflinks := existingInstance.UseReflinks
	if req.UseReflinks != nil {
		effectiveUseReflinks = *req.UseReflinks
	}
	effectiveHardlinkBaseDir := existingInstance.HardlinkBaseDir
	if req.HardlinkBaseDir != nil {
		effectiveHardlinkBaseDir = *req.HardlinkBaseDir
	}

	// Mutual exclusivity: cannot enable both hardlink and reflink modes
	if effectiveUseHardlinks && effectiveUseReflinks {
		RespondError(w, http.StatusBadRequest, "Cannot enable both hardlink and reflink modes - they are mutually exclusive")
		return
	}

	if effectiveUseHardlinks {
		if !effectiveLocalAccess {
			RespondError(w, http.StatusBadRequest, "Cannot enable hardlink mode without local filesystem access")
			return
		}
		if effectiveHardlinkBaseDir == "" {
			RespondError(w, http.StatusBadRequest, "Cannot enable hardlink mode without a base directory")
			return
		}
	}

	if effectiveUseReflinks {
		if !effectiveLocalAccess {
			RespondError(w, http.StatusBadRequest, "Cannot enable reflink mode without local filesystem access")
			return
		}
		if effectiveHardlinkBaseDir == "" {
			RespondError(w, http.StatusBadRequest, "Cannot enable reflink mode without a base directory")
			return
		}
	}

	// Update instance
	updateParams := &models.InstanceUpdateParams{
		TLSSkipVerify:            req.TLSSkipVerify,
		HasLocalFilesystemAccess: req.HasLocalFilesystemAccess,
		UseHardlinks:             req.UseHardlinks,
		HardlinkBaseDir:          req.HardlinkBaseDir,
		HardlinkDirPreset:        req.HardlinkDirPreset,
		UseReflinks:              req.UseReflinks,
		FallbackToRegularMode:    req.FallbackToRegularMode,
	}
	instance, err := h.instanceStore.Update(r.Context(), instanceID, req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword, updateParams)
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

	var settings *models.InstanceReannounceSettings
	if req.ReannounceSettings != nil {
		var err error
		settings, err = h.persistReannounceSettings(r.Context(), instanceID, req.ReannounceSettings)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to save reannounce settings")
			return
		}
	} else {
		settings = h.loadReannounceSettings(r.Context(), instanceID)
	}

	// Return quickly without testing connection
	response := h.buildQuickInstanceResponse(instance)
	response.ReannounceSettings = payloadFromModel(settings)

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

// UpdateInstanceStatus toggles whether an instance should be actively polled
func (h *InstancesHandler) UpdateInstanceStatus(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req UpdateInstanceStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	instance, err := h.instanceStore.SetActiveState(r.Context(), instanceID, req.IsActive)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to update instance status")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance status")
		return
	}

	if !req.IsActive {
		h.clientPool.RemoveClient(instanceID)
	} else {
		// Clear backoff state and errors when re-enabling instance
		h.clientPool.ResetFailureTracking(instanceID)
		go h.testConnectionAsync(instanceID)
	}

	response := h.buildQuickInstanceResponse(instance)
	response.ReannounceSettings = h.getReannounceSettingsPayload(r.Context(), instanceID)
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
		if errors.Is(err, internalqbittorrent.ErrInstanceDisabled) {
			response := TestConnectionResponse{
				Connected: false,
				Message:   "Instance is disabled",
				Error:     err.Error(),
			}
			RespondJSON(w, http.StatusOK, response)
			return
		}
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
