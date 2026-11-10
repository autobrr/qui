// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// regression: #3868
// TestTorrentsInfoFiltersLikeQBittorrent sends torrents/info requests through
// the proxy handler to a fake qBittorrent 5.1 with subcategories on.
func TestTorrentsInfoFiltersLikeQBittorrent(t *testing.T) {
	t.Parallel()

	mainData := `{"rid":1,"full_update":true,"server_state":{"use_subcategories":true},
		"categories":{"movies":{"name":"movies"},"movies/extra":{"name":"movies/extra"}},
		"torrents":{
			"aa":{"name":"A","state":"stalledUP","category":"movies","tags":"keep","progress":1},
			"bb":{"name":"B","state":"downloading","category":"","tags":"","progress":0.5},
			"cc":{"name":"C","state":"stalledUP","category":"movies/extra","tags":"","progress":1}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = io.WriteString(w, mainData)
		case "/api/v2/torrents/categories":
			_, _ = io.WriteString(w, `{"movies":{"name":"movies"},"movies/extra":{"name":"movies/extra"}}`)
		case "/api/v2/torrents/tags":
			_, _ = io.WriteString(w, `["keep"]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	db := testdb.NewMigratedSQLite(t, "proxy-torrents-info")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", srv.URL, "", "", nil, nil, false, nil)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	client, err := pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	handler := NewHandler(pool, nil, instanceStore, qbittorrent.NewSyncManager(pool, nil), nil, nil, "/")

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"filter=stalledup", []string{"aa", "cc"}},
		{"filter=stalledup&category=movies", []string{"aa", "cc"}},
		{"filter=seeding", []string{"aa", "cc"}},
		{"category=movies", []string{"aa", "cc"}},
		{"category=", []string{"bb"}},
		{"tag=", []string{"bb", "cc"}},
		{"tag=keep", []string{"aa"}},
		{"filter=tpyo", []string{}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			ctx := context.WithValue(t.Context(), ClientAPIKeyContextKey, &models.ClientAPIKey{ClientName: "sonarr", InstanceID: instance.ID})
			ctx = context.WithValue(ctx, InstanceIDContextKey, instance.ID)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v2/torrents/info?"+tc.query, nil)
			rec := httptest.NewRecorder()
			handler.handleTorrentsInfo(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var torrents []struct {
				Hash string `json:"hash"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &torrents))
			got := make([]string, 0, len(torrents))
			for _, torrent := range torrents {
				got = append(got, torrent.Hash)
			}
			slices.Sort(got)
			require.Equal(t, tc.want, got)
		})
	}
}
