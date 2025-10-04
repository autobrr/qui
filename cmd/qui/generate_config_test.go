// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/config"
)

func TestGenerateConfigUsesDefaultDirectoryFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	output := runGenerateConfigCommand(t)

	expectedDir := config.GetDefaultConfigDir()
	expectedPath := filepath.Join(expectedDir, "config.toml")

	require.FileExists(t, expectedPath)

	content, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# config.toml - Auto-generated")

	normalizedOutput := filepath.ToSlash(output)
	assert.Contains(t, normalizedOutput, filepath.ToSlash(expectedPath))
}

func TestGenerateConfigWritesToProvidedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")

	output := runGenerateConfigCommand(t, "--config-dir", configDir)

	configPath := filepath.Join(configDir, "config.toml")
	require.FileExists(t, configPath)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# config.toml - Auto-generated")

	normalizedOutput := filepath.ToSlash(output)
	assert.Contains(t, normalizedOutput, filepath.ToSlash(configPath))
}

func TestGenerateConfigSkipsExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	configPath := filepath.Join(configDir, "config.toml")

	require.NoError(t, os.MkdirAll(configDir, 0o755))
	original := []byte("custom configuration")
	require.NoError(t, os.WriteFile(configPath, original, 0o644))

	output := runGenerateConfigCommand(t, "--config-dir", configDir)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(data))
	assert.Contains(t, output, "already exists")
	assert.Contains(t, output, "Skipping generation")
}

func runGenerateConfigCommand(t *testing.T, args ...string) string {
	t.Helper()

	cmd := RunGenerateConfigCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if len(args) > 0 {
		cmd.SetArgs(args)
	}

	require.NoError(t, cmd.Execute())

	return buf.String()
}
