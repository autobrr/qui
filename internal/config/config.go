// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/pkg/httphelpers"
)

var envPrefix = "QUI__"

type AppConfig struct {
	Config  *domain.Config
	viper   *viper.Viper
	dataDir string
	version string

	configMu sync.Mutex

	listenersMu sync.RWMutex
	listeners   []func(*domain.Config)

	logManager *LogManager
}

// zerolog keeps its formatting settings in package globals. Set them once at
// startup: writing them again on a config reload races with any goroutine that
// logs at the same time.
func init() {
	zerolog.TimeFieldFormat = time.RFC3339
}

func New(configDirOrPath string, versions ...string) (*AppConfig, error) {
	version := "dev"
	if len(versions) > 0 && strings.TrimSpace(versions[0]) != "" {
		version = versions[0]
	}

	c := &AppConfig{
		viper:      viper.New(),
		Config:     &domain.Config{},
		version:    version,
		logManager: NewLogManager(version),
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
	c.hydrateConfigFromViper()
	if err := c.loadAllowedHosts(); err != nil {
		return nil, err
	}
	c.Config.Version = c.version

	// Resolve data directory after config is unmarshaled
	c.resolveDataDir()

	if err := c.validateSessionSecret(); err != nil {
		return nil, err
	}
	c.warnWeakSessionSecret()

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

	// Generate secure session secret if not provided. crypto/rand cannot fail on
	// Go 1.24+, and a guessable fallback secret would silently weaken every
	// stored credential, so refuse to start instead.
	sessionSecret, err := generateSecureToken(encryptionKeySize)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to generate a secure session secret")
	}

	c.viper.SetDefault("host", host)
	c.viper.SetDefault("port", 7476)
	c.viper.SetDefault("baseUrl", "/")
	c.viper.SetDefault("corsAllowedOrigins", []string{})
	c.viper.SetDefault("allowedHosts", []string{})
	c.viper.SetDefault("sessionSecret", sessionSecret)
	c.viper.SetDefault("logLevel", "DEBUG")
	c.viper.SetDefault("logPath", "")
	c.viper.SetDefault("logMaxSize", 50)
	c.viper.SetDefault("logMaxBackups", 10)
	c.viper.SetDefault("dataDir", "")   // Empty means auto-detect (next to config file)
	c.viper.SetDefault("backupDir", "") // Empty means <dataDir>/backups
	c.viper.SetDefault("databaseEngine", "sqlite")
	c.viper.SetDefault("databaseDsn", "")
	c.viper.SetDefault("databaseHost", "localhost")
	c.viper.SetDefault("databasePort", 5432)
	c.viper.SetDefault("databaseUser", "")
	c.viper.SetDefault("databasePassword", "")
	c.viper.SetDefault("databaseName", "qui")
	c.viper.SetDefault("databaseSSLMode", "disable")
	c.viper.SetDefault("databaseConnectTimeout", 10)
	c.viper.SetDefault("databaseMaxOpenConns", 25)
	c.viper.SetDefault("databaseMaxIdleConns", 5)
	c.viper.SetDefault("databaseConnMaxLifetime", 300)
	c.viper.SetDefault("qbittorrentTimeout", 60)
	c.viper.SetDefault("checkForUpdates", true)
	c.viper.SetDefault("trackerIconsFetchEnabled", true)
	c.viper.SetDefault("customThemesDir", "") // Empty means <config-dir>/themes
	c.viper.SetDefault("crossSeedRecoverErroredTorrents", false)
	c.viper.SetDefault("pprofEnabled", false)
	c.viper.SetDefault("pprofAddr", "127.0.0.1:6060")
	c.viper.SetDefault("metricsEnabled", false)
	c.viper.SetDefault("metricsHost", "127.0.0.1")
	c.viper.SetDefault("metricsPort", 9074)
	c.viper.SetDefault("metricsBasicAuthUsers", "")
	c.viper.SetDefault("externalProgramAllowList", []string{})

	// Auth disabled
	c.viper.SetDefault("authDisabled", false)
	c.viper.SetDefault("I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA", false)
	c.viper.SetDefault("authDisabledAllowedCIDRs", []string{})

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
		return c.loadFromPath(configDirOrPath)
	}
	return c.loadFromStandardLocations()
}

