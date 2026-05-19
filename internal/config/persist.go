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

// persistMu ensures only one goroutine writes to config.toml at a time.
var persistMu sync.Mutex

// PersistLogSettings atomically updates only the log-related keys in config.toml.
// It preserves all other content and comments.
func (c *AppConfig) PersistLogSettings(level, path string, maxSize, maxBackups int) error {
	persistMu.Lock()
	defer persistMu.Unlock()

	configPath := c.viper.ConfigFileUsed()
	if configPath == "" {
		return errors.New("no config file path available")
	}

	// Read existing file
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse and update only log settings
	updated := updateLogSettingsInTOML(string(content), level, path, maxSize, maxBackups)

	// Write atomically: temp file + fsync + rename
	dir := filepath.Dir(configPath)
	tmpFile, err := os.CreateTemp(dir, ".config.toml.tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Write content
	_, err = tmpFile.WriteString(updated)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Sync to disk
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	tmpFile.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// logSettings holds the values for updating TOML.
type logSettings struct {
	level, path         string
	maxSize, maxBackups int
}

// updateLogSettingsInTOML updates log-related settings in a TOML string.
// It preserves comments and other settings.
func updateLogSettingsInTOML(content, level, path string, maxSize, maxBackups int) string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	updated := make(map[string]bool)
	active := findActiveLogSettings(lines)
	settings := logSettings{level, path, maxSize, maxBackups}

	for _, line := range lines {
		result = append(result, processLogLine(line, settings, updated, active))
	}

	// Append any settings that weren't in the file
	appended := appendMissingSettings(updated, settings)
	if len(appended) > 0 {
		result = append(result, "", "# Log settings")
		result = append(result, appended...)
	}

	return strings.Join(result, "\n")
}

// findActiveLogSettings returns log setting keys that already have active entries.
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

// processLogLine processes a single TOML line, updating log settings as needed.
func processLogLine(line string, s logSettings, updated map[string]bool, active map[string]bool) string {
	trimmed := strings.TrimSpace(line)

	// Preserve empty lines as-is
	if trimmed == "" {
		return line
	}

	key := canonicalLogKey(extractKey(trimmed))
	if key == "" {
		return line
	}

	if strings.HasPrefix(trimmed, "#") {
		// Promote only when there is no active key and we have not already updated it.
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

// appendMissingSettings returns TOML lines for settings not already in the file.
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

// extractKey extracts the key name from a TOML line like "key = value".
func extractKey(line string) string {
	// Handle commented lines that might be settings
	line = strings.TrimPrefix(line, "#")
	line = strings.TrimSpace(line)

	key, _, found := strings.Cut(line, "=")
	if !found {
		return ""
	}
	return strings.TrimSpace(key)
}

// GetLockedLogSettings returns a map of log setting keys that are locked by env/CLI.
func (c *AppConfig) GetLockedLogSettings() map[string]string {
	locked := make(map[string]string)
	checkEnvLock(locked, "level", envPrefix+"LOG_LEVEL")
	checkEnvLock(locked, "path", envPrefix+"LOG_PATH")
	checkEnvLock(locked, "maxSize", envPrefix+"LOG_MAX_SIZE")
	checkEnvLock(locked, "maxBackups", envPrefix+"LOG_MAX_BACKUPS")
	return locked
}

// checkEnvLock adds a lock entry if the environment variable is set.
func checkEnvLock(locked map[string]string, key, envVar string) {
	if value, ok := os.LookupEnv(envVar); ok {
		if strings.TrimSpace(value) == "" {
			locked[key] = lockedByEnvEmpty
		} else {
			locked[key] = lockedByEnv
		}
	}
}

// GetLogSettings returns the current log settings with locked field information.
// The Path field is resolved to an absolute path (relative paths are resolved against the config directory).
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

// canonicalizeLogLevel normalizes a log level string to uppercase.
// Returns "INFO" if the level is empty or invalid.
func canonicalizeLogLevel(level string) string {
	normalized := strings.ToUpper(strings.TrimSpace(level))
	switch normalized {
	case "TRACE", "DEBUG", "INFO", "WARN", "ERROR":
		return normalized
	default:
		return "INFO"
	}
}

// validateLockedFields checks if any locked fields are being modified.
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

// UpdateLogSettings validates and applies log settings updates.
// It rejects changes to locked fields and returns an error if any locked field is modified.
func (c *AppConfig) UpdateLogSettings(update LogSettingsUpdate) (LogSettingsResponse, error) {
	c.configMu.Lock()
	defer c.configMu.Unlock()

	if err := validateLockedFields(update, c.GetLockedLogSettings()); err != nil {
		return LogSettingsResponse{}, err
	}

	// Save current values for rollback on failure
	oldLevel := c.Config.LogLevel
	oldPath := c.Config.LogPath
	oldMaxSize := c.Config.LogMaxSize
	oldMaxBackups := c.Config.LogMaxBackups

	// Track whether the update succeeded; rollback on any failure path
	committed := false
	defer func() {
		if committed {
			return
		}
		// Rollback in-memory config
		c.Config.LogLevel = oldLevel
		c.Config.LogPath = oldPath
		c.Config.LogMaxSize = oldMaxSize
		c.Config.LogMaxBackups = oldMaxBackups
		c.viper.Set("logLevel", oldLevel)
		c.viper.Set("logPath", oldPath)
		c.viper.Set("logMaxSize", oldMaxSize)
		c.viper.Set("logMaxBackups", oldMaxBackups)
		// Best-effort restore of logger state (ApplyLogConfig may have partially
		// applied before failing, e.g., changed log level before path error)
		c.ApplyLogConfig() //nolint:errcheck // best-effort rollback, error is not actionable
	}()

	// Apply updates to in-memory config
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

	// Apply the new log configuration (validates paths work)
	if err := c.ApplyLogConfig(); err != nil {
		return LogSettingsResponse{}, fmt.Errorf("failed to apply log configuration: %w", err)
	}

	// Persist to config file only after successful apply
	if err := c.PersistLogSettings(c.Config.LogLevel, c.Config.LogPath, c.Config.LogMaxSize, c.Config.LogMaxBackups); err != nil {
		return LogSettingsResponse{}, fmt.Errorf("failed to persist settings: %w", err)
	}

	committed = true
	// Construct response inline to avoid deadlock (we already hold configMu)
	return LogSettingsResponse{
		Level:      canonicalizeLogLevel(c.Config.LogLevel),
		Path:       c.ResolveLogPath(c.Config.LogPath),
		MaxSize:    c.Config.LogMaxSize,
		MaxBackups: c.Config.LogMaxBackups,
		ConfigPath: c.viper.ConfigFileUsed(),
		Locked:     c.GetLockedLogSettings(),
	}, nil
}
