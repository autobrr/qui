// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
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
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestGetInstanceCapabilitiesPreservesBackoffMessage(t *testing.T) {
	qbtServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer qbtServer.Close()

	db := testdb.NewMigratedSQLite(t, "instance-capabilities-backoff")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := internalqbittorrent.NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err)
	defer clientPool.Close()

	instance, err := instanceStore.Create(
		context.Background(),
		"blocked",
		qbtServer.URL,
		"admin",
		"password",
		nil,
		nil,
		false,
		nil,
	)
	require.NoError(t, err)

	handler := NewInstancesHandler(instanceStore, nil, nil, clientPool, nil, nil, nil, nil)
	t.Cleanup(handler.connectionTests.Wait)
	router := chi.NewRouter()
	router.Get("/api/instances/{instanceID}/capabilities", handler.GetInstanceCapabilities)

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/instances/%d/capabilities", instance.ID), nil))
	require.Equal(t, http.StatusServiceUnavailable, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/instances/%d/capabilities", instance.ID), nil))
	require.Equal(t, http.StatusServiceUnavailable, second.Code)

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &body))
	require.Contains(t, body.Error, "health-check backoff")
	require.Contains(t, body.Error, "retrying in")
	require.NotEqual(t, "Failed to load instance capabilities", body.Error)
}

func TestInstanceCapabilitiesClientErrorMessageFallsBack(t *testing.T) {
	require.Equal(t, "Failed to load instance capabilities", instanceCapabilitiesClientErrorMessage(errors.New("boom")))
}

func TestUpdateInstanceClearsConnectionBackoff(t *testing.T) {
	failingServer := func() (*httptest.Server, *atomic.Int32) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)
		return srv, &hits
	}
	oldServer, _ := failingServer()
	stillFailingServer, stillFailingHits := failingServer()
	workingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.15.1"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(workingServer.Close)

	db := testdb.NewMigratedSQLite(t, "instance-update-backoff")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := internalqbittorrent.NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	instance, err := instanceStore.Create(t.Context(), "instance", oldServer.URL, "admin", "password", nil, nil, false, nil)
	require.NoError(t, err)

	handler := NewInstancesHandler(instanceStore, nil, nil, clientPool, nil, nil, nil, nil)
	t.Cleanup(handler.connectionTests.Wait)
	router := chi.NewRouter()
	router.Put("/api/instances/{instanceID}", handler.UpdateInstance)
	update := func(host string) {
		t.Helper()
		body := fmt.Sprintf(`{"name":"instance","host":%q,"username":"admin"}`, host)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPut, fmt.Sprintf("/api/instances/%d", instance.ID), strings.NewReader(body)))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		handler.connectionTests.Wait()
	}
	backoff := func(err error) time.Duration {
		t.Helper()
		blocker, ok := errors.AsType[*internalqbittorrent.InstanceHealthBlockerError](err)
		require.True(t, ok, "want a health blocker, got %v", err)
		require.Equal(t, internalqbittorrent.InstanceHealthBlockerBackoff, blocker.Kind)
		return blocker.RetryAfter
	}

	_, err = clientPool.GetClient(t.Context(), instance.ID)
	require.Error(t, err)
	_, err = clientPool.GetClient(t.Context(), instance.ID)
	oldBackoff := backoff(err)

	update(stillFailingServer.URL)
	_, err = clientPool.GetClient(t.Context(), instance.ID)
	require.Error(t, err)
	require.Positive(t, stillFailingHits.Load(), "the new host is tried at once")
	_, err = clientPool.GetClient(t.Context(), instance.ID)
	require.LessOrEqual(t, backoff(err), oldBackoff+time.Second)

	update(workingServer.URL)
	_, err = clientPool.GetClient(t.Context(), instance.ID)
	require.NoError(t, err)
	errs, err := errorStore.GetRecentErrors(t.Context(), instance.ID, 10)
	require.NoError(t, err)
	require.Empty(t, errs)
}

func TestUpdateInstanceKeepsThePublicPeerAddress(t *testing.T) {
	stub := qbtstub.New(t, t.TempDir())
	stub.ListenPort = 51413
	stub.SetExternalAddress("203.0.113.7", "")

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	clientPool, err := internalqbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	instance, err := instanceStore.Create(t.Context(), "seedbox", stub.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)

	handler := NewInstancesHandler(instanceStore, nil, nil, clientPool, internalqbittorrent.NewSyncManager(clientPool, nil), nil, nil, nil)
	t.Cleanup(handler.connectionTests.Wait)
	router := chi.NewRouter()
	router.Get("/api/instances", handler.ListInstances)
	router.Put("/api/instances/{instanceID}", handler.UpdateInstance)
	update := func(fields string) *httptest.ResponseRecorder {
		t.Helper()
		body := fmt.Sprintf(`{"name":"seedbox","host":%q,"username":""%s}`, stub.URL, fields)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPut, fmt.Sprintf("/api/instances/%d", instance.ID), strings.NewReader(body)))
		return rec
	}
	type addresses struct {
		PublicPeerAddress         string `json:"publicPeerAddress"`
		ResolvedPublicPeerAddress string `json:"resolvedPublicPeerAddress"`
	}
	read := func() addresses {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/instances", nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got []addresses
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got, 1)
		return got[0]
	}
	list := func() addresses {
		t.Helper()
		client, err := clientPool.GetClient(t.Context(), instance.ID)
		require.NoError(t, err)
		require.NoError(t, client.SyncOwner().Sync(t.Context()))
		return read()
	}

	rec := update(`,"tlsSkipVerify":true`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, addresses{ResolvedPublicPeerAddress: "203.0.113.7:51413"}, list())
	syncs := stub.Calls("sync/maindata")
	read()
	require.Equal(t, syncs, stub.Calls("sync/maindata"), "the list sends no sync")

	rec = update(`,"publicPeerAddress":"seedbox.example.org:6881"`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, addresses{PublicPeerAddress: "seedbox.example.org:6881", ResolvedPublicPeerAddress: "seedbox.example.org:6881"}, list())

	rec = update(`,"tlsSkipVerify":false`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "seedbox.example.org:6881", list().PublicPeerAddress, "an update without the field keeps the stored value")

	for _, bad := range []string{"seedbox.example.org", "seedbox.example.org:0", "seedbox.example.org:70000", ":6881"} {
		rec = update(fmt.Sprintf(`,"publicPeerAddress":%q`, bad))
		require.Equal(t, http.StatusBadRequest, rec.Code, bad)
	}

	rec = update(`,"publicPeerAddress":""`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, addresses{ResolvedPublicPeerAddress: "203.0.113.7:51413"}, list())
}
