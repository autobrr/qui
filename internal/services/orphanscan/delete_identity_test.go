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
	"sync/atomic"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

const aliasStatus = "skipped: a torrent now uses this file through another path"

// addTorrentAfterScan makes the fake client report a second torrent, late,
// from now on.
func addTorrentAfterScan(svc *Service, ownedSavePath string, late qbt.Torrent, names ...string) {
	late.Hash, late.State = "late", qbt.TorrentStatePausedUp
	stubSync(svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: ownedSavePath, State: qbt.TorrentStatePausedUp}, late}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}, "late": torrentFiles(names...)}, nil
	}
}

func torrentFiles(names ...string) qbt.TorrentFiles {
	files := make(qbt.TorrentFiles, 0, len(names))
	for _, name := range names {
		files = append(files, qbt.TorrentFile{Name: name, Size: 1})
	}
	return files
}

// fileStatuses maps each listed path of the run to "status: message".
func fileStatuses(t *testing.T, store *models.OrphanScanStore, runID int64) map[string]string {
	t.Helper()

	files, err := store.ListFiles(t.Context(), runID, 1000, 0, "")
	require.NoError(t, err)
	got := make(map[string]string, len(files))
	for _, file := range files {
		got[file.FilePath] = file.Status + ": " + file.ErrorMessage
	}
	return got
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// A torrent added after the scan reaches a listed file through a symlinked
// folder, so the delete keeps it, as the scan would have.
// regression: #3596
func TestExecuteDeletion_KeepsFileATorrentReachesThroughAnAliasSinceTheScan(t *testing.T) {
	var orphan, aliasDir string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-alias-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
		writeFile(t, orphan)
		aliasDir = filepath.Join(filepath.Dir(defaultSavePath), "alias")
		symlinkOrSkip(t, filepath.Join(defaultSavePath, "loose"), aliasDir)
	})
	require.Contains(t, fileStatuses(t, f.store, f.runID), orphan, "the scan lists it")

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: aliasDir}, "movie.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.FileExists(t, orphan)
	require.Equal(t, aliasStatus, fileStatuses(t, f.store, f.runID)[orphan])
}

// A hardlink of a listed file made since the scan is kept too, matching the
// scan (#3487).
// regression: #3596
func TestExecuteDeletion_KeepsHardlinkOfATorrentFileMadeSinceTheScan(t *testing.T) {
	var orphan string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-hardlink-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
		writeFile(t, orphan)
	})
	require.Contains(t, fileStatuses(t, f.store, f.runID), orphan, "the scan lists it")
	other := filepath.Join(filepath.Dir(f.defaultSavePath), "elsewhere")
	require.NoError(t, os.MkdirAll(other, 0o750))
	require.NoError(t, os.Link(orphan, filepath.Join(other, "movie.mkv")))

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: other}, "movie.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.FileExists(t, orphan)
	require.Equal(t, aliasStatus, fileStatuses(t, f.store, f.runID)[orphan])
}

