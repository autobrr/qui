//go:build !windows

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import "os"

func publishTemplate(src, dst string) error {
	return os.Rename(src, dst)
}
