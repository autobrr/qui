// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"encoding/binary"
	"hash/fnv"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/qbittorrent"
)

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
		order, changedIdx, newFP = computeRowDelta(resp.Torrents, singleRowKey, singleRowFingerprintFor(opts), g.baselineFP)
	}

	forceFull := g.baselineSnapshot == nil
	baseVersion := g.version

	snapshot := *resp
	detachSnapshotRows(&snapshot)
	if g.baselineSnapshot != nil && snapshot.Counts == nil {
		snapshot.Counts = g.baselineSnapshot.Counts
	}

	prevOrder := g.baselineOrder
	var prevCounts *qbittorrent.TorrentCounts
	if g.baselineSnapshot != nil {
		prevCounts = g.baselineSnapshot.Counts
	}
	g.baselineFP = newFP
	g.baselineOrder = order
	g.baselineSnapshot = &snapshot
	g.rebuildFailed = false
	g.lastFrame = time.Now()
	g.version.Minor++

	if forceFull {
		return &StreamPayload{
			Type:    streamEventUpdate,
			Data:    &snapshot,
			Meta:    meta,
			Version: new(g.version),
		}
	}

	deltaResp := *resp

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

func (g *subscriptionGroup) buildServerStatePayload(serverState *qbt.ServerState, instanceMeta *qbittorrent.InstanceMeta, activeTaskCount int, meta *StreamMeta) *StreamPayload {
	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()

	prev := g.baselineSnapshot
	if prev == nil || g.rebuildFailed {
		return nil
	}
	if serverState == nil {
		serverState = prev.ServerState
	} else if prevSubcategories := prev.ServerState != nil && prev.ServerState.UseSubcategories; prevSubcategories != serverState.UseSubcategories {
		return nil
	}

	snapshot := *prev
	snapshot.ServerState = serverState
	snapshot.InstanceMeta = instanceMeta
	snapshot.ActiveTaskCount = activeTaskCount
	snapshot.CacheMetadata = qbittorrent.NewCacheMetadata(meta.Timestamp, time.Now())
	return g.resendBaselineLocked(snapshot, meta)
}

func (g *subscriptionGroup) baselineVersions() map[int]string {
	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()
	if g.baselineSnapshot == nil {
		return nil
	}
	return g.baselineSnapshot.WebAPIVersions
}

func (g *subscriptionGroup) buildIdlePayload(meta *StreamMeta) *StreamPayload {
	g.baselineMu.Lock()
	defer g.baselineMu.Unlock()

	if g.baselineSnapshot == nil || g.rebuildFailed {
		return nil
	}
	return g.resendBaselineLocked(*g.baselineSnapshot, meta)
}

func (g *subscriptionGroup) resendBaselineLocked(snapshot qbittorrent.TorrentResponse, meta *StreamMeta) *StreamPayload {
	g.baselineSnapshot = &snapshot
	g.lastFrame = time.Now()
	baseVersion := g.version
	g.version.Minor++

	deltaResp := snapshot
	deltaResp.Torrents = nil
	deltaResp.CrossInstanceTorrents = nil
	deltaResp.Counts = nil

	return &StreamPayload{
		Type:    streamEventDelta,
		Data:    &deltaResp,
		Delta:   &StreamDelta{BaseVersion: baseVersion},
		Meta:    meta,
		Version: new(g.version),
	}
}

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
		order, _, fp = computeRowDelta(resp.Torrents, singleRowKey, singleRowFingerprintFor(opts), nil)
	}

	g.baselineFP = fp
	g.baselineOrder = order
	snapshot := *resp
	detachSnapshotRows(&snapshot)
	g.baselineSnapshot = &snapshot
	g.lastFrame = time.Now()
	g.version.Minor++

	return &StreamPayload{
		Type:    streamEventInit,
		Data:    g.baselineSnapshot,
		Meta:    meta,
		Version: new(g.version),
	}
}

type optionalValues struct {
	reannounce int64
	popularity float64
	private    bool
}

func (v *optionalValues) own(tv *qbittorrent.TorrentView) {
	if tv.Reannounce != nil {
		v.reannounce, tv.Reannounce = *tv.Reannounce, &v.reannounce
	}
	if tv.Popularity != nil {
		v.popularity, tv.Popularity = *tv.Popularity, &v.popularity
	}
	if tv.Private != nil {
		v.private, tv.Private = *tv.Private, &v.private
	}
}

