// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/testutil/winreparse"
)

func TestOrphanScan_WindowsNonSurrogateReparseFiles(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "orphan.mkv")
	require.NoError(t, os.WriteFile(file, []byte("synthetic data"), 0o600))
	winreparse.File(t, file, 0x9000001a)
	b := newTestBackend()
	tfm := NewTorrentFileMap(fsops.HostPaths)
	orphans, truncated, err := walkScanRoot(t.Context(), root, tfm, nil, 0, 0, b)
	require.NoError(t, err)
	require.False(t, truncated)
	require.Len(t, orphans, 1)
	require.Equal(t, file, orphans[0].Path)

	tfm.Add(normalizePath(fsops.HostPaths, file))
	disposition, err := safeDeleteFile(t.Context(), root, file, tfm, b)
	require.NoError(t, err)
	require.Equal(t, deleteDispositionSkippedInUse, disposition)
	_, err = os.Lstat(file)
	require.NoError(t, err)

	disposition, err = safeDeleteFile(t.Context(), root, file, NewTorrentFileMap(fsops.HostPaths), b)
	require.NoError(t, err)
	require.Equal(t, deleteDispositionDeleted, disposition)
	_, err = os.Lstat(file)
	require.True(t, os.IsNotExist(err))
}
