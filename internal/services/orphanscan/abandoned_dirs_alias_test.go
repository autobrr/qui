// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

const dirAliasStatus = "skipped: a torrent now uses this directory through another path"

// Each row reaches an empty folder under the scan root only through a symlink,
// so comparing paths lists it. The folder holding it can never be emptied, so
// it is not listed either. leftover is reached by nothing and is still listed.
// regression: #3652
func TestExecuteScan_KeepsEmptyFolderATorrentReachesThroughAnAlias(t *testing.T) {
	for _, tc := range []struct {
		name string
		// reach makes the folders below defaultSavePath and the links in base,
		// and returns the torrent's save path and file.
		reach func(t *testing.T, defaultSavePath, base string) (savePath, file string)
	}{
		{"save path below a symlinked folder, not created yet", func(t *testing.T, defaultSavePath, base string) (string, string) {
			require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "Pack"), 0o750))
			symlinkOrSkip(t, filepath.Join(defaultSavePath, "Pack"), filepath.Join(base, "alias"))
			return filepath.Join(base, "alias", "Movie"), "x.mkv"
		}},
		{"save path is a symlink to an empty folder whose parent holds only it", func(t *testing.T, defaultSavePath, base string) (string, string) {
			require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "Pack", "Movie"), 0o750))
			symlinkOrSkip(t, filepath.Join(defaultSavePath, "Pack", "Movie"), filepath.Join(base, "alias"))
			return filepath.Join(base, "alias"), "x.mkv"
		}},
		{"an unwritten subfolder of the torrent below a symlinked save path", func(t *testing.T, defaultSavePath, base string) (string, string) {
			require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "Pack", "Sub"), 0o750))
			symlinkOrSkip(t, filepath.Join(defaultSavePath, "Pack"), filepath.Join(base, "alias"))
			return filepath.Join(base, "alias"), "Sub/x.mkv"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var savePath, file, leftover string
			opts := orphanOnlyOptions{beforeScan: func(svc *Service, ownedSavePath string) {
				addTorrentAfterScan(svc, ownedSavePath, qbt.Torrent{SavePath: savePath}, file)
			}}
			f := newOrphanOnlyFixture(t, "orphanscan-scan-dir-alias", opts, func(defaultSavePath string) {
				leftover = filepath.Join(defaultSavePath, "leftover")
				require.NoError(t, os.MkdirAll(leftover, 0o750))
				savePath, file = tc.reach(t, defaultSavePath, filepath.Dir(defaultSavePath))
			})

			require.Equal(t, []string{leftover}, f.previewedDirs(t))
		})
	}
}

// eioLstatBackend fails Lstat of one path with an I/O error.
type eioLstatBackend struct {
	fsops.Backend
	path string
}

func (b eioLstatBackend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	if p == b.path {
		return nil, &fs.PathError{Op: "lstat", Path: p, Err: syscall.EIO}
	}
	return b.Backend.Lstat(ctx, p)
}

// A listed folder whose own identity cannot be read could be one a torrent
// reaches, so the scan leaves it out, and the folder that holds it with it.
// regression: #3652
func TestExecuteScan_FolderWithoutIdentityIsNotListed(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(b fsops.Backend, opaque string) fsops.Backend
	}{
		{"identity unreadable", func(b fsops.Backend, opaque string) fsops.Backend {
			return noTargetIdentityBackend{Backend: b, path: opaque}
		}},
		{"lstat fails", func(b fsops.Backend, opaque string) fsops.Backend {
			return eioLstatBackend{Backend: b, path: opaque}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var leftover string
			opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, defaultSavePath string) fsops.Backend {
				return tc.wrap(b, filepath.Join(defaultSavePath, "Pack", "opaque"))
			}}
			f := newOrphanOnlyFixture(t, "orphanscan-scan-dir-no-identity", opts, func(defaultSavePath string) {
				leftover = filepath.Join(defaultSavePath, "leftover")
				require.NoError(t, os.MkdirAll(leftover, 0o750))
				require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "Pack", "opaque"), 0o750))
			})

			require.Equal(t, []string{leftover}, f.previewedDirs(t))
		})
	}
}

