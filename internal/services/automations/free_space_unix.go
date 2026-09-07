// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package automations

import (
	"context"
	"errors"
	"fmt"

	"github.com/autobrr/qui/internal/fsops"
)

// backendFreeSpace reads free space from a local (or remote agent) filesystem path.
func backendFreeSpace(ctx context.Context, backend fsops.Backend, path string) (int64, error) {
	if backend == nil {
		return 0, errors.New("backend is required for path-based free space source")
	}

	result, err := backend.Statfs(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("failed to get filesystem stats for %s: %w", path, err)
	}

	return result.BytesAvailable, nil
}