// Each row reaches the listed file through a path only one step of the
// identity read covers, so dropping that step deletes the file.
// regression: #3596
func TestExecuteDeletion_KeepsFileATorrentReachesThroughEachKindOfPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		// late makes the fake client report what reaches orphan since the scan.
		late func(t *testing.T, f *orphanOnlyFixture, orphan, base string)
	}{
		{"a peer instance's torrent through a symlinked folder", func(t *testing.T, f *orphanOnlyFixture, orphan, base string) {
			alias := filepath.Join(base, "alias")
			symlinkOrSkip(t, filepath.Dir(orphan), alias)
			stubSync(f.svc).listInstances = func(context.Context) ([]*models.Instance, error) {
				return []*models.Instance{{ID: 1, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}, localInstance(2)}, nil
			}
			stubSync(f.svc).getAllTorrents = func(_ context.Context, instanceID int) ([]qbt.Torrent, error) {
				if instanceID == 2 {
					return []qbt.Torrent{
						{Hash: "peer-owned", SavePath: f.torrentSavePath, State: qbt.TorrentStatePausedUp},
						{Hash: "peer-late", SavePath: alias, State: qbt.TorrentStatePausedUp},
					}, nil
				}
				return []qbt.Torrent{{Hash: "owned", SavePath: f.torrentSavePath, State: qbt.TorrentStatePausedUp}}, nil
			}
			stubSync(f.svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
				return map[string]qbt.TorrentFiles{"owned": torrentFiles("owned.mkv"), "peer-owned": torrentFiles("owned.mkv"), "peer-late": torrentFiles("movie.mkv")}, nil
			}
		}},
		{"an Auto TMM torrent whose content path is a symlinked folder", func(t *testing.T, f *orphanOnlyFixture, orphan, base string) {
			alias := filepath.Join(base, "alias")
			symlinkOrSkip(t, filepath.Dir(orphan), alias)
			category := filepath.Join(base, "category")
			require.NoError(t, os.MkdirAll(category, 0o750))
			addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: category, ContentPath: filepath.Join(alias, "movie.mkv")}, "movie.mkv")
		}},
		{"a torrent file that is a symlink to it", func(t *testing.T, f *orphanOnlyFixture, orphan, base string) {
			late := filepath.Join(base, "late")
			require.NoError(t, os.MkdirAll(late, 0o750))
			symlinkOrSkip(t, orphan, filepath.Join(late, "movie.mkv"))
			addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: late}, "movie.mkv")
		}},
		// Paths that differ only in case are two files here, so the read must
		// not fold them into one.
		{"a second spelling of a path that differs only in case", func(t *testing.T, f *orphanOnlyFixture, orphan, base string) {
			late := filepath.Join(base, "late")
			writeFile(t, filepath.Join(late, "Alias", "movie.mkv"))
			if err := os.Symlink(filepath.Dir(orphan), filepath.Join(late, "alias")); err != nil {
				t.Skipf("the filesystem folds case or has no symlinks: %v", err)
			}
			addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: late}, "Alias/movie.mkv", "alias/movie.mkv")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var orphan string
			f := newOrphanOnlyFixture(t, "orphanscan-delete-alias-kinds", orphanOnlyOptions{}, func(defaultSavePath string) {
				orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
				writeFile(t, orphan)
			})
			require.Contains(t, fileStatuses(t, f.store, f.runID), orphan, "the scan lists it")

			tc.late(t, f, orphan, filepath.Dir(f.defaultSavePath))
			f.svc.executeDeletion(t.Context(), 1, f.runID)

			require.FileExists(t, orphan)
			require.Equal(t, aliasStatus, fileStatuses(t, f.store, f.runID)[orphan])
		})
	}
}

// A torrent file that is not on disk, or whose parent is a file, protects
// nothing, and the run goes on.
// regression: #3596
func TestExecuteDeletion_MissingTorrentFileDoesNotStopTheRun(t *testing.T) {
	var orphan string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-missing-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
		writeFile(t, orphan)
	})

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: f.torrentSavePath}, "never/gone.mkv", "owned.mkv/gone.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.NoFileExists(t, orphan)
}

// A disc unit is listed as one directory, and a torrent that reaches a file
// inside it through an alias keeps the whole unit.
// regression: #3596
func TestExecuteDeletion_KeepsDiscUnitHoldingAFileATorrentReachesThroughAnAlias(t *testing.T) {
	var unit, stream, aliasDir string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-disc-alias-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		unit = filepath.Join(defaultSavePath, "Movie")
		stream = filepath.Join(unit, "BDMV", "STREAM", "00000.m2ts")
		writeFile(t, stream)
		aliasDir = filepath.Join(filepath.Dir(defaultSavePath), "alias")
		symlinkOrSkip(t, unit, aliasDir)
	})
	require.Contains(t, fileStatuses(t, f.store, f.runID), unit, "the scan lists it")

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: aliasDir}, "BDMV/STREAM/00000.m2ts")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.FileExists(t, stream)
	require.Equal(t, "skipped: a torrent now uses a file in this folder through another path", fileStatuses(t, f.store, f.runID)[unit])
}

// A disc unit can hold a symlink to a file elsewhere. A torrent that reaches
// that link through an alias since the scan keeps the unit, because removing
// the unit would remove the link the torrent uses.
// regression: #3596
func TestExecuteDeletion_KeepsDiscUnitHoldingASymlinkATorrentReachesThroughAnAlias(t *testing.T) {
	var unit, link, aliasDir string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-disc-symlink-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		unit = filepath.Join(defaultSavePath, "Movie")
		writeFile(t, filepath.Join(unit, "BDMV", "STREAM", "00000.m2ts"))
		stored := filepath.Join(filepath.Dir(defaultSavePath), "store", "00001.m2ts")
		writeFile(t, stored)
		link = filepath.Join(unit, "BDMV", "STREAM", "00001.m2ts")
		symlinkOrSkip(t, stored, link)
		aliasDir = filepath.Join(filepath.Dir(defaultSavePath), "alias")
		symlinkOrSkip(t, unit, aliasDir)
	})
	require.Contains(t, fileStatuses(t, f.store, f.runID), unit, "the scan lists it")

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: aliasDir}, "BDMV/STREAM/00001.m2ts")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	_, err := os.Lstat(link)
	require.NoError(t, err, "the link the torrent uses survives")
	require.Equal(t, "skipped: a torrent now uses a file in this folder through another path", fileStatuses(t, f.store, f.runID)[unit])
}

