// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
)

// torrentGroup returns the group of a torrent in a grouping, from the record
// of the torrent now.
func torrentGroup(mapper *trackergroup.Mapper, f *models.TransferHistoryTorrentFacts, grouping Grouping) string {
	switch grouping {
	case GroupByTracker:
		return groupOf(mapper, f.TrackerDomain, f.Private != nil && !*f.Private)
	case GroupByCategory:
		return f.Category
	default:
		return f.Hash
	}
}

// torrentBucket is the transfer of one torrent in one bucket of the torrent
// rows: a UTC day, a year that qui folded, or the days of the whole period.
type torrentBucket struct {
	facts                *models.TransferHistoryTorrentFacts
	width                Bucket
	bucket               int64
	start                time.Time
	uploaded, downloaded int64
}

// torrentRows reads the torrent rows of the period, of the torrents of the
// group of the query when it has one, and calls emit for each. It returns the
// records of those torrents, by id. A total holds the days of the period, and
// each year that qui folded apart.
func (a *answer) torrentRows(ctx context.Context, store *models.TransferHistoryStore, emit func(torrentBucket)) (map[int64]*models.TransferHistoryTorrentFacts, error) {
	q := a.query
	within := cmp.Or(q.GroupOf, q.GroupBy)
	hash := ""
	if q.Group != nil && within == GroupByTorrent {
		if *q.Group == "" {
			return nil, nil
		}
		hash = *q.Group
	}
	facts, err := store.TorrentFacts(ctx, q.InstanceID, hash)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*models.TransferHistoryTorrentFacts, len(facts))
	for i := range facts {
		f := &facts[i]
		if q.Group == nil || torrentGroup(a.mapper, f, within) == *q.Group {
			byID[f.ID] = f
		}
	}
	if len(byID) == 0 {
		return byID, nil
	}
	// The statement reads only the torrents of a group, so that a small group
	// does not read the days of each torrent of the instance.
	var torrentIDs []int64
	if len(byID) < len(facts) || hash != "" {
		torrentIDs = slices.Sorted(maps.Keys(byID))
	}

	fromDay, toDay := daysOf(q.From, q.To)
	total := q.Bucket == BucketTotal
	rows, err := store.TorrentTransfer(ctx, q.InstanceID, torrentIDs, fromDay, toDay, total)
	if err != nil {
		return nil, err
	}
	// A year holds only the days before the latest fold. The cutoff only
	// grows, so a cutoff read after the rows also covers the years in them,
	// also when a fold commits between the two reads.
	foldedBefore, err := store.TorrentDaysFoldedBefore(ctx)
	if err != nil {
		return nil, err
	}
	foldedTo := min(toDay, foldedBefore)
	for _, r := range rows {
		f := byID[r.TorrentID]
		if f == nil || r.Year && (fromDay >= foldedTo || dayOf(time.Date(int(r.Bucket), time.January, 1, 0, 0, 0, 0, time.UTC)) >= foldedTo) {
			continue
		}
		b := torrentBucket{facts: f, width: BucketDay, bucket: r.Bucket, uploaded: r.Uploaded, downloaded: r.Downloaded}
		switch {
		case r.Year:
			b.width, b.start = BucketYear, time.Date(int(r.Bucket), time.January, 1, 0, 0, 0, 0, time.UTC)
		case total:
			b.width = BucketTotal
			if !q.From.IsZero() {
				b.start = time.Unix(fromDay*86400, 0).UTC()
			}
		default:
			b.start = time.Unix(r.Bucket*86400, 0).UTC()
		}
		emit(b)
	}
	return byID, nil
}

// byTorrent reads the rows of each torrent. A day row is a UTC day, and a
// query for hours gets day rows. A total covers each whole stored bucket that
// overlaps the period, UTC days and the years that qui folded, and starts at
// the first of them.
func (a *answer) byTorrent(ctx context.Context, store *models.TransferHistoryStore) error {
	total := a.query.Bucket == BucketTotal
	noStart := a.query.From.IsZero()
	facts, err := a.torrentRows(ctx, store, func(b torrentBucket) {
		key := rowKey{group: b.facts.Hash, width: b.width, bucket: b.bucket}
		start := b.start
		if total {
			key = rowKey{group: b.facts.Hash, width: BucketTotal}
			if noStart {
				start = time.Time{}
			}
		}
		r := a.rowOf(key, start)
		if start.Before(r.Start) {
			r.Start = start
		}
		addBytes(&r.Bytes, b.uploaded, b.downloaded, b.facts.StatisticsCrossSeed)
	})
	if err != nil {
		return err
	}
	a.torrents = make(map[string]*models.TransferHistoryTorrentFacts, len(facts))
	for _, f := range facts {
		a.torrents[f.Hash] = f
		if total && noStart && (f.BaselineUploaded != 0 || f.BaselineDownloaded != 0) {
			r := a.rowOf(rowKey{group: f.Hash, width: BucketTotal}, time.Time{})
			r.Baseline = &Bytes{}
			addBytes(r.Baseline, f.BaselineUploaded, f.BaselineDownloaded, f.BaselineStatisticsCrossSeed)
		}
	}
	fromDay, toDay := daysOf(a.query.From, a.query.To)
	if total {
		return a.addGaps(ctx, store, fromDay*24, toDay*24, models.TransferHistoryBuckets{}, BucketTotal, false)
	}
	return a.addGaps(ctx, store, fromDay*24, toDay*24, models.TransferHistoryBuckets{Width: 24}, BucketDay, false)
}

// addTorrentFacts sets the facts of the torrent of each row of the grouping by
// torrent. Transfer calls it after the limit, so that only the kept rows get
// them.
func (a *answer) addTorrentFacts(rows []TransferRow) {
	if a.torrents == nil {
		return
	}
	for i := range rows {
		f := a.torrents[rows[i].Group]
		rows[i].Torrent = &TorrentFacts{
			Name:                f.Name,
			TrackerGroup:        torrentGroup(a.mapper, f, GroupByTracker),
			Deleted:             f.Deleted,
			StatisticsCrossSeed: f.StatisticsCrossSeed,
			UploadFirst24h:      f.UploadFirst24h,
		}
	}
}
