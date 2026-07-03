// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"fmt"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

const (
	qbittorrentBadLoginAction     = "qBittorrent authentication failed: stored login data was rejected. Update the instance login details, disable WebUI 2FA for unattended API access, or configure unattended API access."
	qbittorrentAuthRequiredAction = "qBittorrent authentication requires user action: the WebUI requested authentication that qui cannot complete unattended. Update instance login details, disable WebUI 2FA for unattended API access, or configure unattended API access."
	qbittorrentIPBanAction        = "qBittorrent authentication is blocked: the client IP is banned after failed login attempts. Clear or wait out the qBittorrent WebUI ban, then update login details or unattended auth settings."
)

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

	errorStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errorStr, "two-factor") ||
		strings.Contains(errorStr, "2fa") ||
		strings.Contains(errorStr, "mfa") ||
		strings.Contains(errorStr, "otp") ||
		strings.Contains(errorStr, "totp") ||
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
