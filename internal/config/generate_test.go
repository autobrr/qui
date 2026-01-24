// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
