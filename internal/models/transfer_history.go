// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// TransferHistoryStore holds the tables of the Transfer history. Only the
// Transfer history module in internal/services/transferhistory calls it.
type TransferHistoryStore struct {
	db dbinterface.Querier
}

func NewTransferHistoryStore(db dbinterface.Querier) *TransferHistoryStore {
	return &TransferHistoryStore{db: db}
}

// TransferHistoryInstance is an instance that has history. Name is the name
// of the instance at the last write, and stays when the user removes it.
type TransferHistoryInstance struct {
	InstanceID      int
	Name            string
	FirstRecordedAt time.Time
	LastReadingAt   time.Time
}

// TransferHistoryRecord is the stored record of one torrent. ID is 0 until a
// write stores the record.
type TransferHistoryRecord struct {
	ID                  int64
	Hash                string
	Name                string
	TrackerDomain       string
	Category            string
	Private             *bool
	Size                int64
	ReleaseGroup        string
	Resolution          string
	Source              string
	ContentType         string
	StatisticsCrossSeed *bool
	LastUploaded        int64
	LastDownloaded      int64
	UploadFirst24h      *int64
	BaselineUploaded    int64
	BaselineDownloaded  int64
}

// TransferHistoryTorrentDay is the transfer of one torrent on one UTC day.
// Day counts days since 1970-01-01.
type TransferHistoryTorrentDay struct {
	Record     *TransferHistoryRecord
	Day        int64
	Uploaded   int64
	Downloaded int64
}

// TransferHistoryTotal is the transfer of one key in one bucket. Bucket is an
// hour or a day, counted since 1970-01-01 UTC, or 0 for one bucket.
type TransferHistoryTotal struct {
	Bucket              int64
	TrackerDomain       string
	Public              bool
	Category            string
	StatisticsCrossSeed bool
	Uploaded            int64
	Downloaded          int64
}

// TransferHistoryGap is the length of the Recording gaps that ended in one hour.
type TransferHistoryGap struct {
	Hour    int64
	Seconds int64
}

// TransferHistoryWrite is one write of one instance. Records holds each
// record with ID 0 and each record that changed in more than its last
// counters. Counters holds each record whose last counters changed.
// TorrentDays, TrackerHours, CategoryDays, and Gaps add to the stored rows.
type TransferHistoryWrite struct {
	InstanceID      int
	FirstRecordedAt time.Time
	LastReadingAt   time.Time
	Records         []*TransferHistoryRecord
	Counters        []*TransferHistoryRecord
	TorrentDays     []TransferHistoryTorrentDay
	TrackerHours    []TransferHistoryTotal
	CategoryDays    []TransferHistoryTotal
	Gaps            []TransferHistoryGap
}

// TransferHistoryBaseline is the Baseline of the torrents that share a key.
type TransferHistoryBaseline struct {
	TrackerDomain       string
	Private             *bool
	Category            string
	StatisticsCrossSeed bool
	Uploaded            int64
	Downloaded          int64
}

const transferHistoryChunk = 200

