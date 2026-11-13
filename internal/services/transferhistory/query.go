// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	// Embeds the zone database, which Windows and minimal container images lack.
	_ "time/tzdata"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
)

// Bucket is the width of the rows of a transfer query.
type Bucket string

const (
	BucketHour  Bucket = "hour"
	BucketDay   Bucket = "day"
	BucketTotal Bucket = "total"
)

// Grouping is what the rows of a transfer query group by.
type Grouping string

const (
	GroupByTracker  Grouping = "tracker"
	GroupByCategory Grouping = "category"
)

// TransferQuery asks for the transfer of one instance in the period
// [From, To). A zero From is a period with no start. Times are UTC.
// Location sets the midnights of the day buckets of Tracker groups, and nil
// is UTC. An hour counts toward the day of its start. Category totals stay on
// UTC days, because qui stores them for each UTC day.
type TransferQuery struct {
	InstanceID int
	From       time.Time
	To         time.Time
	Bucket     Bucket
	GroupBy    Grouping
	Location   *time.Location
}

// Bytes is an amount of transfer. StatisticsCrossSeedUploaded is the part of Uploaded
// that came from Statistics cross-seeds.
type Bytes struct {
	Uploaded                    int64 `json:"uploaded"`
	Downloaded                  int64 `json:"downloaded"`
	StatisticsCrossSeedUploaded int64 `json:"statisticsCrossSeedUploaded"`
}

// TransferRow is the transfer of one group in one bucket. A row can be
// coarser than the query asks: Width tells its bucket. Start is the start of
// the bucket. A total covers each whole stored bucket that overlaps the
// period, hours for Tracker groups and UTC days for categories, and starts at
// the first of them. GapSeconds is the length of the
// Recording gaps that ended in the bucket. Baseline is set on the total rows
// of a period with no start.
type TransferRow struct {
	Group      string    `json:"group"`
	Start      time.Time `json:"start,omitzero"`
	Width      Bucket    `json:"width"`
	GapSeconds int64     `json:"gapSeconds"`
	Bytes
	Baseline *Bytes `json:"baseline,omitempty"`
}

// InstanceStatus is an instance that has history.
type InstanceStatus struct {
	InstanceID      int       `json:"instanceId"`
	Name            string    `json:"name"`
	FirstRecordedAt time.Time `json:"firstRecordedAt"`
}

// Transfer answers a transfer query. It includes each reading that the module
// got before the call, up to the last write that those readings made due.
func (m *Module) Transfer(ctx context.Context, q TransferQuery) ([]TransferRow, error) {
	if err := q.validate(); err != nil {
		return nil, err
	}
	if err := m.settle(ctx); err != nil {
		return nil, err
	}
	customizations, err := m.customizations.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tracker customizations: %w", err)
	}
	a := answer{query: q, mapper: trackergroup.New(customizations), rows: make(map[rowKey]*TransferRow)}
	if q.GroupBy == GroupByCategory {
		err = a.byCategory(ctx, m.store)
	} else {
		err = a.byTracker(ctx, m.store)
	}
	if err != nil {
		return nil, err
	}
	return a.sorted(), nil
}

// ErrInvalidQuery is the error of a query that the module cannot answer.
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
	case q.GroupBy != GroupByTracker && q.GroupBy != GroupByCategory:
		return fmt.Errorf("%w: unknown grouping %q", ErrInvalidQuery, q.GroupBy)
	}
	return nil
}

// Status returns the instances that have history, by instance id.
func (m *Module) Status(ctx context.Context) ([]InstanceStatus, error) {
	if err := m.settle(ctx); err != nil {
		return nil, err
	}
	instances, err := m.store.Instances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]InstanceStatus, len(instances))
	for i, inst := range instances {
		out[i] = InstanceStatus{InstanceID: inst.InstanceID, Name: inst.Name, FirstRecordedAt: inst.FirstRecordedAt}
	}
	return out, nil
}

type rowKey struct {
	group  string
	bucket int64
}

type answer struct {
	query  TransferQuery
	mapper *trackergroup.Mapper
	rows   map[rowKey]*TransferRow
}

func ceilDiv(a, b int64) int64 { return -floorDiv(-a, b) }

