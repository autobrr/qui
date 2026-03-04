// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// Config represents the application configuration
type Config struct {
	Version            string
	Host               string   `toml:"host" mapstructure:"host"`
	Port               int      `toml:"port" mapstructure:"port"`
	BaseURL            string   `toml:"baseUrl" mapstructure:"baseUrl"`
	CORSAllowedOrigins []string `toml:"corsAllowedOrigins" mapstructure:"corsAllowedOrigins"`
	//nolint:gosec // Config schema requires this field name; value is provided by runtime configuration.
	SessionSecret            string `toml:"sessionSecret" mapstructure:"sessionSecret"`
	LogLevel                 string `toml:"logLevel" mapstructure:"logLevel"`
	LogPath                  string `toml:"logPath" mapstructure:"logPath"`
	LogMaxSize               int    `toml:"logMaxSize" mapstructure:"logMaxSize"`
	LogMaxBackups            int    `toml:"logMaxBackups" mapstructure:"logMaxBackups"`
	DataDir                  string `toml:"dataDir" mapstructure:"dataDir"`
	CheckForUpdates          bool   `toml:"checkForUpdates" mapstructure:"checkForUpdates"`
	PprofEnabled             bool   `toml:"pprofEnabled" mapstructure:"pprofEnabled"`
	MetricsEnabled           bool   `toml:"metricsEnabled" mapstructure:"metricsEnabled"`
	MetricsHost              string `toml:"metricsHost" mapstructure:"metricsHost"`
	MetricsPort              int    `toml:"metricsPort" mapstructure:"metricsPort"`
	MetricsBasicAuthUsers    string `toml:"metricsBasicAuthUsers" mapstructure:"metricsBasicAuthUsers"`
	TrackerIconsFetchEnabled bool   `toml:"trackerIconsFetchEnabled" mapstructure:"trackerIconsFetchEnabled"`

	ExternalProgramAllowList []string `toml:"externalProgramAllowList" mapstructure:"externalProgramAllowList"`

	// CrossSeedRecoverErroredTorrents enables recovery attempts for errored/missingFiles torrents
	// in cross-seed automation. When enabled, qui will pause, recheck, and resume errored torrents
	// before candidate selection. This can cause automation runs to take 25+ minutes per torrent.
	// When disabled (default), errored torrents are simply excluded from candidate selection.
	CrossSeedRecoverErroredTorrents bool `toml:"crossSeedRecoverErroredTorrents" mapstructure:"crossSeedRecoverErroredTorrents"`

	// AuthDisabled disables all authentication when both QUI__AUTH_DISABLED=true and
	// QUI__I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA=true are set. Intended for deployments behind
	// a reverse proxy that handles authentication. Use IsAuthDisabled() to check.
	AuthDisabled               bool     `toml:"authDisabled" mapstructure:"authDisabled"`
	IAcknowledgeThisIsABadIdea bool     `toml:"I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA" mapstructure:"I_ACKNOWLEDGE_THIS_IS_A_BAD_IDEA"`
	AuthDisabledAllowedCIDRs   []string `toml:"authDisabledAllowedCIDRs" mapstructure:"authDisabledAllowedCIDRs"`

	// OIDC Configuration
	OIDCEnabled             bool   `toml:"oidcEnabled" mapstructure:"oidcEnabled"`
	OIDCIssuer              string `toml:"oidcIssuer" mapstructure:"oidcIssuer"`
	OIDCClientID            string `toml:"oidcClientId" mapstructure:"oidcClientId"`
	OIDCClientSecret        string `toml:"oidcClientSecret" mapstructure:"oidcClientSecret"`
	OIDCRedirectURL         string `toml:"oidcRedirectUrl" mapstructure:"oidcRedirectUrl"`
	OIDCDisableBuiltInLogin bool   `toml:"oidcDisableBuiltInLogin" mapstructure:"oidcDisableBuiltInLogin"`
}

// IsAuthDisabled returns true only when both AuthDisabled and
// IAcknowledgeThisIsABadIdea are set, requiring the operator to explicitly
// acknowledge the risks of running without authentication.
func (c *Config) IsAuthDisabled() bool {
	return c.AuthDisabled && c.IAcknowledgeThisIsABadIdea
}

