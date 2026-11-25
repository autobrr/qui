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

type LogManager struct {
	hub         *logstream.Hub
	switchable  *logstream.SwitchableWriter
	version     string
	mu          sync.Mutex
	initialized atomic.Bool
}

func NewLogManager(version string) *LogManager {
	hub := logstream.NewHub(logstream.DefaultBufferSize)
	switchable := logstream.NewSwitchableWriter(ignoreWriteErrors{baseLogWriter(version)}, hub)

	return &LogManager{
		hub:        hub,
		switchable: switchable,
		version:    version,
	}
}

func (lm *LogManager) Initialize() {
	if lm.initialized.Swap(true) {
		return
	}
	log.Logger = log.Logger.Output(lm.switchable).Level(zerolog.TraceLevel)
}

func (lm *LogManager) GetHub() *logstream.Hub {
	return lm.hub
}

func (lm *LogManager) Apply(level, logPath string, maxSize, maxBackups int) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	setLogLevel(level)

	baseWriter := baseLogWriter(lm.version)
	newWriter, newCloser, err := lm.buildWriter(baseWriter, logPath, maxSize, maxBackups)
	if err != nil {
		return err
	}

	oldCloser := lm.switchable.Swap(newWriter, newCloser)

	if oldCloser != nil {
		if closeErr := oldCloser.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("Failed to close old log rotator")
		}
	}

	return nil
}

func (lm *LogManager) buildWriter(baseWriter io.Writer, logPath string, maxSize, maxBackups int) (io.Writer, io.Closer, error) {
	baseWriter = ignoreWriteErrors{baseWriter}
	if logPath == "" {
		return baseWriter, nil, nil
	}

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
		Compress:   true,
	}
	return io.MultiWriter(baseWriter, rotator), rotator, nil
}

type ignoreWriteErrors struct{ io.Writer }

func (w ignoreWriteErrors) Write(p []byte) (int, error) {
	_, _ = w.Writer.Write(p)
	return len(p), nil
}

type LogSettingsResponse struct {
	Level      string            `json:"level"`
	Path       string            `json:"path"`
	MaxSize    int               `json:"maxSize"`
	MaxBackups int               `json:"maxBackups"`
	ConfigPath string            `json:"configPath,omitempty"`
	Locked     map[string]string `json:"locked,omitempty"`
}

type LogSettingsUpdate struct {
	Level      *string `json:"level,omitempty"`
	Path       *string `json:"path,omitempty"`
	MaxSize    *int    `json:"maxSize,omitempty"`
	MaxBackups *int    `json:"maxBackups,omitempty"`
}
