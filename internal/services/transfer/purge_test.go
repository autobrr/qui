// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

// ended stores a Transfer of hash that ended in state, with a helper torrent
// in the registry.
func (r *rig) ended(hash string, state transfer.State) int64 {
	r.t.Helper()
	ctx := r.t.Context()
	record := group(hash)
	record.SourceInstanceID, record.TargetInstanceID = r.source.ID, r.target.ID
	require.NoError(r.t, r.store.Create(ctx, record))
	require.NoError(r.t, r.store.RegisterHelper(ctx, transfer.Helper{InstanceID: r.target.ID, Hash: "helper" + hash,
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

// qui removes the rows of Transfers that finished more than 30 days ago, with
// the registry rows of their helper torrents, and keeps every other row.
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
	require.False(t, r.store.IsHelper(r.target.ID, "helperaaaa"), "the registry row of its helper torrent goes with it")
	require.True(t, r.exists(running.ID), "a Transfer that runs stays")
}

// The user removes a finished row earlier, with the registry rows of its
// helper torrents. A Transfer that runs has no Remove.
func TestTheUserRemovesAFinishedRow(t *testing.T) {
	r := newRig(t)
	done := r.ended("aaaa", transfer.StateDone)
	running := r.waiting("bbbb")

	require.NoError(t, r.svc.Remove(t.Context(), done))
	require.False(t, r.exists(done))
	require.False(t, r.store.IsHelper(r.target.ID, "helperaaaa"))

	require.ErrorIs(t, r.svc.Remove(t.Context(), running.ID), transfer.ErrNotFinished)
	require.True(t, r.exists(running.ID))
	require.ErrorIs(t, r.svc.Remove(t.Context(), 999), transfer.ErrNotFound)
}