func (c *AppConfig) loadFromPath(configDirOrPath string) error {
	configPath := c.resolveConfigPath(configDirOrPath)
	c.viper.SetConfigFile(configPath)

	if err := c.viper.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return fmt.Errorf("failed to read config: %w", err)
		}
		if writeErr := c.writeDefaultConfig(configPath); writeErr != nil {
			return writeErr
		}
		if readErr := c.viper.ReadInConfig(); readErr != nil {
			return fmt.Errorf("failed to read newly created config: %w", readErr)
		}
	}
	return nil
}

func (c *AppConfig) loadFromStandardLocations() error {
	c.viper.SetConfigName("config")
	c.viper.AddConfigPath(".")
	c.viper.AddConfigPath(GetDefaultConfigDir())

	if err := c.viper.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return fmt.Errorf("failed to read config: %w", err)
		}
		defaultConfigPath := filepath.Join(GetDefaultConfigDir(), "config.toml")
		if writeErr := c.writeDefaultConfig(defaultConfigPath); writeErr != nil {
			return writeErr
		}
		c.viper.SetConfigFile(defaultConfigPath)
		if readErr := c.viper.ReadInConfig(); readErr != nil {
			return fmt.Errorf("failed to read newly created config: %w", readErr)
		}
		c.dataDir = filepath.Dir(defaultConfigPath)
	}
	return nil
}

//nolint:errcheck // BindEnv only errors on empty key, which can't happen with static strings
func (c *AppConfig) loadFromEnv() {
	// DO NOT use AutomaticEnv() - it reads ALL env vars and causes conflicts with K8s
	// Instead, explicitly bind only the environment variables we want

	// Use double underscore to avoid conflicts with K8s deployment_PORT patterns
	c.viper.BindEnv("host", envPrefix+"HOST")
	c.viper.BindEnv("port", envPrefix+"PORT")
	c.viper.BindEnv("baseUrl", envPrefix+"BASE_URL")
	c.viper.BindEnv("corsAllowedOrigins", envPrefix+"CORS_ALLOWED_ORIGINS")
	c.viper.BindEnv("allowedHosts", envPrefix+"ALLOWED_HOSTS")
	c.bindOrReadFromFile("sessionSecret", envPrefix+"SESSION_SECRET")
	c.viper.BindEnv("logLevel", envPrefix+"LOG_LEVEL")
	c.viper.BindEnv("logPath", envPrefix+"LOG_PATH")
	c.viper.BindEnv("logMaxSize", envPrefix+"LOG_MAX_SIZE")
	c.viper.BindEnv("logMaxBackups", envPrefix+"LOG_MAX_BACKUPS")
	c.viper.BindEnv("dataDir", envPrefix+"DATA_DIR")
	c.viper.BindEnv("backupDir", envPrefix+"BACKUP_DIR")
	c.viper.BindEnv("databaseEngine", envPrefix+"DATABASE_ENGINE")
	c.bindOrReadFromFile("databaseDsn", envPrefix+"DATABASE_DSN")
	c.viper.BindEnv("databaseHost", envPrefix+"DATABASE_HOST")
	c.viper.BindEnv("databasePort", envPrefix+"DATABASE_PORT")
	c.viper.BindEnv("databaseUser", envPrefix+"DATABASE_USER")
	c.bindOrReadFromFile("databasePassword", envPrefix+"DATABASE_PASSWORD")
	c.viper.BindEnv("databaseName", envPrefix+"DATABASE_NAME")
	c.viper.BindEnv("databaseSSLMode", envPrefix+"DATABASE_SSL_MODE")
	c.viper.BindEnv("databaseConnectTimeout", envPrefix+"DATABASE_CONNECT_TIMEOUT")
	c.viper.BindEnv("databaseMaxOpenConns", envPrefix+"DATABASE_MAX_OPEN_CONNS")
	c.viper.BindEnv("databaseMaxIdleConns", envPrefix+"DATABASE_MAX_IDLE_CONNS")
	c.viper.BindEnv("databaseConnMaxLifetime", envPrefix+"DATABASE_CONN_MAX_LIFETIME")
	c.viper.BindEnv("qbittorrentTimeout", envPrefix+"QBITTORRENT_TIMEOUT")
	c.viper.BindEnv("checkForUpdates", envPrefix+"CHECK_FOR_UPDATES")
	c.viper.BindEnv("trackerIconsFetchEnabled", envPrefix+"TRACKER_ICONS_FETCH_ENABLED")
	c.viper.BindEnv("customThemesDir", envPrefix+"CUSTOM_THEMES_DIR")
	c.viper.BindEnv("crossSeedRecoverErroredTorrents", envPrefix+"CROSS_SEED_RECOVER_ERRORED_TORRENTS")
	c.viper.BindEnv("pprofEnabled", envPrefix+"PPROF_ENABLED")
	c.viper.BindEnv("pprofAddr", envPrefix+"PPROF_ADDR")
	c.viper.BindEnv("metricsEnabled", envPrefix+"METRICS_ENABLED")
	c.viper.BindEnv("metricsHost", envPrefix+"METRICS_HOST")
	c.viper.BindEnv("metricsPort", envPrefix+"METRICS_PORT")
	c.viper.BindEnv("metricsBasicAuthUsers", envPrefix+"METRICS_BASIC_AUTH_USERS")

	c.viper.BindEnv("authDisabled", envPrefix+"AUTH_DISABLED")
	c.viper.BindEnv("I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA", envPrefix+"I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA")
	c.viper.BindEnv("authDisabledAllowedCIDRs", envPrefix+"AUTH_DISABLED_ALLOWED_CIDRS")

	// OIDC environment variables
	c.viper.BindEnv("oidcEnabled", envPrefix+"OIDC_ENABLED")
	c.viper.BindEnv("oidcIssuer", envPrefix+"OIDC_ISSUER")
	c.viper.BindEnv("oidcClientId", envPrefix+"OIDC_CLIENT_ID")
	c.bindOrReadFromFile("oidcClientSecret", envPrefix+"OIDC_CLIENT_SECRET")
	c.viper.BindEnv("oidcRedirectUrl", envPrefix+"OIDC_REDIRECT_URL")
	c.viper.BindEnv("oidcDisableBuiltInLogin", envPrefix+"OIDC_DISABLE_BUILT_IN_LOGIN")
}

