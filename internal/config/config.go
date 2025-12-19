// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/autobrr/qui/internal/domain"
)

var envPrefix = "QUI__"

type AppConfig struct {
	Config  *domain.Config
	viper   *viper.Viper
	dataDir string
	version string

	listenersMu sync.RWMutex
	listeners   []func(*domain.Config)
}

func New(configDirOrPath string, versions ...string) (*AppConfig, error) {
	version := "dev"
	if len(versions) > 0 && strings.TrimSpace(versions[0]) != "" {
		version = versions[0]
	}

	c := &AppConfig{
		viper:   viper.New(),
		Config:  &domain.Config{},
		version: version,
	}

	// Set defaults
	c.defaults()

	// Load from config file
	if err := c.load(configDirOrPath); err != nil {
		return nil, err
	}

	// Override with environment variables
	c.loadFromEnv()

	// Unmarshal the configuration
	if err := c.viper.Unmarshal(c.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	c.Config.Version = c.version

	// Resolve data directory after config is unmarshaled
	c.resolveDataDir()

	// Watch for config changes
	c.watchConfig()

	return c, nil
}

func (c *AppConfig) defaults() {
	// Detect if running in container
	host := "localhost"
	if detectContainer() {
		host = "0.0.0.0"
	}

	// Generate secure session secret if not provided
	sessionSecret, err := generateSecureToken(encryptionKeySize)
	if err != nil {
		// Log error but continue with a fallback
		log.Error().Err(err).Msg("Failed to generate secure session secret, using fallback")
		sessionSecret = "change-me-" + fmt.Sprintf("%d", os.Getpid())
	}

	c.viper.SetDefault("host", host)
	c.viper.SetDefault("port", 7476)
	c.viper.SetDefault("baseUrl", "/")
	c.viper.SetDefault("sessionSecret", sessionSecret)
	c.viper.SetDefault("logLevel", "INFO")
	c.viper.SetDefault("logPath", "")
	c.viper.SetDefault("logMaxSize", 50)
	c.viper.SetDefault("logMaxBackups", 3)
	c.viper.SetDefault("dataDir", "") // Empty means auto-detect (next to config file)
	c.viper.SetDefault("checkForUpdates", true)
	c.viper.SetDefault("trackerIconsFetchEnabled", true)
	c.viper.SetDefault("pprofEnabled", false)
	c.viper.SetDefault("metricsEnabled", false)
	c.viper.SetDefault("metricsHost", "127.0.0.1")
	c.viper.SetDefault("metricsPort", 9074)
	c.viper.SetDefault("metricsBasicAuthUsers", "")
	c.viper.SetDefault("externalProgramAllowList", []string{})

	// OIDC defaults
	c.viper.SetDefault("oidcEnabled", false)
	c.viper.SetDefault("oidcIssuer", "")
	c.viper.SetDefault("oidcClientId", "")
	c.viper.SetDefault("oidcClientSecret", "")
	c.viper.SetDefault("oidcRedirectUrl", "")
	c.viper.SetDefault("oidcDisableBuiltInLogin", false)

}

func (c *AppConfig) load(configDirOrPath string) error {
	c.viper.SetConfigType("toml")

	if configDirOrPath != "" {
		// Determine if this is a directory or file path
		configPath := c.resolveConfigPath(configDirOrPath)
		c.viper.SetConfigFile(configPath)

		// Try to read the config
		if err := c.viper.ReadInConfig(); err != nil {
			// If file doesn't exist, create it
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				if err := c.writeDefaultConfig(configPath); err != nil {
					return err
				}
				// Re-read after creating
				if err := c.viper.ReadInConfig(); err != nil {
					return fmt.Errorf("failed to read newly created config: %w", err)
				}
				return nil
			}
			return fmt.Errorf("failed to read config: %w", err)
		}
	} else {
		// Search for config in standard locations
		c.viper.SetConfigName("config")
		c.viper.AddConfigPath(".")                   // Current directory
		c.viper.AddConfigPath(GetDefaultConfigDir()) // OS-specific config directory

		// Try to read existing config
		if err := c.viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				// No config found, create in OS-specific location
				defaultConfigPath := filepath.Join(GetDefaultConfigDir(), "config.toml")
				if err := c.writeDefaultConfig(defaultConfigPath); err != nil {
					return err
				}
				// Set the config file explicitly and read it
				c.viper.SetConfigFile(defaultConfigPath)
				if err := c.viper.ReadInConfig(); err != nil {
					return fmt.Errorf("failed to read newly created config: %w", err)
				}
				// Explicitly set data directory for newly created config
				configDir := filepath.Dir(defaultConfigPath)
				c.dataDir = configDir
				return nil
			}
			return fmt.Errorf("failed to read config: %w", err)
		}
	}

	return nil
}

