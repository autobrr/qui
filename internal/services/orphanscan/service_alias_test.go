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

// A hardlink of a torrent's file in another root is kept too: a bind mount or
// a symlinked folder can reach a hardlinked file as well.
func TestExecuteScan_KeepsHardlinkOfInUseFileInAnotherRoot(t *testing.T) {
	for _, category := range []string{"aaa", "zzz"} {
		t.Run(category, func(t *testing.T) {
			var mirror, junk string
			opts := orphanOnlyOptions{
				scanCategoryPaths: true,
				categories: func(defaultSavePath string) map[string]qbt.Category {
					return map[string]qbt.Category{category: {Name: category, SavePath: filepath.Join(filepath.Dir(defaultSavePath), category)}}
				},
			}
			f := newOrphanOnlyFixture(t, "orphanscan-hardlink-alias-"+category, opts, func(defaultSavePath string) {
				categoryRoot := filepath.Join(filepath.Dir(defaultSavePath), category)
				mirror = filepath.Join(categoryRoot, "mirror", "owned.mkv")
				junk = filepath.Join(categoryRoot, "junk.mkv")
				require.NoError(t, os.MkdirAll(filepath.Dir(mirror), 0o750))
				if err := os.Link(filepath.Join(defaultSavePath, "mydata", "owned.mkv"), mirror); err != nil {
					t.Skipf("hardlinks unavailable: %v", err)
				}
				writeFile(t, junk)
			})

			require.Equal(t, []string{junk}, f.listedPaths(t), "neither the hardlink nor its directory may be listed")

			f.svc.executeDeletion(t.Context(), 1, f.runID)
			require.NoFileExists(t, junk)
			require.FileExists(t, mirror)
			require.FileExists(t, f.ownedFile)
		})
	}
}
