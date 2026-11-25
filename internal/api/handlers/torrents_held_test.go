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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	claimedHash = "aaaa"
	helperHash  = "ffff"
	ownHash     = "bbbb"
)

func newHeldHandler(t *testing.T) (*TorrentsHandler, *recordingDeleter) {
	t.Helper()
	store, err := transfer.NewStore(t.Context(), testdb.NewMigratedSQLite(t, "torrents-held"))
	require.NoError(t, err)
	require.NoError(t, store.Create(t.Context(), &transfer.Record{
		Kind: transfer.KindTransfer, State: "copying", StartedBy: transfer.StartedByUser,
		SourceInstanceID: 1, TargetInstanceID: 2,
		Members: []transfer.Member{{Hash: claimedHash, SourceSavePath: "/src", TargetInstanceID: 2}},
	}))
	require.NoError(t, store.RegisterHelper(t.Context(), transfer.Helper{
		InstanceID: 1, Hash: helperHash, Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: 1,
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
