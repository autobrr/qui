// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import "encoding/json"

// Config represents the application configuration
type Config struct {
	Host          string `toml:"host" mapstructure:"host"`
	Port          int    `toml:"port" mapstructure:"port"`
	BaseURL       string `toml:"baseUrl" mapstructure:"baseUrl"`
	SessionSecret string `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel      string `toml:"logLevel" mapstructure:"logLevel"`
	LogPath       string `toml:"logPath" mapstructure:"logPath"`
	DataDir       string `toml:"dataDir" mapstructure:"dataDir"`
	PprofEnabled  bool   `toml:"pprofEnabled" mapstructure:"pprofEnabled"`

	HTTPTimeouts HTTPTimeouts `toml:"httpTimeouts" mapstructure:"httpTimeouts"`
}

func (c Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct {
		*Alias
		SessionSecret string `json:"sessionSecret"`
	}{
		SessionSecret: RedactString(c.SessionSecret),
		Alias:         (*Alias)(&c),
	})
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// If the session secret appears to be redacted, don't overwrite the existing value
	if IsRedactedValue(c.SessionSecret) {
		// Keep the original session secret by not updating it
	}

	return nil
}

// HTTPTimeouts represents HTTP server timeout configuration
type HTTPTimeouts struct {
	ReadTimeout  int `toml:"readTimeout" mapstructure:"readTimeout"`   // seconds
	WriteTimeout int `toml:"writeTimeout" mapstructure:"writeTimeout"` // seconds
	IdleTimeout  int `toml:"idleTimeout" mapstructure:"idleTimeout"`   // seconds
}
