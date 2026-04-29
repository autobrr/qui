// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
)

// HelperHandler handles SSH/helper management endpoints for instances.
type HelperHandler struct {
	instanceStore *models.InstanceStore
	sshPool       *sshpool.Pool
}

// NewHelperHandler creates a new helper handler.
func NewHelperHandler(instanceStore *models.InstanceStore, sshPool *sshpool.Pool) *HelperHandler {
	return &HelperHandler{
		instanceStore: instanceStore,
		sshPool:       sshPool,
	}
}

type sshTestRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	AuthType string `json:"authType"` // "key" or "password"
	Key      string `json:"key,omitempty"`
	Password string `json:"password,omitempty"`
}

type sshTestResponse struct {
	Success     bool   `json:"success"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Error       string `json:"error,omitempty"`
}

// TestSSHConnection tests SSH connectivity with the provided credentials.
// POST /api/instances/{instanceID}/ssh-test
func (h *HelperHandler) TestSSHConnection(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req sshTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Host == "" || req.Username == "" {
		RespondError(w, http.StatusBadRequest, "Host and username are required")
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}

	// In this scaffold, we validate the request but don't actually dial.
	// Real SSH dial is wired in Stage C when the pool's Submit is implemented.
	_ = instanceID

	RespondJSON(w, http.StatusOK, sshTestResponse{
		Success: false,
		Error:   "SSH connection testing not yet implemented — deploy the helper when Stage C is complete",
	})
}

type helperStatusResponse struct {
	Deployed       bool   `json:"deployed"`
	Version        string `json:"helperVersion,omitempty"`
	Platform       string `json:"platform,omitempty"`
	Capabilities   string `json:"capabilities,omitempty"`
	AllowedRoots   string `json:"allowedRoots,omitempty"`
	ReflinkRoots   string `json:"reflinkRoots,omitempty"`
	DeployedAt     string `json:"deployedAt,omitempty"`
	LastActivityAt string `json:"lastActivityAt,omitempty"`
}

// GetHelperStatus returns the helper deployment status for an instance.
// GET /api/instances/{instanceID}/helper
func (h *HelperHandler) GetHelperStatus(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to load instance")
		return
	}
	if instance == nil {
		RespondError(w, http.StatusNotFound, "Instance not found")
		return
	}

	resp := helperStatusResponse{
		Deployed:     instance.HelperDeployedAt != nil,
		Version:      instance.HelperVersion,
		Platform:     instance.HelperPlatform,
		Capabilities: instance.HelperCapabilities,
		AllowedRoots: instance.HelperAllowedRoots,
		ReflinkRoots: instance.HelperReflinkRoots,
	}
	if instance.HelperDeployedAt != nil {
		resp.DeployedAt = instance.HelperDeployedAt.Format("2006-01-02T15:04:05Z")
	}
	if instance.HelperLastActivityAt != nil {
		resp.LastActivityAt = instance.HelperLastActivityAt.Format("2006-01-02T15:04:05Z")
	}

	RespondJSON(w, http.StatusOK, resp)
}

// DeployHelper deploys the helper binary to the remote host.
// POST /api/instances/{instanceID}/helper/deploy
func (h *HelperHandler) DeployHelper(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"status":  "not_implemented",
		"message": "Helper deployment is available when Stage C is complete",
	})
}

// RedeployHelper redeploys the helper binary (e.g., after a qui upgrade).
// POST /api/instances/{instanceID}/helper/redeploy
func (h *HelperHandler) RedeployHelper(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"status":  "not_implemented",
		"message": "Helper redeployment is available when Stage C is complete",
	})
}

// RemoveHelper removes the helper from the remote host.
// DELETE /api/instances/{instanceID}/helper
func (h *HelperHandler) RemoveHelper(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"status":  "not_implemented",
		"message": "Helper removal is available when Stage C is complete",
	})
}

// RemoveSSHCredentials clears SSH credentials for an instance.
// DELETE /api/instances/{instanceID}/ssh-credentials
func (h *HelperHandler) RemoveSSHCredentials(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"status":  "not_implemented",
		"message": "SSH credential removal is available when Stage C is complete",
	})
}
