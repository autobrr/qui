// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"encoding/json"
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func tv(hash, name string) qbittorrent.TorrentView {
	return qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: name}}
}

func singleResp(rows ...qbittorrent.TorrentView) *qbittorrent.TorrentResponse {
	return &qbittorrent.TorrentResponse{Torrents: rows, Total: len(rows)}
}

func ci(instanceID int, hash, name string) qbittorrent.CrossInstanceTorrentView {
	return qbittorrent.CrossInstanceTorrentView{
		TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: hash, Name: name}},
		InstanceID:  instanceID,
	}
}

func crossResp(rows ...qbittorrent.CrossInstanceTorrentView) *qbittorrent.TorrentResponse {
	return &qbittorrent.TorrentResponse{CrossInstanceTorrents: rows, Total: len(rows)}
}

func changedHashes(payload *StreamPayload) []string {
	out := make([]string, 0, len(payload.Data.Torrents))
	for _, row := range payload.Data.Torrents {
		out = append(out, row.Hash)
	}
	return out
}

func TestBuildUpdatePayloadSeedsFullThenStreamsDeltas(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	full := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, full.Type)
	require.Nil(t, full.Delta)
	require.Len(t, full.Data.Torrents, 3)
	require.NotNil(t, g.baselineSnapshot)

	steady := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventDelta, steady.Type)
	require.Empty(t, steady.Data.Torrents)
	require.Nil(t, steady.Delta.Order)
	require.Equal(t, 3, steady.Data.Total, "aggregate total still reflects the full page")

	changed := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B2"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventDelta, changed.Type)
	require.Equal(t, []string{"b"}, changedHashes(changed))
	require.Nil(t, changed.Delta.Order, "order is omitted when only values change")
}

func TestBuildUpdatePayloadAddSendsOrderAndRow(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B")), &StreamMeta{})

	added := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("d", "D")), &StreamMeta{})
	require.Equal(t, streamEventDelta, added.Type)
	require.NotNil(t, added.Delta.Order)
	require.Equal(t, []string{"a", "b", "d"}, *added.Delta.Order, "membership change sends full order")
	require.Equal(t, []string{"d"}, changedHashes(added), "only the new row is carried")
}

func TestBuildUpdatePayloadRemoveSendsShorterOrder(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})

	removed := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventDelta, removed.Type)
	require.NotNil(t, removed.Delta.Order)
	require.Equal(t, []string{"a", "c"}, *removed.Delta.Order)
	require.Empty(t, changedHashes(removed), "surviving unchanged rows are not re-sent")
}

func TestBuildUpdatePayloadReorderSendsOrderOnly(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})

	reordered := g.buildUpdatePayload(opts, singleResp(tv("c", "C"), tv("b", "B"), tv("a", "A")), &StreamMeta{})
	require.Equal(t, streamEventDelta, reordered.Type)
	require.NotNil(t, reordered.Delta.Order)
	require.Equal(t, []string{"c", "b", "a"}, *reordered.Delta.Order)
	require.Empty(t, changedHashes(reordered), "a pure reorder carries no row payloads")
}

