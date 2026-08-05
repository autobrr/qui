// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

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

func TestIsQuiLogFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"live file", "qui.log", true},
		{"rotated", "qui-2026-01-01T00-00-00.000.log", true},
		{"rotated compressed", "qui-2026-01-01T00-00-00.000.log.gz", true},
		{"live file compressed", "qui.log.gz", false},
		{"bogus timestamp compressed", "qui-notatimestamp.log.gz", false},
		{"foreign compressed log", "other-service.log.gz", false},
		{"double gzip", "qui-2026-01-01T00-00-00.000.log.gz.gz", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isQuiLogFile(tt.filename, "qui.log"); got != tt.want {
				t.Errorf("isQuiLogFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestLogsHandler_ListLogFiles_NoLogPath(t *testing.T) {
	handler := NewLogsHandler(createTestConfig(t))

	rec := httptest.NewRecorder()
	logsTestRouter(handler).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logs/files", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var files []LogFileEntry
	if err := json.NewDecoder(rec.Body).Decode(&files); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty list, got %d entries", len(files))
	}
}

func TestLogsHandler_ListLogFiles(t *testing.T) {
	handler, logDir := createTestConfigWithLogDir(t)

	writeLogsTestFile(t, logDir, "qui.log", "active")
	writeLogsTestFile(t, logDir, "qui-2026-01-01T00-00-00.000.log", "rotated")
	writeLogsTestFile(t, logDir, "notes.txt", "not a log")
	// .log files in the same directory that are not qui's must not be served.
	writeLogsTestFile(t, logDir, "other-service.log", "someone else's log")
	writeLogsTestFile(t, logDir, "qui-manual-backup.log", "prefix but not a rotation")
	if err := os.Mkdir(filepath.Join(logDir, "nested.log"), 0o750); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	logsTestRouter(handler).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logs/files", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var files []LogFileEntry
	if err := json.NewDecoder(rec.Body).Decode(&files); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 log files, got %d: %+v", len(files), files)
	}
	for _, file := range files {
		if file.Name != "qui.log" && file.Name != "qui-2026-01-01T00-00-00.000.log" {
			t.Errorf("unexpected file in listing: %q", file.Name)
		}
		if file.SizeBytes <= 0 {
			t.Errorf("expected positive size for %q, got %d", file.Name, file.SizeBytes)
		}
	}
}

func TestLogsHandler_ListLogFiles_NonLogExtension(t *testing.T) {
	handler, logDir := createTestConfigWithLogPath(t, "logs/qui.txt")

	writeLogsTestFile(t, logDir, "qui.txt", "active")
	writeLogsTestFile(t, logDir, "qui-2026-01-01T00-00-00.000.txt", "rotated")
	writeLogsTestFile(t, logDir, "other.log", "not ours")

	rec := httptest.NewRecorder()
	logsTestRouter(handler).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logs/files", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var files []LogFileEntry
	if err := json.NewDecoder(rec.Body).Decode(&files); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 log files, got %d: %+v", len(files), files)
	}
	for _, file := range files {
		if file.Name != "qui.txt" && file.Name != "qui-2026-01-01T00-00-00.000.txt" {
			t.Errorf("unexpected file in listing: %q", file.Name)
		}
	}
}

