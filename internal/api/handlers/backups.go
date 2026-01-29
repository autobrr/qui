// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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

	"github.com/andybalholm/brotli"
	"github.com/go-chi/chi/v5"
	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog/log"
	"github.com/ulikunitz/xz"

	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/torrentname"
)

// ExtractedArchive represents the result of streaming extraction to disk.
// Caller must call Close() to clean up the temporary directory.
type ExtractedArchive struct {
	TempDir      string            // Root temp directory (caller must clean up)
	ManifestPath string            // Path to extracted manifest.json
	TorrentPaths map[string]string // archivePath -> absolute file path on disk
}

// Close removes the temporary directory and all extracted files.
func (e *ExtractedArchive) Close() error {
	if e.TempDir != "" {
		return os.RemoveAll(e.TempDir)
	}
	return nil
}

// streamingExtractor defines a streaming archive format extractor.
// A nil extractToDisk function indicates manifest-only (JSON) format.
type streamingExtractor struct {
	suffixes      []string
	extractToDisk func(archivePath string) (*ExtractedArchive, error)
}

// streamingExtractors lists supported formats for streaming extraction.
var streamingExtractors = []streamingExtractor{
	{suffixes: []string{".json"}}, // nil extractToDisk = manifest only
	{suffixes: []string{".zip"}, extractToDisk: extractZipToDisk},
	{suffixes: []string{".tar.gz", ".tgz"}, extractToDisk: extractTarGzToDisk},
	{suffixes: []string{".tar.zst"}, extractToDisk: extractTarZstToDisk},
	{suffixes: []string{".tar.br"}, extractToDisk: extractTarBrToDisk},
	{suffixes: []string{".tar.xz"}, extractToDisk: extractTarXzToDisk},
	{suffixes: []string{".tar"}, extractToDisk: extractTarToDisk},
}

func findStreamingExtractor(filename string) *streamingExtractor {
	filename = strings.ToLower(filename)
	for i := range streamingExtractors {
		for _, suffix := range streamingExtractors[i].suffixes {
			if strings.HasSuffix(filename, suffix) {
				return &streamingExtractors[i]
			}
		}
	}
	return nil
}

// saveUploadToTemp copies the uploaded file to a temp file for streaming extraction.
func saveUploadToTemp(src io.Reader, filename string) (string, error) {
	// Determine extension for temp file
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".tmp"
	}

	tmpFile, err := os.CreateTemp("", "qui-import-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(tmpFile, src); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("copy to temp: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("close temp: %w", err)
	}

	return tmpFile.Name(), nil
}

// copyStreamToFile copies from a reader to a file, creating parent directories as needed.
func copyStreamToFile(src io.Reader, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, src); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	return nil
}

