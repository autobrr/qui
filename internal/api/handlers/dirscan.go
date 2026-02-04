// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
)

// DirScanHandler handles HTTP requests for directory scanning.
type DirScanHandler struct {
	service       *dirscan.Service
	instanceStore *models.InstanceStore
}

// NewDirScanHandler creates a new DirScanHandler.
func NewDirScanHandler(service *dirscan.Service, instanceStore *models.InstanceStore) *DirScanHandler {
	return &DirScanHandler{
		service:       service,
		instanceStore: instanceStore,
	}
}

// DirScanSettingsPayload is the request body for updating settings.
type DirScanSettingsPayload struct {
	Enabled                      *bool    `json:"enabled"`
	MatchMode                    *string  `json:"matchMode"`
	SizeTolerancePercent         *float64 `json:"sizeTolerancePercent"`
	MinPieceRatio                *float64 `json:"minPieceRatio"`
	MaxSearcheesPerRun           *int     `json:"maxSearcheesPerRun"`
	AllowPartial                 *bool    `json:"allowPartial"`
	SkipPieceBoundarySafetyCheck *bool    `json:"skipPieceBoundarySafetyCheck"`
	StartPaused                  *bool    `json:"startPaused"`
	Category                     *string  `json:"category"`
	Tags                         []string `json:"tags"`
}

// GetSettings returns the global directory scanner settings.
func (h *DirScanHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.GetSettings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to get settings")
		RespondError(w, http.StatusInternalServerError, "Failed to get settings")
		return
	}

	// Return default settings if none exist yet
	if settings == nil {
		settings = &models.DirScanSettings{
			MatchMode:                    models.MatchModeStrict,
			SizeTolerancePercent:         5.0,
			MinPieceRatio:                98.0,
			MaxSearcheesPerRun:           0,
			AllowPartial:                 false,
			SkipPieceBoundarySafetyCheck: true,
			StartPaused:                  true,
			Tags:                         []string{},
		}
	}

	RespondJSON(w, http.StatusOK, settings)
}

