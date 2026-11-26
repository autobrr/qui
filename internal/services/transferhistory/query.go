// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
)

type Bucket string

const (
	BucketHour  Bucket = "hour"
	BucketDay   Bucket = "day"
	BucketTotal Bucket = "total"
	BucketYear  Bucket = "year"
)

type Grouping string

const (
	GroupByTracker      Grouping = "tracker"
	GroupByCategory     Grouping = "category"
	GroupByTorrent      Grouping = "torrent"
	GroupByReleaseGroup Grouping = "releaseGroup"
	GroupByResolution   Grouping = "resolution"
	GroupBySource       Grouping = "source"
	GroupByContentType  Grouping = "contentType"
)

var groupings = []Grouping{GroupByTracker, GroupByCategory, GroupByTorrent, GroupByReleaseGroup, GroupByResolution, GroupBySource, GroupByContentType}

func (g Grouping) readsTorrentRows() bool {
	return g == GroupByTorrent || g.releaseAttribute()
}

func (g Grouping) releaseAttribute() bool {
	switch g {
	case GroupByReleaseGroup, GroupByResolution, GroupBySource, GroupByContentType:
		return true
	default:
		return false
	}
}

type TransferQuery struct {
	InstanceID int
	From       time.Time
	To         time.Time
	Bucket     Bucket
	GroupBy    Grouping
	Group      *string
	GroupOf    Grouping
	Limit      int
	Location   *time.Location
}

type Bytes struct {
	Uploaded                    int64 `json:"uploaded"`
	Downloaded                  int64 `json:"downloaded"`
	StatisticsCrossSeedUploaded int64 `json:"statisticsCrossSeedUploaded"`
}

type TransferRow struct {
	Group      string    `json:"group"`
	Start      time.Time `json:"start,omitzero"`
	Width      Bucket    `json:"width"`
	GapSeconds int64     `json:"gapSeconds"`
	Bytes
	Baseline *Bytes        `json:"baseline,omitempty"`
	Torrent  *TorrentFacts `json:"torrent,omitempty"`
}

type TorrentFacts struct {
	Name                string `json:"name"`
	TrackerGroup        string `json:"trackerGroup"`
	Size                int64  `json:"size"`
	Deleted             bool   `json:"deleted"`
	StatisticsCrossSeed bool   `json:"statisticsCrossSeed"`
	UploadFirst24h      *int64 `json:"uploadFirst24h"`
}

type Status struct {
	Instances    []InstanceStatus `json:"instances"`
	HistoryBytes int64            `json:"historyBytes"`
	Settings     Settings         `json:"settings"`
}

type InstanceStatus struct {
	InstanceID      int       `json:"instanceId"`
	Name            string    `json:"name"`
	FirstRecordedAt time.Time `json:"firstRecordedAt"`
}

func (m *Module) Transfer(ctx context.Context, q TransferQuery) ([]TransferRow, error) {
	if err := q.validate(); err != nil {
		return nil, err
	}
	if err := m.settle(ctx); err != nil {
		return nil, err
	}
	if !q.GroupBy.releaseAttribute() {
		return m.read(ctx, q)
	}
	key := keyOf(q)
	rows, version, ok := m.answers.get(key)
	if ok {
		return rows, nil
	}
	rows, err := m.read(ctx, q)
	if err != nil {
		return nil, err
	}
	m.answers.put(key, version, rows)
	return rows, nil
}

func (m *Module) read(ctx context.Context, q TransferQuery) ([]TransferRow, error) {
	customizations, err := m.customizations.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tracker customizations: %w", err)
	}
	m.foldMu.RLock()
	defer m.foldMu.RUnlock()
	a := answer{query: q, mapper: trackergroup.New(customizations), rows: make(map[rowKey]*TransferRow)}
	switch {
	case q.GroupBy == GroupByCategory:
		err = a.byCategory(ctx, m.store)
	case q.GroupBy.readsTorrentRows():
		err = a.byTorrent(ctx, m.store)
	default:
		err = a.byTracker(ctx, m.store)
	}
	if err != nil {
		return nil, err
	}
	rows := a.sorted()
	if q.Group != nil && !q.GroupBy.readsTorrentRows() {
		rows = slices.DeleteFunc(rows, func(r TransferRow) bool { return r.Group != *q.Group })
	}
	if q.Limit > 0 {
		rows = top(rows, q.Limit)
	}
	a.addTorrentFacts(rows)
	return rows, nil
}

