// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/crossseed"
)

func TestWebhookApplyAssemblesThePackThatTheCheckMatched(t *testing.T) {
	var absolute []string
	for n := 25; n < 29; n++ {
		absolute = append(absolute, fmt.Sprintf("Harbor.Signal.-.%d.1080p.WEB-DL.H.264-LUMA", n))
	}
	absoluteFiles := make([]string, len(absolute))
	for i, name := range absolute {
		absoluteFiles[i] = name + ".mkv"
	}
	renamed := make([]string, 4)
	for i := range renamed {
		renamed[i] = fmt.Sprintf("Hafensignal.S01E%02d.1080p.WEB-DL.H.264-LUMA", i+1)
	}
	const absolutePack = "Harbor.Signal.S03.1080p.WEB-DL.H.264-LUMA"
	for _, tc := range []struct {
		name    string
		pack    string
		torrent []byte
		rig     seasonPackEndpointCase
	}{
		{name: "episodes", pack: endpointPack, torrent: endpointPackTorrent(t, 1, 4), rig: seasonPackEndpointCase{held: endpointEpisodes(4), total: 4}},
		{name: "seasonless episodes", pack: absolutePack, torrent: endpointTorrent(t, absolutePack, 1, absoluteFiles...), rig: seasonPackEndpointCase{held: absolute}},
		{name: "Sonarr title", pack: endpointPack, torrent: endpointPackTorrent(t, 1, 4), rig: seasonPackEndpointCase{held: slices.Concat(endpointEpisodes(2), renamed[2:]), total: 4, sonarrTitles: []string{"Hafensignal"}}},
		{name: "alternate title", pack: endpointPack, torrent: endpointPackTorrent(t, 1, 4), rig: seasonPackEndpointCase{held: renamed, total: 4, alternates: seasonPackAlternateTitles{"Hafensignal": {"Harbor Signal"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSeasonPackEndpointRig(t, &tc.rig, heldTitlesReady)
			require.Eventually(t, func() bool {
				status, _ := rig.webhookCheck(webhookPackRequest{TorrentName: tc.pack})
				return status == http.StatusOK
			}, 10*time.Second, 10*time.Millisecond)

			applied := rig.webhookApply(tc.pack, tc.torrent)
			require.True(t, applied.Success)
			require.Len(t, applied.Results, 1)
			require.Equal(t, 1, applied.Results[0].InstanceID)
			require.Equal(t, "lib", applied.Results[0].InstanceName)
			require.Equal(t, "added", applied.Results[0].Status)
			require.EqualValues(t, 1, rig.sync.adds.Load())
			run := rig.runs()[0]
			require.Equal(t, "apply", run.Phase)
			require.Equal(t, "applied", run.Status)
			require.Equal(t, 4, run.MatchedEpisodes)
		})
	}
}

func TestWebhookApplyOfAPackWithNothingHeldWritesNoRun(t *testing.T) {
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{total: 4}, heldTitlesReady)
	for _, torrent := range [][]byte{endpointPackTorrent(t, 1, 4), endpointTorrent(t, endpointPack, 1, "notes.txt")} {
		applied := rig.webhookApply(endpointPack, torrent)
		require.False(t, applied.Success)
		require.Empty(t, applied.Results)
	}
	require.Zero(t, rig.arr.totals.Load())
	require.Empty(t, rig.runs())
}

func TestWebhookApplyReportsWhyAPackWasNotAssembled(t *testing.T) {
	want := map[string]struct {
		status string
		class  crossseed.ResultClass
	}{
		"blocked":                                  {"blocked", crossseed.ClassSkipped},
		"existing check fails":                     {"error", crossseed.ClassFailed},
		"blocklist check fails":                    {"error", crossseed.ClassFailed},
		"cross-seed instance unavailable":          {"crossseed_instance_unavailable", crossseed.ClassFailed},
		"cross-seed instance cannot see the files": {"crossseed_instance_cannot_see_data", crossseed.ClassFailed},
		"below threshold":                          {"below_threshold", crossseed.ClassSkipped},
		"layout mismatch":                          {"error", crossseed.ClassFailed},
		"link fails":                               {"error", crossseed.ClassFailed},
		"recheck skipped":                          {"skipped_recheck", crossseed.ClassSkipped},
		"add fails":                                {"error", crossseed.ClassFailed},
	}
	for _, tc := range seasonPackApplyCases(t) {
		result, ok := want[tc.name]
		if !ok {
			continue
		}
		delete(want, tc.name)
		t.Run(tc.name, func(t *testing.T) {
			rig := newSeasonPackEndpointRig(t, &tc, heldTitlesReady)
			applied := rig.webhookApply(endpointPack, tc.torrent)
			require.False(t, applied.Success)
			require.Len(t, applied.Results, 1)
			require.Equal(t, 1, applied.Results[0].InstanceID)
			require.Equal(t, "lib", applied.Results[0].InstanceName)
			require.Equal(t, result.status, applied.Results[0].Status)
			require.Equal(t, result.class, crossseed.ClassOf(applied.Results[0].Status))
			require.NotEmpty(t, applied.Results[0].Message)
			require.Len(t, rig.runs(), 1)
		})
	}
	require.Empty(t, want)
}

func TestWebhookApplyOfAPackWhoseHeldEpisodesDoNotMatchReportsNothing(t *testing.T) {
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: []string{"Harbor.Signal.S01E01.2160p.WEB-DL.H.265-LUMA"}, total: 4}, heldTitlesReady)
	require.Empty(t, rig.webhookApply(endpointPack, endpointPackTorrent(t, 1, 4)).Results)
	require.Equal(t, "drifted", rig.runs()[0].Reason)
}