func (c *AppConfig) loadFromEnv() {
	// DO NOT use AutomaticEnv() - it reads ALL env vars and causes conflicts with K8s
	// Instead, explicitly bind only the environment variables we want

	// Use double underscore to avoid conflicts with K8s deployment_PORT patterns
	c.viper.BindEnv("host", envPrefix+"HOST")
	c.viper.BindEnv("port", envPrefix+"PORT")
	c.viper.BindEnv("baseUrl", envPrefix+"BASE_URL")
	c.bindOrReadFromFile("sessionSecret", envPrefix+"SESSION_SECRET")
	c.viper.BindEnv("logLevel", envPrefix+"LOG_LEVEL")
	c.viper.BindEnv("logPath", envPrefix+"LOG_PATH")
	c.viper.BindEnv("logMaxSize", envPrefix+"LOG_MAX_SIZE")
	c.viper.BindEnv("logMaxBackups", envPrefix+"LOG_MAX_BACKUPS")
	c.viper.BindEnv("dataDir", envPrefix+"DATA_DIR")
	c.viper.BindEnv("checkForUpdates", envPrefix+"CHECK_FOR_UPDATES")
	c.viper.BindEnv("trackerIconsFetchEnabled", envPrefix+"TRACKER_ICONS_FETCH_ENABLED")
	c.viper.BindEnv("pprofEnabled", envPrefix+"PPROF_ENABLED")
	c.viper.BindEnv("metricsEnabled", envPrefix+"METRICS_ENABLED")
	c.viper.BindEnv("metricsHost", envPrefix+"METRICS_HOST")
	c.viper.BindEnv("metricsPort", envPrefix+"METRICS_PORT")
	c.viper.BindEnv("metricsBasicAuthUsers", envPrefix+"METRICS_BASIC_AUTH_USERS")

	// OIDC environment variables
	c.viper.BindEnv("oidcEnabled", envPrefix+"OIDC_ENABLED")
	c.viper.BindEnv("oidcIssuer", envPrefix+"OIDC_ISSUER")
	c.viper.BindEnv("oidcClientId", envPrefix+"OIDC_CLIENT_ID")
	c.viper.BindEnv("oidcClientSecret", envPrefix+"OIDC_CLIENT_SECRET")
	c.viper.BindEnv("oidcRedirectUrl", envPrefix+"OIDC_REDIRECT_URL")
	c.viper.BindEnv("oidcDisableBuiltInLogin", envPrefix+"OIDC_DISABLE_BUILT_IN_LOGIN")

}

func (c *AppConfig) watchConfig() {
	c.viper.WatchConfig()
	c.viper.OnConfigChange(func(e fsnotify.Event) {
		log.Info().Msgf("Config file changed: %s", e.Name)

		// Reload configuration
		if err := c.viper.Unmarshal(c.Config); err != nil {
			log.Error().Err(err).Msg("Failed to reload configuration")
			return
		}

		// Apply dynamic changes
		c.applyDynamicChanges()
	})
}

func (c *AppConfig) applyDynamicChanges() {
	c.Config.Version = c.version
	c.ApplyLogConfig()

	// Update check for updates flag from config changes
	c.Config.CheckForUpdates = c.viper.GetBool("checkForUpdates")

	c.notifyListeners()
}

// RegisterReloadListener registers a callback that's invoked when the configuration file is reloaded.
func (c *AppConfig) RegisterReloadListener(fn func(*domain.Config)) {
	c.listenersMu.Lock()
	defer c.listenersMu.Unlock()
	c.listeners = append(c.listeners, fn)
}

func (c *AppConfig) notifyListeners() {
	c.listenersMu.RLock()
	listeners := append([]func(*domain.Config){}, c.listeners...)
	c.listenersMu.RUnlock()

	if len(listeners) == 0 {
		return
	}

	copied := *c.Config
	for _, listener := range listeners {
		listener(&copied)
	}
}

