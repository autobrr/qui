// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// aliasWalkBackend spells unix FileIDs.
//go:build !windows

package orphanscan

import (
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

func (f *orphanOnlyFixture) listedPaths(t *testing.T) []string {
	t.Helper()

	files, err := f.store.GetFilesForDeletion(t.Context(), f.runID)
	require.NoError(t, err)
	listed := make([]string, 0, len(files))
	for _, file := range files {
		listed = append(listed, file.FilePath)
	}
	return listed
}

// The alias sits in another scan root, walked before or after the torrent's.
func TestExecuteScan_KeepsAliasOfInUseFileInAnotherRoot(t *testing.T) {
	for _, category := range []string{"aaa", "zzz"} {
		t.Run(category, func(t *testing.T) {
			var mirror, alias, junk string
			opts := orphanOnlyOptions{
				scanCategoryPaths: true,
				categories: func(defaultSavePath string) map[string]qbt.Category {
					return map[string]qbt.Category{category: {Name: category, SavePath: filepath.Join(filepath.Dir(defaultSavePath), category)}}
				},
				wrapBackend: func(b fsops.Backend, defaultSavePath string) fsops.Backend {
					return &aliasWalkBackend{Backend: b, ids: sameFile(filepath.Join(defaultSavePath, "mydata", "owned.mkv"), alias)}
				},
			}
			f := newOrphanOnlyFixture(t, "orphanscan-alias-"+category, opts, func(defaultSavePath string) {
				categoryRoot := filepath.Join(filepath.Dir(defaultSavePath), category)
				mirror = filepath.Join(categoryRoot, "mirror")
				alias = filepath.Join(mirror, "owned.mkv")
				junk = filepath.Join(categoryRoot, "junk.mkv")
				writeFile(t, alias)
				writeFile(t, junk)
			})

			require.Equal(t, []string{junk}, f.listedPaths(t), "neither the alias nor its directory may be listed")

			f.svc.executeDeletion(t.Context(), 1, f.runID)
			require.NoFileExists(t, junk)
			require.FileExists(t, alias)
			require.FileExists(t, f.ownedFile)
		})
	}
}

func TestExecuteScan_ListsOrphanAliasesAcrossRootsOnce(t *testing.T) {
	var first, second string
	opts := orphanOnlyOptions{
		scanCategoryPaths: true,
		categories: func(defaultSavePath string) map[string]qbt.Category {
			return map[string]qbt.Category{"zzz": {Name: "zzz", SavePath: filepath.Join(filepath.Dir(defaultSavePath), "zzz")}}
		},
		wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
			return &aliasWalkBackend{Backend: b, ids: sameFile(first, second)}
		},
	}
	f := newOrphanOnlyFixture(t, "orphanscan-alias-orphans", opts, func(defaultSavePath string) {
		first = filepath.Join(defaultSavePath, "copy.mkv")
		second = filepath.Join(filepath.Dir(defaultSavePath), "zzz", "copy.mkv")
		writeFile(t, first)
		writeFile(t, second)
	})

	require.Equal(t, []string{first}, f.listedPaths(t))
}

// The torrent's save path is itself a symlink. Its files are in use at the
// link's path, so their twins at the target are not orphans, and nothing is
// listed through the link.
func TestExecuteScan_KeepsFilesOfASymlinkedSavePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		link func(defaultSavePath string) string
	}{
		{name: "link inside the default save path", link: func(d string) string { return filepath.Join(d, "mydata") }},
		{name: "link outside pointing in", link: func(d string) string { return filepath.Join(filepath.Dir(d), "aa-out") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var target, junk, stray string
			// The torrent's file is below an ignore path spelled at the link.
			opts := orphanOnlyOptions{
				savePath:        tc.link,
				torrentFileName: "keep/owned.mkv",
				ignorePaths:     func(d string) []string { return []string{filepath.Join(tc.link(d), "keep")} },
			}
			f := newOrphanOnlyFixture(t, "orphanscan-symlinked-save-path", opts, func(defaultSavePath string) {
				link := tc.link(defaultSavePath)
				target = filepath.Join(defaultSavePath, "zz-real")
				require.NoError(t, os.MkdirAll(defaultSavePath, 0o750))
				require.NoError(t, os.Rename(link, target))
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				junk = filepath.Join(defaultSavePath, "junk.mkv")
				stray = filepath.Join(target, "stray.mkv")
				writeFile(t, junk)
				writeFile(t, stray)
			})

			// The stray is found by the walk of the default save path, at its own path.
			require.ElementsMatch(t, []string{junk, stray}, f.listedPaths(t))

			f.svc.executeDeletion(t.Context(), 1, f.runID)
			require.NoFileExists(t, junk)
			require.NoFileExists(t, stray)
			require.FileExists(t, filepath.Join(target, "keep", "owned.mkv"))
		})
	}
}

// A walk of a symlinked root lists nothing, whatever the files below it are:
// an ignore path, another torrent or a hardlink spelled at the target would not
// protect the link's spelling.
func TestScanWalker_SymlinkedRootListsNothing(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	owned := filepath.Join(target, "owned.mkv")
	other := filepath.Join(target, "sub", "other.mkv")
	writeFile(t, owned)
	writeFile(t, other)
	require.NoError(t, os.Link(other, filepath.Join(base, "hardlink.mkv")))
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(filepath.Join(link, "owned.mkv"))

	keys := newFileKeys()
	orphans, dirs, _, err := walkScanRootWithUnitFilter(t.Context(), link, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, 0, nil, newTestBackend(), nil, true, keys)
	require.NoError(t, err)
	require.Empty(t, orphans)
	require.Empty(t, dirs)
	require.Len(t, keys.inUse, 1, "the torrent's file is in use at the link's path")
	require.Empty(t, keys.others)
}
