// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

type InstancesHandler struct {
	instanceStore *models.InstanceStore
	clientPool    *internalqbittorrent.ClientPool
	syncManager   *internalqbittorrent.SyncManager
}

type connectionStatus struct {
	connected bool
	error     string
}

func NewInstancesHandler(instanceStore *models.InstanceStore, clientPool *internalqbittorrent.ClientPool, syncManager *internalqbittorrent.SyncManager) *InstancesHandler {
	return &InstancesHandler{
		instanceStore: instanceStore,
		clientPool:    clientPool,
		syncManager:   syncManager,
	}
}

func (h *InstancesHandler) isDecryptionError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := strings.ToLower(err.Error())
	return strings.Contains(errorStr, "decrypt") &&
		(strings.Contains(errorStr, "password") || strings.Contains(errorStr, "cipher"))
}

func (h *InstancesHandler) testInstanceConnection(ctx context.Context, instanceID int) (connected bool, connectionError string) {
	cacheKey := fmt.Sprintf("connection:status:%d", instanceID)
	cache := h.clientPool.GetCache()

	if cached, found := cache.Get(cacheKey); found {
		if status, ok := cached.(connectionStatus); ok {
			log.Debug().Int("instanceID", instanceID).Bool("connected", status.connected).Msg("Using cached connection status")
			return status.connected, status.error
		}
	}

	// Use shorter timeout for UI operations to prevent hanging
	client, err := h.clientPool.GetClientWithTimeout(ctx, instanceID, 5*time.Second)
	if err != nil {
		status := connectionStatus{connected: false, error: err.Error()}
		cache.SetWithTTL(cacheKey, status, 1, 5*time.Second)

		if !h.isDecryptionError(err) { // Only log if it's not a decryption error (those are already logged in pool)
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to connect to instance")
		}
		return false, err.Error()
	}

	if err := client.HealthCheck(ctx); err != nil {
		status := connectionStatus{connected: false, error: err.Error()}
		cache.SetWithTTL(cacheKey, status, 1, 5*time.Second)

		log.Warn().Err(err).Int("instanceID", instanceID).Msg("Health check failed for instance")
		return false, err.Error()
	}

	status := connectionStatus{connected: true, error: ""}
	cache.SetWithTTL(cacheKey, status, 1, 5*time.Second)

	return true, ""
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
			log.Warn().Err(ctx.Err()).Msg("Context cancelled while building instance responses")
			return responses[:i]
		}
	}

	return responses
}

func (h *InstancesHandler) buildInstanceResponse(ctx context.Context, instance *models.Instance) InstanceResponse {
	// Use cached connection status only, do not test connection synchronously
	cacheKey := fmt.Sprintf("connection:status:%d", instance.ID)
	cache := h.clientPool.GetCache()
	var connected bool
	var connectionError string
	if cached, found := cache.Get(cacheKey); found {
		if status, ok := cached.(connectionStatus); ok {
			connected = status.connected
			connectionError = status.error
		}
	}

	decryptionErrorInstances := h.clientPool.GetInstancesWithDecryptionErrors()
	hasDecryptionError := slices.Contains(decryptionErrorInstances, instance.ID)

	response := InstanceResponse{
		ID:                 instance.ID,
		Name:               instance.Name,
		Host:               instance.Host,
		Username:           instance.Username,
		BasicUsername:      instance.BasicUsername,
		IsActive:           instance.IsActive,
		LastConnectedAt:    instance.LastConnectedAt,
		CreatedAt:          instance.CreatedAt,
		UpdatedAt:          instance.UpdatedAt,
		Connected:          connected,
		HasDecryptionError: hasDecryptionError,
	}

	if connectionError != "" {
		response.ConnectionError = connectionError
	}

	return response
}

type CreateInstanceRequest struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Username      string  `json:"username"`
	Password      string  `json:"password"`
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
}

type UpdateInstanceRequest struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Username      string  `json:"username"`
	Password      string  `json:"password,omitempty"` // Optional for updates
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
}

