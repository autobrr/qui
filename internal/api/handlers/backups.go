// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/models"
)

type BackupsHandler struct {
	service *backups.Service
}

func NewBackupsHandler(service *backups.Service) *BackupsHandler {
	return &BackupsHandler{service: service}
}

type backupSettingsRequest struct {
	Enabled           bool    `json:"enabled"`
	HourlyEnabled     bool    `json:"hourlyEnabled"`
	DailyEnabled      bool    `json:"dailyEnabled"`
	WeeklyEnabled     bool    `json:"weeklyEnabled"`
	MonthlyEnabled    bool    `json:"monthlyEnabled"`
	KeepLast          int     `json:"keepLast"`
	KeepHourly        int     `json:"keepHourly"`
	KeepDaily         int     `json:"keepDaily"`
	KeepWeekly        int     `json:"keepWeekly"`
	KeepMonthly       int     `json:"keepMonthly"`
	IncludeCategories bool    `json:"includeCategories"`
	IncludeTags       bool    `json:"includeTags"`
	CustomPath        *string `json:"customPath"`
}

func (h *BackupsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	settings, err := h.service.GetSettings(r.Context(), instanceID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to load backup settings")
		return
	}

	RespondJSON(w, http.StatusOK, settings)
}

func (h *BackupsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req backupSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	settings := &models.BackupSettings{
		InstanceID:        instanceID,
		Enabled:           req.Enabled,
		HourlyEnabled:     req.HourlyEnabled,
		DailyEnabled:      req.DailyEnabled,
		WeeklyEnabled:     req.WeeklyEnabled,
		MonthlyEnabled:    req.MonthlyEnabled,
		KeepLast:          req.KeepLast,
		KeepHourly:        req.KeepHourly,
		KeepDaily:         req.KeepDaily,
		KeepWeekly:        req.KeepWeekly,
		KeepMonthly:       req.KeepMonthly,
		IncludeCategories: req.IncludeCategories,
		IncludeTags:       req.IncludeTags,
		CustomPath:        req.CustomPath,
	}

	if err := h.service.UpdateSettings(r.Context(), settings); err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to update backup settings")
		return
	}

	updated, err := h.service.GetSettings(r.Context(), instanceID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to load backup settings")
		return
	}

	RespondJSON(w, http.StatusOK, updated)
}

type triggerBackupRequest struct {
	Kind        string `json:"kind"`
	RequestedBy string `json:"requestedBy"`
}

func (h *BackupsHandler) TriggerBackup(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req triggerBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	kind := models.BackupRunKindManual
	if req.Kind != "" {
		switch req.Kind {
		case string(models.BackupRunKindManual):
			kind = models.BackupRunKindManual
		case string(models.BackupRunKindHourly):
			kind = models.BackupRunKindHourly
		case string(models.BackupRunKindDaily):
			kind = models.BackupRunKindDaily
		case string(models.BackupRunKindWeekly):
			kind = models.BackupRunKindWeekly
		case string(models.BackupRunKindMonthly):
			kind = models.BackupRunKindMonthly
		default:
			RespondError(w, http.StatusBadRequest, "Unsupported backup kind")
			return
		}
	}

	requestedBy := strings.TrimSpace(req.RequestedBy)
	if requestedBy == "" {
		requestedBy = "api"
	}

	run, err := h.service.QueueRun(r.Context(), instanceID, kind, requestedBy)
	if err != nil {
		if errors.Is(err, backups.ErrInstanceBusy) {
			RespondError(w, http.StatusConflict, "Backup already running for this instance")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to queue backup run")
		return
	}

	RespondJSON(w, http.StatusAccepted, run)
}

func (h *BackupsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	limit := 25
	offset := 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	runs, err := h.service.ListRuns(r.Context(), instanceID, limit, offset)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to list backup runs")
		return
	}

	RespondJSON(w, http.StatusOK, runs)
}

func (h *BackupsHandler) GetManifest(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	runID, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}

	run, err := h.service.GetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Backup run not found")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to load backup run")
		return
	}

	if run.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Backup run not found")
		return
	}

	manifest, err := h.service.LoadManifest(r.Context(), runID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to load manifest")
		return
	}

	RespondJSON(w, http.StatusOK, manifest)
}

func (h *BackupsHandler) DownloadRun(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	runID, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}

	run, err := h.service.GetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Backup run not found")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to load backup run")
		return
	}

	if run.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Backup run not found")
		return
	}

	if run.ArchivePath == nil || *run.ArchivePath == "" {
		RespondError(w, http.StatusNotFound, "Backup archive not available")
		return
	}

	absolutePath := filepath.Join(h.service.DataDir(), *run.ArchivePath)
	file, err := os.Open(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			RespondError(w, http.StatusNotFound, "Backup archive missing")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to open backup archive")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(absolutePath)+"\"")
	http.ServeContent(w, r, filepath.Base(absolutePath), run.RequestedAt, file)
}

func (h *BackupsHandler) DeleteRun(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	runID, err := strconv.ParseInt(chi.URLParam(r, "runID"), 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}

	run, err := h.service.GetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Backup run not found")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to load backup run")
		return
	}

	if run.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Backup run not found")
		return
	}

	if err := h.service.DeleteRun(r.Context(), runID); err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to delete backup run")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
