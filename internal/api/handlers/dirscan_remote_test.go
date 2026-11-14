// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// dirScanRemoteFixture has one instance of each filesystem mode behind a real handler and store.
type dirScanRemoteFixture struct {
	router                *chi.Mux
	db                    *database.DB
	store                 *models.DirScanStore
	local, none, sftp, ex *models.Instance
}

func newDirScanRemoteFixture(t *testing.T) *dirScanRemoteFixture {
	t.Helper()
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "dirscan-remote")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	create := func(name string, local bool) *models.Instance {
		instance, err := instances.Create(ctx, name, "http://localhost:8080", "", "", nil, nil, false, &local)
		require.NoError(t, err)
		return instance
	}
	remote := func(name string, tier models.SSHTier) *models.Instance {
		instance := create(name, false)
		require.NoError(t, instances.SetSSHCredentials(ctx, instance.ID, "box.example", 22, "qui", sshtest.PrivateKey("")))
		require.NoError(t, instances.SetHostKeyPin(ctx, instance.ID, "box.example", 22, sshtest.NewSigner().PublicKey().Marshal()))
		instance, err := instances.Get(ctx, instance.ID)
		require.NoError(t, err)
		require.NoError(t, instances.SetSSHProbe(ctx, instance, tier, false))
		instance, err = instances.Get(ctx, instance.ID)
		require.NoError(t, err)
		require.Equal(t, models.FilesystemModeRemote, models.FilesystemAccessMode(instance))
		return instance
	}

	store := models.NewDirScanStore(db)
	service := dirscan.NewService(dirscan.DefaultConfig(), store, nil, nil, nil, nil, nil, nil, fsops.NewPool(instances, localbackend.NewBackend()), nil)
	handler := NewDirScanHandler(service, instances)
	router := chi.NewRouter()
	router.Post("/api/dir-scan/directories", handler.CreateDirectory)
	router.Patch("/api/dir-scan/directories/{directoryID}", handler.UpdateDirectory)

	return &dirScanRemoteFixture{
		router: router,
		db:     db,
		store:  store,
		local:  create("local", true),
		none:   create("none", false),
		sftp:   remote("sftp", models.SSHTierSFTP),
		ex:     remote("exec", models.SSHTierExec),
	}
}

func (f *dirScanRemoteFixture) send(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	f.router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body)))
	return resp
}

