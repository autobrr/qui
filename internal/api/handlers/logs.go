// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/logstream"
)

// LogsHandler handles log settings and streaming endpoints.
type LogsHandler struct {
	appConfig *config.AppConfig
}

// NewLogsHandler creates a new LogsHandler.
func NewLogsHandler(appConfig *config.AppConfig) *LogsHandler {
	return &LogsHandler{
		appConfig: appConfig,
	}
}

// Routes registers the log routes on the provided router.
func (h *LogsHandler) Routes(r chi.Router) {
	r.Get("/log-settings", h.GetLogSettings)
	r.Put("/log-settings", h.UpdateLogSettings)
	r.Get("/logs/stream", h.StreamLogs)
	r.Get("/logs/files", h.ListLogFiles)
	r.Get("/logs/files/{filename}", h.DownloadLogFile)
}

// LogFileEntry describes a log file available for download.
type LogFileEntry struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	ModTime   time.Time `json:"modTime"`
}

// rotationTimestampRegex matches lumberjack's backup timestamp
// (2006-01-02T15-04-05.000).
var rotationTimestampRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}$`)

// isQuiLogFile reports whether name is the configured log file or a lumberjack
// rotation of it (base-{timestamp}{ext}). Anything else in the directory —
// including .log files from other services — is not ours to serve.
func isQuiLogFile(name, base string) bool {
	if name == base {
		return true
	}
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext) + "-"
	// Only a rotation carries lumberjack's ".gz": the live file returned above is
	// never compressed, so trimming here cannot widen what the live name matches.
	name = strings.TrimSuffix(name, ".gz")
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
		return false
	}
	return rotationTimestampRegex.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext))
}

// listLogFiles returns the active and rotated log files for the configured
// log path. Returns an empty slice when file logging is not configured or
// the directory does not exist.
func (h *LogsHandler) listLogFiles() []LogFileEntry {
	files := make([]LogFileEntry, 0)

	logPath := h.appConfig.GetLogSettings().Path
	if logPath == "" {
		return files
	}

	entries, err := os.ReadDir(filepath.Dir(logPath))
	if err != nil {
		return files
	}

	base := filepath.Base(logPath)
	for _, entry := range entries {
		// Reject symlinks: a link named like a log file must not let the
		// download endpoint serve a target outside the log directory.
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !isQuiLogFile(entry.Name(), base) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, LogFileEntry{
			Name:      entry.Name(),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime(),
		})
	}

	return files
}

// ListLogFiles returns the log files available for download.
func (h *LogsHandler) ListLogFiles(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, h.listLogFiles())
}

// DownloadLogFile serves a single log file from the configured log directory.
func (h *LogsHandler) DownloadLogFile(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")

	// Traversal guard by construction: the requested name must exactly match
	// a directory entry from the listing, so client input is never joined
	// into a path unless it names one of qui's own log files.
	found := false
	for _, entry := range h.listLogFiles() {
		if entry.Name == filename {
			found = true
			break
		}
	}
	if !found {
		RespondError(w, http.StatusNotFound, "Log file not found")
		return
	}

	// #nosec G703,G304 -- filename is validated against the log directory listing above.
	file, err := os.Open(filepath.Join(filepath.Dir(h.appConfig.GetLogSettings().Path), filename))
	if err != nil {
		RespondError(w, http.StatusNotFound, "Log file not found")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		RespondError(w, http.StatusNotFound, "Log file not found")
		return
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		disposition = fmt.Sprintf("attachment; filename=%q", filename)
	}

	contentType := "text/plain; charset=utf-8"
	if strings.HasSuffix(filename, ".gz") {
		contentType = "application/gzip"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

// GetLogSettings returns the current log settings.
func (h *LogsHandler) GetLogSettings(w http.ResponseWriter, r *http.Request) {
	settings := h.appConfig.GetLogSettings()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// UpdateLogSettings updates the log settings.
func (h *LogsHandler) UpdateLogSettings(w http.ResponseWriter, r *http.Request) {
	var update config.LogSettingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate log level if provided
	if update.Level != nil {
		validLevels := map[string]bool{
			"trace": true, "debug": true, "info": true, "warn": true, "error": true,
			"TRACE": true, "DEBUG": true, "INFO": true, "WARN": true, "ERROR": true,
		}
		if !validLevels[*update.Level] {
			http.Error(w, "invalid log level: "+*update.Level, http.StatusBadRequest)
			return
		}
	}

	// Validate maxSize if provided
	if update.MaxSize != nil && *update.MaxSize < 1 {
		http.Error(w, "maxSize must be at least 1 MB", http.StatusBadRequest)
		return
	}

	// Validate maxBackups if provided
	if update.MaxBackups != nil && *update.MaxBackups < 0 {
		http.Error(w, "maxBackups cannot be negative", http.StatusBadRequest)
		return
	}

	settings, err := h.appConfig.UpdateLogSettings(update)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// StreamLogs streams log lines via SSE.
func (h *LogsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	limit := h.parseLimit(r)

	flusher, hub, err := h.prepareSSE(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sub := hub.Subscribe(r.Context())
	defer hub.Unsubscribe(sub)

	if err := h.sendHistory(w, flusher, hub, limit); err != nil {
		return
	}

	h.streamLoop(r.Context(), w, flusher, sub)
}

func (h *LogsHandler) parseLimit(r *http.Request) int {
	limit := 1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return limit
}

var (
	errStreamingNotSupported = errors.New("streaming not supported")
	errLogStreamNotAvailable = errors.New("log streaming not available")
)

func (h *LogsHandler) prepareSSE(w http.ResponseWriter) (http.Flusher, *logstream.Hub, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, nil, errStreamingNotSupported
	}

	hub := h.appConfig.GetLogManager().GetHub()
	if hub == nil {
		return nil, nil, errLogStreamNotAvailable
	}

	return flusher, hub, nil
}

func (h *LogsHandler) sendHistory(w http.ResponseWriter, flusher http.Flusher, hub *logstream.Hub, limit int) error {
	history := hub.History(limit)
	for _, line := range history {
		if err := writeSSEData(w, line); err != nil {
			return err
		}
	}
	flusher.Flush()
	return nil
}

func writeSSEData(w http.ResponseWriter, data string) error {
	_, err := fmt.Fprintf(w, "data: %s\n\n", data) //nolint:gosec // G705: an SSE stream is text/event-stream, never rendered as HTML
	return err                                     //nolint:wrapcheck // SSE write errors are terminal; wrapping adds no value
}

func (h *LogsHandler) streamLoop(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, sub *logstream.Subscriber) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-sub.Channel():
			if !ok {
				return
			}
			if err := writeSSEData(w, line); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if err := writeSSEComment(w, "keepalive"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEComment(w http.ResponseWriter, comment string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", comment)
	return err //nolint:wrapcheck // SSE write errors are terminal; wrapping adds no value
}

// GetHub returns the log hub from the handler's config.
// This is useful for testing.
func (h *LogsHandler) GetHub() *logstream.Hub {
	return h.appConfig.GetLogManager().GetHub()
}