func (c *AppConfig) watchConfig() {
	// Register the handler before the watcher starts: viper reads onConfigChange
	// from the watcher goroutine without a lock, so setting it after WatchConfig
	// races with an event that arrives right away.
	c.viper.OnConfigChange(func(e fsnotify.Event) {
		log.Info().Msgf("Config file changed: %s", e.Name)

		c.configMu.Lock()
		defer c.configMu.Unlock()

		previousAuthSettings := authReloadSettings{
			authDisabled:               c.Config.AuthDisabled,
			iAcknowledgeThisIsABadIdea: c.Config.IAcknowledgeThisIsABadIdea,
			authDisabledAllowedCIDRs:   append([]string(nil), c.Config.AuthDisabledAllowedCIDRs...),
			oidcEnabled:                c.Config.OIDCEnabled,
			corsAllowedOrigins:         append([]string(nil), c.Config.CORSAllowedOrigins...),
		}

		// Reload configuration
		if err := c.viper.Unmarshal(c.Config); err != nil {
			log.Error().Err(err).Msg("Failed to reload configuration")
			return
		}
		c.hydrateConfigFromViper()

		// Apply dynamic changes
		c.applyDynamicChanges(previousAuthSettings)
	})
	c.viper.WatchConfig()
}

type authReloadSettings struct {
	authDisabled               bool
	iAcknowledgeThisIsABadIdea bool
	authDisabledAllowedCIDRs   []string
	oidcEnabled                bool
	corsAllowedOrigins         []string
}

