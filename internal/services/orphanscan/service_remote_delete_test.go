// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/testutil/sshtest"
)

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

func TestDeletion_RemoteEndpointChangedFailsTheRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		change func(*models.Instance)
	}{
		{"host", func(inst *models.Instance) { inst.SSHHost = "other.example" }},
		{"port", func(inst *models.Instance) { inst.SSHPort = 2222 }},
		{"user", func(inst *models.Instance) { inst.SSHUsername = "someone-else" }},
		{"pin", func(inst *models.Instance) { inst.SSHHostKeyEncrypted = "enc-v2" }},
	} {
		t.Run(tc.name+" changed before the confirm", func(t *testing.T) {
			t.Parallel()

			f := newModeFixture(t, "orphanscan-remote-endpoint-confirm", models.FilesystemModeRemote, nil)
			f.setTier(models.SSHTierExec)
			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

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

func TestMaybeAutoCleanup_RemoteRunWithIncompleteIdentityKeepsBothWarnings(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-fallback-autoclean", sshtest.ExecSFTPOnly, models.SSHTierExec)

	run := f.scan(t, "scheduled")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, IdentityIncompleteMessage+"\n\n"+AutoCleanupSkippedMessage, run.ErrorMessage)
	f.requireNothingDeleted(t)
	f.requireNoFailureEvent(t)
}

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
		{"local without complete identity", &models.OrphanScanRun{FilesystemMode: models.FilesystemModeLocal}, exec, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, runCanDelete(tc.run, tc.caps))
		})
	}
}

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

type recordingPublisher struct {
	mu     sync.Mutex
	events []activity.Event
}

func (p *recordingPublisher) Publish(ev activity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func TestChangeSSH_FailsOnlyRemotePreviews(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-fail-remote-previews", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	localID := f.createRun(t)
	require.NoError(t, f.store.UpdateRunFilesystemMode(t.Context(), localID, models.FilesystemModeLocal, ""))
	require.NoError(t, f.store.UpdateRunStatus(t.Context(), localID, "preview_ready"))
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	publisher := &recordingPublisher{}
	f.svc.SetActivityPublisher(publisher)

	require.NoError(t, f.svc.ChangeSSH(t.Context(), 1, func() error { return nil }))

	failed := f.run(t, run.ID)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
	require.Equal(t, "preview_ready", f.run(t, localID).Status)
	require.Equal(t, []activity.Event{{Kind: activity.KindOrphanScanRun, InstanceID: 1, ResourceID: strconv.FormatInt(run.ID, 10)}}, publisher.events)
	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), ErrInvalidRunStatus)
	f.requireNothingDeleted(t)
}

type pausingRemoveBackend struct {
	fsops.Backend
	once   *sync.Once
	resume <-chan struct{}
}

func (b pausingRemoveBackend) Remove(ctx context.Context, path string, opts fsops.RemoveOptions) error {
	b.once.Do(func() { <-b.resume })
	return b.Backend.Remove(ctx, path, opts)
}

func sshLockHeld(svc *Service) bool {
	mu := svc.getInstanceMutex(svc.sshMu, 1)
	if mu.TryLock() {
		mu.Unlock()
		return false
	}
	return true
}

func TestChangeSSH_DeletionStartWaitsForAPendingChange(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-ssh-change-vs-delete-start", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	changeReturned := make(chan struct{})
	var heldAtStart bool
	f.svc.beforeDeletionStart = func() {
		<-changeReturned
		heldAtStart = sshLockHeld(f.svc)
	}
	var heldInChange bool
	deleted := make(chan struct{})
	require.NoError(t, f.svc.ChangeSSH(t.Context(), 1, func() error {
		heldInChange = sshLockHeld(f.svc)
		go func() {
			f.svc.executeDeletion(t.Context(), 1, run.ID)
			close(deleted)
		}()
		f.instance.mu.Lock()
		f.instance.inst.SSHHostKeyEncrypted = "enc-v2"
		f.instance.mu.Unlock()
		return nil
	}))
	close(changeReturned)
	select {
	case <-deleted:
	case <-time.After(10 * time.Second):
		t.Fatal("the deletion did not finish")
	}

	require.True(t, heldInChange, "the change must run under the SSH lock")
	require.True(t, heldAtStart, "the deletion must start under the SSH lock")
	failed := f.run(t, run.ID)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
	f.requireNothingDeleted(t)
}

