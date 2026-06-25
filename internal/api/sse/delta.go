// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"encoding/json"
	"hash"
	"hash/fnv"
	"slices"
	"strconv"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// buildUpdatePayload turns a freshly materialized page into the frame to broadcast
// to the group, advancing the group's delta baseline as a side effect. It returns
// either a full snapshot ("update") or an incremental delta ("delta"):
//
//   - A full snapshot is sent only when the baseline is unseeded (the first tick for
//     a new group, including every reconnect that recreates a single-subscriber
//     group), which re-baselines every subscriber.
//   - Otherwise a delta is sent: the added or changed rows ride in the returned
//     payload's Data.Torrents (or Data.CrossInstanceTorrents), and StreamDelta.Order
//     carries the full page key order, present only when membership or ordering
//     changed. Aggregate metadata (stats, counts, serverState, ...) always travels
//     in full so dashboard speeds stay live even on a tick with no row changes.
//
// There is intentionally no periodic full keyframe: a recurring full page re-send is
// hundreds of KB and, over an HTTP/2 proxy, head-of-line-blocks every other request
// on the shared connection each time it fires. Correctness instead rests on
// init-seeds-baseline (the client's init equals the server baseline) plus a fresh
// init on every reconnect; the only drift window is the sub-millisecond gap between
// subscription registration and go-sse subscribe, which self-heals on the next
// reconnect.
//
// The caller holds no lock; the group's single-processor invariant (the sending
// flag) already serializes ticks, and baselineMu guards the baseline against the
// unrelated init path.
func (g *subscriptionGroup) buildUpdatePayload(opts StreamOptions, resp *qbittorrent.TorrentResponse, meta *StreamMeta) *StreamPayload {
	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()

	isCross := opts.isMultiInstance()

	var (
		order      []string
		changedIdx []int
		newFP      map[string]uint64
	)
	if isCross {
		order, changedIdx, newFP = computeRowDelta(resp.CrossInstanceTorrents, crossRowKey, crossRowFingerprint, g.baselineFP)
	} else {
		order, changedIdx, newFP = computeRowDelta(resp.Torrents, singleRowKey, singleRowFingerprint, g.baselineFP)
	}

	forceFull := !g.baselineSeeded

	// Advance the baseline before returning so the next tick diffs against this page.
	prevOrder := g.baselineOrder
	g.baselineFP = newFP
	g.baselineOrder = order
	g.baselineSeeded = true

	if forceFull {
		return &StreamPayload{Type: streamEventUpdate, Data: resp, Meta: meta}
	}

	// Shallow-copy the response so the delta frame keeps every aggregate field but
	// replaces the row slice with just the added/changed rows. Aggregate pointers are
	// shared read-only.
	deltaResp := *resp
	if isCross {
		deltaResp.CrossInstanceTorrents = subsetRows(resp.CrossInstanceTorrents, changedIdx)
		deltaResp.Torrents = nil
	} else {
		deltaResp.Torrents = subsetRows(resp.Torrents, changedIdx)
		deltaResp.CrossInstanceTorrents = nil
	}

	delta := &StreamDelta{}
	if !slices.Equal(order, prevOrder) {
		// Send the order even when empty (the page drained to zero rows): a pointer
		// keeps a present-but-empty order distinct from an absent one on the wire, so
		// the client clears instead of holding the deleted rows.
		delta.Order = &order
	}

	return &StreamPayload{Type: streamEventDelta, Data: &deltaResp, Delta: delta, Meta: meta}
}

// seedBaselineIfEmpty primes the delta baseline from a freshly built init snapshot,
// but only if the group has no baseline yet. Seeding at init makes the client's init
// snapshot and the server baseline identical, so the very next tick is a clean delta
// the client applies without drift. A later joiner to an already-seeded group does
// not re-seed (that would desync existing subscribers); its init may differ slightly
// from the shared baseline until it reconnects.
func (g *subscriptionGroup) seedBaselineIfEmpty(opts StreamOptions, resp *qbittorrent.TorrentResponse) {
	if resp == nil {
		return
	}

	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()

	if g.baselineSeeded {
		return
	}

	var (
		order []string
		fp    map[string]uint64
	)
	if opts.isMultiInstance() {
		order, _, fp = computeRowDelta(resp.CrossInstanceTorrents, crossRowKey, crossRowFingerprint, nil)
	} else {
		order, _, fp = computeRowDelta(resp.Torrents, singleRowKey, singleRowFingerprint, nil)
	}

	g.baselineFP = fp
	g.baselineOrder = order
	g.baselineSeeded = true
}