func (f *dirScanRemoteFixture) directory(t *testing.T, dirPath string, instance *models.Instance) *models.DirScanDirectory {
	t.Helper()
	dir, err := f.store.CreateDirectory(t.Context(), &models.DirScanDirectory{Path: dirPath, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
	require.NoError(t, err)
	return dir
}

func TestDirScanTargetInstanceGate(t *testing.T) {
	f := newDirScanRemoteFixture(t)
	const noAccess = `{"error":"Target instance needs filesystem access. Enable Local Filesystem Access, or set up SSH access, in instance settings first."}`
	for i, tt := range []struct {
		name     string
		instance *models.Instance
		wantErr  string
	}{
		{name: "local", instance: f.local},
		{name: "remote sftp tier", instance: f.sftp},
		{name: "remote exec tier", instance: f.ex},
		{name: "no filesystem access keeps its text", instance: f.none, wantErr: noAccess},
	} {
		t.Run(tt.name, func(t *testing.T) {
			created := f.send(t, http.MethodPost, "/api/dir-scan/directories",
				fmt.Sprintf(`{"path":"/data/create-%d","targetInstanceId":%d}`, i, tt.instance.ID))
			moved := f.directory(t, fmt.Sprintf("/data/patch-%d", i), f.local)
			patched := f.send(t, http.MethodPatch, "/api/dir-scan/directories/"+strconv.Itoa(moved.ID),
				fmt.Sprintf(`{"targetInstanceId":%d}`, tt.instance.ID))

			if tt.wantErr != "" {
				require.Equal(t, http.StatusBadRequest, created.Code, created.Body.String())
				require.JSONEq(t, tt.wantErr, created.Body.String())
				require.Equal(t, http.StatusBadRequest, patched.Code, patched.Body.String())
				require.JSONEq(t, tt.wantErr, patched.Body.String())
				return
			}
			require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
			require.Equal(t, http.StatusOK, patched.Code, patched.Body.String())
		})
	}
}

func TestDirScanRemotePathMustBeAbsolute(t *testing.T) {
	f := newDirScanRemoteFixture(t)
	const notAbsolute = `{"error":"Path must be absolute on a remote instance"}`

	create := func(t *testing.T, dirPath string, instance *models.Instance) *httptest.ResponseRecorder {
		t.Helper()
		return f.send(t, http.MethodPost, "/api/dir-scan/directories", fmt.Sprintf(`{"path":%q,"targetInstanceId":%d}`, dirPath, instance.ID))
	}
	patch := func(t *testing.T, dir *models.DirScanDirectory, body string) *httptest.ResponseRecorder {
		t.Helper()
		return f.send(t, http.MethodPatch, "/api/dir-scan/directories/"+strconv.Itoa(dir.ID), body)
	}

	for _, tt := range []struct {
		name string
		run  func(t *testing.T) *httptest.ResponseRecorder
		want int
	}{
		{name: "create, relative on remote", want: http.StatusBadRequest, run: func(t *testing.T) *httptest.ResponseRecorder {
			return create(t, "data/relative", f.ex)
		}},
		{name: "create, a Windows path is relative on a remote instance", want: http.StatusBadRequest, run: func(t *testing.T) *httptest.ResponseRecorder {
			return create(t, `D:\data`, f.sftp)
		}},
		{name: "create, absolute on remote", want: http.StatusCreated, run: func(t *testing.T) *httptest.ResponseRecorder {
			return create(t, "/data/absolute", f.ex)
		}},
		{name: "create, local paths keep their grammar", want: http.StatusCreated, run: func(t *testing.T) *httptest.ResponseRecorder {
			return create(t, "data/relative-local", f.local)
		}},
		{name: "path only update reads the directory's target", want: http.StatusBadRequest, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "/data/path-only", f.ex), `{"path":"data/relative"}`)
		}},
		{name: "path only update to an absolute path", want: http.StatusOK, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "/data/path-only-ok", f.ex), `{"path":"/data/other"}`)
		}},
		{name: "path only update on a local target", want: http.StatusOK, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "/data/path-only-local", f.local), `{"path":"data/local-relative"}`)
		}},
		{name: "moving a relative path to a remote target", want: http.StatusBadRequest, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "data/move-me", f.local), fmt.Sprintf(`{"targetInstanceId":%d}`, f.ex.ID))
		}},
		{name: "moving to a remote target with a new absolute path", want: http.StatusOK, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "data/move-me-too", f.local), fmt.Sprintf(`{"targetInstanceId":%d,"path":"/data/moved"}`, f.ex.ID))
		}},
		{name: "an update that names neither skips the path check", want: http.StatusOK, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "data/other-field", f.ex), `{"category":"movies"}`)
		}},
		{name: "path only update of a missing directory", want: http.StatusNotFound, run: func(t *testing.T) *httptest.ResponseRecorder {
			return f.send(t, http.MethodPatch, "/api/dir-scan/directories/999999", `{"path":"/data/other"}`)
		}},
		{name: "path only update with a target that can't be read", want: http.StatusInternalServerError, run: func(t *testing.T) *httptest.ResponseRecorder {
			broken := newDirScanRemoteFixture(t)
			dir := broken.directory(t, "/data/unreadable-target", broken.ex)
			_, err := broken.db.ExecContext(t.Context(), "DROP VIEW instances_view")
			require.NoError(t, err)
			return broken.send(t, http.MethodPatch, "/api/dir-scan/directories/"+strconv.Itoa(dir.ID), `{"path":"/data/other"}`)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.run(t)
			require.Equal(t, tt.want, resp.Code, resp.Body.String())
			if tt.want == http.StatusBadRequest {
				require.JSONEq(t, notAbsolute, resp.Body.String())
			}
		})
	}
}

