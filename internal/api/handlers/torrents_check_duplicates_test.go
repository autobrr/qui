// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestCheckDuplicatesMatchesHashesInAnyCase(t *testing.T) {
	t.Parallel()

	const (
		v1Hash       = "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567"
		hybridHash   = "fedcba98765432100123456789abcdeffedcba98"
		hybridV2     = "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
		v2OnlyV2     = "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
		v2OnlyHash   = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
		missingHash  = "9999999999999999999999999999999999999999"
		upperV1      = "0A1B2C3D4E5F60718293A4B5C6D7E8F901234567"
		mixedHybrid  = "FedCba98765432100123456789AbCdEfFeDcBa98"
		upperV2Only  = "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD"
		lowerV1Again = v1Hash
	)
	stored := []qbt.Torrent{
		{Name: "Show.S01E01.1080p.WEB-GRP", Hash: v1Hash, InfohashV1: v1Hash, AddedOn: 1},
		{Name: "Film.2024.2160p.BluRay-GRP", Hash: hybridHash, InfohashV1: hybridHash, InfohashV2: hybridV2, AddedOn: 2},
		{Name: "Album.2023.FLAC-GRP", Hash: v2OnlyHash, InfohashV2: v2OnlyV2, AddedOn: 3},
	}

	db := testdb.NewMigratedSQLite(t, "torrents-check-duplicates")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	instance, err := instanceStore.Create(t.Context(), "alpha", srv.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	testutil.SetUnexportedField(t, clientPool, "clients", map[int]*qbittorrent.Client{instance.ID: newStaleCachedClient(t, srv.URL, stored)})

	handler := NewTorrentsHandler(qbittorrent.NewSyncManager(clientPool, nil), nil, instanceStore, nil)
	req := newTorrentFieldRequest(t, instance.ID, map[string]any{
		"hashes": []string{upperV1, mixedHybrid, upperV2Only, missingHash, lowerV1Again},
	})
	rec := httptest.NewRecorder()
	handler.CheckDuplicates(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Duplicates []qbittorrent.DuplicateTorrentMatch `json:"duplicates"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	got := make(map[string][]string, len(resp.Duplicates))
	for _, match := range resp.Duplicates {
		got[match.Hash] = match.MatchedHashes
	}
	require.Equal(t, map[string][]string{
		v1Hash:     {upperV1, lowerV1Again},
		hybridHash: {mixedHybrid},
		v2OnlyHash: {upperV2Only},
	}, got)
}
