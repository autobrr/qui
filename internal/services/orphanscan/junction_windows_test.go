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
	tfm.Add(aliasFile)
	protected := newTestProtected(t, fsops.HostPaths, nil, tfm)

	t.Run("scan excludes protected junction and finds ordinary orphan", func(t *testing.T) {
		orphans, truncated, err := walkScanRoot(t.Context(), scanRoot, protected, 0, 0, backend, nil)
		require.NoError(t, err)
		require.False(t, truncated)
		paths := make([]string, 0, len(orphans))
		for _, file := range orphans {
			paths = append(paths, file.Path)
		}
		assert.ElementsMatch(t, []string{orphan}, paths, "junctions and their descendants must not be offered as ordinary orphan files")
	})

	t.Run("confirmed deletion preserves the seeded alias", func(t *testing.T) {
		disposition, err := safeDeleteTarget(t.Context(), scanRoot, junction, protected, backend)
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
		disposition, err := safeDeleteTarget(t.Context(), root, scanRoot, protected, backend)
		require.NoError(t, err)
		assert.Equal(t, deleteDispositionSkippedInUse, disposition)
		got, err := os.ReadFile(aliasFile)
		require.NoError(t, err)
		assert.Equal(t, payload, got)
	})

	t.Run("unowned junction cannot be deleted as a regular file", func(t *testing.T) {
		_, err := safeDeleteFile(t.Context(), scanRoot, unownedJunction, protected, backend)
		require.Error(t, err)
		_, err = os.Lstat(unownedJunction)
		require.NoError(t, err)
	})

	t.Run("confirmed deletion still removes ordinary orphan", func(t *testing.T) {
		disposition, err := safeDeleteTarget(t.Context(), scanRoot, orphan, protected, backend)
		require.NoError(t, err)
		assert.Equal(t, deleteDispositionDeleted, disposition)
		_, err = os.Stat(orphan)
		assert.True(t, os.IsNotExist(err))
	})
}

// os.Stat follows a junction, so an empty folder a torrent's save path reaches
// through one is kept by the scan and the delete.
// regression: #3652
func TestOrphanScan_WindowsJunctionKeepsEmptyFolderATorrentReaches(t *testing.T) {
	root := t.TempDir()
	scanRoot := filepath.Join(root, "scan root")
	movie := filepath.Join(scanRoot, "Pack", "Movie")
	leftover := filepath.Join(scanRoot, "leftover")
	require.NoError(t, os.MkdirAll(movie, 0o750))
	require.NoError(t, os.MkdirAll(leftover, 0o750))
	junction := filepath.Join(root, "Media Mount")
	out, err := exec.CommandContext(t.Context(), "cmd.exe", "/d", "/c", "mklink", "/j", junction, movie).CombinedOutput()
	require.NoError(t, err, "creating a real junction: %s", out)
	t.Cleanup(func() {
		if err := os.Remove(junction); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove junction: %v", err)
		}
	})

	backend := newTestBackend()
	reached, err := collectInUseIDs(t.Context(), backend, nil, torrentDirs(fsops.HostPaths, []string{filepath.Join(junction, "x.mkv")}, nil))
	require.NoError(t, err)

	listed := []OrphanFile{{Path: movie, IsAbandonedDir: true}, {Path: filepath.Dir(movie), IsAbandonedDir: true}, {Path: leftover, IsAbandonedDir: true}}
	got := dropDirsTorrentsReach(t.Context(), listed, reached, backend)
	require.Len(t, got, 1)
	assert.Equal(t, leftover, got[0].Path, "the scan keeps the folder the junction reaches and the folder holding it")

	protected := newTestProtected(t, fsops.HostPaths, nil, nil)
	protected.inUse = reached
	disposition, err := safeDeleteEmptyDir(t.Context(), scanRoot, movie, protected, backend)
	require.NoError(t, err)
	assert.Equal(t, deleteDispositionSkippedDirInUseThroughAlias, disposition)
	require.DirExists(t, movie)
}
