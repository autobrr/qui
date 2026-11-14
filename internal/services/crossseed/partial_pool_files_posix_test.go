//go:build !windows

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePartialPoolPathInsideRootDanglingTarget(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		dir  string
	}{
		{"inside", root},
		{"outside", t.TempDir()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link := filepath.Join(root, tc.name+".mkv")
			require.NoError(t, os.Symlink(filepath.Join(tc.dir, "missing.mkv"), link))
			err := validatePartialPoolPathInsideRoot(root, link)
			require.ErrorIs(t, err, fs.ErrNotExist)
			require.ErrorContains(t, err, "resolve partial pool target")
		})
	}

	t.Run("existing contained link", func(t *testing.T) {
		target := filepath.Join(root, "video.mkv")
		require.NoError(t, os.WriteFile(target, []byte("synthetic payload"), 0o600))
		link := filepath.Join(root, "valid.mkv")
		require.NoError(t, os.Symlink(target, link))
		require.NoError(t, validatePartialPoolPathInsideRoot(root, link))
	})
	t.Run("ordinary missing path", func(t *testing.T) {
		require.NoError(t, validatePartialPoolPathInsideRoot(root, filepath.Join(root, "missing", "video.mkv")))
	})
}
