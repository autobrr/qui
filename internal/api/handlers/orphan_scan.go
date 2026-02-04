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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/orphanscan"
)

const (
	previewSortSizeDesc          = "size_desc"
	previewSortDirectorySizeDesc = "directory_size_desc"
)

type OrphanScanHandler struct {
	store         *models.OrphanScanStore
	instanceStore *models.InstanceStore
	service       *orphanscan.Service
}

func NewOrphanScanHandler(store *models.OrphanScanStore, instanceStore *models.InstanceStore, service *orphanscan.Service) *OrphanScanHandler {
	return &OrphanScanHandler{
		store:         store,
		instanceStore: instanceStore,
		service:       service,
	}
}

func (h *OrphanScanHandler) requireLocalAccess(w http.ResponseWriter, r *http.Request, instanceID int) bool {
	instance, err := h.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Instance not found")
			return false
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to get instance")
		RespondError(w, http.StatusInternalServerError, "Failed to get instance")
		return false
	}

	if !instance.HasLocalFilesystemAccess {
		RespondError(w, http.StatusForbidden, "Orphan scanning requires local filesystem access. Enable 'Local Filesystem Access' in instance settings first.")
		return false
	}

	return true
}

// OrphanScanSettingsPayload is the request body for creating/updating orphan scan settings.
type OrphanScanSettingsPayload struct {
	Enabled             *bool    `json:"enabled"`
	GracePeriodMinutes  *int     `json:"gracePeriodMinutes"`
	IgnorePaths         []string `json:"ignorePaths"`
	ScanIntervalHours   *int     `json:"scanIntervalHours"`
	PreviewSort         *string  `json:"previewSort"`
	MaxFilesPerRun      *int     `json:"maxFilesPerRun"`
	AutoCleanupEnabled  *bool    `json:"autoCleanupEnabled"`
	AutoCleanupMaxFiles *int     `json:"autoCleanupMaxFiles"`
}

// GetSettings returns the orphan scan settings for an instance.
func (h *OrphanScanHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	settings, err := h.store.GetSettings(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to get settings")
		RespondError(w, http.StatusInternalServerError, "Failed to get settings")
		return
	}

	// Return default settings if none exist
	if settings == nil {
		defaults := orphanscan.DefaultSettings()
		settings = &models.OrphanScanSettings{
			InstanceID:          instanceID,
			Enabled:             defaults.Enabled,
			GracePeriodMinutes:  defaults.GracePeriodMinutes,
			IgnorePaths:         defaults.IgnorePaths,
			ScanIntervalHours:   defaults.ScanIntervalHours,
			PreviewSort:         defaults.PreviewSort,
			MaxFilesPerRun:      defaults.MaxFilesPerRun,
			AutoCleanupEnabled:  defaults.AutoCleanupEnabled,
			AutoCleanupMaxFiles: defaults.AutoCleanupMaxFiles,
		}
	}

	RespondJSON(w, http.StatusOK, settings)
}

