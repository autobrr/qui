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
var sensitiveParams = []string{"apikey", "api_key", "passkey", "token", "password", "authkey", "torrent_pass"}

// sensitiveParamRegex matches sensitive query parameters in a string.
// Used as a fallback when URL parsing fails or for error message redaction.
var sensitiveParamRegex = regexp.MustCompile(`(?i)(apikey|api_key|passkey|token|password|authkey|torrent_pass)=([^&\s]*)`)

// proxyPathRegex matches /proxy/{api-key}/ segments in paths
var proxyPathRegex = regexp.MustCompile(`(/proxy/)([^/]+)(/|$)`)

// pathTokenRegex matches path segments that look like credentials: long,
// unbroken alphanumeric tokens (tracker passkeys, Discord webhook tokens).
// Real path segments (file names, endpoints) virtually always contain dots,
// spaces, or are shorter than this.
var pathTokenRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{24,}$`)

// urlInTextRegex finds http(s) and magnet URLs embedded in free text
// (typically error messages) so each can be redacted individually.
var urlInTextRegex = regexp.MustCompile(`https?://[^\s"'<>]+|magnet:\?[^\s"'<>]+`)

// URLString redacts sensitive query parameter values in a URL string.
// Also redacts userinfo passwords (user:pass@host), proxy path segments,
// credential-shaped path segments, and URLs nested in query values (magnet
// tr= announce URLs, redirect targets).
// If parsing fails, it uses a regex fallback to perform the same redaction.
func URLString(raw string) string {
	if raw == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		// Fallback to regex for unparseable URLs
		return regexRedact(raw)
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

	// Redact credential-shaped path segments (/download/{passkey}/x.torrent,
	// Discord webhook tokens)
	if segments := strings.Split(parsed.Path, "/"); len(segments) > 0 {
		segmentModified := false
		for i, segment := range segments {
			if pathTokenRegex.MatchString(segment) {
				segments[i] = "REDACTED"
				segmentModified = true
			}
		}
		if segmentModified {
			parsed.Path = strings.Join(segments, "/")
			parsed.RawPath = ""
			modified = true
		}
	}

	// Redact sensitive query parameters, and recurse into query values that
	// are themselves URLs (magnet tr= announce URLs carry passkeys)
	query := parsed.Query()
	for key, values := range query {
		redactedWholeParam := false
		for _, param := range sensitiveParams {
			if strings.EqualFold(key, param) {
				// Always redact to exactly one REDACTED value
				query[key] = []string{"REDACTED"}
				modified = true
				redactedWholeParam = true
				break
			}
		}
		if redactedWholeParam {
			continue
		}
		for i, value := range values {
			// Any scheme, not just http(s): magnet tr= values are often
			// udp:// announce URLs carrying a passkey in the path.
			if strings.Contains(value, "://") {
				if redacted := URLString(value); redacted != value {
					values[i] = redacted
					modified = true
				}
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

	if urlErr, ok := errors.AsType[*url.Error](err); ok {
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

// String redacts sensitive information in any string. URLs found in the text
// get full URL redaction (query params, userinfo, path tokens); the remainder
// gets a regex pass for URL fragments.
// This is useful for sanitizing error messages that may contain URLs or URL fragments.
func String(s string) string {
	if s == "" {
		return s
	}
	result := urlInTextRegex.ReplaceAllStringFunc(s, URLString)
	return regexRedact(result)
}

// regexRedact redacts sensitive query params, userinfo passwords, and proxy
// path segments using regexes only. Fallback for unparseable URLs and bare
// URL fragments in text.
func regexRedact(s string) string {
	result := sensitiveParamRegex.ReplaceAllString(s, "${1}=REDACTED")
	result = userinfoPasswordRegex.ReplaceAllString(result, "${1}:REDACTED@")
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
