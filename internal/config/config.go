package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"github.com/autobrr/qui/internal/domain"
)

type AppConfig struct {
	Config       *domain.Config
	viper        *viper.Viper
	databasePath string
}

func New(configPath string) (*AppConfig, error) {
	c := &AppConfig{
		viper:  viper.New(),
		Config: &domain.Config{},
	}

	// Set defaults
	c.defaults()

	// Load from config file
	if err := c.load(configPath); err != nil {
		return nil, err
	}

	// Override with environment variables
	c.loadFromEnv()

	// Unmarshal the configuration
	if err := c.viper.Unmarshal(c.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

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
	sessionSecret := generateSecureToken(32)

	c.viper.SetDefault("host", host)
	c.viper.SetDefault("port", 8080)
	c.viper.SetDefault("baseUrl", "")
	c.viper.SetDefault("sessionSecret", sessionSecret)
	c.viper.SetDefault("logLevel", "INFO")
	c.viper.SetDefault("logPath", "")
}

func (c *AppConfig) load(configPath string) error {
	c.viper.SetConfigType("toml")

	if configPath != "" {
		// Use provided config path from --config flag
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
				// Explicitly set database path for newly created config
				configDir := filepath.Dir(configPath)
				c.databasePath = filepath.Join(configDir, "qui.db")
				log.Info().Msgf("Database path set to: %s (new config)", c.databasePath)
				return nil
			}
			return fmt.Errorf("failed to read config: %w", err)
		}
	} else {
		// Search for config in standard locations
		c.viper.SetConfigName("config")
		c.viper.AddConfigPath(".")                   // Current directory
		c.viper.AddConfigPath(getDefaultConfigDir()) // OS-specific config directory

		// Try to read existing config
		if err := c.viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				// No config found, create in OS-specific location
				defaultConfigPath := filepath.Join(getDefaultConfigDir(), "config.toml")
				if err := c.writeDefaultConfig(defaultConfigPath); err != nil {
					return err
				}
				// Set the config file explicitly and read it
				c.viper.SetConfigFile(defaultConfigPath)
				if err := c.viper.ReadInConfig(); err != nil {
					return fmt.Errorf("failed to read newly created config: %w", err)
				}
				// Explicitly set database path for newly created config
				configDir := filepath.Dir(defaultConfigPath)
				c.databasePath = filepath.Join(configDir, "qui.db")
				log.Info().Msgf("Database path set to: %s (new config)", c.databasePath)
				return nil
			}
			return fmt.Errorf("failed to read config: %w", err)
		}
	}

	// Set database path to be next to the config file
	if c.viper.ConfigFileUsed() != "" {
		configDir := filepath.Dir(c.viper.ConfigFileUsed())
		c.databasePath = filepath.Join(configDir, "qui.db")
		log.Info().Msgf("Database path set to: %s (existing config)", c.databasePath)
	} else {
		// Fallback to current directory if no config file
		c.databasePath = "qui.db"
		log.Warn().Msg("No config file found, using current directory for database")
	}

	return nil
}

func (c *AppConfig) loadFromEnv() {
	// DO NOT use AutomaticEnv() - it reads ALL env vars and causes conflicts with K8s
	// Instead, explicitly bind only the environment variables we want
	
	// Use double underscore to avoid conflicts with K8s deployment_PORT patterns
	c.viper.BindEnv("host", "QUI__HOST")
	c.viper.BindEnv("port", "QUI__PORT")
	c.viper.BindEnv("baseUrl", "QUI__BASE_URL")
	c.viper.BindEnv("sessionSecret", "QUI__SESSION_SECRET")
	c.viper.BindEnv("logLevel", "QUI__LOG_LEVEL")
	c.viper.BindEnv("logPath", "QUI__LOG_PATH")
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
	// Update log level dynamically
	setLogLevel(c.Config.LogLevel)

	// Update log output if path changed
	if c.Config.LogPath != "" {
		if err := setupLogFile(c.Config.LogPath); err != nil {
			log.Error().Err(err).Msg("Failed to update log file")
		}
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
# Default: 8080
port = {{ .port }}

# Base URL
# Set custom baseUrl eg /qui/ to serve in subdirectory.
# Not needed for subdomain, or by accessing with :port directly.
# Optional
#baseUrl = "/qui/"

# Session secret
# Auto-generated if not provided
sessionSecret = "{{ .sessionSecret }}"

# Log file path
# If not defined, logs to stdout
# Optional
#logPath = "log/qui.log"

# Log level
# Default: "INFO"
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
logLevel = "{{ .logLevel }}"
`

	// Prepare template data
	data := map[string]interface{}{
		"host":          c.viper.GetString("host"),
		"port":          c.viper.GetInt("port"),
		"sessionSecret": c.viper.GetString("sessionSecret"),
		"logLevel":      c.viper.GetString("logLevel"),
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

// getDefaultConfigDir returns the OS-specific config directory
func getDefaultConfigDir() string {
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

func generateSecureToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a less secure method if crypto/rand fails
		log.Warn().Err(err).Msg("Failed to generate secure token, using fallback")
		return "change-me-" + fmt.Sprintf("%d", os.Getpid())
	}
	return hex.EncodeToString(bytes)
}

func setLogLevel(level string) {
	switch strings.ToUpper(level) {
	case "TRACE":
		log.Logger = log.Level(-1) // zerolog.TraceLevel
	case "DEBUG":
		log.Logger = log.Level(0) // zerolog.DebugLevel
	case "INFO":
		log.Logger = log.Level(1) // zerolog.InfoLevel
	case "WARN":
		log.Logger = log.Level(2) // zerolog.WarnLevel
	case "ERROR":
		log.Logger = log.Level(3) // zerolog.ErrorLevel
	default:
		log.Logger = log.Level(1) // Default to INFO
	}
}

func setupLogFile(path string) error {
	// Create log directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Update logger output
	log.Logger = log.Output(f)
	return nil
}

// GetDatabasePath returns the path to the database file
func (c *AppConfig) GetDatabasePath() string {
	return c.databasePath
}

// ApplyLogConfig applies the log level and log file configuration
func (c *AppConfig) ApplyLogConfig() {
	// Set log level
	setLogLevel(c.Config.LogLevel)

	// Set log file if configured
	if c.Config.LogPath != "" {
		if err := setupLogFile(c.Config.LogPath); err != nil {
			log.Error().Err(err).Msg("Failed to setup log file")
		}
	}
}

// GetEncryptionKey derives a 32-byte encryption key from the session secret
func (c *AppConfig) GetEncryptionKey() []byte {
	// Use first 32 bytes of session secret as encryption key
	// In production, you might want to derive this differently
	secret := c.Config.SessionSecret
	if len(secret) >= 32 {
		return []byte(secret[:32])
	}

	// Pad the secret if it's too short
	padded := make([]byte, 32)
	copy(padded, []byte(secret))
	return padded
}
