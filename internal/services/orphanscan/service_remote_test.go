// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/remote"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// switchableInstance is instance 1 as both the pool and the service read it,
// so a test can change its filesystem mode between two steps.
type switchableInstance struct {
	mu   sync.Mutex
	inst models.Instance
}

func (s *switchableInstance) Get(_ context.Context, _ int) (*models.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst := s.inst
	return &inst, nil
}

func (s *switchableInstance) set(mode models.FilesystemMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inst.HasLocalFilesystemAccess = mode == models.FilesystemModeLocal
	s.inst.SSHKeyEncrypted, s.inst.SSHHostKeyEncrypted = "", ""
	if mode == models.FilesystemModeRemote {
		s.inst.SSHKeyEncrypted, s.inst.SSHHostKeyEncrypted = "enc-key", "enc-v1"
	}
}

// countingBackend is a backend that can really delete, so a refusal that
// fails to hold shows up as a Remove call and a missing file.
type countingBackend struct {
	fsops.Backend
	removes *atomic.Int32
}

func (b countingBackend) Remove(ctx context.Context, path string, opts fsops.RemoveOptions) error {
	b.removes.Add(1)
	return b.Backend.Remove(ctx, path, opts)
}

type modeFixture struct {
	svc      *Service
	store    *models.OrphanScanStore
	db       *database.DB
	instance *switchableInstance
	removes  *atomic.Int32
	events   chan notifications.Event
	orphan   string
	owned    string
}

// newModeFixture scans nothing yet. It lays out one owned file and one orphan
// under a single save path, and routes remote mode through remoteBackend, or
// through a counting local backend when that is nil.
func newModeFixture(t *testing.T, name string, mode models.FilesystemMode, remoteBackend func(*models.Instance) fsops.Backend) *modeFixture {
	t.Helper()

	root := t.TempDir()
	owned := filepath.Join(root, "owned.mkv")
	orphan := filepath.Join(root, "orphan.mkv")
	require.NoError(t, os.WriteFile(owned, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(orphan, []byte("orphan"), 0o600))

	db := testdb.NewMigratedSQLite(t, name)
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	_, err = instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)

	instance := &switchableInstance{inst: models.Instance{ID: 1, Name: "test", IsActive: true, SSHHost: "box.example", SSHPort: 22}}
	instance.set(mode)

	removes := &atomic.Int32{}
	local := countingBackend{Backend: newTestBackend(), removes: removes}
	if remoteBackend == nil {
		remoteBackend = func(*models.Instance) fsops.Backend { return local }
	}

	events := make(chan notifications.Event, 4)
	svc := NewService(DefaultConfig(), nil, store, nil, scanNotifier{events: events}, fsops.NewPoolWithRemote(instance, local, remoteBackend))
	stubSync(svc).getClient = func(_ context.Context, _ int) (healthChecker, error) {
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}
	stubSync(svc).listInstances = func(ctx context.Context) ([]*models.Instance, error) {
		inst, _ := instance.Get(ctx, 1)
		return []*models.Instance{inst}, nil
	}
	stubSync(svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{Hash: "owned", SavePath: root, State: qbt.TorrentStatePausedUp}}, nil
	}
	stubSync(svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
		return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}}, nil
	}
	stubSync(svc).getAppPreferences = func(_ context.Context, _ int) (qbt.AppPreferences, error) {
		return qbt.AppPreferences{SavePath: root}, nil
	}
	stubSync(svc).subcategoriesEnabled = func(_ context.Context, _ int) (bool, error) { return false, nil }
	stubSync(svc).getCategories = func(_ context.Context, _ int) (map[string]qbt.Category, error) {
		return map[string]qbt.Category{}, nil
	}

	_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{
		InstanceID:          1,
		Enabled:             true,
		GracePeriodMinutes:  0,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupEnabled:  true,
		AutoCleanupMaxFiles: 100,
	})
	require.NoError(t, err)

	return &modeFixture{svc: svc, store: store, db: db, instance: instance, removes: removes, events: events, orphan: orphan, owned: owned}
}

func (f *modeFixture) scan(t *testing.T, triggeredBy string) *models.OrphanScanRun {
	t.Helper()

	runID, err := f.store.CreateRunIfNoActive(t.Context(), 1, triggeredBy)
	require.NoError(t, err)
	f.svc.executeScan(t.Context(), 1, runID)
	return f.run(t, runID)
}

