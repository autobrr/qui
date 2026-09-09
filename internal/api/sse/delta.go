// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"maps"
	"math"
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
// on the shared connection each time it fires. Instead, every frame advances an
// explicit sequence and the complete reconstructed snapshot is retained for new
// subscribers. Clients can therefore detect a missed delta and reconnect for an
// exact init without periodically paying for a full page.
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

	forceFull := g.baselineSnapshot == nil
	baseVersion := g.version

	// Nil edge-triggered fields mean "no update", not "clear". Reconstruct the
	// complete snapshot represented by this frame before retaining it for joiners.
	// Explicit JSON null remains non-nil and therefore still clears preferences.
	snapshot := *resp
	if g.baselineSnapshot != nil {
		if snapshot.AppPreferences == nil {
			snapshot.AppPreferences = g.baselineSnapshot.AppPreferences
		}
		if snapshot.Counts == nil {
			snapshot.Counts = g.baselineSnapshot.Counts
		}
	}

	// Advance the baseline before returning so the next tick diffs against this page.
	prevOrder := g.baselineOrder
	var prevPrefs json.RawMessage
	var prevCounts *qbittorrent.TorrentCounts
	if g.baselineSnapshot != nil {
		prevPrefs = g.baselineSnapshot.AppPreferences
		prevCounts = g.baselineSnapshot.Counts
	}
	g.baselineFP = newFP
	g.baselineOrder = order
	g.baselineSnapshot = &snapshot
	g.version.Minor++

	if forceFull {
		return &StreamPayload{
			Type:    streamEventUpdate,
			Data:    &snapshot,
			Meta:    meta,
			Version: new(g.version),
		}
	}

	// Shallow-copy the response so the delta frame keeps every aggregate field but
	// replaces the row slice with just the added/changed rows. Aggregate pointers are
	// shared read-only.
	deltaResp := *resp

	// ~4.7 KB and edited rarely; the client keeps its cached copy when the field is absent.
	if bytes.Equal(snapshot.AppPreferences, prevPrefs) {
		deltaResp.AppPreferences = nil
	} else {
		deltaResp.AppPreferences = snapshot.AppPreferences
	}

	// Several KB per tick on an instance with many categories, tags and trackers,
	// and unchanged whenever the library is; the client keeps its cached copy.
	if countsEqual(snapshot.Counts, prevCounts) {
		deltaResp.Counts = nil
	} else {
		deltaResp.Counts = snapshot.Counts
	}

	if isCross {
		deltaResp.CrossInstanceTorrents = subsetRows(resp.CrossInstanceTorrents, changedIdx)
		deltaResp.Torrents = nil
	} else {
		deltaResp.Torrents = subsetRows(resp.Torrents, changedIdx)
		deltaResp.CrossInstanceTorrents = nil
	}

	delta := &StreamDelta{BaseVersion: baseVersion}
	if !slices.Equal(order, prevOrder) {
		// Send the order even when empty (the page drained to zero rows): a pointer
		// keeps a present-but-empty order distinct from an absent one on the wire, so
		// the client clears instead of holding the deleted rows.
		delta.Order = &order
	}

	return &StreamPayload{
		Type:    streamEventDelta,
		Data:    &deltaResp,
		Delta:   delta,
		Meta:    meta,
		Version: new(g.version),
	}
}

