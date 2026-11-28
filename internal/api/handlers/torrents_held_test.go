// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/infohash"
)

const (
	claimedHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	helperHash  = "ffffffffffffffffffffffffffffffffffffffff"
	ownHash     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func newHeldHandler(t *testing.T) (*TorrentsHandler, *recordingDeleter) {
	t.Helper()
	store, err := transfer.NewStore(t.Context(), testdb.NewMigratedSQLite(t, "torrents-held"))
	require.NoError(t, err)
	require.NoError(t, store.Create(t.Context(), &transfer.Record{
		Kind: transfer.KindTransfer, State: "copying", StartedBy: transfer.StartedByUser,
		SourceInstanceID: 1, TargetInstanceID: 2,
		Members: []transfer.Member{{Hash: heldHash(t, claimedHash), SourceSavePath: "/src", TargetInstanceID: 2}},
	}))
	require.NoError(t, store.RegisterHelper(t.Context(), transfer.Helper{
		InstanceID: 1, Hash: heldHash(t, helperHash), Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: 1,
	}))
	deleter := &recordingDeleter{}
	handler := &TorrentsHandler{deleter: deleter}
	handler.SetHeldTorrents(store)
	return handler, deleter
}

func TestBulkAction_RefusesDeleteAndMoveOfHeldTorrents(t *testing.T) {
	t.Parallel()

	actions := map[string]map[string]any{
		"delete":          {"action": "delete"},
		"deleteWithFiles": {"action": "deleteWithFiles"},
		"setLocation":     {"action": "setLocation", "location": "/data/elsewhere"},
		"setCategory":     {"action": "setCategory", "category": "tv"},
		"toggleAutoTMM":   {"action": "toggleAutoTMM", "enable": true},
	}
	held := map[string]string{
		claimedHash:           "a Transfer holds this torrent",
		helperHash:            "qui added this torrent for a Transfer or a Share",
		"cccc|" + claimedHash: "a Transfer holds this torrent",
	}
	for name, payload := range actions {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, deleter := newHeldHandler(t)
			for hash, reason := range held {
				body := map[string]any{"hashes": []string{ownHash, hash}}
				maps.Copy(body, payload)

				rec := httptest.NewRecorder()
				handler.BulkAction(rec, newTorrentFieldRequest(t, 1, body))

				require.Equal(t, http.StatusConflict, rec.Code, hash)
				require.Contains(t, rec.Body.String(), reason)
			}
			require.Empty(t, deleter.deleteFiles, "a refused action changes no torrent of the selection")
		})
	}
}

