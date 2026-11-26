// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const rigTick = 250 * time.Millisecond

const rigKeptWait = 2*rigTick + 500*time.Millisecond

const proxyHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const proxyHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type noIgnores struct{}

func (noIgnores) IgnoredPathMatcher(context.Context, int, fsops.PathDialect) (func(string) bool, error) {
	return func(string) bool { return false }, nil
}

type proxyRig struct {
	t          *testing.T
	root       string
	db         *database.DB
	stub       *qbtstub.Server
	handler    *Handler
	instanceID int
	client     *qbittorrent.Client
	upstream   *url.URL
}

func newProxyRig(t *testing.T) *proxyRig {
	return newProxyRigWorker(t, true)
}

func newProxyRigWorker(t *testing.T, start bool) *proxyRig {
	root := t.TempDir()
	r := &proxyRig{t: t, root: root, stub: qbtstub.New(t, filepath.Join(root, "torrents"))}
	r.stub.AddCategory("tv", "")
	r.stub.AddCategory("movies", "")
	for _, dir := range []string{"torrents/incomplete", "torrents/tv", "torrents/movies"} {
		require.NoError(t, os.MkdirAll(r.p(dir), 0o755))
	}

	db := testdb.NewMigratedSQLite(t, "proxy-folder-cleanup")
	r.db = db
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", r.stub.URL, "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	r.instanceID = instance.ID

	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	r.client, err = pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)

	sm := qbittorrent.NewSyncManager(pool, nil)
	fc := qbittorrent.NewFolderCleanup(fsops.NewPool(instanceStore, local.NewBackend()), sm, noIgnores{})
	fc.SetTickForTest(rigTick)
	if start {
		fc.Start(t.Context())
	}
	t.Cleanup(fc.Stop)
	sm.SetFolderCleanup(fc)

	r.handler = NewHandler(pool, nil, instanceStore, sm, nil, nil, "/")
	r.upstream, err = url.Parse(r.stub.URL)
	require.NoError(t, err)
	return r
}

func (r *proxyRig) p(rel string) string { return filepath.Join(r.root, filepath.FromSlash(rel)) }

func (r *proxyRig) add(torrents ...qbtstub.Torrent) {
	for _, t := range torrents {
		r.stub.Add(t)
	}
	require.NoError(r.t, r.client.SyncOwner().Sync(r.t.Context()))
}

func (r *proxyRig) post(handle http.HandlerFunc, endpoint, body string) *httptest.ResponseRecorder {
	return r.postAs(handle, endpoint, "application/x-www-form-urlencoded", body)
}

func (r *proxyRig) postAs(handle http.HandlerFunc, endpoint, contentType, body string) *httptest.ResponseRecorder {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("api-key", "test-key")
	ctx := context.WithValue(r.t.Context(), chi.RouteCtxKey, routeCtx)
	ctx = context.WithValue(ctx, ClientAPIKeyContextKey, &models.ClientAPIKey{ClientName: "sonarr", InstanceID: r.instanceID})
	ctx = context.WithValue(ctx, InstanceIDContextKey, r.instanceID)
	ctx = context.WithValue(ctx, proxyContextKey, &proxyContext{instanceID: r.instanceID, instanceURL: r.upstream, httpClient: http.DefaultClient})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/proxy/test-key/api/v2/"+endpoint, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handle(rec, req)
	return rec
}

func (r *proxyRig) requireGone(rel string) {
	r.t.Helper()
	require.Eventually(r.t, func() bool {
		_, err := os.Lstat(r.p(rel))
		return err != nil
	}, 20*time.Second, 50*time.Millisecond, "%s must be removed", rel)
}

func (r *proxyRig) requireKept(rel string) {
	r.t.Helper()
	time.Sleep(rigKeptWait)
	require.DirExists(r.t, r.p(rel))
}

