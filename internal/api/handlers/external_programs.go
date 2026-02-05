// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/externalprograms"
)

type ExternalProgramsHandler struct {
	externalProgramStore   *models.ExternalProgramStore
	externalProgramService *externalprograms.Service
	clientPool             *qbittorrent.ClientPool
	automationStore        *models.AutomationStore
}

func NewExternalProgramsHandler(
	store *models.ExternalProgramStore,
	service *externalprograms.Service,
	pool *qbittorrent.ClientPool,
	automationStore *models.AutomationStore,
) *ExternalProgramsHandler {
	return &ExternalProgramsHandler{
		externalProgramStore:   store,
		externalProgramService: service,
		clientPool:             pool,
		automationStore:        automationStore,
	}
}

// validateProgramInput validates name and path for external program create/update operations.
// Returns an error message and HTTP status code if validation fails.
func (h *ExternalProgramsHandler) validateProgramInput(name, path string) (string, int, bool) {
	if name == "" {
		return "Name is required", http.StatusBadRequest, false
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return "Path is required", http.StatusBadRequest, false
	}

	// Validate path against allowlist using the shared service (fail closed if service is nil)
	if h.externalProgramService == nil || !h.externalProgramService.IsPathAllowed(path) {
		return "Program path is not allowed", http.StatusForbidden, false
	}

	return "", 0, true
}

// ListExternalPrograms handles GET /api/external-programs
func (h *ExternalProgramsHandler) ListExternalPrograms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	programs, err := h.externalProgramStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list external programs")
		http.Error(w, "Failed to list external programs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(programs); err != nil {
		log.Error().Err(err).Msg("Failed to encode external programs response")
	}
}

// CreateExternalProgram handles POST /api/external-programs
func (h *ExternalProgramsHandler) CreateExternalProgram(w http.ResponseWriter, r *http.Request) {
	var req models.ExternalProgramCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode create external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate and normalize input
	req.Path = strings.TrimSpace(req.Path)
	if errMsg, status, ok := h.validateProgramInput(req.Name, req.Path); !ok {
		http.Error(w, errMsg, status)
		return
	}

	ctx := r.Context()
	program, err := h.externalProgramStore.Create(ctx, &req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create external program")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			http.Error(w, "A program with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create external program", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(program); err != nil {
		log.Error().Err(err).Msg("Failed to encode created program response")
	}
}

