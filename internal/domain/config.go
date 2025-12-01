// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

// Config represents the application configuration
type Config struct {
	ExternalProgramAllowList []string `toml:"externalProgramAllowList" mapstructure:"externalProgramAllowList"`

	Version               string
	Host                  string `toml:"host" mapstructure:"host"`
	BaseURL               string `toml:"baseUrl" mapstructure:"baseUrl"`
	SessionSecret         string `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel              string `toml:"logLevel" mapstructure:"logLevel"`
	LogPath               string `toml:"logPath" mapstructure:"logPath"`
	DataDir               string `toml:"dataDir" mapstructure:"dataDir"`
	MetricsHost           string `toml:"metricsHost" mapstructure:"metricsHost"`
	MetricsBasicAuthUsers string `toml:"metricsBasicAuthUsers" mapstructure:"metricsBasicAuthUsers"`

	// OIDC Configuration
	OIDCIssuer       string `toml:"oidcIssuer" mapstructure:"oidcIssuer"`
	OIDCClientID     string `toml:"oidcClientId" mapstructure:"oidcClientId"`
	OIDCClientSecret string `toml:"oidcClientSecret" mapstructure:"oidcClientSecret"`
	OIDCRedirectURL  string `toml:"oidcRedirectUrl" mapstructure:"oidcRedirectUrl"`

	Port          int `toml:"port" mapstructure:"port"`
	LogMaxSize    int `toml:"logMaxSize" mapstructure:"logMaxSize"`
	LogMaxBackups int `toml:"logMaxBackups" mapstructure:"logMaxBackups"`
	MetricsPort   int `toml:"metricsPort" mapstructure:"metricsPort"`

	CheckForUpdates         bool `toml:"checkForUpdates" mapstructure:"checkForUpdates"`
	PprofEnabled            bool `toml:"pprofEnabled" mapstructure:"pprofEnabled"`
	MetricsEnabled          bool `toml:"metricsEnabled" mapstructure:"metricsEnabled"`
	OIDCEnabled             bool `toml:"oidcEnabled" mapstructure:"oidcEnabled"`
	OIDCDisableBuiltInLogin bool `toml:"oidcDisableBuiltInLogin" mapstructure:"oidcDisableBuiltInLogin"`
}
