// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

func renamedEndpointEpisodes(count int) []string {
	episodes := make([]string, count)
	for i := range episodes {
		episodes[i] = fmt.Sprintf("Hafensignal.S01E%02d.1080p.WEB-DL.H.264-LUMA", i+1)
	}
	return episodes
}

func TestWebhookApplyAssemblesThePackThatTheCheckMatched(t *testing.T) {
	renamed := renamedEndpointEpisodes(4)
	for _, tc := range []struct {
		name string
		rig  seasonPackEndpointCase
	}{
		{name: "Sonarr title", rig: seasonPackEndpointCase{held: slices.Concat(endpointEpisodes(2), renamed[2:]), total: 4, sonarrTitles: []string{"Hafensignal"}}},
		{name: "alternate title of a show that Sonarr does not know", rig: seasonPackEndpointCase{held: renamed, alternates: seasonPackAlternateTitles{"Hafensignal": {"Harbor Signal"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSeasonPackEndpointRig(t, &tc.rig, heldTitlesReady)
			require.Eventually(t, func() bool {
				status, _ := rig.webhookCheck(webhookPackRequest{TorrentName: endpointPack})
				return status == http.StatusOK
			}, 10*time.Second, 10*time.Millisecond)

			applied := rig.webhookApply(endpointPack, endpointPackTorrent(t, 1, 4))
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
		"cross-seed instance unavailable":          {"crossseed_instance_unavailable", crossseed.ClassFailed},
		"cross-seed instance cannot see the files": {"crossseed_instance_cannot_see_data", crossseed.ClassFailed},
		"below threshold":                          {"below_threshold", crossseed.ClassSkipped},
		"layout mismatch":                          {"error", crossseed.ClassFailed},
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

func TestWebhookApplyWithoutANameAssemblesThePackThatTheCheckMatched(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     heldTitlesState
		webhook   bool
		automatic bool
		alternate bool
		held      []string
		want      []string
		runs      int
		totals    int64
	}{
		{name: "webhook setting", state: heldTitlesReady, webhook: true, held: endpointEpisodes(4), want: []string{"added"}, runs: 1, totals: 1},
		{name: "both settings", state: heldTitlesReady, webhook: true, automatic: true, held: endpointEpisodes(4), want: []string{"added"}, runs: 1, totals: 1},
		{name: "both settings, held under an alternate title", state: heldTitlesReady, webhook: true, automatic: true, alternate: true, want: []string{"added"}, runs: 1, totals: 1},
		{name: "both settings, below the threshold", state: heldTitlesReady, webhook: true, automatic: true, held: endpointEpisodes(2), want: []string{"no_match"}, runs: 1, totals: 1},
		{name: "no setting", state: heldTitlesReady, held: endpointEpisodes(4), want: []string{"no_match"}},
		{name: "Held titles building", state: heldTitlesBuilding, webhook: true, held: endpointEpisodes(4), want: []string{"no_match"}},
		{name: "nothing held", state: heldTitlesReady, webhook: true, want: []string{"no_match"}},
		{name: "below the threshold", state: heldTitlesReady, webhook: true, held: endpointEpisodes(2), want: []string{"below_threshold"}, runs: 1, totals: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpointCase := &seasonPackEndpointCase{held: tc.held, total: 4, setup: func(r *seasonPackEndpointRig) {
				r.settings.SeasonPackEnabled, r.settings.SeasonPackAutomationEnabled = tc.webhook, tc.automatic
			}}
			if tc.alternate {
				endpointCase.held, endpointCase.total = renamedEndpointEpisodes(4), 0
				endpointCase.alternates = seasonPackAlternateTitles{"Hafensignal": {"Harbor Signal"}}
			}
			rig := newSeasonPackEndpointRig(t, endpointCase, tc.state)
			applied := rig.webhookApply("", endpointPackTorrent(t, 1, 4))
			var statuses []string
			for _, result := range applied.Results {
				require.Equal(t, "lib", result.InstanceName)
				statuses = append(statuses, result.Status)
			}
			require.Equal(t, tc.want, statuses)
			require.Equal(t, slices.Contains(tc.want, "added"), applied.Success)
			require.Len(t, rig.runs(), tc.runs)
			require.Equal(t, tc.totals, rig.arr.totals.Load())
		})
	}
}

func TestWebhookApplyWithoutANameAddsTheResultOfAnotherInstance(t *testing.T) {
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{heldElsewhere: endpointEpisodes(4), total: 4, setup: func(r *seasonPackEndpointRig) {
		r.servers[1].Add(qbtstub.Torrent{
			Hash: fmt.Sprintf("%040d", 7), Name: endpointPack, SavePath: filepath.Join(r.root, "lib"),
			Layout: qbtstub.RootFolder, Files: []string{endpointEpisode(9) + ".mkv"},
		})
	}}, heldTitlesReady)

	applied := rig.webhookApply("", endpointPackTorrent(t, 1, 4))

	statuses := map[string]string{}
	for _, result := range applied.Results {
		statuses[result.InstanceName] = result.Status
	}
	require.True(t, applied.Success)
	require.Equal(t, map[string]string{"lib": "no_match", "other": "added"}, statuses)
}

func TestWebhookApplyWithoutANameKeepsADirectResult(t *testing.T) {
	pack := endpointPackTorrent(t, 1, 4)
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4, setup: func(r *seasonPackEndpointRig) {
		_, err := r.blocklist.Upsert(t.Context(), &models.CrossSeedBlocklistEntry{InstanceID: 1, InfoHash: endpointHash(t, pack)})
		require.NoError(t, err)
		r.holdPack(false)
	}}, heldTitlesReady)
	applied := rig.webhookApply("", pack)
	require.Len(t, applied.Results, 1)
	require.Equal(t, "blocked", applied.Results[0].Status)
	require.Zero(t, rig.arr.totals.Load())
	require.Empty(t, rig.runs())
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
