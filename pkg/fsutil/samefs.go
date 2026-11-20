// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsutil

import (
	"errors"
	"fmt"
	"os"
)

func SameFilesystem(path1, path2 string) (bool, error) {
	if path1 == "" || path2 == "" {
		return false, errors.New("path must not be empty")
	}
	if _, err := os.Stat(path1); err != nil {
		return false, fmt.Errorf("path does not exist: %s: %w", path1, err)
	}
	if _, err := os.Stat(path2); err != nil {
		return false, fmt.Errorf("path does not exist: %s: %w", path2, err)
	}
	return sameFilesystem(path1, path2)
}