func (f *modeFixture) run(t *testing.T, runID int64) *models.OrphanScanRun {
	t.Helper()

	run, err := f.store.GetRun(t.Context(), runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	return run
}

func (f *modeFixture) requireNothingDeleted(t *testing.T) {
	t.Helper()

	require.Zero(t, f.removes.Load(), "no Remove may reach the backend")
	require.FileExists(t, f.orphan)
	require.FileExists(t, f.owned)
}

func (f *modeFixture) requireNoFailureEvent(t *testing.T) {
	t.Helper()

	select {
	case event := <-f.events:
		require.NotEqual(t, notifications.EventOrphanScanFailed, event.Type, "a refusal is not a failed run to notify about")
	default:
	}
}

// sftpCreds stands in for the instance store's decryption, as remote_test.go
// does. The dial re-reads the row through rows, so it sees the fixture's
// current instance.
type sftpCreds struct {
	rows *switchableInstance
	key  string
	pin  []byte
}

func (c sftpCreds) Get(ctx context.Context, id int) (*models.Instance, error) {
	return c.rows.Get(ctx, id)
}

func (c sftpCreds) GetDecryptedSSHKey(*models.Instance) (string, error) { return c.key, nil }
func (c sftpCreds) GetHostKeyPin(*models.Instance) ([]byte, error)      { return c.pin, nil }

func TestExecuteScan_RemoteInstanceScansOverSFTP(t *testing.T) {
	t.Parallel()

	hostKey := sshtest.NewSigner()
	server := sshtest.NewServer(t, hostKey, sshtest.ExecSFTPOnly)
	host, portText, err := net.SplitHostPort(server.Addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	var pool *sshpool.Pool
	f := newModeFixture(t, "orphanscan-remote-e2e", models.FilesystemModeRemote, func(inst *models.Instance) fsops.Backend {
		return remote.New(pool, inst)
	})
	pool = sshpool.NewPool(sshpool.NewDialer(sftpCreds{rows: f.instance, key: sshtest.PrivateKey(""), pin: hostKey.PublicKey().Marshal()}))
	t.Cleanup(pool.Close)
	f.instance.mu.Lock()
	f.instance.inst.SSHHost, f.instance.inst.SSHPort, f.instance.inst.SSHUsername = host, port, "qui"
	f.instance.mu.Unlock()

	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, models.FilesystemModeRemote, run.FilesystemMode)
	require.Positive(t, server.Accepts(), "the walk must go over SSH")

	files, err := f.store.GetFilesForDeletion(t.Context(), run.ID)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, f.orphan, files[0].FilePath)
}

// flippingBackend calls flip once root has handed out enough entries that the
// walk is well under way, so a test can change the instance mid-walk.
type flippingBackend struct {
	fsops.Backend
	root string
	flip func()
}

func (b flippingBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil || root != b.root {
		return ch, err
	}
	out := make(chan fsops.WalkEntry)
	go func() {
		defer close(out)
		n := 0
		for entry := range ch {
			select {
			case out <- entry:
			case <-ctx.Done():
				for range ch { //nolint:revive // drain so the inner walk can exit
				}
				return
			}
			if n++; n == 100 {
				b.flip()
			}
		}
	}()
	return out, nil
}