func TestChangeSSH_WaitsForAStartingDeletion(t *testing.T) {
	t.Parallel()

	once := &sync.Once{}
	resume := make(chan struct{})
	var f *modeFixture
	f = newModeFixture(t, "orphanscan-delete-start-vs-ssh-change", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
		return pausingRemoveBackend{Backend: countingBackend{Backend: newTestBackend(), removes: f.removes}, once: once, resume: resume}
	})
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	starting := make(chan struct{})
	start := make(chan struct{})
	var heldAtStart bool
	f.svc.beforeDeletionStart = func() {
		heldAtStart = sshLockHeld(f.svc)
		close(starting)
		<-start
	}
	deleted := make(chan struct{})
	go func() {
		f.svc.executeDeletion(t.Context(), 1, run.ID)
		close(deleted)
	}()
	<-starting

	changed := make(chan error, 1)
	go func() {
		changed <- f.svc.ChangeSSH(t.Context(), 1, func() error {
			f.instance.mu.Lock()
			f.instance.inst.SSHHostKeyEncrypted = "enc-v2"
			f.instance.mu.Unlock()
			return nil
		})
	}()
	close(start)

	require.True(t, heldAtStart, "the deletion must start under the SSH lock")
	require.ErrorIs(t, <-changed, ErrDeletionInProgress)
	close(resume)
	<-deleted
	stored, err := f.instance.Get(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "enc-v1", stored.SSHHostKeyEncrypted)
	require.Equal(t, "completed", f.run(t, run.ID).Status)
}

func TestChangeSSH_FailedChangeKeepsThePreview(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-ssh-change-fails", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	errStore := errors.New("store failed")
	require.ErrorIs(t, f.svc.ChangeSSH(t.Context(), 1, func() error { return errStore }), errStore)
	require.Equal(t, "preview_ready", f.run(t, run.ID).Status)
}

func ignoredTorrentDirFixture(t *testing.T, f *modeFixture, savePath func(string) string, extra ...qbt.Torrent) (keep string) {
	t.Helper()

	root := filepath.Dir(f.orphan)
	keep = filepath.Join(root, "keep")
	writeFile(t, filepath.Join(keep, "kept.mkv"))
	torrents := append([]qbt.Torrent{{Hash: "owned", SavePath: savePath(root), State: qbt.TorrentStatePausedUp}}, extra...)
	stubSync(f.svc).getAllTorrents = func(context.Context, int) ([]qbt.Torrent, error) { return torrents, nil }
	stubSync(f.svc).getTorrentFilesBatch = func(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{
			"owned": {{Name: "owned.mkv", Size: 1}, {Name: "keep/kept.mkv", Size: 1}},
			"other": {{Name: "other.bin", Size: 1}},
		}, nil
	}
	settings, err := f.store.GetSettings(t.Context(), 1)
	require.NoError(t, err)
	settings.IgnorePaths = []string{savePath(keep)}
	_, err = f.store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)
	return keep
}