// UpdateSettings updates the global directory scanner settings.
func (h *DirScanHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var payload DirScanSettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get current settings
	settings, err := h.service.GetSettings(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to get settings for update")
		RespondError(w, http.StatusInternalServerError, "Failed to get settings")
		return
	}

	if settings == nil {
		settings = &models.DirScanSettings{}
	}

	// Apply updates
	if payload.Enabled != nil {
		settings.Enabled = *payload.Enabled
	}
	if payload.MatchMode != nil {
		mm := models.MatchMode(*payload.MatchMode)
		switch mm {
		case models.MatchModeStrict, models.MatchModeFlexible:
			settings.MatchMode = mm
		default:
			RespondError(w, http.StatusBadRequest, "Invalid matchMode")
			return
		}
	}
	if payload.SizeTolerancePercent != nil {
		settings.SizeTolerancePercent = *payload.SizeTolerancePercent
	}
	if payload.MinPieceRatio != nil {
		settings.MinPieceRatio = *payload.MinPieceRatio
	}
	if payload.MaxSearcheesPerRun != nil {
		if *payload.MaxSearcheesPerRun < 0 {
			RespondError(w, http.StatusBadRequest, "maxSearcheesPerRun must be >= 0")
			return
		}
		settings.MaxSearcheesPerRun = *payload.MaxSearcheesPerRun
	}
	if payload.AllowPartial != nil {
		settings.AllowPartial = *payload.AllowPartial
	}
	if payload.SkipPieceBoundarySafetyCheck != nil {
		settings.SkipPieceBoundarySafetyCheck = *payload.SkipPieceBoundarySafetyCheck
	}
	if payload.StartPaused != nil {
		settings.StartPaused = *payload.StartPaused
	}
	if payload.Category != nil {
		settings.Category = *payload.Category
	}
	if payload.Tags != nil {
		settings.Tags = payload.Tags
	}

	updated, err := h.service.UpdateSettings(r.Context(), settings)
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to update settings")
		RespondError(w, http.StatusInternalServerError, "Failed to update settings")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// DirScanDirectoryPayload is the request body for creating/updating directories.
type DirScanDirectoryPayload struct {
	Path                *string   `json:"path"`
	QbitPathPrefix      *string   `json:"qbitPathPrefix"`
	Category            *string   `json:"category"`
	Tags                *[]string `json:"tags"`
	Enabled             *bool     `json:"enabled"`
	ArrInstanceID       *int      `json:"arrInstanceId"`
	TargetInstanceID    *int      `json:"targetInstanceId"`
	ScanIntervalMinutes *int      `json:"scanIntervalMinutes"`
}

// ListDirectories returns all configured scan directories.
func (h *DirScanHandler) ListDirectories(w http.ResponseWriter, r *http.Request) {
	dirs, err := h.service.ListDirectories(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to list directories")
		RespondError(w, http.StatusInternalServerError, "Failed to list directories")
		return
	}

	// Return empty array instead of null when no directories exist
	if dirs == nil {
		dirs = []*models.DirScanDirectory{}
	}

	RespondJSON(w, http.StatusOK, dirs)
}

// CreateDirectory creates a new scan directory.
func (h *DirScanHandler) CreateDirectory(w http.ResponseWriter, r *http.Request) {
	var payload DirScanDirectoryPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	dir, ok := h.directoryFromCreatePayload(w, r, &payload)
	if !ok {
		return
	}

	created, err := h.service.CreateDirectory(r.Context(), dir)
	if err != nil {
		log.Error().Err(err).Msg("dirscan: failed to create directory")
		RespondError(w, http.StatusInternalServerError, "Failed to create directory")
		return
	}

	RespondJSON(w, http.StatusCreated, created)
}

func (h *DirScanHandler) directoryFromCreatePayload(w http.ResponseWriter, r *http.Request, payload *DirScanDirectoryPayload) (*models.DirScanDirectory, bool) {
	if payload.Path == nil || *payload.Path == "" {
		RespondError(w, http.StatusBadRequest, "Path is required")
		return nil, false
	}
	if payload.TargetInstanceID == nil || *payload.TargetInstanceID <= 0 {
		RespondError(w, http.StatusBadRequest, "Target instance ID is required")
		return nil, false
	}
	if payload.ScanIntervalMinutes != nil && *payload.ScanIntervalMinutes < 60 {
		RespondError(w, http.StatusBadRequest, "Scan interval must be at least 60 minutes")
		return nil, false
	}

	if validateErr := h.validateTargetInstance(w, r, *payload.TargetInstanceID); validateErr != nil {
		return nil, false
	}

	dir := &models.DirScanDirectory{
		Path:             *payload.Path,
		TargetInstanceID: *payload.TargetInstanceID,
		Enabled:          true,
	}
	if payload.QbitPathPrefix != nil {
		dir.QbitPathPrefix = *payload.QbitPathPrefix
	}
	if payload.Category != nil {
		dir.Category = *payload.Category
	}
	if payload.Tags != nil {
		dir.Tags = *payload.Tags
	}
	if payload.Enabled != nil {
		dir.Enabled = *payload.Enabled
	}
	if payload.ArrInstanceID != nil {
		dir.ArrInstanceID = payload.ArrInstanceID
	}
	if payload.ScanIntervalMinutes != nil {
		dir.ScanIntervalMinutes = *payload.ScanIntervalMinutes
	} else {
		dir.ScanIntervalMinutes = 1440
	}

	return dir, true
}

// GetDirectory returns a specific scan directory.
func (h *DirScanHandler) GetDirectory(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	dir, err := h.service.GetDirectory(r.Context(), dirID)
	if err != nil {
		if errors.Is(err, models.ErrDirectoryNotFound) {
			RespondError(w, http.StatusNotFound, "Directory not found")
			return
		}
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to get directory")
		RespondError(w, http.StatusInternalServerError, "Failed to get directory")
		return
	}

	RespondJSON(w, http.StatusOK, dir)
}

// UpdateDirectory updates a scan directory.
func (h *DirScanHandler) UpdateDirectory(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	var payload DirScanDirectoryPayload
	if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if payload.ScanIntervalMinutes != nil && *payload.ScanIntervalMinutes < 60 {
		RespondError(w, http.StatusBadRequest, "Scan interval must be at least 60 minutes")
		return
	}

	// Validate target instance if being changed
	if payload.TargetInstanceID != nil {
		if validateErr := h.validateTargetInstance(w, r, *payload.TargetInstanceID); validateErr != nil {
			return
		}
	}

	params := &models.DirScanDirectoryUpdateParams{
		Path:                payload.Path,
		QbitPathPrefix:      payload.QbitPathPrefix,
		Category:            payload.Category,
		Tags:                payload.Tags,
		Enabled:             payload.Enabled,
		ArrInstanceID:       payload.ArrInstanceID,
		TargetInstanceID:    payload.TargetInstanceID,
		ScanIntervalMinutes: payload.ScanIntervalMinutes,
	}

	updated, err := h.service.UpdateDirectory(r.Context(), dirID, params)
	if err != nil {
		if errors.Is(err, models.ErrDirectoryNotFound) {
			RespondError(w, http.StatusNotFound, "Directory not found")
			return
		}
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to update directory")
		RespondError(w, http.StatusInternalServerError, "Failed to update directory")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

// DeleteDirectory deletes a scan directory.
func (h *DirScanHandler) DeleteDirectory(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if err := h.service.DeleteDirectory(r.Context(), dirID); err != nil {
		if errors.Is(err, models.ErrDirectoryNotFound) {
			RespondError(w, http.StatusNotFound, "Directory not found")
			return
		}
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to delete directory")
		RespondError(w, http.StatusInternalServerError, "Failed to delete directory")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TriggerScan starts a manual scan for a directory.
func (h *DirScanHandler) TriggerScan(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	runID, err := h.service.StartManualScan(r.Context(), dirID)
	if err != nil {
		if errors.Is(err, models.ErrDirScanRunAlreadyActive) {
			RespondError(w, http.StatusConflict, "A scan is already in progress")
			return
		}
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to start scan")
		RespondError(w, http.StatusInternalServerError, "Failed to start scan")
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]int64{"runId": runID})
}

// CancelScan cancels a running scan for a directory.
func (h *DirScanHandler) CancelScan(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	if err := h.service.CancelScan(r.Context(), dirID); err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to cancel scan")
		RespondError(w, http.StatusInternalServerError, "Failed to cancel scan")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ResetFiles deletes tracked scan progress for a directory.
func (h *DirScanHandler) ResetFiles(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	run, err := h.service.GetActiveRun(r.Context(), dirID)
	if err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to check active run before reset")
		RespondError(w, http.StatusInternalServerError, "Failed to reset scan progress")
		return
	}
	if run != nil {
		RespondError(w, http.StatusConflict, "Cannot reset scan progress while a scan is running")
		return
	}

	if err := h.service.ResetFilesForDirectory(r.Context(), dirID); err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to reset tracked files")
		RespondError(w, http.StatusInternalServerError, "Failed to reset scan progress")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetStatus returns the status of the current or most recent scan.
func (h *DirScanHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	run, err := h.service.GetStatus(r.Context(), dirID)
	if err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to get status")
		RespondError(w, http.StatusInternalServerError, "Failed to get status")
		return
	}

	if run == nil {
		RespondJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}

	RespondJSON(w, http.StatusOK, run)
}

// ListRuns returns recent scan runs for a directory.
func (h *DirScanHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, parseErr := strconv.Atoi(limitStr); parseErr == nil && l > 0 {
			limit = l
		}
	}

	runs, err := h.service.ListRuns(r.Context(), dirID, limit)
	if err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to list runs")
		RespondError(w, http.StatusInternalServerError, "Failed to list runs")
		return
	}

	if runs == nil {
		runs = []*models.DirScanRun{}
	}

	RespondJSON(w, http.StatusOK, runs)
}

// ListRunInjections returns injection attempts (added/failed) for a run.
func (h *DirScanHandler) ListRunInjections(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	runID, err := parseRunID(w, r)
	if err != nil {
		return
	}

	limit := 50
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, parseErr := strconv.Atoi(limitStr); parseErr == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, parseErr := strconv.Atoi(offsetStr); parseErr == nil && o >= 0 {
			offset = o
		}
	}

	injections, err := h.service.ListRunInjections(r.Context(), dirID, runID, limit, offset)
	if err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Int64("runID", runID).Msg("dirscan: failed to list run injections")
		RespondError(w, http.StatusInternalServerError, "Failed to list run injections")
		return
	}
	if injections == nil {
		injections = []*models.DirScanRunInjection{}
	}

	RespondJSON(w, http.StatusOK, injections)
}

