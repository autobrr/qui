// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/services/transfer"
)

func TestTheListFiltersTheTransfersByChipPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		ctx := t.Context()
		add := func(record *transfer.Record, state transfer.State) int64 {
			t.Helper()
			require.NoError(t, store.Create(ctx, record))
			record.State = state
			require.NoError(t, store.Save(ctx, record))
			return record.ID
		}
		finish := func(id int64, state transfer.State) {
			t.Helper()
			require.NoError(t, store.Finish(ctx, id, state))
		}

		first := group("aaaa", "bbbb", "cccc")
		first.Size = 8 << 30
		copying := add(first, transfer.StateCopying)
		waiting := add(group("dddd"), transfer.StateWaiting)
		retry := group("eeee")
		retry.RetryAt = time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
		retry.FailureClass, retry.FailureReason = transfer.FailureTemporary, "The source went offline."
		retry.Way = transfer.WayBitTorrent
		retry.Log = []transfer.LogLine{
			{Message: "Passing 1.0 GiB by a Transfer torrent from seedbox to homelab."},
			{Message: "seedbox uses TCP and μTP.", Hint: &transfer.Hint{Kind: transfer.HintTCPAndUTP, InstanceID: source}},
		}
		retrying := add(retry, transfer.StateFailed)
		done := add(group("ffff"), transfer.StateDone)
		finish(done, transfer.StateDone)
		cancelled := add(group("gggg"), transfer.StateCancelled)
		finish(cancelled, transfer.StateCancelled)
		failed := group("hhhh")
		failed.FailureClass, failed.FailureReason = transfer.FailurePermanent, "A file on the target does not match."
		permanent := add(failed, transfer.StateFailed)
		finish(permanent, transfer.StateFailed)
		export := group("iiii")
		export.Kind = transfer.KindExport
		exporting := add(export, transfer.StateFinishing)

		ids := func(rows []transfer.Row) []int64 {
			out := make([]int64, len(rows))
			for i, row := range rows {
				out[i] = row.ID
			}
			return out
		}
		for _, tc := range []struct {
			filter transfer.Filter
			want   []int64
		}{
			{transfer.FilterAll, []int64{exporting, permanent, cancelled, done, retrying, waiting, copying}},
			{transfer.FilterActive, []int64{exporting, copying}},
		} {
			list, err := store.List(ctx, tc.filter, 100, 0)
			require.NoError(t, err)
			require.Equal(t, tc.want, ids(list.Rows), "chip %q", tc.filter)
			require.Equal(t, transfer.Counts{Active: 2, Waiting: 1, Finished: 2, Failed: 2}, list.Counts, "chip %q", tc.filter)
		}

		list, err := store.List(ctx, transfer.FilterAll, 2, 0)
		require.NoError(t, err)
		require.Equal(t, []int64{exporting, permanent}, ids(list.Rows), "the newest rows up to the limit")
		require.Equal(t, 7, list.Counts.Active+list.Counts.Waiting+list.Counts.Finished+list.Counts.Failed, "the counts ignore the limit")

		list, err = store.List(ctx, transfer.FilterAll, 2, 2)
		require.NoError(t, err)
		require.Equal(t, []int64{cancelled, done}, ids(list.Rows), "the second page starts after the first")
		require.Equal(t, 7, list.Counts.Active+list.Counts.Waiting+list.Counts.Finished+list.Counts.Failed, "the counts ignore the page")

		list, err = store.List(ctx, transfer.FilterActive, 2, 1)
		require.NoError(t, err)
		require.Equal(t, []int64{copying}, ids(list.Rows), "a page of a chip skips rows of that chip only")

		list, err = store.List(ctx, transfer.FilterAll, 100, 0)
		require.NoError(t, err)
		byID := map[int64]transfer.Row{}
		for _, row := range list.Rows {
			byID[row.ID] = row
		}
		group := byID[copying]
		require.Equal(t, "aaaa", group.Hash)
		require.Equal(t, "Name aaaa", group.Name, "a row names the torrent of the request")
		require.Equal(t, 3, group.Torrents)
		require.Equal(t, int64(8<<30), group.Size, "a row has the size of the torrent of the request")
		require.Equal(t, transfer.KindExport, byID[exporting].Kind)
		require.True(t, byID[retrying].RetryAt.Equal(retry.RetryAt))
		require.Equal(t, "The source went offline.", byID[retrying].FailureReason)
		require.Equal(t, transfer.WayBitTorrent, byID[retrying].Way)
		require.Equal(t, []transfer.Hint{{Kind: transfer.HintTCPAndUTP, InstanceID: source}}, byID[retrying].Hints)
		require.Equal(t, transfer.FailurePermanent, byID[permanent].FailureClass)
		require.False(t, byID[permanent].FinishedAt.IsZero())
		require.True(t, byID[copying].FinishedAt.IsZero())
	})
}