func TestExecuteScan_RemoteKeepsAliasOfAnIgnoredTorrentFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		otherRoot  bool
		ignoreFile bool
	}{
		{name: "hardlink in the same root"},
		{name: "another root", otherRoot: true},
		{name: "hardlink in the same root, ignore path is the file", ignoreFile: true},
		{name: "ignore path is the file, another root", otherRoot: true, ignoreFile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, _ := newSSHFixture(t, "orphanscan-remote-ignored-alias", sshtest.ExecGNU, models.SSHTierExec)
			alias := filepath.Join(filepath.Dir(f.orphan), "mirror", "kept.mkv")
			var extra []qbt.Torrent
			if tc.otherRoot {
				link := filepath.Join(t.TempDir(), "link")
				symlinkOrSkip(t, filepath.Dir(f.orphan), link)
				alias = filepath.Join(link, "keep", "kept.mkv")
				extra = append(extra, qbt.Torrent{Hash: "other", SavePath: sftpPath(filepath.Dir(alias)), State: qbt.TorrentStatePausedUp})
			}
			keep := ignoredTorrentDirFixture(t, f, sftpPath, extra...)
			if tc.ignoreFile {
				settings, err := f.store.GetSettings(t.Context(), 1)
				require.NoError(t, err)
				settings.IgnorePaths = []string{sftpPath(filepath.Join(keep, "kept.mkv"))}
				_, err = f.store.UpsertSettings(t.Context(), settings)
				require.NoError(t, err)
			}
			kept := filepath.Join(keep, "kept.mkv")
			if tc.otherRoot {
				writeFile(t, filepath.Join(keep, "other.bin"))
				require.NoError(t, os.Link(kept, filepath.Join(t.TempDir(), "kept.mkv")))
			} else {
				require.NoError(t, os.MkdirAll(filepath.Dir(alias), 0o750))
				require.NoError(t, os.Link(kept, alias))
			}

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.True(t, run.IdentityComplete)
			files, err := f.store.GetFilesForDeletion(t.Context(), run.ID)
			require.NoError(t, err)
			listed := make([]string, 0, len(files))
			for _, file := range files {
				listed = append(listed, file.FilePath)
			}
			if tc.otherRoot {
				require.Equal(t, []string{sftpPath(f.orphan)}, listed)
			} else {
				require.ElementsMatch(t, []string{sftpPath(f.orphan), sftpPath(alias)}, listed)
			}

			require.NoError(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID))
			f.requireCompleted(t)
			require.NoFileExists(t, f.orphan)
			require.FileExists(t, kept)
			if tc.otherRoot {
				require.FileExists(t, alias)
			} else {
				require.NoFileExists(t, alias)
			}
		})
	}
}

func TestExecuteScan_RemoteIgnoredTorrentDirWalk(t *testing.T) {
	t.Parallel()

	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "cut walk", true: "not on disk"}[missing], func(t *testing.T) {
			t.Parallel()

			var f *modeFixture
			var keep string
			f = newModeFixture(t, "orphanscan-remote-ignored-dir-walk", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
				return cutWalkBackend{Backend: countingBackend{Backend: newTestBackend(), removes: f.removes}, root: keep}
			})
			f.setTier(models.SSHTierExec)
			keep = ignoredTorrentDirFixture(t, f, func(p string) string { return p })
			if missing {
				require.NoError(t, os.RemoveAll(keep))
			}

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.Equal(t, missing, run.IdentityComplete)
			require.Equal(t, !missing, run.Partial)
		})
	}
}

type changeAfterRemoveBackend struct {
	fsops.Backend
	once   *sync.Once
	change func()
}

func (b changeAfterRemoveBackend) Remove(ctx context.Context, path string, opts fsops.RemoveOptions) error {
	err := b.Backend.Remove(ctx, path, opts)
	b.once.Do(b.change)
	return err
}

func TestExecuteDeletion_RemoteSSHChangeStopsTheLoop(t *testing.T) {
	t.Parallel()

	user := func(inst *models.Instance) { inst.SSHUsername = "someone-else" }
	for _, tc := range []struct {
		name     string
		change   func(*models.Instance)
		lastFile bool
	}{
		{"endpoint", user, false},
		{"pin", func(inst *models.Instance) { inst.SSHHostKeyEncrypted = "enc-v2" }, false},
		{"endpoint after the last file", user, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *modeFixture
			f = newModeFixture(t, "orphanscan-remote-ssh-change-mid-delete", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
				return changeAfterRemoveBackend{
					Backend: countingBackend{Backend: newTestBackend(), removes: f.removes},
					once:    &sync.Once{},
					change: func() {
						f.instance.mu.Lock()
						defer f.instance.mu.Unlock()
						tc.change(&f.instance.inst)
					},
				}
			})
			f.setTier(models.SSHTierExec)
			root := filepath.Dir(f.orphan)
			if !tc.lastFile {
				require.NoError(t, os.WriteFile(filepath.Join(root, "a-later.mkv"), []byte("later"), 0o600))
			}
			emptyDir := filepath.Join(root, "empty")
			require.NoError(t, os.Mkdir(emptyDir, 0o750))

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.NoError(t, f.store.InsertFiles(t.Context(), run.ID, []models.OrphanScanFile{{FilePath: emptyDir, IsAbandonedDir: true, Status: "pending"}}))
			f.svc.executeDeletion(t.Context(), 1, run.ID)

			done := f.run(t, run.ID)
			require.Equal(t, "completed", done.Status, "run error: %s", done.ErrorMessage)
			require.Equal(t, 1, done.FilesDeleted)
			require.Contains(t, done.ErrorMessage, FilesystemModeChangedMessage)
			require.EqualValues(t, 1, f.removes.Load(), "no Remove may follow the change")
			require.DirExists(t, emptyDir)
			require.FileExists(t, f.owned)

			files, err := f.store.ListFiles(t.Context(), run.ID, 100, 0, "")
			require.NoError(t, err)
			statuses := map[string]int{}
			for _, file := range files {
				statuses[file.Status]++
			}
			skipped := 2
			if tc.lastFile {
				skipped = 1
			}
			require.Equal(t, map[string]int{"deleted": 1, "skipped": skipped}, statuses)
		})
	}
}