func (c *AppConfig) writeDefaultConfig(path string) error {
	// Check if config already exists
	if _, err := os.Stat(path); err == nil {
		log.Debug().Msgf("Config file already exists at: %s", path)
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}
	log.Debug().Msgf("Created config directory: %s", dir)

	// Create config template
	configTemplate := `# config.toml - Auto-generated on first run

# Hostname / IP
# Default: "localhost" (or "0.0.0.0" in containers)
host = "{{ .host }}"

# Port
# Default: 7476
port = {{ .port }}

# Base URL
# Set custom baseUrl eg /qui/ to serve in subdirectory.
# Not needed for subdomain, or by accessing with :port directly.
# Optional
#baseUrl = "/qui/"

# Session secret
# Auto-generated if not provided
# WARNING: Changing this value will break decryption of existing instance passwords!
# If changed, you'll need to re-enter passwords for all existing qBittorrent instances in the UI.
sessionSecret = "{{ .sessionSecret }}"

# Log file path
# If not defined, logs to stdout
# Optional
#logPath = "log/qui.log"

# Log rotation
# Maximum log file size in megabytes before rotation
# Default: {{ .logMaxSize }}
#logMaxSize = {{ .logMaxSize }}

# Number of rotated log files to retain (0 keeps all)
# Default: {{ .logMaxBackups }}
#logMaxBackups = {{ .logMaxBackups }}

# Data directory (default: next to config file)
# Database file (qui.db) will be created inside this directory
#dataDir = "/var/db/qui"

# Check for new releases via api.autobrr.com
# Default: true
#checkForUpdates = true

# Tracker icon fetching
# Disable to prevent qui from requesting tracker favicons from remote trackers.
# Default: true
#trackerIconsFetchEnabled = true

# Log level
# Default: "INFO"
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
logLevel = "{{ .logLevel }}"

# Prometheus Metrics
# Enable Prometheus metrics on separate port (no authentication required)
# Default: false
#metricsEnabled = false

# Metrics server host (bind address for metrics endpoint)
# Default: "127.0.0.1"
# Set to "0.0.0.0" to bind to all interfaces if needed
#metricsHost = "127.0.0.1"

# Metrics server port (separate from main web interface)
# Default: 9074 (standard Prometheus range)
#metricsPort = 9074

# Basic authentication for metrics endpoint (optional)
# Format: "username:bcrypt_hash" or "user1:hash1,user2:hash2" for multiple users
# Passwords must be bcrypt-hashed. Use tools like htpasswd or online bcrypt generators
# Example: "prometheus:$2y$10$example_bcrypt_hash_here"
# Leave empty to disable authentication (default)
#metricsBasicAuthUsers = ""

# External program allow list
# Restrict which executables can be started from qui.
# Provide absolute paths to binaries or directories. Leave commented to allow any program.
#externalProgramAllowList = [
#       "/usr/local/bin/my-script",
#       "/home/user/bin",
#]

# OpenID Connect (OIDC) Configuration
# Enable OIDC authentication
#oidcEnabled = false

# OIDC Issuer URL (e.g. https://auth.example.com)
#oidcIssuer = ""

# OIDC Client ID
#oidcClientId = ""

# OIDC Client Secret
#oidcClientSecret = ""

# OIDC Redirect URL (e.g. http://localhost:7476/api/auth/oidc/callback)
#oidcRedirectUrl = ""

# Disable Built-In Login Form (only works when OIDC is enabled)
#oidcDisableBuiltInLogin = false
`

	// Prepare template data
	data := map[string]any{
		"host":          c.viper.GetString("host"),
		"port":          c.viper.GetInt("port"),
		"sessionSecret": c.viper.GetString("sessionSecret"),
		"logLevel":      c.viper.GetString("logLevel"),
		"logMaxSize":    c.viper.GetInt("logMaxSize"),
		"logMaxBackups": c.viper.GetInt("logMaxBackups"),
	}

	// Parse and execute template
	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse config template: %w", err)
	}

	// Create config file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	log.Info().Msgf("Created default config file: %s", path)
	return nil
}

// Helper functions

// GetDefaultConfigDir returns the OS-specific config directory
func GetDefaultConfigDir() string {
	// First check if XDG_CONFIG_HOME is set (Docker containers set this to /config)
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		// If XDG_CONFIG_HOME is /config (Docker), use it directly
		if xdgConfig == "/config" {
			return xdgConfig
		}
		// Otherwise append qui subdirectory
		return filepath.Join(xdgConfig, "qui")
	}

	switch runtime.GOOS {
	case "windows":
		// Use %APPDATA%\qui on Windows
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "qui")
		}
		// Fallback to home directory
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Roaming", "qui")
	default:
		// Use ~/.config/qui for Unix-like systems
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "qui")
	}
}

func detectContainer() bool {
	// Check Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Check LXC
	if _, err := os.Stat("/dev/.lxc-boot-id"); err == nil {
		return true
	}
	// Check if running as init
	if os.Getpid() == 1 {
		return true
	}
	return false
}

func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func (c *AppConfig) ApplyLogConfig() {
	zerolog.TimeFieldFormat = time.RFC3339

	setLogLevel(c.Config.LogLevel)

	writer := c.baseLogWriter()

	if c.Config.LogPath != "" {
		multiWriter, err := setupLogFile(c.Config.LogPath, writer, c.Config.LogMaxSize, c.Config.LogMaxBackups)
		if err != nil {
			log.Error().Err(err).Msg("Failed to setup log file")
		} else {
			writer = multiWriter
		}
	}

	log.Logger = log.Logger.Output(writer)
}

func setLogLevel(level string) {
	lvl, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	log.Logger = log.Logger.Level(lvl)
}

