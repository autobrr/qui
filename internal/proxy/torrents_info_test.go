// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTorrentsInfoFiltersLikeQBittorrent(t *testing.T) {
	t.Parallel()

	mainData := `{"rid":1,"full_update":true,"server_state":{"use_subcategories":true},
		"categories":{"movies":{"name":"movies"},"movies/extra":{"name":"movies/extra"}},
		"torrents":{
			"aa":{"name":"A","state":"stalledUP","category":"movies","tags":"keep","progress":1},
			"bb":{"name":"B","state":"downloading","category":"","tags":"","progress":0.5},
			"cc":{"name":"C","state":"stalledUP","category":"movies/extra","tags":"","progress":1}}}`
	handler, instanceID, _ := newInfoTestHandler(t, mainData)

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
			ctx := context.WithValue(t.Context(), ClientAPIKeyContextKey, &models.ClientAPIKey{ClientName: "sonarr", InstanceID: instanceID})
			ctx = context.WithValue(ctx, InstanceIDContextKey, instanceID)
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

func newInfoTestHandler(t *testing.T, mainData string) (*Handler, int, string) {
	t.Helper()
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
	keys := models.NewClientAPIKeyStore(db)
	key, _, err := keys.Create(t.Context(), "sonarr", instance.ID)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	client, err := pool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	return NewHandler(pool, keys, instanceStore, qbittorrent.NewSyncManager(pool, nil), nil, nil, "/"), instance.ID, key
}

func TestProxyListsKeepTheAnnounceURL(t *testing.T) {
	t.Parallel()

	const announce = "https://tracker.example.invalid/abc123xyz/announce"
	handler, instanceID, _ := newInfoTestHandler(t, `{"rid":1,"full_update":true,
		"torrents":{"aa":{"name":"A","state":"stalledUP","progress":1,"tracker":"`+announce+`"}}}`)
	request := func(path string) *http.Request {
		ctx := context.WithValue(t.Context(), ClientAPIKeyContextKey, &models.ClientAPIKey{ClientName: "sonarr", InstanceID: instanceID})
		ctx = context.WithValue(ctx, InstanceIDContextKey, instanceID)
		return httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	}
	type row struct {
		Tracker string `json:"tracker"`
	}

	t.Run("torrents/info", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		handler.handleTorrentsInfo(rec, request("/api/v2/torrents/info"))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var rows []row
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
		require.Equal(t, []row{{Tracker: announce}}, rows)
	})

	t.Run("torrents/search with an expression on the passkey", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		handler.handleTorrentSearch(rec, request(`/api/v2/torrents/search?expr=`+url.QueryEscape(`Tracker contains "abc123xyz"`)))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var response struct {
			Torrents []row `json:"torrents"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, []row{{Tracker: announce}}, response.Torrents)
	})
}

func TestTorrentSearchWithoutLimitReturnsAllTorrents(t *testing.T) {
	t.Parallel()

	const total = 100_001
	var b strings.Builder
	b.Grow(total * 56)
	b.WriteString(`{"rid":1,"full_update":true,"torrents":{`)
	for i := range total {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"%06x":{"name":"t-%d","state":"stalledUP","progress":1}`, i, i)
	}
	b.WriteString(`}}`)

	handler, _, key := newInfoTestHandler(t, b.String())
	router := chi.NewRouter()
	handler.Routes(router)
	search := func(t *testing.T, query string) (response struct {
		Torrents []struct{} `json:"torrents"`
		Total    int        `json:"total"`
		HasMore  bool       `json:"hasMore"`
	}) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/proxy/"+key+"/api/v2/torrents/search"+query, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		return response
	}

	t.Run("omitted limit returns every matching torrent", func(t *testing.T) {
		t.Parallel()
		response := search(t, "")
		require.Len(t, response.Torrents, total)
		require.Equal(t, total, response.Total)
		require.False(t, response.HasMore)
	})

	t.Run("explicit limit still paginates", func(t *testing.T) {
		t.Parallel()
		response := search(t, "?limit=25")
		require.Len(t, response.Torrents, 25)
		require.Equal(t, total, response.Total)
		require.True(t, response.HasMore)
	})
}
