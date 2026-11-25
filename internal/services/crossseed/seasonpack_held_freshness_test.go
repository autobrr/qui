// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	freshPack        = "Copper.Lantern.S02.1080p.WEB-DL.DDP5.1.H.264-GRP1"
	freshPackEpisode = "Copper.Lantern.S02E%02d.1080p.WEB-DL.DDP5.1.H.264-GRP1"
)

type freshPackRig struct {
	*freshRig
	packService
}

func freshPackCase(t *testing.T, features []string, count int, run func(*freshPackRig)) {
	t.Helper()
	freshCase(t, features, count, func(rig *freshRig) {
		for _, instance := range rig.svc.instanceStore.(*fakeInstanceStore).instances {
			instance.HasLocalFilesystemAccess, instance.UseHardlinks, instance.HardlinkBaseDir = true, true, "/links"
		}
		rig.svc.automationSettingsLoader = defaultSettings(true, 1)
		sonarr := &spyARRLookupService{}
		rig.svc.arrService = sonarr
		run(&freshPackRig{freshRig: rig, packService: newPackService(rig.svc, sonarr)})
	})
}

func (r *freshPackRig) checkPack(torrentData string) (*SeasonPackCheckResponse, time.Duration) {
	r.t.Helper()
	started := time.Now()
	resp := r.packService.check(r.t, freshPack, torrentData)
	return resp, time.Since(started)
}

func TestSeasonPackCheckFindsAnEpisodeInTheSyncThatRuns(t *testing.T) {
	const takes = 300 * time.Millisecond
	holdsOther := func(rig *freshPackRig, progress float64) {
		rig.instances[0].hold(freshOtherName, progress)
		require.NoError(rig.t, rig.instances[0].owner.Sync(rig.t.Context()))
	}
	for name, tc := range map[string]struct {
		torrent string
		before  func(*freshPackRig)
		matched int
		sonarr  int
	}{
		"coverage": {
			torrent: packOf(t, freshPack, freshPackEpisode, 6, 7), matched: 2, sonarr: 1,
			before: func(rig *freshPackRig) { holdsOther(rig, 1) },
		},
		"no threshold": {
			matched: 1, sonarr: 1,
			before: func(rig *freshPackRig) { holdsOther(rig, 0.5) },
		},
		"nothing held": {
			torrent: packOf(t, freshPack, freshPackEpisode, 7), matched: 1, sonarr: 1,
			before: func(*freshPackRig) {},
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, features := range [][]string{freshOn, nil} {
				freshPackCase(t, features, 1, func(rig *freshPackRig) {
					tc.before(rig)
					rig.instances[0].syncTakes(takes)
					rig.instances[0].hold(freshName, 1)
					rig.instances[0].startSync()

					resp, waited := rig.checkPack(tc.torrent)

					if features == nil {
						require.False(t, resp.Ready)
						require.Zero(t, waited)
						return
					}
					require.True(t, resp.Ready)
					require.Equal(t, tc.matched, resp.Matches[0].MatchedEpisodes)
					require.Equal(t, takes, waited)
					require.Equal(t, tc.sonarr, rig.sonarr.seasonCalls)
					require.Len(t, rig.takeRuns(), 1)
				})
			}
		})
	}
}

func TestSeasonPackCheckWaitsOnceForASync(t *testing.T) {
	const takes = 300 * time.Millisecond
	pack := packOf(t, freshPack, freshPackEpisode, 6, 7)
	t.Run("nothing held, then too little", func(t *testing.T) {
		freshPackCase(t, freshOn, 2, func(rig *freshPackRig) {
			hung, quick := rig.instances[0], rig.instances[1]
			require.NoError(rig.t, quick.owner.Sync(rig.t.Context()))
			hung.syncTakes(time.Minute)
			hung.startSync()
			quick.syncTakes(takes)
			quick.hold(freshName, 1)
			quick.startSync()

			resp, waited := rig.checkPack(pack)

			require.Equal(t, "below_threshold", resp.Reason)
			require.Equal(t, heldSyncWait, waited)
		})
	})
	t.Run("a sync that hangs", func(t *testing.T) {
		freshPackCase(t, freshOn, 1, func(rig *freshPackRig) {
			rig.instances[0].syncTakes(time.Minute)
			rig.instances[0].startSync()

			resp, waited := rig.checkPack(pack)

			require.Equal(t, "no_matches", resp.Reason)
			require.Equal(t, heldSyncWait, waited)
			require.Zero(t, rig.sonarr.seasonCalls)
			require.Empty(t, rig.takeRuns())
		})
	})
	t.Run("enough held", func(t *testing.T) {
		freshPackCase(t, freshOn, 1, func(rig *freshPackRig) {
			rig.instances[0].hold(freshOtherName, 1)
			rig.instances[0].hold(freshName, 1)
			require.NoError(t, rig.instances[0].owner.Sync(rig.t.Context()))
			rig.instances[0].syncTakes(takes)
			rig.instances[0].startSync()

			for _, torrent := range []string{pack, ""} {
				resp, waited := rig.checkPack(torrent)

				require.True(t, resp.Ready)
				require.Zero(t, waited)
			}
		})
	})
	t.Run("no sync due", func(t *testing.T) {
		freshPackCase(t, freshOn, 1, func(rig *freshPackRig) {
			pass(time.Minute)

			resp, waited := rig.checkPack(pack)

			require.Equal(t, "no_matches", resp.Reason)
			require.Zero(t, waited)
			synctest.Wait()
			require.Len(t, rig.instances[0].syncs(), 2, "the check refreshes stale data after its answer")
		})
	})
}
