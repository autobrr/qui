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

// tv builds a single-instance row with a hash and a name; the name drives the
// change fingerprint so tests can flip a row "changed" by changing its name.
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
	opts := StreamOptions{InstanceID: 1}

	// First tick on an unseeded group is a full snapshot that seeds the baseline.
	full := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, full.Type)
	require.Nil(t, full.Delta)
	require.Len(t, full.Data.Torrents, 3)
	require.NotNil(t, g.baselineSnapshot)

	// No change: an aggregate-only delta with no changed rows and no order.
	steady := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventDelta, steady.Type)
	require.Empty(t, steady.Data.Torrents)
	require.Nil(t, steady.Delta.Order)
	require.Equal(t, 3, steady.Data.Total, "aggregate total still reflects the full page")

	// One row's content changes: it rides in the delta, order stays implicit.
	changed := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B2"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventDelta, changed.Type)
	require.Equal(t, []string{"b"}, changedHashes(changed))
	require.Nil(t, changed.Delta.Order, "order is omitted when only values change")
}

func TestBuildUpdatePayloadSendsPreferencesOnlyWhenTheyChange(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	withPrefs := func(prefs string) *qbittorrent.TorrentResponse {
		resp := singleResp(tv("a", "A"))
		resp.AppPreferences = json.RawMessage(prefs)
		return resp
	}

	full := g.buildUpdatePayload(opts, withPrefs(`{"dht":true}`), &StreamMeta{})
	require.JSONEq(t, `{"dht":true}`, string(full.Data.AppPreferences), "the seeding snapshot always carries preferences")

	steady := g.buildUpdatePayload(opts, withPrefs(`{"dht":true}`), &StreamMeta{})
	require.Nil(t, steady.Data.AppPreferences, "unchanged preferences are dropped from the delta")

	edited := g.buildUpdatePayload(opts, withPrefs(`{"dht":false}`), &StreamMeta{})
	require.JSONEq(t, `{"dht":false}`, string(edited.Data.AppPreferences), "an edit is sent")

	again := g.buildUpdatePayload(opts, withPrefs(`{"dht":false}`), &StreamMeta{})
	require.Nil(t, again.Data.AppPreferences, "the baseline advanced to the edited value")

	// Explicit null is the tri-state that clears the client's cached copy, so it
	// must survive the dedup even though the previous value was a real object.
	cleared := g.buildUpdatePayload(opts, withPrefs(`null`), &StreamMeta{})
	require.Equal(t, "null", string(cleared.Data.AppPreferences), "an explicit null still goes out")

	// The response the caller handed us must not be mutated: it is shared with
	// other subscribers and with the REST cache.
	shared := withPrefs(`null`)
	_ = g.buildUpdatePayload(opts, shared, &StreamMeta{})
	require.Equal(t, "null", string(shared.AppPreferences), "dedup only edits the delta copy")
}

func TestBuildUpdatePayloadAddSendsOrderAndRow(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B")), &StreamMeta{})

	added := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("d", "D")), &StreamMeta{})
	require.Equal(t, streamEventDelta, added.Type)
	require.NotNil(t, added.Delta.Order)
	require.Equal(t, []string{"a", "b", "d"}, *added.Delta.Order, "membership change sends full order")
	require.Equal(t, []string{"d"}, changedHashes(added), "only the new row is carried")
}

func TestBuildUpdatePayloadRemoveSendsShorterOrder(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})

	removed := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("c", "C")), &StreamMeta{})
	require.Equal(t, streamEventDelta, removed.Type)
	require.NotNil(t, removed.Delta.Order)
	require.Equal(t, []string{"a", "c"}, *removed.Delta.Order)
	require.Empty(t, changedHashes(removed), "surviving unchanged rows are not re-sent")
}

func TestBuildUpdatePayloadReorderSendsOrderOnly(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{})

	reordered := g.buildUpdatePayload(opts, singleResp(tv("c", "C"), tv("b", "B"), tv("a", "A")), &StreamMeta{})
	require.Equal(t, streamEventDelta, reordered.Type)
	require.NotNil(t, reordered.Delta.Order)
	require.Equal(t, []string{"c", "b", "a"}, *reordered.Delta.Order)
	require.Empty(t, changedHashes(reordered), "a pure reorder carries no row payloads")
}