func (r *proxyRig) perRelease(hash, category string) qbtstub.Torrent {
	return qbtstub.Torrent{Hash: hash, Name: "Show.S01E01-GRP", SavePath: r.p("torrents/tv/Show.S01E01-GRP"),
		Category: category, Layout: qbtstub.NoRootFolder, Files: []string{"Show.S01E01.mkv"}}
}

func TestProxyFolderCleanupDelete(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))

	body := "hashes=" + proxyHashA + "&deleteFiles=true"
	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", body)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, body, r.stub.LastBody("torrents/delete"))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
}

func TestProxyFolderCleanupDeleteAll(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	other := r.perRelease(proxyHashB, "tv")
	other.SavePath = r.p("torrents/tv/Show.S01E02-GRP")
	r.add(r.perRelease(proxyHashA, "tv"), other)

	require.Equal(t, http.StatusOK, r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes=all&deleteFiles=TRUE").Code)
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	r.requireGone("torrents/tv/Show.S01E02-GRP")
}

type hungMount struct {
	fsops.Backend
	hang    string
	release chan struct{}
}

func (m hungMount) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	if strings.HasPrefix(p, m.hang) {
		<-m.release
	}
	return m.Backend.Stat(context.Background(), p)
}

type hungMountPool struct {
	*fsops.Pool
	mount hungMount
}

func (h hungMountPool) Require(ctx context.Context, instanceID int, capability models.FilesystemCapability, _ ...models.FilesystemCapability) (fsops.Backend, *models.Instance, error) {
	_, instance, err := h.Pool.Require(ctx, instanceID, capability)
	if err != nil {
		return nil, nil, err
	}
	return h.mount, instance, nil
}

func TestProxyFolderCleanupHungMountDoesNotHoldTheRequest(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"), qbtstub.Torrent{Hash: proxyHashB, Name: "Film-GRP", SavePath: r.p("torrents/movies/Film-GRP"),
		Category: "movies", Layout: qbtstub.NoRootFolder, Files: []string{"Film.mkv"}})
	mount := hungMount{Backend: local.NewBackend(), hang: r.p("torrents/movies/Film-GRP"), release: make(chan struct{})}
	t.Cleanup(func() { close(mount.release) })
	fc := qbittorrent.NewFolderCleanup(hungMountPool{fsops.NewPool(r.handler.instanceStore, local.NewBackend()), mount}, r.handler.syncManager, noIgnores{})
	fc.SetTickForTest(rigTick)
	fc.Start(t.Context())
	t.Cleanup(fc.Stop)
	r.handler.syncManager.SetFolderCleanup(fc)

	start := time.Now()
	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"|"+proxyHashB+"&deleteFiles=true")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Less(t, time.Since(start), 15*time.Second)
	require.Equal(t, 1, r.stub.Writes("torrents/delete"))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	r.requireKept("torrents/movies/Film-GRP")
	require.NoFileExists(t, r.p("torrents/movies/Film-GRP/Film.mkv"))
}

func TestProxyFolderCleanupDeleteQBittorrentIgnores(t *testing.T) {
	t.Parallel()
	v2 := proxyHashA + strings.Repeat("c", 24)
	for _, tc := range []struct{ name, hashes string }{
		{name: "padded hash", hashes: "%20" + proxyHashA},
		{name: "v2 hash", hashes: v2},
		{name: "v1 hash of a hybrid torrent", hashes: proxyHashB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newProxyRig(t)
			r.add(qbtstub.Torrent{Hash: proxyHashA, InfohashV1: proxyHashB, InfohashV2: v2, Name: "Rel-GRP", SavePath: r.p("torrents/tv/Show"),
				Category: "tv", Layout: qbtstub.RootFolder, Files: []string{"notes.txt~", "Sub/Thumbs.db"}})

			require.Equal(t, http.StatusOK, r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+tc.hashes+"&deleteFiles=true").Code)
			r.requireKept("torrents/tv/Show/Rel-GRP/Sub")
			require.FileExists(t, r.p("torrents/tv/Show/Rel-GRP/notes.txt~"))
		})
	}
}

