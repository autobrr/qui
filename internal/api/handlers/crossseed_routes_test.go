// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type routeFixture struct {
	t         *testing.T
	instances *models.InstanceStore
	routes    *crossseedroute.Service
	router    *chi.Mux
	// qbt answers as qBittorrent 5.0 (WebAPI 2.11.2); old answers as 4.6.
	qbt, old, offline string
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "cross-seed-routes")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := internalqbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	routes := crossseedroute.NewService(models.NewCrossSeedRouteStore(db), instances, clientPool, fsops.NewPool(instances, local.NewBackend()))
	handler := &CrossSeedHandler{instanceStore: instances, routes: routes}
	router := chi.NewRouter()
	router.Route("/api/cross-seed/routes", func(r chi.Router) {
		r.Get("/", handler.ListCrossSeedRoutes)
		r.Get("/{instanceID}", handler.GetCrossSeedRoute)
		r.Put("/{instanceID}", handler.SaveCrossSeedRoute)
		r.Delete("/{instanceID}", handler.ClearCrossSeedRoute)
	})

	return &routeFixture{
		t:         t,
		instances: instances,
		routes:    routes,
		router:    router,
		qbt:       fakeQBittorrent(t, "2.11.2"),
		old:       fakeQBittorrent(t, "2.11.1"),
		offline:   fakeQBittorrent(t, ""),
	}
}

// fakeQBittorrent answers login and the WebAPI version, or 503 for everything
// when version is empty.
func fakeQBittorrent(t *testing.T, version string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case version == "":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case r.URL.Path == "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case r.URL.Path == "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte(version))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *routeFixture) instance(name, host string, mode models.FilesystemMode) *models.Instance {
	f.t.Helper()
	ctx := f.t.Context()
	instance, err := f.instances.Create(ctx, name, host, "admin", "password", nil, nil, false, new(mode == models.FilesystemModeLocal))
	require.NoError(f.t, err)
	if mode == models.FilesystemModeRemote {
		require.NoError(f.t, f.instances.SetSSHCredentials(ctx, instance.ID, "box.example", 22, "qui", sshtest.PrivateKey("")))
		require.NoError(f.t, f.instances.SetHostKeyPin(ctx, instance.ID, "box.example", 22, sshtest.NewSigner().PublicKey().Marshal()))
	}
	instance, err = f.instances.Get(ctx, instance.ID)
	require.NoError(f.t, err)
	return instance
}

// source is a local instance in hardlink mode with the given base folders.
func (f *routeFixture) source(name string, mode models.FilesystemMode, baseDirs ...string) *models.Instance {
	f.t.Helper()
	instance := f.instance(name, f.qbt, mode)
	return f.update(instance, &models.InstanceUpdateParams{UseHardlinks: new(true), HardlinkBaseDir: new(strings.Join(baseDirs, ","))})
}

func (f *routeFixture) update(instance *models.Instance, params *models.InstanceUpdateParams) *models.Instance {
	f.t.Helper()
	updated, err := f.instances.Update(f.t.Context(), instance.ID, instance.Name, instance.Host, instance.Username, "", nil, nil, params)
	require.NoError(f.t, err)
	return updated
}

func (f *routeFixture) do(method string, instanceID int, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	path := "/api/cross-seed/routes/"
	if instanceID != 0 {
		path += strconv.Itoa(instanceID)
	}
	var payload strings.Builder
	if body != nil {
		require.NoError(f.t, json.NewEncoder(&payload).Encode(body))
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequestWithContext(f.t.Context(), method, path, strings.NewReader(payload.String())))
	return rec
}

func (f *routeFixture) save(source, crossSeed *models.Instance, rows ...models.CrossSeedRouteRow) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.do(http.MethodPut, source.ID, crossSeedRouteRequest{CrossSeedInstanceID: crossSeed.ID, Rows: rows})
}

func decodeRoute(t *testing.T, rec *httptest.ResponseRecorder) *crossSeedRouteResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var route *crossSeedRouteResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &route))
	return route
}

