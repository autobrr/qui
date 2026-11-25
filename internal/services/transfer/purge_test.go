// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
)

func (r *rig) ended(hash string, state transfer.State) int64 {
	r.t.Helper()
	return r.endedWithHelper(hash, state, r.target.ID, "helper"+hash)
}

func (r *rig) endedWithHelper(hash string, state transfer.State, instanceID int, helperHash string) int64 {
	r.t.Helper()
	ctx := r.t.Context()
	record := group(hash)
	record.SourceInstanceID, record.TargetInstanceID = r.source.ID, r.target.ID
	require.NoError(r.t, r.store.Create(ctx, record))
	require.NoError(r.t, r.store.RegisterHelper(ctx, transfer.Helper{InstanceID: instanceID, Hash: helperHash,
		Random: []byte{1}, Owner: transfer.OwnerTransfer, OwnerID: record.ID}))
	require.NoError(r.t, r.store.Finish(ctx, record.ID, state))
	nextClockStep()
	return record.ID
}

func nextClockStep() {
	start := time.Now()
	for now := time.Now(); !now.After(start) || !now.Round(0).After(start.Round(0)); now = time.Now() {
		runtime.Gosched()
	}
}

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
		name      string
		helper    func(r *rig) (int, string)
		protected bool
		stale     bool
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

func TestAKeptHelperGoesAtALaterHourlyPassOnceQuiSeesItGone(t *testing.T) {
	removals := []struct {
		name   string
		remove func(r *rig, ids []int64)
	}{
		{name: "the user removes the rows", remove: func(r *rig, ids []int64) {
			for _, id := range ids {
				require.NoError(r.t, r.svc.Remove(r.t.Context(), id))
			}
		}},
		{name: "the cleanup at a start removes the rows", remove: func(r *rig, _ []int64) {
			r.setClock(time.Now().Add(31 * 24 * time.Hour))
			r.restart()
		}},
		{name: "the rows went before the last restart", remove: func(r *rig, _ []int64) {
			r.setClock(time.Now().Add(31 * 24 * time.Hour))
			r.restart()
			r.restart()
			nextClockStep()
		}},
	}
	for _, removal := range removals {
		t.Run(removal.name, func(t *testing.T) {
			r := newRig(t)
			ctx := t.Context()
			syncNow := func(instanceID int) {
				t.Helper()
				owner, err := r.sync.GetSyncOwner(ctx, instanceID)
				require.NoError(t, err)
				require.NoError(t, owner.Sync(ctx))
			}
			file, there := singleFileTorrent(t, "Show.S01E01.mkv", []byte("helper"))
			r.dst.Seed(file, r.dstDir)
			offline, err := r.instances.Create(ctx, "offline", "http://127.0.0.1:1", "", "", nil, nil, false, new(false))
			require.NoError(t, err)
			syncNow(r.target.ID)
			syncNow(r.source.ID)
			helpers := []struct {
				name       string
				instanceID int
				hash       string
				goes       bool
			}{
				{name: "still on the client", instanceID: r.target.ID, hash: there},
				{name: "on an offline instance", instanceID: offline.ID, hash: "1111111111111111111111111111111111111111"},
				{name: "gone from the client", instanceID: r.target.ID, hash: "2222222222222222222222222222222222222222", goes: true},
				{name: "gone, with no sync since", instanceID: r.source.ID, hash: "3333333333333333333333333333333333333333"},
			}
			ids := make([]int64, 0, len(helpers))
			for i, h := range helpers {
				ids = append(ids, r.endedWithHelper(fmt.Sprintf("aaa%d", i), transfer.StateCancelled, h.instanceID, h.hash))
			}
			r.deps.Config.WakeInterval = time.Millisecond
			r.restart()

			removal.remove(r, ids)
			for _, id := range ids {
				require.False(t, r.exists(id))
			}
			for _, h := range helpers {
				require.True(t, r.store.IsHelper(h.instanceID, h.hash), "%s: no sync since the end", h.name)
			}

			syncNow(r.target.ID)
			r.setClock(r.now().Add(time.Hour))
			gone := helpers[2]
			require.Eventually(t, func() bool { return !r.store.IsHelper(gone.instanceID, gone.hash) },
				5*time.Second, time.Millisecond, "the next hourly pass forgets the gone helper")
			check := func() {
				t.Helper()
				r.sync.SetHelperTorrents(r.store)
				for _, h := range helpers {
					require.Equal(t, !h.goes, r.store.IsHelper(h.instanceID, h.hash), h.name)
					err := r.sync.RefuseHelperFiles(ctx, h.instanceID, []string{h.hash})
					if h.goes {
						require.NoError(t, err, h.name)
					} else {
						require.ErrorIs(t, err, qbittorrent.ErrHelperTorrentFiles, h.name)
					}
				}
			}
			check()
			r.restart()
			check()
		})
	}
}

func TestAHelperThatASyncLeftOutStaysProtectedUntilAFullSync(t *testing.T) {
	ways := []struct {
		name    string
		leftOut func(r *rig, id int64, sync func())
	}{
		{name: "the user removes the row", leftOut: func(r *rig, id int64, sync func()) {
			sync()
			require.NoError(r.t, r.svc.Remove(r.t.Context(), id))
		}},
		{name: "the hourly recheck", leftOut: func(r *rig, id int64, sync func()) {
			require.NoError(r.t, r.svc.Remove(r.t.Context(), id))
			sync()
			forgot, err := r.store.ForgetGoneHelpers(r.t.Context(), r.sync.TorrentGoneSince)
			require.NoError(r.t, err)
			require.Zero(r.t, forgot)
		}},
	}
	for _, way := range ways {
		t.Run(way.name, func(t *testing.T) {
			r := newRig(t)
			r.dst.PartialUpdates, r.dst.LeaveOutNew = true, true
			owner, err := r.sync.GetSyncOwner(t.Context(), r.target.ID)
			require.NoError(t, err)
			sync := func() {
				t.Helper()
				require.NoError(t, owner.Sync(t.Context()))
			}
			sync()
			file, hash := singleFileTorrent(t, "Show.S01E01.mkv", []byte("helper"))
			r.dst.Seed(file, r.dstDir)
			id := r.endedWithHelper("aaaa", transfer.StateCancelled, r.target.ID, hash)

			way.leftOut(r, id, sync)
			require.False(t, r.exists(id))
			require.True(t, r.store.IsHelper(r.target.ID, hash), "the sync left the helper out")

			r.dst.DeleteElsewhere(hash)
			sync()
			forgot, err := r.store.ForgetGoneHelpers(t.Context(), r.sync.TorrentGoneSince)
			require.NoError(t, err)
			require.Equal(t, 1, forgot, "the full sync shows the helper gone")
			require.False(t, r.store.IsHelper(r.target.ID, hash))
		})
	}
}

func TestGetOfATransferThatARemovalTook(t *testing.T) {
	r := newRig(t)
	id := r.ended("aaaa", transfer.StateDone)
	_, err := r.db.ExecContext(t.Context(), `DELETE FROM torrent_transfer_members WHERE transfer_id = ?`, id)
	require.NoError(t, err)

	_, err = r.store.Get(t.Context(), id)
	require.ErrorIs(t, err, transfer.ErrNotFound)
}
