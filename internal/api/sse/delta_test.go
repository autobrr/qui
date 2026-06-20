// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"encoding/json"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// tv builds a single-instance row with a hash and a name; the name drives the
// content fingerprint so tests can flip a row "changed" by changing its name.
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
	now := time.Now()

	// First tick on an unseeded group is a full snapshot that seeds the baseline.
	full := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{}, now)
	require.Equal(t, streamEventUpdate, full.Type)
	require.Nil(t, full.Delta)
	require.Len(t, full.Data.Torrents, 3)
	require.True(t, g.baselineSeeded)

	// No change: an aggregate-only delta with no changed rows and no order.
	steady := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{}, now.Add(2*time.Second))
	require.Equal(t, streamEventDelta, steady.Type)
	require.Empty(t, steady.Data.Torrents)
	require.Nil(t, steady.Delta.Order)
	require.Equal(t, 3, steady.Data.Total, "aggregate total still reflects the full page")

	// One row's content changes: it rides in the delta, order stays implicit.
	changed := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B2"), tv("c", "C")), &StreamMeta{}, now.Add(4*time.Second))
	require.Equal(t, streamEventDelta, changed.Type)
	require.Equal(t, []string{"b"}, changedHashes(changed))
	require.Nil(t, changed.Delta.Order, "order is omitted when only values change")
}

func TestBuildUpdatePayloadAddSendsOrderAndRow(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}
	now := time.Now()

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B")), &StreamMeta{}, now)

	added := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("d", "D")), &StreamMeta{}, now.Add(2*time.Second))
	require.Equal(t, streamEventDelta, added.Type)
	require.NotNil(t, added.Delta.Order)
	require.Equal(t, []string{"a", "b", "d"}, *added.Delta.Order, "membership change sends full order")
	require.Equal(t, []string{"d"}, changedHashes(added), "only the new row is carried")
}

func TestBuildUpdatePayloadRemoveSendsShorterOrder(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}
	now := time.Now()

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{}, now)

	removed := g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("c", "C")), &StreamMeta{}, now.Add(2*time.Second))
	require.Equal(t, streamEventDelta, removed.Type)
	require.NotNil(t, removed.Delta.Order)
	require.Equal(t, []string{"a", "c"}, *removed.Delta.Order)
	require.Empty(t, changedHashes(removed), "surviving unchanged rows are not re-sent")
}

func TestBuildUpdatePayloadReorderSendsOrderOnly(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}
	now := time.Now()

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B"), tv("c", "C")), &StreamMeta{}, now)

	reordered := g.buildUpdatePayload(opts, singleResp(tv("c", "C"), tv("b", "B"), tv("a", "A")), &StreamMeta{}, now.Add(2*time.Second))
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
	now := time.Now()

	g.buildUpdatePayload(opts, singleResp(tv("a", "A"), tv("b", "B")), &StreamMeta{}, now)

	cleared := g.buildUpdatePayload(opts, singleResp(), &StreamMeta{}, now.Add(2*time.Second))
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

func TestBuildUpdatePayloadKeyframeReissuesFullSnapshot(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceID: 1}
	now := time.Now()

	g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{}, now)

	// A tick within the keyframe window stays a delta.
	within := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{}, now.Add(deltaKeyframeInterval-time.Second))
	require.Equal(t, streamEventDelta, within.Type)

	// A tick at/after the keyframe interval re-sends the whole page to re-baseline.
	keyframe := g.buildUpdatePayload(opts, singleResp(tv("a", "A")), &StreamMeta{}, now.Add(deltaKeyframeInterval))
	require.Equal(t, streamEventUpdate, keyframe.Type)
	require.Nil(t, keyframe.Delta)
	require.Len(t, keyframe.Data.Torrents, 1)
}

func TestBuildUpdatePayloadCrossInstance(t *testing.T) {
	g := &subscriptionGroup{}
	opts := StreamOptions{InstanceIDs: []int{1, 2}}
	now := time.Now()

	// Seed: same hash on two instances are distinct rows keyed by instanceId:hash.
	full := g.buildUpdatePayload(opts, crossResp(ci(1, "a", "A"), ci(2, "a", "A")), &StreamMeta{}, now)
	require.Equal(t, streamEventUpdate, full.Type)
	require.Len(t, full.Data.CrossInstanceTorrents, 2)

	// Change only instance 2's copy: just that row rides in the delta.
	changed := g.buildUpdatePayload(opts, crossResp(ci(1, "a", "A"), ci(2, "a", "A2")), &StreamMeta{}, now.Add(2*time.Second))
	require.Equal(t, streamEventDelta, changed.Type)
	require.Nil(t, changed.Data.Torrents, "single-instance slice stays empty on a cross-instance delta")
	require.Len(t, changed.Data.CrossInstanceTorrents, 1)
	require.Equal(t, 2, changed.Data.CrossInstanceTorrents[0].InstanceID)
	require.Nil(t, changed.Delta.Order, "value-only change keeps order implicit")
}

func TestComputeRowDeltaFlagsNewAndChangedRows(t *testing.T) {
	base := map[string]uint64{}
	rows := []qbittorrent.TorrentView{tv("a", "A"), tv("b", "B")}

	order, changedIdx, fp := computeRowDelta(rows, singleRowKey, base)
	require.Equal(t, []string{"a", "b"}, order)
	require.Equal(t, []int{0, 1}, changedIdx, "every row is new against an empty baseline")
	require.Len(t, fp, 2)

	// Re-diff identical rows against the produced fingerprints: nothing changed.
	order2, changedIdx2, fp2 := computeRowDelta(rows, singleRowKey, fp)
	require.Equal(t, []string{"a", "b"}, order2)
	require.Empty(t, changedIdx2)
	require.Equal(t, fp, fp2, "fingerprints are stable for unchanged content")

	// Flip one row's content: only that index is flagged.
	mutated := []qbittorrent.TorrentView{tv("a", "A"), tv("b", "B-new")}
	_, changedIdx3, _ := computeRowDelta(mutated, singleRowKey, fp)
	require.Equal(t, []int{1}, changedIdx3)
}
