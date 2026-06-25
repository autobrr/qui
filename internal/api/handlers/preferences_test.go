// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	quiqbt "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// preferencesTestHarness wires a PreferencesHandler to a SyncManager whose client
// pool holds a single injected client. Tests control the client's behaviour by
// pointing its embedded qBittorrent client at a stub server (live success) or at
// a closed server (live error), and by pre-seeding the qui-level last-known-good
// caches via reflection.
type preferencesTestHarness struct {
	handler    *PreferencesHandler
	client     *quiqbt.Client
	instanceID int
}

func newPreferencesTestHarness(t *testing.T) *preferencesTestHarness {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "preferences-handler")

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := quiqbt.NewClientPool(instanceStore, errorStore)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = clientPool.Close()
	})

	syncManager := quiqbt.NewSyncManager(clientPool, nil)

	instance, err := instanceStore.Create(context.Background(), "primary", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	client := &quiqbt.Client{}
	setUnexportedField(t, client, "isHealthy", true)
	setUnexportedField(t, client, "lastHealthCheck", time.Now())

	setUnexportedField(t, clientPool, "clients", map[int]*quiqbt.Client{instance.ID: client})

	return &preferencesTestHarness{
		handler:    NewPreferencesHandler(syncManager),
		client:     client,
		instanceID: instance.ID,
	}
}

// attachEmbeddedClient points the qui client at the given qBittorrent base host so
// that live calls reach it. A real qbt.Client is used so the actual HTTP request
// path (including auto-login) is exercised.
func (h *preferencesTestHarness) attachEmbeddedClient(t *testing.T, host string) {
	t.Helper()

	qbtClient := qbt.NewClient(qbt.Config{
		Host:     host,
		Username: "user",
		Password: "pass",
		Timeout:  2,
	})
	setUnexportedField(t, h.client, "Client", qbtClient)
}

func newPreferencesRequest(t *testing.T, instanceID int) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/instances/"+strconv.Itoa(instanceID)+"/preferences", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", strconv.Itoa(instanceID))

	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

// newQbtStubServer returns an httptest server handling the qBittorrent endpoints
// needed by these tests. The handlers map keys are API paths relative to
// /api/v2/ (e.g. "app/preferences").
func newQbtStubServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		// Set a session cookie via a raw header so the qBittorrent client's jar is
		// populated and subsequent calls skip auto re-login. http.SetCookie is avoided
		// because gosec (G124) flags the missing Secure/HttpOnly/SameSite attributes,
		// which are meaningless for this in-process test stub.
		w.Header().Set("Set-Cookie", "SID=test-session; Path=/")
		_, _ = w.Write([]byte("Ok."))
	})

	for path, handler := range handlers {
		mux.HandleFunc("/api/v2/"+path, handler)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestGetPreferences_GracefulDegradation(t *testing.T) {
	t.Parallel()

	t.Run("live success omits cached-at header", func(t *testing.T) {
		t.Parallel()

		h := newPreferencesTestHarness(t)
		server := newQbtStubServer(t, map[string]http.HandlerFunc{
			"app/preferences": func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(qbt.AppPreferences{Dht: true, MaxConnec: 200})
			},
		})
		h.attachEmbeddedClient(t, server.URL)

		rec := httptest.NewRecorder()
		h.handler.GetPreferences(rec, newPreferencesRequest(t, h.instanceID))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Empty(t, rec.Header().Get("X-Qui-Cached-At"), "fresh live response must not set the staleness header")

		var prefs qbt.AppPreferences
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prefs))
		require.True(t, prefs.Dht)
		require.Equal(t, 200, prefs.MaxConnec)
	})

	t.Run("live error with warm cache serves cached body and header", func(t *testing.T) {
		t.Parallel()

		h := newPreferencesTestHarness(t)

		// No embedded client -> live fetch fails. Pre-seed the last-known-good cache.
		fetchedAt := time.Now().Add(-90 * time.Second).Truncate(time.Second)
		setUnexportedField(t, h.client, "preferencesCache", &qbt.AppPreferences{Dht: true, MaxConnec: 123})
		setUnexportedField(t, h.client, "preferencesFetchedAt", fetchedAt)

		rec := httptest.NewRecorder()
		h.handler.GetPreferences(rec, newPreferencesRequest(t, h.instanceID))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		header := rec.Header().Get("X-Qui-Cached-At")
		require.NotEmpty(t, header, "stale response must set the staleness header")
		parsed, err := time.Parse(time.RFC3339, header)
		require.NoError(t, err, "header must be RFC3339")
		require.Equal(t, fetchedAt.UTC(), parsed.UTC())

		var prefs qbt.AppPreferences
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prefs))
		require.True(t, prefs.Dht)
		require.Equal(t, 123, prefs.MaxConnec)
	})

	t.Run("live error with cold cache returns the error", func(t *testing.T) {
		t.Parallel()

		h := newPreferencesTestHarness(t)

		// No embedded client and no cache -> error surfaces as-is.
		rec := httptest.NewRecorder()
		h.handler.GetPreferences(rec, newPreferencesRequest(t, h.instanceID))

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Empty(t, rec.Header().Get("X-Qui-Cached-At"), "error response must not set the staleness header")
	})
}

