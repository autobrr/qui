// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

type webhookPackAnswer struct {
	CanCrossSeed   bool                          `json:"canCrossSeed"`
	Matches        []crossseed.WebhookCheckMatch `json:"matches"`
	Recommendation string                        `json:"recommendation"`
	SeasonPack     *webhookPackCoverage          `json:"seasonPack"`
}

type webhookPackCoverage struct {
	InstanceID       int     `json:"instanceId"`
	MatchedEpisodes  int     `json:"matchedEpisodes"`
	TotalEpisodes    int     `json:"totalEpisodes"`
	Coverage         float64 `json:"coverage"`
	ThresholdSkipped bool    `json:"thresholdSkipped"`
}

type webhookPackRequest struct {
	TorrentName string `json:"torrentName"`
	TorrentData string `json:"torrentData,omitempty"`
	InstanceIDs []int  `json:"instanceIds,omitempty"`
	Size        uint64 `json:"size,omitempty"`
}

func (r *seasonPackEndpointRig) webhookCheck(request webhookPackRequest) (int, webhookPackAnswer) {
	r.t.Helper()
	resp := r.post("/api/cross-seed/webhook/check", mustJSON(r.t, request))
	var answer webhookPackAnswer
	require.NoError(r.t, json.Unmarshal(resp.Body.Bytes(), &answer))
	return resp.Code, answer
}

func (r *seasonPackEndpointRig) webhookApply(name string, torrent []byte) crossseed.CrossSeedResponse {
	r.t.Helper()
	resp := r.post("/api/cross-seed/apply", mustJSON(r.t, webhookPackRequest{TorrentName: name, TorrentData: base64.StdEncoding.EncodeToString(torrent)}))
	return decodeApply(r.t, resp)
}

func decodeApply(t *testing.T, resp *httptest.ResponseRecorder) crossseed.CrossSeedResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var answer crossseed.CrossSeedResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &answer))
	return answer
}

func (r *seasonPackEndpointRig) holdPack(incomplete bool) {
	files := endpointEpisodes(4)
	for i := range files {
		files[i] += ".mkv"
	}
	r.servers[1].Add(qbtstub.Torrent{
		Hash: fmt.Sprintf("%040d", 7), Name: endpointPack, SavePath: filepath.Join(r.root, "lib"),
		Layout: qbtstub.RootFolder, Files: files, Incomplete: incomplete,
	})
}

func TestWebhookCheckAnswersForASeasonPackAssembly(t *testing.T) {
	held := endpointEpisodes(4)
	otherWeb := []string{"Harbor.Signal.S01E01.2160p.WEB-DL.H.265-LUMA"}
	for _, tc := range []struct {
		name     string
		rig      seasonPackEndpointCase
		request  webhookPackRequest
		want     int
		coverage *webhookPackCoverage
		run      string
	}{
		{name: "the instance with the best coverage, whatever the size", rig: seasonPackEndpointCase{held: endpointEpisodes(3), heldElsewhere: held, total: 4}, request: webhookPackRequest{Size: 999}, want: http.StatusOK,
			coverage: &webhookPackCoverage{InstanceID: 2, MatchedEpisodes: 4, TotalEpisodes: 4, Coverage: 1}, run: "ready"},
		{name: "no episode total", rig: seasonPackEndpointCase{held: endpointEpisodes(1), heldElsewhere: endpointEpisodes(2)}, want: http.StatusOK,
			coverage: &webhookPackCoverage{InstanceID: 2, MatchedEpisodes: 2, ThresholdSkipped: true}, run: "ready_no_threshold"},
		{name: "a requested instance", rig: seasonPackEndpointCase{held: held, heldElsewhere: endpointEpisodes(3), total: 4}, request: webhookPackRequest{InstanceIDs: []int{2}}, want: http.StatusOK,
			coverage: &webhookPackCoverage{InstanceID: 2, MatchedEpisodes: 3, TotalEpisodes: 4, Coverage: 0.75}, run: "ready"},
		{name: "episodes held, none match", rig: seasonPackEndpointCase{held: otherWeb, total: 4}, want: http.StatusNotFound, run: "skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSeasonPackEndpointRig(t, &tc.rig, heldTitlesReady)
			tc.request.TorrentName = endpointPack
			lists := rig.sync.lists.Load()
			status, answer := rig.webhookCheck(tc.request)
			require.Equal(t, tc.want, status)
			require.Equal(t, tc.coverage, answer.SeasonPack)
			require.Equal(t, tc.coverage != nil, answer.CanCrossSeed)
			require.Equal(t, map[bool]string{true: "download", false: "skip"}[tc.coverage != nil], answer.Recommendation)
			require.Empty(t, answer.Matches)
			require.Equal(t, lists, rig.sync.lists.Load(), "the check reads no full torrent list")
			var statuses []string
			for _, run := range rig.runs() {
				require.Equal(t, "check", run.Phase)
				statuses = append(statuses, run.Status)
			}
			if tc.run == "" {
				require.Empty(t, statuses)
				require.Zero(t, rig.arr.totals.Load())
				return
			}
			require.Equal(t, []string{tc.run}, statuses)
		})
	}
}