// A symlink in a disc unit that leads to no file, dangling or in a loop,
// cannot be a torrent's path, so the unit is deleted while the run holds
// torrent identities. A link qui cannot follow could be one, so its unit
// stays and says why.
// regression: #3596
func TestExecuteDeletion_DiscUnitHoldingASymlinkToNoFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		// link makes the symlink at link and returns what to run after the scan.
		link       func(t *testing.T, link, base string) func()
		wantFailed string
	}{
		{"dangling", func(t *testing.T, link, base string) func() {
			symlinkOrSkip(t, filepath.Join(base, "gone.m2ts"), link)
			return func() {}
		}, ""},
		{"loop", func(t *testing.T, link, _ string) func() {
			if runtime.GOOS == "windows" {
				t.Skip("a self-loop link reports differently on Windows")
			}
			symlinkOrSkip(t, link, link)
			return func() {}
		}, ""},
		{"target qui may not read", func(t *testing.T, link, base string) func() {
			if runtime.GOOS == "windows" {
				t.Skip("permission-based stat failure is not reproducible via chmod on Windows")
			}
			if os.Geteuid() == 0 {
				t.Skip("root bypasses permission checks")
			}
			locked := filepath.Join(base, "locked")
			writeFile(t, filepath.Join(locked, "00001.m2ts"))
			symlinkOrSkip(t, filepath.Join(locked, "00001.m2ts"), link)
			return func() {
				require.NoError(t, os.Chmod(locked, 0o000))
				t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
			}
		}, "failed: check directory contents: follow symlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var unit string
			var afterScan func()
			f := newOrphanOnlyFixture(t, "orphanscan-delete-disc-no-file", orphanOnlyOptions{}, func(defaultSavePath string) {
				unit = filepath.Join(defaultSavePath, "Movie")
				writeFile(t, filepath.Join(unit, "BDMV", "STREAM", "00000.m2ts"))
				afterScan = tc.link(t, filepath.Join(unit, "BDMV", "STREAM", "00001.m2ts"), filepath.Dir(defaultSavePath))
			})
			require.Contains(t, fileStatuses(t, f.store, f.runID), unit, "the scan lists it")

			afterScan()
			f.svc.executeDeletion(t.Context(), 1, f.runID)

			status := fileStatuses(t, f.store, f.runID)[unit]
			if tc.wantFailed == "" {
				require.NoDirExists(t, unit, "status: %s", status)
				return
			}
			require.DirExists(t, unit)
			require.Contains(t, status, tc.wantFailed)
		})
	}
}

// noWalkIdentityBackend walks root without the identity of its files, as the
// sftp walk does.
type noWalkIdentityBackend struct {
	fsops.Backend
	root string
}

func (b noWalkIdentityBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil || root != b.root {
		return ch, err
	}
	out := make(chan fsops.WalkEntry)
	go func() {
		defer close(out)
		for entry := range ch {
			if entry.Mode.IsRegular() {
				entry.FileIDErr = errors.New("identity denied")
			}
			out <- entry
		}
	}()
	return out, nil
}

// A disc unit whose files carry no identity cannot be checked, so it stays and
// its entry says why.
// regression: #3596
func TestExecuteDeletion_DiscUnitWithoutFileIdentitiesFails(t *testing.T) {
	var unit, stream string
	opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, defaultSavePath string) fsops.Backend {
		return noWalkIdentityBackend{Backend: b, root: filepath.Join(defaultSavePath, "Movie")}
	}}
	f := newOrphanOnlyFixture(t, "orphanscan-delete-disc-no-identity", opts, func(defaultSavePath string) {
		unit = filepath.Join(defaultSavePath, "Movie")
		stream = filepath.Join(unit, "BDMV", "STREAM", "00000.m2ts")
		writeFile(t, stream)
	})
	require.Contains(t, fileStatuses(t, f.store, f.runID), unit, "the scan lists it")

	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.FileExists(t, stream)
	require.Contains(t, fileStatuses(t, f.store, f.runID)[unit], "failed: check directory contents: cannot read the identity of the files in this folder")
}

