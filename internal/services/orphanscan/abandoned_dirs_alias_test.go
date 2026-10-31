// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

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
// reaches through a symlink is kept there too.
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
