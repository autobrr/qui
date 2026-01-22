// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"errors"
	"strings"

	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/polar"
)

func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

func isDodoFallbackError(err error) bool {
	return errors.Is(err, dodo.ErrLicenseNotFound) ||
		errors.Is(err, dodo.ErrInvalidLicenseKey)
}

func isPolarDefinitiveError(err error) bool {
	return errors.Is(err, polar.ErrConditionMismatch) ||
		errors.Is(err, polar.ErrActivationLimitExceeded) ||
		errors.Is(err, polar.ErrInvalidLicenseKey)
}
