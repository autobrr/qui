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

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// rigTick replaces the 3 s production tick. It stays well above the 200 ms
// debounced sync after a write: a hint on every tick closer than that would
// keep postponing the sync the worker waits on.
const rigTick = 500 * time.Millisecond

// rigKeptWait outlasts two full ticks plus the debounced sync, so a folder
// removed by mistake is gone by the time requireKept looks, even when the
// worker first waits a tick for the sync to drop the torrent's row. In the
// rig a removal lands about 0.5 s after the request.
const rigKeptWait = 2*rigTick + 500*time.Millisecond

const proxyHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const proxyHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type noIgnores struct{}

func (noIgnores) IgnoredPathMatcher(context.Context, int, fsops.PathDialect) (func(string) bool, error) {
	return func(string) bool { return false }, nil
}

// proxyRig is a proxy handler in front of a qBittorrent stub that deletes and
// moves files in a temp dir, with qui's real sync manager and folder cleanup.
type proxyRig struct {
	t          *testing.T
	root       string
	stub       *qbtstub.Server
	handler    *Handler
	instanceID int
	client     *qbittorrent.Client
	upstream   *url.URL
}

func newProxyRig(t *testing.T) *proxyRig {
	root := t.TempDir()
	r := &proxyRig{t: t, root: root, stub: qbtstub.New(t, filepath.Join(root, "torrents"))}
	r.stub.AddCategory("tv", "")
	r.stub.AddCategory("movies", "")
	for _, dir := range []string{"torrents/incomplete", "torrents/tv", "torrents/movies"} {
		require.NoError(t, os.MkdirAll(r.p(dir), 0o755))
	}

	db := testdb.NewMigratedSQLite(t, "proxy-folder-cleanup")
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
	fc.Tick = rigTick
	fc.Start(t.Context())
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
	require.NoError(r.t, r.client.GetSyncManager().Sync(r.t.Context()))
}

// post sends a form to an intercepted endpoint the way a proxy client does.
func (r *proxyRig) post(handle http.HandlerFunc, endpoint, body string) *httptest.ResponseRecorder {
	return r.postAs(handle, endpoint, "application/x-www-form-urlencoded", body)
}

// postAs sends body with contentType; endpoint may carry a query string.
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
	}, 10*time.Second, 50*time.Millisecond, "%s must be removed", rel)
}

// requireKept waits out a tick and the sync, then checks rel is still there.
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

	// The stub writes no status, as qBittorrent does for "Ok.": the proxy
	// still relays an explicit 200.
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

// qBittorrent answers 200 to a delete naming a hash it does not accept, and
// deletes nothing. The torrent stays, so its folders, here holding nothing but
// junk files, must too.
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

// qBittorrent 5 reads only a POST's body, urlencoded or multipart, and keeps
// the last value of a repeated key. The proxy must snapshot a delete with files
// only when qBittorrent reads it that way, or a delete that keeps the files
// loses the torrent's junk files and folders.
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
				// qBittorrent removes Rel-GRP; only folder cleanup removes Show.
				r.requireGone("torrents/tv/Show")
				return
			}
			r.requireKept("torrents/tv/Show/Rel-GRP/Sub")
			require.FileExists(t, r.p("torrents/tv/Show/Rel-GRP/notes.txt~"), "qBittorrent kept the files")
		})
	}
}

// qBittorrent reads a body as urlencoded when the lowercased Content-Type
// starts with the urlencoded type, so a malformed parameter after it does not
// matter. Any other body gets no snapshot.
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
			// Empty the folder whether or not the stub moved the files: had the
			// request been snapshotted, it would now be removed.
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
	// Had the delete queued work, an emptied folder would now be removed.
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
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

// Sonarr's post-import category change on an ATM torrent.
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

// Turning ATM off never moves a torrent, so it queues nothing. The snapshot
// already skips a torrent in ATM for the ATM kind, so only a torrent with ATM
// off, as in a mixed selection, could show a wrongly queued item. Another
// client then deletes it with its files: had the request queued it, its
// emptied folder would be removed, though qui deleted nothing.
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
	r.add(torrent)

	body := url.Values{"id": {proxyHashA}, "path": {r.p("torrents/incomplete/tv")}}.Encode()
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetDownloadPath, "torrents/setDownloadPath", body).Code)
	r.requireGone("torrents/incomplete/Show.S01E01-GRP")
	require.DirExists(t, r.p("torrents/incomplete"))
	require.FileExists(t, r.p("torrents/incomplete/tv/Show.S01E01.mkv"))
}

// A rejected delete: a rejected move would also be held by its unmoved row,
// so only a delete shows that nothing was queued.
func TestProxyFolderCleanupRejectedRequest(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))
	r.stub.Reject = http.StatusConflict

	require.Equal(t, http.StatusConflict, r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=true").Code)
	// Had the request queued work, the emptied folder would now be removed.
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

// A transport error is answered by the proxy's error handler, not qBittorrent.
func TestProxyFolderCleanupUnreachableUpstream(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))
	closed := httptest.NewServer(http.NotFoundHandler())
	r.upstream, _ = url.Parse(closed.URL)
	closed.Close()

	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=true")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	// Had the request queued work, the emptied folder would now be removed.
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

// A response that never wrote a status cannot read as accepted.
func TestProxyFolderCleanupResponseWithoutStatus(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))
	closed := httptest.NewServer(http.NotFoundHandler())
	r.upstream, _ = url.Parse(closed.URL)
	closed.Close()
	r.handler.proxy.ErrorHandler = func(http.ResponseWriter, *http.Request, error) {}

	r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=true")
	// Had the delete queued work, the emptied folder would now be removed.
	require.NoError(t, os.Remove(r.p("torrents/tv/Show.S01E01-GRP/Show.S01E01.mkv")))
	r.requireKept("torrents/tv/Show.S01E01-GRP")
}

// A body that does not parse is forwarded byte for byte, without a snapshot.
func TestProxyFolderCleanupForwardsAnUnparsableBody(t *testing.T) {
	t.Parallel()
	r := newProxyRig(t)
	r.add(r.perRelease(proxyHashA, "tv"))

	body := "hashes=" + proxyHashA + "&category=movies&bad=%zz"
	require.Equal(t, http.StatusOK, r.post(r.handler.handleSetCategory, "torrents/setCategory", body).Code)
	require.Equal(t, body, r.stub.LastBody("torrents/setCategory"))
}
