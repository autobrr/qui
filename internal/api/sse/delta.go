// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"encoding/json"
	"hash/fnv"
	"slices"
	"strconv"
	"time"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// deltaKeyframeInterval bounds how long a group may go without a full snapshot.
// Tick updates are normally incremental deltas, but at least this often the group
// re-sends the whole page as a full "update" frame. This keyframe re-baselines any
// client whose state drifted from the server baseline (a frame missed in the tiny
// window between subscription registration and go-sse subscribe, or a late joiner
// that inited against a slightly different page than the shared baseline). Drift is
// therefore always self-healing within this bound without forcing a reconnect.
const deltaKeyframeInterval = 30 * time.Second

// buildUpdatePayload turns a freshly materialized page into the frame to broadcast
// to the group, advancing the group's delta baseline as a side effect. It returns
// either a full snapshot ("update") or an incremental delta ("delta"):
//
//   - A full snapshot is sent when the baseline is unseeded (first tick for a new
//     group, including every reconnect that recreates a single-subscriber group) or
//     when the keyframe interval has elapsed. Either re-baselines every subscriber.
//   - Otherwise a delta is sent: the added or changed rows ride in the returned
//     payload's Data.Torrents (or Data.CrossInstanceTorrents), and StreamDelta.Order
//     carries the full page key order, present only when membership or ordering
//     changed. Aggregate metadata (stats, counts, serverState, ...) always travels
//     in full so dashboard speeds stay live even on a tick with no row changes.
//
// now is injected for deterministic keyframe testing. The caller holds no lock; the
// group's single-processor invariant (the sending flag) already serializes ticks,
// and baselineMu guards the baseline against the unrelated init path.
func (g *subscriptionGroup) buildUpdatePayload(opts StreamOptions, resp *qbittorrent.TorrentResponse, meta *StreamMeta, now time.Time) *StreamPayload {
	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()

	isCross := opts.isMultiInstance()

	var (
		order      []string
		changedIdx []int
		newFP      map[string]uint64
	)
	if isCross {
		order, changedIdx, newFP = computeRowDelta(resp.CrossInstanceTorrents, crossRowKey, g.baselineFP)
	} else {
		order, changedIdx, newFP = computeRowDelta(resp.Torrents, singleRowKey, g.baselineFP)
	}

	forceFull := !g.baselineSeeded || now.Sub(g.lastFullAt) >= deltaKeyframeInterval

	// Advance the baseline before returning so the next tick diffs against this page,
	// regardless of whether this frame is a delta or a keyframe.
	prevOrder := g.baselineOrder
	g.baselineFP = newFP
	g.baselineOrder = order
	g.baselineSeeded = true

	if forceFull {
		g.lastFullAt = now
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
		// the client clears instead of holding the deleted rows until the next keyframe.
		delta.Order = &order
	}

	return &StreamPayload{Type: streamEventDelta, Data: &deltaResp, Delta: delta, Meta: meta}
}

// seedBaselineIfEmpty primes the delta baseline from a freshly built init snapshot,
// but only if the group has no baseline yet. Seeding at init makes the client's init
// snapshot and the server baseline identical, so the very next tick is a clean delta
// the client applies without drift. A later joiner to an already-seeded group does
// not re-seed (that would desync existing subscribers); its init may differ slightly
// from the shared baseline until the next keyframe re-baselines everyone.
func (g *subscriptionGroup) seedBaselineIfEmpty(opts StreamOptions, resp *qbittorrent.TorrentResponse, now time.Time) {
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
		order, _, fp = computeRowDelta(resp.CrossInstanceTorrents, crossRowKey, nil)
	} else {
		order, _, fp = computeRowDelta(resp.Torrents, singleRowKey, nil)
	}

	g.baselineFP = fp
	g.baselineOrder = order
	g.baselineSeeded = true
	g.lastFullAt = now
}

// computeRowDelta walks the freshly materialized rows in display order, producing
// the new ordered key list, the indices of rows that are new or whose content
// changed since base, and the new key->fingerprint map to store as the next
// baseline. A row is "changed" when its key is absent from base or its fingerprint
// differs.
func computeRowDelta[T any](rows []T, keyOf func(T) string, base map[string]uint64) (order []string, changedIdx []int, newFP map[string]uint64) {
	order = make([]string, len(rows))
	newFP = make(map[string]uint64, len(rows))
	for i := range rows {
		key := keyOf(rows[i])
		order[i] = key
		fp := fingerprintRow(rows[i])
		newFP[key] = fp
		if old, ok := base[key]; !ok || old != fp {
			changedIdx = append(changedIdx, i)
		}
	}
	return order, changedIdx, newFP
}

// fingerprintRow hashes a row's canonical JSON so two ticks can cheaply tell
// whether a row's content changed. TorrentView/CrossInstanceTorrentView are plain
// structs (no maps), so their marshaled form is deterministic across ticks. A
// marshal error (not expected for these types) yields 0, which simply makes the row
// compare equal to another unmarshalable row; the worst case is one tick of
// staleness, self-healed by the next change or keyframe.
func fingerprintRow[T any](row T) uint64 {
	encoded, err := json.Marshal(row)
	if err != nil {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write(encoded)
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