// A torrent file qui may not read is skipped, as the scan skips a denied
// subtree, and the run goes on.
// regression: #3596
func TestExecuteDeletion_TorrentFileWithoutReadPermissionIsSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based stat failure is not reproducible via chmod on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	var orphan, locked string
	f := newOrphanOnlyFixture(t, "orphanscan-delete-unreadable-late", orphanOnlyOptions{}, func(defaultSavePath string) {
		orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
		writeFile(t, orphan)
		locked = filepath.Join(filepath.Dir(defaultSavePath), "locked")
		writeFile(t, filepath.Join(locked, "movie.mkv"))
	})
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: locked}, "movie.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.NoFileExists(t, orphan)
	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status, "run error: %s", run.ErrorMessage)
}

// eioStatBackend fails Stat of one path with an I/O error.
type eioStatBackend struct {
	fsops.Backend
	path string
}

func (b eioStatBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	if p == b.path {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: syscall.EIO}
	}
	return b.Backend.Stat(ctx, p)
}

// A torrent file that cannot be read for any reason but permission could be a
// listed file under another path, so the run stops before it deletes anything.
// regression: #3596
func TestExecuteDeletion_UnreadableTorrentFileStopsTheRunBeforeAnyDelete(t *testing.T) {
	var orphan, broken string
	opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, _ string) fsops.Backend {
		return eioStatBackend{Backend: b, path: broken}
	}}
	f := newOrphanOnlyFixture(t, "orphanscan-delete-eio-late", opts, func(defaultSavePath string) {
		orphan = filepath.Join(defaultSavePath, "loose", "movie.mkv")
		writeFile(t, orphan)
		broken = filepath.Join(filepath.Dir(defaultSavePath), "broken", "movie.mkv")
		writeFile(t, broken)
	})

	addTorrentAfterScan(f.svc, f.torrentSavePath, qbt.Torrent{SavePath: filepath.Dir(broken)}, "movie.mkv")
	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.FileExists(t, orphan)
	run, err := f.store.GetRun(t.Context(), f.runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.ErrorMessage, "could not check which files torrents use")
	require.Contains(t, run.ErrorMessage, broken)
	require.Contains(t, run.ErrorMessage, "Fix access to that path or remove the torrent, then run a new scan.")
}

// noTargetIdentityBackend reports one path without its identity.
type noTargetIdentityBackend struct {
	fsops.Backend
	path string
}

func (b noTargetIdentityBackend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	info, err := b.Backend.Lstat(ctx, p)
	if err == nil && p == b.path {
		info.FileIDErr = errors.New("identity denied")
	}
	return info, err
}

// A target whose own identity cannot be read fails alone, and the run goes on.
// regression: #3596
func TestExecuteDeletion_TargetWithoutIdentityFailsOnlyThatTarget(t *testing.T) {
	var opaque, plain string
	opts := orphanOnlyOptions{wrapBackend: func(b fsops.Backend, defaultSavePath string) fsops.Backend {
		return noTargetIdentityBackend{Backend: b, path: filepath.Join(defaultSavePath, "opaque.mkv")}
	}}
	f := newOrphanOnlyFixture(t, "orphanscan-delete-target-identity", opts, func(defaultSavePath string) {
		opaque = filepath.Join(defaultSavePath, "opaque.mkv")
		plain = filepath.Join(defaultSavePath, "plain.mkv")
		writeFile(t, opaque)
		writeFile(t, plain)
	})

	f.svc.executeDeletion(t.Context(), 1, f.runID)

	require.FileExists(t, opaque)
	require.NoFileExists(t, plain)
	require.Contains(t, fileStatuses(t, f.store, f.runID)[opaque], "failed: file identity")
}

// The exec tier reads identities over SSH, so a late alias on the remote host
// is kept too.
// regression: #3596
func TestExecuteDeletion_RemoteExecTierKeepsFileATorrentReachesThroughAnAlias(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-delete-alias-late", sshtest.ExecGNU, models.SSHTierExec)
	aliasDir := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, filepath.Dir(f.orphan), aliasDir)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	addTorrentAfterScan(f.svc, sftpPath(filepath.Dir(f.owned)), qbt.Torrent{SavePath: sftpPath(aliasDir)}, filepath.Base(f.orphan))
	before := f.batches.Load()
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	require.Greater(t, f.batches.Load(), before, "the delete reads identities through StatBatch")
	f.requireNothingDeleted(t)
	require.Equal(t, aliasStatus, fileStatuses(t, f.store, run.ID)[sftpPath(f.orphan)])
}