func (c *AppConfig) applyDynamicChanges(previousAuthSettings authReloadSettings) {
	c.Config.Version = c.version
	if err := c.ApplyLogConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to apply log configuration")
	}

	if err := c.Config.ValidateAuthDisabledConfig(); err != nil {
		log.Error().Err(err).Msg("auth-disabled config is invalid after reload; keeping previous valid auth-disabled settings")
		c.Config.AuthDisabled = previousAuthSettings.authDisabled
		c.Config.IAcknowledgeThisIsABadIdea = previousAuthSettings.iAcknowledgeThisIsABadIdea
		c.Config.AuthDisabledAllowedCIDRs = append([]string(nil), previousAuthSettings.authDisabledAllowedCIDRs...)
		c.Config.OIDCEnabled = previousAuthSettings.oidcEnabled
		c.Config.CORSAllowedOrigins = append([]string(nil), previousAuthSettings.corsAllowedOrigins...)

		return
	}

	if err := c.Config.NormalizeCORSAllowedOrigins(); err != nil {
		log.Error().Err(err).Msg("CORS config is invalid after reload; keeping previous valid corsAllowedOrigins")
		c.Config.CORSAllowedOrigins = append([]string(nil), previousAuthSettings.corsAllowedOrigins...)
	}

	switch {
	case c.Config.IsAuthDisabled():
		log.Warn().Strs("authDisabledAllowedCIDRs", c.Config.AuthDisabledAllowedCIDRs).Msg("Authentication is disabled via QUI__AUTH_DISABLED. Access is restricted to authDisabledAllowedCIDRs. Make sure qui is behind a reverse proxy with its own authentication.")
	case c.Config.AuthDisabled != c.Config.IAcknowledgeThisIsABadIdea:
		log.Warn().Msg("Only one of QUI__AUTH_DISABLED and QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA is set. Authentication remains enabled. Set both to disable authentication.")
	}
	if c.Config.IsAuthDisabled() && len(c.Config.AllowedHosts) == 0 {
		log.Warn().Msg("allowedHosts is not configured, so qui accepts requests for any hostname while authentication is disabled. Set allowedHosts to block DNS rebinding.")
	}

	c.notifyListeners()
}

func (c *AppConfig) hydrateConfigFromViper() {
	c.Config.Host = c.viper.GetString("host")
	c.Config.Port = c.viper.GetInt("port")
	// Canonical "/prefix/" form; the index.html redirect breaks on a slashless base.
	c.Config.BaseURL = httphelpers.NormalizeBasePath(c.viper.GetString("baseUrl")) + "/"
	c.Config.CORSAllowedOrigins = c.getNormalizedStringSlice("corsAllowedOrigins")
	c.Config.SessionSecret = c.viper.GetString("sessionSecret")

	c.Config.LogLevel = c.viper.GetString("logLevel")
	c.Config.LogPath = c.viper.GetString("logPath")
	c.Config.LogMaxSize = c.viper.GetInt("logMaxSize")
	c.Config.LogMaxBackups = c.viper.GetInt("logMaxBackups")

	c.Config.DataDir = c.viper.GetString("dataDir")
	c.Config.BackupDir = c.viper.GetString("backupDir")
	c.Config.DatabaseEngine = c.viper.GetString("databaseEngine")
	c.Config.DatabaseDSN = c.viper.GetString("databaseDsn")
	c.Config.DatabaseHost = c.viper.GetString("databaseHost")
	c.Config.DatabasePort = c.viper.GetInt("databasePort")
	c.Config.DatabaseUser = c.viper.GetString("databaseUser")
	c.Config.DatabasePassword = c.viper.GetString("databasePassword")
	c.Config.DatabaseName = c.viper.GetString("databaseName")
	c.Config.DatabaseSSLMode = c.viper.GetString("databaseSSLMode")
	c.Config.DatabaseConnectTimeout = c.viper.GetInt("databaseConnectTimeout")
	c.Config.DatabaseMaxOpenConns = c.viper.GetInt("databaseMaxOpenConns")
	c.Config.DatabaseMaxIdleConns = c.viper.GetInt("databaseMaxIdleConns")
	c.Config.DatabaseConnMaxLifetime = c.viper.GetInt("databaseConnMaxLifetime")
	c.Config.QbittorrentTimeout = c.viper.GetInt("qbittorrentTimeout")
	if c.Config.QbittorrentTimeout <= 0 {
		c.Config.QbittorrentTimeout = 60
	}
	c.Config.CheckForUpdates = c.viper.GetBool("checkForUpdates")
	c.Config.TrackerIconsFetchEnabled = c.viper.GetBool("trackerIconsFetchEnabled")
	c.Config.CustomThemesDir = c.viper.GetString("customThemesDir")
	c.Config.CrossSeedRecoverErroredTorrents = c.viper.GetBool("crossSeedRecoverErroredTorrents")
	c.Config.PprofEnabled = c.viper.GetBool("pprofEnabled")
	c.Config.PprofAddr = c.viper.GetString("pprofAddr")

	c.Config.MetricsEnabled = c.viper.GetBool("metricsEnabled")
	c.Config.MetricsHost = c.viper.GetString("metricsHost")
	c.Config.MetricsPort = c.viper.GetInt("metricsPort")
	c.Config.MetricsBasicAuthUsers = c.viper.GetString("metricsBasicAuthUsers")

	c.Config.ExternalProgramAllowList = c.getNormalizedStringSlice("externalProgramAllowList")

	c.Config.AuthDisabled = c.viper.GetBool("authDisabled")
	c.Config.IAcknowledgeThisIsABadIdea = c.viper.GetBool("I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA")
	c.Config.AuthDisabledAllowedCIDRs = c.getNormalizedStringSlice("authDisabledAllowedCIDRs")

	c.Config.OIDCEnabled = c.viper.GetBool("oidcEnabled")
	c.Config.OIDCIssuer = c.viper.GetString("oidcIssuer")
	c.Config.OIDCClientID = c.viper.GetString("oidcClientId")
	c.Config.OIDCClientSecret = c.viper.GetString("oidcClientSecret")
	c.Config.OIDCRedirectURL = c.viper.GetString("oidcRedirectUrl")
	c.Config.OIDCDisableBuiltInLogin = c.viper.GetBool("oidcDisableBuiltInLogin")
}

