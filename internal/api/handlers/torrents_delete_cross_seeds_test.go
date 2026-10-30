// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type fakeDeleteMatches struct {
	hashes map[int][]string
}

func (f *fakeDeleteMatches) FindDeleteMatches(_ context.Context, selection map[int][]string) (*crossseed.DeleteMatchesResponse, error) {
	for instanceID, hashes := range selection {
		f.hashes[instanceID] = slices.Sorted(slices.Values(hashes))
	}
	return &crossseed.DeleteMatchesResponse{
		Matches:   []crossseed.LocalMatch{{InstanceID: 7, Hash: "fff", MatchType: "content_path"}},
		Unchecked: []crossseed.UncheckedInstance{{InstanceID: 8, InstanceName: "offline", Reason: crossseed.UncheckedOffline}},
	}, nil
}

// A select-all check covers the torrents of the filter beyond the loaded rows,
// minus the exclusions, in one request.
// regression: #3341
func TestGetDeleteCrossSeeds_SelectAllChecksEverySelectedTorrent(t *testing.T) {
	t.Parallel()

	db := testdb.NewMigratedSQLite(t, "torrents-delete-cross-seeds")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/sync/maindata" {
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{"aaa":{"name":"A","hash":"aaa","added_on":3},"bbb":{"name":"B","hash":"bbb","added_on":2},"ccc":{"name":"C","hash":"ccc","added_on":1}}}`))
			return
		}
		_, _ = w.Write([]byte("Ok."))
	}))
	t.Cleanup(srv.Close)

	instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	setUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{instance.ID: newStaleCachedClient(t, srv.URL, []qbt.Torrent{
		{Name: "A", Hash: "aaa", AddedOn: 3},
		{Name: "B", Hash: "bbb", AddedOn: 2},
		{Name: "C", Hash: "ccc", AddedOn: 1},
	})})

	finder := &fakeDeleteMatches{hashes: make(map[int][]string)}
	handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
	handler.deleteMatches = finder
	req := newTorrentFieldRequest(t, instance.ID, map[string]any{
		"selectAll":     true,
		"excludeHashes": []string{"CCC"},
	})

	rec := httptest.NewRecorder()
	handler.GetDeleteCrossSeeds(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, map[int][]string{instance.ID: {"aaa", "bbb"}}, finder.hashes)
	var response crossseed.DeleteMatchesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, []crossseed.LocalMatch{{InstanceID: 7, Hash: "fff", MatchType: "content_path"}}, response.Matches)
	require.Equal(t, []crossseed.UncheckedInstance{{InstanceID: 8, InstanceName: "offline", Reason: crossseed.UncheckedOffline}}, response.Unchecked)
}
