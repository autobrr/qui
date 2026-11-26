// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/reannounce"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const stalledHash = "5ca1ab1e5ca1ab1e5ca1ab1e5ca1ab1e5ca1ab1e"

func TestProxyReannounceOfAMonitoredStalledTorrentQueuesAQuiJob(t *testing.T) {
	t.Parallel()
	sent := strings.ToUpper(stalledHash)
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.11.4"))
		case "/api/v2/sync/maindata":
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Show.S01E01.1080p.WEB-GRP","state":"stalledUP","added_on":1700000000}}}`, stalledHash)
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte("[]"))
		case "/api/v2/torrents/reannounce":
			forwarded.Add(1)
			_, _ = w.Write([]byte("forwarded"))
		default:
			http.Error(w, "unavailable", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(upstream.Close)

	db := testdb.NewMigratedSQLite(t, "proxy-reannounce")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", upstream.URL, "", "", nil, nil, false, nil)
	require.NoError(t, err)
	keys := models.NewClientAPIKeyStore(db)
	key, _, err := keys.Create(t.Context(), "autobrr", instance.ID)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	syncManager := qbittorrent.NewSyncManager(pool, nil)

	settingsStore := models.NewInstanceReannounceStore(db)
	cache := reannounce.NewSettingsCache(settingsStore)
	cache.Replace(&models.InstanceReannounceSettings{InstanceID: instance.ID, Enabled: true, MonitorAll: true, MaxRetries: 1, ReannounceIntervalSeconds: 1})
	svc := reannounce.NewService(reannounce.Config{ScanInterval: time.Hour}, instanceStore, settingsStore, cache, pool, syncManager)
	svc.Start(t.Context())

	handler := NewHandler(pool, keys, instanceStore, syncManager, cache, svc, "/")
	router := chi.NewRouter()
	handler.Routes(router)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/proxy/"+key+"/api/v2/torrents/reannounce", strings.NewReader("hashes="+sent))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, proxyLoginSuccessBody, rec.Body.String())
	require.Zero(t, forwarded.Load())
}
