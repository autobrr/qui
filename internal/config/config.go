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
	"io/fs"
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

	watchMu      sync.Mutex
	watchStopped bool

	listenersMu sync.RWMutex
	listeners   []func(*domain.Config)

	logManager *LogManager
}

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

	c.defaults()

	if err := c.load(configDirOrPath); err != nil {
		return nil, err
	}

	c.loadFromEnv()

	if err := c.viper.Unmarshal(c.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	c.hydrateConfigFromViper()
	if err := c.loadAllowedHosts(); err != nil {
		return nil, err
	}
	c.Config.ExperimentalFeatures = c.getNormalizedStringSlice("experimentalFeatures")
	c.Config.Version = c.version

	c.resolveDataDir()

	if err := c.validateSessionSecret(); err != nil {
		return nil, err
	}
	c.warnWeakSessionSecret()
	c.Config.Publish()

	c.watchConfig()

	return c, nil
}

func (c *AppConfig) defaults() {
	host := "localhost"
	if detectContainer() {
		host = "0.0.0.0"
	}

	sessionSecret, err := generateSecureToken(encryptionKeySize)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to generate a secure session secret")
	}

	c.viper.SetDefault("host", host)
	c.viper.SetDefault("port", 7476)
	c.viper.SetDefault("baseUrl", "/")
	c.viper.SetDefault("corsAllowedOrigins", []string{})
	c.viper.SetDefault("allowedHosts", []string{})
	c.viper.SetDefault("experimentalFeatures", []string{})
	c.viper.SetDefault("sessionSecret", sessionSecret)
	c.viper.SetDefault("logLevel", "DEBUG")
	c.viper.SetDefault("logPath", "")
	c.viper.SetDefault("logMaxSize", 50)
	c.viper.SetDefault("logMaxBackups", 10)
	c.viper.SetDefault("dataDir", "")
	c.viper.SetDefault("backupDir", "")
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
	c.viper.SetDefault("problemReports", true)
	c.viper.SetDefault("problemReportsURL", "https://bin.getqui.com")
	c.viper.SetDefault("disableSelfUpdate", false)
	c.viper.SetDefault("trackerIconsFetchEnabled", true)
	c.viper.SetDefault("customThemesDir", "")
	c.viper.SetDefault("crossSeedRecoverErroredTorrents", false)
	c.viper.SetDefault("pprofEnabled", false)
	c.viper.SetDefault("pprofAddr", "127.0.0.1:6060")
	c.viper.SetDefault("metricsEnabled", false)
	c.viper.SetDefault("metricsHost", "127.0.0.1")
	c.viper.SetDefault("metricsPort", 9074)
	c.viper.SetDefault("metricsBasicAuthUsers", "")
	c.viper.SetDefault("externalProgramAllowList", []string{})
	c.viper.SetDefault("externalProgramMaxRunning", 8)

	c.viper.SetDefault("authDisabled", false)
	c.viper.SetDefault("I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA", false)
	c.viper.SetDefault("authDisabledAllowedCIDRs", []string{})

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
		if !errors.Is(err, fs.ErrNotExist) {
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
	return c.loadFromPath(standardConfigPath())
}

func standardConfigPath() string {
	defaultConfigPath := filepath.Join(GetDefaultConfigDir(), "config.toml")
	workDirConfigPath, err := filepath.Abs("config.toml")
	if err != nil {
		return defaultConfigPath
	}
	for _, candidate := range []string{workDirConfigPath, defaultConfigPath} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return defaultConfigPath
}

