// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-bdinfo/pkg/bdinfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/activity"
	"github.com/autobrr/qui/internal/services/discscan"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const discScanHash = "0123456789abcdef0123456789abcdef01234567"

type fakeDiscResolver struct {
	files    qbt.TorrentFiles
	savePath string
}

func (f *fakeDiscResolver) GetTorrentFiles(context.Context, int, string) (*qbt.TorrentFiles, error) {
	return &f.files, nil
}

func (f *fakeDiscResolver) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return &qbt.TorrentProperties{SavePath: f.savePath}, nil
}

func (f *fakeDiscResolver) GetTorrents(context.Context, int, qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	return nil, nil
}

// fakeScanner blocks each scan until release is closed, so tests observe the
// queue. It reports one progress sample before blocking.
type fakeScanner struct {
	started chan string
	release chan struct{}
}

func (f *fakeScanner) scan(ctx context.Context, path string, onProgress func(processed, total int64)) (bdinfo.Result, error) {
	onProgress(50, 100)
	f.started <- path
	select {
	case <-ctx.Done():
		return bdinfo.Result{}, ctx.Err()
	case <-f.release:
		return bdinfo.Result{Report: "report " + path, QuickSummary: "quick", ForumsBlock: "forum"}, nil
	}
}

type discScanFixture struct {
	router     http.Handler
	store      *models.DiscScanStore
	service    *discscan.Service
	instanceID int
	root       string
	scanner    *fakeScanner
	events     <-chan activity.Event
}

func newDiscScanFixture(t *testing.T, hasLocalAccess bool) *discScanFixture {
	t.Helper()
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "disc-scan-handler")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://localhost:8080", "user", "pass", nil, nil, false, &hasLocalAccess)
	require.NoError(t, err)

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Box Set", "Disc 1", "BDMV", "STREAM"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Box Set", "Disc 2", "BDMV", "STREAM"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Box Set", "Extras"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Box Set", "Disc 1", "BDMV", "index.bdmv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Box Set", "Disc 2", "BDMV", "index.bdmv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Box Set", "Extras", "featurette.mkv"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Box Set", "bonus.iso"), []byte("x"), 0o600))

	resolver := &fakeDiscResolver{
		savePath: root,
		files: qbt.TorrentFiles{
			{Index: 0, Name: "Box Set/Disc 1/BDMV/index.bdmv"},
			{Index: 1, Name: "Box Set/Disc 1/BDMV/STREAM/00001.m2ts"},
			{Index: 2, Name: "Box Set/Disc 2/BDMV/index.bdmv"},
			{Index: 3, Name: "Box Set/Extras/featurette.mkv"},
			{Index: 4, Name: "Box Set/bonus.iso"},
		},
	}

	hub := activity.NewHub()
	t.Cleanup(hub.Close)
	events, unsubscribe := hub.Subscribe()
	t.Cleanup(unsubscribe)

	store := models.NewDiscScanStore(db)
	service := discscan.NewService(store)
	service.SetActivityPublisher(hub)
	scanner := &fakeScanner{started: make(chan string, 8), release: make(chan struct{})}
	service.SetScanner(scanner.scan)

	handler := NewDiscScanHandler(service, store, resolver, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	router := chi.NewRouter()
	router.Route("/api/instances/{instanceID}", func(r chi.Router) {
		r.Get("/torrents/{hash}/disc-scans", handler.ListForTorrent)
		r.Post("/torrents/{hash}/disc-scans", handler.Start)
		r.Get("/disc-scans/{runID}", handler.Get)
		r.Post("/disc-scans/{runID}/cancel", handler.Cancel)
	})

	return &discScanFixture{router: router, store: store, service: service, instanceID: instance.ID, root: root, scanner: scanner, events: events}
}

func (fx *discScanFixture) startWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	fx.service.Start(ctx)
}