func top(rows []TransferRow, limit int) []TransferRow {
	uploaded := make(map[string]int64)
	for _, r := range rows {
		uploaded[r.Group] += r.Uploaded
		if r.Baseline != nil {
			uploaded[r.Group] += r.Baseline.Uploaded
		}
	}
	groups := slices.SortedFunc(maps.Keys(uploaded), func(x, y string) int {
		return cmp.Or(cmp.Compare(uploaded[y], uploaded[x]), cmp.Compare(x, y))
	})
	kept := groups[:min(limit, len(groups))]
	rank := make(map[string]int, len(kept))
	for i, group := range kept {
		rank[group] = i
	}
	rows = slices.DeleteFunc(rows, func(r TransferRow) bool { _, ok := rank[r.Group]; return !ok })
	slices.SortStableFunc(rows, func(x, y TransferRow) int { return cmp.Compare(rank[x.Group], rank[y.Group]) })
	return rows
}

var ErrInvalidQuery = errors.New("invalid query")

func validatePeriod(from, to time.Time) error {
	switch {
	case to.IsZero():
		return fmt.Errorf("%w: no end", ErrInvalidQuery)
	case !from.IsZero() && !from.Before(to):
		return fmt.Errorf("%w: the start is not before the end", ErrInvalidQuery)
	}
	return nil
}

func (q TransferQuery) validate() error {
	if err := validatePeriod(q.From, q.To); err != nil {
		return err
	}
	switch {
	case q.Bucket != BucketHour && q.Bucket != BucketDay && q.Bucket != BucketTotal:
		return fmt.Errorf("%w: unknown bucket %q", ErrInvalidQuery, q.Bucket)
	case !slices.Contains(groupings, q.GroupBy):
		return fmt.Errorf("%w: unknown grouping %q", ErrInvalidQuery, q.GroupBy)
	case q.GroupOf != "" && q.GroupOf != q.GroupBy && q.GroupBy != GroupByTorrent:
		return fmt.Errorf("%w: only the grouping by torrent takes the group of another grouping", ErrInvalidQuery)
	case q.GroupOf != "" && !slices.Contains(groupings, q.GroupOf):
		return fmt.Errorf("%w: unknown grouping %q of the group", ErrInvalidQuery, q.GroupOf)
	case q.GroupOf != "" && q.Group == nil:
		return fmt.Errorf("%w: a grouping of the group needs a group", ErrInvalidQuery)
	case q.Limit < 0:
		return fmt.Errorf("%w: the limit is below 0", ErrInvalidQuery)
	}
	return nil
}

func (m *Module) Status(ctx context.Context) (Status, error) {
	if err := m.settle(ctx); err != nil {
		return Status{}, err
	}
	instances, err := m.store.Instances(ctx)
	if err != nil {
		return Status{}, err
	}
	status := Status{Instances: make([]InstanceStatus, len(instances))}
	for i, inst := range instances {
		status.Instances[i] = InstanceStatus{InstanceID: inst.InstanceID, Name: inst.Name, FirstRecordedAt: inst.FirstRecordedAt}
	}
	if status.HistoryBytes, err = m.historySize(ctx); err != nil {
		return Status{}, err
	}
	if status.Settings, err = m.currentSettings(ctx); err != nil {
		return Status{}, err
	}
	return status, nil
}

type rowKey struct {
	group  string
	width  Bucket
	bucket int64
	utcDay bool
}

type answer struct {
	query      TransferQuery
	mapper     *trackergroup.Mapper
	rows       map[rowKey]*TransferRow
	foldedFrom bool
	torrents   map[string]*models.TransferHistoryTorrentFacts
}

func ceilDiv(a, b int64) int64 { return -floorDiv(-a, b) }

func (a *answer) hours() (from, to int64) {
	if !a.query.From.IsZero() {
		from = hourOf(a.query.From)
	}
	return from, ceilDiv(ceilSecond(a.query.To), 3600)
}

func daysOf(from, to time.Time) (fromDay, toDay int64) {
	if !from.IsZero() {
		fromDay = dayOf(from)
	}
	return fromDay, ceilDiv(ceilSecond(to), 86400)
}

func ceilSecond(t time.Time) int64 {
	second := t.Unix()
	if t.Nanosecond() > 0 {
		second++
	}
	return second
}

func groupOf(mapper *trackergroup.Mapper, domain string, public bool) string {
	var private *bool
	if public {
		private = new(false)
	}
	var domains []string
	if domain != "" {
		domains = []string{domain}
	}
	return mapper.Of(domains, private).Name
}

