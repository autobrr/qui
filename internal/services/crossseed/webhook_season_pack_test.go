// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/arr"
)

func TestWebhookCheckOfASeasonPackWaitsOnceForASync(t *testing.T) {
	const takes = 300 * time.Millisecond
	for name, tc := range map[string]struct {
		before func(*freshPackRig)
		ready  bool
		waited time.Duration
		sonarr int
		runs   int
	}{
		"nothing held while a sync hangs": {
			before: func(rig *freshPackRig) { rig.instances[0].syncTakes(time.Minute) },
			waited: heldSyncWait,
		},
		"too little held while a sync hangs": {
			before: func(rig *freshPackRig) {
				rig.instances[0].hold(freshOtherName, 1)
				require.NoError(rig.t, rig.instances[0].owner.Sync(rig.t.Context()))
				rig.instances[0].syncTakes(time.Minute)
			},
			waited: heldSyncWait, sonarr: 1, runs: 1,
		},
		"the sync brings the last episode": {
			before: func(rig *freshPackRig) {
				rig.instances[0].hold(freshOtherName, 1)
				require.NoError(rig.t, rig.instances[0].owner.Sync(rig.t.Context()))
				rig.instances[0].syncTakes(takes)
				rig.instances[0].hold(freshName, 1)
			},
			ready: true, waited: takes, sonarr: 1, runs: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			freshPackCase(t, freshOn, 1, func(rig *freshPackRig) {
				rig.sonarr.seasonResult = &arr.SeasonEpisodeTotalResult{TotalEpisodes: 2}
				tc.before(rig)
				rig.instances[0].startSync()

				resp, waited := rig.freshRig.check(freshPack)

				require.Equal(t, tc.ready, resp.CanCrossSeed)
				require.Equal(t, tc.ready, resp.SeasonPack != nil)
				require.Equal(t, tc.waited, waited)
				require.Equal(t, tc.sonarr, rig.sonarr.seasonCalls)
				require.Len(t, rig.takeRuns(), tc.runs)
			})
		})
	}
}