// UpdateSettings updates the orphan scan settings for an instance.
func (h *OrphanScanHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	var payload OrphanScanSettingsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to decode settings payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Get existing settings or create defaults
	settings, err := h.store.GetSettings(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to get existing settings")
		RespondError(w, http.StatusInternalServerError, "Failed to get settings")
		return
	}

	if settings == nil {
		defaults := orphanscan.DefaultSettings()
		settings = &models.OrphanScanSettings{
			InstanceID:          instanceID,
			Enabled:             defaults.Enabled,
			GracePeriodMinutes:  defaults.GracePeriodMinutes,
			IgnorePaths:         defaults.IgnorePaths,
			ScanIntervalHours:   defaults.ScanIntervalHours,
			PreviewSort:         defaults.PreviewSort,
			MaxFilesPerRun:      defaults.MaxFilesPerRun,
			AutoCleanupEnabled:  defaults.AutoCleanupEnabled,
			AutoCleanupMaxFiles: defaults.AutoCleanupMaxFiles,
		}
	}

	// Apply updates
	if payload.Enabled != nil {
		settings.Enabled = *payload.Enabled
	}
	if payload.GracePeriodMinutes != nil {
		if *payload.GracePeriodMinutes < 0 {
			RespondError(w, http.StatusBadRequest, "Grace period must be non-negative")
			return
		}
		settings.GracePeriodMinutes = *payload.GracePeriodMinutes
	}
	if payload.IgnorePaths != nil {
		settings.IgnorePaths = payload.IgnorePaths
	}
	if payload.ScanIntervalHours != nil {
		if *payload.ScanIntervalHours < 1 {
			RespondError(w, http.StatusBadRequest, "Scan interval must be at least 1 hour")
			return
		}
		settings.ScanIntervalHours = *payload.ScanIntervalHours
	}
	if payload.PreviewSort != nil {
		// Empty is treated as default.
		if *payload.PreviewSort != "" && *payload.PreviewSort != previewSortSizeDesc && *payload.PreviewSort != previewSortDirectorySizeDesc {
			RespondError(w, http.StatusBadRequest, "Invalid preview sort: must be 'size_desc' or 'directory_size_desc'")
			return
		}
		if *payload.PreviewSort == "" {
			settings.PreviewSort = previewSortSizeDesc
		} else {
			settings.PreviewSort = *payload.PreviewSort
		}
	}
	if payload.MaxFilesPerRun != nil {
		if *payload.MaxFilesPerRun < 1 {
			RespondError(w, http.StatusBadRequest, "Max files per run must be at least 1")
			return
		}
		settings.MaxFilesPerRun = *payload.MaxFilesPerRun
	}
	if payload.AutoCleanupEnabled != nil {
		settings.AutoCleanupEnabled = *payload.AutoCleanupEnabled
	}
	if payload.AutoCleanupMaxFiles != nil {
		if *payload.AutoCleanupMaxFiles < 1 {
			RespondError(w, http.StatusBadRequest, "Auto-cleanup max files threshold must be at least 1")
			return
		}
		settings.AutoCleanupMaxFiles = *payload.AutoCleanupMaxFiles
	}

	// Validate and normalize ignore paths
	if len(settings.IgnorePaths) > 0 {
		normalized, err := orphanscan.NormalizeIgnorePaths(settings.IgnorePaths)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		settings.IgnorePaths = normalized
	}

	savedSettings, err := h.store.UpsertSettings(r.Context(), settings)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to save settings")
		RespondError(w, http.StatusInternalServerError, "Failed to save settings")
		return
	}

	RespondJSON(w, http.StatusOK, savedSettings)
}

// TriggerScan starts a manual orphan scan for an instance.
func (h *OrphanScanHandler) TriggerScan(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Orphan scan service not available")
		return
	}

	runID, err := h.service.TriggerScan(r.Context(), instanceID, "manual")
	if err != nil {
		if errors.Is(err, orphanscan.ErrScanInProgress) {
			// Provide actionable context for clients even if the runs list isn't showing the active run.
			if active, aErr := h.store.GetMostRecentActiveRun(r.Context(), instanceID); aErr == nil && active != nil {
				RespondError(w, http.StatusConflict,
					fmt.Sprintf(
						"A scan is already in progress for this instance (runId=%d status=%s startedAt=%s)",
						active.ID,
						active.Status,
						active.StartedAt.UTC().Format(time.RFC3339),
					),
				)
				return
			}

			RespondError(w, http.StatusConflict, "A scan is already in progress for this instance")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to trigger scan")
		RespondError(w, http.StatusInternalServerError, "Failed to start scan")
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]int64{"runId": runID})
}

// ListRuns returns recent orphan scan runs for an instance.
func (h *OrphanScanHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	runs, err := h.store.ListRuns(r.Context(), instanceID, limit)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("orphanscan: failed to list runs")
		RespondError(w, http.StatusInternalServerError, "Failed to list runs")
		return
	}

	if runs == nil {
		runs = []*models.OrphanScanRun{}
	}

	RespondJSON(w, http.StatusOK, runs)
}