func TestProxyFolderCleanupReadsParamsLikeQBittorrent(t *testing.T) {
	t.Parallel()
	multipartBody := func(fields ...string) (string, string) {
		var buf strings.Builder
		mw := multipart.NewWriter(&buf)
		for i := 0; i < len(fields); i += 2 {
			require.NoError(t, mw.WriteField(fields[i], fields[i+1]))
		}
		require.NoError(t, mw.Close())
		return mw.FormDataContentType(), buf.String()
	}
	form := "application/x-www-form-urlencoded"
	keepCT, keepBody := multipartBody("hashes", proxyHashA, "deleteFiles", "false")
	for _, tc := range []struct {
		name, endpoint, contentType, body string
		filesDeleted                      bool
	}{
		{name: "deleteFiles true then false", endpoint: "torrents/delete", contentType: form,
			body: "hashes=" + proxyHashA + "&deleteFiles=true&deleteFiles=false"},
		{name: "deleteFiles false then true", endpoint: "torrents/delete", contentType: form,
			body: "hashes=" + proxyHashA + "&deleteFiles=false&deleteFiles=true", filesDeleted: true},
		{name: "deleteFiles true in the query only", endpoint: "torrents/delete?deleteFiles=true", contentType: form,
			body: "hashes=" + proxyHashA + "&deleteFiles=false"},
		{name: "multipart body keeping files, query deleting them", endpoint: "torrents/delete?hashes=" + proxyHashA + "&deleteFiles=true",
			contentType: keepCT, body: keepBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newProxyRig(t)
			r.add(qbtstub.Torrent{Hash: proxyHashA, Name: "Rel-GRP", SavePath: r.p("torrents/tv/Show"),
				Category: "tv", Layout: qbtstub.RootFolder, Files: []string{"notes.txt~", "Sub/Thumbs.db"}})

			require.Equal(t, http.StatusOK, r.postAs(r.handler.handleDeleteTorrents, tc.endpoint, tc.contentType, tc.body).Code)
			require.Equal(t, 1, r.stub.Writes("torrents/delete"))
			if tc.filesDeleted {
				r.requireGone("torrents/tv/Show")
				return
			}
			r.requireKept("torrents/tv/Show/Rel-GRP/Sub")
			require.FileExists(t, r.p("torrents/tv/Show/Rel-GRP/notes.txt~"), "qBittorrent kept the files")
		})
	}
}

func TestProxyFolderCleanupContentTypeLikeQBittorrent(t *testing.T) {
	t.Parallel()
	body := "hashes=" + proxyHashA + "&category=movies"
	var multipartBody strings.Builder
	mw := multipart.NewWriter(&multipartBody)
	require.NoError(t, mw.WriteField("hashes", proxyHashA))
	require.NoError(t, mw.WriteField("category", "movies"))
	require.NoError(t, mw.Close())
	for _, tc := range []struct {
		name, contentType, body string
		snapshot                bool
	}{
		{name: "malformed parameter", contentType: "application/x-www-form-urlencoded; charset", body: body, snapshot: true},
		{name: "mixed case with a charset", contentType: "Application/X-WWW-Form-Urlencoded;charset=UTF-8", body: body, snapshot: true},
		{name: "multipart", contentType: mw.FormDataContentType(), body: multipartBody.String()},
		{name: "plain text", contentType: "text/plain", body: body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newProxyRig(t)
			r.add(qbtstub.Torrent{Hash: proxyHashA, Name: "Show", SavePath: r.p("torrents/tv"), Category: "tv", AutoTMM: true,
				Layout: qbtstub.NoRootFolder, Files: []string{"Show/Show.S01E01.mkv"}})

			require.Equal(t, http.StatusOK, r.postAs(r.handler.handleSetCategory, "torrents/setCategory", tc.contentType, tc.body).Code)
			require.Equal(t, tc.body, r.stub.LastBody("torrents/setCategory"))
			if tc.snapshot {
				r.requireGone("torrents/tv/Show")
				return
			}
			_ = os.Remove(r.p("torrents/tv/Show/Show.S01E01.mkv"))
			r.requireKept("torrents/tv/Show")
		})
	}
}