// The exec tier walks without following links and follows the one it meets
// over SSH, so a disc unit's symlink is compared by its target there too.
// regression: #3596
func TestExecuteDeletion_RemoteExecTierKeepsDiscUnitHoldingASymlinkATorrentReachesThroughAnAlias(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-delete-disc-symlink-late", sshtest.ExecGNU, models.SSHTierExec)
	unit := filepath.Join(filepath.Dir(f.orphan), "Movie")
	writeFile(t, filepath.Join(unit, "BDMV", "STREAM", "00000.m2ts"))
	stored := filepath.Join(t.TempDir(), "00001.m2ts")
	writeFile(t, stored)
	link := filepath.Join(unit, "BDMV", "STREAM", "00001.m2ts")
	symlinkOrSkip(t, stored, link)
	aliasDir := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, unit, aliasDir)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Contains(t, fileStatuses(t, f.store, run.ID), sftpPath(unit), "the scan lists it")
	addTorrentAfterScan(f.svc, sftpPath(filepath.Dir(f.owned)), qbt.Torrent{SavePath: sftpPath(aliasDir)}, "BDMV/STREAM/00001.m2ts")
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	_, err := os.Lstat(link)
	require.NoError(t, err, "the link the torrent uses survives")
	require.Equal(t, "skipped: a torrent now uses a file in this folder through another path", fileStatuses(t, f.store, run.ID)[sftpPath(unit)])
}

// On the exec tier the find batch reports a denied torrent file per path, so
// the delete skips it and goes on, as it does locally.
// regression: #3596
func TestExecuteDeletion_RemoteExecTierSkipsTorrentFileWithoutReadPermission(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	f, _ := newSSHFixture(t, "orphanscan-remote-delete-unreadable-late", sshtest.ExecGNU, models.SSHTierExec)
	locked := filepath.Join(t.TempDir(), "locked")
	writeFile(t, filepath.Join(locked, "movie.mkv"))

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
	addTorrentAfterScan(f.svc, sftpPath(filepath.Dir(f.owned)), qbt.Torrent{SavePath: sftpPath(locked)}, "movie.mkv")
	before := f.batches.Load()
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	require.Greater(t, f.batches.Load(), before, "the delete reads identities through StatBatch")
	got := f.run(t, run.ID)
	require.Equal(t, "completed", got.Status, "run error: %s", got.ErrorMessage)
	require.NoFileExists(t, f.orphan)
	require.FileExists(t, f.owned)
}

// connectionLostStatBackend fails every Stat as a dropped link once armed,
// after running onStat. err, when set, is the error the link reports.
type connectionLostStatBackend struct {
	fsops.Backend
	armed  *atomic.Bool
	onStat func()
	err    error
}

func (b connectionLostStatBackend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	if !b.armed.Load() {
		return b.Backend.Stat(ctx, p)
	}
	b.onStat()
	err := b.err
	if err == nil {
		err = fsops.ErrConnectionLost
	}
	return nil, &fs.PathError{Op: "stat", Path: p, Err: err}
}

// A connection that drops while the identities are read stops the run before
// any delete, in words that fit a remote instance. When the SSH settings
// changed meanwhile, the run says that instead, since the redial went to the
// new endpoint.
// regression: #3596
func TestExecuteDeletion_RemoteConnectionLostWhileReadingIdentitiesDeletesNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		change func(*models.Instance)
		err    error
		want   string
	}{
		{"connection lost", func(*models.Instance) {}, nil, "the connection to the remote instance was lost"},
		{"endpoint moved", func(inst *models.Instance) { inst.SSHUsername = "someone-else" }, nil, FilesystemModeChangedMessage},
		// A dropped link that also reads as denied is not a denied file.
		{"connection lost reported as denied", func(*models.Instance) {}, errors.Join(fsops.ErrConnectionLost, fs.ErrPermission), "the connection to the remote instance was lost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			armed := &atomic.Bool{}
			var f *modeFixture
			f = newModeFixture(t, "orphanscan-remote-delete-identity-lost", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
				return connectionLostStatBackend{
					Backend: countingBackend{Backend: newTestBackend(), removes: f.removes},
					armed:   armed,
					err:     tc.err,
					onStat: func() {
						f.instance.mu.Lock()
						defer f.instance.mu.Unlock()
						tc.change(&f.instance.inst)
					},
				}
			})
			f.setTier(models.SSHTierExec)
			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

			armed.Store(true)
			f.svc.executeDeletion(t.Context(), 1, run.ID)

			failed := f.run(t, run.ID)
			require.Equal(t, "failed", failed.Status)
			require.Contains(t, failed.ErrorMessage, tc.want)
			f.requireNothingDeleted(t)
		})
	}
}