// webhookHost is an instance's own backend: slash paths, and files by path. Any other read is an empty walk's
// business, since the run a webhook starts is not what these tests look at.
type webhookHost struct {
	fsops.Backend
	files  map[string]bool
	mu     sync.Mutex
	stated []string
}

func (h *webhookHost) Paths() fsops.PathDialect { return fsops.SlashPaths }

func (h *webhookHost) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stated = append(h.stated, p)
	isDir, ok := h.files[p]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}
	return &fsops.LstatInfo{Path: p, IsDir: isDir}, nil
}

func (h *webhookHost) WalkDir(_ context.Context, root string, _ fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, 1)
	ch <- fsops.WalkEntry{Path: root, RelPath: ".", Mode: fs.ModeDir, IsDir: true}
	close(ch)
	return ch, nil
}

// webhookDir is a directory on the remote instance, or with local set, on a local one.
type webhookDir struct {
	path  string
	local bool
}

// webhookRun posts a webhook payload at a remote directory and waits for the scan it starts.
func webhookRun(t *testing.T, host *webhookHost, dirPath string, payload any) (*httptest.ResponseRecorder, *dirScanTriggerResponse) {
	t.Helper()
	return webhookRunIn(t, host, []webhookDir{{path: dirPath}}, payload)
}

// webhookRunIn is webhookRun with the directories created in the given order.
func webhookRunIn(t *testing.T, host *webhookHost, dirs []webhookDir, payload any) (*httptest.ResponseRecorder, *dirScanTriggerResponse) {
	t.Helper()
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "dirscan-remote-webhook")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "remote", "http://localhost:8080", "", "", nil, nil, false, new(false))
	require.NoError(t, err)
	require.NoError(t, instances.SetSSHCredentials(ctx, instance.ID, "box.example", 22, "qui", sshtest.PrivateKey("")))
	require.NoError(t, instances.SetHostKeyPin(ctx, instance.ID, "box.example", 22, sshtest.NewSigner().PublicKey().Marshal()))
	localInstance, err := instances.Create(ctx, "local", "http://localhost:8080", "", "", nil, nil, false, new(true))
	require.NoError(t, err)

	store := models.NewDirScanStore(db)
	pool := fsops.NewPoolWithRemote(instances, localbackend.NewBackend(), func(*models.Instance) fsops.Backend { return host })
	service := dirscan.NewService(dirscan.DefaultConfig(), store, nil, nil, nil, nil, nil, nil, pool, nil)
	hub, events := newActivityHub(t)
	service.SetActivityPublisher(hub)
	handler := NewDirScanHandler(service, instances)
	for _, dir := range dirs {
		target := instance.ID
		if dir.local {
			target = localInstance.ID
		}
		_, err := service.CreateDirectory(ctx, &models.DirScanDirectory{Path: dir.path, Enabled: true, TargetInstanceID: target, ScanIntervalMinutes: 60})
		require.NoError(t, err)
	}

	rec := httptest.NewRecorder()
	handler.WebhookTriggerScan(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/dir-scan/webhook/scan", dirScanWebhookJSONBody(t, payload)))
	if rec.Code != http.StatusAccepted {
		return rec, nil
	}
	var resp dirScanTriggerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	waitDirScanIdle(t, service, events, resp.DirectoryID)
	return rec, &resp
}

