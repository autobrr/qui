// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestCopyGroupsInTheListResponse(t *testing.T) {
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "crossseed-copy-groups-list")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)

	root := t.TempDir()
	stub := qbtstub.New(t, root)
	hashes := []string{cgHash('a'), cgHash('b'), cgHash('c')}
	for i, hash := range hashes {
		stub.Add(qbtstub.Torrent{Hash: hash, Name: cgPack, SavePath: filepath.Join(root, fmt.Sprintf("tracker%d", i)), Files: []string{cgEpisode1, cgEpisode2}})
	}
	instance, err := instances.Create(ctx, "seedbox", stub.URL, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	sm := qbittorrent.NewSyncManager(pool, nil)

	svc := NewService(instances, sm, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)
	svc.copyGroups.interval = 10 * time.Millisecond
	svc.StartCopyGroups(ctx)
	t.Cleanup(svc.StopCopyGroups)

	flat, err := sm.GetTorrentsWithFilters(ctx, instance.ID, 0, 0, "", "", "", qbittorrent.FilterOptions{})
	require.NoError(t, err)
	require.Len(t, flat.Torrents, 3)
	require.Nil(t, flat.CopyGroupsReady)

	var grouped *qbittorrent.TorrentResponse
	require.Eventually(t, func() bool {
		grouped, err = sm.GetTorrentsWithFilters(qbittorrent.WithGroupCopies(ctx), instance.ID, 0, 0, "", "", "", qbittorrent.FilterOptions{})
		return err != nil || *grouped.CopyGroupsReady && grouped.Torrents[0].CopyGroup != nil
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, err)

	require.Len(t, grouped.Torrents, 3)
	id := grouped.Torrents[0].CopyGroup.ID
	require.NotNil(t, grouped.Torrents[0].CopyGroup.FirstSorted)
	for _, row := range grouped.Torrents {
		require.Equal(t, id, row.CopyGroup.ID)
	}
	for _, hash := range hashes {
		require.NotContains(t, id, hash[:8])
		plain := sha256.Sum256([]byte(cacheKeyForTorrentFiles(instance.ID, hash)))
		require.NotEqual(t, hex.EncodeToString(plain[:8]), id)
	}
}
