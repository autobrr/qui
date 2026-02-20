// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/autobrr/qui/internal/logstream"
)

// LogManager handles log configuration with safe runtime reconfiguration.
type LogManager struct {
	hub         *logstream.Hub
	switchable  *logstream.SwitchableWriter
	version     string
	mu          sync.Mutex
	initialized atomic.Bool
}

// NewLogManager creates a new LogManager with the given version string.
func NewLogManager(version string) *LogManager {
	hub := logstream.NewHub(logstream.DefaultBufferSize)
	baseWriter := baseLogWriter(version)
	switchable := logstream.NewSwitchableWriter(baseWriter, hub)

	return &LogManager{
		hub:        hub,
		switchable: switchable,
		version:    version,
	}
}

// Initialize sets up the global logger to use the switchable writer.
// This should only be called once during application startup.
func (lm *LogManager) Initialize() {
	if lm.initialized.Swap(true) {
		return // Already initialized
	}
	// Set the logger output once at startup, and keep the logger level at the
	// lowest level so the global level can be changed at runtime without
	// mutating log.Logger (avoids data races with concurrent logging).
	log.Logger = log.Logger.Output(lm.switchable).Level(zerolog.TraceLevel)
}

// GetHub returns the log streaming hub for SSE endpoints.
func (lm *LogManager) GetHub() *logstream.Hub {
	return lm.hub
}

// Apply updates the log configuration with the given settings.
// It is safe to call this method concurrently from multiple goroutines.
// Returns an error if file logging is requested but cannot be enabled.
func (lm *LogManager) Apply(level, logPath string, maxSize, maxBackups int) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Update log level
	setLogLevel(level)

	// Build the new writer stack
	baseWriter := baseLogWriter(lm.version)
	newWriter, newCloser, err := lm.buildWriter(baseWriter, logPath, maxSize, maxBackups)
	if err != nil {
		return err
	}

	// Swap the writer atomically
	oldCloser := lm.switchable.Swap(newWriter, newCloser)

	// Close the old rotator after swapping (if any)
	if oldCloser != nil {
		if closeErr := oldCloser.Close(); closeErr != nil {
			// Log to the new writer (already swapped)
			log.Debug().Err(closeErr).Msg("Failed to close old log rotator")
		}
	}

	return nil
}

func (lm *LogManager) buildWriter(baseWriter io.Writer, logPath string, maxSize, maxBackups int) (io.Writer, io.Closer, error) {
	if logPath == "" {
		return baseWriter, nil, nil
	}

	// Create log directory if needed
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("failed to create log directory %s: %w", dir, err)
	}

	if maxSize <= 0 {
		maxSize = 50
	}
	if maxBackups < 0 {
		maxBackups = 0
	}

	rotator := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
	}
	return io.MultiWriter(baseWriter, rotator), rotator, nil
}

// LogSettingsResponse represents the log settings for API responses.
type LogSettingsResponse struct {
	Level      string            `json:"level"`
	Path       string            `json:"path"`
	MaxSize    int               `json:"maxSize"`
	MaxBackups int               `json:"maxBackups"`
	ConfigPath string            `json:"configPath,omitempty"`
	Locked     map[string]string `json:"locked,omitempty"`
}

// LogSettingsUpdate represents a request to update log settings.
type LogSettingsUpdate struct {
	Level      *string `json:"level,omitempty"`
	Path       *string `json:"path,omitempty"`
	MaxSize    *int    `json:"maxSize,omitempty"`
	MaxBackups *int    `json:"maxBackups,omitempty"`
}
