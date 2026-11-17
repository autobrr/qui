// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestWin32PathPointer(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`C:\cache\template.db`, `\\?\C:\cache\template.db`},
		{`\\server\share\cache\template.db`, `\\?\UNC\server\share\cache\template.db`},
		{`\\?\C:\cache\template.db`, `\\?\C:\cache\template.db`},
		{`\\?\UNC\server\share\template.db`, `\\?\UNC\server\share\template.db`},
		{`\\.\C:\cache\template.db`, `\\.\C:\cache\template.db`},
	} {
		path, err := Win32PathPointer(tc.input)
		require.NoError(t, err)
		require.Equal(t, tc.want, windows.UTF16PtrToString(path))
	}
}