func (fx *discScanFixture) do(t *testing.T, method, target, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/instances/"+strconv.Itoa(fx.instanceID)+target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (fx *discScanFixture) start(t *testing.T, discPath string, force bool) (int, models.DiscScanRun) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"discPath": discPath, "force": force})
	require.NoError(t, err)
	code, raw := fx.do(t, http.MethodPost, "/torrents/"+discScanHash+"/disc-scans", string(body))
	var run models.DiscScanRun
	if code == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &run))
	}
	return code, run
}

func (fx *discScanFixture) get(t *testing.T, runID int64) models.DiscScanRun {
	t.Helper()
	code, raw := fx.do(t, http.MethodGet, "/disc-scans/"+strconv.FormatInt(runID, 10), "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var run models.DiscScanRun
	require.NoError(t, json.Unmarshal(raw, &run))
	return run
}

func (fx *discScanFixture) waitStatus(t *testing.T, runID int64, status string) models.DiscScanRun {
	t.Helper()
	var run models.DiscScanRun
	require.Eventually(t, func() bool {
		run = fx.get(t, runID)
		return run.Status == status
	}, 5*time.Second, 10*time.Millisecond, "run %d never reached %s", runID, status)
	return run
}

func TestDiscScanStart_DeniedWithoutFilesystemAccess(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, false)

	code, _ := fx.start(t, "Box Set/Disc 1", false)
	require.Equal(t, http.StatusForbidden, code)
}

func TestDiscScanStart_RejectsNonDiscPaths(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)

	for _, discPath := range []string{"", "Box Set/Extras", "Box Set", "Box Set/Disc 1/BDMV", "Box Set/Extras/featurette.mkv", "../Box Set/Disc 1", "/etc", `C:\Box Set\Disc 1`, `\\server\share\Disc 1`, "..\\Box Set\\Disc 1"} {
		code, _ := fx.start(t, discPath, false)
		require.Equalf(t, http.StatusBadRequest, code, "disc path %q", discPath)
	}
}

func TestDiscScanStart_QueuesPendingRowAndFiresEvent(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)

	code, run := fx.start(t, "Box Set/Disc 1", false)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, models.DiscScanStatusPending, run.Status)
	require.Equal(t, "Box Set/Disc 1", run.DiscPath)
	require.Equal(t, filepath.Join(fx.root, "Box Set", "Disc 1"), run.ResolvedPath)
	require.Equal(t, 1, run.QueuePosition)
	select {
	case ev := <-fx.events:
		require.Equal(t, activity.KindDiscScanRun, ev.Kind)
		require.Equal(t, fx.instanceID, ev.InstanceID)
		require.Equal(t, strconv.FormatInt(run.ID, 10), ev.ResourceID)
	case <-time.After(2 * time.Second):
		t.Fatalf("no discscan.run event for run %d", run.ID)
	}

	code, iso := fx.start(t, "Box Set/bonus.iso", false)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, filepath.Join(fx.root, "Box Set", "bonus.iso"), iso.ResolvedPath)
	require.Equal(t, 2, iso.QueuePosition)
}

func TestDiscScanStart_ReturnsCachedRowUnlessForced(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)
	fx.startWorker(t)

	_, first := fx.start(t, "Box Set/Disc 1", false)
	<-fx.scanner.started
	close(fx.scanner.release)
	done := fx.waitStatus(t, first.ID, models.DiscScanStatusCompleted)
	require.Equal(t, "quick", done.QuickSummary)
	require.Equal(t, "forum", done.ForumsBlock)
	require.Equal(t, "report "+done.ResolvedPath, done.Report)

	_, cached := fx.start(t, "Box Set/Disc 1", false)
	require.Equal(t, first.ID, cached.ID)

	_, rescan := fx.start(t, "Box Set/Disc 1", true)
	require.NotEqual(t, first.ID, rescan.ID)
	fx.waitStatus(t, rescan.ID, models.DiscScanStatusCompleted)
}

