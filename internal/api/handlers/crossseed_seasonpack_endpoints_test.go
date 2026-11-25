// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

var updateSeasonPackEndpoints = flag.Bool("update-season-pack-endpoints", false, "rewrite the season pack endpoint fixtures in testdata")

const seasonPackEndpointsFrozen = "The wire format of the season pack endpoints is frozen (ADR 0094). Put new season pack behavior in the webhook check (POST /api/cross-seed/webhook/check) and the apply route (POST /api/cross-seed/apply)."

type seasonPackEndpointCase struct {
	name          string
	held          []string
	heldElsewhere []string
	alternates    seasonPackAlternateTitles
	sonarrTitles  []string
	total         int
	request       seasonPackEndpointRequest
	torrent       []byte
	body          string
	setup         func(*seasonPackEndpointRig)
	fail          func(*seasonPackEndpointRig)
}

func (tc *seasonPackEndpointCase) payload(t *testing.T) []byte {
	if tc.body != "" {
		return []byte(tc.body)
	}
	request := tc.request
	if request.TorrentName == "" {
		request.TorrentName = endpointPack
	}
	if tc.torrent != nil {
		request.TorrentData = base64.StdEncoding.EncodeToString(tc.torrent)
	}
	return mustJSON(t, request)
}

func seasonPackRejectedCases(t *testing.T) []seasonPackEndpointCase {
	t.Helper()
	pack := endpointPackTorrent(t, 1, 4)
	held := endpointEpisodes(4)
	return []seasonPackEndpointCase{
		{name: "invalid body", body: `{bad json`},
		{name: "setting off", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) { r.settings.SeasonPackEnabled = false }},
		{name: "automatic setting alone", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			r.settings.SeasonPackEnabled, r.settings.SeasonPackAutomationEnabled = false, true
		}},
		{name: "no name", body: `{}`},
		{name: "single episode", total: 4, request: seasonPackEndpointRequest{TorrentName: endpointEpisode(1)}, torrent: pack},
		{name: "movie", total: 4, request: seasonPackEndpointRequest{TorrentName: endpointUnheld}, torrent: pack},
		{name: "invalid torrent, not base64", total: 4, request: seasonPackEndpointRequest{TorrentData: "%%%"}},
		{name: "invalid torrent, not a torrent", total: 4, torrent: []byte("not a torrent")},
		{name: "invalid torrent, no episode file", total: 4, torrent: endpointTorrent(t, endpointPack, 1, "notes.txt", "cover.jpg")},
		{name: "no eligible instance", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			r.updateInstance(1, &models.InstanceUpdateParams{UseHardlinks: new(false)})
		}},
		{name: "requested instance is gone", held: held, total: 4, request: seasonPackEndpointRequest{InstanceIDs: []int{99}}, torrent: pack},
		{name: "settings fail to load", held: held, total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) { r.settingsErr = errors.New("settings unavailable") }},
	}
}

func seasonPackCheckCases(t *testing.T) []seasonPackEndpointCase {
	t.Helper()
	pack := endpointPackTorrent(t, 1, 4)
	return append(seasonPackRejectedCases(t),
		seasonPackEndpointCase{name: "ready by name", held: endpointEpisodes(4), total: 4},
		seasonPackEndpointCase{name: "ready with torrent", held: endpointEpisodes(3), total: 4, torrent: pack},
		seasonPackEndpointCase{name: "ready with torrent, no episode total", held: endpointEpisodes(3), torrent: pack},
		seasonPackEndpointCase{name: "ready on one of two instances", held: endpointEpisodes(1), heldElsewhere: endpointEpisodes(4), total: 4},
		seasonPackEndpointCase{name: "ready on a requested instance", held: endpointEpisodes(4), heldElsewhere: endpointEpisodes(3), total: 4, request: seasonPackEndpointRequest{InstanceIDs: []int{2}}},
		seasonPackEndpointCase{name: "ready, no threshold", held: endpointEpisodes(2)},
		seasonPackEndpointCase{name: "ready, no threshold, two instances", held: endpointEpisodes(1), heldElsewhere: endpointEpisodes(2)},
		seasonPackEndpointCase{name: "below threshold", held: endpointEpisodes(2), heldElsewhere: endpointEpisodes(1), total: 4},
		seasonPackEndpointCase{name: "below threshold with torrent", held: endpointEpisodes(2), torrent: pack},
		seasonPackEndpointCase{name: "episodes held, none match", held: []string{"Harbor.Signal.S01E01.2160p.WEB-DL.H.265-LUMA"}, total: 4},
		seasonPackEndpointCase{name: "episodes held, none match, no threshold", held: []string{"Harbor.Signal.S01E01.2160p.WEB-DL.H.265-LUMA"}},
		seasonPackEndpointCase{name: "nothing held", total: 4},
		seasonPackEndpointCase{name: "nothing held with torrent", total: 4, torrent: pack},
		seasonPackEndpointCase{name: "nothing held, no threshold"},
		seasonPackEndpointCase{name: "torrent list fails", held: endpointEpisodes(4), total: 4, fail: func(r *seasonPackEndpointRig) { r.sync.listErr = errors.New("instance offline") }},
	)
}

