// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

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
		store:  store,
		local:  create("local", true),
		none:   create("none", false),
		sftp:   remote("sftp", models.SSHTierSFTP),
		ex:     remote("exec", models.SSHTierExec),
	}
}

func (f *dirScanRemoteFixture) send(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	f.router.ServeHTTP(resp, httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body)))
	return resp
}

func (f *dirScanRemoteFixture) directory(t *testing.T, path string, instance *models.Instance) *models.DirScanDirectory {
	t.Helper()
	dir, err := f.store.CreateDirectory(t.Context(), &models.DirScanDirectory{Path: path, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
	require.NoError(t, err)
	return dir
}

func TestDirScanTargetInstanceGate(t *testing.T) {
	f := newDirScanRemoteFixture(t)
	const noAccess = `{"error":"Target instance must have local filesystem access enabled"}`
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

	create := func(t *testing.T, path string, instance *models.Instance) *httptest.ResponseRecorder {
		t.Helper()
		return f.send(t, http.MethodPost, "/api/dir-scan/directories", fmt.Sprintf(`{"path":%q,"targetInstanceId":%d}`, path, instance.ID))
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
		{name: "an update that names neither reads nothing", want: http.StatusOK, run: func(t *testing.T) *httptest.ResponseRecorder {
			return patch(t, f.directory(t, "/data/other-field", f.ex), `{"category":"movies"}`)
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
