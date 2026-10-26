// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsDirNotEmpty(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "Season 1")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.mkv"), nil, 0o600))
	removeErr := os.Remove(dir)
	require.Error(t, removeErr)

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"remove of a folder that holds a file", removeErr, true},
		{"wrapped errno", fmt.Errorf("cleanup: %w", &fs.PathError{Op: "remove", Path: "/data/x", Err: syscall.ENOTEMPTY}), true},
		// Only codes count. Every caller passes a backend Remove error, and
		// both backends report the code.
		{"text without a code", &fs.PathError{Op: "remove", Path: `D:\data\x`, Err: errors.New("The directory is not empty.")}, false}, //nolint:revive // Windows's own text
		{"remote server text without a code", fmt.Errorf("remove /data/x: %w", errors.New("directory not empty")), false},
		{"permission error on a folder named Not Empty", &fs.PathError{Op: "remove", Path: "/data/Not Empty", Err: fs.ErrPermission}, false},
	} {
		require.Equal(t, tc.want, IsDirNotEmpty(tc.err), tc.name)
	}
}