// ParseAuthDisabledAllowedCIDRs parses configured auth-disabled IP ranges.
// Entries can be either CIDR (for example 192.168.1.0/24) or a single IP
// (for example 192.168.1.10, which is treated as /32 or /128).
func (c *Config) ParseAuthDisabledAllowedCIDRs() ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(c.AuthDisabledAllowedCIDRs))

	for _, raw := range c.AuthDisabledAllowedCIDRs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid authDisabledAllowedCIDRs entry %q: %w", entry, err)
			}
			if prefix != prefix.Masked() {
				return nil, fmt.Errorf("invalid authDisabledAllowedCIDRs entry %q: host bits must be zero for CIDR entries", entry)
			}
			prefixes = append(prefixes, prefix)
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid authDisabledAllowedCIDRs entry %q: %w", entry, err)
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}

	return prefixes, nil
}

// ValidateAuthDisabledConfig validates required settings for auth-disabled mode.
func (c *Config) ValidateAuthDisabledConfig() error {
	if !c.IsAuthDisabled() {
		return nil
	}
	if c.OIDCEnabled {
		return errors.New("OIDC cannot be enabled when authentication is disabled")
	}

	prefixes, err := c.ParseAuthDisabledAllowedCIDRs()
	if err != nil {
		return err
	}
	if len(prefixes) == 0 {
		return errors.New("authDisabledAllowedCIDRs is required when authentication is disabled")
	}

	return nil
}

// NormalizeCORSAllowedOrigins validates and canonicalizes CORS allowlist entries.
// Empty values are ignored, wildcard origins are rejected, and valid entries are
// normalized to browser-style origins (scheme://host[:port]).
func (c *Config) NormalizeCORSAllowedOrigins() error {
	if len(c.CORSAllowedOrigins) == 0 {
		c.CORSAllowedOrigins = nil
		return nil
	}

	normalized := make([]string, 0, len(c.CORSAllowedOrigins))
	seen := make(map[string]struct{}, len(c.CORSAllowedOrigins))

	for _, raw := range c.CORSAllowedOrigins {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		origin, err := normalizeCORSOrigin(entry)
		if err != nil {
			return fmt.Errorf("invalid corsAllowedOrigins entry %q: %w", entry, err)
		}

		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		normalized = append(normalized, origin)
	}

	c.CORSAllowedOrigins = normalized
	return nil
}

func normalizeCORSOrigin(origin string) (string, error) {
	if strings.Contains(origin, "*") {
		return "", errors.New("wildcards are not allowed")
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return "", err
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("scheme must be http or https")
	}

	if parsed.User != nil {
		return "", errors.New("userinfo is not allowed")
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("query and fragment are not allowed")
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("path is not allowed")
	}

	if parsed.Path == "/" {
		return "", errors.New("trailing slash is not allowed")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", errors.New("host is required")
	}

	if strings.Contains(host, "*") {
		return "", errors.New("wildcards are not allowed")
	}

	if parsed.Opaque != "" {
		return "", errors.New("opaque origins are not allowed")
	}

	if _, err := netip.ParseAddr(host); err != nil {
		asciiHost, asciiErr := idna.Lookup.ToASCII(host)
		if asciiErr != nil {
			return "", fmt.Errorf("invalid hostname: %w", asciiErr)
		}
		host = strings.ToLower(asciiHost)
	}

	port := parsed.Port()
	if port != "" {
		portNum, convErr := strconv.Atoi(port)
		if convErr != nil {
			return "", errors.New("port must be numeric")
		}
		if portNum < 1 || portNum > 65535 {
			return "", errors.New("port must be between 1 and 65535")
		}

		if (scheme == "http" && portNum == 80) || (scheme == "https" && portNum == 443) {
			port = ""
		} else {
			port = strconv.Itoa(portNum)
		}
	}

	canonicalHost := host
	if strings.Contains(canonicalHost, ":") {
		canonicalHost = "[" + canonicalHost + "]"
	}

	if port != "" {
		canonicalHost += ":" + port
	}

	return scheme + "://" + canonicalHost, nil
}