func seasonPackApplyCases(t *testing.T) []seasonPackEndpointCase {
	t.Helper()
	pack := endpointPackTorrent(t, 1, 4)
	held := endpointEpisodes(4)
	routed := func(r *seasonPackEndpointRig) {
		id := r.addInstance("seeds", nil)
		r.updateInstance(id, &models.InstanceUpdateParams{UseHardlinks: new(false)})
		require.NoError(r.t, r.routes.Upsert(r.t.Context(), &models.CrossSeedRoute{
			SourceInstanceID: 1, CrossSeedInstanceID: id,
			Rows: []models.CrossSeedRouteRow{{BaseDir: r.links, MappedPath: r.links}},
		}))
	}
	return append(seasonPackRejectedCases(t),
		seasonPackEndpointCase{name: "applied", held: held, total: 4, torrent: pack},
		seasonPackEndpointCase{name: "applied, no episode total", held: held, torrent: pack},
		seasonPackEndpointCase{name: "applied on the second instance", held: endpointEpisodes(1), heldElsewhere: held, total: 4, torrent: pack},
		seasonPackEndpointCase{name: "applied through a route", held: held, total: 4, torrent: pack, setup: routed},
		seasonPackEndpointCase{name: "applied, recheck queued", held: endpointEpisodes(3), total: 4, torrent: pack},
		seasonPackEndpointCase{name: "applied, recheck fails", held: endpointEpisodes(3), total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) { r.sync.recheckErr = errors.New("recheck rejected") }},
		seasonPackEndpointCase{name: "applied, resume queue full", held: endpointEpisodes(3), total: 4, torrent: pack, fail: (*seasonPackEndpointRig).fillResumeQueue},
		seasonPackEndpointCase{name: "applied, recheck fails and resume queue full", held: endpointEpisodes(3), total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) {
			r.fillResumeQueue()
			r.sync.recheckErr = errors.New("recheck rejected")
		}},
		seasonPackEndpointCase{name: "no torrent", held: held, total: 4},
		seasonPackEndpointCase{name: "already exists", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			r.servers[1].Add(qbtstub.Torrent{Hash: endpointHash(t, pack), Name: endpointPack, SavePath: filepath.Join(r.root, "lib"), Layout: qbtstub.RootFolder, Files: []string{"a.mkv"}})
		}},
		seasonPackEndpointCase{name: "blocked", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			_, err := r.blocklist.Upsert(r.t.Context(), &models.CrossSeedBlocklistEntry{InstanceID: 1, InfoHash: endpointHash(t, pack)})
			require.NoError(r.t, err)
		}},
		seasonPackEndpointCase{name: "existing check fails", held: held, total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) { r.sync.hasErr = errors.New("instance offline") }},
		seasonPackEndpointCase{name: "blocklist check fails", held: held, total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) { r.blocklistDB.offline = true }},
		seasonPackEndpointCase{name: "cross-seed instance unavailable", held: held, total: 4, torrent: pack, setup: routed, fail: func(r *seasonPackEndpointRig) {
			_, err := r.instances.SetActiveState(r.t.Context(), 2, false)
			require.NoError(r.t, err)
		}},
		seasonPackEndpointCase{name: "cross-seed instance cannot see the files", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			routed(r)
			r.sync.hidden = true
		}},
		seasonPackEndpointCase{name: "torrent list fails", held: held, total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) { r.sync.listErr = errors.New("instance offline") }},
		seasonPackEndpointCase{name: "below threshold", held: endpointEpisodes(2), heldElsewhere: endpointEpisodes(1), total: 4, torrent: pack},
		seasonPackEndpointCase{name: "nothing held", total: 4, torrent: pack},
		seasonPackEndpointCase{name: "local files differ in size", held: held, total: 4, torrent: endpointPackTorrent(t, 2, 4)},
		seasonPackEndpointCase{name: "layout mismatch", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			require.NoError(r.t, os.Remove(r.links))
			require.NoError(r.t, os.WriteFile(r.links, nil, 0o600))
		}},
		seasonPackEndpointCase{name: "link fails", held: held, total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) {
			testutil.SetUnexportedField(r.t, r.svc, "seasonPackLinkCreator", func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
				return nil, errors.New("disk full")
			})
		}},
		seasonPackEndpointCase{name: "recheck skipped", held: endpointEpisodes(3), total: 4, torrent: pack, setup: func(r *seasonPackEndpointRig) { r.settings.SkipRecheck = true }},
		seasonPackEndpointCase{name: "add fails", held: held, total: 4, torrent: pack, fail: func(r *seasonPackEndpointRig) { r.sync.addErr = errors.New("add rejected") }},
	)
}

func TestSeasonPackEndpointsKeepTheirWireFormat(t *testing.T) {
	for endpoint, cases := range map[string][]seasonPackEndpointCase{"check": seasonPackCheckCases(t), "apply": seasonPackApplyCases(t)} {
		for mode, held := range map[string]heldTitlesState{"held_titles_off": heldTitlesOff, "held_titles_ready": heldTitlesReady} {
			path := filepath.Join("testdata", "season_pack_endpoints", endpoint+"_"+mode+".json")
			want := map[string]jsontext.Value{}
			if !*updateSeasonPackEndpoints {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(data, &want))
				require.Len(t, want, len(cases))
			}
			got := map[string]seasonPackEndpointRecord{}
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) {
						record := newSeasonPackEndpointRig(t, &tc, held).record(endpoint, tc.payload(t))
						got[tc.name] = record
						if !*updateSeasonPackEndpoints {
							require.Contains(t, want, tc.name, seasonPackEndpointsFrozen)
							require.JSONEq(t, string(want[tc.name]), string(mustJSON(t, record)), seasonPackEndpointsFrozen)
						}
					})
				}
			})
			if *updateSeasonPackEndpoints {
				data, err := json.Marshal(got, json.Deterministic(true), jsontext.WithIndent("  "))
				require.NoError(t, err)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o600))
			}
		}
	}
}