func detachSnapshotRows(snapshot *qbittorrent.TorrentResponse) {
	snapshot.Torrents = slices.Clone(snapshot.Torrents)
	snapshot.CrossInstanceTorrents = slices.Clone(snapshot.CrossInstanceTorrents)
	torrents := make([]qbt.Torrent, len(snapshot.Torrents)+len(snapshot.CrossInstanceTorrents))
	values := make([]optionalValues, len(torrents))
	for i := range snapshot.Torrents {
		row := &snapshot.Torrents[i]
		values[i].own(row)
		if row.Torrent != nil {
			torrents[i] = *row.Torrent
			row.Torrent = &torrents[i]
		}
	}
	views := make([]qbittorrent.TorrentView, len(snapshot.CrossInstanceTorrents))
	for i := range snapshot.CrossInstanceTorrents {
		row := &snapshot.CrossInstanceTorrents[i]
		if row.TorrentView == nil {
			continue
		}
		views[i] = *row.TorrentView
		idx := len(snapshot.Torrents) + i
		values[idx].own(&views[i])
		if row.Torrent != nil {
			torrents[idx] = *row.Torrent
			views[i].Torrent = &torrents[idx]
		}
		row.TorrentView = &views[i]
	}
}

func computeRowDelta[T any](rows []T, keyOf func(T) string, fpOf func(*fpBuf, T) uint64, base map[string]uint64) (order []string, changedIdx []int, newFP map[string]uint64) {
	order = make([]string, len(rows))
	newFP = make(map[string]uint64, len(rows))
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

type fpBuf []byte

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
	b.i64(t.ConnectionsLimit)
	b.str(t.CreatedBy)
	b.i64(t.CreationDate)
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
	b.i64(t.PieceSize)
	b.i64(t.PiecesNum)
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
	b.i64(t.TotalWasted)
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

func (b *fpBuf) optional(tv *qbittorrent.TorrentView) {
	b.bit(tv.Reannounce != nil)
	b.bit(tv.Popularity != nil)
	b.bit(tv.Private != nil)
}

func (b *fpBuf) copyGroup(g *qbittorrent.CopyGroupRow) {
	b.bit(g != nil)
	if g == nil {
		return
	}
	b.str(g.ID)
	b.bit(g.FirstSorted != nil)
	if g.FirstSorted != nil {
		b.i64(int64(g.FirstSorted.InstanceID))
		b.str(g.FirstSorted.Hash)
	}
}

func (b fpBuf) sum() uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

func singleRowFingerprint(b *fpBuf, tv qbittorrent.TorrentView) uint64 {
	*b = (*b)[:0]
	b.torrent(tv.Torrent)
	b.str(string(tv.TrackerHealth))
	b.optional(&tv)
	b.copyGroup(tv.CopyGroup)
	return b.sum()
}

func oneRowFingerprint(b *fpBuf, tv qbittorrent.TorrentView) uint64 {
	*b = (*b)[:0]
	b.torrent(tv.Torrent)
	b.str(string(tv.TrackerHealth))
	b.optional(&tv)
	b.copyGroup(tv.CopyGroup)
	if t := tv.Torrent; t != nil {
		b.i64(t.TimeActive)
		b.i64(t.SeedingTime)
		b.i64(t.NumSeeds)
		b.i64(t.NumLeechs)
		b.i64(t.NumComplete)
		b.i64(t.NumIncomplete)
		b.i64(t.ConnectionsCount)
		b.i64(t.PiecesHave)
		b.i64(t.Reannounce)
	}
	return b.sum()
}

func singleRowFingerprintFor(opts StreamOptions) func(*fpBuf, qbittorrent.TorrentView) uint64 {
	if opts.View.Limit == 1 {
		return oneRowFingerprint
	}
	return singleRowFingerprint
}

func crossRowFingerprint(b *fpBuf, c qbittorrent.CrossInstanceTorrentView) uint64 {
	*b = (*b)[:0]
	if c.TorrentView != nil {
		b.torrent(c.Torrent)
		b.str(string(c.TrackerHealth))
		b.optional(c.TorrentView)
		b.copyGroup(c.CopyGroup)
	}
	b.i64(int64(c.InstanceID))
	b.str(c.InstanceName)
	return b.sum()
}

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
		maps.Equal(a.Trackers, b.Trackers)
}

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

func singleRowKey(tv qbittorrent.TorrentView) string {
	if tv.Torrent == nil {
		return ""
	}
	return tv.Hash
}

func crossRowKey(c qbittorrent.CrossInstanceTorrentView) string {
	hash := ""
	if c.TorrentView != nil && c.Torrent != nil {
		hash = c.Hash
	}
	return strconv.Itoa(c.InstanceID) + ":" + hash
}