func TestBuildUpdatePayloadEmptiedPageSendsPresentEmptyOrder(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B")), &StreamMeta{})

	cleared := g.buildUpdatePayload(opts, singleResp(), &StreamMeta{})
	require.Equal(t, streamEventDelta, cleared.Type)
	require.NotNil(t, cleared.Delta.Order, "an emptied page must send a present order, not omit it")
	require.Empty(t, *cleared.Delta.Order)
	require.Empty(t, changedHashes(cleared))

	encoded, err := json.Marshal(cleared)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"order":[]`)
}

func TestBuildUpdatePayloadHasNoPeriodicKeyframe(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	first := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, first.Type, "first tick seeds with a full snapshot")

	for range 200 {
		p := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{})
		require.Equal(t, streamEventDelta, p.Type, "every later tick stays a delta")
	}
}

func TestVolatileFieldsDoNotTriggerResend(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}

	base := &qbt.Torrent{Hash: "a", Name: "A", Reannounce: 1800, TimeActive: 100, SeedingTime: 100, NumSeeds: 3}
	g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: base}), &StreamMeta{})

	ticked := *base
	ticked.Reannounce = 1798
	ticked.TimeActive = 102
	ticked.SeedingTime = 102
	ticked.LastActivity = 999
	ticked.NumSeeds = 7
	ticked.Popularity = 1.5
	idle := g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: &ticked}), &StreamMeta{})
	require.Equal(t, streamEventDelta, idle.Type)
	require.Empty(t, changedHashes(idle), "a torrent that only ticked its counters must not be re-sent")

	active := ticked
	active.DlSpeed = 1024
	sent := g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: &active}), &StreamMeta{})
	require.Equal(t, []string{"a"}, changedHashes(sent), "a real change (speed) must re-send the row")
}

func TestOneRowViewResendsLiveCounters(t *testing.T) {
	counters := map[string]func(*qbt.Torrent){
		"TimeActive":       func(tr *qbt.Torrent) { tr.TimeActive++ },
		"SeedingTime":      func(tr *qbt.Torrent) { tr.SeedingTime++ },
		"NumSeeds":         func(tr *qbt.Torrent) { tr.NumSeeds++ },
		"NumLeechs":        func(tr *qbt.Torrent) { tr.NumLeechs++ },
		"NumComplete":      func(tr *qbt.Torrent) { tr.NumComplete++ },
		"NumIncomplete":    func(tr *qbt.Torrent) { tr.NumIncomplete++ },
		"ConnectionsCount": func(tr *qbt.Torrent) { tr.ConnectionsCount++ },
		"PiecesHave":       func(tr *qbt.Torrent) { tr.PiecesHave++ },
		"Reannounce":       func(tr *qbt.Torrent) { tr.Reannounce-- },
	}
	for name, tick := range counters {
		for _, tc := range []struct {
			view   string
			limit  int
			resend bool
		}{
			{view: "details row", limit: 1, resend: true},
			{view: "main list", limit: 300, resend: false},
		} {
			t.Run(name+"/"+tc.view, func(t *testing.T) {
				g := &subscriptionGroup{}
				opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1, Limit: tc.limit}}
				base := qbt.Torrent{Hash: "a", Name: "A"}
				g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: &base}), &StreamMeta{})

				ticked := base
				tick(&ticked)
				payload := g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: &ticked}), &StreamMeta{})
				if tc.resend {
					require.Equal(t, []string{"a"}, changedHashes(payload))
				} else {
					require.Empty(t, changedHashes(payload))
				}
			})
		}
	}
}

func TestBuildUpdatePayloadCrossInstance(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{Unified: true, InstanceIDs: []int{1, 2}}}

	full := g.buildUpdatePayload(opts, crossResp(ci(1, "a", "A"), ci(2, "a", "A")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, full.Type)
	require.Len(t, full.Data.CrossInstanceTorrents, 2)

	changed := g.buildUpdatePayload(opts, crossResp(ci(1, "a", "A"), ci(2, "a", "A2")), &StreamMeta{})
	require.Equal(t, streamEventDelta, changed.Type)
	require.Nil(t, changed.Data.Torrents, "single-instance slice stays empty on a cross-instance delta")
	require.Len(t, changed.Data.CrossInstanceTorrents, 1)
	require.Equal(t, 2, changed.Data.CrossInstanceTorrents[0].InstanceID)
	require.Nil(t, changed.Delta.Order, "value-only change keeps order implicit")
}

func TestCrossInstanceMetadataChangeIsFlaggedAsChanged(t *testing.T) {
	rowA := qbittorrent.CrossInstanceTorrentView{
		TorrentView:  &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "a", Name: "A"}},
		InstanceID:   1,
		InstanceName: "alpha",
	}
	rowRenamed := rowA
	rowRenamed.InstanceName = "alpha-renamed"

	_, _, fp := computeRowDelta([]qbittorrent.CrossInstanceTorrentView{rowA}, crossRowKey, crossRowFingerprint, nil)
	_, changedIdx, _ := computeRowDelta([]qbittorrent.CrossInstanceTorrentView{rowRenamed}, crossRowKey, crossRowFingerprint, fp)
	require.Equal(t, []int{0}, changedIdx, "an instance_name change must be detected as a changed row")

	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{Unified: true, InstanceIDs: []int{1}}}
	g.buildUpdatePayload(opts, crossResp(rowA), &StreamMeta{})
	delta := g.buildUpdatePayload(opts, crossResp(rowRenamed), &StreamMeta{})
	require.Equal(t, streamEventDelta, delta.Type)
	require.Len(t, delta.Data.CrossInstanceTorrents, 1, "metadata-only change still carries the row")
	require.Equal(t, "alpha-renamed", delta.Data.CrossInstanceTorrents[0].InstanceName)
}

func TestDeltaOmitsUnchangedCounts(t *testing.T) {
	counts := func(seeding int) *qbittorrent.TorrentCounts {
		return &qbittorrent.TorrentCounts{
			Status:     map[string]int{"all": 3, "seeding": seeding},
			Categories: map[string]int{"tv": 2, "movies": 1},
			Tags:       map[string]int{"cross-seed": 1},
			Trackers:   map[string]int{"tracker.example.invalid": 3},
			Total:      3,
		}
	}

	respWith := func(c *qbittorrent.TorrentCounts, rows ...qbittorrent.TorrentView) *qbittorrent.TorrentResponse {
		resp := singleResp(rows...)
		resp.Counts = c
		return resp
	}

	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{Unified: true, InstanceIDs: []int{1}}}

	full := g.buildUpdatePayload(opts, respWith(counts(2), tv("a", "A")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, full.Type)
	require.NotNil(t, full.Data.Counts, "the seeding snapshot always carries counts")

	unchanged := g.buildUpdatePayload(opts, respWith(counts(2), tv("a", "A-renamed")), &StreamMeta{})
	require.Equal(t, streamEventDelta, unchanged.Type)
	require.Nil(t, unchanged.Data.Counts, "counts identical to the last frame must be omitted")

	changed := g.buildUpdatePayload(opts, respWith(counts(3), tv("a", "A-renamed")), &StreamMeta{})
	require.Equal(t, streamEventDelta, changed.Type)
	require.NotNil(t, changed.Data.Counts, "changed counts must be sent")
	require.Equal(t, 3, changed.Data.Counts.Status["seeding"])
}

func TestInitUsesExactVersionedBaseline(t *testing.T) {
	counts := &qbittorrent.TorrentCounts{Status: map[string]int{"all": 1}, Total: 1}
	opts := StreamOptions{View: qbittorrent.TorrentListView{Unified: true, InstanceIDs: []int{1}}}
	g := &subscriptionGroup{version: StreamVersion{Major: 7}}

	seeded := singleResp(tv("a", "A"))
	seeded.Counts = counts
	first := g.buildInitPayload(opts, seeded, &StreamMeta{})
	require.Equal(t, seeded, first.Data)
	require.Equal(t, &StreamVersion{Major: 7, Minor: 1}, first.Version)

	joinerCandidate := singleResp(tv("a", "not-the-baseline"))
	joinerCandidate.Counts = &qbittorrent.TorrentCounts{Total: 9}
	joiner := g.buildInitPayload(opts, joinerCandidate, &StreamMeta{})
	require.Same(t, first.Data, joiner.Data)
	require.Equal(t, first.Version, joiner.Version)

	delta := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{})
	require.Equal(t, StreamVersion{Major: 7, Minor: 1}, delta.Delta.BaseVersion)
	require.Equal(t, &StreamVersion{Major: 7, Minor: 2}, delta.Version)
	require.Empty(t, delta.Data.Torrents)

	late := g.buildInitPayload(opts, singleResp(tv("a", "stale")), &StreamMeta{})
	require.Equal(t, &StreamVersion{Major: 7, Minor: 2}, late.Version)
	require.Equal(t, "A", late.Data.Torrents[0].Name)
	require.Equal(t, counts, late.Data.Counts)
}

func TestCountsEqual(t *testing.T) {
	build := func() *qbittorrent.TorrentCounts {
		c := &qbittorrent.TorrentCounts{
			Status:     map[string]int{},
			Categories: map[string]int{},
			Total:      9,
		}
		for i := range 50 {
			c.Status[fmt.Sprintf("s%02d", i)] = i
			c.Categories[fmt.Sprintf("c%02d", i)] = i * 2
		}
		return c
	}

	want := build()
	require.True(t, countsEqual(nil, nil))
	require.False(t, countsEqual(want, nil))
	require.False(t, countsEqual(nil, want))
	require.True(t, countsEqual(want, want))
	for range 3 {
		require.True(t, countsEqual(want, build()))
	}
}

func TestServerStatePayloadRebuildsWhenSubcategoryModeChanges(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{View: qbittorrent.TorrentListView{InstanceID: 1}}
	seed := singleResp(tv("a", "A"))
	seed.ServerState = &qbt.ServerState{DlInfoSpeed: 1}
	g.buildUpdatePayload(opts, seed, &StreamMeta{})

	same := g.buildServerStatePayload(&qbt.ServerState{DlInfoSpeed: 2}, nil, 0, &StreamMeta{})
	require.NotNil(t, same)
	require.Equal(t, int64(2), same.Data.ServerState.DlInfoSpeed)
	require.Equal(t, int64(2), g.baselineSnapshot.ServerState.DlInfoSpeed, "a joiner must get the new server state")
	require.Equal(t, same.Delta.BaseVersion.Minor+1, same.Version.Minor)

	require.Nil(t, g.buildServerStatePayload(&qbt.ServerState{DlInfoSpeed: 3, UseSubcategories: true}, nil, 0, &StreamMeta{}))
}
