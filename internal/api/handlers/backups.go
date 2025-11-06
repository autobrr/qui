// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/torrentname"
)

type BackupsHandler struct {
	service *backups.Service
}

func NewBackupsHandler(service *backups.Service) *BackupsHandler {
	return &BackupsHandler{service: service}
}

type backupSettingsRequest struct {
	Enabled           bool `json:"enabled"`
	HourlyEnabled     bool `json:"hourlyEnabled"`
	DailyEnabled      bool `json:"dailyEnabled"`
	WeeklyEnabled     bool `json:"weeklyEnabled"`
	MonthlyEnabled    bool `json:"monthlyEnabled"`
	KeepHourly        int  `json:"keepHourly"`
	KeepDaily         int  `json:"keepDaily"`
	KeepWeekly        int  `json:"keepWeekly"`
	KeepMonthly       int  `json:"keepMonthly"`
	IncludeCategories bool `json:"includeCategories"`
	IncludeTags       bool `json:"includeTags"`
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
		KeepHourly:        req.KeepHourly,
		KeepDaily:         req.KeepDaily,
		KeepWeekly:        req.KeepWeekly,
		KeepMonthly:       req.KeepMonthly,
		IncludeCategories: req.IncludeCategories,
		IncludeTags:       req.IncludeTags,
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

type restoreRequest struct {
	Mode               string   `json:"mode"`
	DryRun             bool     `json:"dryRun"`
	ExcludeHashes      []string `json:"excludeHashes"`
	StartPaused        *bool    `json:"startPaused"`
	SkipHashCheck      *bool    `json:"skipHashCheck"`
	AutoResumeVerified *bool    `json:"autoResumeVerified"`
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

type backupRunWithProgress struct {
	*models.BackupRun
	ProgressCurrent    int     `json:"progressCurrent"`
	ProgressTotal      int     `json:"progressTotal"`
	ProgressPercentage float64 `json:"progressPercentage"`
}

type backupRunsResponse struct {
	Runs    []*backupRunWithProgress `json:"runs"`
	HasMore bool                     `json:"hasMore"`
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

	requestedLimit := limit
	effectiveLimit := requestedLimit + 1

	runs, err := h.service.ListRuns(r.Context(), instanceID, effectiveLimit, offset)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to list backup runs")
		return
	}

	hasMore := len(runs) > requestedLimit
	if hasMore {
		runs = runs[:requestedLimit]
	}

	// Merge progress data for running backups
	runsWithProgress := make([]*backupRunWithProgress, len(runs))
	for i, run := range runs {
		runWithProgress := &backupRunWithProgress{BackupRun: run}
		if run.Status == models.BackupRunStatusRunning {
			if progress := h.service.GetProgress(run.ID); progress != nil {
				runWithProgress.ProgressCurrent = progress.Current
				runWithProgress.ProgressTotal = progress.Total
				runWithProgress.ProgressPercentage = progress.Percentage
			}
		}
		runsWithProgress[i] = runWithProgress
	}

	response := &backupRunsResponse{
		Runs:    runsWithProgress,
		HasMore: hasMore,
	}

	RespondJSON(w, http.StatusOK, response)
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

func (h *BackupsHandler) DownloadTorrentBlob(w http.ResponseWriter, r *http.Request) {
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

	torrentHash := strings.TrimSpace(chi.URLParam(r, "torrentHash"))
	if torrentHash == "" {
		RespondError(w, http.StatusBadRequest, "Invalid torrent hash")
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

	item, err := h.service.GetItem(r.Context(), runID, torrentHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Torrent not found in backup")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to load backup item")
		return
	}

	if item.TorrentBlobPath == nil || strings.TrimSpace(*item.TorrentBlobPath) == "" {
		RespondError(w, http.StatusNotFound, "Cached torrent unavailable")
		return
	}

	dataDir := strings.TrimSpace(h.service.DataDir())
	if dataDir == "" {
		RespondError(w, http.StatusInternalServerError, "Backup data directory unavailable")
		return
	}

	rel := filepath.Clean(*item.TorrentBlobPath)
	absTarget, err := filepath.Abs(filepath.Join(dataDir, rel))
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to resolve torrent path")
		return
	}

	baseDir, err := filepath.Abs(dataDir)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to resolve data directory")
		return
	}

	relCheck, err := filepath.Rel(baseDir, absTarget)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		RespondError(w, http.StatusNotFound, "Cached torrent unavailable")
		return
	}

	file, err := os.Open(absTarget)
	if err != nil {
		if os.IsNotExist(err) {
			altRel := filepath.ToSlash(filepath.Join("backups", rel))
			altAbs := filepath.Join(dataDir, altRel)
			if altFile, altErr := os.Open(altAbs); altErr == nil {
				file = altFile
				defer file.Close()
				goto serve
			}
			RespondError(w, http.StatusNotFound, "Cached torrent file missing")
			return
		}
		RespondError(w, http.StatusInternalServerError, "Failed to open torrent file")
		return
	}
	defer file.Close()

