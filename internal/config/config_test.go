// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
)

const testConfigContent = "host = \"localhost\"\nport = 8080\nsessionSecret = \"test-secret\"\n"

func TestDatabasePathResolution(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, tmpDir string) (configPath string, envDataDir string, expectedDBPath string)
	}{
		{
			name: "default_next_to_config",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o644))
				return configPath, "", filepath.Join(tmpDir, "qui.db")
			},
		},
		{
			name: "explicit_data_dir_in_config",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				dataDir := filepath.Join(tmpDir, "data")
				require.NoError(t, os.MkdirAll(dataDir, 0o755))
				content := testConfigContent + fmt.Sprintf("dataDir = %q\n", dataDir)
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
				return configPath, "", filepath.Join(dataDir, "qui.db")
			},
		},
		{
			name: "env_var_override",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				configDataDir := filepath.Join(tmpDir, "config-data")
				envDataDir := filepath.Join(tmpDir, "env-data")
				require.NoError(t, os.MkdirAll(configDataDir, 0o755))
				require.NoError(t, os.MkdirAll(envDataDir, 0o755))
				content := testConfigContent + fmt.Sprintf("dataDir = %q\n", configDataDir)
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
				return configPath, envDataDir, filepath.Join(envDataDir, "qui.db")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath, envValue, expectedDBPath := tt.prepare(t, tmpDir)
			if envValue != "" {
				t.Setenv(envPrefix+"DATA_DIR", envValue)
			}

			cfg, err := New(configPath)
			require.NoError(t, err)

			assert.Equal(t, filepath.Clean(expectedDBPath), filepath.Clean(cfg.GetDatabasePath()))
		})
	}
}

func TestGenerateSecureTokenHexOutput(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{name: "standard_32_bytes", length: 32},
		{name: "small_token", length: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := generateSecureToken(tt.length)
			require.NoError(t, err)
			require.NotEmpty(t, token)

			assert.Len(t, token, tt.length*2)
			_, err = hex.DecodeString(token)
			require.NoError(t, err)
		})
	}
}

func TestGetEncryptionKey(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{name: "truncates_long_secret", secret: strings.Repeat("a", encryptionKeySize+8)},
		{name: "pads_short_secret", secret: "short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AppConfig{Config: &domain.Config{SessionSecret: tt.secret}}

			key := cfg.GetEncryptionKey()
			require.Len(t, key, encryptionKeySize)

			if len(tt.secret) >= encryptionKeySize {
				assert.Equal(t, []byte(tt.secret[:encryptionKeySize]), key)
			} else {
				expected := make([]byte, encryptionKeySize)
				copy(expected, tt.secret)
				assert.Equal(t, expected, key)
			}
		})
	}
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
					err := os.MkdirAll(inputPath, 0o755)
					require.NoError(t, err)
				} else {
					err := os.WriteFile(inputPath, []byte("test"), 0o644)
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

func TestNewLoadsConfigFromFileOrDirectory(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, tmpDir string) (inputPath string, expectedHost string, expectedPort int, expectedDBPath string)
	}{
		{
			name: "config_file_path",
			prepare: func(t *testing.T, tmpDir string) (string, string, int, string) {
				configPath := filepath.Join(tmpDir, "myconfig.toml")
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o644))
				return configPath, "localhost", 8080, filepath.Join(tmpDir, "qui.db")
			},
		},
		{
			name: "config_directory_path",
			prepare: func(t *testing.T, tmpDir string) (string, string, int, string) {
				configDir := filepath.Join(tmpDir, "configdir")
				require.NoError(t, os.MkdirAll(configDir, 0o755))
				content := "host = \"0.0.0.0\"\nport = 9090\nsessionSecret = \"dir-secret\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(content), 0o644))
				return configDir, "0.0.0.0", 9090, filepath.Join(configDir, "qui.db")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			inputPath, expectedHost, expectedPort, expectedDBPath := tt.prepare(t, tmpDir)

			cfg, err := New(inputPath)
			require.NoError(t, err)

			assert.Equal(t, expectedHost, cfg.Config.Host)
			assert.Equal(t, expectedPort, cfg.Config.Port)
			assert.Equal(t, filepath.Clean(expectedDBPath), filepath.Clean(cfg.GetDatabasePath()))
		})
	}
}

func TestBindOrReadFromFile(t *testing.T) {
	tmpKeyFile := func(t *testing.T, tmpDir string) string {
		configPath := filepath.Join(tmpDir, "key-file.txt")
		content := "key-from-file"
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
		return configPath
	}

	tmpKeyFileWithNewline := func(t *testing.T, tmpDir string) string {
		configPath := filepath.Join(tmpDir, "key-file.txt")
		content := "key-from-file\n"
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
		return configPath
	}

	noTmpKeyFile := func(t *testing.T, tmpDir string) string {
		return ""
	}

	genConfigFile := func(t *testing.T, tmpDir string) string {
		configPath := filepath.Join(tmpDir, "myconfig.toml")
		require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o644))
		return configPath
	}

	tests := []struct {
		name            string
		envVarValue     string
		envVarFileValue func(t *testing.T, tmpDir string) string
		expectedValue   string
	}{
		{
			name:            "Only _FILE env var",
			envVarValue:     "",
			envVarFileValue: tmpKeyFile,
			expectedValue:   "key-from-file",
		},
		{
			name:            "Only normal env var",
			envVarValue:     "key-not-from-file",
			envVarFileValue: noTmpKeyFile,
			expectedValue:   "key-not-from-file",
		},
		{
			name:            "_FILE takes precedence over env var",
			envVarValue:     "key-not-from-file",
			envVarFileValue: tmpKeyFile,
			expectedValue:   "key-from-file",
		},
		{
			name:            "File with trailing newline is trimmed",
			envVarValue:     "",
			envVarFileValue: tmpKeyFileWithNewline,
			expectedValue:   "key-from-file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVar := envPrefix + "SESSION_SECRET"

			if tt.envVarValue != "" {
				t.Setenv(envVar, tt.envVarValue)
			}

			envVarFilePath := tt.envVarFileValue(t, t.TempDir())
			if envVarFilePath != "" {
				t.Setenv(envVar+"_FILE", envVarFilePath)
			}

			configPath := genConfigFile(t, t.TempDir())
			cfg, err := New(configPath)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, cfg.Config.SessionSecret)
		})
	}
}