// ListFiles returns scanned files for a directory.
func (h *DirScanHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	dirID, err := parseDirectoryID(w, r)
	if err != nil {
		return
	}

	if !h.requireDirectory(w, r, dirID) {
		return
	}

	limit := 100
	offset := 0
	var status *models.DirScanFileStatus

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, parseErr := strconv.Atoi(limitStr); parseErr == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, parseErr := strconv.Atoi(offsetStr); parseErr == nil && o >= 0 {
			offset = o
		}
	}
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		s := models.DirScanFileStatus(statusStr)
		switch s {
		case models.DirScanFileStatusPending,
			models.DirScanFileStatusMatched,
			models.DirScanFileStatusNoMatch,
			models.DirScanFileStatusError,
			models.DirScanFileStatusAlreadySeeding,
			models.DirScanFileStatusInQBittorrent:
			status = &s
		default:
			RespondError(w, http.StatusBadRequest, "Invalid status")
			return
		}
	}

	files, err := h.service.ListFiles(r.Context(), dirID, status, limit, offset)
	if err != nil {
		log.Error().Err(err).Int("directoryID", dirID).Msg("dirscan: failed to list files")
		RespondError(w, http.StatusInternalServerError, "Failed to list files")
		return
	}

	if files == nil {
		files = []*models.DirScanFile{}
	}

	RespondJSON(w, http.StatusOK, files)
}