// A torrent folder whose identity cannot be read could be any listed folder,
// so the scan lists no folder. Files are judged without it, so they are still
// listed, and the run is partial, so it does not clean up by itself.
// regression: #3652
func TestExecuteScan_UnreadableTorrentFolderListsNoFolder(t *testing.T) {
	var junk, broken string
	opts := orphanOnlyOptions{
		scheduled:           true,
		autoCleanupMaxFiles: 100,
		wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
			return eioStatBackend{Backend: b, path: broken}
		},
		beforeScan: func(svc *Service, ownedSavePath string) {
			addTorrentAfterScan(svc, ownedSavePath, qbt.Torrent{SavePath: filepath.Dir(broken)}, "Sub/x.mkv")
		},
	}
	f := newOrphanOnlyFixture(t, "orphanscan-scan-dir-eio", opts, func(defaultSavePath string) {
		junk = filepath.Join(defaultSavePath, "loose", "junk.mkv")
		writeFile(t, junk)
		require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "leftover"), 0o750))
		broken = filepath.Join(filepath.Dir(defaultSavePath), "elsewhere", "Sub")
		require.NoError(t, os.MkdirAll(broken, 0o750))
	})

	require.Equal(t, map[string]string{junk: "pending: "}, fileStatuses(t, f.store, f.runID))
	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.True(t, run.Partial, "run message: %s", run.ErrorMessage)
	require.Equal(t, "preview_ready", run.Status, "a partial run does not clean up by itself")
	require.Contains(t, run.ErrorMessage, broken)
	require.FileExists(t, junk)
}

// A torrent added since the scan reaches a listed empty folder through a
// symlink, so the delete keeps it and says why. Its parent now holds it, and
// leftover is reached by nothing and goes.
// regression: #3652
func TestExecuteDeletion_KeepsEmptyFolderATorrentReachesThroughAnAliasSinceTheScan(t *testing.T) {
	var pack, movie, leftover string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-dir-alias-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		pack = filepath.Join(defaultSavePath, "Pack")
		movie = filepath.Join(pack, "Movie")
		leftover = filepath.Join(defaultSavePath, "leftover")
		require.NoError(t, os.MkdirAll(movie, 0o750))
		require.NoError(t, os.MkdirAll(leftover, 0o750))
	})
	require.ElementsMatch(t, []string{pack, movie, leftover}, f.previewedDirs(t), "the preview holds only folders")

	alias := filepath.Join(filepath.Dir(f.defaultSavePath), "alias")
	symlinkOrSkip(t, movie, alias)
	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: alias}, "x.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.DirExists(t, movie)
	require.NoDirExists(t, leftover)
	statuses := fileStatuses(t, f.store, f.runID)
	require.Equal(t, dirAliasStatus, statuses[movie])
	require.Equal(t, "skipped: directory still holds something this run did not delete", statuses[pack])
	require.Equal(t, "deleted: ", statuses[leftover])
}

// lateDirIdentityBackend reports the folder at *path without its identity,
// once the test sets path.
type lateDirIdentityBackend struct {
	fsops.Backend
	path *string
}

func (b lateDirIdentityBackend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	info, err := b.Backend.Lstat(ctx, p)
	if err == nil && p == *b.path {
		info.FileIDErr = errors.New("identity denied")
	}
	return info, err
}