// TestBuildUpdatePayloadEmptiedPageSendsPresentEmptyOrder guards the N->0 clear:
// when the page drains to zero rows, the delta must carry a present-but-empty order
// that survives JSON marshaling, otherwise the client cannot distinguish a full
// clear from an aggregate-only tick and leaves the deleted rows on screen.
func TestBuildUpdatePayloadEmptiedPageSendsPresentEmptyOrder(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B")), &StreamMeta{})

	cleared := g.buildUpdatePayload(opts, singleResp(), &StreamMeta{})
	require.Equal(t, streamEventDelta, cleared.Type)
	require.NotNil(t, cleared.Delta.Order, "an emptied page must send a present order, not omit it")
	require.Empty(t, *cleared.Delta.Order)
	require.Empty(t, changedHashes(cleared))

	// The present-but-empty order must serialize on the wire (not be dropped by
	// omitempty), so the client sees `"order":[]` and clears.
	encoded, err := json.Marshal(cleared)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"order":[]`)
}

// TestBuildUpdatePayloadHasNoPeriodicKeyframe verifies that after the seed there is
// no periodic full re-send. A recurring full page would head-of-line-block every
// other request on a shared HTTP/2 connection each time it fired.
func TestBuildUpdatePayloadHasNoPeriodicKeyframe(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	first := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, first.Type, "first tick seeds with a full snapshot")

	for range 200 {
		p := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{})
		require.Equal(t, streamEventDelta, p.Type, "every later tick stays a delta")
	}
}

// TestVolatileFieldsDoNotTriggerResend is the core of the streaming fix: a torrent
// whose only change is a per-second counter (reannounce/time_active/...) or peer
// jitter must NOT be re-sent, otherwise a mostly-idle page re-sends nearly every row
// every tick and the "delta" is almost a full snapshot.
func TestVolatileFieldsDoNotTriggerResend(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}

	base := &qbt.Torrent{Hash: "a", Name: "A", Reannounce: 1800, TimeActive: 100, SeedingTime: 100, NumSeeds: 3}
	g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: base}), &StreamMeta{})

	// Only volatile fields move (counters tick, peer count jitters): no resend.
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

	// A real change (download speed) does trigger a resend.
	active := ticked
	active.DlSpeed = 1024
	sent := g.buildUpdatePayload(opts, singleResp(qbittorrent.TorrentView{Torrent: &active}), &StreamMeta{})
	require.Equal(t, []string{"a"}, changedHashes(sent), "a real change (speed) must re-send the row")
}

func TestBuildUpdatePayloadCrossInstance(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceIDs: []int{1, 2}}

	// Seed: same hash on two instances are distinct rows keyed by instanceId:hash.
	full := g.buildUpdatePayload(opts, crossResp(ci(1, "a", "A"), ci(2, "a", "A")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, full.Type)
	require.Len(t, full.Data.CrossInstanceTorrents, 2)

	// Change only instance 2's copy: just that row rides in the delta.
	changed := g.buildUpdatePayload(opts, crossResp(ci(1, "a", "A"), ci(2, "a", "A2")), &StreamMeta{})
	require.Equal(t, streamEventDelta, changed.Type)
	require.Nil(t, changed.Data.Torrents, "single-instance slice stays empty on a cross-instance delta")
	require.Len(t, changed.Data.CrossInstanceTorrents, 1)
	require.Equal(t, 2, changed.Data.CrossInstanceTorrents[0].InstanceID)
	require.Nil(t, changed.Delta.Order, "value-only change keeps order implicit")
}

// TestCrossInstanceMetadataChangeIsFlaggedAsChanged confirms a table-visible
// cross-instance field (instance name) is part of the change fingerprint, so a
// metadata change re-sends the row rather than hiding behind an aggregate-only tick.
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
	opts := StreamOptions{InstanceIDs: []int{1}}
	g.buildUpdatePayload(opts, crossResp(rowA), &StreamMeta{})
	delta := g.buildUpdatePayload(opts, crossResp(rowRenamed), &StreamMeta{})
	require.Equal(t, streamEventDelta, delta.Type)
	require.Len(t, delta.Data.CrossInstanceTorrents, 1, "metadata-only change still carries the row")
	require.Equal(t, "alpha-renamed", delta.Data.CrossInstanceTorrents[0].InstanceName)
}

// TestDeltaOmitsUnchangedCounts pins the sidebar-counts dedup: counts are several
// KB per tick and only move when the library does, so an unchanged tick must drop
// the field and a changed one must carry it.
func TestDeltaOmitsUnchangedCounts(t *testing.T) {
	counts := func(seeding int) *qbittorrent.TorrentCounts {
		return &qbittorrent.TorrentCounts{
			Status:     map[string]int{"all": 3, "seeding": seeding},
			Categories: map[string]int{"tv": 2, "movies": 1},
			Tags:       map[string]int{"cross-seed": 1},
			Trackers:   map[string]int{"tracker.example.invalid": 3},
			TrackerTransfers: map[string]qbittorrent.TrackerTransferStats{
				"tracker.example.invalid": {Uploaded: 100, Downloaded: 200, Count: 3},
			},
			Total: 3,
		}
	}

	respWith := func(c *qbittorrent.TorrentCounts, rows ...qbittorrent.TorrentView) *qbittorrent.TorrentResponse {
		resp := singleResp(rows...)
		resp.Counts = c
		return resp
	}

	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceIDs: []int{1}}

	full := g.buildUpdatePayload(opts, respWith(counts(2), tv("a", "A")), &StreamMeta{})
	require.Equal(t, streamEventUpdate, full.Type)
	require.NotNil(t, full.Data.Counts, "the seeding snapshot always carries counts")

	// Same counts, and a row changed so this is a real delta frame.
	unchanged := g.buildUpdatePayload(opts, respWith(counts(2), tv("a", "A-renamed")), &StreamMeta{})
	require.Equal(t, streamEventDelta, unchanged.Type)
	require.Nil(t, unchanged.Data.Counts, "counts identical to the last frame must be omitted")

	// A counts change must put the field back on the wire.
	changed := g.buildUpdatePayload(opts, respWith(counts(3), tv("a", "A-renamed")), &StreamMeta{})
	require.Equal(t, streamEventDelta, changed.Type)
	require.NotNil(t, changed.Data.Counts, "changed counts must be sent")
	require.Equal(t, 3, changed.Data.Counts.Status["seeding"])
}

// TestInitUsesExactVersionedBaseline pins continuity for late joiners. Once a
// group has a baseline, an independently materialized candidate cannot replace or
// diverge from it; every init identifies the exact retained frame version.
func TestInitUsesExactVersionedBaseline(t *testing.T) {
	counts := &qbittorrent.TorrentCounts{Status: map[string]int{"all": 1}, Total: 1}
	prefs := json.RawMessage(`{"max_ratio":2}`)
	opts := StreamOptions{InstanceIDs: []int{1}}
	g := &subscriptionGroup{version: StreamVersion{Major: 7}}

	seeded := singleResp(tv("a", "A"))
	seeded.Counts = counts
	seeded.AppPreferences = prefs
	first := g.buildInitPayload(opts, seeded, &StreamMeta{})
	require.Same(t, seeded, first.Data)
	require.Equal(t, &StreamVersion{Major: 7, Minor: 1}, first.Version)

	// This candidate was built later but does not represent the group's baseline.
	joinerCandidate := singleResp(tv("a", "not-the-baseline"))
	joinerCandidate.Counts = &qbittorrent.TorrentCounts{Total: 9}
	joiner := g.buildInitPayload(opts, joinerCandidate, &StreamMeta{})
	require.Same(t, seeded, joiner.Data)
	require.Equal(t, first.Version, joiner.Version)

	// Even an aggregate-only frame advances Minor. Its retained full snapshot keeps
	// edge-triggered fields that were omitted from the delta on the wire.
	delta := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{})
	require.Equal(t, StreamVersion{Major: 7, Minor: 1}, delta.Delta.BaseVersion)
	require.Equal(t, &StreamVersion{Major: 7, Minor: 2}, delta.Version)
	require.Empty(t, delta.Data.Torrents)

	late := g.buildInitPayload(opts, singleResp(tv("a", "stale")), &StreamMeta{})
	require.Equal(t, &StreamVersion{Major: 7, Minor: 2}, late.Version)
	require.Equal(t, "A", late.Data.Torrents[0].Name)
	require.Equal(t, counts, late.Data.Counts)
	require.Equal(t, prefs, late.Data.AppPreferences)
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

func TestComputeRowDeltaFlagsNewAndChangedRows(t *testing.T) {
	base := map[string]uint64{}
	rows := []qbittorrent.TorrentView{tv("a", "A"), tv("b", "B")}

	order, changedIdx, fp := computeRowDelta(rows, singleRowKey, singleRowFingerprint, base)
	require.Equal(t, []string{"a", "b"}, order)
	require.Equal(t, []int{0, 1}, changedIdx, "every row is new against an empty baseline")
	require.Len(t, fp, 2)

	// Re-diff identical rows against the produced fingerprints: nothing changed.
	order2, changedIdx2, fp2 := computeRowDelta(rows, singleRowKey, singleRowFingerprint, fp)
	require.Equal(t, []string{"a", "b"}, order2)
	require.Empty(t, changedIdx2)
	require.Equal(t, fp, fp2, "fingerprints are stable for unchanged content")

	// Flip one row's content: only that index is flagged.
	mutated := []qbittorrent.TorrentView{tv("a", "A"), tv("b", "B-new")}
	_, changedIdx3, _ := computeRowDelta(mutated, singleRowKey, singleRowFingerprint, fp)
	require.Equal(t, []int{1}, changedIdx3)
}
