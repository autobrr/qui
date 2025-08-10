// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package domain

// Config represents the application configuration
type Config struct {
	Host          string       `toml:"host" mapstructure:"host"`
	Port          int          `toml:"port" mapstructure:"port"`
	BaseURL       string       `toml:"baseUrl" mapstructure:"baseUrl"`
	SessionSecret string       `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel      string       `toml:"logLevel" mapstructure:"logLevel"`
	LogPath       string       `toml:"logPath" mapstructure:"logPath"`
	Polar         PolarConfig  `toml:"polar" mapstructure:"polar"`
	HTTPTimeouts  HTTPTimeouts `toml:"httpTimeouts" mapstructure:"httpTimeouts"`
}

// HTTPTimeouts represents HTTP server timeout configuration
type HTTPTimeouts struct {
	ReadTimeout  int `toml:"readTimeout" mapstructure:"readTimeout"`   // seconds
	WriteTimeout int `toml:"writeTimeout" mapstructure:"writeTimeout"` // seconds
	IdleTimeout  int `toml:"idleTimeout" mapstructure:"idleTimeout"`   // seconds
}

// PolarConfig represents user-configurable Polar settings
// Publisher credentials (AccessToken, OrganizationID, Environment) are baked into the binary
type PolarConfig struct {
	LicenseValidationInterval string `toml:"licenseValidationInterval" mapstructure:"licenseValidationInterval"`
	OfflineGracePeriod        string `toml:"offlineGracePeriod" mapstructure:"offlineGracePeriod"`
}