// The delete reads each torrent folder before it deletes anything. One it
// cannot read for any reason but a missing or denied lookup could be a listed
// folder, so the run stops. A listed folder whose own identity is unreadable
// fails alone. A missing or denied torrent folder protects nothing.
// regression: #3652
func TestExecuteDeletion_FolderIdentityReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		// late runs after the scan, with opaque the folder to deny identity to.
		late func(t *testing.T, f *orphanOnlyFixture, opaque *string, broken *string, base string)
		// wantFailedRun is a run error, or "" when the run completes.
		wantFailedRun string
		// wantMovie is movie's status, or "" when it is deleted.
		wantMovie string
	}{
		{"torrent folder read fails", func(t *testing.T, f *orphanOnlyFixture, _ *string, broken *string, base string) {
			*broken = filepath.Join(base, "late", "Sub")
			require.NoError(t, os.MkdirAll(*broken, 0o750))
			addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: filepath.Dir(*broken)}, "Sub/x.mkv")
		}, "could not check which files or folders torrents use", ""},
		{"listed folder identity unreadable", func(t *testing.T, f *orphanOnlyFixture, opaque *string, _ *string, _ string) {
			*opaque = filepath.Join(f.defaultSavePath, "Movie")
		}, "", "failed: file identity"},
		{"torrent folder missing or denied", func(t *testing.T, f *orphanOnlyFixture, _ *string, _ *string, base string) {
			if runtime.GOOS == "windows" {
				t.Skip("permission-based stat failure is not reproducible via chmod on Windows")
			}
			if os.Geteuid() == 0 {
				t.Skip("root bypasses permission checks")
			}
			locked := filepath.Join(base, "locked")
			require.NoError(t, os.MkdirAll(filepath.Join(locked, "inner"), 0o750))
			require.NoError(t, os.Chmod(locked, 0o000))
			t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
			late := []qbt.Torrent{
				{Hash: "owned", SavePath: f.torrentSavePath, State: qbt.TorrentStatePausedUp},
				{Hash: "denied", SavePath: filepath.Join(locked, "inner"), State: qbt.TorrentStatePausedUp},
				{Hash: "missing", SavePath: filepath.Join(base, "never"), State: qbt.TorrentStatePausedUp},
			}
			stubSync(f.svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) { return late, nil }
			stubSync(f.svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
				return map[string]qbt.TorrentFiles{"owned": torrentFiles("owned.mkv"), "denied": torrentFiles("Sub/x.mkv"), "missing": torrentFiles("Sub/x.mkv")}, nil
			}
		}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var movie, leftover, opaque, broken string
			opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
				return lateDirIdentityBackend{Backend: lateEIOStatBackend{Backend: b, path: &broken}, path: &opaque}
			}}
			f := newOrphanOnlyFixture(t, "orphanscan-delete-dir-identity", opts, func(defaultSavePath string) {
				movie = filepath.Join(defaultSavePath, "Movie")
				leftover = filepath.Join(defaultSavePath, "leftover")
				require.NoError(t, os.MkdirAll(movie, 0o750))
				require.NoError(t, os.MkdirAll(leftover, 0o750))
			})
			require.ElementsMatch(t, []string{movie, leftover}, f.previewedDirs(t))

			tc.late(t, f, &opaque, &broken, filepath.Dir(f.defaultSavePath))
			f.svc.executeDeletion(t.Context(), 1, f.runID)

			run, err := f.store.GetRun(t.Context(), f.runID)
			require.NoError(t, err)
			if tc.wantFailedRun != "" {
				require.Equal(t, "failed", run.Status)
				require.Contains(t, run.ErrorMessage, tc.wantFailedRun)
				require.Contains(t, run.ErrorMessage, broken)
				require.DirExists(t, movie)
				require.DirExists(t, leftover)
				return
			}
			require.Equal(t, "completed", run.Status, "run error: %s", run.ErrorMessage)
			require.NoDirExists(t, leftover)
			if tc.wantMovie == "" {
				require.NoDirExists(t, movie)
				return
			}
			require.DirExists(t, movie)
			require.Contains(t, fileStatuses(t, f.store, f.runID)[movie], tc.wantMovie)
		})
	}
}

// A run reads the torrent files only when it lists a file, and the torrent
// folders only when it lists a folder, so one it cannot read of the other kind
// does not stop it.
// regression: #3652
func TestExecuteDeletion_ReadsOnlyTheTorrentPathsOfWhatItLists(t *testing.T) {
	for _, tc := range []struct {
		name string
		// listed makes what the scan lists, below defaultSavePath.
		listed func(t *testing.T, defaultSavePath string) string
		// unreadable is the torrent path, below the late save path, whose Stat fails.
		unreadable string
	}{
		{"files only, a torrent folder unreadable", func(t *testing.T, defaultSavePath string) string {
			junk := filepath.Join(defaultSavePath, "junk.mkv")
			writeFile(t, junk)
			return junk
		}, "Sub"},
		{"folders only, a torrent file unreadable", func(t *testing.T, defaultSavePath string) string {
			leftover := filepath.Join(defaultSavePath, "leftover")
			require.NoError(t, os.MkdirAll(leftover, 0o750))
			return leftover
		}, filepath.Join("Sub", "x.mkv")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var listed, broken string
			opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
				return lateEIOStatBackend{Backend: b, path: &broken}
			}}
			f := newOrphanOnlyFixture(t, "orphanscan-delete-reads-listed-half", opts, func(defaultSavePath string) {
				listed = tc.listed(t, defaultSavePath)
			})
			require.Equal(t, map[string]string{listed: "pending: "}, fileStatuses(t, f.store, f.runID))

			late := filepath.Join(filepath.Dir(f.defaultSavePath), "late")
			writeFile(t, filepath.Join(late, "Sub", "x.mkv"))
			broken = filepath.Join(late, tc.unreadable)
			addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: late}, "Sub/x.mkv")
			f.svc.executeDeletion(t.Context(), 1, f.runID)

			run, err := f.store.GetRun(t.Context(), f.runID)
			require.NoError(t, err)
			require.Equal(t, "completed", run.Status, "run error: %s", run.ErrorMessage)
			require.NoFileExists(t, listed)
		})
	}
}

