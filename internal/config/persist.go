// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	lockedByEnv      = "environment"
	lockedByEnvEmpty = "environment (empty)"
)

var persistMu sync.Mutex

func (c *AppConfig) PersistLogSettings(level, path string, maxSize, maxBackups int) error {
	persistMu.Lock()
	defer persistMu.Unlock()

	configPath := c.viper.ConfigFileUsed()
	if configPath == "" {
		return errors.New("no config file path available")
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	updated := updateLogSettingsInTOML(string(content), level, path, maxSize, maxBackups)

	dir := filepath.Dir(configPath)
	tmpFile, err := os.CreateTemp(dir, ".config.toml.tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	_, err = tmpFile.WriteString(updated)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

type logSettings struct {
	level, path         string
	maxSize, maxBackups int
}

func updateLogSettingsInTOML(content, level, path string, maxSize, maxBackups int) string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	updated := make(map[string]bool)
	active := findActiveLogSettings(lines)
	settings := logSettings{level, path, maxSize, maxBackups}

	for _, line := range lines {
		result = append(result, processLogLine(line, settings, updated, active))
	}

	appended := appendMissingSettings(updated, settings)
	if len(appended) > 0 {
		result = append(result, "", "# Log settings")
		result = append(result, appended...)
	}

	return strings.Join(result, "\n")
}

func findActiveLogSettings(lines []string) map[string]bool {
	active := make(map[string]bool)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key := canonicalLogKey(extractKey(trimmed))
		if key == "" {
			continue
		}
		active[key] = true
	}
	return active
}

func processLogLine(line string, s logSettings, updated map[string]bool, active map[string]bool) string {
	trimmed := strings.TrimSpace(line)

	if trimmed == "" {
		return line
	}

	key := canonicalLogKey(extractKey(trimmed))
	if key == "" {
		return line
	}

	if strings.HasPrefix(trimmed, "#") {
		if active[key] || updated[key] {
			return line
		}
	}

	return renderLogSettingLine(key, s, updated)
}

func renderLogSettingLine(key string, s logSettings, updated map[string]bool) string {
	updated[key] = true

	switch key {
	case "logLevel":
		return fmt.Sprintf("logLevel = %q", s.level)
	case "logPath":
		if s.path == "" {
			return fmt.Sprintf("#logPath = %q", s.path)
		}
		return fmt.Sprintf("logPath = %q", s.path)
	case "logMaxSize":
		return fmt.Sprintf("logMaxSize = %d", s.maxSize)
	case "logMaxBackups":
		return fmt.Sprintf("logMaxBackups = %d", s.maxBackups)
	default:
		return ""
	}
}

func canonicalLogKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "loglevel":
		return "logLevel"
	case "logpath":
		return "logPath"
	case "logmaxsize":
		return "logMaxSize"
	case "logmaxbackups":
		return "logMaxBackups"
	default:
		return ""
	}
}

func appendMissingSettings(updated map[string]bool, s logSettings) []string {
	var appended []string
	if !updated["logLevel"] {
		appended = append(appended, fmt.Sprintf("logLevel = %q", s.level))
	}
	if !updated["logPath"] && s.path != "" {
		appended = append(appended, fmt.Sprintf("logPath = %q", s.path))
	}
	if !updated["logMaxSize"] {
		appended = append(appended, fmt.Sprintf("logMaxSize = %d", s.maxSize))
	}
	if !updated["logMaxBackups"] {
		appended = append(appended, fmt.Sprintf("logMaxBackups = %d", s.maxBackups))
	}
	return appended
}

func extractKey(line string) string {
	line = strings.TrimPrefix(line, "#")
	line = strings.TrimSpace(line)

	key, _, found := strings.Cut(line, "=")
	if !found {
		return ""
	}
	return strings.TrimSpace(key)
}

func (c *AppConfig) GetLockedLogSettings() map[string]string {
	locked := make(map[string]string)
	checkEnvLock(locked, "level", envPrefix+"LOG_LEVEL")
	checkEnvLock(locked, "path", envPrefix+"LOG_PATH")
	checkEnvLock(locked, "maxSize", envPrefix+"LOG_MAX_SIZE")
	checkEnvLock(locked, "maxBackups", envPrefix+"LOG_MAX_BACKUPS")
	return locked
}

func checkEnvLock(locked map[string]string, key, envVar string) {
	if value, ok := os.LookupEnv(envVar); ok {
		if strings.TrimSpace(value) == "" {
			locked[key] = lockedByEnvEmpty
		} else {
			locked[key] = lockedByEnv
		}
	}
}

