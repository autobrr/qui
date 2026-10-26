// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

// foldersRig sends each instance's requests to its stub in process, so a
// synctest bubble can advance its clock.
type foldersRig struct {
	sm *SyncManager
	// versionRequests counts app/webapiVersion requests.
	versionRequests atomic.Int32
}

func newFoldersRig(stubs map[int]*qbtstub.Server) *foldersRig {
	r := &foldersRig{}
	clients := make(map[int]*Client, len(stubs))
	for id, stub := range stubs {
		transport := peerSyncTestTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v2/app/webapiVersion" {
				r.versionRequests.Add(1)
			}
			rec := httptest.NewRecorder()
			stub.Config.Handler.ServeHTTP(rec, req)
			return rec.Result(), nil
		})
		qbtClient := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid", APIKey: "test-key"}).WithHTTPClient(&http.Client{Transport: transport})
		clients[id] = &Client{Client: qbtClient, isHealthy: true}
	}
	r.sm = &SyncManager{clientPool: &ClientPool{clients: clients}}
	return r
}

func (r *foldersRig) folders(t *testing.T, instanceID int) (InstanceFolders, error) {
	return r.sm.InstanceFolders(t.Context(), fsops.SlashPaths, instanceID)
}

type stubCategory struct {
	save     string
	download *qbt.CategoryDownloadPath
}

func newFoldersStub(t *testing.T, categories map[string]stubCategory) *qbtstub.Server {
	stub := qbtstub.New(t, "/data")
	stub.DownloadPath = "/dl/temp"
	for name, c := range categories {
		stub.AddCategory(name, c.save)
		stub.SetCategoryDownloadPath(name, c.download)
	}
	return stub
}

