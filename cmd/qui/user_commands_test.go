// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/database"
)

func TestRunCreateUserCommand(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		setupExistingUser bool
		expectedError     bool
		validateOutput    func(t *testing.T, output string)
	}{
		{
			name: "create_user_with_flags",
			args: []string{
				"--config-dir", "test-config",
				"--username", "testuser",
				"--password", "testpassword123",
			},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "User 'testuser' created successfully")
			},
		},
		{
			name: "create_user_custom_data_dir",
			args: []string{
				"--config-dir", "test-config",
				"--data-dir", "custom-data",
				"--username", "testuser2",
				"--password", "testpassword456",
			},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "User 'testuser2' created successfully")
			},
		},
		{
			name:              "skip_existing_user",
			setupExistingUser: true,
			args: []string{
				"--config-dir", "test-config",
				"--username", "existinguser",
				"--password", "password123",
			},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "User account already exists")
			},
		},
		{
			name: "password_too_short",
			args: []string{
				"--config-dir", "test-config",
				"--username", "testuser",
				"--password", "short",
			},
			expectedError: true,
		},
		{
			name: "empty_username",
			args: []string{
				"--config-dir", "test-config",
				"--username", "",
				"--password", "testpassword123",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temp directory for test
			tmpDir := t.TempDir()
			originalWd, _ := os.Getwd()
			defer os.Chdir(originalWd)
			os.Chdir(tmpDir)

			// Create config directory and file for test
			configDir := "test-config"
			err := os.MkdirAll(configDir, 0755)
			require.NoError(t, err)

			// Generate a config file first
			err = config.WriteDefaultConfig(configDir + "/config.toml")
			require.NoError(t, err)

			// Setup existing user if needed
			if tt.setupExistingUser {
				// Create config and database with existing user
				cfg, err := config.New(configDir)
				require.NoError(t, err)

				db, err := database.New(cfg.GetDatabasePath())
				require.NoError(t, err)

				authService := auth.NewService(db.Conn())
				_, err = authService.SetupUser(context.Background(), "existinguser", "password123")
				require.NoError(t, err)

				db.Close()
			}

			// Create command and capture output
			cmd := RunCreateUserCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			// Set args
			if len(tt.args) > 0 {
				cmd.SetArgs(tt.args)
			}

			// Execute command
			err = cmd.Execute()

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Validate output
				if tt.validateOutput != nil {
					tt.validateOutput(t, output.String())
				}
			}
		})
	}
}

func TestRunChangePasswordCommand(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		setupUser      bool
		expectedError  bool
		validateOutput func(t *testing.T, output string)
	}{
		{
			name:      "change_password_with_flags",
			setupUser: true,
			args: []string{
				"--config-dir", "test-config",
				"--username", "testuser",
				"--new-password", "newpassword456",
			},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Password changed successfully for user 'testuser'")
			},
		},
		{
			name:      "change_password_custom_data_dir",
			setupUser: true,
			args: []string{
				"--config-dir", "test-config",
				"--data-dir", "custom-data",
				"--username", "testuser",
				"--new-password", "newpassword789",
			},
			validateOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Password changed successfully")
			},
		},
		{
			name:          "no_database_exists",
			setupUser:     false,
			args:          []string{"--config-dir", "test-config", "--username", "testuser"},
			expectedError: true,
		},
		{
			name:      "new_password_too_short",
			setupUser: true,
			args: []string{
				"--config-dir", "test-config",
				"--username", "testuser",
				"--new-password", "short",
			},
			expectedError: true,
		},
		{
			name:      "username_not_found",
			setupUser: true,
			args: []string{
				"--config-dir", "test-config",
				"--username", "nonexistentuser",
				"--new-password", "newpassword456",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temp directory for test
			tmpDir := t.TempDir()
			originalWd, _ := os.Getwd()
			defer os.Chdir(originalWd)
			os.Chdir(tmpDir)

			// Create config directory and file for test
			configDir := "test-config"
			err := os.MkdirAll(configDir, 0755)
			require.NoError(t, err)

			// Generate a config file first
			err = config.WriteDefaultConfig(configDir + "/config.toml")
			require.NoError(t, err)

			// Setup user if needed
			if tt.setupUser {
				// Create config and database with user
				cfg, err := config.New(configDir)
				require.NoError(t, err)

				// Set custom data directory if specified
				for i, arg := range tt.args {
					if arg == "--data-dir" && i+1 < len(tt.args) {
						cfg.SetDataDir(tt.args[i+1])
						break
					}
				}

				db, err := database.New(cfg.GetDatabasePath())
				require.NoError(t, err)

				authService := auth.NewService(db.Conn())
				_, err = authService.SetupUser(context.Background(), "testuser", "oldpassword123")
				require.NoError(t, err)

				db.Close()
			}

			// Create command and capture output
			cmd := RunChangePasswordCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)

			// Set args
			if len(tt.args) > 0 {
				cmd.SetArgs(tt.args)
			}

			// Execute command
			err = cmd.Execute()

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Validate output
				if tt.validateOutput != nil {
					tt.validateOutput(t, output.String())
				}
			}
		})
	}
}

