// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

// regression: #4242
// A running copy shows the bytes it passed of all its bytes, and its speed
// over the last ten seconds: a copy that passed no byte in that time shows
// no speed. The progress ends with the Transfer.
func TestProgressCountsTheCopiedBytes(t *testing.T) {
	r := newRig(t)
	s := newShow(t, showName)
	r.seed(s)
	var id int64
	ready := make(chan struct{})
	var seen []transfer.Progress
	var stalled transfer.Progress
	// The clock moves one second on at each write, and 11 more after the
	// third.
	at := clock
	r.disk.onWrite = func(string) {
		<-ready
		at = at.Add(time.Second)
		r.setClock(at)
		p, _ := r.svc.Progress(id)
		seen = append(seen, p)
		if len(seen) == 3 {
			at = at.Add(11 * time.Second)
			r.setClock(at)
			stalled, _ = r.svc.Progress(id)
		}
	}

	id, err := r.svc.Start(t.Context(), r.request(s))
	require.NoError(t, err)
	close(ready)
	r.svc.Wait()

	require.Equal(t, transfer.StateDone, r.get(id).State, r.get(id).FailureReason)
	require.Greater(t, len(seen), 2)
	require.Equal(t, transfer.Progress{Done: 0, Total: 70}, seen[0], "nothing passed at the first write")
	for i := 1; i < len(seen); i++ {
		require.Equal(t, int64(70), seen[i].Total)
		require.GreaterOrEqual(t, seen[i].Done, seen[i-1].Done)
	}
	require.Positive(t, seen[1].Speed)
	require.Positive(t, seen[2].Speed)
	require.Equal(t, seen[2].Done, stalled.Done)
	require.Zero(t, stalled.Speed, "nothing passed over the last ten seconds")
	last := seen[len(seen)-1].Done
	require.Positive(t, last)
	require.Less(t, last, int64(70), "the last write comes before its piece passed")
	_, ok := r.svc.Progress(id)
	require.False(t, ok, "a finished Transfer has no progress")
}
