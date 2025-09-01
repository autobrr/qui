// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDataDirConfiguration(t *testing.T) {
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
dataDir = "/custom/path"`,
			expectedInPath: filepath.ToSlash("/custom/path/qui.db"),
		},
		{
			name: "env_var_override",
			configContent: `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
dataDir = "/config/path"`,
			envVar:         "/env/override",
			expectedInPath: filepath.ToSlash("/env/override/qui.db"),
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
				os.Setenv(envPrefix+"DATA_DIR", tt.envVar)
				defer os.Unsetenv(envPrefix + "DATA_DIR")
			}

			// Create config
			cfg, err := New(configPath)
			require.NoError(t, err)

			// Check database path
			dbPath := cfg.GetDatabasePath()
			if strings.HasPrefix(tt.expectedInPath, "/") {
				// For Unix-style absolute paths, normalize both for comparison
				normalizedDbPath := filepath.ToSlash(dbPath)
				assert.Contains(t, normalizedDbPath, tt.expectedInPath)
			} else {
				assert.Contains(t, dbPath, tt.expectedInPath)
			}
		})
	}
}

func TestDataDirBackwardCompatibility(t *testing.T) {
	// Ensure existing configs work without dataDir
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
dataDir = "/config/file/path"`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Env var should override config
	os.Setenv(envPrefix+"DATA_DIR", "/env/var/path")
	defer os.Unsetenv(envPrefix + "DATA_DIR")

	cfg, err := New(configPath)
	require.NoError(t, err)

	assert.Equal(t, filepath.ToSlash("/env/var/path/qui.db"), filepath.ToSlash(cfg.GetDatabasePath()))
}

func TestGenerateSecureToken(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "standard_32_bytes",
			length: 32,
		},
		{
			name:   "small_token",
			length: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := generateSecureToken(tt.length)
			require.NoError(t, err)
			assert.NotEmpty(t, token)
			// Hex encoding produces 2 characters per byte
			assert.Len(t, token, tt.length*2)
		})
	}
}

func TestEncryptionKeySize(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
host = "localhost"
port = 8080
sessionSecret = "very-long-session-secret-that-is-over-32-bytes-long-for-testing"`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := New(configPath)
	require.NoError(t, err)

	key := cfg.GetEncryptionKey()
	assert.Len(t, key, encryptionKeySize)
	assert.Equal(t, 32, encryptionKeySize)
}

func TestConfigDirResolution(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		setupFile      bool
		fileIsDir      bool
		expectedSuffix string
	}{
		{
			name:           "toml_file_extension",
			input:          "/path/to/custom.toml",
			expectedSuffix: "custom.toml",
		},
		{
			name:           "TOML_file_extension_uppercase",
			input:          "/path/to/CONFIG.TOML",
			expectedSuffix: "CONFIG.TOML",
		},
		{
			name:           "directory_path",
			input:          "/path/to/config",
			expectedSuffix: "config.toml",
		},
		{
			name:           "existing_file_without_toml",
			input:          "/path/to/configfile",
			setupFile:      true,
			fileIsDir:      false,
			expectedSuffix: "configfile",
		},
		{
			name:           "existing_directory",
			input:          "/path/to/configdir",
			setupFile:      true,
			fileIsDir:      true,
			expectedSuffix: "config.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			inputPath := filepath.Join(tmpDir, filepath.Base(tt.input))

			if tt.setupFile {
				if tt.fileIsDir {
					err := os.MkdirAll(inputPath, 0755)
					require.NoError(t, err)
				} else {
					err := os.WriteFile(inputPath, []byte("test"), 0644)
					require.NoError(t, err)
				}
			}

			c := &AppConfig{}
			result := c.resolveConfigPath(inputPath)
			assert.True(t, strings.HasSuffix(result, tt.expectedSuffix),
				"Expected result %s to end with %s", result, tt.expectedSuffix)
		})
	}
}

func TestConfigDirBackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "myconfig.toml")

	configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Test with direct file path (old behavior)
	cfg, err := New(configPath)
	require.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Config.Host)
	assert.Equal(t, 8080, cfg.Config.Port)
}

func TestConfigDirNewBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
host = "0.0.0.0"
port = 9090
sessionSecret = "dir-test-secret"`

	err = os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Test with directory path (new behavior)
	cfg, err := New(configDir)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", cfg.Config.Host)
	assert.Equal(t, 9090, cfg.Config.Port)

	// Database should be in the same directory as the config
	dbPath := cfg.GetDatabasePath()
	expectedPath := filepath.Join(configDir, "qui.db")
	assert.Equal(t, expectedPath, dbPath)
}
