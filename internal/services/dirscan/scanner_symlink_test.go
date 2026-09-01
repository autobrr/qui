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

// A root-level symlinked media file is scanned via its target (develop
// parity: scanSingleFile follows the link), carrying the target's size and
// identity. Symlinks inside a searchee directory are still skipped by the
// walk.
func TestScanDirectory_ScansRootLevelSymlinksViaTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	root := t.TempDir()
	realFile := filepath.Join(root, "Movie.2024.1080p.WEB.x264-GRP.mkv")
	require.NoError(t, os.WriteFile(realFile, []byte("data"), 0o600))
	linked := filepath.Join(root, "Linked.2024.1080p.WEB.x264-GRP.mkv")
	require.NoError(t, os.Symlink(realFile, linked))

	scanner := NewScanner(localbackend.NewBackend())
	result, err := scanner.ScanDirectory(context.Background(), root)
	require.NoError(t, err)

	require.Len(t, result.Searchees, 2)
	paths := []string{result.Searchees[0].Path, result.Searchees[1].Path}
	require.Contains(t, paths, realFile)
	require.Contains(t, paths, linked)
	for _, s := range result.Searchees {
		if s.Path == linked {
			require.Equal(t, int64(4), s.Files[0].Size, "linked searchee carries the target's metadata")
		}
	}
}
