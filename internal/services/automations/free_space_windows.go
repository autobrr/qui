// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package automations

import (
	"context"
	"errors"

	"github.com/autobrr/qui/internal/fsops"
)

// backendFreeSpace is unsupported on Windows, matching pre-fsops behavior. The
// backend can serve path-based free space here too; enabling it is a deliberate
// follow-up, not a refactor side effect. Use the qBittorrent path source instead.
func backendFreeSpace(context.Context, fsops.Backend, string) (int64, error) {
	return 0, errors.New("path-based free space source is not supported on Windows")
}
