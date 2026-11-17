// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/pathutil"
)

func resolvePartialPoolExistingPath(path string) (string, error) {
	// Resolve the opened object once, including junctions and 8.3 aliases.
	// filepath.EvalSymlinks does not follow junctions marked ModeIrregular.
	pointer, err := pathutil.Win32PathPointer(path)
	if err != nil {
		return "", err
	}
	// Metadata access is sufficient; do not require read access to file contents.
	handle, err := windows.CreateFile(pointer, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	buffer := make([]uint16, 260)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:n]), nil
		}
		buffer = make([]uint16, n+1)
	}
}