// The download path rows are what qBittorrent 4.6.7, 5.1.4, 5.2.4 and
// 5.4.0alpha1 reported as an automatically managed torrent's download_path
// for the same categories, with the download path /dl/temp. qBittorrent never
// produces a parent missing from the map or a relative download path itself,
// so those rows follow its source.
//
// The version rows were observed live with Automatic Torrent Management on and
// categories tv=/media/tv, tv/anime="". 5.0.0beta1 is the only release with
// WebAPI 2.10.x; it was not run live, but upstream "Follow the parent category
// options" (a1c78a045) is in it. Each gate has a row for the highest version
// below it and the lowest version at it.
func TestInstanceFolders(t *testing.T) {
	t.Parallel()

	on := func(p string) *qbt.CategoryDownloadPath { return &qbt.CategoryDownloadPath{Enabled: true, Path: p} }
	off := &qbt.CategoryDownloadPath{}
	subsOff := func(s *qbtstub.Server) { s.SubcategoriesOff = true }
	downloadOff := func(s *qbtstub.Server) { s.DownloadPathOff = true }

	versioned := map[string]stubCategory{"tv": {save: "/media/tv"}, "tv/anime": {}, "movies:hd": {}}
	flatSaves := []string{"/data/movies hd", "/data/tv/anime", "/media/tv"}
	nestedSaves := []string{"/data/movies hd", "/media/tv", "/media/tv/anime"}
	namedDownloads := []string{"/dl/temp/movies hd", "/dl/temp/tv", "/dl/temp/tv/anime"}
	tv := []string{"/data/tv"}
	tvHD := []string{"/data/tv", "/data/tv/hd"}

	tests := []struct {
		name       string
		version    string
		setup      func(s *qbtstub.Server)
		categories map[string]stubCategory
		want       InstanceFolders
		wantErr    string
	}{
		{name: "4.5 predates subcategories", version: "2.8.19", categories: versioned,
			want: InstanceFolders{CategorySaveFolders: flatSaves, DownloadFolders: []string{"/dl/temp"}}},
		{name: "4.6 with subcategories on saves under the default path", version: "2.9.3", categories: versioned,
			want: InstanceFolders{CategorySaveFolders: flatSaves, DownloadFolders: []string{"/dl/temp"}}},
		{name: "5.0 beta1 with subcategories on", version: "2.10.4", categories: versioned,
			want: InstanceFolders{CategorySaveFolders: nestedSaves, DownloadFolders: namedDownloads}},
		{name: "5.0 with subcategories off", version: "2.11.2", setup: subsOff, categories: versioned,
			want: InstanceFolders{CategorySaveFolders: flatSaves, DownloadFolders: namedDownloads}},
		{name: "5.0 with subcategories on", version: "2.11.2", categories: versioned,
			want: InstanceFolders{CategorySaveFolders: nestedSaves, DownloadFolders: namedDownloads}},
		{name: "5.1 with subcategories on", version: "2.11.4", categories: versioned,
			want: InstanceFolders{CategorySaveFolders: nestedSaves, DownloadFolders: namedDownloads}},
		{name: "5.2 always nests", version: "2.15.1", setup: subsOff, categories: versioned,
			want: InstanceFolders{CategorySaveFolders: nestedSaves, DownloadFolders: namedDownloads}},
		{name: "5.3 dev build before the file name rule", version: "2.15.3", setup: func(s *qbtstub.Server) { s.Platform = "windows" }, categories: versioned,
			want: InstanceFolders{CategorySaveFolders: nestedSaves, DownloadFolders: namedDownloads}},
		{name: "5.3 on Windows", version: "2.15.4", setup: func(s *qbtstub.Server) { s.Platform = "windows" }, categories: versioned,
			want: InstanceFolders{
				CategorySaveFolders: []string{"/data/movies_hd", "/media/tv", "/media/tv/anime"},
				DownloadFolders:     []string{"/dl/temp/movies_hd", "/dl/temp/tv", "/dl/temp/tv/anime"},
			}},
		{name: "5.3 on Linux", version: "2.15.4", setup: func(s *qbtstub.Server) { s.Platform = "linux" }, categories: versioned,
			want: InstanceFolders{
				CategorySaveFolders: []string{"/data/movies:hd", "/media/tv", "/media/tv/anime"},
				DownloadFolders:     []string{"/dl/temp/movies:hd", "/dl/temp/tv", "/dl/temp/tv/anime"},
			}},

		{name: "own save paths", setup: downloadOff, categories: map[string]stubCategory{
			"movies": {save: "//media/movies/"}, "music": {save: "music"}, "spaced": {save: " archive "},
			// A child inherits an explicit dot, and walks the whole parent chain.
			"dot": {save: "."}, "dot/hd": {}, "a": {save: "/archive"}, "a/b": {}, "a/b/c": {},
		}, want: InstanceFolders{CategorySaveFolders: []string{
			"/archive", "/archive/b", "/archive/b/c", "/data", "/data/ archive ", "/data/hd", "/data/music", "/media/movies",
		}}},
		{name: "folder names before 5.3", setup: downloadOff, categories: map[string]stubCategory{
			// toValidPath replaces a run with one space and leaves a backslash
			// alone, and nesting converts only the last segment.
			`films:?"x`: {}, `a\b`: {}, "movies": {save: "/archive"}, "movies/h:d": {},
		}, want: InstanceFolders{CategorySaveFolders: []string{"/archive", "/archive/h d", `/data/a\b`, "/data/films x"}}},

		{name: "4.6 inherit is the download path itself", version: "2.9.3", categories: map[string]stubCategory{"tv": {}},
			want: InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/temp"}}},
		{name: "4.6 inherit with the download path off", version: "2.9.3", setup: downloadOff, categories: map[string]stubCategory{"tv": {}},
			want: InstanceFolders{CategorySaveFolders: tv}},
		{name: "4.6 turned off", version: "2.9.3", categories: map[string]stubCategory{"tv": {download: off}},
			want: InstanceFolders{CategorySaveFolders: tv}},
		{name: "4.6 own relative path under the download path even when it is off", version: "2.9.3", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("inc")}},
			want:       InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/temp/inc"}}},
		{name: "4.6 own empty path is named after the category", version: "2.9.3", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/temp/tv"}}},
		{name: "4.6 own absolute path", version: "2.9.3", categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}},
			want: InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/fast/tv"}}},
		{name: "4.6 subcategory ignores its parent", version: "2.9.3", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("/fast")}, "tv/hd": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv/hd", "/fast"}}},

		{name: "5.1 subcategories off, inherit is named under the download path", setup: subsOff, categories: map[string]stubCategory{"tv": {}},
			want: InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/temp/tv"}}},
		{name: "5.1 subcategories off, subcategory ignores its parent's path", setup: subsOff,
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv/hd", "/fast/tv"}}},
		{name: "5.1 subcategories off, inherit with the download path off", setup: func(s *qbtstub.Server) { subsOff(s); downloadOff(s) },
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/fast/tv"}}},
		{name: "5.1 subcategories off, parent turned off does not apply", setup: subsOff,
			categories: map[string]stubCategory{"tv": {download: off}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv/hd"}}},
		{name: "5.1 subcategories off, empty path is the full name", setup: func(s *qbtstub.Server) { subsOff(s); downloadOff(s) },
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}, "tv/hd": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv/hd", "/fast/tv"}}},
		{name: "5.1 subcategories off, relative path under the download path", setup: subsOff,
			categories: map[string]stubCategory{"tv": {download: on("inc")}, "tv/hd": {download: on("x")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/inc", "/dl/temp/x"}}},
		{name: "5.1 subcategories on, inherit takes the parent's path",
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/fast/tv"}}},
		{name: "5.1 subcategories on, relative path under the parent's",
			categories: map[string]stubCategory{"tv": {download: on("inc")}, "tv/hd": {download: on("x")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/inc", "/dl/temp/inc/x"}}},

		{name: "5.2 inherit with the download path off", version: "2.15.1", setup: downloadOff, categories: map[string]stubCategory{"tv": {}},
			want: InstanceFolders{CategorySaveFolders: tv}},
		{name: "5.2 turned off", version: "2.15.1", categories: map[string]stubCategory{"tv": {download: off}},
			want: InstanceFolders{CategorySaveFolders: tv}},
		{name: "5.2 empty path under the download path even when it is off", version: "2.15.1", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/temp/tv"}}},
		{name: "5.2 relative path under the download path even when it is off", version: "2.15.1", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("inc")}},
			want:       InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/temp/inc"}}},
		{name: "5.2 relative path is cleaned", version: "2.15.1", categories: map[string]stubCategory{"tv": {download: on("../inc")}},
			want: InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/dl/inc"}}},
		{name: "5.2 subcategory inherit nests under the parent", version: "2.15.1", categories: map[string]stubCategory{"tv": {}, "tv/hd": {}},
			want: InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv", "/dl/temp/tv/hd"}}},
		{name: "5.2 inherit takes the parent's path with the download path off", version: "2.15.1", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/fast/tv"}}},
		{name: "5.2 inherit takes the parent's relative path twice", version: "2.15.1",
			categories: map[string]stubCategory{"tv": {download: on("inc")}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/inc", "/dl/temp/inc/inc"}}},
		{name: "5.2 inherit takes the parent's off", version: "2.15.1",
			categories: map[string]stubCategory{"tv": {download: off}, "tv/hd": {}},
			want:       InstanceFolders{CategorySaveFolders: tvHD}},
		{name: "5.2 parent turned off still names a folder", version: "2.15.1", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: off}, "tv/hd": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv/hd"}}},
		{name: "5.2 empty path under the parent's path", version: "2.15.1", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}, "tv/hd": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/fast/tv", "/fast/tv/hd"}}},
		{name: "5.2 parent that inherits still names a folder", version: "2.15.1", setup: downloadOff,
			categories: map[string]stubCategory{"tv": {}, "tv/hd": {download: on("")}},
			want:       InstanceFolders{CategorySaveFolders: tvHD, DownloadFolders: []string{"/dl/temp/tv/hd"}}},
		{name: "5.2 parent missing from the map", version: "2.15.1", categories: map[string]stubCategory{"tv/hd": {}},
			want: InstanceFolders{CategorySaveFolders: []string{"/data/tv/hd"}, DownloadFolders: []string{"/dl/temp/tv/hd"}}},
		{name: "an absolute own path needs no download path", version: "2.15.1",
			setup:      func(s *qbtstub.Server) { s.DownloadPath = "dl"; downloadOff(s) },
			categories: map[string]stubCategory{"tv": {download: on("/fast/tv")}},
			want:       InstanceFolders{CategorySaveFolders: tv, DownloadFolders: []string{"/fast/tv"}}},

		{name: "monitored folders and their save paths", setup: func(s *qbtstub.Server) {
			s.ScanDirs = map[string]any{"/data/watch": 0, "/data/blackhole": 1, "/media/watch": "/data/from-watch", "/media/rel": "from-rel"}
		}, want: InstanceFolders{MonitoredFolders: []string{"/data/blackhole", "/data/from-rel", "/data/from-watch", "/data/watch", "/media/rel", "/media/watch"}}},
		{name: "a monitored folder that is not absolute is left out", setup: func(s *qbtstub.Server) {
			s.ScanDirs = map[string]any{"watch": 0}
		}},

		{name: "a trailing separator is cleaned", setup: func(s *qbtstub.Server) { s.SetDefaultSavePath("/data/") }},
		{name: "trailing spaces are kept", setup: func(s *qbtstub.Server) { s.SetDefaultSavePath("/data  ") },
			want: InstanceFolders{DefaultSavePath: "/data  "}},
		{name: "an empty default save path", setup: func(s *qbtstub.Server) { s.SetDefaultSavePath("") },
			wantErr: "qBittorrent reported an empty default save path"},
		{name: "a relative default save path", setup: func(s *qbtstub.Server) { s.SetDefaultSavePath("data") },
			wantErr: `qBittorrent default save path "data" is not absolute`},
		{name: "leading spaces make the default save path relative", setup: func(s *qbtstub.Server) { s.SetDefaultSavePath("  /data") },
			wantErr: `qBittorrent default save path "  /data" is not absolute`},
		{name: "a relative download path that is on", setup: func(s *qbtstub.Server) { s.DownloadPath = "dl" },
			wantErr: `qBittorrent download path "dl" is not absolute`},
		{name: "a category download folder on a relative download path", version: "2.15.1",
			setup:      func(s *qbtstub.Server) { s.DownloadPath = "dl"; downloadOff(s) },
			categories: map[string]stubCategory{"tv": {download: on("")}},
			wantErr:    `download path of category "tv" rests on the qBittorrent download path "dl", which is not absolute`},
	}
	// The rows are written in slash paths. The host run writes each path in
	// the form of the host, so the Windows CI job checks its path rules.
	dialects := []struct {
		name string
		d    fsops.PathDialect
		path func(string) string
	}{
		{"slash", fsops.SlashPaths, func(p string) string { return p }},
		{"host", fsops.HostPaths, hostPath},
	}
	for _, tt := range tests {
		for _, dialect := range dialects {
			t.Run(tt.name+"/"+dialect.name, func(t *testing.T) {
				t.Parallel()
				p := dialect.path
				paths := func(list []string) []string {
					out := slices.Clone(list)
					for i, s := range out {
						out[i] = p(s)
					}
					slices.Sort(out)
					return out
				}

				categories := make(map[string]stubCategory, len(tt.categories))
				for name, c := range tt.categories {
					c.save = p(c.save)
					if c.download != nil {
						c.download = &qbt.CategoryDownloadPath{Enabled: c.download.Enabled, Path: p(c.download.Path)}
					}
					categories[name] = c
				}
				stub := newFoldersStub(t, categories)
				stub.WebAPIVersion = tt.version
				if tt.setup != nil {
					tt.setup(stub)
				}
				stub.SetDefaultSavePath(p(stub.DefaultSavePath()))
				stub.DownloadPath = p(stub.DownloadPath)
				scanDirs := make(map[string]any, len(stub.ScanDirs))
				for dir, target := range stub.ScanDirs {
					if s, ok := target.(string); ok {
						target = p(s)
					}
					scanDirs[p(dir)] = target
				}
				stub.ScanDirs = scanDirs

				got, err := newFoldersRig(map[int]*qbtstub.Server{1: stub}).sm.InstanceFolders(t.Context(), dialect.d, 1)
				if tt.wantErr != "" {
					// The text quotes a path as qBittorrent sent it, in
					// slash form only in the slash run.
					if dialect.d == fsops.SlashPaths {
						require.EqualError(t, err, tt.wantErr)
					} else {
						require.Error(t, err)
					}
					return
				}
				require.NoError(t, err)

				want := tt.want
				if want.DefaultSavePath == "" {
					want.DefaultSavePath = "/data"
				}
				if !stub.DownloadPathOff {
					want.DownloadPath = "/dl/temp"
				}
				require.Equal(t, InstanceFolders{
					DefaultSavePath:     p(want.DefaultSavePath),
					DownloadPath:        p(want.DownloadPath),
					CategorySaveFolders: paths(want.CategorySaveFolders),
					DownloadFolders:     paths(want.DownloadFolders),
					MonitoredFolders:    paths(want.MonitoredFolders),
				}, got)
			})
		}
	}
}

// hostPath writes a slash fixture path in the form of the host. On Windows an
// absolute path also gets a drive, because \data is not absolute there.
func hostPath(p string) string {
	if strings.HasPrefix(p, "/") {
		p = filepath.VolumeName(os.TempDir()) + p
	}
	return filepath.FromSlash(p)
}

// The preferences and the categories are read on every call, so a change in
// qBittorrent's WebUI reaches the next call while the version is still kept.
func TestInstanceFoldersSeesAChangeWhileTheVersionIsKept(t *testing.T) {
	stub := newFoldersStub(t, map[string]stubCategory{
		"tv":       {save: "/media/tv"},
		"tv/anime": {},
		"archive":  {save: "/media/archive", download: &qbt.CategoryDownloadPath{Enabled: true, Path: "/fast/archive"}},
	})
	synctest.Test(t, func(t *testing.T) {
		r := newFoldersRig(map[int]*qbtstub.Server{1: stub})
		before, err := r.folders(t, 1)
		require.NoError(t, err)
		require.Contains(t, before.CategorySaveFolders, "/media/tv/anime")
		require.Contains(t, before.DownloadFolders, "/fast/archive")

		stub.SetDefaultSavePath("/new")
		stub.SubcategoriesOff = true
		stub.SetCategoryDownloadPath("archive", nil)
		time.Sleep(10 * time.Second)

		after, err := r.folders(t, 1)
		require.NoError(t, err)
		require.EqualValues(t, 1, r.versionRequests.Load(), "the version is kept")
		require.Equal(t, "/new", after.DefaultSavePath)
		require.Equal(t, []string{"/media/archive", "/media/tv", "/new/tv/anime"}, after.CategorySaveFolders)
		require.NotContains(t, after.DownloadFolders, "/fast/archive")

		stub.SubcategoriesOff = false
		again, err := r.folders(t, 1)
		require.NoError(t, err)
		require.EqualValues(t, 1, r.versionRequests.Load(), "the version is kept")
		require.Equal(t, []string{"/media/archive", "/media/tv", "/media/tv/anime"}, again.CategorySaveFolders)
	})
}

// Reading the version refreshes qBittorrent's capabilities with a request, so
// the answer is kept for a minute.
func TestInstanceFoldersKeepsTheVersionForAMinute(t *testing.T) {
	stubs := map[int]*qbtstub.Server{1: newFoldersStub(t, nil), 2: newFoldersStub(t, nil)}
	synctest.Test(t, func(t *testing.T) {
		r := newFoldersRig(stubs)
		_, err := r.folders(t, 1)
		require.NoError(t, err)
		require.EqualValues(t, 1, r.versionRequests.Load())

		time.Sleep(categoryVersionTTL - time.Second)
		_, err = r.folders(t, 1)
		require.NoError(t, err)
		require.EqualValues(t, 1, r.versionRequests.Load(), "a call within the minute keeps the version")

		time.Sleep(time.Second)
		_, err = r.folders(t, 1)
		require.NoError(t, err)
		require.EqualValues(t, 2, r.versionRequests.Load(), "a call after the minute asks again")

		// Another instance's call after the minute drops this one's expired
		// entry.
		time.Sleep(categoryVersionTTL)
		_, err = r.folders(t, 2)
		require.NoError(t, err)
		require.NotContains(t, r.sm.categoryVersions, 1, "an expired entry is dropped")
		require.Contains(t, r.sm.categoryVersions, 2)
	})
}

func TestInstanceFoldersDoesNotKeepAVersionError(t *testing.T) {
	t.Parallel()
	stub := newFoldersStub(t, nil)
	stub.WebAPIVersionStatus = http.StatusInternalServerError
	r := newFoldersRig(map[int]*qbtstub.Server{1: stub})
	_, err := r.folders(t, 1)
	require.Error(t, err)

	stub.WebAPIVersionStatus = 0
	_, err = r.folders(t, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, r.versionRequests.Load(), "the next call asks again")
}

// A client that keeps syncing skips health checks, so its capabilities can
// still be those of 4.6 after an in-place upgrade.
func TestInstanceFoldersRefreshesStaleCapabilities(t *testing.T) {
	t.Parallel()
	stub := newFoldersStub(t, map[string]stubCategory{"tv": {save: "/media/tv"}, "tv/anime": {}})
	r := newFoldersRig(map[int]*qbtstub.Server{1: stub})
	r.sm.clientPool.clients[1].applyCapabilitiesLocked("2.9.3")

	got, err := r.folders(t, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"/media/tv", "/media/tv/anime"}, got.CategorySaveFolders)
}

// Without the categories no category folder is kept, so a failed read fails
// the call.
func TestInstanceFoldersFailWhenTheCategoriesCannotBeRead(t *testing.T) {
	t.Parallel()
	stub := newFoldersStub(t, map[string]stubCategory{"archive": {save: "/media/archive"}})
	stub.CategoriesStatus = http.StatusInternalServerError
	_, err := newFoldersRig(map[int]*qbtstub.Server{1: stub}).folders(t, 1)
	require.Error(t, err)
}