func TestDiscScanWorker_RunsInOrderAndReportsProgress(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)

	_, first := fx.start(t, "Box Set/Disc 1", false)
	_, second := fx.start(t, "Box Set/Disc 2", false)
	fx.startWorker(t)

	require.Equal(t, filepath.Join(fx.root, "Box Set", "Disc 1"), <-fx.scanner.started)
	scanning := fx.waitStatus(t, first.ID, models.DiscScanStatusScanning)
	require.Equal(t, int64(50), scanning.ProcessedBytes)
	require.Equal(t, int64(100), scanning.TotalBytes)
	require.Equal(t, models.DiscScanStatusPending, fx.get(t, second.ID).Status)
	require.Equal(t, 1, fx.get(t, second.ID).QueuePosition)

	close(fx.scanner.release)
	fx.waitStatus(t, first.ID, models.DiscScanStatusCompleted)
	require.Equal(t, filepath.Join(fx.root, "Box Set", "Disc 2"), <-fx.scanner.started)
	fx.waitStatus(t, second.ID, models.DiscScanStatusCompleted)
}

func TestDiscScanCancel_PendingAndScanning(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)

	_, pending := fx.start(t, "Box Set/Disc 1", false)
	code, _ := fx.do(t, http.MethodPost, "/disc-scans/"+strconv.FormatInt(pending.ID, 10)+"/cancel", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, models.DiscScanStatusCanceled, fx.get(t, pending.ID).Status)

	code, _ = fx.do(t, http.MethodPost, "/disc-scans/"+strconv.FormatInt(pending.ID, 10)+"/cancel", "")
	require.Equal(t, http.StatusConflict, code)

	fx.startWorker(t)
	_, running := fx.start(t, "Box Set/Disc 2", false)
	<-fx.scanner.started
	code, _ = fx.do(t, http.MethodPost, "/disc-scans/"+strconv.FormatInt(running.ID, 10)+"/cancel", "")
	require.Equal(t, http.StatusOK, code)
	canceled := fx.waitStatus(t, running.ID, models.DiscScanStatusCanceled)
	require.Empty(t, canceled.Report)

	// The scanner returned on ctx cancel; a later release changes nothing.
	close(fx.scanner.release)
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, models.DiscScanStatusCanceled, fx.get(t, running.ID).Status)

	code, _ = fx.do(t, http.MethodPost, "/disc-scans/999999/cancel", "")
	require.Equal(t, http.StatusNotFound, code)
}

func TestDiscScanStart_MarksInterruptedScansFailed(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)

	_, interrupted := fx.start(t, "Box Set/Disc 1", false)
	_, queued := fx.start(t, "Box Set/Disc 2", false)
	started, err := fx.store.MarkScanning(t.Context(), interrupted.ID)
	require.NoError(t, err)
	require.True(t, started)

	fx.startWorker(t)
	failed := fx.waitStatus(t, interrupted.ID, models.DiscScanStatusFailed)
	require.Equal(t, "interrupted by qui restart", failed.ErrorMessage)
	require.Equal(t, filepath.Join(fx.root, "Box Set", "Disc 2"), <-fx.scanner.started)
	fx.waitStatus(t, queued.ID, models.DiscScanStatusScanning)
}

func TestDiscScanList_NewestRowPerDiscPath(t *testing.T) {
	t.Parallel()
	fx := newDiscScanFixture(t, true)

	_, disc1 := fx.start(t, "Box Set/Disc 1", false)
	_, disc2 := fx.start(t, "Box Set/Disc 2", false)
	_, err := fx.store.MarkCanceled(t.Context(), disc1.ID)
	require.NoError(t, err)
	_, disc1Again := fx.start(t, "Box Set/Disc 1", false)
	require.NotEqual(t, disc1.ID, disc1Again.ID)

	code, raw := fx.do(t, http.MethodGet, "/torrents/"+discScanHash+"/disc-scans", "")
	require.Equal(t, http.StatusOK, code)
	var runs []models.DiscScanRun
	require.NoError(t, json.Unmarshal(raw, &runs))
	require.Len(t, runs, 2)
	require.Equal(t, disc1Again.ID, runs[0].ID)
	require.Equal(t, disc2.ID, runs[1].ID)
}
