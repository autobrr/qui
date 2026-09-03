// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	quiqbt "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestGetTorrentField_TagBaselineAcceptsFrontendMixedSelectionPayload(t *testing.T) {
	t.Parallel()

	instanceStore, syncManager, instanceIDs := createTorrentFieldTestHarness(t, map[string][]qbt.Torrent{
		"alpha": {
			{Name: "Alpha", Hash: "aaa", Tags: "movies"},
		},
		"beta": {
			{Name: "Beta", Hash: "bbb", Tags: "hdr"},
		},
	})

	handler := NewTorrentsHandler(syncManager, nil, instanceStore)
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"field":       "tags",
		"hashes":      []string{"aaa", "bbb"},
		"targets":     []map[string]any{{"instanceId": instanceIDs["alpha"], "hash": "aaa"}, {"instanceId": instanceIDs["beta"], "hash": "bbb"}},
		"instanceIds": []int{instanceIDs["alpha"], instanceIDs["beta"]},
	})

	rec := httptest.NewRecorder()
	handler.GetTorrentField(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response quiqbt.TorrentFieldResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.ElementsMatch(t, []string{"movies", "hdr"}, response.Values)
}

func TestGetTorrentField_TagBaselineHandlesDuplicateCrossInstanceHashesWithoutTargets(t *testing.T) {
	t.Parallel()

	instanceStore, syncManager, instanceIDs := createTorrentFieldTestHarness(t, map[string][]qbt.Torrent{
		"alpha": {
			{Name: "Alpha", Hash: "shared", Tags: "movies"},
		},
		"beta": {
			{Name: "Beta", Hash: "shared", Tags: "hdr"},
		},
	})

	handler := NewTorrentsHandler(syncManager, nil, instanceStore)
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"field":       "tags",
		"hashes":      []string{"shared"},
		"instanceIds": []int{instanceIDs["alpha"], instanceIDs["beta"]},
	})

	rec := httptest.NewRecorder()
	handler.GetTorrentField(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response quiqbt.TorrentFieldResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.ElementsMatch(t, []string{"movies", "hdr"}, response.Values)
}

func TestGetTorrentField_MagnetURIReturnsSelectedLinks(t *testing.T) {
	t.Parallel()

	instanceStore, syncManager, instanceIDs := createTorrentFieldTestHarness(t, map[string][]qbt.Torrent{
		"alpha": {
			{Name: "Alpha", Hash: "aaa", MagnetURI: "magnet:?xt=urn:btih:aaa"},
			{Name: "Beta", Hash: "bbb"},
			{Name: "Gamma", Hash: "ccc", MagnetURI: "magnet:?xt=urn:btih:ccc"},
		},
	})

	handler := NewTorrentsHandler(syncManager, nil, instanceStore)
	req := newTorrentFieldRequest(t, instanceIDs["alpha"], map[string]any{
		"field": "magnet_uri",
		"sort":  "name",
		"order": "asc",
	})

	rec := httptest.NewRecorder()
	handler.GetTorrentField(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response quiqbt.TorrentFieldResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, []string{
		"magnet:?xt=urn:btih:aaa",
		"magnet:?xt=urn:btih:ccc",
	}, response.Values)
}