type BackupsHandler struct {
	service *backups.Service
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

// validateBlobPath checks that a blob path doesn't escape the base directory.
// Returns the safe absolute path or empty string if the path is unsafe.
func validateBlobPath(baseDir, blobPath string) string {
	if baseDir == "" || blobPath == "" {
		return ""
	}

	// Treat Unix-style absolute paths as unsafe on all platforms.
	// (On Windows, filepath.IsAbs("/etc/passwd") is false.)
	if strings.HasPrefix(blobPath, "/") {
		return ""
	}
	// Reject any Windows volume-prefixed path (e.g. C:\..., \\server\share\...).
	if filepath.VolumeName(blobPath) != "" {
		return ""
	}

	rel := filepath.Clean(blobPath)
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
		return ""
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return ""
	}
	absTarget := filepath.Join(absBase, rel)
	relCheck, err := filepath.Rel(absBase, absTarget)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return ""
	}
	return absTarget
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
		RespondError(w, http.StatusBadRequest, "Unsupported format. Supported: zip, tar.gz, tar.zst, tar.br, tar.xz, tar")
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

	// Marshal manifest BEFORE setting headers or creating writers
	// This ensures any marshal error can be reported properly via RespondError
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		RespondError(w, http.StatusInternalServerError, "Failed to marshal manifest")
		return
	}

	filename := fmt.Sprintf("qui-backup_instance-%d_%s_%s.%s", instanceID, strings.ToLower(string(run.Kind)), run.RequestedAt.Format("2006-01-02_15-04-05"), extension)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

	if format == "zip" {
		// Create zip writer
		zipWriter := zip.NewWriter(w)
		defer zipWriter.Close()

		// Add manifest to zip
		manifestHeader := &zip.FileHeader{
			Name:   "manifest.json",
			Method: zip.Deflate,
		}
		manifestHeader.Modified = run.RequestedAt
		manifestWriter, err := zipWriter.CreateHeader(manifestHeader)
		if err != nil {
			log.Error().Err(err).Int64("runID", runID).Msg("Failed to create manifest entry in streaming ZIP")
			return
		}
		if _, err := manifestWriter.Write(manifestData); err != nil {
			log.Error().Err(err).Int64("runID", runID).Msg("Failed to write manifest in streaming ZIP")
			return
		}

		// Add torrent files to zip
		for _, item := range manifest.Items {
			if item.TorrentBlob == "" {
				continue
			}

			// Validate blob path to prevent directory traversal
			torrentPath := validateBlobPath(h.service.DataDir(), item.TorrentBlob)
			if torrentPath == "" {
				continue
			}
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
				log.Error().Err(err).Int64("runID", runID).Str("path", item.ArchivePath).Msg("Failed to create zip entry")
				return
			}

			if _, err := io.Copy(writer, file); err != nil {
				file.Close()
				log.Error().Err(err).Int64("runID", runID).Str("path", item.ArchivePath).Msg("Failed to write torrent to zip")
				return
			}

			file.Close()
		}

		// Close zip writer to finalize
		if err := zipWriter.Close(); err != nil {
			log.Error().Err(err).Int64("runID", runID).Msg("Failed to finalize zip")
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
		defer compressor.Close()

		tarWriter := tar.NewWriter(compressor)
		defer tarWriter.Close()

		// Add manifest to tar
		manifestHeader := &tar.Header{
			Name:    "manifest.json",
			Size:    int64(len(manifestData)),
			Mode:    0644,
			ModTime: run.RequestedAt,
		}
		if err := tarWriter.WriteHeader(manifestHeader); err != nil {
			log.Error().Err(err).Int64("runID", runID).Msg("Failed to write manifest header in streaming TAR")
			return
		}
		if _, err := tarWriter.Write(manifestData); err != nil {
			log.Error().Err(err).Int64("runID", runID).Msg("Failed to write manifest in streaming TAR")
			return
		}

		// Add torrent files to tar
		for _, item := range manifest.Items {
			if item.TorrentBlob == "" {
				continue
			}

			// Validate blob path to prevent directory traversal
			torrentPath := validateBlobPath(h.service.DataDir(), item.TorrentBlob)
			if torrentPath == "" {
				continue
			}
			file, err := os.Open(torrentPath)
			if err != nil {
				// Skip missing files
				continue
			}

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
				log.Error().Err(err).Int64("runID", runID).Str("path", item.ArchivePath).Msg("Failed to write tar header")
				return
			}

			if _, err := io.Copy(tarWriter, file); err != nil {
				file.Close()
				log.Error().Err(err).Int64("runID", runID).Str("path", item.ArchivePath).Msg("Failed to write torrent to tar")
				return
			}

			file.Close()
		}
		// tarWriter and compressor are closed by defers
	}
}

func (h *BackupsHandler) ImportManifest(w http.ResponseWriter, r *http.Request) {
	instanceID, err := strconv.Atoi(chi.URLParam(r, "instanceID"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	// Parse multipart form with reduced memory limit (large files spool to disk)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		RespondError(w, http.StatusBadRequest, "Failed to parse multipart form")
		return
	}

	var manifestData []byte
	var torrentPaths map[string]string

	// Check for archive upload first (zip or tar.gz containing manifest + torrents)
	if archiveFile, archiveHeader, err := r.FormFile("archive"); err == nil {
		defer archiveFile.Close()

		// Find streaming extractor
		extractor := findStreamingExtractor(archiveHeader.Filename)
		if extractor == nil {
			RespondError(w, http.StatusBadRequest, "Unsupported format. Use .json (manifest-only), .zip, .tar.gz, .tar.zst, .tar.br, .tar.xz, or .tar")
			return
		}

		if extractor.extractToDisk == nil {
			// Manifest-only upload (JSON) - read directly (small file)
			manifestData, err = io.ReadAll(archiveFile)
			if err != nil {
				RespondError(w, http.StatusInternalServerError, "Failed to read manifest file")
				return
			}
		} else {
			// Save upload to temp file for streaming extraction
			archivePath, err := saveUploadToTemp(archiveFile, archiveHeader.Filename)
			if err != nil {
				RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save archive: %v", err))
				return
			}
			defer os.Remove(archivePath)

			// Extract to temp directory
			extracted, err := extractor.extractToDisk(archivePath)
			if err != nil {
				RespondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to extract archive: %v", err))
				return
			}
			defer extracted.Close()

			// Read manifest from extracted temp file
			manifestData, err = os.ReadFile(extracted.ManifestPath)
			if err != nil {
				RespondError(w, http.StatusBadRequest, "Failed to read manifest from archive")
				return
			}

			torrentPaths = extracted.TorrentPaths
		}
	} else {
		// Fall back to manifest-only upload
		file, _, err := r.FormFile("manifest")
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Either 'archive' (zip/tar.gz) or 'manifest' file is required")
			return
		}
		defer file.Close()

		manifestData, err = io.ReadAll(file)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, "Failed to read manifest file")
			return
		}
	}

	// Get requestedBy from context or use default
	requestedBy := "api-import"
	if user := r.Context().Value("user"); user != nil {
		// TODO: extract username from context if available
		requestedBy = "user"
	}

	run, err := h.service.ImportManifestFromDir(r.Context(), instanceID, manifestData, requestedBy, torrentPaths)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to import manifest: %v", err))
		return
	}

	RespondJSON(w, http.StatusCreated, run)
}