func (c *AppConfig) loadAllowedHosts() error {
	var entries []string
	if value, present := os.LookupEnv(envPrefix + "ALLOWED_HOSTS"); present {
		if value != "" {
			entries = strings.Split(value, ",")
		}
	} else {
		switch value := c.viper.Get("allowedHosts").(type) {
		case []string:
			entries = value
		case []any:
			for _, item := range value {
				entry, ok := item.(string)
				if !ok {
					return errors.New("allowedHosts must be an array of strings")
				}
				entries = append(entries, entry)
			}
		default:
			return errors.New("allowedHosts must be an array of strings")
		}
	}
	entries = slices.Clone(entries)
	for i, entry := range entries {
		entries[i] = strings.TrimSpace(entry)
	}
	if _, err := httphelpers.NewHostAllowlist(entries); err != nil {
		return err
	}
	if len(entries) == 0 {
		log.Info().Msg("allowedHosts is not configured, accepting requests for any host")
		return nil
	}
	c.Config.AllowedHosts = withLocalHosts(entries)
	log.Info().Strs("allowedHosts", c.Config.AllowedHosts).Msg("Accepting requests only for the listed hosts")
	return nil
}

// withLocalHosts admits loopback names and the machine hostname, as Sonarr and Radarr do,
// so a list that names only the public hostname does not lock out local access.
func withLocalHosts(entries []string) []string {
	local := []string{"localhost", "127.0.0.1", "::1"}
	if name, err := os.Hostname(); err == nil {
		if _, err := httphelpers.NewHostAllowlist([]string{name}); err == nil {
			local = append(local, name)
		}
	}
	for _, host := range local {
		if !slices.Contains(entries, host) {
			entries = append(entries, host)
		}
	}
	return entries
}

func (c *AppConfig) getNormalizedStringSlice(key string) []string {
	switch value := c.viper.Get(key).(type) {
	case []string:
		return normalizeStringSlice(value)
	case []any:
		normalized := make([]string, 0, len(value))
		for _, item := range value {
			entry, ok := item.(string)
			if !ok {
				continue
			}
			trimmed := strings.TrimSpace(entry)
			if trimmed != "" {
				normalized = append(normalized, trimmed)
			}
		}
		return normalized
	case string:
		return splitStringSliceValue(value)
	default:
		return normalizeStringSlice(c.viper.GetStringSlice(key))
	}
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}

	return normalized
}

