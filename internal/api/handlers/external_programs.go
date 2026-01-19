// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/externalprograms"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type ExternalProgramsHandler struct {
	externalProgramStore *models.ExternalProgramStore
	clientPool           *qbittorrent.ClientPool
	config               *domain.Config
}

func NewExternalProgramsHandler(store *models.ExternalProgramStore, pool *qbittorrent.ClientPool, cfg *domain.Config) *ExternalProgramsHandler {
	return &ExternalProgramsHandler{
		externalProgramStore: store,
		clientPool:           pool,
		config:               cfg,
	}
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
	json.NewEncoder(w).Encode(programs)
}

// CreateExternalProgram handles POST /api/external-programs
func (h *ExternalProgramsHandler) CreateExternalProgram(w http.ResponseWriter, r *http.Request) {
	var req models.ExternalProgramCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode create external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	if !h.isPathAllowed(req.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
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
	json.NewEncoder(w).Encode(program)
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

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	if !h.isPathAllowed(req.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
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
	json.NewEncoder(w).Encode(program)
}

// DeleteExternalProgram handles DELETE /api/external-programs/{id}
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

	ctx := r.Context()

	// Get the program configuration
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

	if !h.isPathAllowed(program.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
		return
	}

	if !program.Enabled {
		http.Error(w, "Program is disabled", http.StatusBadRequest)
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

	// Execute for each torrent hash
	results := make([]map[string]any, 0, len(req.Hashes))
	for _, hash := range req.Hashes {
		result := h.executeForHash(ctx, program, hash, torrentIndex)
		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": results,
	})
}

// executeForHash executes the external program for a single torrent hash
func (h *ExternalProgramsHandler) executeForHash(ctx context.Context, program *models.ExternalProgram, hash string, torrentIndex map[string]*qbt.Torrent) map[string]any {
	result := map[string]any{
		"hash":    hash,
		"success": false,
	}

	// Look up torrent in the pre-built index (O(1) lookup)
	torrent, found := torrentIndex[strings.ToLower(hash)]
	if !found {
		result["error"] = fmt.Sprintf("Torrent with hash %s not found", hash)
		return result
	}

	// Execute the external program (async mode - fire and forget)
	// The allowlist check is already done at the handler level, so we pass empty here
	execResult := externalprograms.Execute(ctx, program, torrent, externalprograms.DefaultOptions())

	if !execResult.Started {
		result["error"] = fmt.Sprintf("Failed to start program: %v", execResult.Error)
		return result
	}

	result["success"] = true
	if program.UseTerminal {
		result["message"] = "Terminal window opened successfully"
	} else {
		result["message"] = "Program started successfully"
	}

	return result
}

func (h *ExternalProgramsHandler) isPathAllowed(programPath string) bool {
	if h == nil || h.config == nil {
		return true
	}
	return externalprograms.IsPathAllowed(programPath, h.config.ExternalProgramAllowList)
}
