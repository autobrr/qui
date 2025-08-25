// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteDefaultConfigPublic(t *testing.T) {
	tests := []struct {
		name            string
		existingFile    bool
		expectedError   bool
		validateContent func(t *testing.T, content string)
	}{
		{
			name:          "create_new_config",
			existingFile:  false,
			expectedError: false,
			validateContent: func(t *testing.T, content string) {
				// Check for essential config elements
				assert.Contains(t, content, "# config.toml")
				assert.Contains(t, content, "host =")
				assert.Contains(t, content, "port =")
				assert.Contains(t, content, "sessionSecret =")
				assert.Contains(t, content, "logLevel =")
				assert.Contains(t, content, "[httpTimeouts]")
			},
		},
		{
			name:          "skip_existing_config",
			existingFile:  true,
			expectedError: false,
			validateContent: func(t *testing.T, content string) {
				// Should not overwrite existing content
				assert.Equal(t, "existing content", content)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")

			if tt.existingFile {
				err := os.WriteFile(configPath, []byte("existing content"), 0644)
				require.NoError(t, err)
			}

			// Call the simplified public function
			err := WriteDefaultConfig(configPath)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Read and validate content
				content, err := os.ReadFile(configPath)
				require.NoError(t, err)
				tt.validateContent(t, string(content))
			}
		})
	}
}

func TestGetDefaultConfigDirPublic(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		envVars     map[string]string
		expectedDir string
	}{
		{
			name:        "linux_default",
			goos:        "linux",
			envVars:     map[string]string{},
			expectedDir: ".config/qui",
		},
		{
			name:        "macos_default",
			goos:        "darwin",
			envVars:     map[string]string{},
			expectedDir: ".config/qui",
		},
		{
			name:        "windows_default",
			goos:        "windows",
			envVars:     map[string]string{"APPDATA": "C:\\Users\\test\\AppData\\Roaming"},
			expectedDir: "C:\\Users\\test\\AppData\\Roaming\\qui",
		},
		{
			name:        "xdg_config_home_set",
			goos:        "linux",
			envVars:     map[string]string{"XDG_CONFIG_HOME": "/custom/config"},
			expectedDir: "/custom/config/qui",
		},
		{
			name:        "docker_config_path",
			goos:        "linux",
			envVars:     map[string]string{"XDG_CONFIG_HOME": "/config"},
			expectedDir: "/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore environment
			oldEnvVars := make(map[string]string)
			for key := range tt.envVars {
				oldEnvVars[key] = os.Getenv(key)
				os.Setenv(key, tt.envVars[key])
			}
			defer func() {
				for key, val := range oldEnvVars {
					if val == "" {
						os.Unsetenv(key)
					} else {
						os.Setenv(key, val)
					}
				}
			}()

			// Mock runtime.GOOS if needed
			if tt.goos != runtime.GOOS {
				t.Skip("Skipping test for different OS")
			}

			dir := GetDefaultConfigDir()
			if strings.Contains(tt.expectedDir, ".config") {
				assert.Contains(t, dir, tt.expectedDir)
			} else {
				assert.Equal(t, tt.expectedDir, dir)
			}
		})
	}
}

func TestConfigGenerationIntegration(t *testing.T) {
	t.Run("generate_config_in_custom_directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		configDir := filepath.Join(tmpDir, "custom", "config")

		// Ensure directory doesn't exist
		_, err := os.Stat(configDir)
		assert.True(t, os.IsNotExist(err))

		// Generate config in the directory (inline path resolution logic)
		configPath := filepath.Join(configDir, "config.toml")

		err = WriteDefaultConfig(configPath)
		assert.NoError(t, err)

		// Verify file was created
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())

		// Verify content
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "host =")
		assert.Contains(t, string(content), "sessionSecret =")
	})

	t.Run("generate_config_with_file_path", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "my-special-config.toml")

		// Generate config with direct file path
		err := WriteDefaultConfig(configPath)
		assert.NoError(t, err)

		// Verify file was created at exact path
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())
		assert.Equal(t, "my-special-config.toml", info.Name())
	})

	t.Run("prevent_overwrite_existing_config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		// Create existing config
		existingContent := "# Important existing config\nhost = \"production\""
		err := os.WriteFile(configPath, []byte(existingContent), 0644)
		require.NoError(t, err)

		// Try to generate config - should not overwrite
		err = WriteDefaultConfig(configPath)
		assert.NoError(t, err)

		// Verify original content preserved
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, existingContent, string(content))
	})
}

// Note: All functions are now implemented in config.go