// parseDirectoryID extracts and validates the directory ID from the URL.
func parseDirectoryID(w http.ResponseWriter, r *http.Request) (int, error) {
	idStr := chi.URLParam(r, "directoryID")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid directory ID")
		return 0, errors.New("invalid directory ID")
	}
	return id, nil
}

func parseRunID(w http.ResponseWriter, r *http.Request) (int64, error) {
	idStr := chi.URLParam(r, "runID")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return 0, errors.New("invalid run ID")
	}
	return id, nil
}

// validateTargetInstance validates that the target instance exists and has local filesystem access.
// Returns an error if validation fails (response is already written in that case).
func (h *DirScanHandler) validateTargetInstance(w http.ResponseWriter, r *http.Request, instanceID int) error {
	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusBadRequest, "Target instance not found")
			return fmt.Errorf("instance not found: %w", err)
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("dirscan: failed to get instance")
		RespondError(w, http.StatusInternalServerError, "Failed to validate target instance")
		return fmt.Errorf("get instance: %w", err)
	}

	if !instance.HasLocalFilesystemAccess {
		RespondError(w, http.StatusBadRequest, "Target instance must have local filesystem access enabled")
		return errors.New("instance lacks local filesystem access")
	}

	return nil
}

func (h *DirScanHandler) requireDirectory(w http.ResponseWriter, r *http.Request, directoryID int) bool {
	_, err := h.service.GetDirectory(r.Context(), directoryID)
	switch {
	case err == nil:
		return true
	case errors.Is(err, models.ErrDirectoryNotFound):
		RespondError(w, http.StatusNotFound, "Directory not found")
	default:
		log.Error().Err(err).Int("directoryID", directoryID).Msg("dirscan: failed to validate directory")
		RespondError(w, http.StatusInternalServerError, "Failed to validate directory")
	}
	return false
}
