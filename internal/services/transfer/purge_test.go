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