func TestGetAlternativeSpeedLimitsMode_GracefulDegradation(t *testing.T) {
	t.Parallel()

	t.Run("live success omits cached-at header and warms cache", func(t *testing.T) {
		t.Parallel()

		h := newPreferencesTestHarness(t)
		server := newQbtStubServer(t, map[string]http.HandlerFunc{
			"transfer/speedLimitsMode": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("1"))
			},
		})
		h.attachEmbeddedClient(t, server.URL)

		rec := httptest.NewRecorder()
		h.handler.GetAlternativeSpeedLimitsMode(rec, newPreferencesRequest(t, h.instanceID))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Empty(t, rec.Header().Get("X-Qui-Cached-At"), "fresh live response must not set the staleness header")

		var body map[string]bool
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.True(t, body["enabled"])

		// The successful live fetch must have populated the last-known-good cache.
		cached, ok := h.client.GetCachedAlternativeSpeedLimitsMode()
		require.True(t, ok)
		require.True(t, cached)
	})

	t.Run("live error with warm cache serves cached value and header", func(t *testing.T) {
		t.Parallel()

		h := newPreferencesTestHarness(t)

		// No embedded client -> live fetch fails. Pre-seed the last-known-good cache.
		fetchedAt := time.Now().Add(-45 * time.Second).Truncate(time.Second)
		setUnexportedField(t, h.client, "altSpeedMode", true)
		setUnexportedField(t, h.client, "altSpeedFetched", true)
		setUnexportedField(t, h.client, "altSpeedFetchedAt", fetchedAt)

		rec := httptest.NewRecorder()
		h.handler.GetAlternativeSpeedLimitsMode(rec, newPreferencesRequest(t, h.instanceID))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		header := rec.Header().Get("X-Qui-Cached-At")
		require.NotEmpty(t, header, "stale response must set the staleness header")
		parsed, err := time.Parse(time.RFC3339, header)
		require.NoError(t, err, "header must be RFC3339")
		require.Equal(t, fetchedAt.UTC(), parsed.UTC())

		var body map[string]bool
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.True(t, body["enabled"])
	})

	t.Run("live error with cold cache returns the error", func(t *testing.T) {
		t.Parallel()

		h := newPreferencesTestHarness(t)

		rec := httptest.NewRecorder()
		h.handler.GetAlternativeSpeedLimitsMode(rec, newPreferencesRequest(t, h.instanceID))

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Empty(t, rec.Header().Get("X-Qui-Cached-At"), "error response must not set the staleness header")
	})
}
