// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package tray

import (
	"fmt"
	"os"
)

// Run does nothing: only Windows has a Tray.
func Run(Menu) {}

// Remove does nothing: only Windows has a Tray.
func Remove() {}

// ShowError prints msg to stderr: only Windows has a Tray build.
func ShowError(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}
