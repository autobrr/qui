// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

// The exec tier deletes over the real SSH path, end to end (#3236).
func TestConfirmDeletion_RemoteExecTierDeletesOverSSH(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-exec-delete-e2e", sshtest.ExecGNU, models.SSHTierExec)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.True(t, run.IdentityComplete)

	require.NoError(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID))
	f.requireCompleted(t)
	require.NoFileExists(t, f.orphan)
	require.FileExists(t, f.owned)
	require.Equal(t, "completed", f.run(t, run.ID).Status)
}

// A row on the exec tier whose walk fell back to sftp missed identities, so
// its preview is kept and nothing is deleted.
func TestConfirmDeletion_RemoteRunWithIncompleteIdentityIsRefused(t *testing.T) {
	t.Parallel()

	t.Run("find refused by the server", func(t *testing.T) {
		t.Parallel()

		f, _ := newSSHFixture(t, "orphanscan-remote-fallback-refuse", sshtest.ExecSFTPOnly, models.SSHTierExec)
		run := f.scan(t, "manual")
		require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
		require.False(t, run.IdentityComplete)

		require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), fsops.ErrNotCapable)
		require.Equal(t, "preview_ready", f.run(t, run.ID).Status)
		f.requireNothingDeleted(t)
	})

	t.Run("backend that drops file identity", func(t *testing.T) {
		t.Parallel()

		var f *modeFixture
		f = newModeFixture(t, "orphanscan-remote-noid-refuse", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
			return noIdentityBackend{countingBackend{Backend: newTestBackend(), removes: f.removes}}
		})
		f.setTier(models.SSHTierExec)
		run := f.scan(t, "manual")
		require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
		require.False(t, run.IdentityComplete)
		require.Contains(t, run.ErrorMessage, IdentityIncompleteMessage)

		require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), fsops.ErrNotCapable)
		require.Equal(t, "preview_ready", f.run(t, run.ID).Status)
		f.requireNothingDeleted(t)
	})

	// The torrent files below the cut were never keyed, so a second path to
	// one of them in another root would be listed.
	t.Run("root walk cut by a lost connection", func(t *testing.T) {
		t.Parallel()

		cutRoot := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(cutRoot, "kept.bin"), []byte("x"), 0o600))
		var f *modeFixture
		f = newModeFixture(t, "orphanscan-remote-cut-refuse", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
			return cutWalkBackend{Backend: countingBackend{Backend: newTestBackend(), removes: f.removes}, root: cutRoot}
		})
		f.setTier(models.SSHTierExec)
		stubSync(f.svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
			return []qbt.Torrent{
				{Hash: "owned", SavePath: filepath.Dir(f.orphan), State: qbt.TorrentStatePausedUp},
				{Hash: "cut", SavePath: cutRoot, State: qbt.TorrentStatePausedUp},
			}, nil
		}
		stubSync(f.svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
			return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}, "cut": {{Name: "kept.bin", Size: 1}}}, nil
		}

		run := f.scan(t, "manual")
		require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
		require.True(t, run.Partial)
		require.False(t, run.IdentityComplete)

		require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), fsops.ErrNotCapable)
		f.requireNothingDeleted(t)
	})
}

// The in-use walk of an ignored symlinked save path is cut, so its files were
// never keyed and their twins at the target would be listed.
func TestConfirmDeletion_RemoteRunWithACutIgnoredLinkIsRefused(t *testing.T) {
	t.Parallel()

	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "kept.bin"), []byte("x"), 0o600))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var f *modeFixture
	f = newModeFixture(t, "orphanscan-remote-cut-link-refuse", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
		return cutWalkBackend{Backend: countingBackend{Backend: newTestBackend(), removes: f.removes}, root: link}
	})
	f.setTier(models.SSHTierExec)
	stubSync(f.svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{
			{Hash: "owned", SavePath: filepath.Dir(f.orphan), State: qbt.TorrentStatePausedUp},
			{Hash: "linked", SavePath: link, State: qbt.TorrentStatePausedUp},
		}, nil
	}
	stubSync(f.svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}, "linked": {{Name: "kept.bin", Size: 1}}}, nil
	}
	settings, err := f.store.GetSettings(t.Context(), 1)
	require.NoError(t, err)
	settings.IgnorePaths = []string{link}
	_, err = f.store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.False(t, run.IdentityComplete)

	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), fsops.ErrNotCapable)
	f.requireNothingDeleted(t)
}

