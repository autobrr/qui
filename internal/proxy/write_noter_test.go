// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type fakeWriteNoter struct {
	mu     sync.Mutex
	writes []int
}

func (f *fakeWriteNoter) NoteWrite(instanceID int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, instanceID)
}

func (f *fakeWriteNoter) taken() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	writes := f.writes
	f.writes = nil
	return writes
}

type proxiedDelete struct {
	instanceID int
	hashes     []string
	keyName    string
	// deletesSent is the number of delete requests that qBittorrent got
	// before the note.
	deletesSent int
}

type fakeDeleteNoter struct {
	stub *qbtstub.Server
	mu   sync.Mutex
	seen []proxiedDelete
}

func (f *fakeDeleteNoter) NoteProxiedDelete(_ context.Context, instanceID int, hashes []string, keyName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, proxiedDelete{instanceID, hashes, keyName, f.stub.Writes("torrents/delete")})
}

func TestProxyNotesTorrentWrites(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stub := qbtstub.New(t, filepath.Join(root, "torrents"))
	stub.Add(qbtstub.Torrent{Hash: proxyHashA, Name: "Show.S01E01-GRP", SavePath: filepath.Join(root, "torrents", "tv"),
		Layout: qbtstub.NoRootFolder, Files: []string{"Show.S01E01.mkv"}})

	db := testdb.NewMigratedSQLite(t, "proxy-write-noter")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", stub.URL, "", "", nil, nil, false, nil)
	require.NoError(t, err)
	rawKey, _, err := models.NewClientAPIKeyStore(db).Create(t.Context(), "autobrr", instance.ID)
	require.NoError(t, err)

	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	client, err := pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))

	handler := NewHandler(pool, models.NewClientAPIKeyStore(db), instanceStore, qbittorrent.NewSyncManager(pool, nil), nil, nil, "/")
	noter := &fakeWriteNoter{}
	handler.writeNoter = noter
	deletes := &fakeDeleteNoter{stub: stub}
	handler.deleteNoter = deletes
	router := chi.NewRouter()
	handler.Routes(router)

	send := func(method, endpoint, body string) {
		req := httptest.NewRequestWithContext(t.Context(), method, "/proxy/"+rawKey+"/api/v2/"+endpoint, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	send(http.MethodPost, "torrents/add", "urls=magnet%3A%3Fxt%3Durn%3Abtih%3A"+proxyHashB)
	require.Equal(t, 1, stub.Writes("torrents/add"), "the proxy forwards the add")
	require.Equal(t, []int{instance.ID}, noter.taken(), "a forwarded POST is a write")

	send(http.MethodPost, "torrents/setLocation", "hashes="+proxyHashA+"&location="+filepath.ToSlash(filepath.Join(root, "torrents", "movies")))
	require.Equal(t, 1, stub.Writes("torrents/setLocation"), "the proxy sends the move")
	require.Equal(t, []int{instance.ID}, noter.taken(), "an intercepted POST is a write")

	send(http.MethodGet, "torrents/info", "")
	send(http.MethodGet, "torrents/properties?hash="+proxyHashA, "")
	require.Empty(t, noter.taken(), "a GET is no write")

	send(http.MethodPost, "torrents/delete", "hashes="+proxyHashA+"|"+proxyHashB+"&deleteFiles=false")
	require.Equal(t, 1, stub.Writes("torrents/delete"), "the proxy forwards the delete")
	require.Equal(t, []proxiedDelete{{instance.ID, []string{proxyHashA, proxyHashB}, "autobrr", 0}}, deletes.seen,
		"the delete note names the Client Proxy API key, before qBittorrent gets the delete")

	// qBittorrent also reads a multipart body, and keeps the last value of a field.
	multipartBody := "--rig\r\nContent-Disposition: form-data; name=\"hashes\"\r\n\r\n" + proxyHashA + "\r\n" +
		"--rig\r\nContent-Disposition: form-data; name=\"hashes\"\r\n\r\n" + proxyHashB + "\r\n" +
		"--rig\r\nContent-Disposition: form-data; name=\"deleteFiles\"\r\n\r\nfalse\r\n--rig--\r\n"
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/proxy/"+rawKey+"/api/v2/torrents/delete", strings.NewReader(multipartBody))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=rig")
	router.ServeHTTP(httptest.NewRecorder(), req)
	require.Len(t, deletes.seen, 2, "a multipart delete gets a delete note")
	require.Equal(t, []string{proxyHashB}, deletes.seen[1].hashes)
	require.Equal(t, "autobrr", deletes.seen[1].keyName)
}
