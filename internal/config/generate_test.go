// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

// A commented log key falls back to the built-in default, so a changed default
// reaches installs that never edited the line.
func TestWriteDefaultConfigCommentsOutLogKeys(t *testing.T) {
	c := &AppConfig{viper: viper.New()}
	c.defaults()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := c.writeDefaultConfig(path); err != nil {
		t.Fatalf("writeDefaultConfig: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		assert.Empty(t, canonicalLogKey(extractKey(trimmed)), "log key must stay commented: %s", line)
	}
}

func TestGetDefaultConfigDirRespectsXDGConfigHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("APPDATA", "")

	dir := GetDefaultConfigDir()

	expected := filepath.Join(tmpDir, "qui")
	assert.Equal(t, filepath.Clean(expected), filepath.Clean(dir))
}

func TestGetDefaultConfigDirDockerPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/config")
	t.Setenv("APPDATA", "")

	dir := GetDefaultConfigDir()

	assert.Equal(t, "/config", dir)
}

func TestGetDefaultConfigDirFallsBackToOsDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")

	var expected string
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", tmpDir)
		expected = filepath.Join(tmpDir, "qui")
	} else {
		t.Setenv("APPDATA", "")
		t.Setenv("HOME", tmpDir)
		expected = filepath.Join(tmpDir, ".config", "qui")
	}

	dir := GetDefaultConfigDir()

	assert.Equal(t, filepath.Clean(expected), filepath.Clean(dir))
}
