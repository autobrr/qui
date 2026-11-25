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
	return windows.MoveFileEx(from, to, 0)
}