//nolint:errcheck // BindEnv only errors on empty key, which can't happen with static strings
func (c *AppConfig) loadFromEnv() {
	c.viper.BindEnv("host", envPrefix+"HOST")
	c.viper.BindEnv("port", envPrefix+"PORT")
	c.viper.BindEnv("baseUrl", envPrefix+"BASE_URL")
	c.viper.BindEnv("corsAllowedOrigins", envPrefix+"CORS_ALLOWED_ORIGINS")
	c.viper.BindEnv("allowedHosts", envPrefix+"ALLOWED_HOSTS")
	c.viper.BindEnv("experimentalFeatures", envPrefix+"EXPERIMENTAL_FEATURES")
	c.bindOrReadFromFile("sessionSecret", envPrefix+"SESSION_SECRET")
	c.viper.BindEnv("sessionCookieSecure", envPrefix+"SESSION_COOKIE_SECURE")
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
	c.viper.BindEnv("problemReports", envPrefix+"PROBLEM_REPORTS")
	c.viper.BindEnv("problemReportsURL", envPrefix+"PROBLEM_REPORTS_URL")
	c.viper.BindEnv("externalProgramMaxRunning", envPrefix+"EXTERNAL_PROGRAM_MAX_RUNNING")
	c.viper.BindEnv("disableSelfUpdate", envPrefix+"DISABLE_SELF_UPDATE")
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

	c.viper.BindEnv("oidcEnabled", envPrefix+"OIDC_ENABLED")
	c.viper.BindEnv("oidcIssuer", envPrefix+"OIDC_ISSUER")
	c.viper.BindEnv("oidcClientId", envPrefix+"OIDC_CLIENT_ID")
	c.bindOrReadFromFile("oidcClientSecret", envPrefix+"OIDC_CLIENT_SECRET")
	c.viper.BindEnv("oidcRedirectUrl", envPrefix+"OIDC_REDIRECT_URL")
	c.viper.BindEnv("oidcDisableBuiltInLogin", envPrefix+"OIDC_DISABLE_BUILT_IN_LOGIN")
	c.viper.BindEnv("oidcAllowedSubject", envPrefix+"OIDC_ALLOWED_SUBJECT")
	c.viper.BindEnv("oidcAllowedEmail", envPrefix+"OIDC_ALLOWED_EMAIL")
}

func (c *AppConfig) watchConfig() {
	c.viper.OnConfigChange(func(e fsnotify.Event) {
		c.watchMu.Lock()
		defer c.watchMu.Unlock()
		if c.watchStopped {
			return
		}
		log.Info().Msgf("Config file changed: %s", e.Name)
		time.Sleep(configReloadDelay)
		c.reload()
	})
	c.viper.WatchConfig()
}

func (c *AppConfig) StopWatching() {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	c.watchStopped = true
}

const configReloadDelay = 200 * time.Millisecond

func (c *AppConfig) reload() {
	c.configMu.Lock()
	defer c.configMu.Unlock()

	if err := c.viper.ReadInConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to read configuration file")
		return
	}

	previousAuthSettings := authReloadSettings{
		authDisabled:               c.Config.AuthDisabled,
		iAcknowledgeThisIsABadIdea: c.Config.IAcknowledgeThisIsABadIdea,
		authDisabledAllowedCIDRs:   append([]string(nil), c.Config.AuthDisabledAllowedCIDRs...),
		oidcEnabled:                c.Config.OIDCEnabled,
		oidcAllowedSubject:         c.Config.OIDCAllowedSubject,
		oidcAllowedEmail:           c.Config.OIDCAllowedEmail,
		corsAllowedOrigins:         append([]string(nil), c.Config.CORSAllowedOrigins...),
	}

	if err := c.viper.Unmarshal(c.Config); err != nil {
		log.Error().Err(err).Msg("Failed to reload configuration")
		return
	}
	c.hydrateConfigFromViper()

	c.applyDynamicChanges(previousAuthSettings)
	c.Config.Publish()
}

func (c *AppConfig) PublishConfig() {
	c.configMu.Lock()
	defer c.configMu.Unlock()
	c.Config.Publish()
}

type authReloadSettings struct {
	authDisabled               bool
	iAcknowledgeThisIsABadIdea bool
	authDisabledAllowedCIDRs   []string
	oidcEnabled                bool
	oidcAllowedSubject         string
	oidcAllowedEmail           string
	corsAllowedOrigins         []string
}

func (c *AppConfig) applyDynamicChanges(previousAuthSettings authReloadSettings) {
	c.Config.Version = c.version
	if err := c.ApplyLogConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to apply log configuration")
	}

	if err := c.Config.ValidateOIDCAllowedUser(); err != nil {
		log.Error().Err(err).Msg("OIDC allowed user config is invalid after reload; keeping previous oidcAllowedSubject and oidcAllowedEmail")
		c.Config.OIDCAllowedSubject = previousAuthSettings.oidcAllowedSubject
		c.Config.OIDCAllowedEmail = previousAuthSettings.oidcAllowedEmail
	}
	WarnIfOIDCAllowedUserUnset(c.Config)

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

func WarnIfOIDCAllowedUserUnset(cfg *domain.Config) {
	if cfg.OIDCEnabled && cfg.OIDCAllowedSubject == "" && cfg.OIDCAllowedEmail == "" {
		log.Warn().Msg("neither oidcAllowedSubject nor oidcAllowedEmail is set: any user your OIDC provider authenticates for this client can log in")
	}
}

