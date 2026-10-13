// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

func TestOrphanScan_WindowsJunctionProtectsSeededDescendant(t *testing.T) {
	root := t.TempDir()
	scanRoot := filepath.Join(root, "scan root")
	target := filepath.Join(root, "seeded target")
	require.NoError(t, os.MkdirAll(scanRoot, 0o750))
	require.NoError(t, os.MkdirAll(target, 0o750))
	payload := []byte("synthetic seeded data")
	targetFile := filepath.Join(target, "seeded.mkv")
	require.NoError(t, os.WriteFile(targetFile, payload, 0o600))
	orphan := filepath.Join(scanRoot, "orphan.txt")
	require.NoError(t, os.WriteFile(orphan, []byte("unowned data"), 0o600))
	junction := filepath.Join(scanRoot, "Media Mount")
	unownedJunction := filepath.Join(scanRoot, "Unowned Alias")
	for _, link := range []string{junction, unownedJunction} {
		out, err := exec.CommandContext(t.Context(), "cmd.exe", "/d", "/c", "mklink", "/j", link, target).CombinedOutput()
		require.NoError(t, err, "creating a real junction: %s", out)
		t.Cleanup(func() {
			if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
				t.Errorf("remove junction: %v", err)
			}
		})
	}
	aliasFile := filepath.Join(junction, "seeded.mkv")
	aliasInfo, err := os.Stat(aliasFile)
	require.NoError(t, err)
	targetInfo, err := os.Stat(targetFile)
	require.NoError(t, err)
	require.True(t, os.SameFile(aliasInfo, targetInfo))

	backend := newTestBackend()
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(normalizePath(fsops.HostPaths, aliasFile))

	t.Run("scan excludes protected junction and finds ordinary orphan", func(t *testing.T) {
		orphans, truncated, err := walkScanRoot(t.Context(), scanRoot, tfm, nil, 0, 0, backend)
		require.NoError(t, err)
		require.False(t, truncated)
		paths := make([]string, 0, len(orphans))
		for _, file := range orphans {
			paths = append(paths, file.Path)
		}
		assert.ElementsMatch(t, []string{orphan}, paths, "junctions and their descendants must not be offered as ordinary orphan files")
	})

	t.Run("confirmed deletion preserves the seeded alias", func(t *testing.T) {
		disposition, err := safeDeleteTarget(t.Context(), scanRoot, junction, tfm, nil, backend)
		t.Logf("junction deletion: disposition=%d error=%v", disposition, err)
		assert.False(t, err == nil && disposition == deleteDispositionDeleted, "a protected junction must not be deleted")
		for _, path := range []string{aliasFile, targetFile} {
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Errorf("seeded path must remain usable: %v", readErr)
				continue
			}
			assert.Equal(t, payload, got)
		}
	})

	t.Run("deletion of the containing directory preserves the seeded alias", func(t *testing.T) {
		disposition, err := safeDeleteTarget(t.Context(), root, scanRoot, tfm, nil, backend)
		require.NoError(t, err)
		assert.Equal(t, deleteDispositionSkippedInUse, disposition)
		got, err := os.ReadFile(aliasFile)
		require.NoError(t, err)
		assert.Equal(t, payload, got)
	})

	t.Run("unowned junction cannot be deleted as a regular file", func(t *testing.T) {
		_, err := safeDeleteFile(t.Context(), scanRoot, unownedJunction, tfm, backend)
		require.Error(t, err)
		_, err = os.Lstat(unownedJunction)
		require.NoError(t, err)
	})

	t.Run("confirmed deletion still removes ordinary orphan", func(t *testing.T) {
		disposition, err := safeDeleteTarget(t.Context(), scanRoot, orphan, tfm, nil, backend)
		require.NoError(t, err)
		assert.Equal(t, deleteDispositionDeleted, disposition)
		_, err = os.Stat(orphan)
		assert.True(t, os.IsNotExist(err))
	})
}