type InstanceResponse struct {
	ID                 int        `json:"id"`
	Name               string     `json:"name"`
	Host               string     `json:"host"`
	Username           string     `json:"username"`
	BasicUsername      *string    `json:"basicUsername,omitempty"`
	IsActive           bool       `json:"isActive"`
	LastConnectedAt    *time.Time `json:"lastConnectedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	Connected          bool       `json:"connected"`
	ConnectionError    string     `json:"connectionError,omitempty"`
	HasDecryptionError bool       `json:"hasDecryptionError"`
}

type TestConnectionResponse struct {
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

type DeleteInstanceResponse struct {
	Message string `json:"message"`
}

type InstanceStatsResponse struct {
	InstanceID int          `json:"instanceId"`
	Connected  bool         `json:"connected"`
	Torrents   TorrentStats `json:"torrents"`
	Speeds     SpeedStats   `json:"speeds"`
}

type TorrentStats struct {
	Total       int `json:"total"`
	Downloading int `json:"downloading"`
	Seeding     int `json:"seeding"`
	Paused      int `json:"paused"`
	Error       int `json:"error"`
	Completed   int `json:"completed"`
}

type SpeedStats struct {
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
}

func (h *InstancesHandler) ListInstances(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true"

	instances, err := h.instanceStore.List(r.Context(), activeOnly)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	response := h.buildInstanceResponsesParallel(r.Context(), instances)

	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var req CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" || req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Name and host are required")
		return
	}

	instance, err := h.instanceStore.Create(r.Context(), req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword)
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

func (h *InstancesHandler) UpdateInstance(w http.ResponseWriter, r *http.Request) {
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

	if req.Name == "" || req.Host == "" {
		RespondError(w, http.StatusBadRequest, "Name and host are required")
		return
	}

	instance, err := h.instanceStore.Update(r.Context(), instanceID, req.Name, req.Host, req.Username, req.Password, req.BasicUsername, req.BasicPassword)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to update instance")
		RespondError(w, http.StatusInternalServerError, "Failed to update instance")
		return
	}

	h.clientPool.RemoveClient(instanceID)

	// Return quickly without testing connection
	response := h.buildQuickInstanceResponse(instance)

	// Test connection asynchronously
	go h.testConnectionAsync(instance.ID)

	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	if err := h.instanceStore.Delete(r.Context(), instanceID); err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return
		}
		log.Error().Err(err).Msg("Failed to delete instance")
		RespondError(w, http.StatusInternalServerError, "Failed to delete instance")
		return
	}

	h.clientPool.RemoveClient(instanceID)

	response := DeleteInstanceResponse{
		Message: "Instance deleted successfully",
	}
	RespondJSON(w, http.StatusOK, response)
}

func (h *InstancesHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	client, err := h.clientPool.GetClient(r.Context(), instanceID)
	if err != nil {
		response := TestConnectionResponse{
			Connected: false,
			Error:     err.Error(),
		}
		RespondJSON(w, http.StatusOK, response)
		return
	}

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

func (h *InstancesHandler) getDefaultStats(instanceID int) InstanceStatsResponse {
	return InstanceStatsResponse{
		InstanceID: instanceID,
		Connected:  false,
		Torrents: TorrentStats{
			Total:       0,
			Downloading: 0,
			Seeding:     0,
			Paused:      0,
			Error:       0,
			Completed:   0,
		},
		Speeds: SpeedStats{
			Download: 0,
			Upload:   0,
		},
	}
}

func (h *InstancesHandler) buildStatsFromCounts(torrentCounts *internalqbittorrent.TorrentCounts) TorrentStats {
	return TorrentStats{
		Total:       torrentCounts.Total,
		Downloading: torrentCounts.Status["downloading"],
		Seeding:     torrentCounts.Status["seeding"],
		Paused:      torrentCounts.Status["paused"],
		Error:       torrentCounts.Status["errored"],
		Completed:   torrentCounts.Status["completed"],
	}
}

func (h *InstancesHandler) GetInstanceStats(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	defaultStats := h.getDefaultStats(instanceID)

	client, err := h.clientPool.GetClient(r.Context(), instanceID)
	if err != nil {
		if !h.isDecryptionError(err) { // Only log if it's not a decryption error (those are already logged in pool)
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		}
		RespondJSON(w, http.StatusOK, defaultStats)
		return
	}

	stats := defaultStats
	stats.Connected = client.IsHealthy()

	h.populateInstanceStats(r.Context(), instanceID, &stats)
	RespondJSON(w, http.StatusOK, stats)
}

func (h *InstancesHandler) populateInstanceStats(ctx context.Context, instanceID int, stats *InstanceStatsResponse) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	type countsResult struct {
		counts *internalqbittorrent.TorrentCounts
		err    error
	}
	type speedsResult struct {
		speeds *internalqbittorrent.InstanceSpeeds
		err    error
	}

	countsCh := make(chan countsResult, 1)
	speedsCh := make(chan speedsResult, 1)

	go func() {
		torrentCounts, err := h.syncManager.GetTorrentCounts(ctx, instanceID)
		countsCh <- countsResult{counts: torrentCounts, err: err}
	}()

	go func() {
		speeds, err := h.syncManager.GetInstanceSpeeds(ctx, instanceID)
		speedsCh <- speedsResult{speeds: speeds, err: err}
	}()

	var countsRes countsResult
	var speedsRes speedsResult

	for range 2 {
		select {
		case countsRes = <-countsCh:
			if countsRes.err != nil {
				if errors.Is(countsRes.err, context.DeadlineExceeded) {
					log.Warn().Int("instanceID", instanceID).Msg("Timeout getting torrent counts")
				} else {
					log.Error().Err(countsRes.err).Int("instanceID", instanceID).Msg("Failed to get torrent counts")
				}
			} else {
				stats.Torrents = h.buildStatsFromCounts(countsRes.counts)
			}
		case speedsRes = <-speedsCh:
			if speedsRes.err != nil {
				if errors.Is(speedsRes.err, context.DeadlineExceeded) {
					log.Warn().Int("instanceID", instanceID).Msg("Timeout getting instance speeds")
				} else {
					log.Warn().Err(speedsRes.err).Int("instanceID", instanceID).Msg("Failed to get instance speeds")
				}
			} else {
				stats.Speeds.Download = speedsRes.speeds.Download
				stats.Speeds.Upload = speedsRes.speeds.Upload
			}
		case <-ctx.Done():
			log.Warn().Err(ctx.Err()).Int("instanceID", instanceID).Msg("Context cancelled while populating instance stats")
			return
		}
	}
}

// buildQuickInstanceResponse creates a response without testing connection
func (h *InstancesHandler) buildQuickInstanceResponse(instance *models.Instance) InstanceResponse {
	return InstanceResponse{
		ID:                 instance.ID,
		Name:               instance.Name,
		Host:               instance.Host,
		Username:           instance.Username,
		BasicUsername:      instance.BasicUsername,
		IsActive:           instance.IsActive,
		LastConnectedAt:    instance.LastConnectedAt,
		CreatedAt:          instance.CreatedAt,
		UpdatedAt:          instance.UpdatedAt,
		Connected:          false, // Will be updated asynchronously
		ConnectionError:    "",
		HasDecryptionError: false,
	}
}

// testConnectionAsync tests connection in background and updates cache
func (h *InstancesHandler) testConnectionAsync(instanceID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Debug().Int("instanceID", instanceID).Msg("Testing connection asynchronously")

	// Use shorter timeout for UI operations
	client, err := h.clientPool.GetClientWithTimeout(ctx, instanceID, 5*time.Second)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Async connection test failed")

		// Cache the failure result
		cacheKey := fmt.Sprintf("connection:status:%d", instanceID)
		status := connectionStatus{connected: false, error: err.Error()}
		cache := h.clientPool.GetCache()
		cache.SetWithTTL(cacheKey, status, 1, 5*time.Second)
		return
	}

	if err := client.HealthCheck(ctx); err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("Async health check failed")

		// Cache the failure result
		cacheKey := fmt.Sprintf("connection:status:%d", instanceID)
		status := connectionStatus{connected: false, error: err.Error()}
		cache := h.clientPool.GetCache()
		cache.SetWithTTL(cacheKey, status, 1, 5*time.Second)
		return
	}

	log.Debug().Int("instanceID", instanceID).Msg("Async connection test succeeded")

	// Cache the success result
	cacheKey := fmt.Sprintf("connection:status:%d", instanceID)
	status := connectionStatus{connected: true, error: ""}
	cache := h.clientPool.GetCache()
	cache.SetWithTTL(cacheKey, status, 1, 5*time.Second)
}