// LoadInstance returns the instance row, or nil when the instance has no
// history, and its records.
func (s *TransferHistoryStore) LoadInstance(ctx context.Context, instanceID int) (*TransferHistoryInstance, []*TransferHistoryRecord, error) {
	inst := TransferHistoryInstance{InstanceID: instanceID}
	err := s.db.QueryRowContext(ctx, `
		SELECT name, first_recorded_at, last_reading_at
		FROM transfer_history_instances
		WHERE instance_id = ?`, instanceID).Scan(&inst.Name, &inst.FirstRecordedAt, &inst.LastReadingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load transfer history instance: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.hash, t.name, t.tracker_domain, t.category, t.private, t.size, t.release_group,
			t.resolution, t.source, t.content_type, t.statistics_cross_seed, c.uploaded, c.downloaded,
			t.upload_first_24h, t.baseline_uploaded, t.baseline_downloaded
		FROM transfer_history_torrents t
		JOIN transfer_history_counters c ON c.torrent_id = t.id
		WHERE t.instance_id = ?`, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("load transfer history records: %w", err)
	}
	defer rows.Close()

	var records []*TransferHistoryRecord
	for rows.Next() {
		var r TransferHistoryRecord
		var private, crossSeed sql.NullBool
		var first24h sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Hash, &r.Name, &r.TrackerDomain, &r.Category, &private, &r.Size,
			&r.ReleaseGroup, &r.Resolution, &r.Source, &r.ContentType, &crossSeed,
			&r.LastUploaded, &r.LastDownloaded, &first24h, &r.BaselineUploaded, &r.BaselineDownloaded); err != nil {
			return nil, nil, fmt.Errorf("scan transfer history record: %w", err)
		}
		if private.Valid {
			r.Private = &private.Bool
		}
		if crossSeed.Valid {
			r.StatisticsCrossSeed = &crossSeed.Bool
		}
		if first24h.Valid {
			r.UploadFirst24h = &first24h.Int64
		}
		records = append(records, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read transfer history records: %w", err)
	}
	return &inst, records, nil
}

// Write stores w in one transaction and returns the id of each record in
// Records, by hash. After an error, the stored rows stay as they were, unless
// the commit itself reported the error.
func (s *TransferHistoryStore) Write(ctx context.Context, w *TransferHistoryWrite) (map[string]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transfer history write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transfer_history_instances (instance_id, name, first_recorded_at, last_reading_at)
		VALUES (?, COALESCE((SELECT name FROM instances_view WHERE id = ?), ''), ?, ?)
		ON CONFLICT (instance_id) DO UPDATE SET
			name = COALESCE((SELECT name FROM instances_view WHERE id = excluded.instance_id), transfer_history_instances.name),
			last_reading_at = excluded.last_reading_at`,
		w.InstanceID, w.InstanceID, w.FirstRecordedAt, w.LastReadingAt); err != nil {
		return nil, fmt.Errorf("write transfer history instance: %w", err)
	}

	ids, err := upsertTransferHistoryRecords(ctx, tx, w.InstanceID, w.Records)
	if err != nil {
		return nil, err
	}
	idOf := func(r *TransferHistoryRecord) int64 {
		if r.ID != 0 {
			return r.ID
		}
		return ids[r.Hash]
	}

	if err := execTransferHistoryChunks(ctx, tx, w.Counters, 3, `
		INSERT INTO transfer_history_counters (torrent_id, uploaded, downloaded)
		VALUES %s
		ON CONFLICT (torrent_id) DO UPDATE SET
			uploaded = excluded.uploaded,
			downloaded = excluded.downloaded`,
		func(r *TransferHistoryRecord) []any {
			return []any{idOf(r), r.LastUploaded, r.LastDownloaded}
		}); err != nil {
		return nil, fmt.Errorf("write transfer history counters: %w", err)
	}

	if err := execTransferHistoryChunks(ctx, tx, w.TorrentDays, 4, `
		INSERT INTO transfer_history_torrent_days (torrent_id, day, uploaded, downloaded)
		VALUES %s
		ON CONFLICT (torrent_id, day) DO UPDATE SET
			uploaded = transfer_history_torrent_days.uploaded + excluded.uploaded,
			downloaded = transfer_history_torrent_days.downloaded + excluded.downloaded`,
		func(d TransferHistoryTorrentDay) []any {
			return []any{idOf(d.Record), d.Day, d.Uploaded, d.Downloaded}
		}); err != nil {
		return nil, fmt.Errorf("write transfer history torrent days: %w", err)
	}

	if err := execTransferHistoryChunks(ctx, tx, w.TrackerHours, 7, `
		INSERT INTO transfer_history_tracker_hours
			(instance_id, hour, tracker_domain, public, statistics_cross_seed, uploaded, downloaded)
		VALUES %s
		ON CONFLICT (instance_id, hour, tracker_domain, public, statistics_cross_seed) DO UPDATE SET
			uploaded = transfer_history_tracker_hours.uploaded + excluded.uploaded,
			downloaded = transfer_history_tracker_hours.downloaded + excluded.downloaded`,
		func(t TransferHistoryTotal) []any {
			return []any{w.InstanceID, t.Bucket, t.TrackerDomain, t.Public, t.StatisticsCrossSeed, t.Uploaded, t.Downloaded}
		}); err != nil {
		return nil, fmt.Errorf("write transfer history tracker hours: %w", err)
	}

	if err := execTransferHistoryChunks(ctx, tx, w.CategoryDays, 8, `
		INSERT INTO transfer_history_category_days
			(instance_id, day, tracker_domain, public, category, statistics_cross_seed, uploaded, downloaded)
		VALUES %s
		ON CONFLICT (instance_id, day, tracker_domain, public, category, statistics_cross_seed) DO UPDATE SET
			uploaded = transfer_history_category_days.uploaded + excluded.uploaded,
			downloaded = transfer_history_category_days.downloaded + excluded.downloaded`,
		func(t TransferHistoryTotal) []any {
			return []any{w.InstanceID, t.Bucket, t.TrackerDomain, t.Public, t.Category, t.StatisticsCrossSeed, t.Uploaded, t.Downloaded}
		}); err != nil {
		return nil, fmt.Errorf("write transfer history category days: %w", err)
	}

	if err := execTransferHistoryChunks(ctx, tx, w.Gaps, 3, `
		INSERT INTO transfer_history_gaps (instance_id, hour, seconds)
		VALUES %s
		ON CONFLICT (instance_id, hour) DO UPDATE SET
			seconds = transfer_history_gaps.seconds + excluded.seconds`,
		func(g TransferHistoryGap) []any {
			return []any{w.InstanceID, g.Hour, g.Seconds}
		}); err != nil {
		return nil, fmt.Errorf("write transfer history gaps: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transfer history write: %w", err)
	}
	return ids, nil
}

