// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Win32PathPointer returns an absolute UTF-16 path for raw Win32 file APIs.
// Extended prefixes preserve long drive and UNC paths independently of the
// host's long-path setting. Existing extended and device prefixes are retained.
func Win32PathPointer(path string) (*uint16, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(abs, `\\?\`) && !strings.HasPrefix(abs, `\\.\`) {
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + abs[2:]
		} else {
			abs = `\\?\` + abs
		}
	}
	return windows.UTF16PtrFromString(abs)
}