// buildInitPayload returns the exact complete snapshot represented by the group's
// current baseline. The first init seeds that baseline from resp; later callers
// ignore independently materialized rows so they cannot join halfway through a
// delta sequence with a different page. A nil result means the group is unseeded
// and no candidate snapshot was supplied.
func (g *subscriptionGroup) buildInitPayload(opts StreamOptions, resp *qbittorrent.TorrentResponse, meta *StreamMeta) *StreamPayload {
	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()

	if g.baselineSnapshot != nil {
		return &StreamPayload{
			Type:    streamEventInit,
			Data:    g.baselineSnapshot,
			Meta:    meta,
			Version: new(g.version),
		}
	}
	if resp == nil {
		return nil
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
	g.baselineSnapshot = resp
	g.version.Minor++

	return &StreamPayload{
		Type:    streamEventInit,
		Data:    g.baselineSnapshot,
		Meta:    meta,
		Version: new(g.version),
	}
}

// computeRowDelta walks the freshly materialized rows in display order, producing
// the new ordered key list, the indices of rows that are new or whose change
// fingerprint differs from base, and the new key->fingerprint map to store as the
// next baseline.
func computeRowDelta[T any](rows []T, keyOf func(T) string, fpOf func(*fpBuf, T) uint64, base map[string]uint64) (order []string, changedIdx []int, newFP map[string]uint64) {
	order = make([]string, len(rows))
	newFP = make(map[string]uint64, len(rows))
	// One buffer for the whole page: the hash is consumed per row, so nothing
	// outlives an iteration.
	buf := make(fpBuf, 0, fpBufSize)
	for i := range rows {
		key := keyOf(rows[i])
		order[i] = key
		fp := fpOf(&buf, rows[i])
		newFP[key] = fp
		if old, ok := base[key]; !ok || old != fp {
			changedIdx = append(changedIdx, i)
		}
	}
	return order, changedIdx, newFP
}

// fpBuf accumulates a row's change-relevant fields as flat bytes so the row is
// hashed with a single FNV write instead of a JSON encode. Strings carry a length
// prefix so adjacent fields cannot alias ("ab"+"c" vs "a"+"bc").
type fpBuf []byte

// fpBufSize covers a typical row (fixed fields plus name and paths).
const fpBufSize = 1024

func (b *fpBuf) i64(v int64)   { *b = binary.LittleEndian.AppendUint64(*b, uint64(v)) }
func (b *fpBuf) f64(v float64) { *b = binary.LittleEndian.AppendUint64(*b, math.Float64bits(v)) }
func (b *fpBuf) str(s string)  { b.i64(int64(len(s))); *b = append(*b, s...) }
func (b *fpBuf) bit(v bool) {
	if v {
		*b = append(*b, 1)
	} else {
		*b = append(*b, 0)
	}
}

// torrent appends every change-relevant torrent field. The per-second counters and
// swarm/peer-count jitter that move on an otherwise-idle torrent every tick
// (Reannounce, TimeActive, SeedingTime, ETA, LastActivity, SeenComplete, Popularity,
// Availability, NumComplete, NumIncomplete, NumLeechs, NumSeeds) are deliberately
// left out: on a mostly-idle instance they are the sole fields that change each tick,
// so including them would flag nearly every row as changed and make each "delta"
// almost as large as a full snapshot (the root cause of the stream saturating an
// HTTP/2 connection). The excluded fields still ride along with their current value
// whenever a row is sent for a real change; they are just not on their own a reason
// to resend a row. MagnetURI and HasMetadata are also left out, for a different
// reason: TorrentView shadows them out of the serialized payload entirely, so
// changes to them have nothing to update and must not resend a row (issue #2328).
// TestTorrentFingerprintCoversEveryField pins this partition, so a
// go-qbittorrent bump that adds a field fails until the field is categorized here.
func (b *fpBuf) torrent(t *qbt.Torrent) {
	if t == nil {
		return
	}
	b.i64(t.AddedOn)
	b.i64(t.AmountLeft)
	b.bit(t.AutoManaged)
	b.str(t.Category)
	b.str(t.Comment)
	b.i64(t.Completed)
	b.i64(t.CompletionOn)
	b.str(t.CreatedBy)
	b.str(t.ContentPath)
	b.i64(t.DlLimit)
	b.i64(t.DlSpeed)
	b.str(t.DownloadPath)
	b.i64(t.Downloaded)
	b.i64(t.DownloadedSession)
	b.bit(t.FirstLastPiecePrio)
	b.bit(t.ForceStart)
	b.str(t.Hash)
	b.str(t.InfohashV1)
	b.str(t.InfohashV2)
	b.bit(t.Private)
	b.f64(t.MaxRatio)
	b.i64(t.MaxSeedingTime)
	b.i64(t.MaxInactiveSeedingTime)
	b.str(t.Name)
	b.i64(t.Priority)
	b.f64(t.Progress)
	b.f64(t.Ratio)
	b.f64(t.RatioLimit)
	b.str(t.SavePath)
	b.i64(t.SeedingTimeLimit)
	b.i64(t.InactiveSeedingTimeLimit)
	b.str(t.ShareLimitAction)
	b.str(t.ShareLimitsMode)
	b.bit(t.SequentialDownload)
	b.i64(t.Size)
	b.str(string(t.State))
	b.bit(t.SuperSeeding)
	b.str(t.Tags)
	b.i64(t.TotalSize)
	b.str(t.Tracker)
	b.i64(t.TrackersCount)
	b.i64(t.UpLimit)
	b.i64(t.Uploaded)
	b.i64(t.UploadedSession)
	b.i64(t.UpSpeed)
	b.i64(int64(len(t.Trackers)))
	for i := range t.Trackers {
		tr := &t.Trackers[i]
		b.str(tr.Url)
		b.i64(int64(tr.Status))
		b.i64(int64(tr.NumPeers))
		b.i64(int64(tr.NumSeeds))
		b.i64(int64(tr.NumLeechers))
		b.i64(int64(tr.NumDownloaded))
		b.str(tr.Message)
	}
}

func (b fpBuf) sum() uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// singleRowFingerprint hashes a single-instance row's change-relevant content.
// The caller owns the buffer so a page of rows hashes with one allocation
// instead of one per row; it is reset here, so callers may pass a zero fpBuf.
func singleRowFingerprint(b *fpBuf, tv qbittorrent.TorrentView) uint64 {
	*b = (*b)[:0]
	b.torrent(tv.Torrent)
	b.str(string(tv.TrackerHealth))
	return b.sum()
}

// crossRowFingerprint hashes a cross-instance row's change-relevant content,
// including its instance identity (the same torrent on two instances is two rows).
func crossRowFingerprint(b *fpBuf, c qbittorrent.CrossInstanceTorrentView) uint64 {
	*b = (*b)[:0]
	if c.TorrentView != nil {
		b.torrent(c.Torrent)
		b.str(string(c.TrackerHealth))
	}
	b.i64(int64(c.InstanceID))
	b.str(c.InstanceName)
	return b.sum()
}

// countsEqual compares read-only snapshots without allocating or sorting map keys.
func countsEqual(a, b *qbittorrent.TorrentCounts) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Total == b.Total &&
		maps.Equal(a.Status, b.Status) &&
		maps.Equal(a.Categories, b.Categories) &&
		maps.Equal(a.CategorySizes, b.CategorySizes) &&
		maps.Equal(a.Tags, b.Tags) &&
		maps.Equal(a.TagSizes, b.TagSizes) &&
		maps.Equal(a.Trackers, b.Trackers) &&
		maps.Equal(a.TrackerTransfers, b.TrackerTransfers)
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