// TestGetTorrentField_MagnetURIExplicitHashes pins magnet values through the
// explicit-hash field path. List payloads no longer carry magnet_uri (issue
// #2328), so this endpoint is the only source for the copy-magnet action.
func TestGetTorrentField_MagnetURIExplicitHashes(t *testing.T) {
	t.Parallel()

	instanceStore, syncManager, instanceIDs := createTorrentFieldTestHarness(t, map[string][]qbt.Torrent{
		"alpha": {
			{Name: "Alpha", Hash: "aaa", MagnetURI: "magnet:?xt=urn:btih:aaa"},
			{Name: "Beta", Hash: "bbb", MagnetURI: "magnet:?xt=urn:btih:bbb"},
			{Name: "Gamma", Hash: "ccc", MagnetURI: "magnet:?xt=urn:btih:ccc"},
		},
	})

	handler := NewTorrentsHandler(syncManager, nil, instanceStore)
	req := newTorrentFieldRequest(t, instanceIDs["alpha"], map[string]any{
		"field":  "magnet_uri",
		"hashes": []string{"aaa", "ccc"},
	})

	rec := httptest.NewRecorder()
	handler.GetTorrentField(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response quiqbt.TorrentFieldResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.ElementsMatch(t, []string{
		"magnet:?xt=urn:btih:aaa",
		"magnet:?xt=urn:btih:ccc",
	}, response.Values)
}

// TestGetTorrentField_MagnetURICrossInstanceFilterScope pins magnet values
// through the cross-instance filter-scope field path, the third MagnetURI
// read site (issue #2328).
func TestGetTorrentField_MagnetURICrossInstanceFilterScope(t *testing.T) {
	t.Parallel()

	instanceStore, syncManager, instanceIDs := createTorrentFieldTestHarness(t, map[string][]qbt.Torrent{
		"alpha": {
			{Name: "Alpha", Hash: "aaa", MagnetURI: "magnet:?xt=urn:btih:aaa"},
		},
		"beta": {
			{Name: "Beta", Hash: "bbb", MagnetURI: "magnet:?xt=urn:btih:bbb"},
			{Name: "Gamma", Hash: "ccc"},
		},
	})

	handler := NewTorrentsHandler(syncManager, nil, instanceStore)
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"field":       "magnet_uri",
		"instanceIds": []int{instanceIDs["alpha"], instanceIDs["beta"]},
	})

	rec := httptest.NewRecorder()
	handler.GetTorrentField(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response quiqbt.TorrentFieldResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.ElementsMatch(t, []string{
		"magnet:?xt=urn:btih:aaa",
		"magnet:?xt=urn:btih:bbb",
	}, response.Values)
}

// TestGetTorrentField_CrossInstancePartialResultsRejected pins the filter-scope
// partial guard for non-tags fields: a truncated magnet list behind a 200 would
// read as complete, so a failed instance must fail the whole request.
func TestGetTorrentField_CrossInstancePartialResultsRejected(t *testing.T) {
	t.Parallel()

	instanceStore, syncManager, instanceIDs := createTorrentFieldTestHarness(t, map[string][]qbt.Torrent{
		"alpha": {
			{Name: "Alpha", Hash: "aaa", MagnetURI: "magnet:?xt=urn:btih:aaa"},
		},
	})

	// A second instance with no pool client and an unreachable address: its
	// per-instance fetch errors, which marks the cross-instance aggregate partial.
	broken, err := instanceStore.Create(context.Background(), "beta", "http://127.0.0.1:1", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	handler := NewTorrentsHandler(syncManager, nil, instanceStore)
	req := newTorrentFieldRequest(t, allInstancesID, map[string]any{
		"field":       "magnet_uri",
		"instanceIds": []int{instanceIDs["alpha"], broken.ID},
	})

	rec := httptest.NewRecorder()
	handler.GetTorrentField(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}

func TestListCrossInstanceTorrentsSkipsFreshData(t *testing.T) {
	handler, release := createStaleCrossInstanceReadHarness(t)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/torrents/cross-instance?limit=10", nil)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ListCrossInstanceTorrents(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		release()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("cross-instance list joined a stale in-flight sync")
	}

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, rec.Header().Get("X-Data-Source"))
}

func TestGetTorrentFieldCrossInstanceReadSkipsFreshData(t *testing.T) {
	handler, release := createStaleCrossInstanceReadHarness(t)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := newTorrentFieldRequestWithContext(ctx, t, allInstancesID, map[string]any{
		"field": "magnet_uri",
	})
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.GetTorrentField(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		release()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("cross-instance torrent field read joined a stale in-flight sync")
	}

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func createTorrentFieldTestHarness(t *testing.T, torrentsByInstanceName map[string][]qbt.Torrent) (*models.InstanceStore, *quiqbt.SyncManager, map[string]int) {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "torrents-field")

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := quiqbt.NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = clientPool.Close()
	})

	syncManager := quiqbt.NewSyncManager(clientPool, nil)
	instanceIDs := make(map[string]int, len(torrentsByInstanceName))
	clients := make(map[int]*quiqbt.Client, len(torrentsByInstanceName))

	for instanceName, torrents := range torrentsByInstanceName {
		instance, createErr := instanceStore.Create(context.Background(), instanceName, "http://localhost:8080", "user", "pass", nil, nil, false, nil)
		require.NoError(t, createErr)

		instanceIDs[instanceName] = instance.ID
		clients[instance.ID] = newCachedClient(t, torrents)
	}

	setUnexportedField(t, clientPool, "clients", clients)

	return instanceStore, syncManager, instanceIDs
}