// Changing an instance's filesystem access invalidates its SSH connection, so
// a remote walk in progress ends as a lost connection. Roots walked before the
// cut must not become a preview that no confirmation could ever delete.
func TestExecuteScan_ModeChangedMidScanFailsTheRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		twoRoots bool
		switched models.FilesystemMode
	}{
		{"single root switched to local", false, models.FilesystemModeLocal},
		{"single root switched to no access", false, models.FilesystemModeNone},
		{"second root switched to local", true, models.FilesystemModeLocal},
		{"second root switched to no access", true, models.FilesystemModeNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hostKey := sshtest.NewSigner()
			server := sshtest.NewServer(t, hostKey, sshtest.ExecSFTPOnly)
			host, portText, err := net.SplitHostPort(server.Addr)
			require.NoError(t, err)
			port, err := strconv.Atoi(portText)
			require.NoError(t, err)

			var pool *sshpool.Pool
			var f *modeFixture
			var once sync.Once
			var bigRoot string
			f = newModeFixture(t, "orphanscan-mode-changed-mid-scan", models.FilesystemModeRemote, func(inst *models.Instance) fsops.Backend {
				return flippingBackend{Backend: remote.New(pool, inst), root: bigRoot, flip: func() {
					once.Do(func() {
						f.instance.set(tc.switched)
						pool.Invalidate(1)
					})
				}}
			})
			pool = sshpool.NewPool(sshpool.NewDialer(sftpCreds{rows: f.instance, key: sshtest.PrivateKey(""), pin: hostKey.PublicKey().Marshal()}))
			t.Cleanup(pool.Close)
			f.instance.mu.Lock()
			f.instance.inst.SSHHost, f.instance.inst.SSHPort, f.instance.inst.SSHUsername = host, port, "qui"
			f.instance.mu.Unlock()

			// Made after the fixture's root so it sorts after it, and a two-root
			// scan has finished that root before the mode changes.
			bigRoot = filepath.Join(t.TempDir(), "zbig")
			for i := range 300 {
				dir := filepath.Join(bigRoot, fmt.Sprintf("d%03d", i))
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "orphan.bin"), []byte("x"), 0o600))
			}
			torrents := []qbt.Torrent{{Hash: "big", SavePath: bigRoot, State: qbt.TorrentStatePausedUp}}
			if tc.twoRoots {
				torrents = append([]qbt.Torrent{{Hash: "owned", SavePath: filepath.Dir(f.orphan), State: qbt.TorrentStatePausedUp}}, torrents...)
			}
			stubSync(f.svc).getAllTorrents = func(_ context.Context, _ int) ([]qbt.Torrent, error) { return torrents, nil }
			stubSync(f.svc).getTorrentFilesBatch = func(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
				return map[string]qbt.TorrentFiles{"owned": {{Name: "owned.mkv", Size: 1}}, "big": {{Name: "d000/kept.bin", Size: 1}}}, nil
			}

			run := f.scan(t, "scheduled")
			require.Equal(t, "failed", run.Status, "run error: %s", run.ErrorMessage)
			require.Equal(t, ScanModeChangedMessage, run.ErrorMessage)
			require.Equal(t, models.FilesystemModeRemote, run.FilesystemMode)

			files, err := f.store.ListFiles(t.Context(), run.ID, 1000, 0, "")
			require.NoError(t, err)
			require.Empty(t, files, "a run failed for a mode change must record no files")

			// A scheduled run is never looked at, so its failure has to be notified.
			select {
			case event := <-f.events:
				require.Equal(t, notifications.EventOrphanScanFailed, event.Type)
				require.Equal(t, ScanModeChangedMessage, event.ErrorMessage)
			default:
				t.Fatal("a scan failed for a mode change must notify")
			}
			f.requireNothingDeleted(t)
		})
	}
}

func TestConfirmDeletion_RemoteRunIsRefusedAndKeepsItsPreview(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-refuse", models.FilesystemModeRemote, nil)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, models.FilesystemModeRemote, run.FilesystemMode)

	err := f.svc.ConfirmDeletion(t.Context(), 1, run.ID)
	require.ErrorIs(t, err, fsops.ErrNotCapable)

	require.Equal(t, "preview_ready", f.run(t, run.ID).Status)
	f.requireNothingDeleted(t)
	f.requireNoFailureEvent(t)
}

func TestConfirmDeletion_ModeChangedSinceTheScanFailsTheRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		scanned  models.FilesystemMode
		switched models.FilesystemMode
	}{
		{"remote preview confirmed as local", models.FilesystemModeRemote, models.FilesystemModeLocal},
		{"local preview confirmed after access was removed", models.FilesystemModeLocal, models.FilesystemModeNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newModeFixture(t, "orphanscan-mode-changed", tc.scanned, nil)
			run := f.scan(t, "manual")
			require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

			f.instance.set(tc.switched)
			err := f.svc.ConfirmDeletion(t.Context(), 1, run.ID)
			require.ErrorIs(t, err, ErrFilesystemModeChanged)

			stale := f.run(t, run.ID)
			require.Equal(t, "failed", stale.Status)
			require.Equal(t, FilesystemModeChangedMessage, stale.ErrorMessage)
			f.requireNothingDeleted(t)
			f.requireNoFailureEvent(t)
		})
	}
}

