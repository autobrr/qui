// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
	"unsafe"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	quiqbt "github.com/autobrr/qui/internal/qbittorrent"
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

func createTorrentFieldTestHarness(t *testing.T, torrentsByInstanceName map[string][]qbt.Torrent) (*models.InstanceStore, *quiqbt.SyncManager, map[string]int) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "qui-torrent-field-test-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	db, err := database.New(filepath.Join(tmpDir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	errorStore := models.NewInstanceErrorStore(db)
	clientPool, err := quiqbt.NewClientPool(instanceStore, errorStore)
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

func newTorrentFieldRequest(t *testing.T, instanceID int, payload map[string]any) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/instances/"+strconv.Itoa(instanceID)+"/torrents/field", bytes.NewReader(body))
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
