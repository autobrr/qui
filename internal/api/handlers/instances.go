package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

type InstancesHandler struct {
	instanceStore *models.InstanceStore
	clientManager *internalqbittorrent.ClientManager
}

func NewInstancesHandler(instanceStore *models.InstanceStore, clientManager *internalqbittorrent.ClientManager) *InstancesHandler {
	return &InstancesHandler{
		instanceStore: instanceStore,
		clientManager: clientManager,
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

// SimpleTorrentCounts represents basic torrent counts for dashboard
type SimpleTorrentCounts struct {
	All         int `json:"all"`
	Downloading int `json:"downloading"`
	Seeding     int `json:"seeding"`
	Completed   int `json:"completed"`
	Paused      int `json:"paused"`
	Error       int `json:"error"`
}

// calculateTorrentCounts calculates basic torrent counts by status
func (h *InstancesHandler) calculateTorrentCounts(torrents []qbt.Torrent) *SimpleTorrentCounts {
	counts := &SimpleTorrentCounts{}
	counts.All = len(torrents)

	for _, torrent := range torrents {
		state := strings.ToLower(string(torrent.State))

		switch state {
		case "downloading", "metadl", "stalleddl", "forceddl", "queueddl":
			counts.Downloading++
		case "uploading", "stalledup", "forcedup", "queuedup":
			counts.Seeding++
		case "pauseddl", "pausedup":
			counts.Paused++
		case "error", "missingfiles":
			counts.Error++
		}

		if torrent.Progress >= 1.0 {
			counts.Completed++
		}
	}

	return counts
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
	client, err := h.clientManager.GetClient(context.Background(), instance.ID)
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

	// Client manager will handle reconnection automatically when needed
	// No need to explicitly remove client

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

	// Client manager will handle cleanup automatically
	// No need to explicitly remove client

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
	client, err := h.clientManager.GetClient(r.Context(), instanceID)
	if err != nil {
		RespondJSON(w, http.StatusOK, map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	// Perform health check by trying a simple API call
	if _, err := client.GetWebAPIVersionCtx(r.Context()); err != nil {
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
	client, err := h.clientManager.GetClient(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("Failed to get client")
		// Return default stats instead of error
		RespondJSON(w, http.StatusOK, stats)
		return
	}

	// Update connected status (test with simple API call)
	stats["connected"] = true
	if _, err := client.GetWebAPIVersionCtx(r.Context()); err != nil {
		stats["connected"] = false
	}

	// Get basic stats from client
	// For dashboard, we'll use a simpler approach - just get torrent count and basic server state
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get actual count by making a torrent query
	allTorrents, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	if err == nil {
		stats["torrents"] = map[string]interface{}{
			"total":       len(allTorrents),
			"downloading": h.countTorrentsByState(allTorrents, []string{"downloading", "metaDL", "forcedDL", "queuedDL", "stalledDL"}),
			"seeding":     h.countTorrentsByState(allTorrents, []string{"uploading", "forcedUP", "queuedUP", "stalledUP"}),
			"completed":   h.countCompletedTorrents(allTorrents),
		}

		// Calculate speeds from the torrents
		var totalDownloadSpeed, totalUploadSpeed int64
		for _, torrent := range allTorrents {
			totalDownloadSpeed += torrent.DlSpeed
			totalUploadSpeed += torrent.UpSpeed
		}

		stats["speeds"] = map[string]interface{}{
			"download": totalDownloadSpeed,
			"upload":   totalUploadSpeed,
		}
	}

	RespondJSON(w, http.StatusOK, stats)
}

// Helper functions for counting torrents by state
func (h *InstancesHandler) countTorrentsByState(torrents []qbt.Torrent, states []string) int {
	count := 0
	for _, torrent := range torrents {
		for _, state := range states {
			if strings.EqualFold(string(torrent.State), state) {
				count++
				break
			}
		}
	}
	return count
}

func (h *InstancesHandler) countCompletedTorrents(torrents []qbt.Torrent) int {
	count := 0
	for _, torrent := range torrents {
		if torrent.Progress >= 1.0 {
			count++
		}
	}
	return count
}
