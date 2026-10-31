// Copyright (c) 2026, s0up and the autobrr contributors.
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

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
)

// listingQBittorrent answers as qBittorrent 5.0, and answers each directory
// listing with the status and the entries that list returns. Status 0 drops
// the connection.
func listingQBittorrent(t *testing.T, list func(dirPath, mode string) (int, []string)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.2"))
		case "/api/v2/app/getDirectoryContent":
			status, entries := list(r.URL.Query().Get("dirPath"), r.URL.Query().Get("mode"))
			if status == 0 {
				conn, _, err := http.NewResponseController(w).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
			if status != http.StatusOK {
				http.Error(w, "no", status)
				return
			}
			_ = json.NewEncoder(w).Encode(entries)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// diskListing lists the folders of a real folder as qBittorrent does, with
// each entry as a full path, and 404 for a missing folder. views maps a path
// that the Cross-seed instance sees to the real folder.
func diskListing(views map[string]string) func(string, string) (int, []string) {
	return func(dirPath, mode string) (int, []string) {
		folder, ok := views[dirPath]
		if !ok || mode != string(qbt.DirectoryContentDirs) {
			return http.StatusNotFound, nil
		}
		entries, err := os.ReadDir(folder)
		if err != nil {
			return http.StatusNotFound, nil
		}
		var listed []string
		for _, entry := range entries {
			if entry.IsDir() {
				listed = append(listed, strings.TrimRight(dirPath, `/\`)+"/"+entry.Name())
			}
		}
		return http.StatusOK, listed
	}
}

func (f *routeFixture) testRoute(source, crossSeed *models.Instance, rows ...models.CrossSeedRouteRow) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.request(http.MethodPost, "/api/cross-seed/routes/"+strconv.Itoa(source.ID)+"/test", crossSeedRouteRequest{CrossSeedInstanceID: crossSeed.ID, Rows: rows})
}

func decodeRouteTest(t *testing.T, rec *httptest.ResponseRecorder) *crossseedroute.TestResult {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result *crossseedroute.TestResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	return result
}

// assertOnlyFolders fails when dir holds anything other than the folders in want.
func assertOnlyFolders(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "qui keeps the base folder")
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	assert.ElementsMatch(t, want, got, "the test removes its marker")
}

func TestCrossSeedRouteTestReportsEachFolder(t *testing.T) {
	f := newRouteFixture(t)
	// same keeps its path and does not exist yet, so qui creates it.
	same := filepath.Join(t.TempDir(), "links")
	mapped := filepath.Join(t.TempDir(), "links")
	// stale is seen at a folder that is not the base folder.
	stale := filepath.Join(t.TempDir(), "links")
	staleView := t.TempDir()
	hidden := filepath.Join(t.TempDir(), "links")
	// A file where a parent folder must be, so qui cannot create the base folder.
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	unwritable := filepath.Join(blocker, "links")
	require.NoError(t, os.MkdirAll(mapped, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(mapped, "movies"), 0o755))

	source := f.source("library", models.FilesystemModeLocal, same, mapped, stale, hidden, unwritable)
	crossSeed := f.instance("seeds", listingQBittorrent(t, diskListing(map[string]string{
		same:             same,
		`D:\seeds\Links`: mapped,
		"/seeds/stale":   staleView,
		unwritable:       unwritable,
	})), models.FilesystemModeNone)

	got := decodeRouteTest(t, f.testRoute(source, crossSeed,
		models.CrossSeedRouteRow{BaseDir: mapped, MappedPath: `D:\seeds\Links`},
		models.CrossSeedRouteRow{BaseDir: stale, MappedPath: "/seeds/stale"},
		models.CrossSeedRouteRow{BaseDir: hidden, MappedPath: "/seeds/hidden"},
	))

	assert.Empty(t, got.Unavailable)
	require.Len(t, got.Rows, 5)
	want := []struct {
		baseDir, listed string
		result          crossseedroute.TestOutcome
	}{
		{same, same, crossseedroute.TestSees},
		{mapped, `D:\seeds\Links`, crossseedroute.TestSees},
		{stale, "/seeds/stale", crossseedroute.TestSeesFolderNotMarker},
		{hidden, "/seeds/hidden", crossseedroute.TestCannotSeeFolder},
		{unwritable, unwritable, crossseedroute.TestCannotWrite},
	}
	for i, row := range want {
		assert.Equal(t, row.baseDir, got.Rows[i].BaseDir, i)
		assert.Equal(t, row.listed, got.Rows[i].ListedPath, i)
		assert.Equal(t, row.result, got.Rows[i].Result, got.Rows[i])
	}
	assert.NotEmpty(t, got.Rows[4].Error, "a write failure names its cause")

	assertOnlyFolders(t, same)
	assertOnlyFolders(t, mapped, "movies")
	assertOnlyFolders(t, stale)
	assertOnlyFolders(t, hidden)
}

func TestCrossSeedRouteTestReportsAnUnavailableCrossSeedInstanceOnce(t *testing.T) {
	cases := []struct {
		name string
		host func(f *routeFixture) string
		// created is true when qui reaches the base folders before it finds out.
		created bool
		off     bool
	}{
		{name: "offline", host: func(f *routeFixture) string { return f.offline }},
		{name: "older than qBittorrent 5.0", host: func(f *routeFixture) string { return f.old }},
		{name: "turned off in qui", host: func(f *routeFixture) string { return f.qbt }, off: true},
		{
			// go-qbittorrent turns a 5xx answer into an error of its own, not
			// an unexpected status, so it is no path problem.
			name: "listing fails",
			host: func(f *routeFixture) string {
				return listingQBittorrent(f.t, func(string, string) (int, []string) { return http.StatusInternalServerError, nil })
			},
			created: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRouteFixture(t)
			first := filepath.Join(t.TempDir(), "links")
			second := filepath.Join(t.TempDir(), "links")
			source := f.source("library", models.FilesystemModeLocal, first, second)
			crossSeed := f.instance("seeds", tc.host(f), models.FilesystemModeNone)
			if tc.off {
				_, err := f.instances.SetActiveState(t.Context(), crossSeed.ID, false)
				require.NoError(t, err)
			}

			got := decodeRouteTest(t, f.testRoute(source, crossSeed))
			assert.Contains(t, got.Unavailable, "Cross-seed instance unavailable: seeds")
			assert.Empty(t, got.Rows)

			if !tc.created {
				assert.NoDirExists(t, first, "qui checks the Cross-seed instance before it writes")
				return
			}
			assertOnlyFolders(t, first)
		})
	}
}

func TestCrossSeedRouteTestRefuses(t *testing.T) {
	f := newRouteFixture(t)
	library := filepath.Join(t.TempDir(), "links")
	crossSeed := f.instance("seeds", f.qbt, models.FilesystemModeNone)

	cases := []struct {
		name   string
		source *models.Instance
		rows   []models.CrossSeedRouteRow
		want   string
	}{
		{name: "source without filesystem access", source: f.source("none", models.FilesystemModeNone, library), want: "needs Local Filesystem Access"},
		{name: "remote source", source: f.source("remote", models.FilesystemModeRemote, "/data/links"), want: "remote is a remote instance"},
		{name: "row that is not a base folder", source: f.source("local", models.FilesystemModeLocal, library), rows: []models.CrossSeedRouteRow{{BaseDir: t.TempDir()}}, want: "is not a base folder"},
		{name: "relative mapped path", source: f.source("relative", models.FilesystemModeLocal, library), rows: []models.CrossSeedRouteRow{{BaseDir: library, MappedPath: "seeds"}}, want: "is not an absolute path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.testRoute(tc.source, crossSeed, tc.rows...)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.want)
		})
	}
	assert.NoDirExists(t, library, "a refused test writes nothing")

	rec := f.testRoute(f.source("unknown", models.FilesystemModeLocal, library), &models.Instance{ID: 999})
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestCrossSeedRouteListingCheck(t *testing.T) {
	entries := []string{
		"/seeds/links/Plain/",
		`D:\seeds\links\Upper`,
		`\\nas\share\links\Unc\`,
		"/seeds/links/back\\Slash",
	}
	cases := []struct {
		name   string
		status int
		find   string
		want   bool
	}{
		{name: "slash entry with a trailing separator", status: http.StatusOK, find: "Plain", want: true},
		{name: "case differs on a slash entry", status: http.StatusOK, find: "plain"},
		{name: "case ignored on a drive-letter entry", status: http.StatusOK, find: "upper", want: true},
		{name: "backslash entry", status: http.StatusOK, find: "Unc", want: true},
		{name: "case kept on a UNC entry", status: http.StatusOK, find: "unc"},
		{name: "last element after a backslash", status: http.StatusOK, find: "Slash", want: true},
		{name: "missing entry", status: http.StatusOK, find: "links"},
		{name: "folder not found", status: http.StatusNotFound, find: "Plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRouteFixture(t)
			crossSeed := f.instance("seeds", listingQBittorrent(t, func(_, mode string) (int, []string) {
				assert.Equal(t, string(qbt.DirectoryContentAll), mode)
				return tc.status, entries
			}), models.FilesystemModeNone)

			found, err := f.routes.Lists(t.Context(), crossSeed.ID, "/seeds/links", tc.find, qbt.DirectoryContentAll)
			if tc.status != http.StatusOK {
				require.ErrorIs(t, err, crossseedroute.ErrCannotSeeFolder)
				require.NotErrorIs(t, err, crossseedroute.ErrUnavailable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, found)
		})
	}

	t.Run("transport error", func(t *testing.T) {
		f := newRouteFixture(t)
		// The listing drops the connection without an answer.
		crossSeed := f.instance("seeds", listingQBittorrent(t, func(string, string) (int, []string) {
			return 0, nil
		}), models.FilesystemModeNone)
		// go-qbittorrent waits seconds before it tries again, so stop it there.
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		defer cancel()
		_, err := f.routes.Lists(ctx, crossSeed.ID, "/seeds/links", "Plain", qbt.DirectoryContentAll)
		require.ErrorIs(t, err, crossseedroute.ErrUnavailable)
		require.NotErrorIs(t, err, crossseedroute.ErrCannotSeeFolder)
	})
}