// The instance can change after ConfirmDeletion checked it and before the
// deletion goroutine resolves its backend. The user already confirmed, so this
// failure is notified like any other.
func TestExecuteDeletion_ModeChangedAfterConfirmationDeletesNothing(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-mode-changed-late", models.FilesystemModeLocal, nil)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	f.instance.set(models.FilesystemModeRemote)
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	failed := f.run(t, run.ID)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
	f.requireNothingDeleted(t)
	select {
	case event := <-f.events:
		require.Equal(t, notifications.EventOrphanScanFailed, event.Type)
	default:
		t.Fatal("a deletion that failed after confirmation must notify")
	}
}

// previewRun builds a preview_ready run over the fixture's orphan whose mode
// column holds whatever create left there.
func (f *modeFixture) previewRun(t *testing.T, create func() int64) int64 {
	t.Helper()

	runID := create()
	require.NoError(t, f.store.UpdateRunScanPaths(t.Context(), runID, []string{filepath.Dir(f.orphan)}))
	require.NoError(t, f.store.InsertFiles(t.Context(), runID, []models.OrphanScanFile{{FilePath: f.orphan, FileSize: 6, Status: "pending"}}))
	require.NoError(t, f.store.UpdateRunFoundStats(t.Context(), runID, 1, false, 6))
	require.NoError(t, f.store.UpdateRunStatus(t.Context(), runID, "preview_ready"))
	return runID
}

func (f *modeFixture) createRun(t *testing.T) int64 {
	t.Helper()

	runID, err := f.store.CreateRunIfNoActive(t.Context(), 1, "manual")
	require.NoError(t, err)
	return runID
}

// A run whose mode write never happened must not pass for a local one.
func TestConfirmDeletion_RunWithoutASavedModeCannotDelete(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-unsaved-mode", models.FilesystemModeLocal, nil)
	runID := f.previewRun(t, func() int64 { return f.createRun(t) })

	require.ErrorIs(t, f.svc.ConfirmDeletion(t.Context(), 1, runID), ErrFilesystemModeChanged)
	f.requireNothingDeleted(t)
}

// A run from before the column existed was scanned locally, and confirms that way.
func TestConfirmDeletion_RunWithoutARecordedModeConfirmsAsLocal(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-legacy-mode", models.FilesystemModeLocal, nil)
	var runID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(),
		`INSERT INTO orphan_scan_runs (instance_id, status, triggered_by) VALUES (1, 'pending', 'manual') RETURNING id`,
	).Scan(&runID))
	require.NoError(t, f.store.UpdateRunScanPaths(t.Context(), runID, []string{filepath.Dir(f.orphan)}))
	require.NoError(t, f.store.InsertFiles(t.Context(), runID, []models.OrphanScanFile{{FilePath: f.orphan, FileSize: 6, Status: "pending"}}))
	require.NoError(t, f.store.UpdateRunFoundStats(t.Context(), runID, 1, false, 6))
	require.NoError(t, f.store.UpdateRunStatus(t.Context(), runID, "preview_ready"))
	require.Equal(t, models.FilesystemModeLocal, f.run(t, runID).FilesystemMode)

	require.NoError(t, f.svc.ConfirmDeletion(t.Context(), 1, runID))
	select {
	case event := <-f.events:
		require.Equal(t, notifications.EventOrphanScanCompleted, event.Type, event.ErrorMessage)
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not finish")
	}
	require.NoFileExists(t, f.orphan)
	require.FileExists(t, f.owned)
}

func TestMaybeAutoCleanup_RemoteRunKeepsItsPreviewWithAWarning(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-remote-autoclean", models.FilesystemModeRemote, nil)
	run := f.scan(t, "scheduled")

	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Contains(t, run.ErrorMessage, "Automatic cleanup does not run for instances reached over SSH")
	f.requireNothingDeleted(t)
	f.requireNoFailureEvent(t)
}

func TestExecuteScan_FailsWhenTheModeCannotBeSaved(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-mode-write", models.FilesystemModeRemote, nil)
	_, err := f.db.ExecContext(t.Context(), `
		CREATE TRIGGER reject_orphan_mode BEFORE UPDATE OF filesystem_mode ON orphan_scan_runs
		BEGIN SELECT RAISE(ABORT, 'mode write failed'); END
	`)
	require.NoError(t, err)

	run := f.scan(t, "manual")
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.ErrorMessage, "mode write failed")
	f.requireNothingDeleted(t)
}