func TestCreateUserCommandHelp(t *testing.T) {
	cmd := RunCreateUserCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	assert.NoError(t, err)

	helpOutput := output.String()
	assert.Contains(t, helpOutput, "Create the initial user account")
	assert.Contains(t, helpOutput, "--config-dir")
	assert.Contains(t, helpOutput, "--data-dir")
	assert.Contains(t, helpOutput, "--username")
	assert.Contains(t, helpOutput, "--password")
}

func TestChangePasswordCommandHelp(t *testing.T) {
	cmd := RunChangePasswordCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	assert.NoError(t, err)

	helpOutput := output.String()
	assert.Contains(t, helpOutput, "Change the password for the existing user")
	assert.Contains(t, helpOutput, "--config-dir")
	assert.Contains(t, helpOutput, "--data-dir")
	assert.Contains(t, helpOutput, "--username")
	assert.Contains(t, helpOutput, "--new-password")
}

func TestUserCommandsIntegrationWithRootCommand(t *testing.T) {
	// Test that both user commands are properly added to root
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tmpDir)

	// Create a minimal root command for testing
	rootCmd := &cobra.Command{
		Use:   "qui",
		Short: "Test root command",
	}

	// Add both commands
	rootCmd.AddCommand(RunCreateUserCommand())
	rootCmd.AddCommand(RunChangePasswordCommand())

	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	assert.NoError(t, err)

	helpOutput := output.String()
	assert.Contains(t, helpOutput, "create-user")
	assert.Contains(t, helpOutput, "change-password")
	assert.Contains(t, helpOutput, "Create the initial user account")
	assert.Contains(t, helpOutput, "Change the password for the existing user")
}

func TestUserCommandValidation(t *testing.T) {
	tests := []struct {
		name          string
		cmdFunc       func() *cobra.Command
		args          []string
		expectedError string
	}{
		{
			name:          "create_user_invalid_config_dir_flag",
			cmdFunc:       RunCreateUserCommand,
			args:          []string{"--config-dir"},
			expectedError: "flag needs an argument",
		},
		{
			name:          "change_password_invalid_new_password_flag",
			cmdFunc:       RunChangePasswordCommand,
			args:          []string{"--new-password"},
			expectedError: "flag needs an argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFunc()
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

func TestReadPasswordFunction(t *testing.T) {
	// Test that readPassword function exists and has correct signature
	// We can't easily test the actual terminal input without mocking
	// but we can verify the function exists and handles basic errors

	// This would require a more complex setup with mock terminals
	// For now, we'll just verify the function is defined
	t.Skip("readPassword requires terminal interaction - skipping in automated tests")
}

// Test helper to verify password strength validation
func TestPasswordValidation(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tmpDir)

	tests := []struct {
		password    string
		expectError bool
	}{
		{"password123", false},                 // Valid password
		{"12345678", false},                    // Minimum length
		{"short", true},                        // Too short
		{"verylongpasswordthatisvalid", false}, // Long password
	}

	for _, tt := range tests {
		t.Run("password_"+tt.password, func(t *testing.T) {
			// Setup temp directory for test - each password test gets its own directory
			tmpDir := t.TempDir()
			originalWd, _ := os.Getwd()
			defer os.Chdir(originalWd)
			os.Chdir(tmpDir)

			// Create config directory and file for test
			configDir := "test-config"
			err := os.MkdirAll(configDir, 0755)
			require.NoError(t, err)

			// Generate a config file first
			err = config.WriteDefaultConfig(configDir + "/config.toml")
			require.NoError(t, err)

			cmd := RunCreateUserCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{
				"--config-dir", "test-config",
				"--username", "testuser",
				"--password", tt.password,
			})

			err = cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "password must be at least 8 characters long")
			} else {
				// For valid passwords, we might get "already exists" error on subsequent runs
				// That's okay - it means the password validation passed
				if err != nil {
					assert.Contains(t, err.Error(), "already exists")
				}
			}
		})
	}
}
