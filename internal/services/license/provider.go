// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"errors"
	"strings"
)

var (
	ErrDodoClientNotConfigured = errors.New("dodo client not configured")
	ErrLicenseNotActive        = errors.New("license is not active")
)

func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}