func replacePin(t *testing.T, f *modeFixture) {
	f.instance.mu.Lock()
	f.instance.inst.SSHHostKeyEncrypted = "enc-v2"
	f.instance.mu.Unlock()
	if err := f.svc.failRemotePreviews(t.Context(), 1); err != nil {
		t.Error(err)
	}
}

func replacePinOnFileMap(t *testing.T, f *modeFixture) {
	files := stubSync(f.svc).getTorrentFilesBatch
	var once sync.Once
	stubSync(f.svc).getTorrentFilesBatch = func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
		once.Do(func() { replacePin(t, f) })
		return files(ctx, instanceID, hashes)
	}
}

func TestExecuteScan_RemotePinReplacedDuringTheScanFailsTheRun(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-pin-mid-scan", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	replacePinOnFileMap(t, f)

	run := f.scan(t, "manual")
	require.Equal(t, "failed", run.Status)
	require.Equal(t, ScanModeChangedMessage, run.ErrorMessage)
	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), ErrInvalidRunStatus)
	f.requireNothingDeleted(t)
}

func saveNewKey(t *testing.T, f *modeFixture) {
	err := f.svc.ChangeSSH(t.Context(), 1, func() error {
		f.instance.mu.Lock()
		f.instance.inst.SSHKeyEncrypted = "key-v2"
		f.instance.mu.Unlock()
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func TestExecuteScan_RemoteKeySavedDuringTheWalkFailsTheRun(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-key-mid-walk", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	files := stubSync(f.svc).getTorrentFilesBatch
	var once sync.Once
	stubSync(f.svc).getTorrentFilesBatch = func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
		once.Do(func() { saveNewKey(t, f) })
		return files(ctx, instanceID, hashes)
	}

	run := f.scan(t, "manual")
	require.Equal(t, "failed", run.Status)
	require.Equal(t, ScanModeChangedMessage, run.ErrorMessage)
	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), ErrInvalidRunStatus)
	f.requireNothingDeleted(t)
}

func TestExecuteScan_RemoteKeySavedBeforeThePreviewIsPublishedFailsTheRun(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-key-before-preview", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	f.svc.beforePreviewReady = func() { saveNewKey(t, f) }

	run := f.scan(t, "manual")
	require.Equal(t, "failed", run.Status)
	require.Equal(t, ScanModeChangedMessage, run.ErrorMessage)
	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), ErrInvalidRunStatus)
	f.requireNothingDeleted(t)
}

func TestConfirmDeletion_RemoteKeySavedWhenFailingThePreviewsErrsIsRefused(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-key-fail-previews-errs", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	_, err := f.db.ExecContext(t.Context(), `
		CREATE TRIGGER refuse_failing_runs BEFORE UPDATE OF status ON orphan_scan_runs
		WHEN NEW.status = 'failed'
		BEGIN SELECT RAISE(ABORT, 'injected store error'); END
	`)
	require.NoError(t, err)
	saveNewKey(t, f)
	require.Equal(t, "preview_ready", f.run(t, run.ID).Status, "the injected error must keep the preview ready")
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER refuse_failing_runs`)
	require.NoError(t, err)

	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID), ErrFilesystemModeChanged)
	require.Equal(t, "failed", f.run(t, run.ID).Status)
	f.requireNothingDeleted(t)
}

type serverBackend struct {
	fsops.Backend
	connected *atomic.Value
	mu        *sync.Mutex
	walked    *[]string
	once      *sync.Once
	walking   chan struct{}
	resume    <-chan struct{}
}

func (b serverBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	b.mu.Lock()
	*b.walked = append(*b.walked, b.connected.Load().(string))
	b.mu.Unlock()
	b.once.Do(func() {
		close(b.walking)
		<-b.resume
	})
	return b.Backend.WalkDir(ctx, root, opts)
}

