// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunGenerateConfigCommand(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		setupExistingFile  bool
		expectedError      bool
		validateOutput     func(t *testing.T, output string)
		validateConfigFile func(t *testing.T, configPath string)
	}{
		{
			name: "generate_config_default_location",
			args: []string{},
			validateOutput: func(t *testing.T, output string) {
				// Since a config might already exist, we check for either creation or exists message
				hasCreated := strings.Contains(output, "Configuration file created")
				hasExists := strings.Contains(output, "Configuration file already exists")
				assert.True(t, hasCreated || hasExists, "Expected either 'created' or 'already exists' message, got: %s", output)
				assert.Contains(t, output, "config.toml")
			},
			validateConfigFile: func(t *testing.T, configPath string) {
				// configPath will be the actual OS default location
				if _, err := os.Stat(configPath); err == nil {
					content, err := os.ReadFile(configPath)
					require.NoError(t, err)
					// Just verify it's a valid config file
					assert.NotEmpty(t, string(content))
				}
			},
		},
		{
			name: "generate_config_custom_directory",
			args: []string{"--config-dir", "custom/path"},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Configuration file created")
				assert.Contains(t, output, "custom/path/config.toml")
			},
			validateConfigFile: func(t *testing.T, configPath string) {
				assert.True(t, strings.HasSuffix(configPath, "custom/path/config.toml"))
				content, err := os.ReadFile(configPath)
				require.NoError(t, err)
				assert.Contains(t, string(content), "# config.toml")
			},
		},
		{
			name: "generate_config_custom_file",
			args: []string{"--config-dir", "custom/myconfig.toml"},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Configuration file created")
				assert.Contains(t, output, "custom/myconfig.toml")
			},
			validateConfigFile: func(t *testing.T, configPath string) {
				assert.True(t, strings.HasSuffix(configPath, "custom/myconfig.toml"))
				assert.Equal(t, "myconfig.toml", filepath.Base(configPath))
			},
		},
		{
			name:              "skip_existing_config",
			args:              []string{"--config-dir", "existing/path"},
			setupExistingFile: true,
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Configuration file already exists")
				assert.Contains(t, output, "existing/path/config.toml")
			},
			validateConfigFile: func(t *testing.T, configPath string) {
				content, err := os.ReadFile(configPath)
				require.NoError(t, err)
				// Should preserve existing content
				assert.Equal(t, "# Existing config content", string(content))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temp directory for test
			tmpDir := t.TempDir()
			originalWd, _ := os.Getwd()
			defer os.Chdir(originalWd)
			os.Chdir(tmpDir)

			// Setup existing file if needed
			if tt.setupExistingFile {
				existingPath := filepath.Join(tmpDir, "existing", "path", "config.toml")
				err := os.MkdirAll(filepath.Dir(existingPath), 0755)
				require.NoError(t, err)
				err = os.WriteFile(existingPath, []byte("# Existing config content"), 0644)
				require.NoError(t, err)
			}

			// Create command and capture output
			cmd := RunGenerateConfigCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			// Set args
			if len(tt.args) > 0 {
				cmd.SetArgs(tt.args)
			}

			// Execute command
			err := cmd.Execute()

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Validate output
				if tt.validateOutput != nil {
					tt.validateOutput(t, output.String())
				}

				// Determine expected config path
				var expectedConfigPath string
				if len(tt.args) >= 2 && tt.args[0] == "--config-dir" {
					if strings.HasSuffix(tt.args[1], ".toml") {
						expectedConfigPath = filepath.Join(tmpDir, tt.args[1])
					} else {
						expectedConfigPath = filepath.Join(tmpDir, tt.args[1], "config.toml")
					}
				} else {
					// For default location, we need to use the actual OS default dir
					// Since test runs in temp dir, this won't conflict with user's actual config
					// We'll extract the path from the output message
					outputStr := output.String()
					lines := strings.SplitSeq(outputStr, "\n")
					for line := range lines {
						if strings.Contains(line, "Configuration file created successfully at:") {
							expectedConfigPath = strings.TrimSpace(strings.Split(line, "at:")[1])
							break
						}
					}
				}

				// Validate config file
				if tt.validateConfigFile != nil {
					tt.validateConfigFile(t, expectedConfigPath)
				}
			}
		})
	}
}

func TestGenerateConfigCommandHelp(t *testing.T) {
	cmd := RunGenerateConfigCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	assert.NoError(t, err)

	helpOutput := output.String()
	assert.Contains(t, helpOutput, "Generate a default configuration file")
	assert.Contains(t, helpOutput, "--config-dir")
	assert.Contains(t, helpOutput, "OS-specific default location")
}

func TestGenerateConfigCommandValidation(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		expectedError string
	}{
		{
			name:          "invalid_config_dir_flag_without_value",
			args:          []string{"--config-dir"},
			expectedError: "flag needs an argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := RunGenerateConfigCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestGenerateConfigIntegrationWithRootCommand(t *testing.T) {
	// Test that the generate-config command is properly added to root
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tmpDir)

	// Create a minimal root command for testing
	rootCmd := &cobra.Command{
		Use:   "qui",
		Short: "Test root command",
	}

	// Add generate-config command
	rootCmd.AddCommand(RunGenerateConfigCommand())

	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	rootCmd.SetArgs([]string{"generate-config", "--help"})

	err := rootCmd.Execute()
	assert.NoError(t, err)

	helpOutput := output.String()
	assert.Contains(t, helpOutput, "generate-config")
	assert.Contains(t, helpOutput, "Generate a default configuration file")
}
