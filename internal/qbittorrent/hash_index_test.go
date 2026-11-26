// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/infohash"
)

const (
	indexV1Hash       = "1111111111111111111111111111111111111111"
	indexV2OnlyFull   = "2222222222222222222222222222222222222222aaaaaaaaaaaaaaaaaaaaaaaa"
	indexHybridV1     = "3333333333333333333333333333333333333333"
	indexHybridV2Full = "4444444444444444444444444444444444444444bbbbbbbbbbbbbbbbbbbbbbbb"
	indexLegacyV1     = "5555555555555555555555555555555555555555"
	indexLegacyV2Full = "6666666666666666666666666666666666666666cccccccccccccccccccccccc"
	indexMissingHash  = "9999999999999999999999999999999999999999"
)

func testHash(s string) infohash.Hash {
	hash, err := infohash.Parse(s)
	if err != nil {
		panic(err)
	}
	return hash
}

func snapshotFromMaindata(t *testing.T, maindata string) *qbt.SyncSnapshot {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = io.WriteString(w, maindata)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClientWithTimeout(1, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	return client.SyncOwner().SnapshotUnchecked()
}

func hashIndexSnapshot(t *testing.T) *qbt.SyncSnapshot {
	t.Helper()
	return snapshotFromMaindata(t, `{"rid":1,"full_update":true,"torrents":{
		"`+indexV1Hash+`":{"name":"v1","infohash_v1":"`+indexV1Hash+`"},
		"`+indexV2OnlyFull[:40]+`":{"name":"v2 only","infohash_v2":"`+indexV2OnlyFull+`"},
		"`+indexHybridV2Full[:40]+`":{"name":"hybrid","infohash_v1":"`+indexHybridV1+`","infohash_v2":"`+indexHybridV2Full+`"},
		"`+indexLegacyV1+`":{"name":"hybrid under v1","infohash_v1":"`+indexLegacyV1+`","infohash_v2":"`+indexLegacyV2Full+`"}
	}}`)
}

func TestHashIndexFindsEachTorrentByAnyOfItsHashesInAnyCase(t *testing.T) {
	t.Parallel()
	index := NewHashIndex(hashIndexSnapshot(t))

	for _, tc := range []struct {
		name string
		hash string
		want string
	}{
		{"v1 by its hash", indexV1Hash, "v1"},
		{"v2 only by its full v2", indexV2OnlyFull, "v2 only"},
		{"v2 only by its torrent ID", indexV2OnlyFull[:40], "v2 only"},
		{"hybrid by its v1", indexHybridV1, "hybrid"},
		{"hybrid by its full v2", indexHybridV2Full, "hybrid"},
		{"hybrid by its torrent ID", indexHybridV2Full[:40], "hybrid"},
		{"hybrid stored under v1 by its v1", indexLegacyV1, "hybrid under v1"},
		{"hybrid stored under v1 by its full v2", indexLegacyV2Full, "hybrid under v1"},
	} {
		for _, input := range []string{tc.hash, strings.ToUpper(tc.hash)} {
			torrent, ok := index.Torrent(testHash(input))
			require.True(t, ok, "%s: %s", tc.name, input)
			assert.Equal(t, tc.want, torrent.Name, "%s: %s", tc.name, input)
		}
	}
}

func TestHashIndexMissesAnUnknownOrZeroHash(t *testing.T) {
	t.Parallel()
	index := NewHashIndex(hashIndexSnapshot(t))

	for _, hash := range []infohash.Hash{testHash(indexMissingHash), {}} {
		torrent, ok := index.Torrent(hash)
		assert.False(t, ok)
		assert.Nil(t, torrent)
	}

	var empty HashIndex
	_, ok := empty.Torrent(testHash(indexV1Hash))
	assert.False(t, ok)
	assert.Zero(t, empty.Len())
}

func TestHashIndexBatchScansTheSnapshotAtMostOnce(t *testing.T) {
	t.Parallel()
	snapshot := hashIndexSnapshot(t)
	scans := 0
	index := newHashIndex(snapshot, func(s *qbt.SyncSnapshot) map[infohash.Hash]*qbt.Torrent {
		scans++
		return alternateHashes(s)
	})

	found := index.Torrents(infohash.ParseAll([]string{
		indexV1Hash, indexMissingHash, indexHybridV1, "not a hash", indexLegacyV2Full, strings.ToUpper(indexMissingHash),
	}))

	names := make([]string, len(found))
	for i, torrent := range found {
		if torrent != nil {
			names[i] = torrent.Name
		}
	}
	assert.Equal(t, []string{"v1", "", "hybrid", "", "hybrid under v1", ""}, names)
	assert.Equal(t, 1, scans)
}

func TestHashIndexScansNothingWhenEveryHashIsAKey(t *testing.T) {
	t.Parallel()
	snapshot := hashIndexSnapshot(t)
	scans := 0
	index := newHashIndex(snapshot, func(s *qbt.SyncSnapshot) map[infohash.Hash]*qbt.Torrent {
		scans++
		return alternateHashes(s)
	})

	found := foundTorrents(index.Torrents(infohash.ParseAll([]string{indexV1Hash, indexV2OnlyFull, strings.ToUpper(indexLegacyV1)})))

	assert.Len(t, found, 3)
	assert.Zero(t, scans)
}

func TestHashIndexesOfOneSnapshotShareOneScan(t *testing.T) {
	t.Parallel()
	snapshot := hashIndexSnapshot(t)
	scans := 0
	scan := func(s *qbt.SyncSnapshot) map[infohash.Hash]*qbt.Torrent {
		scans++
		return alternateHashes(s)
	}

	for range 3 {
		_, ok := newHashIndex(snapshot, scan).Torrent(testHash(indexMissingHash))
		assert.False(t, ok)
	}

	assert.Equal(t, 1, scans)
}
