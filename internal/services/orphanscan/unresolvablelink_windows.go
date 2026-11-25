// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package orphanscan

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isUnresolvableLinkError(err error) bool {
	return errors.Is(err, windows.ERROR_CANT_RESOLVE_FILENAME)
}