func TestCheckScheduledScans_AdmitsLocalAndRemoteInstances(t *testing.T) {
	t.Parallel()

	db := testdb.NewMigratedSQLite(t, "orphanscan-schedule-gate")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	for range 3 {
		inst, err := instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{InstanceID: inst.ID, Enabled: true, IgnorePaths: []string{}, ScanIntervalHours: 24})
		require.NoError(t, err)
	}

	cfg := DefaultConfig()
	cfg.MaxJitter = time.Nanosecond
	svc := NewService(cfg, nil, store, nil, nil, nil)
	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{
			{ID: 1, IsActive: true, HasLocalFilesystemAccess: true},
			remoteInstance(2, "box.example", 22),
			{ID: 3, IsActive: true},
		}, nil
	}
	scheduled := make(chan int, 3)
	stubSync(svc).getClient = func(_ context.Context, instanceID int) (healthChecker, error) {
		scheduled <- instanceID
		return nil, errNotStubbed
	}

	svc.checkScheduledScans(t.Context())

	got := map[int]bool{}
	for range 2 {
		select {
		case id := <-scheduled:
			got[id] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("scheduled instances = %v, want 1 and 2", got)
		}
	}
	require.Equal(t, map[int]bool{1: true, 2: true}, got)
	select {
	case id := <-scheduled:
		t.Fatalf("instance %d without filesystem access was scheduled", id)
	case <-time.After(200 * time.Millisecond):
	}
}