// lateEIOStatBackend fails Stat of *path with an I/O error, once the test sets
// path.
type lateEIOStatBackend struct {
	fsops.Backend
	path *string
}

func (b lateEIOStatBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	return eioStatBackend{Backend: b.Backend, path: *b.path}.Stat(ctx, p)
}

// enableAbandonedDirs turns on Delete Abandoned Directories for the fixture's
// instance.
func (f *modeFixture) enableAbandonedDirs(t *testing.T) {
	t.Helper()

	settings, err := f.store.GetSettings(t.Context(), 1)
	require.NoError(t, err)
	settings.DeleteAbandonedDirs = true
	_, err = f.store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)
}

// findBatches counts the batch stats the server ran: a find at depth 0.
func findBatches(server *sshtest.Server) int {
	n := 0
	for _, words := range server.Commands() {
		if slices.Contains(words, "find") && slices.Contains(words, "-maxdepth") {
			n++
		}
	}
	return n
}

// The exec tier reads folder identities over SSH, so a folder a torrent
// reaches through a symlink is kept there too, before and after the scan.
// regression: #3652
func TestExecuteScan_RemoteExecTierKeepsEmptyFolderATorrentReachesThroughAnAlias(t *testing.T) {
	t.Parallel()

	f, server := newSSHFixture(t, "orphanscan-remote-dir-alias", sshtest.ExecGNU, models.SSHTierExec)
	f.enableAbandonedDirs(t)
	root := filepath.Dir(f.orphan)
	movie := filepath.Join(root, "Pack", "Movie")
	leftover := filepath.Join(root, "leftover")
	require.NoError(t, os.MkdirAll(movie, 0o750))
	require.NoError(t, os.MkdirAll(leftover, 0o750))
	alias := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, movie, alias)

	addTorrentAfterScan(f.svc, sftpPath(root), qbt.Torrent{SavePath: sftpPath(alias)}, "x.mkv")
	before := findBatches(server)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	statuses := fileStatuses(t, f.store, run.ID)
	require.Contains(t, statuses, sftpPath(leftover))
	require.NotContains(t, statuses, sftpPath(movie))
	require.NotContains(t, statuses, sftpPath(filepath.Dir(movie)))
	require.Greater(t, findBatches(server), before, "the scan reads folder identities in a find batch")
	require.NoError(t, f.svc.CancelRun(t.Context(), run.ID))

	// Listed by a scan no torrent reached it in, then reached before the delete.
	addTorrentAfterScan(f.svc, sftpPath(root), qbt.Torrent{SavePath: sftpPath(filepath.Join(t.TempDir(), "elsewhere"))}, "x.mkv")
	run = f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Contains(t, fileStatuses(t, f.store, run.ID), sftpPath(movie), "the scan lists it")
	addTorrentAfterScan(f.svc, sftpPath(root), qbt.Torrent{SavePath: sftpPath(alias)}, "x.mkv")
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	require.DirExists(t, movie)
	require.NoDirExists(t, leftover)
	require.Equal(t, dirAliasStatus, fileStatuses(t, f.store, run.ID)[sftpPath(movie)])
}

// The sftp tier reads no identity, so it lists folders by spelling, as before.
// It cannot delete them, so a folder kept only by identity is never at risk.
// regression: #3652
func TestExecuteScan_SFTPTierListsEmptyFolderBySpelling(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-sftp-dir-alias", sshtest.ExecSFTPOnly, models.SSHTierSFTP)
	f.enableAbandonedDirs(t)
	root := filepath.Dir(f.orphan)
	movie := filepath.Join(root, "Movie")
	require.NoError(t, os.MkdirAll(movie, 0o750))
	alias := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, movie, alias)
	addTorrentAfterScan(f.svc, sftpPath(root), qbt.Torrent{SavePath: sftpPath(alias)}, "x.mkv")

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Contains(t, fileStatuses(t, f.store, run.ID), sftpPath(movie))
	require.False(t, run.Partial, "run message: %s", run.ErrorMessage)
}

