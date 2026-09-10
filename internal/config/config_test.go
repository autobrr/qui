// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))
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
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
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
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
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

func TestCustomThemesDirResolution(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, tmpDir string) (configPath string, env string, expected string)
	}{
		{
			name: "default_themes_subdir_of_config",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))
				return configPath, "", filepath.Join(tmpDir, "themes")
			},
		},
		{
			name: "absolute_override_in_config",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				themesDir := filepath.Join(tmpDir, "custom-themes")
				content := testConfigContent + fmt.Sprintf("customThemesDir = %q\n", themesDir)
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
				return configPath, "", themesDir
			},
		},
		{
			name: "relative_override_resolved_against_config_dir",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				content := testConfigContent + "customThemesDir = \"my-themes\"\n"
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
				return configPath, "", filepath.Join(tmpDir, "my-themes")
			},
		},
		{
			name: "env_override",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))
				envDir := filepath.Join(tmpDir, "env-themes")
				return configPath, envDir, envDir
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath, env, expected := tt.prepare(t, tmpDir)
			if env != "" {
				t.Setenv(envPrefix+"CUSTOM_THEMES_DIR", env)
			}

			cfg, err := New(configPath)
			require.NoError(t, err)

			assert.Equal(t, filepath.Clean(expected), filepath.Clean(cfg.GetCustomThemesDir()))
		})
	}
}

func TestBackupDirResolution(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, tmpDir string) (configPath string, env string, expected string)
	}{
		{
			name: "default_backups_subdir_of_data_dir",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				dataDir := filepath.Join(tmpDir, "data")
				content := testConfigContent + fmt.Sprintf("dataDir = %q\n", dataDir)
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
				return configPath, "", filepath.Join(dataDir, "backups")
			},
		},
		{
			name: "absolute_override_in_config",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				backupDir := filepath.Join(tmpDir, "backup-storage")
				content := testConfigContent + fmt.Sprintf("backupDir = %q\n", backupDir)
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
				return configPath, "", backupDir
			},
		},
		{
			name: "relative_override_resolved_against_config_dir",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				content := testConfigContent + "backupDir = \"my-backups\"\n"
				require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
				return configPath, "", filepath.Join(tmpDir, "my-backups")
			},
		},
		{
			name: "env_override",
			prepare: func(t *testing.T, tmpDir string) (string, string, string) {
				configPath := filepath.Join(tmpDir, "config.toml")
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))
				envDir := filepath.Join(tmpDir, "env-backups")
				return configPath, envDir, envDir
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath, env, expected := tt.prepare(t, tmpDir)
			if env != "" {
				t.Setenv(envPrefix+"BACKUP_DIR", env)
			}

			cfg, err := New(configPath)
			require.NoError(t, err)

			assert.Equal(t, filepath.Clean(expected), filepath.Clean(cfg.GetBackupDir()))
		})
	}
}

func TestEnsureCustomThemesDirCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))

	cfg, err := New(configPath)
	require.NoError(t, err)

	dir, err := cfg.EnsureCustomThemesDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(filepath.Join(tmpDir, "themes")), filepath.Clean(dir))

	info, statErr := os.Stat(dir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
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
		{name: "long_secret", secret: strings.Repeat("a", encryptionKeySize+8)},
		{name: "short_secret", secret: "short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AppConfig{Config: &domain.Config{SessionSecret: tt.secret}}

			key := cfg.GetEncryptionKey()
			require.Len(t, key, encryptionKeySize)
			assert.Equal(t, key, cfg.GetEncryptionKey(), "derivation must be deterministic")
			assert.NotEqual(t, cfg.GetLegacyEncryptionKey(), key, "derived key must differ from the truncated secret")
		})
	}

	t.Run("distinguishes_secrets_sharing_a_prefix", func(t *testing.T) {
		prefix := strings.Repeat("a", encryptionKeySize)
		first := &AppConfig{Config: &domain.Config{SessionSecret: prefix + "one"}}
		second := &AppConfig{Config: &domain.Config{SessionSecret: prefix + "two"}}

		assert.NotEqual(t, first.GetEncryptionKey(), second.GetEncryptionKey())
		assert.Equal(t, first.GetLegacyEncryptionKey(), second.GetLegacyEncryptionKey(), "the legacy key only saw the shared prefix")
	})
}

func TestSessionSecretRejectsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		// wantGenerated asserts the default 64-character hex secret survived.
		wantGenerated bool
	}{
		{
			name:    "explicit_empty",
			content: "host = \"localhost\"\nsessionSecret = \"\"\n",
			wantErr: true,
		},
		{
			name:    "whitespace_only",
			content: "host = \"localhost\"\nsessionSecret = \"   \\t \"\n",
			wantErr: true,
		},
		{
			name:          "absent_key_keeps_the_generated_default",
			content:       "host = \"localhost\"\nport = 8080\n",
			wantGenerated: true,
		},
		{
			name:    "short_secret_is_accepted_with_a_warning",
			content: "host = \"localhost\"\nsessionSecret = \"short\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tt.content), 0o600))

			cfg, err := New(dir)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "sessionSecret is empty")
				assert.Nil(t, cfg)
				return
			}

			require.NoError(t, err)
			if tt.wantGenerated {
				assert.Len(t, cfg.Config.SessionSecret, encryptionKeySize*2, "the generated default is hex of 32 random bytes")
				return
			}
			assert.Equal(t, "short", cfg.Config.SessionSecret)
		})
	}

	// An empty QUI__SESSION_SECRET reads as unset, because viper's AllowEmptyEnv
	// is off, so the file value or the generated default still applies. Empty
	// therefore never becomes the key from this source either.
	t.Run("empty_env_var_reads_as_unset", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte("sessionSecret = \"from-the-file-abcdefghijklmnop\"\n"), 0o600))
		t.Setenv("QUI__SESSION_SECRET", "")

		cfg, err := New(dir)
		require.NoError(t, err)
		assert.Equal(t, "from-the-file-abcdefghijklmnop", cfg.Config.SessionSecret)
	})
}

// TestSessionSecretFileRejectsEmptyFile covers the _FILE source.
// bindOrReadFromFile refuses an empty file with log.Fatal, so this needs a
// subprocess.
func TestSessionSecretFileRejectsEmptyFile(t *testing.T) {
	if os.Getenv("QUI_TEST_EMPTY_SECRET_FILE") == "1" {
		dir := t.TempDir()
		secretFile := filepath.Join(dir, "secret")
		require.NoError(t, os.WriteFile(secretFile, []byte("   \n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte("host = \"localhost\"\n"), 0o600))
		t.Setenv("QUI__SESSION_SECRET_FILE", secretFile)

		_, _ = New(dir)
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSessionSecretFileRejectsEmptyFile$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "QUI_TEST_EMPTY_SECRET_FILE=1")

	output, err := cmd.CombinedOutput()
	require.Error(t, err, "an empty secret file must not load: %s", output)
	assert.Contains(t, string(output), "file is empty")
}

// TestGetEncryptionKeyGoldenVector freezes the hash, the nil salt and the info
// string together. A row sealed under a different derivation still carries the
// qui2 prefix, so the rewrite pass skips it and the legacy key does not apply.
// Every stored credential then becomes permanently unreadable, with no warning.
// Changing this constant means changing that contract, not fixing a test.
func TestGetEncryptionKeyGoldenVector(t *testing.T) {
	cfg := &AppConfig{Config: &domain.Config{SessionSecret: "qui-golden-vector-session-secret"}}

	assert.Equal(t, "8ced9a614da47fa9d7868174b649cba66c236a66b2866801cd2f0af68423f531", hex.EncodeToString(cfg.GetEncryptionKey()))
}

