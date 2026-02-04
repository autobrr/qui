// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/config"
)

func TestLogsHandler_GetLogSettings(t *testing.T) {
	// Create a minimal config for testing
	appConfig := createTestConfig(t)

	handler := NewLogsHandler(appConfig)

	req := httptest.NewRequest(http.MethodGet, "/log-settings", http.NoBody)
	rec := httptest.NewRecorder()

	handler.GetLogSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var settings config.LogSettingsResponse
	if err := json.NewDecoder(rec.Body).Decode(&settings); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if settings.Level == "" {
		t.Error("expected non-empty level")
	}
}

func TestLogsHandler_UpdateLogSettings_InvalidLevel(t *testing.T) {
	appConfig := createTestConfig(t)
	handler := NewLogsHandler(appConfig)

	body := strings.NewReader(`{"level": "INVALID"}`)
	req := httptest.NewRequest(http.MethodPut, "/log-settings", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.UpdateLogSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestLogsHandler_UpdateLogSettings_InvalidMaxSize(t *testing.T) {
	appConfig := createTestConfig(t)
	handler := NewLogsHandler(appConfig)

	body := strings.NewReader(`{"maxSize": 0}`)
	req := httptest.NewRequest(http.MethodPut, "/log-settings", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.UpdateLogSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestLogsHandler_UpdateLogSettings_InvalidMaxBackups(t *testing.T) {
	appConfig := createTestConfig(t)
	handler := NewLogsHandler(appConfig)

	body := strings.NewReader(`{"maxBackups": -1}`)
	req := httptest.NewRequest(http.MethodPut, "/log-settings", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.UpdateLogSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestLogsHandler_UpdateLogSettings_InvalidJSON(t *testing.T) {
	appConfig := createTestConfig(t)
	handler := NewLogsHandler(appConfig)

	body := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest(http.MethodPut, "/log-settings", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.UpdateLogSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestLogsHandler_StreamLogs_SSEHeaders(t *testing.T) {
	appConfig := createTestConfig(t)
	handler := NewLogsHandler(appConfig)

	// Use a cancellable context to stop the SSE handler
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, "/logs/stream", http.NoBody).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Run in a goroutine since SSE blocks
	done := make(chan struct{})
	go func() {
		handler.StreamLogs(rec, req)
		close(done)
	}()

	// Give it a moment to set headers
	time.Sleep(50 * time.Millisecond)

	// Cancel the context to stop the handler
	cancel()

	// Wait for handler to finish before checking
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not stop within timeout")
	}

	// Check headers were set
	headers := rec.Header()
	if ct := headers.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
	}
	if cc := headers.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control 'no-cache', got %q", cc)
	}
	if xa := headers.Get("X-Accel-Buffering"); xa != "no" {
		t.Errorf("expected X-Accel-Buffering 'no', got %q", xa)
	}
}

func TestLogsHandler_StreamLogs_WritesToHub(t *testing.T) {
	appConfig := createTestConfig(t)
	handler := NewLogsHandler(appConfig)
	hub := handler.GetHub()

	// Write some log lines before connecting
	hub.Write("test line 1")
	hub.Write("test line 2")

	// Use a cancellable context to stop the SSE handler
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, "/logs/stream?limit=10", http.NoBody).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Run in a goroutine
	done := make(chan struct{})
	go func() {
		handler.StreamLogs(rec, req)
		close(done)
	}()

	// Give time for initial history to be sent
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to stop the handler
	cancel()

	// Wait for handler to finish before reading body
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not stop within timeout")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "test line 1") {
		t.Error("expected body to contain 'test line 1'")
	}
	if !strings.Contains(body, "test line 2") {
		t.Error("expected body to contain 'test line 2'")
	}
}

// createTestConfig creates a minimal AppConfig for testing.
func createTestConfig(t *testing.T) *config.AppConfig {
	t.Helper()

	// Create a temp dir for config
	tempDir := t.TempDir()

	// Create a minimal config.toml file
	configPath := tempDir + "/config.toml"
	configContent := `host = "127.0.0.1"
port = 8080
logLevel = "info"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	cfg, err := config.New(tempDir, "test")
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	return cfg
}