func (a *answer) row(group string, bucket int64, width Bucket, start time.Time) *TransferRow {
	return a.rowOf(rowKey{group: group, width: width, bucket: bucket}, start)
}

func (a *answer) rowOf(key rowKey, start time.Time) *TransferRow {
	r := a.rows[key]
	if r == nil {
		r = &TransferRow{Group: key.group, Start: start, Width: key.width}
		a.rows[key] = r
	}
	return r
}

func addBytes(b *Bytes, uploaded, downloaded int64, crossSeed bool) {
	b.Uploaded += uploaded
	b.Downloaded += downloaded
	if crossSeed {
		b.StatisticsCrossSeedUploaded += uploaded
	}
}

func (a *answer) startOf(width Bucket, bucket int64) time.Time {
	switch {
	case width == BucketHour:
		return time.Unix(bucket*3600, 0).UTC()
	case width == BucketDay:
		return startOfLocalDay(time.Unix(bucket*86400, 0).UTC(), a.dayLocation())
	case a.query.From.IsZero():
		return time.Time{}
	case a.query.GroupBy == GroupByCategory || a.foldedFrom:
		return time.Unix(dayOf(a.query.From)*86400, 0).UTC()
	default:
		return a.startOf(BucketHour, hourOf(a.query.From))
	}
}

func (a *answer) dayLocation() *time.Location {
	if a.query.Location == nil || a.query.GroupBy == GroupByCategory {
		return time.UTC
	}
	return a.query.Location
}

func (a *answer) trackerBuckets(ctx context.Context, store *models.TransferHistoryStore, from, to int64) (models.TransferHistoryBuckets, error) {
	if a.query.Bucket != BucketDay {
		return models.TransferHistoryBuckets{Width: map[Bucket]int64{BucketHour: 1, BucketTotal: 0}[a.query.Bucket]}, nil
	}
	instances, err := store.Instances(ctx)
	if err != nil {
		return models.TransferHistoryBuckets{}, err
	}
	first, last := to, to
	for _, instance := range instances {
		if instance.InstanceID == a.query.InstanceID {
			first, last = hourOf(instance.FirstRecordedAt), hourOf(instance.LastReadingAt)+1
		}
	}
	return models.TransferHistoryBuckets{Offsets: zoneOffsets(a.dayLocation(), max(from, first), min(to, last))}, nil
}

func zoneOffsets(loc *time.Location, fromHour, toHour int64) []models.TransferHistoryOffset {
	var offsets []models.TransferHistoryOffset
	for start := time.Unix(fromHour*3600, 0); ; {
		zone := start.In(loc)
		_, seconds := zone.Zone()
		if minutes := floorDiv(int64(seconds), 60); len(offsets) == 0 || offsets[len(offsets)-1].Minutes != minutes {
			offsets = append(offsets, models.TransferHistoryOffset{FromHour: ceilDiv(start.Unix(), 3600), Minutes: minutes})
		}
		_, end := zone.ZoneBounds()
		if end.IsZero() || end.Unix() >= toHour*3600 {
			return offsets
		}
		if !end.After(start) {
			end = start.Add(time.Hour)
		}
		start = end
	}
}

func startOfLocalDay(day time.Time, loc *time.Location) time.Time {
	year, month, date := day.Date()
	start := time.Date(year, month, date, 0, 0, 0, 0, loc)
	if start.Day() != date {
		_, start = start.ZoneBounds()
	}
	return start.UTC()
}

func (a *answer) byTracker(ctx context.Context, store *models.TransferHistoryStore) error {
	from, to := a.hours()
	detailDay, hasHours, err := store.HourlyDetailStart(ctx, a.query.InstanceID)
	if err != nil {
		return err
	}
	fromDay, toDay := daysOf(a.query.From, a.query.To)
	if hasHours {
		toDay = min(toDay, detailDay)
	}
	folded := false
	if fromDay < toDay {
		if folded, err = a.trackerDays(ctx, store, fromDay, toDay); err != nil {
			return err
		}
		if folded {
			from = max(from, toDay*24)
		}
	}
	var buckets models.TransferHistoryBuckets
	if from < to {
		if buckets, err = a.trackerBuckets(ctx, store, from, to); err != nil {
			return err
		}
		if err := a.trackerHours(ctx, store, from, to, buckets); err != nil {
			return err
		}
	}
	if err := a.addBaselines(ctx, store, func(b models.TransferHistoryBaseline) string {
		return groupOf(a.mapper, b.TrackerDomain, b.Private != nil && !*b.Private)
	}); err != nil {
		return err
	}
	if folded {
		width, days := BucketDay, models.TransferHistoryBuckets{Width: 24}
		if a.query.Bucket == BucketTotal {
			width, days = BucketTotal, models.TransferHistoryBuckets{}
		}
		if err := a.addGaps(ctx, store, fromDay*24, toDay*24, days, width, width == BucketDay); err != nil {
			return err
		}
	}
	return a.addGaps(ctx, store, from, to, buckets, a.query.Bucket, false)
}