func TestWebhookSkipsTheSeasonPackStepWhenItsGateIsClosed(t *testing.T) {
	pack := endpointPackTorrent(t, 1, 4)
	for _, tc := range []struct {
		name  string
		state heldTitlesState
		setup func(*seasonPackEndpointRig)
	}{
		{name: "setting off", state: heldTitlesReady, setup: func(r *seasonPackEndpointRig) { r.settings.SeasonPackEnabled = false }},
		{name: "automatic setting alone", state: heldTitlesReady, setup: func(r *seasonPackEndpointRig) {
			r.settings.SeasonPackEnabled, r.settings.SeasonPackAutomationEnabled = false, true
		}},
		{name: "Held titles building", state: heldTitlesBuilding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4, setup: tc.setup}, tc.state)
			lists := rig.sync.lists.Load()
			status, answer := rig.webhookCheck(webhookPackRequest{TorrentName: endpointPack})
			require.Equal(t, http.StatusNotFound, status)
			require.Nil(t, answer.SeasonPack)
			directScan := map[heldTitlesState]int64{heldTitlesOff: 1, heldTitlesBuilding: 1}[tc.state]
			require.Equal(t, directScan, rig.sync.lists.Load()-lists, "only the direct check reads the torrent list")

			for _, torrent := range [][]byte{pack, endpointTorrent(t, endpointPack, 1, "notes.txt")} {
				applied := rig.webhookApply(endpointPack, torrent)
				require.False(t, applied.Success)
				require.Empty(t, applied.Results)
			}
			require.Zero(t, rig.arr.totals.Load())
			require.Zero(t, rig.sync.adds.Load())
			require.Empty(t, rig.runs())
		})
	}
}

func TestWebhookCheckPrefersADirectMatchOverAnAssembly(t *testing.T) {
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4, setup: func(r *seasonPackEndpointRig) { r.holdPack(true) }}, heldTitlesReady)
	status, answer := rig.webhookCheck(webhookPackRequest{TorrentName: endpointPack})
	require.Equal(t, http.StatusAccepted, status)
	require.Len(t, answer.Matches, 1)
	require.Nil(t, answer.SeasonPack)
	require.Empty(t, rig.runs())
	require.Zero(t, rig.arr.totals.Load())
}

func TestWebhookCheckNeedsALinkEligibleInstanceForAnAssembly(t *testing.T) {
	rig := newSeasonPackEndpointRig(t, &seasonPackEndpointCase{held: endpointEpisodes(4), total: 4, setup: func(r *seasonPackEndpointRig) {
		r.updateInstance(1, &models.InstanceUpdateParams{UseHardlinks: new(false)})
	}}, heldTitlesReady)
	status, answer := rig.webhookCheck(webhookPackRequest{TorrentName: endpointPack})
	require.Equal(t, http.StatusNotFound, status)
	require.Nil(t, answer.SeasonPack)
	require.Empty(t, rig.webhookApply(endpointPack, endpointPackTorrent(t, 1, 4)).Results)
	require.Zero(t, rig.arr.totals.Load())
	require.Empty(t, rig.runs())
}
