// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

// progressAtEachWrite records the progress of Transfer id at each write on
// the target, and moves the clock one second on at each.
func (r *rig) progressAtEachWrite(id *int64, ready <-chan struct{}) func() []transfer.Progress {
	var mu sync.Mutex
	var seen []transfer.Progress
	at := clock
	r.disk.onWrite = func(string) {
		<-ready
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(time.Second)
		r.setClock(at)
		// A Transfer without a progress shows as Done -1.
		p, ok := r.svc.Progress(*id)
		if !ok {
			p.Done = -1
		}
		seen = append(seen, p)
	}
	return func() []transfer.Progress {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

// A running copy shows the bytes it passed of all its bytes, and its speed.
// The progress ends with the Transfer.
func TestProgressCountsTheCopiedBytes(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	var id int64
	ready := make(chan struct{})
	seen := r.progressAtEachWrite(&id, ready)

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	close(ready)
	r.svc.Wait()

	require.Equal(t, transfer.StateDone, r.get(id).State, r.get(id).FailureReason)
	progress := seen()
	require.Greater(t, len(progress), 2)
	require.Equal(t, transfer.Progress{Done: 0, Total: 70}, progress[0], "nothing passed at the first write")
	for i := 1; i < len(progress); i++ {
		require.Equal(t, int64(70), progress[i].Total)
		require.GreaterOrEqual(t, progress[i].Done, progress[i-1].Done)
		require.Positive(t, progress[i].Speed)
	}
	last := progress[len(progress)-1].Done
	require.Positive(t, last)
	require.Less(t, last, int64(70), "the last write comes before its piece passed")
	_, ok := r.svc.Progress(id)
	require.False(t, ok, "a finished Transfer has no progress")
}

// A resumed copy counts the pieces that it proved before as passed.
func TestProgressOfAResumedCopyStartsAtTheProvenPieces(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	writes := 0
	r.disk.onWrite = func(string) {
		if writes++; writes == 5 {
			r.cancel()
		}
	}
	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	r.svc.Wait()
	require.True(t, r.get(id).FinishedAt.IsZero(), "the copy stopped halfway")

	ready := make(chan struct{})
	close(ready)
	seen := r.progressAtEachWrite(&id, ready)
	r.restart()
	r.svc.Wait()

	require.Equal(t, transfer.StateDone, r.get(id).State, r.get(id).FailureReason)
	first := seen()[0]
	require.Equal(t, int64(70), first.Total)
	require.Positive(t, first.Done, "the proven pieces count as passed")
}

// A Transfer torrent shows the bytes that the target downloaded of the
// group.
func TestProgressCountsTheBytesOfATransferTorrent(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	hold := newHoldAt(r.db, "passed by BitTorrent")
	r.restartOn(hold)
	r.connect()
	s := newShow(t, showName)
	r.seed(s)

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	<-hold.held
	progress, ok := r.svc.Progress(id)
	close(hold.release)
	r.svc.Wait()

	require.True(t, ok)
	require.Equal(t, int64(70), progress.Total)
	require.Equal(t, int64(70), progress.Done)
	require.Equal(t, transfer.StateDone, r.get(id).State, r.get(id).FailureReason)
}