serve:

	info, err := file.Stat()
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to inspect torrent file")
		return
	}

	filename := ""
	if item.ArchiveRelPath != nil && strings.TrimSpace(*item.ArchiveRelPath) != "" {
		filename = filepath.Base(filepath.ToSlash(*item.ArchiveRelPath))
	}
	if filename == "" {
		filename = torrentname.SanitizeExportFilename(item.Name, item.TorrentHash, "", item.TorrentHash)
	}

	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	http.ServeContent(w, r, filename, info.ModTime(), file)
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

func (h *BackupsHandler) DeleteAllRuns(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	if err := h.service.DeleteAllRuns(r.Context(), instanceID); err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to delete backups")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *BackupsHandler) PreviewRestore(w http.ResponseWriter, r *http.Request) {
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

	if err := h.ensureRunOwnership(r.Context(), instanceID, runID); err != nil {
		h.respondRunError(w, err)
		return
	}

	var req restoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	mode, err := backups.ParseRestoreMode(req.Mode)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	var planOpts *backups.RestorePlanOptions
	if len(req.ExcludeHashes) > 0 {
		planOpts = &backups.RestorePlanOptions{ExcludeHashes: req.ExcludeHashes}
	}

	plan, err := h.service.PlanRestoreDiff(r.Context(), runID, mode, planOpts)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to build restore plan")
		return
	}

	RespondJSON(w, http.StatusOK, plan)
}

func (h *BackupsHandler) ExecuteRestore(w http.ResponseWriter, r *http.Request) {
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

	if err := h.ensureRunOwnership(r.Context(), instanceID, runID); err != nil {
		h.respondRunError(w, err)
		return
	}

	var req restoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	mode, err := backups.ParseRestoreMode(req.Mode)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	startPaused := true
	if req.StartPaused != nil {
		startPaused = *req.StartPaused
	}
	skipHashCheck := false
	if req.SkipHashCheck != nil {
		skipHashCheck = *req.SkipHashCheck
	}

	autoResume := true
	if req.AutoResumeVerified != nil {
		autoResume = *req.AutoResumeVerified
	}

	result, err := h.service.ExecuteRestore(r.Context(), runID, mode, backups.RestoreOptions{
		DryRun:             req.DryRun,
		StartPaused:        startPaused,
		SkipHashCheck:      skipHashCheck,
		AutoResumeVerified: autoResume,
		ExcludeHashes:      req.ExcludeHashes,
	})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to execute restore")
		return
	}

	RespondJSON(w, http.StatusOK, result)
}

func (h *BackupsHandler) ensureRunOwnership(ctx context.Context, instanceID int, runID int64) error {
	run, err := h.service.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.InstanceID != instanceID {
		return sql.ErrNoRows
	}
	return nil
}

func (h *BackupsHandler) respondRunError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		RespondError(w, http.StatusNotFound, "Backup run not found")
		return
	}
	RespondError(w, http.StatusInternalServerError, "Failed to load backup run")
}
