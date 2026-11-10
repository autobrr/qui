// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package winreparse

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/pkg/pathutil"
)

// File attaches a synthetic reparse tag to an existing fixture file.
// It has no provider data, so metadata and deletion work but normal reads do not.
func File(t *testing.T, path string, tag uint32) {
	t.Helper()
	p, err := pathutil.Win32PathPointer(path)
	require.NoError(t, err)
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	require.NoError(t, err)
	defer func() { _ = windows.CloseHandle(h) }()

	data := make([]byte, 8)
	if tag&0x80000000 == 0 {
		// Non-Microsoft tags require a provider GUID as well as the header.
		data = make([]byte, 24)
		copy(data[8:], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	}
	binary.LittleEndian.PutUint32(data, tag)
	var returned uint32
	require.NoError(t, windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT,
		&data[0], uint32(len(data)), nil, 0, &returned, nil))
	fi, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, os.ModeIrregular, fi.Mode().Type())
	var found windows.Win32finddata
	search, err := windows.FindFirstFile(p, &found)
	require.NoError(t, err)
	require.NoError(t, windows.FindClose(search))
	require.Equal(t, tag, found.Reserved0)
}