// LastReadingAt returns the time of the last reading that a write stored for
// an instance, or the zero time when the instance has no history.
func (s *TransferHistoryStore) LastReadingAt(ctx context.Context, instanceID int) (time.Time, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT last_reading_at FROM transfer_history_instances WHERE instance_id = ?`, instanceID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read transfer history last reading: %w", err)
	}
	return at, nil
}

// upsertTransferHistoryRecords writes the records and returns the id of each
// hash. An existing record keeps its Baseline.
func upsertTransferHistoryRecords(ctx context.Context, tx dbinterface.TxQuerier, instanceID int, records []*TransferHistoryRecord) (map[string]int64, error) {
	ids := make(map[string]int64, len(records))
	for chunk := range transferHistoryChunks(records) {
		args := make([]any, 0, len(chunk)*15)
		for _, r := range chunk {
			args = append(args, instanceID, r.Hash, r.Name, r.TrackerDomain, r.Category, r.Private, r.Size,
				r.ReleaseGroup, r.Resolution, r.Source, r.ContentType, r.StatisticsCrossSeed,
				r.UploadFirst24h, r.BaselineUploaded, r.BaselineDownloaded)
		}
		query := dbinterface.BuildQueryWithPlaceholders(`
			INSERT INTO transfer_history_torrents (instance_id, hash, name, tracker_domain, category, private,
				size, release_group, resolution, source, content_type, statistics_cross_seed,
				upload_first_24h, baseline_uploaded, baseline_downloaded)
			VALUES %s
			ON CONFLICT (instance_id, hash) DO UPDATE SET
				name = excluded.name,
				tracker_domain = excluded.tracker_domain,
				category = excluded.category,
				private = excluded.private,
				size = excluded.size,
				release_group = excluded.release_group,
				resolution = excluded.resolution,
				source = excluded.source,
				content_type = excluded.content_type,
				statistics_cross_seed = excluded.statistics_cross_seed,
				upload_first_24h = excluded.upload_first_24h
			RETURNING id, hash`, 15, len(chunk))
		if err := scanTransferHistoryIDs(ctx, tx, query, args, ids); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func scanTransferHistoryIDs(ctx context.Context, tx dbinterface.TxQuerier, query string, args []any, ids map[string]int64) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("write transfer history records: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return fmt.Errorf("scan transfer history record id: %w", err)
		}
		ids[hash] = id
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read transfer history record ids: %w", err)
	}
	return nil
}

func execTransferHistoryChunks[T any](ctx context.Context, tx dbinterface.TxQuerier, items []T, perRow int, template string, values func(T) []any) error {
	for chunk := range transferHistoryChunks(items) {
		args := make([]any, 0, len(chunk)*perRow)
		for _, item := range chunk {
			args = append(args, values(item)...)
		}
		if _, err := tx.ExecContext(ctx, dbinterface.BuildQueryWithPlaceholders(template, perRow, len(chunk)), args...); err != nil {
			return err
		}
	}
	return nil
}

func transferHistoryChunks[T any](items []T) func(func([]T) bool) {
	return func(yield func([]T) bool) {
		for start := 0; start < len(items); start += transferHistoryChunk {
			if !yield(items[start:min(start+transferHistoryChunk, len(items))]) {
				return
			}
		}
	}
}

// transferHistoryBucket returns the SQL bucket of a column that counts units, for
// buckets of width units, and the GROUP BY term that starts with it. Width 0
// is one bucket.
func transferHistoryBucket(column string, width int64) (bucket, groupBy string) {
	switch width {
	case 0:
		return "0", ""
	case 1:
		return column, column + ", "
	default:
		bucket = fmt.Sprintf("%s / %d", column, width)
		return bucket, bucket + ", "
	}
}

// TrackerTotals returns the hourly totals of hours [fromHour, toHour), by
// tracker domain, in buckets of width hours. Width 0 is one bucket.
func (s *TransferHistoryStore) TrackerTotals(ctx context.Context, instanceID int, fromHour, toHour, width int64) ([]TransferHistoryTotal, error) {
	bucket, groupBy := transferHistoryBucket("hour", width)
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+bucket+`, tracker_domain, public, statistics_cross_seed,
			CAST(SUM(uploaded) AS BIGINT), CAST(SUM(downloaded) AS BIGINT)
		FROM transfer_history_tracker_hours
		WHERE instance_id = ? AND hour >= ? AND hour < ?
		GROUP BY `+groupBy+`tracker_domain, public, statistics_cross_seed`,
		instanceID, fromHour, toHour)
	if err != nil {
		return nil, fmt.Errorf("read transfer history tracker totals: %w", err)
	}
	defer rows.Close()
	var totals []TransferHistoryTotal
	for rows.Next() {
		var t TransferHistoryTotal
		if err := rows.Scan(&t.Bucket, &t.TrackerDomain, &t.Public, &t.StatisticsCrossSeed, &t.Uploaded, &t.Downloaded); err != nil {
			return nil, fmt.Errorf("scan transfer history tracker total: %w", err)
		}
		totals = append(totals, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history tracker totals: %w", err)
	}
	return totals, nil
}

// CategoryTotals returns the daily totals of days [fromDay, toDay), by
// category, for each day, or as one bucket when total is set.
func (s *TransferHistoryStore) CategoryTotals(ctx context.Context, instanceID int, fromDay, toDay int64, total bool) ([]TransferHistoryTotal, error) {
	width := int64(1)
	if total {
		width = 0
	}
	bucket, groupBy := transferHistoryBucket("day", width)
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+bucket+`, category, statistics_cross_seed,
			CAST(SUM(uploaded) AS BIGINT), CAST(SUM(downloaded) AS BIGINT)
		FROM transfer_history_category_days
		WHERE instance_id = ? AND day >= ? AND day < ?
		GROUP BY `+groupBy+`category, statistics_cross_seed`,
		instanceID, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("read transfer history category totals: %w", err)
	}
	defer rows.Close()
	var totals []TransferHistoryTotal
	for rows.Next() {
		var t TransferHistoryTotal
		if err := rows.Scan(&t.Bucket, &t.Category, &t.StatisticsCrossSeed, &t.Uploaded, &t.Downloaded); err != nil {
			return nil, fmt.Errorf("scan transfer history category total: %w", err)
		}
		totals = append(totals, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history category totals: %w", err)
	}
	return totals, nil
}

// GapSeconds returns the length of the Recording gaps in hours
// [fromHour, toHour), by bucket of width hours. Width 0 is one bucket.
func (s *TransferHistoryStore) GapSeconds(ctx context.Context, instanceID int, fromHour, toHour, width int64) (map[int64]int64, error) {
	bucket, groupBy := transferHistoryBucket("hour", width)
	query := `
		SELECT ` + bucket + `, CAST(COALESCE(SUM(seconds), 0) AS BIGINT)
		FROM transfer_history_gaps
		WHERE instance_id = ? AND hour >= ? AND hour < ?`
	if groupBy != "" {
		query += " GROUP BY " + strings.TrimSuffix(groupBy, ", ")
	}
	rows, err := s.db.QueryContext(ctx, query,
		instanceID, fromHour, toHour)
	if err != nil {
		return nil, fmt.Errorf("read transfer history gaps: %w", err)
	}
	defer rows.Close()
	gaps := make(map[int64]int64)
	for rows.Next() {
		var b, seconds int64
		if err := rows.Scan(&b, &seconds); err != nil {
			return nil, fmt.Errorf("scan transfer history gap: %w", err)
		}
		gaps[b] = seconds
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history gaps: %w", err)
	}
	return gaps, nil
}

// Baselines returns the Baseline of the records of an instance, by the
// tracker domain, private flag, category, and Statistics cross-seed flag of
// each record now.
func (s *TransferHistoryStore) Baselines(ctx context.Context, instanceID int) ([]TransferHistoryBaseline, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tracker_domain, private, category, COALESCE(statistics_cross_seed, FALSE),
			CAST(SUM(baseline_uploaded) AS BIGINT), CAST(SUM(baseline_downloaded) AS BIGINT)
		FROM transfer_history_torrents
		WHERE instance_id = ? AND (baseline_uploaded > 0 OR baseline_downloaded > 0)
		GROUP BY tracker_domain, private, category, COALESCE(statistics_cross_seed, FALSE)`,
		instanceID)
	if err != nil {
		return nil, fmt.Errorf("read transfer history baselines: %w", err)
	}
	defer rows.Close()
	var baselines []TransferHistoryBaseline
	for rows.Next() {
		var b TransferHistoryBaseline
		var private sql.NullBool
		if err := rows.Scan(&b.TrackerDomain, &private, &b.Category, &b.StatisticsCrossSeed, &b.Uploaded, &b.Downloaded); err != nil {
			return nil, fmt.Errorf("scan transfer history baseline: %w", err)
		}
		if private.Valid {
			b.Private = &private.Bool
		}
		baselines = append(baselines, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history baselines: %w", err)
	}
	return baselines, nil
}

// Instances returns the instances that have history, by instance id.
func (s *TransferHistoryStore) Instances(ctx context.Context) ([]TransferHistoryInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT instance_id, name, first_recorded_at, last_reading_at
		FROM transfer_history_instances
		ORDER BY instance_id`)
	if err != nil {
		return nil, fmt.Errorf("read transfer history instances: %w", err)
	}
	defer rows.Close()
	var instances []TransferHistoryInstance
	for rows.Next() {
		var inst TransferHistoryInstance
		if err := rows.Scan(&inst.InstanceID, &inst.Name, &inst.FirstRecordedAt, &inst.LastReadingAt); err != nil {
			return nil, fmt.Errorf("scan transfer history instance: %w", err)
		}
		instances = append(instances, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history instances: %w", err)
	}
	return instances, nil
}