func TestWebhookTriggerScan_RemoteFilePathScansParent(t *testing.T) {
	const dirPath = "/data/lib"
	files := map[string]bool{
		"/data/lib/Show":          true,
		"/data/lib/Show/ep01.mkv": false,
	}
	for _, tt := range []struct {
		name    string
		payload any
		want    string
	}{
		{name: "a file scans its folder", payload: webhookPayloadSimple{Path: "/data/lib/Show/ep01.mkv"}, want: "/data/lib/Show"},
		{name: "a folder scans itself", payload: webhookPayloadSimple{Path: "/data/lib/Show"}, want: "/data/lib/Show"},
		{name: "a path the host can't stat scans as given", payload: webhookPayloadSimple{Path: "/data/lib/Gone/ep01.mkv"}, want: "/data/lib/Gone/ep01.mkv"},
		{name: "an arr payload", payload: webhookPayloadArr{Series: webhookPayloadSeries{Path: "/data/lib/Show/"}}, want: "/data/lib/Show"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec, resp := webhookRun(t, &webhookHost{files: files}, dirPath, tt.payload)
			require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
			require.Equal(t, tt.want, resp.ScanRoot)
			require.Equal(t, dirPath, resp.DirectoryPath)
		})
	}
}

// A path on the qui host says nothing about the instance's host, whichever way the two disagree.
func TestWebhookTriggerScan_RemotePathIgnoresQuiHost(t *testing.T) {
	onQuiHost := filepath.ToSlash(filepath.Join(t.TempDir(), "same-name.mkv"))
	require.NoError(t, os.WriteFile(filepath.FromSlash(onQuiHost), []byte("x"), 0o600))
	// Only a slash directory reaches the slash grammar, and a Windows temp dir does not start with one.
	if !strings.HasPrefix(onQuiHost, "/") {
		t.Skip("the qui host's temp dir has no slash path on this host")
	}
	parent := path.Dir(onQuiHost)

	host := &webhookHost{files: map[string]bool{onQuiHost: true}}
	rec, resp := webhookRun(t, host, parent, webhookPayloadSimple{Path: onQuiHost})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Equal(t, onQuiHost, resp.ScanRoot, "a folder on the instance's host, though a file here")
	require.Contains(t, host.stated, onQuiHost)
}

func TestWebhookRoutingKeepsSlashPaths(t *testing.T) {
	for _, tt := range []struct {
		name     string
		dir      string
		path     string
		wantCode int
		wantRoot string
	}{
		{name: "a child keeps its slashes", dir: "/data/lib", path: "/data/lib/Show/Season 01", wantCode: http.StatusAccepted, wantRoot: "/data/lib/Show/Season 01"},
		{name: "a backslash in a name stays", dir: "/data/lib", path: `/data/lib/Show\Odd`, wantCode: http.StatusAccepted, wantRoot: `/data/lib/Show\Odd`},
		{name: "a sibling does not route", dir: "/data/lib", path: "/data/library/Show", wantCode: http.StatusNotFound},
		{name: "a dot dot does not route", dir: "/data/lib", path: "/data/lib/../other", wantCode: http.StatusNotFound},
		{name: "a Windows path does not route", dir: "/data/lib", path: `D:\data\lib\Show`, wantCode: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec, resp := webhookRun(t, &webhookHost{files: map[string]bool{tt.path: true}}, tt.dir, webhookPayloadSimple{Path: tt.path})
			require.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			if tt.wantCode == http.StatusAccepted {
				require.Equal(t, tt.wantRoot, resp.ScanRoot)
			}
		})
	}
}

// A shorter local match on a Windows qui host cleans the path to backslashes, and the scan root must stay
// the one the remote directory read. Linux reads both grammars the same, so only Windows can tell.
func TestWebhookRoutingTakesThePathFromTheBestMatch(t *testing.T) {
	local, remote := webhookDir{path: "/lib", local: true}, webhookDir{path: "/lib/sub"}
	for _, tt := range []struct {
		name string
		dirs []webhookDir
	}{
		{name: "the shorter local match listed first", dirs: []webhookDir{local, remote}},
		{name: "the shorter local match listed last", dirs: []webhookDir{remote, local}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			host := &webhookHost{files: map[string]bool{"/lib/sub/x": true}}
			rec, resp := webhookRunIn(t, host, tt.dirs, webhookPayloadSimple{Path: "/lib/sub/x"})
			require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
			require.Equal(t, remote.path, resp.DirectoryPath)
			require.Equal(t, "/lib/sub/x", resp.ScanRoot)
		})
	}
}