// computeRowDelta walks the freshly materialized rows in display order, producing
// the new ordered key list, the indices of rows that are new or whose change
// fingerprint differs from base, and the new key->fingerprint map to store as the
// next baseline.
func computeRowDelta[T any](rows []T, keyOf func(T) string, fpOf func(T) uint64, base map[string]uint64) (order []string, changedIdx []int, newFP map[string]uint64) {
	order = make([]string, len(rows))
	newFP = make(map[string]uint64, len(rows))
	for i := range rows {
		key := keyOf(rows[i])
		order[i] = key
		fp := fpOf(rows[i])
		newFP[key] = fp
		if old, ok := base[key]; !ok || old != fp {
			changedIdx = append(changedIdx, i)
		}
	}
	return order, changedIdx, newFP
}

// maskVolatile zeroes the per-second counters and swarm/peer-count jitter that change
// on an otherwise-idle torrent every tick. They are excluded from change detection
// only: on a mostly-idle instance these are the sole fields that move each tick, so
// hashing them would flag nearly every row as changed and make each "delta" almost
// as large as a full snapshot (the root cause of the stream saturating an HTTP/2
// connection). The excluded fields still ride along with their current value whenever
// a row is sent for a real change (speed, progress, state, ratio, ...); they are just
// not on their own a reason to resend a row.
func maskVolatile(t *qbt.Torrent) qbt.Torrent {
	masked := *t
	masked.Reannounce = 0
	masked.TimeActive = 0
	masked.SeedingTime = 0
	masked.ETA = 0
	masked.LastActivity = 0
	masked.SeenComplete = 0
	masked.Popularity = 0
	masked.Availability = 0
	masked.NumComplete = 0
	masked.NumIncomplete = 0
	masked.NumLeechs = 0
	masked.NumSeeds = 0
	return masked
}

func writeTorrentFingerprint(h hash.Hash64, t *qbt.Torrent) {
	if t == nil {
		return
	}
	masked := maskVolatile(t)
	if encoded, err := json.Marshal(&masked); err == nil {
		_, _ = h.Write(encoded)
	}
}

// singleRowFingerprint hashes a single-instance row's change-relevant content.
func singleRowFingerprint(tv qbittorrent.TorrentView) uint64 {
	h := fnv.New64a()
	writeTorrentFingerprint(h, tv.Torrent)
	_, _ = h.Write([]byte(tv.TrackerHealth))
	return h.Sum64()
}

// crossRowFingerprint hashes a cross-instance row's change-relevant content,
// including its instance identity (the same torrent on two instances is two rows).
func crossRowFingerprint(c qbittorrent.CrossInstanceTorrentView) uint64 {
	h := fnv.New64a()
	if c.TorrentView != nil {
		writeTorrentFingerprint(h, c.Torrent)
		_, _ = h.Write([]byte(c.TrackerHealth))
	}
	_, _ = h.Write([]byte(strconv.Itoa(c.InstanceID)))
	_, _ = h.Write([]byte(c.InstanceName))
	return h.Sum64()
}

// subsetRows returns the rows at the given indices, preserving order.
func subsetRows[T any](rows []T, idx []int) []T {
	if len(idx) == 0 {
		return nil
	}
	out := make([]T, 0, len(idx))
	for _, i := range idx {
		out = append(out, rows[i])
	}
	return out
}

// singleRowKey is a single-instance row's identity: its torrent hash.
func singleRowKey(tv qbittorrent.TorrentView) string {
	if tv.Torrent == nil {
		return ""
	}
	return tv.Hash
}

// crossRowKey is a cross-instance row's identity: "<instanceID>:<hash>". The same
// torrent cross-seeded onto two instances shares a hash but is two distinct rows,
// so the instance id must be part of the key. Mirrors the frontend's crossInstanceRowKey.
func crossRowKey(c qbittorrent.CrossInstanceTorrentView) string {
	hash := ""
	// Guard the embedded *TorrentView pointer before reading the promoted Hash, which
	// resolves through it (a nil TorrentView would panic on c.Hash).
	if c.TorrentView != nil && c.Torrent != nil {
		hash = c.Hash
	}
	return strconv.Itoa(c.InstanceID) + ":" + hash
}