func TestWebhookApplyOfAPackFollowsTheAutoResumeSwitch(t *testing.T) {
	pack := endpointPackTorrent(t, 1, 4)
	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprintf("skip %t", skip), func(t *testing.T) {
			complete := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4}, heldTitlesReady)
			complete.settings.SkipAutoResumeWebhook = skip
			require.True(t, complete.webhookApply(endpointPack, pack).Success)
			require.Equal(t, strconv.FormatBool(skip), complete.sync.added["paused"])

			partial := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(3), total: 4}, heldTitlesReady)
			partial.settings.SkipAutoResumeWebhook = skip
			partial.fillResumeQueue()
			require.True(t, partial.webhookApply(endpointPack, pack).Success)
			require.Equal(t, "true", partial.sync.added["paused"])
			queued := !skip
			require.Equal(t, queued, strings.Contains(partial.runs()[0].Message, "resume queue is full"))
		})
	}
}

func TestWebhookApplyWithoutANameFollowsTheAutomaticSwitch(t *testing.T) {
	pack := endpointPackTorrent(t, 1, 4)
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic %t", automatic), func(t *testing.T) {
			rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4, setup: func(r *seasonPackEndpointRig) {
				r.settings.SeasonPackEnabled, r.settings.SeasonPackAutomationEnabled = !automatic, automatic
			}}, heldTitlesReady)
			applied := rig.webhookApply("", pack)
			require.Equal(t, automatic, applied.Success)
			require.Equal(t, map[bool]int64{true: 1}[automatic], rig.sync.adds.Load())
		})
	}
}

func TestWebhookApplyAndSeasonPackApplyAddOnePackOnce(t *testing.T) {
	pack := endpointPackTorrent(t, 1, 4)
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4}, heldTitlesReady)
	second, done := make(chan struct{}), make(chan struct{})
	rig.arr.onTotal = func(call int64) {
		if call == 2 {
			close(second)
		}
	}
	var webhook crossseed.CrossSeedResponse
	rig.sync.onAdd = sync.OnceFunc(func() {
		go func() {
			defer close(done)
			webhook = rig.webhookApply(endpointPack, pack)
		}()
		select {
		case <-second:
		case <-done:
		}
	})
	legacy := rig.post("/api/cross-seed/season-pack/apply", mustJSON(t, webhookPackRequest{TorrentName: endpointPack, TorrentData: base64.StdEncoding.EncodeToString(pack)}))
	<-done

	require.Equal(t, http.StatusOK, legacy.Code)
	require.False(t, webhook.Success)
	require.Len(t, webhook.Results, 1)
	require.Equal(t, "exists", webhook.Results[0].Status)
	require.EqualValues(t, 1, rig.sync.adds.Load())
	reasons := map[string]int{}
	for _, run := range rig.runs() {
		reasons[run.Reason]++
	}
	require.Equal(t, map[string]int{"applied": 1, "already_exists": 1}, reasons)
}
