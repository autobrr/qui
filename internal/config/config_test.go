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
		setupFunc      func(t *testing.T) (configPath string, cleanup func())
		envVars        map[string]string
		expectedDBPath string
		description    string
	}{
		{
			name: "default_behavior_db_next_to_config",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.toml")
				configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
`
				err := os.WriteFile(configPath, []byte(configContent), 0644)
				require.NoError(t, err)
				return configPath, func() {}
			},
			envVars:        map[string]string{},
			expectedDBPath: "qui.db", // Will be next to config file
			description:    "Database should be created next to config file when not explicitly configured",
		},
		{
			name: "explicit_path_in_config",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				dbDir := filepath.Join(tmpDir, "database")
				err := os.MkdirAll(dbDir, 0755)
				require.NoError(t, err)
				
				configPath := filepath.Join(tmpDir, "config.toml")
				configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
databasePath = "` + filepath.Join(dbDir, "custom.db") + `"
`
				err = os.WriteFile(configPath, []byte(configContent), 0644)
				require.NoError(t, err)
				return configPath, func() {}
			},
			envVars:        map[string]string{},
			expectedDBPath: "custom.db",
			description:    "Database path should use explicitly configured path from config file",
		},
		{
			name: "explicit_path_via_env_var",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.toml")
				configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
`
				err := os.WriteFile(configPath, []byte(configContent), 0644)
				require.NoError(t, err)
				return configPath, func() {}
			},
			envVars: map[string]string{
				"QUI__DATABASE_PATH": "/var/db/qui/qui.db",
			},
			expectedDBPath: "/var/db/qui/qui.db",
			description:    "Database path should use environment variable when set",
		},
		{
			name: "env_var_overrides_config",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.toml")
				configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
databasePath = "/original/path.db"
`
				err := os.WriteFile(configPath, []byte(configContent), 0644)
				require.NoError(t, err)
				return configPath, func() {}
			},
			envVars: map[string]string{
				"QUI__DATABASE_PATH": "/override/path.db",
			},
			expectedDBPath: "/override/path.db",
			description:    "Environment variable should override config file setting",
		},
		{
			name: "docker_scenario_xdg_config",
			setupFunc: func(t *testing.T) (string, func()) {
				// Simulate Docker environment where XDG_CONFIG_HOME=/config
				tmpDir := t.TempDir()
				configDir := filepath.Join(tmpDir, "config")
				err := os.MkdirAll(configDir, 0755)
				require.NoError(t, err)
				
				// Set XDG_CONFIG_HOME like Docker does
				oldXDG := os.Getenv("XDG_CONFIG_HOME")
				os.Setenv("XDG_CONFIG_HOME", configDir)
				
				configPath := filepath.Join(configDir, "config.toml")
				configContent := `
host = "0.0.0.0"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
`
				err = os.WriteFile(configPath, []byte(configContent), 0644)
				require.NoError(t, err)
				
				return configPath, func() {
					if oldXDG != "" {
						os.Setenv("XDG_CONFIG_HOME", oldXDG)
					} else {
						os.Unsetenv("XDG_CONFIG_HOME")
					}
				}
			},
			envVars:        map[string]string{},
			expectedDBPath: "qui.db",
			description:    "Docker setup should work with database next to config in /config",
		},
		{
			name: "readonly_config_writable_db",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				
				// Simulate /etc for config (read-only in real scenario)
				etcDir := filepath.Join(tmpDir, "etc", "qui")
				err := os.MkdirAll(etcDir, 0755)
				require.NoError(t, err)
				
				// Simulate /var/db for database (writable)
				varDbDir := filepath.Join(tmpDir, "var", "db", "qui")
				err = os.MkdirAll(varDbDir, 0755)
				require.NoError(t, err)
				
				configPath := filepath.Join(etcDir, "config.toml")
				configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
databasePath = "` + filepath.Join(varDbDir, "qui.db") + `"
logPath = "` + filepath.Join(tmpDir, "var", "log", "qui.log") + `"
`
				err = os.WriteFile(configPath, []byte(configContent), 0644)
				require.NoError(t, err)
				
				return configPath, func() {}
			},
			envVars:        map[string]string{},
			expectedDBPath: "qui.db",
			description:    "Should support read-only config directory with writable database path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			configPath, cleanup := tt.setupFunc(t)
			defer cleanup()

			// Set environment variables
			for k, v := range tt.envVars {
				oldVal := os.Getenv(k)
				os.Setenv(k, v)
				defer func(key, val string) {
					if val != "" {
						os.Setenv(key, val)
					} else {
						os.Unsetenv(key)
					}
				}(k, oldVal)
			}

			// Create config
			cfg, err := New(configPath)
			require.NoError(t, err, tt.description)
			require.NotNil(t, cfg)

			// Check database path
			dbPath := cfg.GetDatabasePath()
			assert.Contains(t, dbPath, tt.expectedDBPath, tt.description)
			
			// Verify the path is absolute or relative as expected
			if filepath.IsAbs(tt.expectedDBPath) {
				assert.True(t, filepath.IsAbs(dbPath), "Expected absolute path")
			}
		})
	}
}

func TestDatabasePathBackwardCompatibility(t *testing.T) {
	// Test that existing deployments continue to work without any changes
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	
	// Minimal config without databasePath (like existing deployments)
	configContent := `
host = "localhost"
port = 8080
sessionSecret = "existing-secret"
logLevel = "INFO"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := New(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Database should be next to config file (old behavior)
	dbPath := cfg.GetDatabasePath()
	expectedPath := filepath.Join(tmpDir, "qui.db")
	assert.Equal(t, expectedPath, dbPath, "Backward compatibility: database should be next to config file")
}

func TestDockerEnvironmentCompatibility(t *testing.T) {
	// Test that Docker environment with XDG_CONFIG_HOME=/config works correctly
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Save and set XDG_CONFIG_HOME
	oldXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", "/config")
	defer func() {
		if oldXDG != "" {
			os.Setenv("XDG_CONFIG_HOME", oldXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()

	// Test getDefaultConfigDir function behavior
	appConfig := &AppConfig{}
	_ = appConfig
	
	// In Docker, XDG_CONFIG_HOME=/config should return /config directly
	defaultDir := getDefaultConfigDir()
	assert.Equal(t, "/config", defaultDir, "Docker environment should use /config directly")
}

func TestConfigPrecedence(t *testing.T) {
	// Test that environment variables take precedence over config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	
	configContent := `
host = "localhost"
port = 8080
sessionSecret = "test-secret"
logLevel = "INFO"
databasePath = "/config/file/path.db"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set environment variable
	os.Setenv("QUI__DATABASE_PATH", "/env/var/path.db")
	defer os.Unsetenv("QUI__DATABASE_PATH")

	cfg, err := New(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Environment variable should win
	dbPath := cfg.GetDatabasePath()
	assert.Equal(t, "/env/var/path.db", dbPath, "Environment variable should override config file")
}