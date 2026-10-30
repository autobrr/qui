// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package fsops

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// A localized Windows words ERROR_DIR_NOT_EMPTY in its own language, so the
// code has to match on its own. The German text below holds no "not empty".
func TestIsDirNotEmptyMatchesWindowsCodeInAnyLanguage(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("cleanup: %w", &fs.PathError{Op: "remove", Path: `D:\data\x`, Err: windows.ERROR_DIR_NOT_EMPTY})
	require.True(t, IsDirNotEmpty(err))
	require.True(t, IsDirNotEmpty(localizedErrnoError{windows.ERROR_DIR_NOT_EMPTY, "Das Verzeichnis ist nicht leer."}))
	require.False(t, IsDirNotEmpty(&fs.PathError{Op: "remove", Path: `D:\data\Not Empty`, Err: windows.ERROR_ACCESS_DENIED}))
}

// localizedErrnoError is an error whose message is not English but which unwraps
// to the Windows code, the shape a translated system message takes.
type localizedErrnoError struct {
	errno windows.Errno
	msg   string
}

func (e localizedErrnoError) Error() string { return e.msg }
func (e localizedErrnoError) Unwrap() error { return e.errno }
