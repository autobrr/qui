// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// aliasWalkBackend spells unix FileIDs.
//go:build !windows

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
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
				categoryFolders: func(defaultSavePath string) []string {
					return []string{filepath.Join(filepath.Dir(defaultSavePath), category)}
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
		categoryFolders: func(defaultSavePath string) []string {
			return []string{filepath.Join(filepath.Dir(defaultSavePath), "zzz")}
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
	inside := func(d string) string { return filepath.Join(d, "mydata") }
	for _, tc := range []struct {
		name   string
		link   func(defaultSavePath string) string
		ignore string // relative to the link
		// hardlink gives the torrent's file a second link outside the scan roots.
		hardlink bool
	}{
		// The torrent's file is below an ignore path spelled at the link.
		{name: "link inside the default save path", link: inside, ignore: "keep"},
		{name: "link outside pointing in", link: func(d string) string { return filepath.Join(filepath.Dir(d), "aa-out") }, ignore: "keep"},
		{name: "link itself ignored", link: inside, ignore: "."},
		{name: "torrent's file has a hardlink", link: inside, ignore: "keep", hardlink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var target, junk, stray string
			opts := orphanOnlyOptions{
				savePath:        tc.link,
				torrentFileName: "keep/owned.mkv",
				ignorePaths:     func(d string) []string { return []string{filepath.Join(tc.link(d), tc.ignore)} },
			}
			f := newOrphanOnlyFixture(t, "orphanscan-symlinked-save-path", opts, func(defaultSavePath string) {
				link := tc.link(defaultSavePath)
				target = filepath.Join(defaultSavePath, "zz-real")
				require.NoError(t, os.MkdirAll(defaultSavePath, 0o750))
				require.NoError(t, os.Rename(link, target))
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if tc.hardlink {
					require.NoError(t, os.Link(filepath.Join(target, "keep", "owned.mkv"), filepath.Join(filepath.Dir(defaultSavePath), "owned.mkv")))
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

// failFollowRootBackend fails each walk that follows its root, as a broken
// mount below the link does.
type failFollowRootBackend struct{ fsops.Backend }

func (b failFollowRootBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	if !opts.FollowRoot {
		return b.Backend.WalkDir(ctx, root, opts)
	}
	ch := make(chan fsops.WalkEntry, 1)
	ch <- fsops.WalkEntry{Path: filepath.Join(root, "broken"), Err: errors.New("input/output error")}
	close(ch)
	return ch, nil
}

// The walk of an ignored symlinked save path can fail after the target is
// walked, and the twins of its files stay listed at the target. The run is then
// partial, so a scheduled run does not delete them by itself. A link whose
// target is gone has no twins, so it does not make the run partial.
func TestExecuteScan_IgnoredSymlinkedSavePathReadError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		missing     bool
		wantPartial bool
	}{
		{name: "read error makes the run partial", wantPartial: true},
		{name: "missing target is not a walk error", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := orphanOnlyOptions{
				scheduled:           true,
				autoCleanupMaxFiles: 100,
				torrentFileName:     "keep/owned.mkv",
				ignorePaths:         func(d string) []string { return []string{filepath.Join(d, "mydata")} },
				wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
					if tc.missing {
						return b
					}
					return failFollowRootBackend{b}
				},
			}
			f := newOrphanOnlyFixture(t, "orphanscan-ignored-link-read-error", opts, func(defaultSavePath string) {
				link := filepath.Join(defaultSavePath, "mydata")
				target := filepath.Join(defaultSavePath, "zz-real")
				if tc.missing {
					require.NoError(t, os.RemoveAll(link))
				} else {
					require.NoError(t, os.Rename(link, target))
				}
				require.NoError(t, os.Symlink(target, link))
			})

			run, err := f.store.GetRun(t.Context(), f.runID)
			require.NoError(t, err)
			// A partial run skips automatic cleanup before it starts a delete.
			require.Equal(t, tc.wantPartial, run.Partial, "run message: %s", run.ErrorMessage)
		})
	}
}

// vanishFollowRootBackend removes gone when a walk that follows its root
// emits its first entry below the root, as a folder deleted mid-walk does.
type vanishFollowRootBackend struct {
	fsops.Backend
	gone string
}

func (b vanishFollowRootBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil || !opts.FollowRoot {
		return ch, err
	}
	out := make(chan fsops.WalkEntry)
	go func() {
		defer close(out)
		removed := false
		for entry := range ch {
			if !removed && entry.Path != root {
				removed = true
				_ = os.RemoveAll(b.gone)
			}
			out <- entry
		}
	}()
	return out, nil
}

// A folder below the target of an ignored symlinked save path can vanish
// while it is walked. It has no files left to protect, so the walk goes on
// and the torrent's file at the target is not listed.
func TestExecuteScan_IgnoredSymlinkedSavePathVanishedFolder(t *testing.T) {
	var target, gone string
	opts := orphanOnlyOptions{
		torrentFileName: "keep/owned.mkv",
		ignorePaths:     func(d string) []string { return []string{filepath.Join(d, "mydata")} },
		wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
			return vanishFollowRootBackend{Backend: b, gone: gone}
		},
	}
	f := newOrphanOnlyFixture(t, "orphanscan-ignored-link-vanished", opts, func(defaultSavePath string) {
		link := filepath.Join(defaultSavePath, "mydata")
		target = filepath.Join(defaultSavePath, "zz-real")
		gone = filepath.Join(target, "ab-gone")
		require.NoError(t, os.Rename(link, target))
		require.NoError(t, os.Symlink(target, link))
		// More files than the walk buffers, so the walk is still in aa-fill
		// when ab-gone is removed.
		for i := range 128 {
			writeFile(t, filepath.Join(target, "aa-fill", fmt.Sprintf("%03d.mkv", i)))
		}
		writeFile(t, filepath.Join(gone, "x.mkv"))
	})
	require.NoDirExists(t, gone)

	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.False(t, run.Partial, "run message: %s", run.ErrorMessage)
	owned := filepath.Join(target, "keep", "owned.mkv")
	require.False(t, slices.Contains(f.listedPaths(t), owned), "the torrent's file is listed")

	f.svc.executeDeletion(t.Context(), 1, f.runID)
	require.FileExists(t, owned)
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

// The walk of a symlinked root skips a denied subtree but fails on a cut
// connection, as the scan walk does, so a cut-short tree never reads as whole.
func TestRecordInUse_Errors(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(target, 0o750))
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	owned := filepath.Join(link, "owned.mkv")
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(owned)
	ownedEntry := fsops.WalkEntry{Path: owned, FileID: hardlink.UnixFileID(1, 2), Nlinks: 1}
	denied := fmt.Errorf("read %s: %w", filepath.Join(link, "locked"), fs.ErrPermission)

	for _, tc := range []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "denied subtree is skipped", err: denied},
		{name: "cut connection fails the walk", err: errors.Join(denied, fsops.ErrConnectionLost), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeWalkBackend{Backend: newTestBackend(), entries: []fsops.WalkEntry{
				{Path: filepath.Join(link, "locked"), Err: tc.err}, ownedEntry,
			}}
			keys := newFileKeys()
			_, _, _, err := walkScanRootWithUnitFilter(t.Context(), link, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, 0, nil, backend, nil, false, keys)
			if tc.wantErr {
				require.ErrorIs(t, err, fsops.ErrConnectionLost)
				return
			}
			require.NoError(t, err)
			require.Len(t, keys.inUse, 1)
		})
	}
}

