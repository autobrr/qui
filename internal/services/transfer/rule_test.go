// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/transfer"
)

// A Preview runs the checks of Start and tells the way and the size of the
// Transfer: each file of the group once. It records, claims, and writes
// nothing.
func TestPreviewTellsTheWayAndTheSize(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	crossSeed, _ := crossSeedOf(t, s)
	r.src.Seed(crossSeed, r.srcDir)

	preview, err := r.svc.Preview(t.Context(), r.request(s))

	require.NoError(t, err)
	require.Equal(t, transfer.WaySFTP, preview.Way)
	require.Equal(t, int64(70), preview.Bytes, "the cross-seed reads the same 70 bytes")
	require.Len(t, preview.Members, 2)
	require.False(t, r.store.IsClaimed(r.source.ID, s.hash))
	unfinished, err := r.store.Unfinished(t.Context())
	require.NoError(t, err)
	require.Empty(t, unfinished)
	require.Zero(t, r.dst.Writes("torrents/add"))
	require.Empty(t, tree(t, r.dstDir))

	bittorrent := r.request(s)
	bittorrent.Options.ForcedWay = transfer.WayBitTorrent
	preview, err = r.svc.Preview(t.Context(), bittorrent)
	require.NoError(t, err)
	require.Equal(t, transfer.WayBitTorrent, preview.Way)

	_, err = r.svc.Preview(t.Context(), transfer.Request{SourceInstanceID: r.source.ID, Hash: strings.Repeat("ab", 20), TargetInstanceID: r.target.ID})
	_, refused := errors.AsType[*transfer.RefusalError](err)
	require.True(t, refused, "want a refusal, got %v", err)
}

// After a Transfer fails for good, a rule starts no new Transfer of the
// torrent: the failure waits for the user. The user can still start one.
func TestARuleWaitsForTheUserAfterAPermanentFailure(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	r.disk.free = 69
	byRule := r.request(s)
	byRule.StartedBy, byRule.RuleID = transfer.StartedByRule, new(7)
	require.Equal(t, transfer.FailurePermanent, r.run(byRule).FailureClass)

	_, err := r.svc.Start(t.Context(), byRule)
	require.ErrorIs(t, err, transfer.ErrWaitsForUser)
	_, err = r.svc.Preview(t.Context(), byRule)
	require.ErrorIs(t, err, transfer.ErrWaitsForUser)

	r.disk.free = 0
	require.Equal(t, transfer.StateDone, r.run(r.request(s)).State)
}

// A Transfer sends its end as an event: finished, or failed with the reason.
func TestTransferSendsItsEndAsAnEvent(t *testing.T) {
	r := newRig(t)
	sent := &events{}
	r.deps.Notifier = sent
	r.restart()
	s := newShow(t, showName)
	r.seed(s)

	r.disk.free = 69
	r.run(r.request(s))
	r.disk.free = 0
	r.run(r.request(s))

	require.Len(t, sent.events, 2)
	failed, finished := sent.events[0], sent.events[1]
	require.Equal(t, notifications.EventTransferFailed, failed.Type)
	require.Equal(t, notifications.EventTransferFinished, finished.Type)
	for _, event := range sent.events {
		require.Equal(t, r.source.ID, event.InstanceID)
		require.Equal(t, showName, event.TorrentName)
		require.Equal(t, s.hash, event.TorrentHash)
	}
	require.Equal(t, "Torrent: "+showName+"\nTarget: homelab", finished.Message)
	require.True(t, strings.HasPrefix(failed.Message, "Torrent: "+showName+"\nTarget: homelab\nError: Not enough free space on homelab"), failed.Message)
}

type events struct {
	mu     sync.Mutex
	events []notifications.Event
}

func (e *events) Notify(_ context.Context, event notifications.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}
