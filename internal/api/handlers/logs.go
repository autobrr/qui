// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

	h.streamLoop(w, flusher, r.Context(), sub)
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
	return err //nolint:wrapcheck // SSE write errors are terminal; wrapping adds no value
}

func (h *LogsHandler) streamLoop(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, sub *logstream.Subscriber) {
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
