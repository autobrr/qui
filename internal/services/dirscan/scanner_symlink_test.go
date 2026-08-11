// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	localbackend "github.com/autobrr/qui/internal/fsops/local"
)

// A root-level symlinked file must not become a searchee, matching the
// directory walk, which already skips symlinks.
func TestScanDirectory_SkipsRootLevelSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	root := t.TempDir()
	realFile := filepath.Join(root, "Movie.2024.1080p.WEB.x264-GRP.mkv")
	require.NoError(t, os.WriteFile(realFile, []byte("data"), 0o600))
	require.NoError(t, os.Symlink(realFile, filepath.Join(root, "Linked.2024.1080p.WEB.x264-GRP.mkv")))

	scanner := NewScanner(localbackend.NewBackend())
	result, err := scanner.ScanDirectory(context.Background(), root)
	require.NoError(t, err)

	require.Len(t, result.Searchees, 1)
	require.Equal(t, realFile, result.Searchees[0].Path)
}
