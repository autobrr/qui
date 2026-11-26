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

const caseEditHash = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"

type caseEditRig struct {
	sm         *SyncManager
	instanceID int
	mu         sync.Mutex
	writes     map[string]string
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
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Show.S01E01.1080p.WEB-GRP","category":"tv","state":"stalledUP"}},"categories":{"tv":{"name":"tv"},"movies":{"name":"movies"}}}`, caseEditHash)
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"tv":{"name":"tv"},"movies":{"name":"movies"}}`))
		case "/api/v2/torrents/export":
			_, _ = w.Write([]byte("d4:infod4:name4:showee"))
		case "/api/v2/torrents/addTags", "/api/v2/torrents/setCategory":
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

func TestEditsByAnUppercaseHashSucceed(t *testing.T) {
	t.Parallel()
	upper := strings.ToUpper(caseEditHash)

	t.Run("add tags", func(t *testing.T) {
		t.Parallel()
		rig := newCaseEditRig(t)
		require.NoError(t, rig.sm.AddTags(t.Context(), rig.instanceID, []string{upper}, "keep"))
		require.True(t, strings.EqualFold(caseEditHash, rig.sent("/api/v2/torrents/addTags")), rig.sent("/api/v2/torrents/addTags"))
	})

	t.Run("set category", func(t *testing.T) {
		t.Parallel()
		rig := newCaseEditRig(t)
		require.NoError(t, rig.sm.SetCategory(t.Context(), rig.instanceID, []string{upper}, "movies"))
		require.True(t, strings.EqualFold(caseEditHash, rig.sent("/api/v2/torrents/setCategory")), rig.sent("/api/v2/torrents/setCategory"))
	})
}

func TestExportByAnUppercaseHashNamesTheFileAfterTheTorrent(t *testing.T) {
	t.Parallel()
	rig := newCaseEditRig(t)

	data, name, _, err := rig.sm.ExportTorrent(t.Context(), rig.instanceID, strings.ToUpper(caseEditHash))

	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.Equal(t, "Show.S01E01.1080p.WEB-GRP", name)
}