// hours returns the stored hours that overlap the period.
func (a *answer) hours() (from, to int64) {
	if !a.query.From.IsZero() {
		from = hourOf(a.query.From)
	}
	return from, ceilDiv(ceilSecond(a.query.To), 3600)
}

// daysOf returns the stored UTC days that overlap the period [from, to).
func daysOf(from, to time.Time) (fromDay, toDay int64) {
	if !from.IsZero() {
		fromDay = dayOf(from)
	}
	return fromDay, ceilDiv(ceilSecond(to), 86400)
}

// ceilSecond returns t in whole seconds since 1970, rounded up.
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
	key := rowKey{group, bucket}
	r := a.rows[key]
	if r == nil {
		r = &TransferRow{Group: group, Start: start, Width: width}
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

// startOf returns the start time of a bucket of an answer row.
func (a *answer) startOf(width Bucket, bucket int64) time.Time {
	switch {
	case width == BucketHour:
		return time.Unix(bucket*3600, 0).UTC()
	case width == BucketDay:
		return startOfLocalDay(time.Unix(bucket*86400, 0).UTC(), a.dayLocation())
	case a.query.From.IsZero():
		return time.Time{}
	case a.query.GroupBy == GroupByCategory:
		return a.startOf(BucketDay, dayOf(a.query.From))
	default:
		return a.startOf(BucketHour, hourOf(a.query.From))
	}
}

// dayLocation returns the time zone of the day buckets of the answer. The
// days of categories are always UTC days, because qui stores them so.
func (a *answer) dayLocation() *time.Location {
	if a.query.Location == nil || a.query.GroupBy == GroupByCategory {
		return time.UTC
	}
	return a.query.Location
}

// trackerBuckets returns how the store groups the hours [from, to) of a query
// by Tracker group: by local day of the query zone for a day bucket.
func (a *answer) trackerBuckets(ctx context.Context, store *models.TransferHistoryStore, from, to int64) (models.TransferHistoryBuckets, error) {
	if a.query.Bucket != BucketDay {
		return models.TransferHistoryBuckets{Width: map[Bucket]int64{BucketHour: 1, BucketTotal: 0}[a.query.Bucket]}, nil
	}
	// Only the recorded hours hold rows, so the offsets cover only them. An hour
	// before or after them gets the nearest offset.
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

// zoneOffsets returns the offsets from UTC of loc in the hours [fromHour,
// toHour), with the first hour of each.
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
		// Past the zone data, Go can return a zone period that ends where it
		// starts, for example on 2040-12-31 in America/New_York.
		if !end.After(start) {
			end = start.Add(time.Hour)
		}
		start = end
	}
}

// startOfLocalDay returns the first instant in loc of the date of day. Where
// summer time starts at midnight, the day starts at the change, not at a
// midnight that does not exist.
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
	buckets, err := a.trackerBuckets(ctx, store, from, to)
	if err != nil {
		return err
	}
	totals, err := store.TrackerTotals(ctx, a.query.InstanceID, from, to, buckets)
	if err != nil {
		return err
	}
	for _, t := range totals {
		r := a.row(groupOf(a.mapper, t.TrackerDomain, t.Public), t.Bucket, a.query.Bucket, a.startOf(a.query.Bucket, t.Bucket))
		addBytes(&r.Bytes, t.Uploaded, t.Downloaded, t.StatisticsCrossSeed)
	}
	if err := a.addBaselines(ctx, store, func(b models.TransferHistoryBaseline) string {
		return groupOf(a.mapper, b.TrackerDomain, b.Private != nil && !*b.Private)
	}); err != nil {
		return err
	}
	return a.addGaps(ctx, store, from, to, buckets)
}

// byCategory reads the daily totals, because qui stores the category totals
// for each day.
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
	return a.addGaps(ctx, store, from*24, to*24, buckets)
}

func (a *answer) addGaps(ctx context.Context, store *models.TransferHistoryStore, fromHour, toHour int64, buckets models.TransferHistoryBuckets) error {
	gaps, err := store.GapSeconds(ctx, a.query.InstanceID, fromHour, toHour, buckets)
	if err != nil {
		return err
	}
	for key, r := range a.rows {
		r.GapSeconds = gaps[key.bucket]
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
