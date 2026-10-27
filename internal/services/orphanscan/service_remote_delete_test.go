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

// The run is bound to the SSH user, host, port and pin it walked. A change
// between the scan and the confirm, or after the confirm, fails the run.
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

// Skipping cleanup adds a short notice after the scan's own warning, which says
// what the server did wrong and what to do.
func TestMaybeAutoCleanup_RemoteRunWithIncompleteIdentityKeepsBothWarnings(t *testing.T) {
	t.Parallel()

	f, _ := newSSHFixture(t, "orphanscan-remote-fallback-autoclean", sshtest.ExecSFTPOnly, models.SSHTierExec)

	run := f.scan(t, "scheduled")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, IdentityIncompleteMessage+"\n\n"+AutoCleanupSkippedMessage, run.ErrorMessage)
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

type recordingPublisher struct {
	mu     sync.Mutex
	events []activity.Event
}

func (p *recordingPublisher) Publish(ev activity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

// A host key replaced at the same address leaves the endpoint as it was, so
// the SSH change fails the remote preview itself. A local preview of the same
// instance walked no SSH server and keeps its review.
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

// pausingRemoveBackend holds its first Remove until resume closes, so the run
// stays deleting while an SSH change checks it.
type pausingRemoveBackend struct {
	fsops.Backend
	once   *sync.Once
	resume <-chan struct{}
}

func (b pausingRemoveBackend) Remove(ctx context.Context, path string, opts fsops.RemoveOptions) error {
	b.once.Do(func() { <-b.resume })
	return b.Backend.Remove(ctx, path, opts)
}

// sshLockHeld reports whether anyone holds the SSH lock of instance 1. A test
// asks it at a moment when only the code under test can hold the lock, so a
// lock that is missing shows at once, without a wait for a race to happen.
func sshLockHeld(svc *Service) bool {
	mu := svc.getInstanceMutex(svc.sshMu, 1)
	if mu.TryLock() {
		mu.Unlock()
		return false
	}
	return true
}

// An SSH change that passed its deletion check before the deletion started
// is stored and fails the preview before the run can start, so nothing is
// deleted through the new pin.
func TestChangeSSH_DeletionStartWaitsForAPendingChange(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-ssh-change-vs-delete-start", models.FilesystemModeRemote, nil)
	f.setTier(models.SSHTierExec)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	// The hook runs only once ChangeSSH has returned, so the change no longer
	// holds the lock, and the lock is held only if the deletion took it.
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
		// The deletion starts after the change passed its deletion check.
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

// A deletion holds the SSH lock until its run is deleting, so a change that
// arrives while the deletion starts waits for it and then stores nothing.
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
		// Nothing else runs yet, so the lock is held only if the deletion
		// took it.
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
	// The deletion holds the lock, so the change takes it only after the run
	// is deleting, wherever it is when the deletion goes on.
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

// A change that fails stores nothing, so the remote preview keeps its review.
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

// ignoredTorrentDirFixture adds keep/kept.mkv to the fixture's torrent and
// ignores keep/, so the scan walk never enters the folder that holds it.
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

// A hardlink stands in for a bind mount alias here: both are a second path
// to one file identity, and a test server cannot mount. The alias of a
// torrent's file at or below an ignore path is kept, in the same root and in
// another one, and the confirmed delete leaves it.
func TestExecuteScan_RemoteKeepsAliasOfAnIgnoredTorrentFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		otherRoot  bool
		ignoreFile bool
	}{
		{name: "same root"},
		{name: "another root", otherRoot: true},
		{name: "ignore path is the file", ignoreFile: true},
		{name: "ignore path is the file, another root", otherRoot: true, ignoreFile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, _ := newSSHFixture(t, "orphanscan-remote-ignored-alias", sshtest.ExecGNU, models.SSHTierExec)
			aliasDir := filepath.Join(filepath.Dir(f.orphan), "mirror")
			var extra []qbt.Torrent
			if tc.otherRoot {
				aliasDir = t.TempDir()
				writeFile(t, filepath.Join(aliasDir, "other.bin"))
				extra = append(extra, qbt.Torrent{Hash: "other", SavePath: sftpPath(aliasDir), State: qbt.TorrentStatePausedUp})
			}
			keep := ignoredTorrentDirFixture(t, f, sftpPath, extra...)
			if tc.ignoreFile {
				settings, err := f.store.GetSettings(t.Context(), 1)
				require.NoError(t, err)
				settings.IgnorePaths = []string{sftpPath(filepath.Join(keep, "kept.mkv"))}
				_, err = f.store.UpsertSettings(t.Context(), settings)
				require.NoError(t, err)
			}
			alias := filepath.Join(aliasDir, "kept.mkv")
			require.NoError(t, os.MkdirAll(aliasDir, 0o750))
			require.NoError(t, os.Link(filepath.Join(keep, "kept.mkv"), alias))

			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
			require.True(t, run.IdentityComplete)
			files, err := f.store.GetFilesForDeletion(t.Context(), run.ID)
			require.NoError(t, err)
			require.Len(t, files, 1)
			require.Equal(t, sftpPath(f.orphan), files[0].FilePath)

			require.NoError(t, f.svc.ConfirmDeletion(t.Context(), 1, run.ID))
			f.requireCompleted(t)
			require.NoFileExists(t, f.orphan)
			require.FileExists(t, alias)
		})
	}
}

