// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package orphanscan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
)

func TestSafeDeleteTarget_ProtectsSymlinkedTorrentDescendant(t *testing.T) {
	for _, selection := range []string{"symlink", "containing directory"} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			container := filepath.Join(root, "container")
			require.NoError(t, os.Mkdir(target, 0o750))
			require.NoError(t, os.Mkdir(container, 0o750))
			payload := []byte("synthetic seeded data")
			targetFile := filepath.Join(target, "seeded.mkv")
			require.NoError(t, os.WriteFile(targetFile, payload, 0o600))
			link := filepath.Join(container, "alias")
			require.NoError(t, os.Symlink(target, link))
			aliasFile := filepath.Join(link, "seeded.mkv")
			selected := link
			if selection == "containing directory" {
				selected = container
			}
			tfm := NewTorrentFileMap(fsops.HostPaths)
			tfm.Add(aliasFile)
			backend := local.NewBackend()
			disposition, err := safeDeleteTarget(t.Context(), root, selected, tfm, nil, backend)
			require.NoError(t, err)
			require.Equal(t, deleteDispositionSkippedInUse, disposition)
			for _, path := range []string{aliasFile, targetFile} {
				got, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, payload, got)
			}

			disposition, err = safeDeleteTarget(t.Context(), root, selected, NewTorrentFileMap(fsops.HostPaths), nil, backend)
			require.NoError(t, err)
			require.Equal(t, deleteDispositionDeleted, disposition, "unowned links remain removable")
			_, err = os.Lstat(selected)
			require.True(t, os.IsNotExist(err))
			got, err := os.ReadFile(targetFile)
			require.NoError(t, err)
			require.Equal(t, payload, got, "deletion never follows the link")
		})
	}
}
