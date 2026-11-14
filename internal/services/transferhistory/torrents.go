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
	"github.com/autobrr/qui/pkg/releases"
)

// torrentGroup returns the group of a torrent in a grouping, from the record
// of the torrent now. A release attribute that the name does not hold is "".
func torrentGroup(mapper *trackergroup.Mapper, f *models.TransferHistoryTorrentFacts, grouping Grouping) string {
	switch grouping {
	case GroupByTracker:
		return groupOf(mapper, f.TrackerDomain, f.Private != nil && !*f.Private)
	case GroupByCategory:
		return f.Category
	case GroupByReleaseGroup:
		return f.ReleaseGroup
	case GroupByResolution:
		return f.Resolution
	case GroupBySource:
		return f.Source
	case GroupByContentType:
		if f.ContentType == string(releases.ContentTypeUnknown) {
			return ""
		}
		return f.ContentType
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
	inPeriod, err := yearsInPeriod(ctx, store, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		f := byID[r.TorrentID]
		if f == nil || r.Year && !inPeriod(r.Bucket) {
			continue
		}
		b := torrentBucket{facts: f, width: BucketDay, bucket: r.Bucket, uploaded: r.Uploaded, downloaded: r.Downloaded}
		switch {
		case r.Year:
			b.width, b.start = BucketYear, yearStart(r.Bucket)
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

// yearsInPeriod returns whether the year row of a fold belongs to the period
// of UTC days [fromDay, toDay). A year holds only the days before the latest
// fold. The caller reads the rows first: the cutoff only grows, so a cutoff
// read after the rows also covers the years in them, also when a fold
// commits between the two reads.
func yearsInPeriod(ctx context.Context, store *models.TransferHistoryStore, fromDay, toDay int64) (func(year int64) bool, error) {
	foldedBefore, err := store.TorrentDaysFoldedBefore(ctx)
	if err != nil {
		return nil, err
	}
	foldedTo := min(toDay, foldedBefore)
	return func(year int64) bool {
		return fromDay < foldedTo && dayOf(yearStart(year)) < foldedTo
	}, nil
}

func yearStart(year int64) time.Time {
	return time.Date(int(year), time.January, 1, 0, 0, 0, 0, time.UTC)
}

// byTorrent reads the rows of each torrent, and adds them up by the group of
// each torrent in the grouping of the query. A day row is a UTC day, and a
// query for hours gets day rows. A total covers each whole stored bucket that
// overlaps the period, UTC days and the years that qui folded, and starts at
// the first of them.
func (a *answer) byTorrent(ctx context.Context, store *models.TransferHistoryStore) error {
	if a.query.GroupBy == GroupByTorrent && a.query.Group == nil && a.query.Bucket == BucketTotal && a.query.Limit > 0 {
		return a.topTorrents(ctx, store)
	}
	total := a.query.Bucket == BucketTotal
	noStart := a.query.From.IsZero()
	facts, err := a.torrentRows(ctx, store, func(b torrentBucket) {
		group := torrentGroup(a.mapper, b.facts, a.query.GroupBy)
		key := rowKey{group: group, width: b.width, bucket: b.bucket}
		start := b.start
		if total {
			key = rowKey{group: group, width: BucketTotal}
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
	if a.query.GroupBy == GroupByTorrent {
		a.torrents = make(map[string]*models.TransferHistoryTorrentFacts, len(facts))
	}
	for _, f := range facts {
		if a.torrents != nil {
			a.torrents[f.Hash] = f
		}
		if total && noStart && (f.BaselineUploaded != 0 || f.BaselineDownloaded != 0) {
			r := a.rowOf(rowKey{group: torrentGroup(a.mapper, f, a.query.GroupBy), width: BucketTotal}, time.Time{})
			if r.Baseline == nil {
				r.Baseline = &Bytes{}
			}
			addBytes(r.Baseline, f.BaselineUploaded, f.BaselineDownloaded, f.BaselineStatisticsCrossSeed)
		}
	}
	fromDay, toDay := daysOf(a.query.From, a.query.To)
	if total {
		return a.addGaps(ctx, store, fromDay*24, toDay*24, models.TransferHistoryBuckets{}, BucketTotal, false)
	}
	return a.addGaps(ctx, store, fromDay*24, toDay*24, models.TransferHistoryBuckets{Width: 24}, BucketDay, false)
}

// torrentSum is the transfer of one torrent in the period, for topTorrents.
type torrentSum struct {
	uploaded, downloaded int64
	start                time.Time
	baseline             *models.TransferHistoryTorrentBaseline
}

func (s *torrentSum) rank() int64 {
	if s.baseline == nil {
		return s.uploaded
	}
	return s.uploaded + s.baseline.Uploaded
}

// topTorrents answers the totals of the torrents with the most upload, as
// byTorrent does. It reads the records of the torrents that it keeps only,
// because the records of each torrent of a large library take longer to read
// than the bytes of a short period. It keeps each torrent that ties with the
// last one, so that Transfer picks among them by hash, as for the other
// queries.
func (a *answer) topTorrents(ctx context.Context, store *models.TransferHistoryStore) error {
	q := a.query
	fromDay, toDay := daysOf(q.From, q.To)
	rows, err := store.TorrentTransfer(ctx, q.InstanceID, nil, fromDay, toDay, true)
	if err != nil {
		return err
	}
	inPeriod, err := yearsInPeriod(ctx, store, fromDay, toDay)
	if err != nil {
		return err
	}
	var start time.Time
	if !q.From.IsZero() {
		start = time.Unix(fromDay*86400, 0).UTC()
	}
	sums := make(map[int64]*torrentSum)
	sumOf := func(id int64) *torrentSum {
		s := sums[id]
		if s == nil {
			s = &torrentSum{start: start}
			sums[id] = s
		}
		return s
	}
	for _, r := range rows {
		if r.Year && !inPeriod(r.Bucket) {
			continue
		}
		s := sumOf(r.TorrentID)
		s.uploaded += r.Uploaded
		s.downloaded += r.Downloaded
		if r.Year && !start.IsZero() && yearStart(r.Bucket).Before(s.start) {
			s.start = yearStart(r.Bucket)
		}
	}
	if q.From.IsZero() {
		baselines, err := store.TorrentBaselines(ctx, q.InstanceID)
		if err != nil {
			return err
		}
		for i := range baselines {
			sumOf(baselines[i].TorrentID).baseline = &baselines[i]
		}
	}

	ids := slices.SortedFunc(maps.Keys(sums), func(x, y int64) int { return cmp.Compare(sums[y].rank(), sums[x].rank()) })
	if len(ids) > q.Limit {
		last := sums[ids[q.Limit-1]].rank()
		if n := slices.IndexFunc(ids[q.Limit:], func(id int64) bool { return sums[id].rank() < last }); n >= 0 {
			ids = ids[:q.Limit+n]
		}
	}
	if len(ids) == 0 {
		return nil
	}
	facts, err := store.TorrentFactsOf(ctx, ids)
	if err != nil {
		return err
	}
	a.torrents = make(map[string]*models.TransferHistoryTorrentFacts, len(facts))
	for i := range facts {
		f := &facts[i]
		s := sums[f.ID]
		a.torrents[f.Hash] = f
		r := a.rowOf(rowKey{group: f.Hash, width: BucketTotal}, s.start)
		addBytes(&r.Bytes, s.uploaded, s.downloaded, f.StatisticsCrossSeed)
		if s.baseline != nil {
			r.Baseline = &Bytes{}
			addBytes(r.Baseline, s.baseline.Uploaded, s.baseline.Downloaded, s.baseline.StatisticsCrossSeed)
		}
	}
	return a.addGaps(ctx, store, fromDay*24, toDay*24, models.TransferHistoryBuckets{}, BucketTotal, false)
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
