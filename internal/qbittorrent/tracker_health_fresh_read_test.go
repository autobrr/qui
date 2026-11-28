// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/infohash"
)

func TestFreshReadThatStillFindsUnregisteredKeepsTheNewTrackerAnswer(t *testing.T) {
	t.Parallel()

	for _, webAPI := range []string{"2.15.1", "2.11.4"} {
		t.Run(webAPI, func(t *testing.T) {
			t.Parallel()
			rig := newSinceRig(t, webAPI)
			rig.pass(t)
			started := rig.now

			rig.now = rig.now.Add(time.Hour)
			rig.answer(qbt.TrackerStatusNotWorking, "Unregistered torrent: trumped by a new upload")
			still, err := rig.sm.StillUnregistered(t.Context(), 1, []string{flagsHashDown})
			require.NoError(t, err)
			hash, err := infohash.Parse(flagsHashDown)
			require.NoError(t, err)
			require.True(t, still.Has(hash))

			by, ok := rig.sm.GetTrackerHealthSnapshot(1).UnregisteredBy(flagsHashDown)
			require.True(t, ok)
			require.Equal(t, "Unregistered torrent: trumped by a new upload", by.Message)
			rig.requireSince(t, started, "the fresh read keeps the time")
		})
	}
}