func TestCrossSeedRouteSaveRefusesBrokenRules(t *testing.T) {
	library := filepath.Join(t.TempDir(), "links")
	other := filepath.Join(t.TempDir(), "links")
	row := func(baseDir, mapped string) models.CrossSeedRouteRow {
		return models.CrossSeedRouteRow{BaseDir: baseDir, MappedPath: mapped}
	}

	cases := []struct {
		name string
		// setup returns the Source instance, the Cross-seed instance, and the rows to save.
		setup func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow)
		want  string
	}{
		{
			name: "source in regular mode",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.instance("library", f.qbt, models.FilesystemModeLocal), f.instance("seeds", f.qbt, models.FilesystemModeNone), nil
			},
			want: "library must use hardlink or reflink mode",
		},
		{
			name: "source without base folder",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, " , "), f.instance("seeds", f.qbt, models.FilesystemModeNone), nil
			},
			want: "at least one base folder",
		},
		{
			name: "source without filesystem access",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeNone, library), f.instance("seeds", f.qbt, models.FilesystemModeNone), nil
			},
			want: "library needs Local Filesystem Access",
		},
		{
			name: "remote source",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeRemote, "/data/links"), f.instance("seeds", f.qbt, models.FilesystemModeNone), nil
			},
			want: "library is a remote instance",
		},
		{
			name: "route to itself",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				source := f.source("library", models.FilesystemModeLocal, library)
				return source, source, nil
			},
			want: "library cannot be its own Cross-seed instance",
		},
		{
			name: "cross-seed instance has its own route",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				crossSeed := f.instance("seeds", f.qbt, models.FilesystemModeNone)
				third := f.instance("third", f.qbt, models.FilesystemModeNone)
				require.NoError(t, f.routes.Upsert(t.Context(), &models.CrossSeedRoute{SourceInstanceID: crossSeed.ID, CrossSeedInstanceID: third.ID}))
				return f.source("library", models.FilesystemModeLocal, library), crossSeed, nil
			},
			want: "seeds routes its own cross-seeds",
		},
		{
			name: "another instance routes to the source",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				source := f.source("library", models.FilesystemModeLocal, library)
				first := f.instance("first", f.qbt, models.FilesystemModeNone)
				require.NoError(t, f.routes.Upsert(t.Context(), &models.CrossSeedRoute{SourceInstanceID: first.ID, CrossSeedInstanceID: source.ID}))
				return source, f.instance("seeds", f.qbt, models.FilesystemModeNone), nil
			},
			want: "library is the Cross-seed instance of another instance",
		},
		{
			name: "remote cross-seed instance",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library), f.instance("seeds", f.qbt, models.FilesystemModeRemote), nil
			},
			want: "seeds is a remote instance",
		},
		{
			name: "offline cross-seed instance",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library), f.instance("seeds", f.offline, models.FilesystemModeNone), nil
			},
			want: "Cross-seed instance unavailable: seeds",
		},
		{
			name: "cross-seed instance turned off in qui",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				crossSeed := f.instance("seeds", f.qbt, models.FilesystemModeNone)
				_, err := f.instances.SetActiveState(t.Context(), crossSeed.ID, false)
				require.NoError(t, err)
				return f.source("library", models.FilesystemModeLocal, library), crossSeed, nil
			},
			want: "Cross-seed instance unavailable: seeds: qBittorrent instance is disabled",
		},
		{
			name: "cross-seed instance older than qBittorrent 5.0",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library), f.instance("seeds", f.old, models.FilesystemModeNone), nil
			},
			want: "seeds runs a qBittorrent version older than 5.0",
		},
		{
			name: "row for a folder that is not a base folder",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library), f.instance("seeds", f.qbt, models.FilesystemModeNone), []models.CrossSeedRouteRow{row(other, "/seeds/links")}
			},
			want: "is not a base folder of library",
		},
		{
			name: "relative mapped path",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library), f.instance("seeds", f.qbt, models.FilesystemModeNone), []models.CrossSeedRouteRow{row(library, "seeds/links")}
			},
			want: "is not an absolute path",
		},
		{
			name: "two rows map to one path",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library, other), f.instance("seeds", f.qbt, models.FilesystemModeNone),
					[]models.CrossSeedRouteRow{row(library, `D:\Seeds`), row(other, "d:/seeds/")}
			},
			want: "map to the same or nested folders",
		},
		{
			name: "two rows map to nested paths",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library, other), f.instance("seeds", f.qbt, models.FilesystemModeNone),
					[]models.CrossSeedRouteRow{row(library, "/seeds"), row(other, "/seeds/more")}
			},
			want: "map to the same or nested folders",
		},
		{
			name: "local cross-seed instance with a changed path",
			setup: func(f *routeFixture) (*models.Instance, *models.Instance, []models.CrossSeedRouteRow) {
				return f.source("library", models.FilesystemModeLocal, library), f.instance("seeds", f.qbt, models.FilesystemModeLocal), []models.CrossSeedRouteRow{row(library, "/seeds/links")}
			},
			want: "so " + library + " must keep the same path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRouteFixture(t)
			source, crossSeed, rows := tc.setup(f)

			rec := f.save(source, crossSeed, rows...)

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.want)
			route, err := f.routes.Get(t.Context(), source.ID)
			require.NoError(t, err)
			assert.Nil(t, route, "a refused route is not stored")
		})
	}
}