func (c *AppConfig) hydrateConfigFromViper() {
	c.Config.Host = c.viper.GetString("host")
	c.Config.Port = c.viper.GetInt("port")
	c.Config.BaseURL = httphelpers.NormalizeBasePath(c.viper.GetString("baseUrl")) + "/"
	c.Config.CORSAllowedOrigins = c.getNormalizedStringSlice("corsAllowedOrigins")
	c.Config.SessionSecret = c.viper.GetString("sessionSecret")
	c.Config.SessionCookieSecure = c.viper.GetBool("sessionCookieSecure")

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
	c.Config.ProblemReports = c.viper.GetBool("problemReports")
	c.Config.ProblemReportsURL = c.viper.GetString("problemReportsURL")
	c.Config.DisableSelfUpdate = c.viper.GetBool("disableSelfUpdate")
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
	c.Config.ExternalProgramMaxRunning = c.viper.GetInt("externalProgramMaxRunning")

	c.Config.AuthDisabled = c.viper.GetBool("authDisabled")
	c.Config.IAcknowledgeThisIsABadIdea = c.viper.GetBool("I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA")
	c.Config.AuthDisabledAllowedCIDRs = c.getNormalizedStringSlice("authDisabledAllowedCIDRs")

	c.Config.OIDCEnabled = c.viper.GetBool("oidcEnabled")
	c.Config.OIDCIssuer = c.viper.GetString("oidcIssuer")
	c.Config.OIDCClientID = c.viper.GetString("oidcClientId")
	c.Config.OIDCClientSecret = c.viper.GetString("oidcClientSecret")
	c.Config.OIDCRedirectURL = c.viper.GetString("oidcRedirectUrl")
	c.Config.OIDCDisableBuiltInLogin = c.viper.GetBool("oidcDisableBuiltInLogin")
	c.Config.OIDCAllowedSubject = c.viper.GetString("oidcAllowedSubject")
	c.Config.OIDCAllowedEmail = c.viper.GetString("oidcAllowedEmail")
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
	if _, err := os.Stat(path); err == nil {
		log.Debug().Msgf("Config file already exists at: %s", path)
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}
	log.Debug().Msgf("Created config directory: %s", dir)

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

# Send the browser session cookie only over HTTPS
# Enable this when qui is served through an HTTPS reverse proxy.
# With this enabled, login over plain HTTP does not work.
# An HTTPS oidcRedirectUrl enables it automatically.
#sessionCookieSecure = false

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

# Hide Self-update in the web UI. "qui update" from the shell still works (requires restart)
# Package maintainers who pin the qui version should set this to true.
# Default: false
#disableSelfUpdate = false

# Tracker and peer client icon fetching
# Disable to stop qui from downloading tracker favicons from tracker sites and
# peer client logos from their upstream repositories.
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

# Maximum number of external programs that run at the same time (requires restart)
# Other programs wait for a free slot. A value of 0 or less uses the default.
# Default: 8
#externalProgramMaxRunning = 8

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

# Only allow one OIDC user to log in, by subject identifier (sub) or by the email
# address the provider has verified. Set one of the two, not both. When neither
# is set, any user your provider authenticates for this client can log in.
# The subject is unique per provider; if your provider lets several accounts
# share an email address, all of them can log in with oidcAllowedEmail.
# Environment variable: QUI__OIDC_ALLOWED_SUBJECT
#oidcAllowedSubject = ""

# Environment variable: QUI__OIDC_ALLOWED_EMAIL
#oidcAllowedEmail = ""
`

	data := map[string]any{
		"host":          c.viper.GetString("host"),
		"port":          c.viper.GetInt("port"),
		"sessionSecret": c.viper.GetString("sessionSecret"),
		"logLevel":      c.viper.GetString("logLevel"),
		"logMaxSize":    c.viper.GetInt("logMaxSize"),
		"logMaxBackups": c.viper.GetInt("logMaxBackups"),
	}

	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse config template: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	if absPath, absErr := filepath.Abs(path); absErr == nil {
		path = absPath
	}
	log.Info().Msgf("Created default config file: %s", path)
	return nil
}

func GetDefaultConfigDir() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		if xdgConfig == "/config" {
			return xdgConfig
		}
		return filepath.Join(xdgConfig, "qui")
	}

	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "qui")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "AppData", "Roaming", "qui")
		}
		return filepath.Join(".", "qui")
	default:
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "qui")
		}
		return filepath.Join(".", ".config", "qui")
	}
}

func detectContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/dev/.lxc-boot-id"); err == nil {
		return true
	}
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
	c.logManager.Initialize()

	resolvedPath := c.ResolveLogPath(c.Config.LogPath)

	return c.logManager.Apply(c.Config.LogLevel, resolvedPath, c.Config.LogMaxSize, c.Config.LogMaxBackups)
}

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

func InitDefaultLogger(version string) {
	log.Logger = log.Logger.Output(baseLogWriter(version))
}

func isDevBuild(version string) bool {
	v := strings.ToLower(strings.TrimSpace(version))
	return v == "" || v == "dev" || strings.HasSuffix(v, "-dev")
}

func (c *AppConfig) resolveConfigPath(configDirOrPath string) string {
	if strings.HasSuffix(strings.ToLower(configDirOrPath), ".toml") {
		return configDirOrPath
	}

	if info, err := os.Stat(configDirOrPath); err == nil && !info.IsDir() {
		return configDirOrPath
	}

	return filepath.Join(configDirOrPath, "config.toml")
}

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

func (c *AppConfig) GetConfigPath() string {
	return c.viper.ConfigFileUsed()
}

func (c *AppConfig) GetDatabasePath() string {
	return filepath.Join(c.dataDir, "qui.db")
}

func (c *AppConfig) GetDataDir() string {
	return c.dataDir
}

func (c *AppConfig) SetDataDir(dir string) {
	c.dataDir = dir
}

func (c *AppConfig) GetBackupDir() string {
	dir := strings.TrimSpace(c.Config.Live().BackupDir)
	if dir == "" {
		return filepath.Join(c.dataDir, "backups")
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(c.GetConfigDir(), dir)
	}
	return dir
}

func (c *AppConfig) GetConfigDir() string {
	if c.viper.ConfigFileUsed() != "" {
		return filepath.Dir(c.viper.ConfigFileUsed())
	}
	return GetDefaultConfigDir()
}

func (c *AppConfig) GetCustomThemesDir() string {
	dir := strings.TrimSpace(c.Config.Live().CustomThemesDir)
	if dir == "" {
		return filepath.Join(c.GetConfigDir(), "themes")
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(c.GetConfigDir(), dir)
	}
	return dir
}

func (c *AppConfig) EnsureCustomThemesDir() (string, error) {
	dir := c.GetCustomThemesDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return dir, fmt.Errorf("failed to create custom themes directory %s: %w", dir, err)
	}
	return dir, nil
}

func (c *AppConfig) SetDefaultLogPath(path string) {
	c.configMu.Lock()
	defer c.configMu.Unlock()
	c.viper.SetDefault("logPath", path)
	c.Config.LogPath = c.viper.GetString("logPath")
}

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

const encryptionKeyInfo = "qui credential encryption key"

func WriteDefaultConfig(path string) error {
	c := &AppConfig{
		viper: viper.New(),
	}

	c.defaults()

	return c.writeDefaultConfig(path)
}

func (c *AppConfig) GetEncryptionKey() []byte {
	key, err := hkdf.Key(sha256.New, []byte(c.Config.SessionSecret), nil, encryptionKeyInfo, encryptionKeySize)
	if err != nil {
		log.Fatal().Err(err).Int("length", len(c.Config.SessionSecret)).Msg(
			"sessionSecret is too short to derive the credential encryption key in FIPS 140-only mode. Lengthening it makes stored credentials unreadable, so re-enter them in the UI afterwards")
	}
	return key
}

func (c *AppConfig) GetLegacyEncryptionKey() []byte {
	secret := c.Config.SessionSecret
	if len(secret) >= encryptionKeySize {
		return []byte(secret[:encryptionKeySize])
	}

	padded := make([]byte, encryptionKeySize)
	copy(padded, secret)
	return padded
}

func (c *AppConfig) validateSessionSecret() error {
	if strings.TrimSpace(c.Config.SessionSecret) != "" {
		return nil
	}

	return errors.New("sessionSecret is empty. Set it in config.toml or QUI__SESSION_SECRET to a random value of at least 32 characters. Credentials saved while it was empty will not decrypt under the new value, so enter them again in the UI")
}

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
