// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"os"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/logbudget"
)

const packOtherSource = "Amber.Quay.S01E%02d.2160p.WEB.x265-GRP9"

func (p packService) webhookCheck(t *testing.T, name string) *WebhookCheckResponse {
	t.Helper()
	resp, err := p.CheckWebhook(t.Context(), &WebhookCheckRequest{TorrentName: name})
	require.NoError(t, err)
	return resp
}

func TestWebhookCheckOfASeasonPackWaitsOnceForASync(t *testing.T) {
	const takes = 300 * time.Millisecond
	for name, tc := range map[string]struct {
		before func(*freshPackRig)
		ready  bool
		waited time.Duration
		sonarr int
		runs   int
	}{
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

func TestWebhookCheckOfAPackAsksSonarrNothingWhileTheAlternateTitlesAreNotKnown(t *testing.T) {
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('4', packAliasedLocal, 1, 2, 3, 4), nil))
	held := rig.held(gatedAlternateTitles{packAliasSource(), make(chan struct{})})

	resp := held.webhookCheck(t, packNotHeld)

	require.Nil(t, resp.SeasonPack)
	require.Zero(t, held.sonarr.seasonCalls)
	require.Empty(t, held.takeRuns())
}

func TestWebhookCheckOfAPackReadsNoFullListAfterARebuildDuringTheSonarrCall(t *testing.T) {
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('1', packShowEpisode, 1, 2, 3, 4), nil))
	held := rig.held()
	require.NotNil(t, held.webhookCheck(t, packShow).SeasonPack)
	held.arrService = &lookupArr{during: func() {
		h := held.heldTitles
		h.mu.Lock()
		defer h.mu.Unlock()
		h.instances[1] = &heldInstance{snapshot: h.instances[1].snapshot, needsBuild: true}
	}}

	require.Nil(t, held.webhookCheck(t, packShow).SeasonPack)
}

func TestWebhookCheckWritesNoDebugLineForAnEpisodeThatDoesNotMatchThePack(t *testing.T) {
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('1', packOtherSource, 1, 2, 3, 4), nil))
	held := rig.held()

	lines := logbudget.Count(t, func() { require.Nil(t, held.webhookCheck(t, packShow).SeasonPack) })

	require.Equal(t, 4, lines[logbudget.Line{Level: "trace", Message: "[CROSSSEED-MATCH] Release filtered"}])
	require.Zero(t, lines[logbudget.Line{Level: "debug", Message: "[CROSSSEED-MATCH] Release filtered"}])
}

func TestWebhookAssemblesAPackWhileAnotherInstanceDoesNotAnswer(t *testing.T) {
	torrent := packOf(t, packShow, packShowEpisode, 1, 2, 3, 4)
	root := t.TempDir()
	rig := newPackRig(t, root,
		packLibraryOnDisk(t, root, packTorrents('1', packShowEpisode, 1, 2, 3, 4)),
		serveHeldLibrary(t, packTorrents('2', packShowEpisode, 1, 2, 3, 4), nil),
	)
	held := rig.applying(rig.held())
	held.syncManager = unreachableInstance{qbittorrentSync: held.syncManager, id: 2}
	require.NoError(t, os.MkdirAll(rig.links, 0o755))

	checked := held.webhookCheck(t, packShow)
	require.NotNil(t, checked.SeasonPack)
	require.Equal(t, 1, checked.SeasonPack.InstanceID)

	resp, err := held.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentName: packShow, TorrentData: torrent})
	require.NoError(t, err)
	require.True(t, resp.Success, "%+v", resp.Results)
	require.Len(t, resp.Results, 1)
	require.Equal(t, 1, resp.Results[0].InstanceID)
}

func TestWebhookSaysNothingOfSeasonPacksWhileHeldTitlesIsOff(t *testing.T) {
	rig := newPackRig(t, t.TempDir(), serveHeldLibrary(t, packTorrents('1', packShowEpisode, 1, 2, 3, 4), nil))
	full := rig.full()
	torrent := packOf(t, packShow, packShowEpisode, 1, 2, 3, 4)

	logbudget.AssertSilent(t, func() {
		full.webhookCheck(t, packShow)
		resp, err := full.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentName: packShow, TorrentData: torrent})
		require.NoError(t, err)
		require.Empty(t, resp.Results)
	}, "season pack", "held titles")
}

func TestWebhookApplyAssemblesNothingWhileADirectMatchIsStillDownloading(t *testing.T) {
	torrent := packOf(t, packShow, packShowEpisode, 1, 2, 3, 4)
	for name, pending := range map[string][]qbt.Torrent{
		"episodes alone": nil,
		"pack still downloading": {{
			Hash: heldHash('2', 1), Name: packShow, Size: 4 * packLocalFileSize, TotalSize: 4 * packLocalFileSize, Progress: 0.5, AddedOn: 1,
		}},
	} {
		for _, torrentName := range []string{packShow, ""} {
			t.Run(name+" named "+torrentName, func(t *testing.T) {
				root := t.TempDir()
				rig := newPackRig(t, root, packLibraryOnDisk(t, root, slices.Concat(packTorrents('1', packShowEpisode, 1, 2, 3, 4), pending)))
				held := rig.applying(rig.held())
				require.NoError(t, os.MkdirAll(rig.links, 0o755))

				resp, err := held.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentName: torrentName, TorrentData: torrent})

				require.NoError(t, err)
				assembles := pending == nil
				require.Equal(t, assembles, resp.Success, "%+v", resp.Results)
				require.Equal(t, assembles, held.sonarr.seasonCalls > 0)
				require.Equal(t, assembles, len(held.takeRuns()) > 0)
			})
		}
	}
}

func TestSeasonPackAppliesGiveOnePackOneTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var applies seasonPackApplies
		first, releaseFirst := applies.claim("pack")
		second, releaseSecond := applies.claim("pack")
		other, releaseOther := applies.claim("other")
		giveUp, err := first.takeTurn(t.Context())
		require.NoError(t, err)

		_, err = other.takeTurn(t.Context())
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		canceled := make(chan error)
		go func() {
			_, err := second.takeTurn(ctx)
			canceled <- err
		}()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-canceled, context.Canceled)

		turn := make(chan bool)
		go func() {
			_, err := second.takeTurn(t.Context())
			turn <- err == nil && second.addedTo[3]
		}()
		synctest.Wait()
		first.addedTo[3] = true
		giveUp()
		releaseFirst()
		require.True(t, <-turn)
		third, releaseThird := applies.claim("pack")
		require.Same(t, second, third)

		releaseSecond()
		releaseThird()
		releaseOther()
		require.Empty(t, applies.packs)
	})
}
