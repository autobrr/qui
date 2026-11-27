// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

const (
	caseEditHash   = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"
	caseEditV2Only = "d00dfeedd00dfeedd00dfeedd00dfeedd00dfeedd00dfeedd00dfeedd00dfeed"
	caseEditHybrid = "beefcafebeefcafebeefcafebeefcafebeefcafebeefcafebeefcafebeefcafe"
	caseEditV1     = "facade00facade00facade00facade00facade00"
)

type caseEditRig struct {
	sm         *SyncManager
	instanceID int
	mu         sync.Mutex
	writes     map[string]string
	syncs      int
}

func (r *caseEditRig) sent(endpoint string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writes[endpoint]
}

func newCaseEditRig(t *testing.T) *caseEditRig {
	t.Helper()
	rig := &caseEditRig{writes: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.4"))
		case "/api/v2/sync/maindata":
			rig.mu.Lock()
			rig.syncs++
			rig.mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Show.S01E01.1080p.WEB-GRP","category":"tv","state":"stalledUP"},%q:{"name":"Show.S01E02.1080p.WEB-GRP","infohash_v2":%q,"category":"tv","state":"stalledUP"},%q:{"name":"Show.S01E03.1080p.WEB-GRP","infohash_v1":%q,"infohash_v2":%q,"category":"tv","state":"stalledUP"}},"categories":{"tv":{"name":"tv"},"movies":{"name":"movies"}}}`,
				caseEditHash, caseEditV2Only[:40], caseEditV2Only, caseEditHybrid[:40], caseEditV1, caseEditHybrid)
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"tv":{"name":"tv"},"movies":{"name":"movies"}}`))
		case "/api/v2/torrents/export":
			rig.mu.Lock()
			rig.writes[r.URL.Path] = r.FormValue("hash")
			rig.mu.Unlock()
			_, _ = w.Write([]byte("d4:infod4:name4:showee"))
		case "/api/v2/torrents/setCategory", "/api/v2/torrents/stop":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rig.mu.Lock()
			rig.writes[r.URL.Path] = r.Form.Get("hashes")
			rig.mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	pool := setupTestPool(t)
	t.Cleanup(func() { _ = pool.Close() })
	inst, err := pool.instanceStore.Create(t.Context(), "mock", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	qbtClient := qbt.NewClient(qbt.Config{Host: srv.URL, Timeout: 60})
	client := &Client{
		Client:            qbtClient,
		instanceID:        inst.ID,
		syncManager:       syncowner.New(qbtClient, qbt.DefaultSyncOptions()),
		optimisticUpdates: ttlcache.New[string, *OptimisticTorrentUpdate](),
	}
	client.updateHealthStatus(true)
	require.NoError(t, client.syncManager.Sync(t.Context()))
	pool.mu.Lock()
	pool.clients[inst.ID] = client
	pool.mu.Unlock()

	rig.sm = NewSyncManager(pool, nil)
	rig.instanceID = inst.ID
	return rig
}

func TestAnEditByAnotherHashOfATorrentSendsItsTorrentID(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		sent string
		want string
	}{
		"the uppercase v2 of a v2-only torrent":  {sent: strings.ToUpper(caseEditV2Only), want: caseEditV2Only[:40]},
		"the v1 of a hybrid stored under its v2": {sent: caseEditV1, want: caseEditHybrid[:40]},
		"an uppercase torrent ID":                {sent: strings.ToUpper(caseEditHash), want: caseEditHash},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rig := newCaseEditRig(t)
			require.NoError(t, rig.sm.SetCategory(t.Context(), rig.instanceID, []string{tc.sent}, "movies"))
			require.Equal(t, tc.want, rig.sent("/api/v2/torrents/setCategory"))
		})
	}
}

func TestExportByAnotherHashOfATorrentSendsItsTorrentIDAndNamesTheFileAfterIt(t *testing.T) {
	t.Parallel()
	unknown := strings.Repeat("ab", 20)
	for name, tc := range map[string]struct {
		sent     string
		wantID   string
		wantName string
	}{
		"the uppercase v2 of a v2-only torrent": {sent: strings.ToUpper(caseEditV2Only), wantID: caseEditV2Only[:40], wantName: "Show.S01E02.1080p.WEB-GRP"},
		"a hash qBittorrent does not list":      {sent: unknown, wantID: unknown, wantName: unknown},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rig := newCaseEditRig(t)

			data, file, _, err := rig.sm.ExportTorrent(t.Context(), rig.instanceID, tc.sent)

			require.NoError(t, err)
			require.NotEmpty(t, data)
			require.Equal(t, tc.wantName, file)
			require.Equal(t, tc.wantID, rig.sent("/api/v2/torrents/export"))
		})
	}
}

func TestABulkActionOnListedHashesRunsNoSyncRetry(t *testing.T) {
	t.Parallel()
	rig := newCaseEditRig(t)

	require.NoError(t, rig.sm.BulkAction(t.Context(), rig.instanceID, []string{caseEditHash, strings.ToUpper(caseEditV2Only)}, "pause"))

	require.Equal(t, caseEditHash+"|"+caseEditV2Only[:40], rig.sent("/api/v2/torrents/stop"))
	rig.mu.Lock()
	defer rig.mu.Unlock()
	require.Equal(t, 1, rig.syncs)
}