// An SSH preview never completes, and the next scan replaces it, so pacing by
// completed runs alone scheduled a remote instance on every tick.
func TestCheckScheduledScans_RemotePreviewWaitsForTheInterval(t *testing.T) {
	t.Parallel()

	db := testdb.NewMigratedSQLite(t, "orphanscan-schedule-preview")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	store := models.NewOrphanScanStore(db)
	for range 2 {
		inst, err := instanceStore.Create(t.Context(), "test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		_, err = store.UpsertSettings(t.Context(), &models.OrphanScanSettings{InstanceID: inst.ID, Enabled: true, IgnorePaths: []string{}, ScanIntervalHours: 24})
		require.NoError(t, err)
	}
	insertRun := func(instanceID int, status, age string) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(t.Context(), `
			INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, files_found, filesystem_mode, started_at)
			VALUES (?, ?, 'scheduled', 3, 'remote', datetime('now', ?)) RETURNING id`,
			instanceID, status, age,
		).Scan(&id))
		return id
	}
	preview := insertRun(1, "preview_ready", "-1 hours")
	// A preview a later scan replaced, and that scan failed. The failure retries as it always has.
	insertRun(2, "canceled", "-1 hours")
	_, err = db.ExecContext(t.Context(),
		`INSERT INTO orphan_scan_runs (instance_id, status, triggered_by, filesystem_mode, completed_at) VALUES (2, 'failed', 'scheduled', 'remote', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.MaxJitter = time.Nanosecond
	svc := NewService(cfg, nil, store, nil, nil, nil)
	stubSync(svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{remoteInstance(1, "box.example", 22), remoteInstance(2, "box.example", 22)}, nil
	}
	scheduled := make(chan int, 4)
	stubSync(svc).getClient = func(_ context.Context, instanceID int) (healthChecker, error) {
		scheduled <- instanceID
		return nil, errNotStubbed
	}
	next := func() int {
		select {
		case id := <-scheduled:
			return id
		case <-time.After(5 * time.Second):
			t.Fatal("no instance was scheduled")
			return 0
		}
	}
	requireNoneScheduled := func() {
		select {
		case id := <-scheduled:
			t.Fatalf("instance %d was scheduled", id)
		case <-time.After(200 * time.Millisecond):
		}
	}

	svc.checkScheduledScans(t.Context())
	require.Equal(t, 2, next(), "the instance whose last scan failed is retried")
	requireNoneScheduled()

	_, err = db.ExecContext(t.Context(), `UPDATE orphan_scan_runs SET started_at = datetime('now', '-25 hours') WHERE id = ?`, preview)
	require.NoError(t, err)
	svc.checkScheduledScans(t.Context())
	got := map[int]bool{next(): true, next(): true}
	require.Equal(t, map[int]bool{1: true, 2: true}, got, "a preview older than the interval is due")
}

// A confirm can fail the run after another one already queued its deletion.
func TestExecuteDeletion_RunNoLongerReadyDeletesNothing(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-delete-not-ready", models.FilesystemModeLocal, nil)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	failed, err := f.store.FailPreviewReadyRun(t.Context(), run.ID, FilesystemModeChangedMessage)
	require.NoError(t, err)
	require.True(t, failed)

	f.svc.executeDeletion(t.Context(), 1, run.ID)

	require.Equal(t, "failed", f.run(t, run.ID).Status)
	f.requireNothingDeleted(t)
}

// The run can leave preview_ready between ConfirmDeletion's status check and its refusal.
func TestConfirmDeletion_ModeChangedRefusalLeavesAMovedOnRunAlone(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-refusal-race", models.FilesystemModeLocal, nil)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	f.instance.set(models.FilesystemModeRemote)
	stubSync(f.svc).listInstances = func(ctx context.Context) ([]*models.Instance, error) {
		require.NoError(t, f.store.UpdateRunStatus(ctx, run.ID, "deleting"))
		inst, _ := f.instance.Get(ctx, 1)
		return []*models.Instance{inst}, nil
	}

	err := f.svc.ConfirmDeletion(t.Context(), 1, run.ID)
	require.ErrorIs(t, err, ErrInvalidRunStatus)
	require.NotErrorIs(t, err, ErrFilesystemModeChanged)
	require.Equal(t, "deleting", f.run(t, run.ID).Status)
}

func TestMaybeAutoCleanup_ModeChangedNotifies(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-autoclean-mode-changed", models.FilesystemModeRemote, nil)
	run := f.scan(t, "scheduled")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	settings, err := f.store.GetSettings(t.Context(), 1)
	require.NoError(t, err)

	f.instance.set(models.FilesystemModeLocal)
	f.svc.maybeAutoCleanup(t.Context(), 1, run.ID, settings, 1)

	require.Equal(t, "failed", f.run(t, run.ID).Status)
	f.requireNothingDeleted(t)
	select {
	case event := <-f.events:
		require.Equal(t, notifications.EventOrphanScanFailed, event.Type)
		require.Equal(t, FilesystemModeChangedMessage, event.ErrorMessage)
	default:
		t.Fatal("a scheduled run failed by a mode change must notify")
	}
}

// A concurrent trigger can cancel the preview between the status check and the
// refusal. Nothing failed then, so nothing is announced.
func TestMaybeAutoCleanup_ModeChangedOnACanceledRunDoesNotNotify(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-autoclean-canceled", models.FilesystemModeRemote, nil)
	run := f.scan(t, "scheduled")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	settings, err := f.store.GetSettings(t.Context(), 1)
	require.NoError(t, err)

	f.instance.set(models.FilesystemModeLocal)
	stubSync(f.svc).listInstances = func(ctx context.Context) ([]*models.Instance, error) {
		require.NoError(t, f.store.UpdateRunStatus(ctx, run.ID, "canceled"))
		inst, _ := f.instance.Get(ctx, 1)
		return []*models.Instance{inst}, nil
	}
	f.svc.maybeAutoCleanup(t.Context(), 1, run.ID, settings, 1)

	require.Equal(t, "canceled", f.run(t, run.ID).Status)
	f.requireNothingDeleted(t)
	f.requireNoFailureEvent(t)
}

// An SSH preview can never be confirmed, so it must not strand later scans.
func TestTriggerScan_ReplacesARemotePreview(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		triggeredBy string
		modeNow     models.FilesystemMode
	}{
		{"manual", "manual", models.FilesystemModeRemote},
		{"scheduled", "scheduled", models.FilesystemModeRemote},
		{"mode changed since", "manual", models.FilesystemModeLocal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newModeFixture(t, "orphanscan-replace-remote", models.FilesystemModeRemote, nil)
			old := f.scan(t, "manual")
			require.Equal(t, "preview_ready", old.Status, "run error: %s", old.ErrorMessage)
			require.Positive(t, old.FilesFound)

			f.instance.set(tc.modeNow)
			runID, err := f.svc.TriggerScan(t.Context(), 1, tc.triggeredBy)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				status := f.run(t, runID).Status
				return status != "pending" && status != "scanning"
			}, 10*time.Second, 10*time.Millisecond)

			require.Equal(t, "canceled", f.run(t, old.ID).Status)
			replacement := f.run(t, runID)
			require.Equal(t, "preview_ready", replacement.Status, "run error: %s", replacement.ErrorMessage)
			require.Equal(t, tc.modeNow, replacement.FilesystemMode)
			f.requireNothingDeleted(t)
		})
	}
}

func TestTriggerScan_LocalPreviewStillBlocks(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-local-preview-blocks", models.FilesystemModeLocal, nil)
	old := f.scan(t, "manual")
	require.Equal(t, "preview_ready", old.Status, "run error: %s", old.ErrorMessage)
	require.Positive(t, old.FilesFound)

	_, err := f.svc.TriggerScan(t.Context(), 1, "manual")
	require.ErrorIs(t, err, ErrScanInProgress)
	require.Equal(t, "preview_ready", f.run(t, old.ID).Status)
	runs, err := f.store.ListRuns(t.Context(), 1, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
}

// Without Identity the walker cannot tell a second path to a seeded file from an orphan.
func TestCanDeleteOrphans(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		caps models.FilesystemCapabilities
		want bool
	}{
		{"read only", models.FilesystemCapabilities{Read: true}, false},
		{"write without identity", models.FilesystemCapabilities{Read: true, Write: true}, false},
		{"identity without write", models.FilesystemCapabilities{Read: true, Identity: true}, false},
		{"write and identity", models.FilesystemCapabilities{Read: true, Write: true, Identity: true}, true},
		{"local", models.FilesystemCapabilitiesOf(&models.Instance{HasLocalFilesystemAccess: true}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, canDeleteOrphans(tc.caps))
		})
	}
}

// The deletion asks the pool for Write, so an instance that lost it after the
// confirm is refused before any backend is built for it.
func TestExecuteDeletion_NeverBuildsABackendWithoutWrite(t *testing.T) {
	t.Parallel()

	var built atomic.Int32
	var f *modeFixture
	f = newModeFixture(t, "orphanscan-delete-require-write", models.FilesystemModeLocal, func(*models.Instance) fsops.Backend {
		built.Add(1)
		return countingBackend{Backend: newTestBackend(), removes: f.removes}
	})
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)

	f.instance.set(models.FilesystemModeRemote)
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	require.Equal(t, "failed", f.run(t, run.ID).Status)
	require.Zero(t, built.Load(), "no remote backend may be built for a delete")
	f.requireNothingDeleted(t)
}

// The run's label is the mode of the row its backend was built from, not of a
// second read that can disagree with it.
func TestExecuteScan_RecordsTheModeOfTheRowItWalks(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-mode-one-read", models.FilesystemModeRemote, nil)
	stubSync(f.svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{{ID: 1, Name: "test", IsActive: true, HasLocalFilesystemAccess: true}}, nil
	}

	run := f.scan(t, "manual")
	require.Equal(t, models.FilesystemModeRemote, run.FilesystemMode)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, ScanModeChangedMessage, run.ErrorMessage)
	f.requireNothingDeleted(t)
}

// A deletion that got past the confirm still checks the run's recorded mode
// against the row it deletes through.
func TestExecuteDeletion_RemoteRunOnALocalInstanceDeletesNothing(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-delete-recorded-mode", models.FilesystemModeRemote, nil)
	run := f.scan(t, "manual")
	require.Equal(t, "preview_ready", run.Status, "run error: %s", run.ErrorMessage)
	require.Equal(t, models.FilesystemModeRemote, run.FilesystemMode)

	f.instance.set(models.FilesystemModeLocal)
	f.svc.executeDeletion(t.Context(), 1, run.ID)

	failed := f.run(t, run.ID)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, FilesystemModeChangedMessage, failed.ErrorMessage)
	f.requireNothingDeleted(t)
}

// Peers are chosen by the endpoint of the row the walk's backend was built
// from, so an edit that lands during the walk cannot drop a peer on the host
// that was walked.
func TestExecuteScan_PairsPeersByTheRowItWalks(t *testing.T) {
	t.Parallel()

	f := newModeFixture(t, "orphanscan-peers-one-read", models.FilesystemModeRemote, nil)
	stubSync(f.svc).listInstances = func(_ context.Context) ([]*models.Instance, error) {
		return []*models.Instance{remoteInstance(1, "moved.example", 22), remoteInstance(2, "box.example", 22)}, nil
	}
	stubSync(f.svc).getClient = func(_ context.Context, instanceID int) (healthChecker, error) {
		if instanceID == 2 {
			return nil, errors.New("qBittorrent unreachable")
		}
		return stubHealthChecker{healthy: true, lastSync: time.Now().Add(-time.Minute)}, nil
	}

	run := f.scan(t, "manual")
	require.Equal(t, "failed", run.Status)
	require.Contains(t, run.ErrorMessage, "same SSH host")
	f.requireNothingDeleted(t)
}