// cutWalkBackend ends the walk of root with a lost connection, after the
// entries it did hand out.
type cutWalkBackend struct {
	fsops.Backend
	root string
}

func (b cutWalkBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	in, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil || root != b.root {
		return in, err
	}
	out := make(chan fsops.WalkEntry, 1)
	go func() {
		defer close(out)
		for e := range in {
			out <- e
		}
		out <- fsops.WalkEntry{Path: filepath.Join(root, "sub"), Err: fsops.ErrConnectionLost}
	}()
	return out, nil
}

// The run is bound to the SSH user, host and port it walked. A change between
// the scan and the confirm, or after the confirm, fails the run.
func TestDeletion_RemoteEndpointChangedFailsTheRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		change func(*models.Instance)
	}{
		{"host", func(inst *models.Instance) { inst.SSHHost = "other.example" }},
		{"port", func(inst *models.Instance) { inst.SSHPort = 2222 }},
		{"user", func(inst *models.Instance) { inst.SSHUsername = "someone-else" }},
	} {
		t.Run(tc.name+" changed before the confirm", func(t *testing.T) {
			t.Parallel()

			f := newModeFixture(t, "orphanscan-remote-endpoint-confirm", models.FilesystemModeRemote, nil)
			f.setTier(models.SSHTierExec)
			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

			// Saving SSH credentials also resets the tier until the next probe.
			f.instance.mu.Lock()
			tc.change(&f.instance.inst)
			f.instance.inst.SSHTier = models.SSHTierSFTP
			f.instance.mu.Unlock()
			require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), ErrFilesystemModeChanged)

			failed := f.run(t, run.ID)
			require.Equal(t, "failed", failed.Status)
			require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
			f.requireNothingDeleted(t)
		})

		t.Run(tc.name+" changed after the confirm", func(t *testing.T) {
			t.Parallel()

			f := newModeFixture(t, "orphanscan-remote-endpoint-delete", models.FilesystemModeRemote, nil)
			f.setTier(models.SSHTierExec)
			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

			f.instance.mu.Lock()
			tc.change(&f.instance.inst)
			f.instance.mu.Unlock()
			f.svc.executeDeletion(t.Context(), 1, run.ID)

			failed := f.run(t, run.ID)
			require.Equal(t, "failed", failed.Status)
			require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
			f.requireNothingDeleted(t)
		})
	}
}

// Automatic cleanup runs on a remote instance under the same gates as local
// ones, plus a walk that read every file's identity.
func TestMaybeAutoCleanup_RemoteExecTierRunCleansUp(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-exec-autoclean", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)

	run := f.scan(t, "scheduled")
	f.requireCompleted(t)
	require.Equal(t, "completed", f.run(t, run.ID).Status)
	require.NoFileExists(t, f.orphan)
	require.FileExists(t, f.owned)
}

// Skipping cleanup adds its reason after the scan's own warning, which says
// what the server did wrong.
func TestMaybeAutoCleanup_RemoteRunWithIncompleteIdentityKeepsBothWarnings(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-fallback-autoclean", sshtest.ExecSFTPOnly, models.SSHTierExec)

	run := f.scan(t, "scheduled")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, IdentityIncompleteMessage+"\n\n"+AutoCleanupNoIdentityMessage, run.ErrorMessage)
	f.requireNothingDeleted(t)
	f.requireNoFailureEvent(t)
}

// failingRemoveBackend fails Remove for the paths fail names with err.
type failingRemoveBackend struct {
	fsops.Backend
	fail func(path string) bool
	err  error
}

func (b failingRemoveBackend) Remove(ctx context.Context, path string, opts fsops.RemoveOptions) error {
	if b.fail(path) {
		return &fs.PathError{Op: "remove", Path: path, Err: b.err}
	}
	return b.Backend.Remove(ctx, path, opts)
}

