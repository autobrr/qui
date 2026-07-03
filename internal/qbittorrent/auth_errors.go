// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

const (
	qbittorrentBadLoginAction     = "qBittorrent authentication failed: stored login data was rejected. Update the instance login details, disable WebUI 2FA for unattended API access, or configure unattended API access."
	qbittorrentAuthRequiredAction = "qBittorrent authentication requires user action: the WebUI requested authentication that qui cannot complete unattended. Update instance login details, disable WebUI 2FA for unattended API access, or configure unattended API access."
	qbittorrentIPBanAction        = "qBittorrent authentication is blocked: the client IP is banned after failed login attempts. Clear or wait out the qBittorrent WebUI ban, then update login details or unattended auth settings."
)

// twoFactorPattern requires word boundaries so the short tokens only match
// standalone mentions, not substrings of hostnames or identifiers. URLs are
// stripped with urlPattern (tracker_statuses.go) before matching so instance
// hosts and paths embedded in transport errors (e.g. Get
// "http://otp-gateway.local/...") cannot trip auth-failure classification.
var twoFactorPattern = regexp.MustCompile(`\b(?:two-factor|2fa|mfa|totp|otp)\b`)

// ActionableAuthFailureMessage maps qBittorrent authentication failures to
// user-facing remediation text while preserving the original error for logs.
func ActionableAuthFailureMessage(err error) (string, bool) {
	if err == nil {
		return "", false
	}

	switch {
	case errors.Is(err, qbt.ErrBadCredentials):
		return qbittorrentBadLoginAction, true
	case errors.Is(err, qbt.ErrIPBanned):
		return qbittorrentIPBanAction, true
	}

	errorStr := strings.ToLower(urlPattern.ReplaceAllString(err.Error(), " "))
	switch {
	case twoFactorPattern.MatchString(errorStr) ||
		strings.Contains(errorStr, "auth required") ||
		strings.Contains(errorStr, "authentication required") ||
		strings.Contains(errorStr, "login required") ||
		strings.Contains(errorStr, "status code: 401"):
		return qbittorrentAuthRequiredAction, true
	default:
		return "", false
	}
}

// ActionableInstanceError preserves the original error chain while making stored
// instance errors actionable on REST and SSE metadata surfaces.
func ActionableInstanceError(err error) error {
	if message, ok := ActionableAuthFailureMessage(err); ok {
		return fmt.Errorf("%s: %w", message, err)
	}
	return err
}
