// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
)

// ended stores a Transfer of hash that ended in state, with a helper torrent
// in the registry.
func (r *rig) ended(hash string, state transfer.State) int64 {
	r.t.Helper()
	return r.endedWithHelper(hash, state, r.target.ID, "helper"+hash)
}

// endedWithHelper stores a Transfer of hash that ended in state, with the
// helper torrent helperHash on instanceID in the registry.
func (r *rig) endedWithHelper(hash string, state transfer.State, instanceID int, helperHash string) int64 {
	r.t.Helper()
	ctx := r.t.Context()
	record := group(hash)
	record.SourceInstanceID, record.TargetInstanceID = r.source.ID, r.target.ID
	require.NoError(r.t, r.store.Create(ctx, record))
	require.NoError(r.t, r.store.RegisterHelper(ctx, transfer.Helper{InstanceID: instanceID, Hash: helperHash,
		Random: []byte{1}, Owner: transfer.OwnerTransfer, OwnerID: record.ID}))
	require.NoError(r.t, r.store.Finish(ctx, record.ID, state))
	return record.ID
}

// waiting stores a Transfer of hash that waits for its retry.
func (r *rig) waiting(hash string) *transfer.Record {
	r.t.Helper()
	record := group(hash)
	record.SourceInstanceID, record.TargetInstanceID = r.source.ID, r.target.ID
	record.State, record.RetryAt = transfer.StateFailed, time.Now().Add(365*24*time.Hour)
	require.NoError(r.t, r.store.Create(r.t.Context(), record))
	return record
}

func (r *rig) exists(id int64) bool {
	r.t.Helper()
	_, err := r.store.Get(r.t.Context(), id)
	return err == nil
}

// qui removes the rows of Transfers that finished more than 30 days ago, and
// keeps every other row.
func TestTransfersRemoveFinishedRowsAfter30Days(t *testing.T) {
	r := newRig(t)
	old := r.ended("aaaa", transfer.StateDone)
	running := r.waiting("bbbb")

	r.setClock(time.Now().Add(29 * 24 * time.Hour))
	r.restart()
	require.True(t, r.exists(old), "a row of 29 days stays")

	r.setClock(time.Now().Add(31 * 24 * time.Hour))
	r.restart()
	require.False(t, r.exists(old))
	require.True(t, r.exists(running.ID), "a Transfer that runs stays")
}

// regression: #4242
// When the row of a Transfer goes, by the user or after 30 days, qui forgets
// a helper torrent only when it sees that the helper is gone: the client is
// connected and its synced torrent list, from a sync after the end of the
// Transfer, does not hold it. A helper that is still there, or one on an
// offline instance, stays protected, also after a restart.
func TestTheRowOfATransferTakesOnlyItsGoneHelpersAlong(t *testing.T) {
	removals := []struct {
		name   string
		remove func(r *rig, id int64)
	}{
		{name: "the user removes the row", remove: func(r *rig, id int64) {
			require.NoError(r.t, r.svc.Remove(r.t.Context(), id))
		}},
		{name: "the row is 30 days old", remove: func(r *rig, _ int64) {
			r.setClock(time.Now().Add(31 * 24 * time.Hour))
			r.restart()
		}},
	}
	helpers := []struct {
		name string
		// helper registers the helper torrent, and returns its instance and
		// its hash.
		helper    func(r *rig) (int, string)
		protected bool
		// stale leaves the last sync of the target before the end.
		stale bool
	}{
		{name: "still on the client", protected: true, helper: func(r *rig) (int, string) {
			file, hash := singleFileTorrent(r.t, "Show.S01E01.mkv", []byte("helper"))
			r.dst.Seed(file, r.dstDir)
			return r.target.ID, hash
		}},
		{name: "on an offline instance", protected: true, helper: func(r *rig) (int, string) {
			offline, err := r.instances.Create(r.t.Context(), "offline", "http://127.0.0.1:1", "", "", nil, nil, false, new(false))
			require.NoError(r.t, err)
			return offline.ID, "1111111111111111111111111111111111111111"
		}},
		{name: "gone from the client", protected: false, helper: func(r *rig) (int, string) {
			return r.target.ID, "2222222222222222222222222222222222222222"
		}},
		{name: "gone, with no sync since the end", protected: true, stale: true, helper: func(r *rig) (int, string) {
			return r.target.ID, "3333333333333333333333333333333333333333"
		}},
	}
	for _, removal := range removals {
		for _, tc := range helpers {
			t.Run(removal.name+", "+tc.name, func(t *testing.T) {
				r := newRig(t)
				instanceID, hash := tc.helper(r)
				owner, err := r.sync.GetSyncOwner(t.Context(), r.target.ID)
				require.NoError(t, err)
				require.NoError(t, owner.Sync(t.Context()))
				id := r.endedWithHelper("aaaa", transfer.StateCancelled, instanceID, hash)
				if !tc.stale {
					require.NoError(t, owner.Sync(t.Context()), "the target syncs after the end")
				}

				removal.remove(r, id)
				require.False(t, r.exists(id))
				check := func() {
					t.Helper()
					r.sync.SetHelperTorrents(r.store)
					require.Equal(t, tc.protected, r.store.IsHelper(instanceID, hash))
					err := r.sync.RefuseHelperFiles(t.Context(), instanceID, []string{hash})
					if tc.protected {
						require.ErrorIs(t, err, qbittorrent.ErrHelperTorrentFiles)
					} else {
						require.NoError(t, err)
					}
				}
				check()
				r.restart()
				check()
			})
		}
	}
}

// regression: #4242
// A removal that comes between the two reads of Get leaves the Transfer
// without its torrents: Get reports it as not found.
func TestGetOfATransferThatARemovalTook(t *testing.T) {
	r := newRig(t)
	id := r.ended("aaaa", transfer.StateDone)
	_, err := r.db.ExecContext(t.Context(), `DELETE FROM torrent_transfer_members WHERE transfer_id = ?`, id)
	require.NoError(t, err)

	_, err = r.store.Get(t.Context(), id)
	require.ErrorIs(t, err, transfer.ErrNotFound)
}