// The remote walk of an ignore path that holds a torrent's files can be cut.
// Its files were never keyed, so the run cannot be deleted. One that is not
// on disk has no files to key and changes nothing.
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

// changeAfterRemoveBackend changes the instance after its first Remove, as an
// SSH change made around ChangeSSH would.
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

// The remote backend redials with the instance's current row, so a change to
// the SSH endpoint or the pin during a deletion stops it before the next
// entry, in the file loop and in the directory loop.
func TestExecuteDeletion_RemoteSSHChangeStopsTheLoop(t *testing.T) {
	t.Parallel()

	user := func(inst *models.Instance) { inst.SSHUsername = "someone-else" }
	for _, tc := range []struct {
		name   string
		change func(*models.Instance)
		// lastFile leaves one file to delete, so the change lands before the directories.
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

// replacePin stores a new pin at the same user, host and port and fails the
// remote previews as ChangeSSH does, but without its deletion check, as a
// change made around it would.
func replacePin(t *testing.T, f *modeFixture) {
	f.instance.mu.Lock()
	f.instance.inst.SSHHostKeyEncrypted = "enc-v2"
	f.instance.mu.Unlock()
	if err := f.svc.failRemotePreviews(t.Context(), 1); err != nil {
		t.Error(err)
	}
}

// replacePinOnFileMap replaces the pin the next time a scan or a deletion
// builds its file map. That is after the backend is built and, in a deletion,
// before its first entry.
func replacePinOnFileMap(t *testing.T, f *modeFixture) {
	files := stubSync(f.svc).getTorrentFilesBatch
	var once sync.Once
	stubSync(f.svc).getTorrentFilesBatch = func(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
		once.Do(func() { replacePin(t, f) })
		return files(ctx, instanceID, hashes)
	}
}

// A pin replaced while a remote scan runs leaves the mode and the address as
// they were, and failing the previews finds none yet. The scan compares
// the pin at its end, so a preview walked on the old server never becomes one
// a confirm or automatic cleanup could delete on the new one.
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

// saveNewKey stores a new private key through ChangeSSH, as the credentials
// route does. The user, host, port and pin stay as they were, so the endpoint
// a scan compares at its end does not change.
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

// A new private key can see other files through the same server and account.
// Saved while a remote scan walks, it finds no preview to fail yet, so the
// scan must not publish the preview it walked with the old key.
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

// A new private key saved after the scan reloaded the instance passes its
// endpoint compare and still finds the run scanning. The preview is published
// under the SSH lock only if no change was stored since the walk began.
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

// A new private key saved while failing the previews hits a database error
// leaves a ready preview walked with the old key. The key is part of the
// endpoint, so confirm still refuses that preview and deletes nothing.
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

// serverBackend records the server each walk reaches and holds the first
// walk until resume closes. The pool serves its cached connection until an
// SSH change drops it, and only that moves connected to the new server.
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

// An SSH change stores the new pin and then drops the pooled connection. A
// scan that read the row between the two would walk the old server under the
// new pin, and the change would find no preview to fail yet. The scan reads
// the row under the change's lock, so it walks the new server.
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

	// The scan reads the row first in Require. The hook goes on only once
	// ChangeSSH has returned, so the change no longer holds the lock, and the
	// lock is held only if the scan took it.
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

// ChangeSSH refuses a pin once the run is deleting, but one stored around it
// finds no preview to fail. The deletion compares the pin the run was walked
// with, so such a pin still stops it before any delete.
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

// An SSH change between the deletion's start check and its first entry stops
// the run before any delete, and a run that deleted nothing ends failed.
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

// A deletion that cannot reload the instance cannot tell whether the SSH
// settings changed, so it stops before the next entry.
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
