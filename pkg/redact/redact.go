// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package redact provides utilities for redacting sensitive information from URLs and errors.
package redact

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// sensitiveParams lists query parameter names that should be redacted (case-insensitive).
var sensitiveParams = []string{"apikey", "api_key", "passkey", "token", "password"}

// sensitiveParamRegex matches sensitive query parameters in a string.
// Used as a fallback when URL parsing fails or for error message redaction.
var sensitiveParamRegex = regexp.MustCompile(`(?i)(apikey|api_key|passkey|token|password)=([^&\s]*)`)

// proxyPathRegex matches /proxy/{api-key}/ segments in paths
var proxyPathRegex = regexp.MustCompile(`(/proxy/)([^/]+)(/|$)`)

// URLString redacts sensitive query parameter values in a URL string.
// Also redacts passwords in userinfo (user:pass@host) and proxy path segments.
// If the URL can be parsed, it replaces values of known sensitive parameters with REDACTED.
// If parsing fails, it uses a regex fallback to perform the same redaction.
func URLString(raw string) string {
	if raw == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		// Fallback to regex for unparseable URLs
		return String(raw)
	}

	modified := false

	// Redact userinfo password (user:pass@host -> user:REDACTED@host)
	if parsed.User != nil {
		if _, hasPass := parsed.User.Password(); hasPass {
			parsed.User = url.UserPassword(parsed.User.Username(), "REDACTED")
			modified = true
		}
	}

	// Redact proxy path segments (/proxy/{api-key}/ -> /proxy/REDACTED/)
	if strings.Contains(parsed.Path, "/proxy/") {
		newPath := proxyPathRegex.ReplaceAllString(parsed.Path, "${1}REDACTED${3}")
		if newPath != parsed.Path {
			parsed.Path = newPath
			parsed.RawPath = "" // Clear RawPath to force re-encoding
			modified = true
		}
	}

	// Redact sensitive query parameters
	query := parsed.Query()
	for _, param := range sensitiveParams {
		// Check all case variations - url.Values keys are case-sensitive
		for key := range query {
			if strings.EqualFold(key, param) {
				// Always redact to exactly one REDACTED value
				query[key] = []string{"REDACTED"}
				modified = true
			}
		}
	}

	if !modified {
		return raw
	}

	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// URLError wraps a *url.Error (if present) with a redacted URL.
// If err is or wraps *url.Error, returns a cloned error with the URL redacted.
// Otherwise returns err unchanged.
func URLError(err error) error {
	if err == nil {
		return nil
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Clone the error with redacted URL
		return &url.Error{
			Op:  urlErr.Op,
			URL: URLString(urlErr.URL),
			Err: urlErr.Err,
		}
	}

	return err
}

// userinfoPasswordRegex matches user:password@ patterns in URLs
var userinfoPasswordRegex = regexp.MustCompile(`(://[^/:@\s]+):([^@\s]+)@`)

// String redacts sensitive query parameter values in any string using regex.
// Also redacts userinfo passwords and proxy path segments.
// This is useful for sanitizing error messages that may contain URLs or URL fragments.
func String(s string) string {
	if s == "" {
		return s
	}
	// Redact sensitive query params
	result := sensitiveParamRegex.ReplaceAllString(s, "${1}=REDACTED")
	// Redact userinfo passwords (user:pass@ -> user:REDACTED@)
	result = userinfoPasswordRegex.ReplaceAllString(result, "${1}:REDACTED@")
	// Redact proxy path segments
	result = proxyPathRegex.ReplaceAllString(result, "${1}REDACTED${3}")
	return result
}

// ProxyPath redacts API key segments from proxy paths.
// /proxy/{api-key}/... -> /proxy/REDACTED/...
func ProxyPath(path string) string {
	if path == "" || !strings.Contains(path, "/proxy/") {
		return path
	}
	return proxyPathRegex.ReplaceAllString(path, "${1}REDACTED${3}")
}

// BasicAuthUser redacts the password from a basic auth credential string.
// "user:password" -> "user:REDACTED"
func BasicAuthUser(cred string) string {
	if cred == "" {
		return cred
	}
	idx := strings.Index(cred, ":")
	if idx < 0 {
		return cred // No password part
	}
	return cred[:idx+1] + "REDACTED"
}