func TestExecuteScan_RemoteScanWaitsForAPendingSSHChange(t *testing.T) {
	t.Parallel()

	var connected atomic.Value
	connected.Store("old")
	var mu sync.Mutex
	var walked []string
	walking := make(chan struct{})
	resume := make(chan struct{})
	var f *modeFixture
	f = newModeFixture(t, "orphanscan-scan-vs-ssh-change", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
		return serverBackend{
			Backend:   countingBackend{Backend: newTestBackend(), removes: f.removes},
			connected: &connected, mu: &mu, walked: &walked,
			once: &sync.Once{}, walking: walking, resume: resume,
		}
	})
	f.setTier(models.SSHTierExec)
	runID, err := f.store.CreateRunIfNoActive(t.Context(), 1, "manual")
	require.NoError(t, err)

	changeReturned := make(chan struct{})
	var heldAtRead bool
	var readOnce sync.Once
	f.instance.onGet = func() {
		readOnce.Do(func() {
			<-changeReturned
			heldAtRead = sshLockHeld(f.svc)
		})
	}
	scanned := make(chan struct{})
	var heldInChange bool
	require.NoError(t, f.svc.ChangeSSH(t.Context(), 1, func() error {
		heldInChange = sshLockHeld(f.svc)
		f.instance.mu.Lock()
		f.instance.inst.SSHHostKeyEncrypted = "enc-v2"
		f.instance.mu.Unlock()
		go func() {
			f.svc.executeScan(t.Context(), 1, runID)
			close(scanned)
		}()
		connected.Store("new")
		return nil
	}))
	close(changeReturned)
	select {
	case <-walking:
	case <-time.After(10 * time.Second):
		t.Fatal("the scan did not reach its walk")
	}
	close(resume)
	<-scanned

	require.True(t, heldInChange, "the change must run under the SSH lock")
	require.True(t, heldAtRead, "the scan must read the instance under the SSH lock")
	run := f.run(t, runID)
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"new"}, walked)
}

func TestExecuteDeletion_RemotePinReplacedAfterTheStartDeletesNothing(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-pin-at-delete-start", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	var statusAtReplace string
	var once sync.Once
	f.instance.onGet = func() {
		once.Do(func() {
			statusAtReplace = f.run(t, run.ID).Status
			replacePin(t, f)
		})
	}
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	require.Equal(t, "deleting", statusAtReplace, "the pin must change after the run went deleting")
	failed := f.run(t, run.ID)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
	f.requireNothingDeleted(t)
}

func TestExecuteDeletion_RemoteSSHChangeBeforeTheFirstEntryFailsTheRun(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-ssh-change-before-delete", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	replacePinOnFileMap(t, f)
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	failed := f.run(t, run.ID)
	require.Equal(t, "failed", failed.Status)
	require.Zero(t, failed.FilesDeleted)
	require.Contains(t, failed.ErrorMessage, FilesystemModeChangedMessage)
	f.requireNothingDeleted(t)
	files, err := f.store.ListFiles(t.Context(), run.ID, 100, 0, "")
	require.NoError(t, err)
	for _, file := range files {
		require.Equal(t, "skipped", file.Status)
	}
}

func TestExecuteDeletion_RemoteInstanceReloadErrorStopsTheLoop(t *testing.T) {
	t.Parallel()

	var f *modeFixture
	f = newModeFixture(t, "orphanscan-remote-reload-error-mid-delete", models.FilesystemModeRemote, func(*models.Instance) fsops.Backend {
		return changeAfterRemoveBackend{
			Backend: countingBackend{Backend: newTestBackend(), removes: f.removes},
			once:    &sync.Once{},
			change: func() {
				stubSync(f.svc).listInstances = func(context.Context) ([]*models.Instance, error) {
					return nil, errors.New("database is locked")
				}
			},
		}
	})
	f.setTier(models.SSHTierExec)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(f.orphan), "a-later.mkv"), []byte("later"), 0o600))

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	done := f.run(t, run.ID)
	require.Equal(t, 1, done.FilesDeleted)
	require.Contains(t, done.ErrorMessage, FilesystemModeChangedMessage)
	require.EqualValues(t, 1, f.removes.Load(), "no Remove may follow the failed reload")
	require.FileExists(t, f.owned)
}
