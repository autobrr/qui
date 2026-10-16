// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package hardlinktree

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// Windows reports a folder that is not empty by its code. The message text is
// translated on a non-English install, so only the code is checked.
func TestIsDirNotEmptyMatchesWindowsCode(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("rollback: %w", &fs.PathError{Op: "remove", Path: `D:\data\x`, Err: windows.ERROR_DIR_NOT_EMPTY})
	require.True(t, isDirNotEmpty(err))
}