func (a *answer) trackerHours(ctx context.Context, store *models.TransferHistoryStore, from, to int64, buckets models.TransferHistoryBuckets) error {
	totals, err := store.TrackerTotals(ctx, a.query.InstanceID, from, to, buckets)
	if err != nil {
		return err
	}
	for _, t := range totals {
		r := a.row(groupOf(a.mapper, t.TrackerDomain, t.Public), t.Bucket, a.query.Bucket, a.startOf(a.query.Bucket, t.Bucket))
		addBytes(&r.Bytes, t.Uploaded, t.Downloaded, t.StatisticsCrossSeed)
	}
	return nil
}

func (a *answer) trackerDays(ctx context.Context, store *models.TransferHistoryStore, fromDay, toDay int64) (bool, error) {
	width := BucketDay
	if a.query.Bucket == BucketTotal {
		width = BucketTotal
	}
	totals, err := store.TrackerDayTotals(ctx, a.query.InstanceID, fromDay, toDay, width == BucketTotal)
	if err != nil {
		return false, err
	}
	a.foldedFrom = len(totals) > 0
	for _, t := range totals {
		key := rowKey{group: groupOf(a.mapper, t.TrackerDomain, t.Public), width: width, bucket: t.Bucket, utcDay: width == BucketDay}
		start := time.Unix(t.Bucket*86400, 0).UTC()
		if width == BucketTotal {
			start = a.startOf(width, t.Bucket)
		}
		addBytes(&a.rowOf(key, start).Bytes, t.Uploaded, t.Downloaded, t.StatisticsCrossSeed)
	}
	return len(totals) > 0, nil
}

func (a *answer) byCategory(ctx context.Context, store *models.TransferHistoryStore) error {
	width, buckets := BucketDay, models.TransferHistoryBuckets{Width: 24}
	if a.query.Bucket == BucketTotal {
		width, buckets = BucketTotal, models.TransferHistoryBuckets{}
	}
	from, to := daysOf(a.query.From, a.query.To)
	totals, err := store.CategoryTotals(ctx, a.query.InstanceID, from, to, width == BucketTotal)
	if err != nil {
		return err
	}
	for _, t := range totals {
		r := a.row(t.Category, t.Bucket, width, a.startOf(width, t.Bucket))
		addBytes(&r.Bytes, t.Uploaded, t.Downloaded, t.StatisticsCrossSeed)
	}
	if err := a.addBaselines(ctx, store, func(b models.TransferHistoryBaseline) string { return b.Category }); err != nil {
		return err
	}
	return a.addGaps(ctx, store, from*24, to*24, buckets, width, false)
}

func (a *answer) addGaps(ctx context.Context, store *models.TransferHistoryStore, fromHour, toHour int64, buckets models.TransferHistoryBuckets, width Bucket, utcDay bool) error {
	if fromHour >= toHour {
		return nil
	}
	gaps, err := store.GapSeconds(ctx, a.query.InstanceID, fromHour, toHour, buckets)
	if err != nil {
		return err
	}
	for key, r := range a.rows {
		if key.width == width && key.utcDay == utcDay {
			r.GapSeconds += gaps[key.bucket]
		}
	}
	return nil
}

func (a *answer) addBaselines(ctx context.Context, store *models.TransferHistoryStore, group func(models.TransferHistoryBaseline) string) error {
	if !a.query.From.IsZero() || a.query.Bucket != BucketTotal {
		return nil
	}
	baselines, err := store.Baselines(ctx, a.query.InstanceID)
	if err != nil {
		return err
	}
	for _, b := range baselines {
		r := a.row(group(b), 0, BucketTotal, time.Time{})
		if r.Baseline == nil {
			r.Baseline = &Bytes{}
		}
		addBytes(r.Baseline, b.Uploaded, b.Downloaded, b.StatisticsCrossSeed)
	}
	return nil
}

func (a *answer) sorted() []TransferRow {
	out := make([]TransferRow, 0, len(a.rows))
	for _, r := range a.rows {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(x, y TransferRow) int {
		return cmp.Or(x.Start.Compare(y.Start), cmp.Compare(x.Group, y.Group))
	})
	return out
}