// A file in use below a symlinked root whose identity cannot be read fails the
// walk, so the run is partial and its twin at the target is not deleted by
// itself. Over sftp no file has an identity, so the walk goes on.
func TestRecordInUse_FileIDErr(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(target, 0o750))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(target, link))
	owned := filepath.Join(link, "owned.mkv")
	tfm := NewTorrentFileMap(fsops.HostPaths)
	tfm.Add(owned)
	entry := fsops.WalkEntry{Path: owned, FileIDErr: errors.New("access is denied")}
	sftp := &models.Instance{SSHHost: "host", SSHKeyEncrypted: "key", SSHHostKeyEncrypted: "hostkey", SSHTier: models.SSHTierSFTP}

	for _, tc := range []struct {
		name     string
		instance *models.Instance
		wantErr  bool
	}{
		{name: "identity backend fails the walk", wantErr: true},
		{name: "sftp walk goes on", instance: sftp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeWalkBackend{Backend: newTestBackend(), entries: []fsops.WalkEntry{entry}}
			_, _, _, err := walkScanRootWithUnitFilter(t.Context(), link, newTestProtected(t, fsops.HostPaths, nil, tfm), 0, 0, nil, backend, tc.instance, false, newFileKeys())
			if tc.wantErr {
				require.ErrorIs(t, err, entry.FileIDErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// A hardlink of a torrent's file in another root is kept too: a bind mount or
// a symlinked folder can reach a hardlinked file as well.
func TestExecuteScan_KeepsHardlinkOfInUseFileInAnotherRoot(t *testing.T) {
	for _, category := range []string{"aaa", "zzz"} {
		t.Run(category, func(t *testing.T) {
			var mirror, junk string
			opts := orphanOnlyOptions{
				scanCategoryPaths: true,
				categoryFolders: func(defaultSavePath string) []string {
					return []string{filepath.Join(filepath.Dir(defaultSavePath), category)}
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

// No walk enters an ignore path, so a torrent's file at or below one is keyed
// by a walk of its own, and a second path to it in this or another root is kept.
func TestExecuteScan_KeepsAliasOfAnIgnoredTorrentFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alias func(defaultSavePath string) string
	}{
		{name: "same root", alias: func(d string) string { return filepath.Join(d, "mydata", "mirror", "owned.mkv") }},
		{name: "another root", alias: func(d string) string { return filepath.Join(filepath.Dir(d), "zzz", "mirror", "owned.mkv") }},
	} {
		for _, ignore := range []string{"keep", filepath.Join("keep", "owned.mkv")} {
			kind := map[bool]string{true: "folder", false: "file"}[ignore == "keep"]
			t.Run(tc.name+", ignoring the "+kind, func(t *testing.T) {
				var alias, junk string
				opts := orphanOnlyOptions{
					scanCategoryPaths: true,
					categoryFolders: func(defaultSavePath string) []string {
						return []string{filepath.Join(filepath.Dir(defaultSavePath), "zzz")}
					},
					torrentFileName: "keep/owned.mkv",
					ignorePaths:     func(d string) []string { return []string{filepath.Join(d, "mydata", ignore)} },
					wrapBackend: func(b fsops.Backend, defaultSavePath string) fsops.Backend {
						return &aliasWalkBackend{Backend: b, ids: sameFile(filepath.Join(defaultSavePath, "mydata", "keep", "owned.mkv"), alias)}
					},
				}
				f := newOrphanOnlyFixture(t, "orphanscan-ignored-alias", opts, func(defaultSavePath string) {
					alias = tc.alias(defaultSavePath)
					junk = filepath.Join(filepath.Dir(defaultSavePath), "zzz", "junk.mkv")
					writeFile(t, alias)
					writeFile(t, junk)
				})

				require.Equal(t, []string{junk}, f.listedPaths(t), "neither the alias nor its directory may be listed")
				run, err := f.store.GetRun(t.Context(), f.runID)
				require.NoError(t, err)
				require.True(t, run.IdentityComplete)
			})
		}
	}
}

// An ignore path that is or holds a torrent's file is often unmounted
// (#2483), so a missing one is no walk error. Any other error leaves the twins
// of its files unprotected, so the run is partial and no cleanup runs by itself.
func TestExecuteScan_IgnoredTorrentDirWalkErrors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		missing     bool
		wantPartial bool
	}{
		{name: "read error makes the run partial", wantPartial: true},
		{name: "missing ignore path is not a walk error", missing: true},
	} {
		for _, ignore := range []string{"keep", filepath.Join("keep", "owned.mkv")} {
			kind := map[bool]string{true: "folder", false: "file"}[ignore == "keep"]
			t.Run(tc.name+", ignoring the "+kind, func(t *testing.T) {
				opts := orphanOnlyOptions{
					scheduled:           true,
					autoCleanupMaxFiles: 100,
					torrentFileName:     "keep/owned.mkv",
					ignorePaths:         func(d string) []string { return []string{filepath.Join(d, "mydata", ignore)} },
					wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
						if tc.missing {
							return b
						}
						return failFollowRootBackend{b}
					},
				}
				f := newOrphanOnlyFixture(t, "orphanscan-ignored-torrent-dir-error", opts, func(defaultSavePath string) {
					if tc.missing {
						require.NoError(t, os.RemoveAll(filepath.Join(defaultSavePath, "mydata", ignore)))
					}
				})

				run, err := f.store.GetRun(t.Context(), f.runID)
				require.NoError(t, err)
				require.Equal(t, tc.wantPartial, run.Partial, "run message: %s", run.ErrorMessage)
				require.Equal(t, !tc.wantPartial, run.IdentityComplete)
			})
		}
	}
}