// A failed delete on a remote instance is reported in words that fit it, from
// the file loop and the directory loop alike, and the loop goes on so the
// entries after a dropped connection are still tried.
func TestExecuteDeletion_RemoteFailureText(t *testing.T) {
	t.Parallel()

	const (
		lost       = "the connection to the remote instance was lost"
		permission = "the account qui uses on the remote instance has write access"
	)
	for _, tc := range []struct {
		name     string
		failDir  bool
		err      error
		want     string
		wantNone []string
	}{
		{"connection lost in the file loop", false, fsops.ErrConnectionLost, lost, nil},
		{"connection lost in the directory loop", true, fsops.ErrConnectionLost, lost, nil},
		{"permission denied", false, fs.ErrPermission, permission, []string{"qui process"}},
		// sftp sends EROFS as a generic failure, so a read-only text would only mislead.
		{"read-only text stays local", false, syscall.EROFS, "Check the details for specific errors", []string{":ro"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *modeFixture
			var emptyDir string
			f = newModeFixture(t, "orphanscan-remote-failure-text", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
				return failingRemoveBackend{
					Backend: countingBackend{Backend: newTestBackend(), removes: f.removes},
					fail: func(path string) bool {
						if tc.failDir {
							return path == emptyDir
						}
						return path == f.orphan
					},
					err: tc.err,
				}
			})
			f.setTier(models.SSHTierExec)
			root := filepath.Dir(f.orphan)
			later := filepath.Join(root, "z-later.mkv")
			require.NoError(t, os.WriteFile(later, []byte("later"), 0o600))
			emptyDir = filepath.Join(root, "empty")
			require.NoError(t, os.Mkdir(emptyDir, 0o750))

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.NoError(t, f.store.InsertFiles(t.Context(), run.ID, []models.OrphanScanFile{{FilePath: emptyDir, IsAbandonedDir: true, Status: "pending"}}))

			f.svc.executeDeletion(t.Context(), 1, run.ID)

			done := f.run(t, run.ID)
			require.Equal(t, "completed", done.Status, "run error: %s", done.ErrorMessage)
			require.Contains(t, done.ErrorMessage, tc.want)
			for _, unwanted := range tc.wantNone {
				require.NotContains(t, done.ErrorMessage, unwanted)
			}
			require.NoFileExists(t, later, "a failure must not stop the loop")
			require.FileExists(t, f.owned)

			files, err := f.store.ListFiles(t.Context(), run.ID, 100, 0, "")
			require.NoError(t, err)
			failed := 0
			for _, file := range files {
				if file.Status == "failed" {
					failed++
					require.Contains(t, file.ErrorMessage, "remove")
				}
			}
			require.Equal(t, 1, failed)
		})
	}
}

func TestRunCanDelete(t *testing.T) {
	t.Parallel()

	exec := models.FilesystemCapabilities{Read: true, Write: true, Identity: true}
	sftp := models.FilesystemCapabilities{Read: true, Write: true}
	remote := func(identityComplete bool, endpoint string) *models.OrphanScanRun {
		return &models.OrphanScanRun{FilesystemMode: models.FilesystemModeRemote, IdentityComplete: identityComplete, FilesystemEndpoint: endpoint}
	}
	for _, tc := range []struct {
		name string
		run  *models.OrphanScanRun
		caps models.FilesystemCapabilities
		want bool
	}{
		{"remote with identity on the exec tier", remote(true, "qui@box:22"), exec, true},
		{"remote with identity on the sftp tier", remote(true, "qui@box:22"), sftp, false},
		{"remote walk that missed an identity", remote(false, "qui@box:22"), exec, false},
		// A local file with no ID, such as one on a network share, deletes as it always has.
		{"local without complete identity", &models.OrphanScanRun{FilesystemMode: models.FilesystemModeLocal}, exec, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, runCanDelete(tc.run, tc.caps))
		})
	}
}

// A remote run from before the endpoint and identity were recorded keeps its
// preview, and the next scan replaces it.
func TestConfirmDeletion_RemoteRunFromBeforeTheColumnsKeepsItsPreview(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-old-run", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	runID := f.previewRun(t, func() int64 {
		id := f.createRun(t)
		require.NoError(t, f.store.UpdateRunFilesystemMode(t.Context(), id, models.FilesystemModeRemote, ""))
		return id
	})

	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, runID), fsops.ErrNotCapable)
	require.Equal(t, "preview_ready", f.run(t, runID).Status)
	f.requireNothingDeleted(t)
}