func splitStringSliceValue(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})

	return normalizeStringSlice(parts)
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

//nolint:funlen // config template is inherently long
func (c *AppConfig) writeDefaultConfig(path string) error {
	// Check if config already exists
	if _, err := os.Stat(path); err == nil {
		log.Debug().Msgf("Config file already exists at: %s", path)
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
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

# CORS allowlist
# Empty (default) disables CORS.
# Entries must be explicit origins (scheme + host + optional non-default port).
# Wildcards are not allowed.
# Example:
#corsAllowedOrigins = ["https://sso.example.com", "https://panel.example.com"]

# Allowed request hosts
# Empty (default) permits all hosts. Restart after changes.
# List the Host received by qui. X-Forwarded-Host is ignored.
# Use hostnames, IP addresses, or leading *. subdomain wildcards, without ports.
# Direct loopback GET and HEAD probes to the three built-in health endpoints bypass this list.
#allowedHosts = ["qui.example.com", "localhost", "::1", "*.home.example.com"]

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
# Size in MB that starts a rotation
# Default: {{ .logMaxSize }}
#logMaxSize = {{ .logMaxSize }}

# Number of rotated log files that qui keeps (0 keeps all)
# Rotated files are gzip-compressed. Measured on the logs of qui, a 50 MB file
# compresses to 2 to 3 MB.
# Default: {{ .logMaxBackups }}
#logMaxBackups = {{ .logMaxBackups }}

# Data directory (default: next to config file)
# Database file (qui.db) will be created inside this directory
#dataDir = "/var/db/qui"

# Backup directory (default: <dataDir>/backups)
# Backup manifests, archives and cached .torrent files are stored here.
# A relative path is resolved against the config directory.
# If you change this on an existing install, move the contents of
# <dataDir>/backups into the new directory yourself.
#backupDir = "/mnt/storage/qui-backups"

# Custom themes directory (default: <config-dir>/themes, auto-created)
# Drop sideloaded *.css theme files here. Listing requires premium access.
# A relative path is resolved against the config directory.
#customThemesDir = "/config/themes"

# Database engine
# Options: "sqlite" (default), "postgres"
#databaseEngine = "sqlite"

# Postgres connection settings (used when databaseEngine = "postgres")
# Preferred: provide a DSN and ignore host/user/password fields.
#databaseDsn = "postgres://user:password@localhost:5432/qui?sslmode=disable"
#databaseHost = "localhost"
#databasePort = 5432
#databaseUser = ""
#databasePassword = ""
#databaseName = "qui"
#databaseSSLMode = "disable"
#databaseConnectTimeout = 10
#databaseMaxOpenConns = 25
#databaseMaxIdleConns = 5
#databaseConnMaxLifetime = 300

# HTTP timeout in seconds for requests qui makes to qBittorrent instances
# (sync, health checks, capabilities). Raise it for very large or slow
# instances whose responses take longer than 60 seconds.
# Default: 60
#qbittorrentTimeout = 60

# Check for new releases via api.autobrr.com
# Default: true
#checkForUpdates = true

# Tracker icon fetching
# Disable to prevent qui from requesting tracker favicons from remote trackers.
# Default: true
#trackerIconsFetchEnabled = true

# Cross-seed errored torrent recovery (requires restart)
# When enabled, cross-seed automation will attempt to recover torrents in error/missingFiles state
# by triggering recheck and waiting for completion. This can cause runs to take 25+ minutes.
# When disabled (default), errored torrents are simply excluded from candidate selection.
# Default: false
#crossSeedRecoverErroredTorrents = false

# Log level
# Default: "DEBUG"
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
# DEBUG records sufficient detail to diagnose most reports.
# TRACE adds per-request and per-sync-tick detail and makes the file grow quickly.
#logLevel = "{{ .logLevel }}"

# Prometheus Metrics
# Enable Prometheus metrics on a separate port
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
# Format: "username:password" or "user1:password1,user2:password2" for multiple users
# Passwords are plaintext and can contain colons. Usernames cannot contain colons.
# Commas cannot appear in usernames or passwords. Protect this configuration file.
# Example: "prometheus:secret"
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

	// Create config file with owner-only permissions. It holds the session
	// secret, so it must never be group/world readable or writable, regardless
	// of the process umask (e.g. when UMASK is set for content sharing).
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "AppData", "Roaming", "qui")
		}
		return filepath.Join(".", "qui")
	default:
		// Use ~/.config/qui for Unix-like systems
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "qui")
		}
		return filepath.Join(".", ".config", "qui")
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