func TestLogsHandler_ListLogFiles_RejectsSymlinks(t *testing.T) {
	handler, logDir := createTestConfigWithLogDir(t)
	writeLogsTestFile(t, logDir, "qui.log", "active")
	// A symlink named like a rotation, pointing outside the log directory.
	writeLogsTestFile(t, filepath.Dir(logDir), "target.log", "outside")
	linkName := "qui-2026-01-01T00-00-00.000.log"
	if err := os.Symlink(filepath.Join(filepath.Dir(logDir), "target.log"), filepath.Join(logDir, linkName)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	router := logsTestRouter(handler)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logs/files", http.NoBody))
	var files []LogFileEntry
	if err := json.NewDecoder(rec.Body).Decode(&files); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(files) != 1 || files[0].Name != "qui.log" {
		t.Fatalf("expected only qui.log in listing, got %+v", files)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logs/files/"+linkName, http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for symlink, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogsHandler_DownloadLogFile(t *testing.T) {
	handler, logDir := createTestConfigWithLogDir(t)
	writeLogsTestFile(t, logDir, "qui.log", "log content here")

	rec := httptest.NewRecorder()
	logsTestRouter(handler).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/logs/files/qui.log", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "log content here" {
		t.Errorf("unexpected body: %q", body)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename=qui.log` {
		t.Errorf("unexpected Content-Disposition: %q", cd)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}
}

func TestLogsHandler_DownloadLogFile_Rejected(t *testing.T) {
	handler, logDir := createTestConfigWithLogDir(t)
	writeLogsTestFile(t, logDir, "qui.log", "log content")
	writeLogsTestFile(t, logDir, "notes.txt", "not a log")
	writeLogsTestFile(t, logDir, "other-service.log", "someone else's log")
	// A .log file outside the log directory that traversal must not reach.
	writeLogsTestFile(t, filepath.Dir(logDir), "secret.log", "secret")

	tests := []struct {
		name   string
		target string
	}{
		{"missing file", "/logs/files/missing.log"},
		{"non-log extension", "/logs/files/notes.txt"},
		{"unrelated log in same dir", "/logs/files/other-service.log"},
		{"posix traversal", "/logs/files/..%2Fsecret.log"},
		{"posix traversal double-encoded", "/logs/files/%2E%2E%2Fsecret.log"},
		{"windows traversal", "/logs/files/..%5Csecret.log"},
		{"absolute posix path", "/logs/files/%2Fetc%2Fpasswd"},
		{"windows drive letter", "/logs/files/C:%5Csecret.log"},
		{"windows unc path", "/logs/files/%5C%5Cserver%5Cshare%5Csecret.log"},
	}

	router := logsTestRouter(handler)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody))

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestLogsHandler_DownloadLogFile_RejectedDirectParam invokes the handler with
// pre-decoded URL params, independent of how the router escapes path segments,
// to prove the handler's own validation rejects traversal on every OS.
func TestLogsHandler_DownloadLogFile_RejectedDirectParam(t *testing.T) {
	handler, logDir := createTestConfigWithLogDir(t)
	writeLogsTestFile(t, logDir, "qui.log", "log content")
	// A .log file outside the log directory that traversal must not reach.
	writeLogsTestFile(t, filepath.Dir(logDir), "secret.log", "secret")

	tests := []struct {
		name     string
		filename string
	}{
		{"posix traversal", "../secret.log"},
		{"windows traversal", `..\secret.log`},
		{"absolute posix path", "/etc/passwd"},
		{"leading backslash", `\secret.log`},
		{"windows drive letter", `C:\secret.log`},
		{"windows unc path", `\\server\share\secret.log`},
		{"nested path", "logs/qui.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routeCtx := chi.NewRouteContext()
			routeCtx.URLParams.Add("filename", tt.filename)
			ctx := context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/logs/files/ignored", http.NoBody)
			rec := httptest.NewRecorder()

			handler.DownloadLogFile(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected status 404 for %q, got %d: %s", tt.filename, rec.Code, rec.Body.String())
			}
		})
	}
}

func logsTestRouter(handler *LogsHandler) *chi.Mux {
	router := chi.NewRouter()
	handler.Routes(router)
	return router
}

func writeLogsTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// createTestConfigWithLogDir creates a LogsHandler with file logging
// configured under a temp directory and returns the log directory.
func createTestConfigWithLogDir(t *testing.T) (*LogsHandler, string) {
	t.Helper()
	return createTestConfigWithLogPath(t, "logs/qui.log")
}

// createTestConfigWithLogPath is like createTestConfigWithLogDir but with a
// caller-chosen logPath (slash-relative, under a "logs" subdirectory).
func createTestConfigWithLogPath(t *testing.T, logPath string) (*LogsHandler, string) {
	t.Helper()

	tempDir := t.TempDir()
	logDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		t.Fatal(err)
	}

	configContent := `host = "127.0.0.1"
port = 8080
logLevel = "info"
logPath = "` + logPath + `"
`
	if err := os.WriteFile(filepath.Join(tempDir, "config.toml"), []byte(configContent), 0o600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	cfg, err := config.New(tempDir, "test")
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	return NewLogsHandler(cfg), logDir
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
