// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
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

func TestExecuteScan_KeepsFilesOfASymlinkedSavePath(t *testing.T) {
	inside := func(d string) string { return filepath.Join(d, "mydata") }
	for _, tc := range []struct {
		name     string
		link     func(defaultSavePath string) string
		ignore   string
		hardlink bool
	}{
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

			require.ElementsMatch(t, []string{junk, stray}, f.listedPaths(t))

			f.svc.executeDeletion(t.Context(), 1, f.runID)
			require.NoFileExists(t, junk)
			require.NoFileExists(t, stray)
			require.FileExists(t, filepath.Join(target, "keep", "owned.mkv"))
		})
	}
}

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
			require.Equal(t, tc.wantPartial, run.Partial, "run message: %s", run.ErrorMessage)
		})
	}
}

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

func TestExecuteScan_ListsHardlinkOfInUseFileInAnotherRoot(t *testing.T) {
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

			require.ElementsMatch(t, []string{junk, mirror, filepath.Dir(mirror)}, f.listedPaths(t))

			f.svc.executeDeletion(t.Context(), 1, f.runID)
			require.NoFileExists(t, junk)
			require.NoFileExists(t, mirror)
			require.FileExists(t, f.ownedFile)
		})
	}
}

func TestExecuteScan_KeepsAliasOfAHardlinkedTorrentFileThroughASymlinkedFolder(t *testing.T) {
	for _, category := range []string{"aaa", "zzz"} {
		t.Run(category, func(t *testing.T) {
			var alias, junk string
			opts := orphanOnlyOptions{
				scanCategoryPaths: true,
				categoryFolders: func(defaultSavePath string) []string {
					return []string{filepath.Join(filepath.Dir(defaultSavePath), category, "mydata")}
				},
			}
			f := newOrphanOnlyFixture(t, "orphanscan-hardlinked-alias-"+category, opts, func(defaultSavePath string) {
				base := filepath.Dir(defaultSavePath)
				owned := filepath.Join(defaultSavePath, "mydata", "owned.mkv")
				require.NoError(t, os.Link(owned, filepath.Join(base, "owned.mkv")))
				symlinkOrSkip(t, defaultSavePath, filepath.Join(base, category))
				alias = filepath.Join(base, category, "mydata", "owned.mkv")
				junk = filepath.Join(defaultSavePath, "junk.mkv")
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

func linkedPeerFixture(t *testing.T, f *modeFixture, path func(string) string, rel string) (movie string) {
	t.Helper()

	root := filepath.Dir(f.orphan)
	target := filepath.Join(root, "real")
	require.NoError(t, os.MkdirAll(target, 0o750))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "linked")))
	movie = filepath.Join(root, strings.Replace(rel, "linked", "real", 1), "movie.mkv")
	writeFile(t, movie)

	sync := stubSync(f.svc)
	torrents, files := sync.getAllTorrents, sync.getTorrentFilesBatch
	sync.getAllTorrents = func(ctx context.Context, id int) ([]qbt.Torrent, error) {
		if id == 2 {
			return []qbt.Torrent{{Hash: "peer", SavePath: path(filepath.Join(root, rel)), State: qbt.TorrentStatePausedUp}}, nil
		}
		return torrents(ctx, id)
	}
	sync.getTorrentFilesBatch = func(ctx context.Context, id int, hashes []string) (map[string]qbt.TorrentFiles, error) {
		if id == 2 {
			return map[string]qbt.TorrentFiles{"peer": {{Name: "movie.mkv", Size: 1}}}, nil
		}
		return files(ctx, id, hashes)
	}
	sync.listInstances = func(ctx context.Context) ([]*models.Instance, error) {
		inst, err := f.instance.Get(ctx, 1)
		if err != nil {
			return nil, err
		}
		peer := *inst
		peer.ID, peer.Name = 2, "peer"
		return []*models.Instance{inst, &peer}, nil
	}
	return movie
}

func TestExecuteScan_KeepsFilesOfAPeerBehindASymlink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote bool
		fold   bool
		twin   bool
		rel    string
	}{
		{name: "local, link is the save path", rel: "linked"},
		{name: "local, save path below a link", rel: "linked/sub"},
		{name: "local, peer spells the scan root in another case", fold: true, rel: "linked"},
		{name: "local, scan root differs from the link only in case", twin: true, rel: "linked"},
		{name: "exec tier, link is the save path", remote: true, rel: "linked"},
		{name: "exec tier, save path below a link", remote: true, rel: "linked/sub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f *modeFixture
			path := func(p string) string { return p }
			if tc.remote {
				f, _ = newSSHFixture(t, "orphanscan-linked-peer-remote", sshtest.ExecGNU, models.SSHTierExec)
				path = sftpPath
			} else {
				f = newModeFixture(t, "orphanscan-linked-peer-local", models.FilesystemModeLocal, nil)
			}
			save := path
			if tc.fold {
				root := filepath.Dir(f.orphan)
				parent := filepath.Dir(root)
				foldedParent := filepath.Join(filepath.Dir(parent), strings.ToUpper(filepath.Base(parent)))
				require.NotEqual(t, parent, foldedParent)
				require.NoError(t, os.Symlink(parent, foldedParent))
				t.Cleanup(func() { _ = os.Remove(foldedParent) })
				folded := filepath.Join(foldedParent, filepath.Base(root))
				save = func(p string) string { return strings.Replace(p, root, folded, 1) }
			}
			movie := linkedPeerFixture(t, f, save, tc.rel)
			if tc.twin {
				twin := filepath.Join(filepath.Dir(f.orphan), "LINKED")
				writeFile(t, filepath.Join(twin, "keep.mkv"))
				sync := stubSync(f.svc)
				torrents, files := sync.getAllTorrents, sync.getTorrentFilesBatch
				sync.getAllTorrents = func(ctx context.Context, id int) ([]qbt.Torrent, error) {
					list, err := torrents(ctx, id)
					if id == 1 {
						list = append(list, qbt.Torrent{Hash: "twin", SavePath: twin, State: qbt.TorrentStatePausedUp})
					}
					return list, err
				}
				sync.getTorrentFilesBatch = func(ctx context.Context, id int, hashes []string) (map[string]qbt.TorrentFiles, error) {
					m, err := files(ctx, id, hashes)
					if id == 1 && slices.Contains(hashes, "twin") {
						m["twin"] = qbt.TorrentFiles{{Name: "keep.mkv", Size: 1}}
					}
					return m, err
				}
			}

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.True(t, run.IdentityComplete)
			files, err := f.store.GetFilesForDeletion(t.Context(), run.ID)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, path(f.orphan), files[0].FilePath)

			require.NoError(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID))
			f.requireCompleted(t)
			require.NoFileExists(t, f.orphan)
			require.FileExists(t, movie)
		})
	}
}

func TestExecuteScan_PeerBehindASymlinkWalk(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rel         string
		missing     bool
		wantPartial bool
	}{
		{name: "read error", rel: "linked", wantPartial: true},
		{name: "target gone", rel: "linked", missing: true},
		{name: "real folder", rel: "real"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newModeFixture(t, "orphanscan-linked-peer-walk", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
				if tc.missing {
					return newTestBackend()
				}
				return failFollowRootBackend{newTestBackend()}
			})
			f.setTier(models.SSHTierExec)
			movie := linkedPeerFixture(t, f, func(p string) string { return p }, tc.rel)
			if tc.missing {
				require.NoError(t, os.RemoveAll(filepath.Dir(movie)))
			}

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.Equal(t, !tc.wantPartial, run.IdentityComplete)
			require.Equal(t, tc.wantPartial, run.Partial, "run message: %s", run.ErrorMessage)
		})
	}
}
