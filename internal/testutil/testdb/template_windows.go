// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/pathutil"
)

func publishTemplate(src, dst string) error {
	from, err := pathutil.Win32PathPointer(src)
	if err != nil {
		return err
	}
	to, err := pathutil.Win32PathPointer(dst)
	if err != nil {
		return err
	}
	// Replacing a winner can make a concurrent reader fail with a sharing
	// violation. Flags 0 atomically publishes only when dst does not exist.
	return windows.MoveFileEx(from, to, 0)
}