func TestGetLegacyEncryptionKey(t *testing.T) {
	tests := []struct {
		name     string
		secret   string
		expected []byte
	}{
		{
			name:     "truncates_long_secret",
			secret:   strings.Repeat("a", encryptionKeySize+8),
			expected: []byte(strings.Repeat("a", encryptionKeySize)),
		},
		{
			name:     "pads_short_secret",
			secret:   "short",
			expected: append([]byte("short"), make([]byte, encryptionKeySize-len("short"))...),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AppConfig{Config: &domain.Config{SessionSecret: tt.secret}}

			key := cfg.GetLegacyEncryptionKey()
			require.Len(t, key, encryptionKeySize)
			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestWarnWeakSessionSecret(t *testing.T) {
	tests := []struct {
		name       string
		secret     string
		expectWarn bool
	}{
		{name: "short_secret_warns", secret: "short", expectWarn: true},
		{name: "long_secret_stays_quiet", secret: strings.Repeat("a", encryptionKeySize), expectWarn: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs strings.Builder
			previous := log.Logger
			log.Logger = zerolog.New(&logs)
			t.Cleanup(func() { log.Logger = previous })

			cfg := &AppConfig{Config: &domain.Config{SessionSecret: tt.secret}}
			cfg.warnWeakSessionSecret()

			if tt.expectWarn {
				assert.Contains(t, logs.String(), "sessionSecret is shorter than 32 characters")
				assert.Contains(t, logs.String(), "On a new install set at least 32 characters", "the warning has to name a path forward")
				return
			}
			assert.Empty(t, logs.String())
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
					err := os.WriteFile(inputPath, []byte("test"), 0o600)
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
				require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))
				return configPath, "localhost", 8080, filepath.Join(tmpDir, "qui.db")
			},
		},
		{
			name: "config_directory_path",
			prepare: func(t *testing.T, tmpDir string) (string, string, int, string) {
				configDir := filepath.Join(tmpDir, "configdir")
				require.NoError(t, os.MkdirAll(configDir, 0o755))
				content := "host = \"0.0.0.0\"\nport = 9090\nsessionSecret = \"dir-secret\"\n"
				require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(content), 0o600))
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
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
		return configPath
	}

	tmpKeyFileWithNewline := func(t *testing.T, tmpDir string) string {
		configPath := filepath.Join(tmpDir, "key-file.txt")
		content := "key-from-file\n"
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
		return configPath
	}

	noTmpKeyFile := func(t *testing.T, tmpDir string) string {
		return ""
	}

	genConfigFile := func(t *testing.T, tmpDir string) string {
		configPath := filepath.Join(tmpDir, "myconfig.toml")
		require.NoError(t, os.WriteFile(configPath, []byte(testConfigContent), 0o600))
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

	var listenerCalls atomic.Int32
	cfg.RegisterReloadListener(func(_ *domain.Config) {
		listenerCalls.Add(1)
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
	assert.Equal(t, int32(0), listenerCalls.Load())
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

	var listenerCalls atomic.Int32
	cfg.RegisterReloadListener(func(conf *domain.Config) {
		listenerCalls.Add(1)
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
	assert.Equal(t, int32(1), listenerCalls.Load())
	assert.Equal(t, zerolog.ErrorLevel, zerolog.GlobalLevel())
}

func TestApplyDynamicChangesRejectsInvalidCORSReload(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLevel)
	})

	cfg := &AppConfig{
		Config: &domain.Config{
			LogLevel:                 "info",
			CORSAllowedOrigins:       []string{"https://good.example"},
			AuthDisabledAllowedCIDRs: []string{},
		},
		version:    "test",
		logManager: NewLogManager("test"),
	}

	var listenerCalls atomic.Int32
	cfg.RegisterReloadListener(func(conf *domain.Config) {
		listenerCalls.Add(1)
		assert.Equal(t, []string{"https://good.example"}, conf.CORSAllowedOrigins)
	})

	previous := authReloadSettings{
		corsAllowedOrigins: []string{"https://good.example"},
	}

	cfg.Config.CORSAllowedOrigins = []string{"https://*.example.com"}
	cfg.applyDynamicChanges(previous)

	assert.Equal(t, []string{"https://good.example"}, cfg.Config.CORSAllowedOrigins)
	assert.Equal(t, int32(1), listenerCalls.Load())
}

func TestApplyDynamicChangesRejectsInvalidAuthDisabledReloadAlsoRestoresCORS(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLevel)
	})

	cfg := &AppConfig{
		Config: &domain.Config{
			LogLevel:                   "warn",
			AuthDisabled:               true,
			IAcknowledgeThisIsABadIdea: true,
			AuthDisabledAllowedCIDRs:   nil, // invalid when auth is disabled
			CORSAllowedOrigins:         []string{"https://*.example.com"},
		},
		version:    "test",
		logManager: NewLogManager("test"),
	}

	var listenerCalls atomic.Int32
	cfg.RegisterReloadListener(func(_ *domain.Config) {
		listenerCalls.Add(1)
	})

	previous := authReloadSettings{
		authDisabled:               false,
		iAcknowledgeThisIsABadIdea: false,
		authDisabledAllowedCIDRs:   nil,
		oidcEnabled:                false,
		corsAllowedOrigins:         []string{"https://good.example"},
	}

	cfg.applyDynamicChanges(previous)

	assert.False(t, cfg.Config.AuthDisabled)
	assert.False(t, cfg.Config.IAcknowledgeThisIsABadIdea)
	assert.Nil(t, cfg.Config.AuthDisabledAllowedCIDRs)
	assert.False(t, cfg.Config.OIDCEnabled)
	assert.Equal(t, []string{"https://good.example"}, cfg.Config.CORSAllowedOrigins)
	assert.Equal(t, int32(0), listenerCalls.Load())
}

