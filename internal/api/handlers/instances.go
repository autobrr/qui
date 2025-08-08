package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

func NewInstancesHandler(instanceStore *models.InstanceStore, clientPool *internalqbittorrent.ClientPool, syncManager *internalqbittorrent.SyncManager) *InstancesHandler {
	return &InstancesHandler{
		instanceStore: instanceStore,
		clientPool:    clientPool,
		syncManager:   syncManager,
	}
}

// CreateInstanceRequest represents a request to create a new instance
type CreateInstanceRequest struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Username      string  `json:"username"`
	Password      string  `json:"password"`
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
}

// UpdateInstanceRequest represents a request to update an instance
type UpdateInstanceRequest struct {
	Name          string  `json:"name"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Username      string  `json:"username"`
	Password      string  `json:"password,omitempty"` // Optional for updates
	BasicUsername *string `json:"basicUsername,omitempty"`
	BasicPassword *string `json:"basicPassword,omitempty"`
}

// ListInstances returns all instances
func (h *InstancesHandler) ListInstances(w http.ResponseWriter, r *http.Request) {
	// Check if only active instances are requested
	activeOnly := r.URL.Query().Get("active") == "true"

	instances, err := h.instanceStore.List(activeOnly)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances")
		RespondError(w, http.StatusInternalServerError, "Failed to list instances")
		return
	}

	// Don't include encrypted passwords in response
	response := make([]map[string]interface{}, len(instances))
	for i, instance := range instances {
		response[i] = map[string]interface{}{
			"id":              instance.ID,
			"name":            instance.Name,
			"host":            instance.Host,
			"port":            instance.Port,
			"username":        instance.Username,
			"basicUsername":   instance.BasicUsername,
			"isActive":        instance.IsActive,
			"lastConnectedAt": instance.LastConnectedAt,
			"createdAt":       instance.CreatedAt,
			"updatedAt":       instance.UpdatedAt,
		}
	}

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
	if req.Name == "" || req.Host == "" || req.Port == 0 {
		RespondError(w, http.StatusBadRequest, "Name, host, and port are required")
		return
	}

	// Create instance
	instance, err := h.instanceStore.Create(req.Name, req.Host, req.Port, req.Username, req.Password, req.BasicUsername, req.BasicPassword)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create instance")
		RespondError(w, http.StatusInternalServerError, "Failed to create instance")
		return
	}

	// Test connection
	client, err := h.clientPool.GetClient(instance.ID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instance.ID).Msg("Failed to connect to new instance")
		// Don't fail the creation, just warn
	} else {
		// Connection successful
		_ = client
	}

	RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"id":              instance.ID,
		"name":            instance.Name,
		"host":            instance.Host,
		"port":            instance.Port,
		"username":        instance.Username,
		"basicUsername":   instance.BasicUsername,
		"isActive":        instance.IsActive,
		"lastConnectedAt": instance.LastConnectedAt,
		"createdAt":       instance.CreatedAt,
		"updatedAt":       instance.UpdatedAt,
	})
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
	if req.Name == "" || req.Host == "" || req.Port == 0 {
		RespondError(w, http.StatusBadRequest, "Name, host, and port are required")
		return
	}

	// Update instance
	instance, err := h.instanceStore.Update(instanceID, req.Name, req.Host, req.Port, req.Username, req.Password, req.BasicUsername, req.BasicPassword)
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

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"id":              instance.ID,
		"name":            instance.Name,
		"host":            instance.Host,
		"port":            instance.Port,
		"username":        instance.Username,
		"basicUsername":   instance.BasicUsername,
		"isActive":        instance.IsActive,
		"lastConnectedAt": instance.LastConnectedAt,
		"createdAt":       instance.CreatedAt,
		"updatedAt":       instance.UpdatedAt,
	})
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
	if err := h.instanceStore.Delete(instanceID); err != nil {
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

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Instance deleted successfully",
	})
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
	client, err := h.clientPool.GetClient(instanceID)
	if err != nil {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	// Perform health check
	if err := client.HealthCheck(r.Context()); err != nil {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"connected": true,
		"message":   "Connection successful",
	})
}

// GetInstanceStats returns statistics for an instance
func (h *InstancesHandler) GetInstanceStats(w http.ResponseWriter, r *http.Request) {
	// Get instance ID from URL
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Default stats for when connection fails
	stats := map[string]interface{}{
		"instanceId": instanceID,
		"connected":  false,
		"torrents": map[string]interface{}{
			"total":       0,
			"downloading": 0,
			"seeding":     0,
			"paused":      0,
			"error":       0,
			"completed":   0,
		},
		"speeds": map[string]interface{}{
			"download": 0,
			"upload":   0,
		},
	}

	// Get client
	client, err := h.clientPool.GetClient(instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		// Return default stats instead of error
		RespondJSON(w, http.StatusOK, stats)
		return
	}

	// Update connected status
	stats["connected"] = client.IsHealthy()

	// Get stats from the sync manager which uses cached data
	// This ensures the dashboard doesn't make slow direct API calls to qBittorrent
	// Use a longer timeout for slow instances with 10k+ torrents
	// 30 seconds should be enough for initial cold cache load
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	
	torrentCounts, err := h.syncManager.GetTorrentCounts(ctx, instanceID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn().Int("instanceID", instanceID).Msg("Timeout getting torrent counts for dashboard stats")
		} else {
			log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get torrent counts")
		}
		// Return default stats instead of error
		RespondJSON(w, http.StatusOK, stats)
		return
	}

	// Update stats with counts from cached data
	stats["torrents"] = map[string]interface{}{
		"total":       torrentCounts.Total,
		"downloading": torrentCounts.Status["downloading"],
		"seeding":     torrentCounts.Status["seeding"],
		"paused":      torrentCounts.Status["paused"],
		"error":       torrentCounts.Status["errored"],
		"completed":   torrentCounts.Status["completed"],
	}

	// Get speeds from the sync manager which calculates from cached torrents
	// This avoids making slow API calls to qBittorrent for large instances
	speeds, err := h.syncManager.GetInstanceSpeeds(ctx, instanceID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Warn().Int("instanceID", instanceID).Msg("Timeout getting instance speeds")
		} else {
			log.Warn().Err(err).Int("instanceID", instanceID).Msg("Failed to get instance speeds")
		}
		// Set default speeds
		stats["speeds"] = map[string]interface{}{
			"download": 0,
			"upload":   0,
		}
	} else {
		// Use calculated speeds from cached torrent data
		stats["speeds"] = map[string]interface{}{
			"download": speeds.Download,
			"upload":   speeds.Upload,
		}
	}

	RespondJSON(w, http.StatusOK, stats)
}
