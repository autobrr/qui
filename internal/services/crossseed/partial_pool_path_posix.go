//go:build !windows

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "path/filepath"

func resolvePartialPoolExistingPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
