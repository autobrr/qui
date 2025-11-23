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
	"sync"

	"archive/tar"
	"archive/zip"

	kgzip "github.com/klauspost/compress/gzip"

	"github.com/andybalholm/brotli"
	"github.com/go-chi/chi/v5"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/torrentname"
)

var registerCompressorOnce sync.Once

func init() {
	// Note: klauspost deflate compressor registration removed due to conflicts with default registration
	// The standard library's zip package already registers a deflate compressor
}

type BackupsHandler struct {
	service *backups.Service
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

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

// DownloadRun downloads a backup archive.
// Query parameters:
//   - format: compression format (zip, tar.gz, tar.zst, tar.br, tar.xz, tar) - defaults to zip
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

	if run.Status != models.BackupRunStatusSuccess {
		RespondError(w, http.StatusNotFound, "Backup not available")
		return
	}

	// Load manifest
	manifest, err := h.service.LoadManifest(r.Context(), runID)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to load backup manifest")
		return
	}

	// Parse format parameter
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "zip"
	}
	supportedFormats := map[string]bool{
		"zip":     true,
		"tar.gz":  true,
		"tar.zst": true,
		"tar.br":  true,
		"tar.xz":  true,
		"tar":     true,
	}
	if !supportedFormats[format] {
		RespondError(w, http.StatusBadRequest, "Unsupported format. Supported: zip, tar.gz, tar.zst, tar.br, tar.lz4, tar.xz, tar")
		return
	}

	// Set headers based on format
	var contentType, extension string
	switch format {
	case "zip":
		contentType = "application/zip"
		extension = "zip"
	case "tar.gz":
		contentType = "application/gzip"
		extension = "tar.gz"
	case "tar.zst":
		contentType = "application/zstd"
		extension = "tar.zst"
	case "tar.br":
		contentType = "application/x-brotli"
		extension = "tar.br"
	case "tar.xz":
		contentType = "application/x-xz"
		extension = "tar.xz"
	case "tar":
		contentType = "application/x-tar"
		extension = "tar"
	}
	filename := fmt.Sprintf("qui-backup_instance-%d_%s_%s.%s", instanceID, strings.ToLower(string(run.Kind)), run.RequestedAt.Format("2006-01-02_15-04-05"), extension)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

	if format == "zip" {
		// Create zip writer
		zipWriter := zip.NewWriter(w)
		defer zipWriter.Close()

		// Add manifest to zip
		manifestData, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to marshal manifest")
			return
		}

		manifestHeader := &zip.FileHeader{
			Name:   "manifest.json",
			Method: zip.Deflate,
		}
		manifestHeader.Modified = run.RequestedAt
		manifestWriter, err := zipWriter.CreateHeader(manifestHeader)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to create manifest entry")
			return
		}
		if _, err := manifestWriter.Write(manifestData); err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to write manifest")
			return
		}

		// Add torrent files to zip
		for _, item := range manifest.Items {
			if item.TorrentBlob == "" {
				continue
			}

			torrentPath := filepath.Join(h.service.DataDir(), item.TorrentBlob)
			file, err := os.Open(torrentPath)
			if err != nil {
				// Skip missing files
				continue
			}

			header := &zip.FileHeader{
				Name:   item.ArchivePath,
				Method: zip.Deflate,
			}
			header.Modified = run.RequestedAt

			writer, err := zipWriter.CreateHeader(header)
			if err != nil {
				file.Close()
				RespondError(w, http.StatusInternalServerError, "Failed to create zip entry")
				return
			}

			if _, err := io.Copy(writer, file); err != nil {
				file.Close()
				RespondError(w, http.StatusInternalServerError, "Failed to write torrent to zip")
				return
			}

			file.Close()
		}

		// Close zip writer to finalize
		if err := zipWriter.Close(); err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to finalize zip")
			return
		}
	} else {
		// Handle tar-based formats
		var compressor io.WriteCloser
		var err error
		switch format {
		case "tar.gz":
			compressor, err = kgzip.NewWriterLevel(w, kgzip.DefaultCompression)
		case "tar.zst":
			compressor, err = zstd.NewWriter(w)
		case "tar.br":
			compressor = brotli.NewWriter(w)
		case "tar.xz":
			compressor, err = xz.NewWriter(w)
		case "tar":
			compressor = &nopCloser{w}
		default:
			RespondError(w, http.StatusInternalServerError, "Unsupported format")
			return
		}
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to create compressor")
			return
		}
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to create compressor")
			return
		}
		defer compressor.Close()

		tarWriter := tar.NewWriter(compressor)
		defer tarWriter.Close()

		// Add manifest to tar
		manifestData, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to marshal manifest")
			return
		}

		manifestHeader := &tar.Header{
			Name:    "manifest.json",
			Size:    int64(len(manifestData)),
			Mode:    0644,
			ModTime: run.RequestedAt,
		}
		if err := tarWriter.WriteHeader(manifestHeader); err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to write manifest header")
			return
		}
		if _, err := tarWriter.Write(manifestData); err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to write manifest")
			return
		}

		// Add torrent files to tar
		for _, item := range manifest.Items {
			if item.TorrentBlob == "" {
				continue
			}

			torrentPath := filepath.Join(h.service.DataDir(), item.TorrentBlob)
			file, err := os.Open(torrentPath)
			if err != nil {
				// Skip missing files
				continue
			}
			defer file.Close()

			stat, err := file.Stat()
			if err != nil {
				file.Close()
				continue
			}

			header := &tar.Header{
				Name:    item.ArchivePath,
				Size:    stat.Size(),
				Mode:    0644,
				ModTime: run.RequestedAt,
			}
			if err := tarWriter.WriteHeader(header); err != nil {
				file.Close()
				RespondError(w, http.StatusInternalServerError, "Failed to write tar header")
				return
			}

			if _, err := io.Copy(tarWriter, file); err != nil {
				file.Close()
				RespondError(w, http.StatusInternalServerError, "Failed to write torrent to tar")
				return
			}

			file.Close()
		}

		// Close writers
		if err := tarWriter.Close(); err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to finalize tar")
			return
		}
		if err := compressor.Close(); err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to finalize compression")
			return
		}
	}
}

func (h *BackupsHandler) ImportManifest(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		RespondError(w, http.StatusBadRequest, "Failed to parse multipart form")
		return
	}

	file, _, err := r.FormFile("manifest")
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Manifest file is required")
		return
	}
	defer file.Close()

	manifestData, err := io.ReadAll(file)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to read manifest file")
		return
	}

	// Get requestedBy from context or use default
	requestedBy := "api-import"
	if user := r.Context().Value("user"); user != nil {
		// TODO: extract username from context if available
		requestedBy = "user"
	}

	run, err := h.service.ImportManifest(r.Context(), instanceID, manifestData, requestedBy)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to import manifest: %v", err))
		return
	}

	RespondJSON(w, http.StatusCreated, run)
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
