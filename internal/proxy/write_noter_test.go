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

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/qbtstub"
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
	instanceID  int
	hashes      []string
	keyName     string
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

	rig := newNoterRig(t, stub.URL)
	instance := rig.instance
	client, err := rig.pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))

	noter := &fakeWriteNoter{}
	rig.handler.writeNoter = noter
	deletes := &fakeDeleteNoter{stub: stub}
	rig.handler.deleteNoter = deletes
	rig.send(t, http.MethodPost, "torrents/add", addMagnet)
	require.Equal(t, 1, stub.Writes("torrents/add"), "the proxy forwards the add")
	require.Equal(t, []int{instance.ID}, noter.taken(), "a forwarded POST is a write")

	rig.send(t, http.MethodPost, "torrents/setLocation", "hashes="+proxyHashA+"&location="+filepath.ToSlash(filepath.Join(root, "torrents", "movies")))
	require.Equal(t, 1, stub.Writes("torrents/setLocation"), "the proxy sends the move")
	require.Equal(t, []int{instance.ID}, noter.taken(), "an intercepted POST is a write")

	rig.send(t, http.MethodGet, "torrents/info", "")
	rig.send(t, http.MethodGet, "torrents/properties?hash="+proxyHashA, "")
	require.Empty(t, noter.taken(), "a GET is no write")

	rig.send(t, http.MethodPost, "torrents/delete", "hashes="+proxyHashA+"|"+proxyHashB+"&deleteFiles=false")
	require.Equal(t, 1, stub.Writes("torrents/delete"), "the proxy forwards the delete")
	require.Equal(t, []proxiedDelete{{instance.ID, []string{proxyHashA, proxyHashB}, "autobrr", 0}}, deletes.seen,
		"the delete note names the Client Proxy API key, before qBittorrent gets the delete")

	multipartBody := "--rig\r\nContent-Disposition: form-data; name=\"hashes\"\r\n\r\n" + proxyHashA + "\r\n" +
		"--rig\r\nContent-Disposition: form-data; name=\"hashes\"\r\n\r\n" + proxyHashB + "\r\n" +
		"--rig\r\nContent-Disposition: form-data; name=\"deleteFiles\"\r\n\r\nfalse\r\n--rig--\r\n"
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/proxy/"+rig.key+"/api/v2/torrents/delete", strings.NewReader(multipartBody))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=rig")
	rig.router.ServeHTTP(httptest.NewRecorder(), req)
	require.Len(t, deletes.seen, 2, "a multipart delete gets a delete note")
	require.Equal(t, []string{proxyHashB}, deletes.seen[1].hashes)
	require.Equal(t, "autobrr", deletes.seen[1].keyName)
}