// fileListLess makes the fake client report, besides the owned torrent, one
// torrent at savePath whose file list qBittorrent does not have yet.
func fileListLess(svc *Service, ownedSavePath, savePath string, magnet bool) {
	late := qbt.Torrent{Hash: "late", SavePath: savePath, State: qbt.TorrentStateCheckingResumeData}
	if magnet {
		late.State, late.HasMetadata = qbt.TorrentStateMetaDl, new(false)
	}
	stubSync(svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: ownedSavePath, State: qbt.TorrentStatePausedUp}, late}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": torrentFiles("owned.mkv")}, nil
	}
}

// A magnet without metadata, or a torrent qBittorrent is still checking, has
// no file list, so only its save path says where it will write. A save path
// that is a symlink to an empty folder keeps that folder, in the scan and in
// the delete.
// regression: #3652
func TestOrphanScan_KeepsEmptyFolderATorrentWithoutAFileListReachesThroughAnAlias(t *testing.T) {
	for _, magnet := range []bool{true, false} {
		name := map[bool]string{true: "magnet without metadata", false: "torrent being checked"}[magnet]
		t.Run("scan, "+name, func(t *testing.T) {
			var alias, leftover string
			opts := orphanOnlyOptions{beforeScan: func(svc *Service, ownedSavePath string) {
				fileListLess(svc, ownedSavePath, alias, magnet)
			}}
			f := newOrphanOnlyFixture(t, "orphanscan-scan-dir-alias-no-files", opts, func(defaultSavePath string) {
				require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "Magnet"), 0o750))
				leftover = filepath.Join(defaultSavePath, "leftover")
				require.NoError(t, os.MkdirAll(leftover, 0o750))
				alias = filepath.Join(filepath.Dir(defaultSavePath), "alias")
				symlinkOrSkip(t, filepath.Join(defaultSavePath, "Magnet"), alias)
			})

			require.Equal(t, []string{leftover}, f.previewedDirs(t))
		})
		t.Run("delete, "+name, func(t *testing.T) {
			var magnetDir, leftover string
			f := newOrphanOnlyFixture(t, "orphanscan-delete-dir-alias-no-files", orphanOnlyOptions{}, func(defaultSavePath string) {
				magnetDir = filepath.Join(defaultSavePath, "Magnet")
				leftover = filepath.Join(defaultSavePath, "leftover")
				require.NoError(t, os.MkdirAll(magnetDir, 0o750))
				require.NoError(t, os.MkdirAll(leftover, 0o750))
			})
			require.ElementsMatch(t, []string{magnetDir, leftover}, f.previewedDirs(t))

			alias := filepath.Join(filepath.Dir(f.defaultSavePath), "alias")
			symlinkOrSkip(t, magnetDir, alias)
			fileListLess(f.svc, f.torrentSavePath, alias, magnet)
			f.svc.executeDeletion(t.Context(), 1, f.runID)

			require.DirExists(t, magnetDir)
			require.NoDirExists(t, leftover)
			require.Equal(t, dirAliasStatus, fileStatuses(t, f.store, f.runID)[magnetDir])
		})
	}
}

// A disc unit holding a symlink to a folder a torrent reaches is still deleted
// whole, as before: only a torrent's file inside a unit keeps it (#3612).
// regression: #3652
func TestExecuteDeletion_DiscUnitHoldingASymlinkToAFolderATorrentReachesIsDeleted(t *testing.T) {
	var unit, stored string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-disc-dir-link", orphanOnlyOptions{}, func(defaultSavePath string) {
		unit = filepath.Join(defaultSavePath, "Movie")
		writeFile(t, filepath.Join(unit, "BDMV", "STREAM", "00000.m2ts"))
		stored = filepath.Join(filepath.Dir(defaultSavePath), "store", "Folder")
		require.NoError(t, os.MkdirAll(stored, 0o750))
		symlinkOrSkip(t, stored, filepath.Join(unit, "BDMV", "LINK"))
		require.NoError(t, os.MkdirAll(filepath.Join(defaultSavePath, "leftover"), 0o750))
	})
	require.Contains(t, fileStatuses(t, f.store, f.runID), unit, "the scan lists it")

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: stored}, "x.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.Equal(t, "deleted: ", fileStatuses(t, f.store, f.runID)[unit])
	require.DirExists(t, stored)
}
