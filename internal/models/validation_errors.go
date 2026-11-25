// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import "errors"

var (
	ErrBasicAuthPasswordRequired = errors.New("basic_password is required when basic auth is enabled")
)
