// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func engines(t *testing.T, run func(t *testing.T, db dbinterface.Querier)) {
	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		run(t, testdb.NewMigratedSQLite(t, "share-store"))
	})
	t.Run("postgres", func(t *testing.T) {
		run(t, testdb.NewMigratedPostgres(t, "share-store"))
	})
}

var storeClock = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func newRecord(hash string) *share.Share {
	return &share.Share{
		InstanceID:          3,
		Hash:                hash,
		Name:                "Show.S01.1080p-GRP",
		SavePath:            "/data/tv",
		Files:               []string{"Show.S01.1080p-GRP/Show.S01E01.mkv", "Show.S01.1080p-GRP/Show.S01E02.mkv"},
		Size:                70,
		HelperHash:          "b" + hash[1:],
		Tracker:             share.TrackerOwn,
		OwnerSecret:         "owner-" + hash,
		RecipientSecret:     "recipient-" + hash,
		TorrentFile:         []byte("d4:infod4:name4:testee"),
		EndAtFirstDownload:  true,
		FirstDownloaderOnly: true,
		ExpiresAt:           storeClock.Add(7 * 24 * time.Hour),
		State:               share.StateActive,
		CreatedAt:           storeClock,
	}
}

func TestTheShareStoreKeepsASharePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, db dbinterface.Querier) {
		store := share.NewStore(db)
		ctx := t.Context()
		want := newRecord("a000000000000000000000000000000000000001")
		require.NoError(t, store.Create(ctx, want))
		require.NotZero(t, want.ID)

		got, err := store.Get(ctx, want.ID)
		require.NoError(t, err)
		require.Equal(t, want, got)

		holder := netip.MustParseAddr("2001:db8::7")
		require.NoError(t, store.SetHolder(ctx, want.ID, holder))
		got, err = store.Get(ctx, want.ID)
		require.NoError(t, err)
		require.Equal(t, holder, got.Holder)

		peers := []share.Peer{{Address: "198.51.100.7:6881", Progress: 0.5, SeenAt: storeClock.Add(time.Minute)}}
		require.NoError(t, store.SaveActivity(ctx, want.ID, share.Activity{Peers: peers, Downloaded: 35, Uploaded: 40}))
		require.NoError(t, store.SetState(ctx, want.ID, share.StateEnded, "Stopped by the user.", storeClock.Add(time.Hour)))
		require.NoError(t, store.SetHolder(ctx, want.ID, netip.Addr{}))

		got, err = store.Get(ctx, want.ID)
		require.NoError(t, err)
		require.Zero(t, got.Holder, "the zero address clears the holder")
		require.Equal(t, peers, got.Peers)
		require.Equal(t, int64(35), got.Downloaded)
		require.Equal(t, int64(40), got.Uploaded)
		require.Equal(t, share.StateEnded, got.State)
		require.Equal(t, "Stopped by the user.", got.EndReason)
		require.Equal(t, storeClock.Add(time.Hour), got.EndedAt)

		_, err = store.Get(ctx, want.ID+100)
		require.ErrorIs(t, err, share.ErrNotFound)
	})
}

func TestTheShareStoreListsUnfinishedAndOldEndedSharesPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, db dbinterface.Querier) {
		store := share.NewStore(db)
		ctx := t.Context()
		active := newRecord("a000000000000000000000000000000000000001")
		ending := newRecord("a000000000000000000000000000000000000002")
		oldEnded := newRecord("a000000000000000000000000000000000000003")
		newEnded := newRecord("a000000000000000000000000000000000000004")
		for _, s := range []*share.Share{active, ending, oldEnded, newEnded} {
			require.NoError(t, store.Create(ctx, s))
		}
		require.NoError(t, store.SetState(ctx, ending.ID, share.StateEnding, "Expired.", time.Time{}))
		require.NoError(t, store.SetState(ctx, oldEnded.ID, share.StateEnded, "Expired.", storeClock.Add(-31*24*time.Hour)))
		require.NoError(t, store.SetState(ctx, newEnded.ID, share.StateEnded, "Expired.", storeClock.Add(-29*24*time.Hour)))

		unfinished, err := store.Unfinished(ctx)
		require.NoError(t, err)
		require.Equal(t, []int64{active.ID, ending.ID}, ids(unfinished))

		list, err := store.List(ctx)
		require.NoError(t, err)
		require.Equal(t, []int64{newEnded.ID, oldEnded.ID, ending.ID, active.ID}, ids(list))

		old, err := store.EndedBefore(ctx, storeClock.Add(-30*24*time.Hour))
		require.NoError(t, err)
		require.Equal(t, []int64{oldEnded.ID}, old)
	})
}

func TestTheShareStoreRemovesOnlyEndedSharesPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, db dbinterface.Querier) {
		store := share.NewStore(db)
		ctx := t.Context()
		running := newRecord("a000000000000000000000000000000000000001")
		require.NoError(t, store.Create(ctx, running))
		require.ErrorIs(t, store.Remove(ctx, running.ID), share.ErrNotEnded)

		require.NoError(t, store.SetState(ctx, running.ID, share.StateEnded, "Expired.", storeClock))
		require.NoError(t, store.Remove(ctx, running.ID))
		_, err := store.Get(ctx, running.ID)
		require.ErrorIs(t, err, share.ErrNotFound)
	})
}

func ids(list []share.Share) []int64 {
	out := make([]int64, 0, len(list))
	for _, s := range list {
		out = append(out, s.ID)
	}
	return out
}
