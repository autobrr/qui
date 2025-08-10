// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabasePathConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		configContent  string
		envVar         string
		expectedInPath string
	}{
		{
			name: "default_next_to_config",
			configContent: `
host = "localhost"
port = 8080
sessionSecret = "test-secret"`,
			expectedInPath: "qui.db",
		},
		{
			name: "explicit_in_config",
			configContent: `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
databasePath = "/custom/path.db"`,
			expectedInPath: "/custom/path.db",
		},
		{
			name: "env_var_override",
			configContent: `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
databasePath = "/config/path.db"`,
			envVar:         "/env/override.db",
			expectedInPath: "/env/override.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			err := os.WriteFile(configPath, []byte(tt.configContent), 0644)
			require.NoError(t, err)

			// Set env var if specified
			if tt.envVar != "" {
				os.Setenv("QUI__DATABASE_PATH", tt.envVar)
				defer os.Unsetenv("QUI__DATABASE_PATH")
			}

			// Create config
			cfg, err := New(configPath)
			require.NoError(t, err)

			// Check database path
			dbPath := cfg.GetDatabasePath()
			if filepath.IsAbs(tt.expectedInPath) {
				assert.Equal(t, tt.expectedInPath, dbPath)
			} else {
				assert.Contains(t, dbPath, tt.expectedInPath)
			}
		})
	}
}

func TestBackwardCompatibility(t *testing.T) {
	// Ensure existing configs work without databasePath
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	
	configContent := `
host = "localhost"
port = 8080
sessionSecret = "existing-secret"`
	
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := New(configPath)
	require.NoError(t, err)

	// Database should be next to config (old behavior)
	dbPath := cfg.GetDatabasePath()
	expectedPath := filepath.Join(tmpDir, "qui.db")
	assert.Equal(t, expectedPath, dbPath)
}

func TestEnvironmentVariablePrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	
	configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
databasePath = "/config/file/path.db"`
	
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Env var should override config
	os.Setenv("QUI__DATABASE_PATH", "/env/var/path.db")
	defer os.Unsetenv("QUI__DATABASE_PATH")

	cfg, err := New(configPath)
	require.NoError(t, err)
	
	assert.Equal(t, "/env/var/path.db", cfg.GetDatabasePath())
}