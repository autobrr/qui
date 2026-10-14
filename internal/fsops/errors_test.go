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
		{"Windows text", &fs.PathError{Op: "remove", Path: `D:\data\x`, Err: errors.New("The directory is not empty.")}, true}, //nolint:revive // Windows's own text
		{"permission error on a folder named Not Empty", &fs.PathError{Op: "remove", Path: "/data/Not Empty", Err: fs.ErrPermission}, false},
		{"wrapped permission error on a folder named Not Empty", fmt.Errorf("cleanup: %w", &fs.PathError{Op: "remove", Path: "/data/Not Empty", Err: syscall.EACCES}), false},
		{"rename error naming Not Empty", &os.LinkError{Op: "rename", Old: "/data/Not Empty", New: "/data/b", Err: syscall.EACCES}, false},
	} {
		require.Equal(t, tc.want, IsDirNotEmpty(tc.err), tc.name)
	}
}
