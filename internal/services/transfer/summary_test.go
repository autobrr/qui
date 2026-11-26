// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

func (r *rig) requireNothingStarted(hash string) {
	r.t.Helper()
	list, err := r.store.List(r.t.Context(), transfer.FilterAll, 10, 0)
	require.NoError(r.t, err)
	require.Empty(r.t, list.Rows, "the summary records no Transfer")
	require.False(r.t, r.store.IsClaimed(r.source.ID, hash), "the summary claims no torrent")
	require.Zero(r.t, r.dst.Writes("torrents/add"))
	require.Zero(r.t, r.src.Writes("torrents/add"))
}

func TestSummaryShowsTheTransferThatStartWouldStart(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Placement.SavePath = filepath.Join(r.dstDir, "new", "folder")

	summary, err := r.svc.Summary(t.Context(), req)
	require.NoError(t, err)

	require.Equal(t, transfer.WaySFTP, summary.Way)
	require.Equal(t, int64(70), summary.Bytes)
	require.Len(t, summary.Members, 1)
	require.Equal(t, s.hash, summary.Members[0].Hash)
	require.True(t, summary.FreeKnown)
	require.Positive(t, summary.Free)
	require.False(t, summary.NoFileAccess)
	require.Empty(t, summary.Hints)
	r.requireNothingStarted(s.hash)
}

func TestSummaryOfATransferTorrent(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.connect()
	r.dst.WebAPIVersion = "2.15.2"
	r.dst.FreeSpace = 1234
	r.src.BittorrentProtocol = 1
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Placement.SavePath = filepath.Join(r.dstDir, "elsewhere")

	summary, err := r.svc.Summary(t.Context(), req)
	require.NoError(t, err)

	require.Equal(t, transfer.WayBitTorrent, summary.Way)
	require.True(t, summary.NoFileAccess)
	require.True(t, summary.FreeKnown)
	require.Equal(t, int64(1234), summary.Free)
	require.Equal(t, []transfer.Hint{{Kind: transfer.HintTCPAndUTP, InstanceID: r.target.ID}}, summary.Hints)
	r.requireNothingStarted(s.hash)
}
