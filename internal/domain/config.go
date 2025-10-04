// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

// Config represents the application configuration
type Config struct {
	Version               string
	Host                  string `toml:"host" mapstructure:"host"`
	Port                  int    `toml:"port" mapstructure:"port"`
	BaseURL               string `toml:"baseUrl" mapstructure:"baseUrl"`
	SessionSecret         string `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel              string `toml:"logLevel" mapstructure:"logLevel"`
	LogPath               string `toml:"logPath" mapstructure:"logPath"`
	LogMaxSize            int    `toml:"logMaxSize" mapstructure:"logMaxSize"`
	LogMaxBackups         int    `toml:"logMaxBackups" mapstructure:"logMaxBackups"`
	DataDir               string `toml:"dataDir" mapstructure:"dataDir"`
	CheckForUpdates       bool   `toml:"checkForUpdates" mapstructure:"checkForUpdates"`
	PprofEnabled          bool   `toml:"pprofEnabled" mapstructure:"pprofEnabled"`
	MetricsEnabled        bool   `toml:"metricsEnabled" mapstructure:"metricsEnabled"`
	MetricsHost           string `toml:"metricsHost" mapstructure:"metricsHost"`
	MetricsPort           int    `toml:"metricsPort" mapstructure:"metricsPort"`
	MetricsBasicAuthUsers string `toml:"metricsBasicAuthUsers" mapstructure:"metricsBasicAuthUsers"`

	HTTPTimeouts HTTPTimeouts `toml:"httpTimeouts" mapstructure:"httpTimeouts"`
}

// HTTPTimeouts represents HTTP server timeout configuration
type HTTPTimeouts struct {
	ReadTimeout  int `toml:"readTimeout" mapstructure:"readTimeout"`   // seconds
	WriteTimeout int `toml:"writeTimeout" mapstructure:"writeTimeout"` // seconds
	IdleTimeout  int `toml:"idleTimeout" mapstructure:"idleTimeout"`   // seconds
}