// GetRun returns a specific orphan scan run with its file list.
func (h *OrphanScanHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	runIDStr := chi.URLParam(r, "runID")
	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil || runID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}

	run, err := h.store.GetRun(r.Context(), runID)
	if err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("orphanscan: failed to get run")
		RespondError(w, http.StatusInternalServerError, "Failed to get run")
		return
	}
	if run == nil {
		RespondError(w, http.StatusNotFound, "Run not found")
		return
	}

	// Verify run belongs to this instance
	if run.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Run not found")
		return
	}

	// Parse pagination
	limit := 100
	offset := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	previewSort := "size_desc"
	if settings, sErr := h.store.GetSettings(r.Context(), instanceID); sErr == nil && settings != nil && settings.PreviewSort != "" {
		previewSort = settings.PreviewSort
	}

	files, err := h.store.ListFiles(r.Context(), runID, limit, offset, previewSort)
	if err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("orphanscan: failed to list files")
		RespondError(w, http.StatusInternalServerError, "Failed to get files")
		return
	}

	if files == nil {
		files = []*models.OrphanScanFile{}
	}

	type RunWithFiles struct {
		*models.OrphanScanRun
		Files []*models.OrphanScanFile `json:"files"`
	}

	RespondJSON(w, http.StatusOK, RunWithFiles{
		OrphanScanRun: run,
		Files:         files,
	})
}

// ConfirmDeletion confirms deletion of orphan files from a preview_ready run.
func (h *OrphanScanHandler) ConfirmDeletion(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	runIDStr := chi.URLParam(r, "runID")
	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil || runID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}

	// Verify run exists and belongs to this instance
	run, err := h.store.GetRun(r.Context(), runID)
	if err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("orphanscan: failed to get run for confirmation")
		RespondError(w, http.StatusInternalServerError, "Failed to get run")
		return
	}
	if run == nil {
		RespondError(w, http.StatusNotFound, "Run not found")
		return
	}

	if run.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Run not found")
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Orphan scan service not available")
		return
	}

	if err := h.service.ConfirmDeletion(r.Context(), instanceID, runID); err != nil {
		if errors.Is(err, orphanscan.ErrScanInProgress) {
			RespondError(w, http.StatusConflict, "A scan or deletion is already in progress for this instance")
			return
		}
		if errors.Is(err, orphanscan.ErrRunNotFound) {
			RespondError(w, http.StatusNotFound, "Run not found")
			return
		}
		if errors.Is(err, orphanscan.ErrInvalidRunStatus) {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Error().Err(err).Int64("runID", runID).Msg("orphanscan: failed to confirm deletion")
		RespondError(w, http.StatusInternalServerError, "Failed to start deletion")
		return
	}

	RespondJSON(w, http.StatusAccepted, map[string]string{"status": "deleting"})
}

// CancelRun cancels a pending, scanning, or preview_ready run.
func (h *OrphanScanHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
	instanceID, err := parseInstanceID(w, r)
	if err != nil {
		return
	}

	if !h.requireLocalAccess(w, r, instanceID) {
		return
	}

	runIDStr := chi.URLParam(r, "runID")
	runID, err := strconv.ParseInt(runIDStr, 10, 64)
	if err != nil || runID <= 0 {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}

	// Verify run exists and belongs to this instance
	run, err := h.store.GetRun(r.Context(), runID)
	if err != nil {
		log.Error().Err(err).Int64("runID", runID).Msg("orphanscan: failed to get run for cancellation")
		RespondError(w, http.StatusInternalServerError, "Failed to get run")
		return
	}
	if run == nil {
		RespondError(w, http.StatusNotFound, "Run not found")
		return
	}

	if run.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Run not found")
		return
	}

	if h.service == nil {
		RespondError(w, http.StatusServiceUnavailable, "Orphan scan service not available")
		return
	}

	if err := h.service.CancelRun(r.Context(), runID); err != nil {
		if errors.Is(err, orphanscan.ErrRunNotFound) {
			RespondError(w, http.StatusNotFound, "Run not found")
			return
		}
		if errors.Is(err, orphanscan.ErrCannotCancelDuringDeletion) {
			RespondError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, orphanscan.ErrRunAlreadyFinished) || errors.Is(err, orphanscan.ErrInvalidRunStatus) {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Error().Err(err).Int64("runID", runID).Msg("orphanscan: failed to cancel run")
		RespondError(w, http.StatusInternalServerError, "Failed to cancel run")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}