func TestProxyFolderCleanupDeleteKeepingFiles(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))

	require.Equal(t, http.StatusOK, r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=false").Code)
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

const rigHintWait = time.Second

func TestProxyFolderCleanupHintsARefreshOnlyWhenItQueues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		endpoint string
		body     string
		handle   func(h *Handler) http.HandlerFunc
		hint     bool
	}{
		{"delete with files", "torrents/delete", "hashes=" + proxyHashA + "&deleteFiles=true",
			func(h *Handler) http.HandlerFunc { return h.handleDeleteTorrents }, true},
		{"delete keeping files", "torrents/delete", "hashes=" + proxyHashA + "&deleteFiles=false",
			func(h *Handler) http.HandlerFunc { return h.handleDeleteTorrents }, false},
		{"turn ATM on", "torrents/setAutoManagement", "hashes=" + proxyHashA + "&enable=true",
			func(h *Handler) http.HandlerFunc { return h.handleSetAutoManagement }, true},
		{"turn ATM off", "torrents/setAutoManagement", "hashes=" + proxyHashA + "&enable=false",
			func(h *Handler) http.HandlerFunc { return h.handleSetAutoManagement }, false},
		{"delete of missing content", "torrents/delete", "hashes=" + proxyHashB + "&deleteFiles=true",
			func(h *Handler) http.HandlerFunc { return h.handleDeleteTorrents }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newProxyRigWorker(t, false)
			r.add(r.perRelease(proxyHashA, "movies"))
			r.add(qbtstub.Torrent{Hash: proxyHashB, Name: "Gone-GRP", SavePath: r.p("torrents/tv"), Category: "tv",
				Layout: qbtstub.RootFolder, Files: []string{"a.mkv"}})
			require.NoError(t, os.RemoveAll(r.p("torrents/tv/Gone-GRP")))
			before := r.stub.Syncs()

			require.Equal(t, http.StatusOK, r.post(tc.handle(r.handler), tc.endpoint, tc.body).Code)
			if tc.hint {
				require.Eventually(t, func() bool { return r.stub.Syncs() > before }, rigHintWait, 20*time.Millisecond)
				return
			}
			time.Sleep(rigHintWait)
			require.Equal(t, before, r.stub.Syncs())
		})
	}
}

func TestProxyFolderCleanupSetLocation(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))

	body := url.Values{"hashes": {proxyHashA}, "location": {r.p("torrents/movies")}}.Encode()
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetLocation, "torrents/setLocation", body).Code)
	require.Equal(t, body, r.stub.LastBody("torrents/setLocation"))
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.FileExists(t, r.p("torrents/movies/Show.S01E01.mkv"))
}

func TestProxyFolderCleanupSetCategory(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(qbtstub.Torrent{Hash: proxyHashA, Name: "Show", SavePath: r.p("torrents/tv"), Category: "tv", AutoTMM: true,
		Layout: qbtstub.NoRootFolder, Files: []string{"Show/Show.S01E01.mkv"}})

	body := "hashes=" + proxyHashA + "&category=movies"
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetCategory, "torrents/setCategory", body).Code)
	require.Equal(t, body, r.stub.LastBody("torrents/setCategory"))
	r.requireGone("torrents/tv/Show")
	require.DirExists(t, r.p("torrents/tv"))
}

func TestProxyFolderCleanupSetAutoManagement(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "movies"))

	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetAutoManagement, "torrents/setAutoManagement", "hashes="+proxyHashA+"&enable=true").Code)
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.FileExists(t, r.p("torrents/movies/Show.S01E01.mkv"))
}