func createStaleCrossInstanceReadHarness(t *testing.T) (*TorrentsHandler, func()) {
	t.Helper()

	releaseSync := make(chan struct{})
	var releaseSyncOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/sync/maindata":
			<-releaseSync
			_, _ = w.Write([]byte(`{"rid":2,"full_update":true,"torrents":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))

	db := testdb.NewMigratedSQLite(t, "torrents-stale-cross-instance")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := quiqbt.NewClientPool(instanceStore, errorStore, 60*time.Second)
	require.NoError(t, err)

	instance, err := instanceStore.Create(context.Background(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	client := newStaleCachedClient(t, srv.URL, []qbt.Torrent{
		{Name: "Alpha", Hash: "aaa", MagnetURI: "magnet:?xt=urn:btih:aaa", AddedOn: 1},
	})
	setUnexportedField(t, clientPool, "clients", map[int]*quiqbt.Client{instance.ID: client})

	release := func() {
		releaseSyncOnce.Do(func() {
			close(releaseSync)
			srv.Close()
			_ = clientPool.Close()
		})
	}
	t.Cleanup(release)

	return NewTorrentsHandler(quiqbt.NewSyncManager(clientPool, nil), nil, instanceStore), release
}

func newCachedClient(t *testing.T, torrents []qbt.Torrent) *quiqbt.Client {
	t.Helper()

	syncManager := qbt.NewSyncManager(nil, qbt.SyncOptions{DynamicSync: false})
	torrentMap := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentMap[torrent.Hash] = torrent
	}

	setUnexportedField(t, syncManager, "data", &qbt.MainData{Torrents: torrentMap})
	setUnexportedField(t, syncManager, "allTorrents", append([]qbt.Torrent(nil), torrents...))
	setUnexportedField(t, syncManager, "lastSync", time.Now())

	client := &quiqbt.Client{}
	setUnexportedField(t, client, "isHealthy", true)
	setUnexportedField(t, client, "lastHealthCheck", time.Now())
	setUnexportedField(t, client, "syncManager", syncManager)

	return client
}

func newStaleCachedClient(t *testing.T, host string, torrents []qbt.Torrent) *quiqbt.Client {
	t.Helper()

	qbtClient := qbt.NewClient(qbt.Config{Host: host, Timeout: 60})
	syncOpts := qbt.DefaultSyncOptions()
	syncOpts.DynamicSync = true
	syncManager := qbtClient.NewSyncManager(syncOpts)

	torrentMap := make(map[string]qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		torrentMap[torrent.Hash] = torrent
	}

	setUnexportedField(t, syncManager, "data", &qbt.MainData{Torrents: torrentMap})
	setUnexportedField(t, syncManager, "allTorrents", append([]qbt.Torrent(nil), torrents...))
	setUnexportedField(t, syncManager, "lastSync", time.Now().Add(-time.Hour))
	setUnexportedField(t, syncManager, "lastSuccessfulSync", time.Now().Add(-time.Hour))

	client := &quiqbt.Client{Client: qbtClient}
	setUnexportedField(t, client, "isHealthy", true)
	setUnexportedField(t, client, "lastHealthCheck", time.Now())
	setUnexportedField(t, client, "syncManager", syncManager)
	setUnexportedField(t, client, "optimisticUpdates", ttlcache.New(ttlcache.Options[string, *quiqbt.OptimisticTorrentUpdate]{}))

	return client
}

func newTorrentFieldRequest(t *testing.T, instanceID int, payload map[string]any) *http.Request {
	t.Helper()
	return newTorrentFieldRequestWithContext(context.Background(), t, instanceID, payload)
}

func newTorrentFieldRequestWithContext(ctx context.Context, t *testing.T, instanceID int, payload map[string]any) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/instances/"+strconv.Itoa(instanceID)+"/torrents/field", bytes.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", strconv.Itoa(instanceID))

	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func setUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()

	field := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	require.Truef(t, field.IsValid(), "field %s missing on %T", fieldName, target)

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}