// UpdateExternalProgram handles PUT /api/external-programs/{id}
func (h *ExternalProgramsHandler) UpdateExternalProgram(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		http.Error(w, "Missing program ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid program ID", http.StatusBadRequest)
		return
	}

	var req models.ExternalProgramUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode update external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate and normalize input
	req.Path = strings.TrimSpace(req.Path)
	if errMsg, status, ok := h.validateProgramInput(req.Name, req.Path); !ok {
		http.Error(w, errMsg, status)
		return
	}

	ctx := r.Context()
	program, err := h.externalProgramStore.Update(ctx, id, &req)
	if err != nil {
		if err == models.ErrExternalProgramNotFound {
			http.Error(w, "Program not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to update external program")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			http.Error(w, "A program with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to update external program", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(program); err != nil {
		log.Error().Err(err).Msg("Failed to encode updated program response")
	}
}

// DeleteExternalProgram handles DELETE /api/external-programs/{id}
// Query params:
//   - force=true: Proceed with deletion even if automations reference this program.
//     The external program action will be removed from all referencing automations.
//
// If automations reference this program and force is not set, returns 409 Conflict
// with a list of affected automations.
func (h *ExternalProgramsHandler) DeleteExternalProgram(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		http.Error(w, "Missing program ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid program ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	force := r.URL.Query().Get("force") == "true"

	// Check if any automations reference this program
	if h.automationStore != nil {
		refs, err := h.automationStore.FindByExternalProgramID(ctx, id)
		if err != nil {
			log.Error().Err(err).Int("id", id).Msg("Failed to check automation references")
			http.Error(w, "Failed to check automation references", http.StatusInternalServerError)
			return
		}

		if len(refs) > 0 {
			if !force {
				// Return conflict with list of affected automations
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				if err := json.NewEncoder(w).Encode(map[string]any{
					"error":       "Program is referenced by automations",
					"message":     "This program is used by one or more automation rules. Use force=true to delete anyway, which will remove the external program action from these automations.",
					"automations": refs,
				}); err != nil {
					log.Error().Err(err).Msg("Failed to encode conflict response")
				}
				return
			}

			// Force delete: clear the external program action from all referencing automations
			cleared, err := h.automationStore.ClearExternalProgramAction(ctx, id)
			if err != nil {
				log.Error().Err(err).Int("id", id).Msg("Failed to clear external program actions from automations")
				http.Error(w, "Failed to clear external program actions from automations", http.StatusInternalServerError)
				return
			}
			log.Info().Int("id", id).Int64("automationsUpdated", cleared).Msg("Cleared external program action from automations")
		}
	}

	// Delete the program
	if err := h.externalProgramStore.Delete(ctx, id); err != nil {
		if err == models.ErrExternalProgramNotFound {
			http.Error(w, "Program not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to delete external program")
		http.Error(w, "Failed to delete external program", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ExecuteExternalProgram handles POST /api/external-programs/execute
func (h *ExternalProgramsHandler) ExecuteExternalProgram(w http.ResponseWriter, r *http.Request) {
	var req models.ExternalProgramExecute
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode execute external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ProgramID == 0 {
		http.Error(w, "Program ID is required", http.StatusBadRequest)
		return
	}

	if req.InstanceID == 0 {
		http.Error(w, "Instance ID is required", http.StatusBadRequest)
		return
	}

	if len(req.Hashes) == 0 {
		http.Error(w, "At least one torrent hash is required", http.StatusBadRequest)
		return
	}

	if h.externalProgramService == nil {
		http.Error(w, "External program service not configured", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	// Get the program configuration (we need it to get the instance ID for bulk torrent fetch)
	program, err := h.externalProgramStore.GetByID(ctx, req.ProgramID)
	if err != nil {
		if err == models.ErrExternalProgramNotFound {
			http.Error(w, "Program not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("programId", req.ProgramID).Msg("Failed to get external program")
		http.Error(w, "Failed to get program configuration", http.StatusInternalServerError)
		return
	}

	// Pre-check: program must be enabled
	if !program.Enabled {
		http.Error(w, "Program is disabled", http.StatusBadRequest)
		return
	}

	// Pre-check: path must be allowed
	if !h.externalProgramService.IsPathAllowed(program.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
		return
	}

	// Get client for the instance
	client, err := h.clientPool.GetClient(ctx, req.InstanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceId", req.InstanceID).Msg("Failed to get client for instance")
		http.Error(w, fmt.Sprintf("Failed to get client for instance: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch all torrents once (O(m) instead of O(n·m) where n=hashes, m=torrents)
	torrents, err := client.GetTorrents(qbt.TorrentFilterOptions{})
	if err != nil {
		log.Error().Err(err).Int("instanceId", req.InstanceID).Msg("Failed to get torrents from instance")
		http.Error(w, fmt.Sprintf("Failed to get torrents: %v", err), http.StatusInternalServerError)
		return
	}

	// Build hash index for O(1) lookups
	torrentIndex := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentIndex[strings.ToLower(torrents[i].Hash)] = &torrents[i]
	}

	// Execute for each torrent hash using the shared service
	results := make([]map[string]any, 0, len(req.Hashes))
	for _, hash := range req.Hashes {
		result := map[string]any{
			"hash":    hash,
			"success": false,
		}

		// Look up torrent in the pre-built index (O(1) lookup)
		torrent, found := torrentIndex[strings.ToLower(hash)]
		if !found {
			result["error"] = fmt.Sprintf("Torrent with hash %s not found", hash)
			results = append(results, result)
			continue
		}

		// Execute using the shared external programs service
		execResult := h.externalProgramService.Execute(ctx, externalprograms.ExecuteRequest{
			Program:    program,
			Torrent:    torrent,
			InstanceID: req.InstanceID,
		})
		result["success"] = execResult.Success
		if execResult.Success {
			result["message"] = execResult.Message
		} else if execResult.Error != nil {
			result["error"] = execResult.Error.Error()
		}

		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"results": results}); err != nil {
		log.Error().Err(err).Msg("Failed to encode execute response")
	}
}