// --- Streaming extractors (write directly to disk) ---

// extractZipToDisk extracts a zip archive to a temp directory.
func extractZipToDisk(archivePath string) (*ExtractedArchive, error) {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer archiveFile.Close()

	info, err := archiveFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat archive: %w", err)
	}

	reader, err := zip.NewReader(archiveFile, info.Size())
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "qui-extract-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	result := &ExtractedArchive{
		TempDir:      tempDir,
		TorrentPaths: make(map[string]string),
	}

	cleanup := func() { os.RemoveAll(tempDir) }

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		name := file.Name
		baseName := strings.ToLower(filepath.Base(name))

		if baseName == "manifest.json" {
			destPath := filepath.Join(tempDir, "manifest.json")
			if err := extractZipFileToDisk(file, destPath); err != nil {
				cleanup()
				return nil, fmt.Errorf("extract manifest: %w", err)
			}
			result.ManifestPath = destPath
		} else if strings.HasSuffix(baseName, ".torrent") {
			// Validate path to prevent directory traversal
			safePath := filepath.Clean(name)
			if filepath.IsAbs(safePath) || strings.HasPrefix(safePath, "..") {
				continue
			}
			destPath := filepath.Join(tempDir, "torrents", safePath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				cleanup()
				return nil, fmt.Errorf("create dir: %w", err)
			}
			if err := extractZipFileToDisk(file, destPath); err != nil {
				cleanup()
				return nil, fmt.Errorf("extract %s: %w", name, err)
			}
			result.TorrentPaths[name] = destPath
		}
	}

	if result.ManifestPath == "" {
		cleanup()
		return nil, fmt.Errorf("manifest.json not found in archive")
	}

	return result, nil
}

// extractZipFileToDisk extracts a single zip file entry to disk.
func extractZipFileToDisk(zf *zip.File, destPath string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, rc)
	return err
}

// extractTarGzToDisk extracts a tar.gz archive to a temp directory.
func extractTarGzToDisk(archivePath string) (*ExtractedArchive, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gzReader.Close()

	return extractTarReaderToDisk(gzReader)
}

// extractTarToDisk extracts a plain tar archive to a temp directory.
func extractTarToDisk(archivePath string) (*ExtractedArchive, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	return extractTarReaderToDisk(f)
}

// extractTarZstToDisk extracts a tar.zst archive to a temp directory.
func extractTarZstToDisk(archivePath string) (*ExtractedArchive, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	zstReader, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open zstd: %w", err)
	}
	defer zstReader.Close()

	return extractTarReaderToDisk(zstReader)
}

// extractTarBrToDisk extracts a tar.br archive to a temp directory.
func extractTarBrToDisk(archivePath string) (*ExtractedArchive, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	brReader := brotli.NewReader(f)
	return extractTarReaderToDisk(brReader)
}

// extractTarXzToDisk extracts a tar.xz archive to a temp directory.
func extractTarXzToDisk(archivePath string) (*ExtractedArchive, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	xzReader, err := xz.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open xz: %w", err)
	}

	return extractTarReaderToDisk(xzReader)
}

// extractTarReaderToDisk extracts a tar stream to a temp directory.
func extractTarReaderToDisk(r io.Reader) (*ExtractedArchive, error) {
	tempDir, err := os.MkdirTemp("", "qui-extract-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	result := &ExtractedArchive{
		TempDir:      tempDir,
		TorrentPaths: make(map[string]string),
	}

	cleanup := func() { os.RemoveAll(tempDir) }

	tarReader := tar.NewReader(r)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("read tar: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := header.Name
		baseName := strings.ToLower(filepath.Base(name))

		if baseName == "manifest.json" {
			destPath := filepath.Join(tempDir, "manifest.json")
			if err := copyStreamToFile(tarReader, destPath); err != nil {
				cleanup()
				return nil, fmt.Errorf("extract manifest: %w", err)
			}
			result.ManifestPath = destPath
		} else if strings.HasSuffix(baseName, ".torrent") {
			// Validate path to prevent directory traversal
			safePath := filepath.Clean(name)
			if filepath.IsAbs(safePath) || strings.HasPrefix(safePath, "..") {
				// Skip unsafe paths but continue reading to consume the entry
				_, _ = io.Copy(io.Discard, tarReader)
				continue
			}
			destPath := filepath.Join(tempDir, "torrents", safePath)
			if err := copyStreamToFile(tarReader, destPath); err != nil {
				cleanup()
				return nil, fmt.Errorf("extract %s: %w", name, err)
			}
			result.TorrentPaths[name] = destPath
		} else {
			// Skip other files but consume the data
			_, _ = io.Copy(io.Discard, tarReader)
		}
	}

	if result.ManifestPath == "" {
		cleanup()
		return nil, fmt.Errorf("manifest.json not found in archive")
	}

	return result, nil
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