func (c *AppConfig) GetLogSettings() LogSettingsResponse {
	c.configMu.Lock()
	level := canonicalizeLogLevel(c.Config.LogLevel)
	path := c.ResolveLogPath(c.Config.LogPath)
	maxSize := c.Config.LogMaxSize
	maxBackups := c.Config.LogMaxBackups
	configPath := c.viper.ConfigFileUsed()
	c.configMu.Unlock()

	return LogSettingsResponse{
		Level:      level,
		Path:       path,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		ConfigPath: configPath,
		Locked:     c.GetLockedLogSettings(),
	}
}

func canonicalizeLogLevel(level string) string {
	normalized := strings.ToUpper(strings.TrimSpace(level))
	switch normalized {
	case "TRACE", "DEBUG", "INFO", "WARN", "ERROR":
		return normalized
	default:
		return "INFO"
	}
}

func validateLockedFields(update LogSettingsUpdate, locked map[string]string) error {
	if update.Level != nil && locked["level"] != "" {
		return fmt.Errorf("cannot modify level: locked by %s", locked["level"])
	}
	if update.Path != nil && locked["path"] != "" {
		return fmt.Errorf("cannot modify path: locked by %s", locked["path"])
	}
	if update.MaxSize != nil && locked["maxSize"] != "" {
		return fmt.Errorf("cannot modify maxSize: locked by %s", locked["maxSize"])
	}
	if update.MaxBackups != nil && locked["maxBackups"] != "" {
		return fmt.Errorf("cannot modify maxBackups: locked by %s", locked["maxBackups"])
	}
	return nil
}

func (c *AppConfig) UpdateLogSettings(update LogSettingsUpdate) (LogSettingsResponse, error) {
	c.configMu.Lock()
	defer c.configMu.Unlock()

	if err := validateLockedFields(update, c.GetLockedLogSettings()); err != nil {
		return LogSettingsResponse{}, err
	}
	if update.Path != nil && *update.Path != "" && !strings.EqualFold(filepath.Ext(*update.Path), ".log") {
		return LogSettingsResponse{}, errors.New("log file path must end in .log")
	}

	oldLevel := c.Config.LogLevel
	oldPath := c.Config.LogPath
	oldMaxSize := c.Config.LogMaxSize
	oldMaxBackups := c.Config.LogMaxBackups

	committed := false
	defer func() {
		if committed {
			return
		}
		c.Config.LogLevel = oldLevel
		c.Config.LogPath = oldPath
		c.Config.LogMaxSize = oldMaxSize
		c.Config.LogMaxBackups = oldMaxBackups
		c.viper.Set("logLevel", oldLevel)
		c.viper.Set("logPath", oldPath)
		c.viper.Set("logMaxSize", oldMaxSize)
		c.viper.Set("logMaxBackups", oldMaxBackups)
		c.ApplyLogConfig() //nolint:errcheck // best-effort rollback, error is not actionable
	}()

	if update.Level != nil {
		c.Config.LogLevel = canonicalizeLogLevel(*update.Level)
		c.viper.Set("logLevel", c.Config.LogLevel)
	}
	if update.Path != nil {
		c.Config.LogPath = *update.Path
		c.viper.Set("logPath", c.Config.LogPath)
	}
	if update.MaxSize != nil {
		c.Config.LogMaxSize = *update.MaxSize
		c.viper.Set("logMaxSize", c.Config.LogMaxSize)
	}
	if update.MaxBackups != nil {
		c.Config.LogMaxBackups = *update.MaxBackups
		c.viper.Set("logMaxBackups", c.Config.LogMaxBackups)
	}

	if err := c.ApplyLogConfig(); err != nil {
		return LogSettingsResponse{}, fmt.Errorf("failed to apply log configuration: %w", err)
	}

	if err := c.PersistLogSettings(c.Config.LogLevel, c.Config.LogPath, c.Config.LogMaxSize, c.Config.LogMaxBackups); err != nil {
		return LogSettingsResponse{}, fmt.Errorf("failed to persist settings: %w", err)
	}

	committed = true
	return LogSettingsResponse{
		Level:      canonicalizeLogLevel(c.Config.LogLevel),
		Path:       c.ResolveLogPath(c.Config.LogPath),
		MaxSize:    c.Config.LogMaxSize,
		MaxBackups: c.Config.LogMaxBackups,
		ConfigPath: c.viper.ConfigFileUsed(),
		Locked:     c.GetLockedLogSettings(),
	}, nil
}