func (c *AppConfig) ApplyLogConfig() error {
	// Initialize the log manager on first call (sets up switchable writer)
	c.logManager.Initialize()

	// Resolve relative log paths to config directory
	resolvedPath := c.ResolveLogPath(c.Config.LogPath)

	// Apply configuration through the log manager
	return c.logManager.Apply(c.Config.LogLevel, resolvedPath, c.Config.LogMaxSize, c.Config.LogMaxBackups)
}

// GetLogManager returns the LogManager for log streaming.
func (c *AppConfig) GetLogManager() *LogManager {
	return c.logManager
}

func setLogLevel(level string) {
	lvl, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
}

func baseLogWriter(version string) io.Writer {
	if isDevBuild(version) {
		writer := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339,
			PartsOrder: []string{zerolog.TimestampFieldName, zerolog.LevelFieldName, zerolog.MessageFieldName},
			FormatTimestamp: func(i any) string {
				if i == nil {
					return ""
				}
				return fmt.Sprint(i)
			},
			FormatMessage: func(i any) string {
				if i == nil {
					return ""
				}
				msg := strings.TrimSpace(fmt.Sprint(i))
				if msg == "" {
					return ""
				}
				return msg
			}}
		return writer
	}
	return os.Stderr
}

// InitDefaultLogger configures zerolog with the default writer for this version.
// This is used by CLI entry points before a configuration file is loaded.
func InitDefaultLogger(version string) {
	log.Logger = log.Logger.Output(baseLogWriter(version))
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

// GetBackupDir returns the resolved backup root directory.
// Empty config defaults to <dataDir>/backups; a relative override is resolved
// against the config directory, an absolute override is used verbatim.
func (c *AppConfig) GetBackupDir() string {
	dir := strings.TrimSpace(c.Config.BackupDir)
	if dir == "" {
		return filepath.Join(c.dataDir, "backups")
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(c.GetConfigDir(), dir)
	}
	return dir
}

// GetConfigDir returns the directory containing the config file
func (c *AppConfig) GetConfigDir() string {
	if c.viper.ConfigFileUsed() != "" {
		return filepath.Dir(c.viper.ConfigFileUsed())
	}
	// Fallback to default config directory when no config file is explicitly used
	return GetDefaultConfigDir()
}

// GetCustomThemesDir returns the resolved custom themes directory.
// Empty config defaults to <config-dir>/themes; a relative override is resolved
// against the config directory, an absolute override is used verbatim.
func (c *AppConfig) GetCustomThemesDir() string {
	dir := strings.TrimSpace(c.Config.CustomThemesDir)
	if dir == "" {
		return filepath.Join(c.GetConfigDir(), "themes")
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(c.GetConfigDir(), dir)
	}
	return dir
}

// EnsureCustomThemesDir resolves the custom themes directory and creates it if missing.
// The resolved path is returned even when creation fails so callers can report it.
func (c *AppConfig) EnsureCustomThemesDir() (string, error) {
	dir := c.GetCustomThemesDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return dir, fmt.Errorf("failed to create custom themes directory %s: %w", dir, err)
	}
	return dir, nil
}

// ResolveLogPath resolves a log path, making relative paths relative to the config directory.
// Returns empty string if logPath is empty (stdout only).
func (c *AppConfig) ResolveLogPath(logPath string) string {
	if logPath == "" {
		return ""
	}
	if filepath.IsAbs(logPath) {
		return logPath
	}
	return filepath.Join(c.GetConfigDir(), logPath)
}

const encryptionKeySize = 32

