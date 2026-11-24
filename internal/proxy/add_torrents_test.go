// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type fakeAddNoter struct {
	adds []int
}

func (f *fakeAddNoter) NoteProxiedAdd(instanceID int) {
	f.adds = append(f.adds, instanceID)
}

const addMagnet = "urls=magnet%3A%3Fxt%3Durn%3Abtih%3A" + proxyHashB

type noterRig struct {
	handler  *Handler
	router   chi.Router
	pool     *qbittorrent.ClientPool
	instance *models.Instance
	key      string
}

func newNoterRig(t *testing.T, qbURL string) *noterRig {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "proxy-noter")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "test", qbURL, "", "", nil, nil, false, nil)
	require.NoError(t, err)
	keys := models.NewClientAPIKeyStore(db)
	key, _, err := keys.Create(t.Context(), "autobrr", instance.ID)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 10*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	handler := NewHandler(pool, keys, instanceStore, qbittorrent.NewSyncManager(pool, nil), nil, nil, "/")
	router := chi.NewRouter()
	handler.Routes(router)
	return &noterRig{handler: handler, router: router, pool: pool, instance: instance, key: key}
}

func (r *noterRig) send(t *testing.T, method, endpoint, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, "/proxy/"+r.key+"/api/v2/"+endpoint, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

func TestProxyNotesOnlyAcceptedAdds(t *testing.T) {
	t.Parallel()

	stub := qbtstub.New(t, t.TempDir())
	stubURL, err := url.Parse(stub.URL)
	require.NoError(t, err)
	forward := httputil.NewSingleHostReverseProxy(stubURL)
	var status int
	var answer string
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/torrents/add" {
			forward.ServeHTTP(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(qb.Close)

	rig := newNoterRig(t, qb.URL)
	noter := &fakeAddNoter{}
	rig.handler.addNoter = noter
	for name, tc := range map[string]struct {
		status   int
		answer   string
		accepted bool
	}{
		"text reject":  {http.StatusOK, "Fails.", false},
		"JSON pending": {http.StatusAccepted, `{"success_count":0,"failure_count":0,"pending_count":1}`, true},
		"JSON reject":  {http.StatusConflict, `{"success_count":0,"failure_count":1,"pending_count":0}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			noter.adds = nil
			status, answer = tc.status, tc.answer

			got := rig.send(t, http.MethodPost, "torrents/add", addMagnet)

			require.Equal(t, tc.status, got.Code)
			require.Equal(t, tc.answer, got.Body.String())
			if tc.accepted {
				require.Equal(t, []int{rig.instance.ID}, noter.adds)
			} else {
				require.Empty(t, noter.adds)
			}
		})
	}

	noter.adds = nil
	status, answer = http.StatusOK, "Ok."
	rig.send(t, http.MethodPost, "torrents/recheck", "")
	rig.send(t, http.MethodPost, "torrents/setLocation", "")
	rig.send(t, http.MethodPost, "torrents/delete", "")
	rig.send(t, http.MethodGet, "torrents/info", "")
	require.Empty(t, noter.adds, "no other request notes an add")
}