func TestRenameRefusesHeldTorrents(t *testing.T) {
	t.Parallel()

	renames := map[string]struct {
		call    func(*TorrentsHandler) http.HandlerFunc
		payload map[string]any
	}{
		"torrent": {func(h *TorrentsHandler) http.HandlerFunc { return h.RenameTorrent }, map[string]any{"name": "New name"}},
		"file":    {func(h *TorrentsHandler) http.HandlerFunc { return h.RenameTorrentFile }, map[string]any{"oldPath": "a.mkv", "newPath": "b.mkv"}},
		"folder":  {func(h *TorrentsHandler) http.HandlerFunc { return h.RenameTorrentFolder }, map[string]any{"oldPath": "a", "newPath": "b"}},
	}
	for name, rename := range renames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, _ := newHeldHandler(t)
			for _, hash := range []string{claimedHash, helperHash} {
				rec := httptest.NewRecorder()
				rename.call(handler)(rec, newTorrentHashRequest(t, 1, hash, rename.payload))

				require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBulkAction_DeletesATorrentThatNoTransferHolds(t *testing.T) {
	t.Parallel()
	handler, deleter := newHeldHandler(t)

	rec := httptest.NewRecorder()
	handler.BulkAction(rec, newTorrentFieldRequest(t, 1, map[string]any{"action": "deleteWithFiles", "hashes": []string{ownHash}}))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []bool{true}, deleter.deleteFiles)
}

func newTorrentHashRequest(t *testing.T, instanceID int, hash string, payload map[string]any) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/", bytes.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("instanceID", strconv.Itoa(instanceID))
	routeCtx.URLParams.Add("hash", hash)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func heldHash(t *testing.T, s string) infohash.Hash {
	t.Helper()
	hash, err := infohash.Parse(s)
	require.NoError(t, err)
	return hash
}

func newHybridHeldHandler(t *testing.T) (*TorrentsHandler, int, *[]string) {
	t.Helper()

	const (
		claimedV1 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
		claimedV2 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1b2b2b2b2b2b2b2b2b2b2b2b2"
		helperV1  = "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
		helperV2  = "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3d4d4d4d4d4d4d4d4d4d4d4d4"
		revClaim1 = "e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5"
		revClaim2 = "e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5f6f6f6f6f6f6f6f6f6f6f6f6"
		revHelp1  = "1212121212121212121212121212121212121212"
		revHelp2  = "1212121212121212121212121212121212121212343434343434343434343434"
		freeHash  = "9999999999999999999999999999999999999999"
	)

	db := testdb.NewMigratedSQLite(t, "torrents-hybrid-held")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	var mu sync.Mutex
	var mutated []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.9.3"))
			return
		case "/api/v2/sync/maindata":
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true}`))
			return
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v2/torrents/") {
				mu.Lock()
				mutated = append(mutated, r.URL.Path)
				mu.Unlock()
			}
		}
		_, _ = w.Write([]byte("Ok."))
	}))
	t.Cleanup(srv.Close)

	instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	client := newStaleCachedClient(t, srv.URL, []qbt.Torrent{
		{Name: "ClaimedV1", Hash: claimedV1, InfohashV1: claimedV1, InfohashV2: claimedV2},
		{Name: "HelperV1", Hash: helperV1, InfohashV1: helperV1, InfohashV2: helperV2},
		{Name: "RevClaim", Hash: revClaim1, InfohashV1: revClaim1, InfohashV2: revClaim2},
		{Name: "RevHelp", Hash: revHelp1, InfohashV1: revHelp1, InfohashV2: revHelp2},
		{Name: "Free", Hash: freeHash},
	})
	testutil.SetUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{instance.ID: client})

	store, err := transfer.NewStore(t.Context(), db)
	require.NoError(t, err)
	require.NoError(t, store.Create(t.Context(), &transfer.Record{
		Kind: transfer.KindTransfer, State: "copying", StartedBy: transfer.StartedByUser,
		SourceInstanceID: instance.ID, TargetInstanceID: instance.ID + 1,
		Members: []transfer.Member{
			{Hash: heldHash(t, claimedV1), SourceSavePath: "/src", TargetInstanceID: instance.ID + 1},
			{Hash: heldHash(t, revClaim2), SourceSavePath: "/src", TargetInstanceID: instance.ID + 1},
		},
	}))
	require.NoError(t, store.RegisterHelper(t.Context(), transfer.Helper{
		InstanceID: instance.ID, Hash: heldHash(t, helperV1), Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: 1,
	}))
	require.NoError(t, store.RegisterHelper(t.Context(), transfer.Helper{
		InstanceID: instance.ID, Hash: heldHash(t, revHelp2), Random: []byte("salt2"), Owner: transfer.OwnerShare, OwnerID: 2,
	}))

	handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
	handler.SetHeldTorrents(store)
	return handler, instance.ID, &mutated
}

func TestBulkAction_RefusesDeleteAndMoveOfHeldHybridTorrentsByOtherHash(t *testing.T) {
	t.Parallel()

	actions := map[string]map[string]any{
		"delete":          {"action": "delete"},
		"deleteWithFiles": {"action": "deleteWithFiles"},
		"setLocation":     {"action": "setLocation", "location": "/data/elsewhere"},
		"setCategory":     {"action": "setCategory", "category": "tv"},
		"toggleAutoTMM":   {"action": "toggleAutoTMM", "enable": true},
	}
	variants := []struct {
		name   string
		hash   string
		reason string
	}{
		{"claimed v1 requested by v2", "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1b2b2b2b2b2b2b2b2b2b2b2b2", "a Transfer holds this torrent"},
		{"claimed v1 requested by uppercase v2", "A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1B2B2B2B2B2B2B2B2B2B2B2B2", "a Transfer holds this torrent"},
		{"helper v1 requested by v2", "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3d4d4d4d4d4d4d4d4d4d4d4d4", "qui added this torrent for a Transfer or a Share"},
		{"claimed v2 requested by v1", "e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5", "a Transfer holds this torrent"},
		{"helper v2 requested by v1", "1212121212121212121212121212121212121212", "qui added this torrent for a Transfer or a Share"},
	}

	for actionName, payload := range actions {
		t.Run(actionName, func(t *testing.T) {
			t.Parallel()
			handler, instanceID, mutated := newHybridHeldHandler(t)
			for _, tc := range variants {
				body := map[string]any{"hashes": []string{"9999999999999999999999999999999999999999", tc.hash}}
				maps.Copy(body, payload)

				rec := httptest.NewRecorder()
				handler.BulkAction(rec, newTorrentFieldRequest(t, instanceID, body))

				require.Equal(t, http.StatusConflict, rec.Code, tc.name)
				require.Contains(t, rec.Body.String(), tc.reason, tc.name)
			}
			require.Empty(t, *mutated, "no qBittorrent mutation should run for refused held hybrid torrents")
		})
	}
}

func TestRenameRefusesHeldHybridTorrentsByOtherHash(t *testing.T) {
	t.Parallel()

	renames := map[string]struct {
		call    func(*TorrentsHandler) http.HandlerFunc
		payload map[string]any
	}{
		"torrent": {func(h *TorrentsHandler) http.HandlerFunc { return h.RenameTorrent }, map[string]any{"name": "New name"}},
		"file":    {func(h *TorrentsHandler) http.HandlerFunc { return h.RenameTorrentFile }, map[string]any{"oldPath": "a.mkv", "newPath": "b.mkv"}},
		"folder":  {func(h *TorrentsHandler) http.HandlerFunc { return h.RenameTorrentFolder }, map[string]any{"oldPath": "a", "newPath": "b"}},
	}
	otherHashes := []string{
		"a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1b2b2b2b2b2b2b2b2b2b2b2b2",
		"c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3d4d4d4d4d4d4d4d4d4d4d4d4",
		"e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5",
		"1212121212121212121212121212121212121212",
	}
	for name, rename := range renames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, instanceID, mutated := newHybridHeldHandler(t)
			for _, hash := range otherHashes {
				rec := httptest.NewRecorder()
				rename.call(handler)(rec, newTorrentHashRequest(t, instanceID, hash, rename.payload))

				require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			}
			require.Empty(t, *mutated, "no qBittorrent rename should run for refused held hybrid torrents")
		})
	}
}

func TestRespondIfHeld_RefreshesEmptySnapshotOrStopsUnresolvedAliases(t *testing.T) {
	t.Parallel()

	const (
		claimedV1 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
		claimedV2 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1b2b2b2b2b2b2b2b2b2b2b2b2"
	)

	t.Run("fresh sync reveals held hybrid torrent when cached snapshot is empty", func(t *testing.T) {
		t.Parallel()

		db := testdb.NewMigratedSQLite(t, "torrents-hybrid-fresh")
		instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
		require.NoError(t, err)
		clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
		require.NoError(t, err)
		t.Cleanup(func() { _ = clientPool.Close() })

		var mu sync.Mutex
		var deleted []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/sync/maindata":
				_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{"` + claimedV1 + `":{"name":"ClaimedHybrid","hash":"` + claimedV1 + `","infohash_v1":"` + claimedV1 + `","infohash_v2":"` + claimedV2 + `"}}}`))
				return
			case "/api/v2/torrents/delete":
				mu.Lock()
				deleted = append(deleted, r.URL.Path)
				mu.Unlock()
			}
			_, _ = w.Write([]byte("Ok."))
		}))
		t.Cleanup(srv.Close)

		instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		testutil.SetUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{
			instance.ID: newStaleCachedClient(t, srv.URL, nil),
		})

		store, err := transfer.NewStore(t.Context(), db)
		require.NoError(t, err)
		require.NoError(t, store.Create(t.Context(), &transfer.Record{
			Kind: transfer.KindTransfer, State: "copying", StartedBy: transfer.StartedByUser,
			SourceInstanceID: instance.ID, TargetInstanceID: instance.ID + 1,
			Members: []transfer.Member{{Hash: heldHash(t, claimedV1), SourceSavePath: "/src", TargetInstanceID: instance.ID + 1}},
		}))

		handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
		handler.SetHeldTorrents(store)

		rec := httptest.NewRecorder()
		handler.BulkAction(rec, newTorrentFieldRequest(t, instance.ID, map[string]any{
			"action": "delete",
			"hashes": []string{claimedV2},
		}))

		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "a Transfer holds this torrent")
		require.Empty(t, deleted)
	})

	t.Run("unresolved alias fails closed before downstream retries", func(t *testing.T) {
		t.Parallel()

		db := testdb.NewMigratedSQLite(t, "torrents-hybrid-unresolved")
		store, err := transfer.NewStore(t.Context(), db)
		require.NoError(t, err)
		require.NoError(t, store.Create(t.Context(), &transfer.Record{
			Kind: transfer.KindTransfer, State: "copying", StartedBy: transfer.StartedByUser,
			SourceInstanceID: 1, TargetInstanceID: 2,
			Members: []transfer.Member{{Hash: heldHash(t, claimedV1), SourceSavePath: "/src", TargetInstanceID: 2}},
		}))

		deleter := &recordingDeleter{}
		handler := &TorrentsHandler{
			deleter:         deleter,
			contentResolver: &mockContentResolver{},
		}
		handler.SetHeldTorrents(store)

		rec := httptest.NewRecorder()
		handler.BulkAction(rec, newTorrentFieldRequest(t, 1, map[string]any{
			"action": "delete",
			"hashes": []string{claimedV2},
		}))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
		require.Empty(t, deleter.deleteFiles)
	})
}