// encryptionKeyInfo binds the derived credential key to this one purpose, so a
// future key taken from the same session secret is independent of it. It is
// frozen, not versioned: the qui2 ciphertext prefix carries format version, and
// changing this literal would orphan every stored credential with no fallback.
// The identifier avoids the substring "cred", which gosec G101 matches on.
const encryptionKeyInfo = "qui credential encryption key"

func WriteDefaultConfig(path string) error {
	c := &AppConfig{
		viper: viper.New(),
	}

	c.defaults()

	return c.writeDefaultConfig(path)
}

// GetEncryptionKey derives the 32-byte credential encryption key from the whole
// session secret with HKDF-SHA256.
func (c *AppConfig) GetEncryptionKey() []byte {
	key, err := hkdf.Key(sha256.New, []byte(c.Config.SessionSecret), nil, encryptionKeyInfo, encryptionKeySize)
	if err != nil {
		// Reachable only under GODEBUG=fips140=only, which rejects a secret
		// shorter than 112 bits. That host opted into the policy, so refusing to
		// start is right even though a short secret only warns everywhere else.
		log.Fatal().Err(err).Int("length", len(c.Config.SessionSecret)).Msg(
			"sessionSecret is too short to derive the credential encryption key in FIPS 140-only mode. Lengthening it makes stored credentials unreadable, so re-enter them in the UI afterwards")
	}
	return key
}

// GetLegacyEncryptionKey returns the pre-HKDF key, the session secret truncated
// to 32 bytes or zero-padded up to it. Credentials written before the derived
// key shipped are still readable only with this.
func (c *AppConfig) GetLegacyEncryptionKey() []byte {
	secret := c.Config.SessionSecret
	if len(secret) >= encryptionKeySize {
		return []byte(secret[:encryptionKeySize])
	}

	padded := make([]byte, encryptionKeySize)
	copy(padded, secret)
	return padded
}

// validateSessionSecret rejects an empty session secret. Viper only falls back
// to the generated default when the key is absent, so an explicit empty value in
// config.toml survives loading. Every install would then derive the same
// credential key from the empty string.
func (c *AppConfig) validateSessionSecret() error {
	// Validated on the trimmed value but never stored trimmed. Rewriting the
	// secret would change the derived key and break stored credentials.
	if strings.TrimSpace(c.Config.SessionSecret) != "" {
		return nil
	}

	return errors.New("sessionSecret is empty. Set it in config.toml or QUI__SESSION_SECRET to a random value of at least 32 characters. Credentials saved while it was empty will not decrypt under the new value, so enter them again in the UI")
}

// warnWeakSessionSecret reports a session secret shorter than the key HKDF
// derives from it. HKDF spreads the secret over 32 bytes. It cannot add entropy
// the secret does not have. This warns and never refuses, because lengthening
// the secret would make every stored credential undecryptable.
func (c *AppConfig) warnWeakSessionSecret() {
	length := len(c.Config.SessionSecret)
	if length >= encryptionKeySize {
		return
	}

	log.Warn().
		Int("length", length).
		Int("recommended", encryptionKeySize).
		Msg("sessionSecret is shorter than 32 characters, so the credential encryption key is only as strong as the secret. On a new install set at least 32 characters. On an install that already stores credentials leave it alone, because changing it makes every stored credential unreadable and you would have to enter them all again")
}

// bindOrReadFromFile sets the viper variable from a file if the _FILE suffixed
// environment variable is present, otherwise it binds to the regular env var.
func (c *AppConfig) bindOrReadFromFile(viperVar, envVar string) {
	envVarFile := envVar + "_FILE"
	filePath := os.Getenv(envVarFile)
	if filePath == "" {
		c.viper.BindEnv(viperVar, envVar) //nolint:errcheck // BindEnv only errors on empty key
		return
	}

	content, err := os.ReadFile(filePath) //nolint:gosec // G703: the path comes from the operator's own *_FILE environment variable
	if err != nil {
		log.Fatal().Err(err).Str("path", filePath).Msg("Could not read " + envVarFile)
	}

	secret := strings.TrimSpace(string(content))
	if secret == "" {
		log.Fatal().Str("path", filePath).Msg(envVarFile + " file is empty")
	}

	c.viper.Set(viperVar, secret)
}