func TestApplyDynamicChangesRejectsInvalidAuthDisabledReload(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLevel)
	})

	cfg := &AppConfig{
		Config: &domain.Config{
			LogLevel:                   "warn",
			AuthDisabled:               true,
			IAcknowledgeThisIsABadIdea: true,
			AuthDisabledAllowedCIDRs:   []string{"127.0.0.1/32"},
			OIDCEnabled:                true, // invalid with auth-disabled
		},
		version:    "test",
		logManager: NewLogManager("test"),
	}

	var listenerCalls int32
	cfg.RegisterReloadListener(func(_ *domain.Config) {
		atomic.AddInt32(&listenerCalls, 1)
	})

	previousAuth := authReloadSettings{
		authDisabled:               true,
		iAcknowledgeThisIsABadIdea: true,
		authDisabledAllowedCIDRs:   []string{"127.0.0.1/32"},
		oidcEnabled:                false,
	}

	cfg.applyDynamicChanges(previousAuth)

	assert.Equal(t, "test", cfg.Config.Version)
	assert.True(t, cfg.Config.AuthDisabled)
	assert.True(t, cfg.Config.IAcknowledgeThisIsABadIdea)
	assert.Equal(t, []string{"127.0.0.1/32"}, cfg.Config.AuthDisabledAllowedCIDRs)
	assert.False(t, cfg.Config.OIDCEnabled)
	assert.Equal(t, int32(0), atomic.LoadInt32(&listenerCalls))
	assert.Equal(t, zerolog.WarnLevel, zerolog.GlobalLevel())
	require.NoError(t, cfg.Config.ValidateAuthDisabledConfig())
}

func TestApplyDynamicChangesNotifiesOnValidAuthDisabledReload(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLevel)
	})

	cfg := &AppConfig{
		Config: &domain.Config{
			LogLevel:                   "error",
			AuthDisabled:               true,
			IAcknowledgeThisIsABadIdea: true,
			AuthDisabledAllowedCIDRs:   []string{"10.0.0.0/8"},
		},
		version:    "test",
		logManager: NewLogManager("test"),
	}

	var listenerCalls int32
	cfg.RegisterReloadListener(func(conf *domain.Config) {
		atomic.AddInt32(&listenerCalls, 1)
		assert.True(t, conf.IsAuthDisabled())
		assert.Equal(t, []string{"10.0.0.0/8"}, conf.AuthDisabledAllowedCIDRs)
	})

	previousAuth := authReloadSettings{
		authDisabled:               false,
		iAcknowledgeThisIsABadIdea: false,
		authDisabledAllowedCIDRs:   nil,
		oidcEnabled:                false,
	}

	cfg.applyDynamicChanges(previousAuth)

	assert.Equal(t, "test", cfg.Config.Version)
	assert.Equal(t, int32(1), atomic.LoadInt32(&listenerCalls))
	assert.Equal(t, zerolog.ErrorLevel, zerolog.GlobalLevel())
}

func TestHydrateConfigFromViperSplitsStringSlices(t *testing.T) {
	tests := []struct {
		name                    string
		authDisabledCIDRsValue  any
		externalAllowListValue  any
		wantAuthDisabledCIDRs   []string
		wantExternalProgramList []string
	}{
		{
			name:                    "splits comma separated values",
			authDisabledCIDRsValue:  "127.0.0.1/32, 192.168.1.0/24",
			externalAllowListValue:  "/usr/local/bin/a, /usr/local/bin/b",
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32", "192.168.1.0/24"},
			wantExternalProgramList: []string{"/usr/local/bin/a", "/usr/local/bin/b"},
		},
		{
			name:                    "splits whitespace separated values",
			authDisabledCIDRsValue:  "127.0.0.1/32 192.168.1.0/24",
			externalAllowListValue:  "/usr/local/bin/a /usr/local/bin/b",
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32", "192.168.1.0/24"},
			wantExternalProgramList: []string{"/usr/local/bin/a", "/usr/local/bin/b"},
		},
		{
			name:                    "trims and drops empty values",
			authDisabledCIDRsValue:  " , 127.0.0.1/32,,   ",
			externalAllowListValue:  "   ",
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32"},
			wantExternalProgramList: nil,
		},
		{
			name:                    "preserves list values from config",
			authDisabledCIDRsValue:  []string{" 127.0.0.1/32 ", "", "192.168.1.0/24"},
			externalAllowListValue:  []any{" /usr/local/bin/a ", "", "/usr/local/bin/b"},
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32", "192.168.1.0/24"},
			wantExternalProgramList: []string{"/usr/local/bin/a", "/usr/local/bin/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			v.Set("authDisabledAllowedCIDRs", tt.authDisabledCIDRsValue)
			v.Set("externalProgramAllowList", tt.externalAllowListValue)

			cfg := &AppConfig{
				Config: &domain.Config{},
				viper:  v,
			}

			cfg.hydrateConfigFromViper()

			assert.Equal(t, tt.wantAuthDisabledCIDRs, cfg.Config.AuthDisabledAllowedCIDRs)
			assert.Equal(t, tt.wantExternalProgramList, cfg.Config.ExternalProgramAllowList)
		})
	}
}