func TestHydrateConfigFromViperSplitsStringSlices(t *testing.T) {
	tests := []struct {
		name                    string
		authDisabledCIDRsValue  any
		corsAllowedOriginsValue any
		externalAllowListValue  any
		wantAuthDisabledCIDRs   []string
		wantCORSAllowedOrigins  []string
		wantExternalProgramList []string
	}{
		{
			name:                    "splits comma separated values",
			authDisabledCIDRsValue:  "127.0.0.1/32, 192.168.1.0/24",
			corsAllowedOriginsValue: "https://a.example, https://b.example",
			externalAllowListValue:  "/usr/local/bin/a, /usr/local/bin/b",
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32", "192.168.1.0/24"},
			wantCORSAllowedOrigins:  []string{"https://a.example", "https://b.example"},
			wantExternalProgramList: []string{"/usr/local/bin/a", "/usr/local/bin/b"},
		},
		{
			name:                    "splits whitespace separated values",
			authDisabledCIDRsValue:  "127.0.0.1/32 192.168.1.0/24",
			corsAllowedOriginsValue: "https://a.example https://b.example",
			externalAllowListValue:  "/usr/local/bin/a /usr/local/bin/b",
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32", "192.168.1.0/24"},
			wantCORSAllowedOrigins:  []string{"https://a.example", "https://b.example"},
			wantExternalProgramList: []string{"/usr/local/bin/a", "/usr/local/bin/b"},
		},
		{
			name:                    "trims and drops empty values",
			authDisabledCIDRsValue:  " , 127.0.0.1/32,,   ",
			corsAllowedOriginsValue: " , https://a.example,,   ",
			externalAllowListValue:  "   ",
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32"},
			wantCORSAllowedOrigins:  []string{"https://a.example"},
			wantExternalProgramList: nil,
		},
		{
			name:                    "preserves list values from config",
			authDisabledCIDRsValue:  []string{" 127.0.0.1/32 ", "", "192.168.1.0/24"},
			corsAllowedOriginsValue: []any{" https://a.example ", "", "https://b.example"},
			externalAllowListValue:  []any{" /usr/local/bin/a ", "", "/usr/local/bin/b"},
			wantAuthDisabledCIDRs:   []string{"127.0.0.1/32", "192.168.1.0/24"},
			wantCORSAllowedOrigins:  []string{"https://a.example", "https://b.example"},
			wantExternalProgramList: []string{"/usr/local/bin/a", "/usr/local/bin/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			v.Set("authDisabledAllowedCIDRs", tt.authDisabledCIDRsValue)
			v.Set("corsAllowedOrigins", tt.corsAllowedOriginsValue)
			v.Set("externalProgramAllowList", tt.externalAllowListValue)

			cfg := &AppConfig{
				Config: &domain.Config{},
				viper:  v,
			}

			cfg.hydrateConfigFromViper()

			assert.Equal(t, tt.wantAuthDisabledCIDRs, cfg.Config.AuthDisabledAllowedCIDRs)
			assert.Equal(t, tt.wantCORSAllowedOrigins, cfg.Config.CORSAllowedOrigins)
			assert.Equal(t, tt.wantExternalProgramList, cfg.Config.ExternalProgramAllowList)
		})
	}
}

func TestBaseURLNormalization(t *testing.T) {
	tests := map[string]string{
		"":          "/",
		"/":         "/",
		"/qui/":     "/qui/",
		"/qui":      "/qui/",
		"qui":       "/qui/",
		"qui/":      "/qui/",
		" /qui ":    "/qui/", //nolint:gocritic // the whitespace is the input under test
		"/apps/qui": "/apps/qui/",
	}

	for input, want := range tests {
		v := viper.New()
		v.Set("baseUrl", input)
		cfg := &AppConfig{Config: &domain.Config{}, viper: v}

		cfg.hydrateConfigFromViper()

		assert.Equal(t, want, cfg.Config.BaseURL, "input %q", input)
	}
}
