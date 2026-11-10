// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/testutil/winreparse"
)

func TestScanner_WindowsNonSurrogateReparseFiles(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "Synthetic Film")
	require.NoError(t, os.Mkdir(release, 0o750))
	for _, file := range []string{filepath.Join(root, "single.mkv"), filepath.Join(release, "nested.mkv")} {
		require.NoError(t, os.WriteFile(file, []byte("synthetic data"), 0o600))
		winreparse.File(t, file, 0x9000001a)
	}
	result, err := NewScanner(local.NewBackend()).ScanDirectory(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, result.Searchees, 2)
	require.Equal(t, 2, result.TotalFiles)
	require.EqualValues(t, 28, result.TotalSize)
}
