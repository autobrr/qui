// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/infohash"
)

type fakeTorrentLabels map[infohash.Hash]qbittorrent.TorrentLabel

func (f fakeTorrentLabels) TorrentLabel(_ context.Context, _ int, hash infohash.Hash) qbittorrent.TorrentLabel {
	return f[hash]
}

func TestUpsertBlocklistEntrySavesTheTorrentNameAndTrackerDomain(t *testing.T) {
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "crossseed-blocklist-entry")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(ctx, "Test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	hash := mustParseHash(t, "1111111111111111111111111111111111111111")
	svc := &Service{blocklistStore: models.NewCrossSeedBlocklistStore(db), torrentLabels: fakeTorrentLabels{
		hash: {Name: "Quiet.Harbor.S01.1080p.WEB-DL-NOGRP", TrackerDomain: "tracker.example.invalid"},
	}}

	entry, err := svc.UpsertBlocklistEntry(ctx, instance.ID, hash, "", "Search.Result.Title-GRP")
	require.NoError(t, err)
	require.Equal(t, models.BlockedTorrent{Name: "Quiet.Harbor.S01.1080p.WEB-DL-NOGRP", Tracker: "tracker.example.invalid"}, entry.BlockedTorrent)

	svc.torrentLabels = fakeTorrentLabels{}
	entry, err = svc.UpsertBlocklistEntry(ctx, instance.ID, hash, "bad files", "")
	require.NoError(t, err)
	require.Equal(t, "bad files", entry.Note)
	require.Equal(t, models.BlockedTorrent{Name: "Quiet.Harbor.S01.1080p.WEB-DL-NOGRP", Tracker: "tracker.example.invalid"}, entry.BlockedTorrent, "a block after the torrent is gone keeps the name and tracker")

	svc.torrentLabels = nil
	other := mustParseHash(t, "2222222222222222222222222222222222222222")
	entry, err = svc.UpsertBlocklistEntry(ctx, instance.ID, other, "", "")
	require.NoError(t, err)
	require.Zero(t, entry.BlockedTorrent)

	failed := mustParseHash(t, "3333333333333333333333333333333333333333")
	entry, err = svc.UpsertBlocklistEntry(ctx, instance.ID, failed, "", "Paper.Lanterns.2019.720p.BluRay-FAKEGRP")
	require.NoError(t, err)
	require.Equal(t, models.BlockedTorrent{Name: "Paper.Lanterns.2019.720p.BluRay-FAKEGRP"}, entry.BlockedTorrent, "a block of a torrent the instance lacks keeps the name the caller gives")
}