func TestCrossSeedRouteSaveReadClearList(t *testing.T) {
	f := newRouteFixture(t)
	library := filepath.Join(t.TempDir(), "links")
	other := filepath.Join(t.TempDir(), "links")
	// A base folder listed twice is one row.
	source := f.source("library", models.FilesystemModeLocal, library, other, library+string(filepath.Separator))
	second := f.source("second", models.FilesystemModeLocal, library)
	crossSeed := f.instance("seeds", f.qbt, models.FilesystemModeNone)

	// The key is cleaned, and a base folder without a row keeps the same path.
	rec := f.save(source, crossSeed, models.CrossSeedRouteRow{BaseDir: " " + other + string(filepath.Separator) + " ", MappedPath: " /seeds/links "})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	wantRows := []models.CrossSeedRouteRow{{BaseDir: library}, {BaseDir: other, MappedPath: "/seeds/links"}}

	got := decodeRoute(t, f.do(http.MethodGet, source.ID, nil))
	assert.Equal(t, crossSeed.ID, got.CrossSeedInstanceID)
	assert.Equal(t, wantRows, got.Rows)
	assert.Empty(t, got.BrokenRule)

	// Several Source instances can route to one Cross-seed instance.
	require.Equal(t, http.StatusOK, f.save(second, crossSeed).Code)
	sources, err := f.routes.ListSources(t.Context(), crossSeed.ID)
	require.NoError(t, err)
	assert.Equal(t, []int{source.ID, second.ID}, sources)
	var listed []models.CrossSeedRoute
	rec = f.do(http.MethodGet, 0, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed, 2)
	assert.Equal(t, source.ID, listed[0].SourceInstanceID)
	assert.Equal(t, second.ID, listed[1].SourceInstanceID)

	// The read keeps a saved route that no longer passes, and names the rule.
	_, err = f.instances.SetActiveState(t.Context(), crossSeed.ID, false)
	require.NoError(t, err)
	got = decodeRoute(t, f.do(http.MethodGet, source.ID, nil))
	assert.Equal(t, wantRows, got.Rows)
	assert.Contains(t, got.BrokenRule, "Cross-seed instance unavailable")

	require.Equal(t, http.StatusNoContent, f.do(http.MethodDelete, source.ID, nil).Code)
	assert.Nil(t, decodeRoute(t, f.do(http.MethodGet, source.ID, nil)))
}

func TestCrossSeedRouteUnknownInstance(t *testing.T) {
	f := newRouteFixture(t)
	source := f.source("library", models.FilesystemModeLocal, filepath.Join(t.TempDir(), "links"))

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := f.do(method, 999, crossSeedRouteRequest{CrossSeedInstanceID: source.ID})
		assert.Equal(t, http.StatusNotFound, rec.Code, method)
	}
	rec := f.do(http.MethodPut, source.ID, crossSeedRouteRequest{CrossSeedInstanceID: 999})
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestCrossSeedRouteDeletedWithEitherInstance(t *testing.T) {
	f := newRouteFixture(t)
	library := filepath.Join(t.TempDir(), "links")
	crossSeed := f.instance("seeds", f.qbt, models.FilesystemModeNone)
	kept := f.source("kept", models.FilesystemModeLocal, library)
	deleted := f.source("deleted", models.FilesystemModeLocal, library)
	require.Equal(t, http.StatusOK, f.save(kept, crossSeed).Code)
	require.Equal(t, http.StatusOK, f.save(deleted, crossSeed).Code)

	require.NoError(t, f.instances.Delete(t.Context(), deleted.ID))
	routes, err := f.routes.List(t.Context())
	require.NoError(t, err)
	require.Len(t, routes, 1)
	assert.Equal(t, kept.ID, routes[0].SourceInstanceID)

	require.NoError(t, f.instances.Delete(t.Context(), crossSeed.ID))
	routes, err = f.routes.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, routes)
}

func TestCrossSeedRouteAvailabilityOfDisabledInstance(t *testing.T) {
	f := newRouteFixture(t)
	instance := f.instance("seeds", f.qbt, models.FilesystemModeNone)
	require.NoError(t, f.routes.CheckAvailable(t.Context(), instance.ID, true))

	_, err := f.instances.SetActiveState(t.Context(), instance.ID, false)
	require.NoError(t, err)

	err = f.routes.CheckAvailable(t.Context(), instance.ID, false)
	require.ErrorIs(t, err, internalqbittorrent.ErrInstanceDisabled)
	require.ErrorIs(t, err, crossseedroute.ErrUnavailable)
}

func TestCrossSeedRouteAvailabilityChecksVersionOnlyForListing(t *testing.T) {
	f := newRouteFixture(t)
	instance := f.instance("seeds", f.old, models.FilesystemModeNone)

	require.NoError(t, f.routes.CheckAvailable(t.Context(), instance.ID, false))
	require.ErrorIs(t, f.routes.CheckAvailable(t.Context(), instance.ID, true), crossseedroute.ErrUnavailable)
}