func TestProxyFolderCleanupDisableAutoManagementIsNotATrigger(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "movies"))

	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetAutoManagement, "torrents/setAutoManagement", "hashes="+proxyHashA+"&enable=false").Code)
	r.stub.DeleteElsewhere(proxyHashA)
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

func TestProxyFolderCleanupEditCategory(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.stub.AddCategory("archive", r.p("torrents/old/archive"))
	r.add(qbtstub.Torrent{Hash: proxyHashA, Name: "A", SavePath: r.p("torrents/old/archive"), Category: "archive", AutoTMM: true,
		Layout: qbtstub.NoRootFolder, Files: []string{"A/a.mkv"}})

	body := url.Values{"category": {"archive"}, "savePath": {r.p("torrents/new")}}.Encode()
	require.Equal(t, http.StatusOK, r.post(r.handler.handleEditCategory, "torrents/editCategory", body).Code)
	r.requireGone("torrents/old")
	require.FileExists(t, r.p("torrents/new/A/a.mkv"))
}

func TestProxyFolderCleanupSetSavePath(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))

	body := url.Values{"id": {proxyHashA}, "path": {r.p("torrents/movies")}}.Encode()
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetSavePath, "torrents/setSavePath", body).Code)
	r.requireGone("torrents/tv/Show.S01E01-GRP")
	require.FileExists(t, r.p("torrents/movies/Show.S01E01.mkv"))
}

func TestProxyFolderCleanupSetDownloadPath(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	torrent := r.perRelease(proxyHashA, "tv")
	torrent.DownloadPath = r.p("torrents/incomplete/Show.S01E01-GRP")
	torrent.Incomplete = true
	r.add(torrent)

	body := url.Values{"id": {proxyHashA}, "path": {r.p("torrents/incomplete/tv")}}.Encode()
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetDownloadPath, "torrents/setDownloadPath", body).Code)
	r.requireGone("torrents/incomplete/Show.S01E01-GRP")
	require.DirExists(t, r.p("torrents/incomplete"))
	require.FileExists(t, r.p("torrents/incomplete/tv/Show.S01E01.mkv"))
}

func TestProxyFolderCleanupSetDownloadPathFromSavePath(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(qbtstub.Torrent{Hash: proxyHashA, Name: "Show", SavePath: r.p("torrents/tv"), Category: "tv", Incomplete: true,
		Layout: qbtstub.NoRootFolder, Files: []string{"Show/Show.S01E01.mkv"}})

	body := url.Values{"id": {proxyHashA}, "path": {r.p("torrents/incomplete/tv")}}.Encode()
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetDownloadPath, "torrents/setDownloadPath", body).Code)
	r.requireGone("torrents/tv/Show")
	require.DirExists(t, r.p("torrents/tv"))
	require.FileExists(t, r.p("torrents/incomplete/tv/Show/Show.S01E01.mkv"))
}

func TestProxyFolderCleanupRejectedRequest(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))
	r.stub.Reject = http.StatusConflict

	require.Equal(t, http.StatusConflict, r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=true").Code)
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

func TestProxyFolderCleanupUnreachableUpstream(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))
	closed := httptest.NewServer(http.NotFoundHandler())
	r.upstream, _ = url.Parse(closed.URL)
	closed.Close()

	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=true")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

func TestProxyFolderCleanupResponseWithoutStatus(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))
	closed := httptest.NewServer(http.NotFoundHandler())
	r.upstream, _ = url.Parse(closed.URL)
	closed.Close()
	r.handler.proxy.ErrorHandler = func(http.ResponseWriter, *http.Request, error) {}

	r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=true")
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

func TestProxyFolderCleanupForwardsAnUnparsableBody(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))

	body := "hashes=" + proxyHashA + "&category=movies&bad=%zz"
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetCategory, "torrents/setCategory", body).Code)
	require.Equal(t, body, r.stub.LastBody("torrents/setCategory"))
}