func setupLogFile(path string, base io.Writer, maxSize, maxBackups int) (io.Writer, error) {
	// Create log directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	if maxSize <= 0 {
		maxSize = 50
	}

	if maxBackups < 0 {
		maxBackups = 0
	}

	rotator := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
	}

	return io.MultiWriter(base, rotator), nil
}

func baseLogWriter(version string) io.Writer {
	if isDevBuild(version) {
		writer := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
		writer.PartsOrder = []string{zerolog.TimestampFieldName, zerolog.LevelFieldName, zerolog.MessageFieldName}
		writer.FormatTimestamp = func(i any) string {
			if i == nil {
				return ""
			}
			return fmt.Sprint(i)
		}
		writer.FormatMessage = func(i any) string {
			if i == nil {
				return ""
			}
			msg := strings.TrimSpace(fmt.Sprint(i))
			if msg == "" {
				return ""
			}
			return msg
		}
		return writer
	}
	return os.Stderr
}

func (c *AppConfig) baseLogWriter() io.Writer {
	return baseLogWriter(c.version)
}

// DefaultLogWriter returns the base log writer for the provided version.
func DefaultLogWriter(version string) io.Writer {
	return baseLogWriter(version)
}

// InitDefaultLogger configures zerolog with the default writer for this version.
// This is used by CLI entry points before a configuration file is loaded.
func InitDefaultLogger(version string) {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Logger.Output(DefaultLogWriter(version))
}

func isDevBuild(version string) bool {
	v := strings.ToLower(strings.TrimSpace(version))
	return v == "" || v == "dev" || strings.HasSuffix(v, "-dev")
}

// resolveConfigPath determines the actual config file path from the provided directory or file path
func (c *AppConfig) resolveConfigPath(configDirOrPath string) string {
	// Check if it's a direct file path (ends with .toml) - backward compatibility
	if strings.HasSuffix(strings.ToLower(configDirOrPath), ".toml") {
		return configDirOrPath
	}

	// Check if the path points to an existing file (backward compatibility)
	if info, err := os.Stat(configDirOrPath); err == nil && !info.IsDir() {
		return configDirOrPath
	}

	// Treat as directory path and append config.toml
	return filepath.Join(configDirOrPath, "config.toml")
}

// resolveDataDir sets the data directory based on configuration
func (c *AppConfig) resolveDataDir() {
	switch {
	case c.Config.DataDir != "":
		c.dataDir = c.Config.DataDir
	case c.viper.ConfigFileUsed() != "":
		c.dataDir = filepath.Dir(c.viper.ConfigFileUsed())
	default:
		c.dataDir = "."
	}
}

// GetDatabasePath returns the path to the database file
func (c *AppConfig) GetDatabasePath() string {
	return filepath.Join(c.dataDir, "qui.db")
}

// GetDataDir returns the resolved data directory path.
func (c *AppConfig) GetDataDir() string {
	return c.dataDir
}

// SetDataDir sets the data directory (used by CLI flags)
func (c *AppConfig) SetDataDir(dir string) {
	c.dataDir = dir
}

// GetConfigDir returns the directory containing the config file
func (c *AppConfig) GetConfigDir() string {
	if c.viper.ConfigFileUsed() != "" {
		return filepath.Dir(c.viper.ConfigFileUsed())
	}
	// Fallback to default config directory when no config file is explicitly used
	return GetDefaultConfigDir()
}

const encryptionKeySize = 32

func WriteDefaultConfig(path string) error {
	c := &AppConfig{
		viper: viper.New(),
	}

	c.defaults()

	return c.writeDefaultConfig(path)
}

// GetEncryptionKey derives a 32-byte encryption key from the session secret
func (c *AppConfig) GetEncryptionKey() []byte {
	// Use first 32 bytes of session secret as encryption key
	// In production, you might want to derive this differently
	secret := c.Config.SessionSecret
	if len(secret) >= encryptionKeySize {
		return []byte(secret[:encryptionKeySize])
	}

	// Pad the secret if it's too short
	padded := make([]byte, encryptionKeySize)
	copy(padded, []byte(secret))
	return padded
}

// bindOrReadFromFile sets the viper variable from a file if the _FILE suffixed
// environment variable is present, otherwise it binds to the regular env var.
func (c *AppConfig) bindOrReadFromFile(viperVar string, envVar string) {
	envVarFile := envVar + "_FILE"
	filePath := os.Getenv(envVarFile)
	if filePath == "" {
		c.viper.BindEnv(viperVar, envVar)
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatal().Err(err).Str("path", filePath).Msg("Could not read " + envVarFile)
	}

	secret := strings.TrimSpace(string(content))
	if secret == "" {
		log.Fatal().Str("path", filePath).Msg(envVarFile + " file is empty")
	}

	c.viper.Set(viperVar, secret)
}
