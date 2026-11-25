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

	"github.com/autobrr/qui/internal/api/middleware"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/logstream"
	"github.com/autobrr/qui/internal/models"
)

type LogsHandler struct {
	appConfig *config.AppConfig
	shutdown  <-chan struct{}
}

func NewLogsHandler(appConfig *config.AppConfig, shutdown <-chan struct{}) *LogsHandler {
	return &LogsHandler{
		appConfig: appConfig,
		shutdown:  shutdown,
	}
}

func (h *LogsHandler) Routes(r chi.Router) {
	logs := middleware.Permissions(models.AreaLogs)
	r.With(logs.Read).Get("/log-settings", h.GetLogSettings)
	r.With(logs.Write).Put("/log-settings", h.UpdateLogSettings)
	r.With(logs.Read).Get("/logs/files", h.ListLogFiles)
	r.With(logs.Read).Get("/logs/files/{filename}", h.DownloadLogFile)
}

type LogFileEntry struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	ModTime   time.Time `json:"modTime"`
}

var rotationTimestampRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}$`)

func isQuiLogFile(name, base string) bool {
	if name == base {
		return true
	}
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext) + "-"
	name = strings.TrimSuffix(name, ".gz")
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
		return false
	}
	return rotationTimestampRegex.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext))
}

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

func (h *LogsHandler) ListLogFiles(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, h.listLogFiles())
}

func (h *LogsHandler) DownloadLogFile(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")

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

func (h *LogsHandler) GetLogSettings(w http.ResponseWriter, r *http.Request) {
	settings := h.appConfig.GetLogSettings()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (h *LogsHandler) UpdateLogSettings(w http.ResponseWriter, r *http.Request) {
	var update config.LogSettingsUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if update.Path != nil && *update.Path == h.appConfig.GetLogSettings().Path {
		update.Path = nil
	}
	if update.Path != nil && !middleware.PassesBrowserOnly(w, r) {
		return
	}

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

	if update.MaxSize != nil && *update.MaxSize < 1 {
		http.Error(w, "maxSize must be at least 1 MB", http.StatusBadRequest)
		return
	}

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
	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func (h *LogsHandler) streamLoop(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, sub *logstream.Subscriber) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.shutdown:
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
	return err
}

func (h *LogsHandler) GetHub() *logstream.Hub {
	return h.appConfig.GetLogManager().GetHub()
}
