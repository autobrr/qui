// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops/local"
)

func TestScanner_WindowsJunctionsAreNotMediaFiles(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	scanRoot := filepath.Join(root, "scan")
	release := filepath.Join(scanRoot, "Synthetic Film")
	require.NoError(t, os.MkdirAll(target, 0o750))
	require.NoError(t, os.MkdirAll(release, 0o750))
	payload := []byte("synthetic media")
	require.NoError(t, os.WriteFile(filepath.Join(target, "target.mkv"), payload, 0o600))
	ordinary := filepath.Join(release, "ordinary.mkv")
	require.NoError(t, os.WriteFile(ordinary, payload, 0o600))
	for _, link := range []string{
		filepath.Join(scanRoot, "Directory Mount"),
		filepath.Join(scanRoot, "Root Mount.mkv"),
		filepath.Join(release, "Nested Mount.mkv"),
	} {
		out, err := exec.CommandContext(t.Context(), "cmd.exe", "/d", "/c", "mklink", "/j", link, target).CombinedOutput()
		require.NoError(t, err, "%s", out)
		t.Cleanup(func() { require.NoError(t, os.Remove(link)) })
	}

	result, err := NewScanner(local.NewBackend()).ScanDirectory(t.Context(), scanRoot)
	require.NoError(t, err)
	require.Len(t, result.Searchees, 1, "root-level junctions must not become media files")
	require.Len(t, result.Searchees[0].Files, 1, "nested junctions must not become media files")
	require.Equal(t, ordinary, result.Searchees[0].Files[0].Path)
	require.Equal(t, 1, result.TotalFiles)
	require.EqualValues(t, len(payload), result.TotalSize)
	got, err := os.ReadFile(filepath.Join(target, "target.mkv"))
	require.NoError(t, err)
	require.Equal(t, payload, got)
}
