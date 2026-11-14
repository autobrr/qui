// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
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
	// Writes is the number of writes that stored the instance.
	Writes int64
	// CompleteReadingAt is the time of the last reading that a write stored
	// with all its parts, or nil while the first write lacks parts.
	CompleteReadingAt *time.Time
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
	// BaselineTrackerDomain and BaselineCategory are the values at the first
	// sight of the torrent. Only the insert of a record stores them, so
	// LoadInstance leaves them empty.
	BaselineTrackerDomain string
	BaselineCategory      string
	// BaselineStatisticsCrossSeed is the Statistics cross-seed flag of the
	// Baseline. It is nil while the Baseline follows StatisticsCrossSeed.
	BaselineStatisticsCrossSeed *bool
	// Deleted is set while the torrent is not in qBittorrent.
	Deleted bool
	// Completed is set when the torrent completed since the user added it last.
	Completed bool
	// Final holds the values of the last reading that held the torrent. No
	// column stores it, so LoadInstance leaves it nil.
	Final *TransferHistoryFinal
}

// TransferHistoryFinal is what a delete event keeps of a torrent, apart from
// its last counters.
type TransferHistoryFinal struct {
	Ratio          float64
	SeedingSeconds int64
}

type TransferHistoryEventKind string

const (
	TransferHistoryEventAdd        TransferHistoryEventKind = "add"
	TransferHistoryEventCompletion TransferHistoryEventKind = "completion"
	TransferHistoryEventDelete     TransferHistoryEventKind = "delete"
)

// TransferHistoryEvent is one add, completion, or delete of a torrent.
// Delete is set on a delete event only.
type TransferHistoryEvent struct {
	Record *TransferHistoryRecord
	Kind   TransferHistoryEventKind
	Time   time.Time
	Delete *TransferHistoryDelete
}

// TransferHistoryDelete is what a delete event holds. Ratio and
// SeedingSeconds are nil when no reading held the torrent since qui started.
// CauseName is the name of the automation rule or of the Client Proxy API key.
type TransferHistoryDelete struct {
	Uploaded       int64
	Downloaded     int64
	Ratio          *float64
	SeedingSeconds *int64
	CauseKind      string
	CauseName      string
}

type TransferHistoryHashChange struct {
	From string
	To   string
}

// TransferHistoryDeleteRow is a stored delete event with its torrent.
type TransferHistoryDeleteRow struct {
	Time          time.Time
	Hash          string
	Name          string
	TrackerDomain string
	Private       *bool
	TransferHistoryDelete
}

// TransferHistoryEventCount is the number of events of one kind on one UTC
// day. Day counts days since 1970-01-01.
type TransferHistoryEventCount struct {
	Day   int64
	Kind  TransferHistoryEventKind
	Count int64
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

// TransferHistoryLibraryDay is the library of an instance on one UTC day.
// Day counts days since 1970-01-01. UniqueSize is nil for a day without a
// completed Copy groups build.
type TransferHistoryLibraryDay struct {
	Day        int64
	Torrents   int64
	TotalSize  int64
	UniqueSize *int64
}

// TransferHistoryInstanceHour is the state of an instance in one UTC hour.
// Hour counts hours since 1970-01-01. States holds the torrent count of each
// qBittorrent state. The speeds are the highest of the readings of the hour,
// in bytes per second. A reading holds the highest speed since the reading
// before it, so up to one minute before the hour can count toward it.
type TransferHistoryInstanceHour struct {
	Hour             int64
	States           map[string]int64
	FreeDisk         int64
	MaxUploadSpeed   int64
	MaxDownloadSpeed int64
}

// TransferHistoryWrite is one write of one instance. Records holds each
// record with ID 0 and each record that changed in more than its last
// counters. Counters holds each record whose last counters changed.
// TorrentDays, TrackerHours, CategoryDays, and Gaps add to the stored rows.
// LibraryDays and InstanceHours replace the stored rows of their day or hour,
// except that a stored Unique size stays when the write has none, and the
// speeds keep the highest value.
type TransferHistoryWrite struct {
	InstanceID      int
	FirstRecordedAt time.Time
	LastReadingAt   time.Time
	// Number is the number of this write of the instance: one more than the
	// stored writes.
	Number int64
	// CompleteReadingAt is the stored CompleteReadingAt of the instance after
	// this write.
	CompleteReadingAt *time.Time
	Records           []*TransferHistoryRecord
	Counters          []*TransferHistoryRecord
	TorrentDays       []TransferHistoryTorrentDay
	TrackerHours      []TransferHistoryTotal
	CategoryDays      []TransferHistoryTotal
	Gaps              []TransferHistoryGap
	HashChanges       []TransferHistoryHashChange
	Events            []TransferHistoryEvent
	LibraryDays       []TransferHistoryLibraryDay
	InstanceHours     []TransferHistoryInstanceHour
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

// ErrTransferHistoryCommitUnknown is the error of a write whose commit
// returned an error. The database can hold the write or not.
var ErrTransferHistoryCommitUnknown = errors.New("transfer history commit outcome unknown")

// ErrTransferHistoryWriteNumber is the error of a write whose Number is not
// one more than the stored writes of the instance, as when the database
// already holds a write with that number. The write stores nothing.
var ErrTransferHistoryWriteNumber = errors.New("transfer history write number does not follow the stored writes")

// LoadInstance returns the instance row, or nil when the instance has no
// history, and its records.
func (s *TransferHistoryStore) LoadInstance(ctx context.Context, instanceID int) (*TransferHistoryInstance, []*TransferHistoryRecord, error) {
	inst := TransferHistoryInstance{InstanceID: instanceID}
	var complete sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT name, first_recorded_at, last_reading_at, writes, complete_reading_at
		FROM transfer_history_instances
		WHERE instance_id = ?`, instanceID).Scan(&inst.Name, &inst.FirstRecordedAt, &inst.LastReadingAt, &inst.Writes, &complete)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load transfer history instance: %w", err)
	}
	if complete.Valid {
		inst.CompleteReadingAt = &complete.Time
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.hash, t.name, t.tracker_domain, t.category, t.private, t.size, t.release_group,
			t.resolution, t.source, t.content_type, t.statistics_cross_seed, c.uploaded, c.downloaded,
			t.upload_first_24h, t.baseline_uploaded, t.baseline_downloaded, t.deleted, t.completed,
			t.baseline_statistics_cross_seed
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
		var private, crossSeed, baselineCrossSeed sql.NullBool
		var first24h sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Hash, &r.Name, &r.TrackerDomain, &r.Category, &private, &r.Size,
			&r.ReleaseGroup, &r.Resolution, &r.Source, &r.ContentType, &crossSeed,
			&r.LastUploaded, &r.LastDownloaded, &first24h, &r.BaselineUploaded, &r.BaselineDownloaded, &r.Deleted, &r.Completed, &baselineCrossSeed); err != nil {
			return nil, nil, fmt.Errorf("scan transfer history record: %w", err)
		}
		if private.Valid {
			r.Private = &private.Bool
		}
		if crossSeed.Valid {
			r.StatisticsCrossSeed = &crossSeed.Bool
		}
		if baselineCrossSeed.Valid {
			r.BaselineStatisticsCrossSeed = &baselineCrossSeed.Bool
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
// the error is ErrTransferHistoryCommitUnknown. It returns
// ErrTransferHistoryWriteNumber when w.Number does not follow the stored writes.
func (s *TransferHistoryStore) Write(ctx context.Context, w *TransferHistoryWrite) (map[string]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transfer history write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO transfer_history_instances (instance_id, name, first_recorded_at, last_reading_at, writes, complete_reading_at)
		VALUES (?, COALESCE((SELECT name FROM instances_view WHERE id = ?), ''), ?, ?, ?, ?)
		ON CONFLICT (instance_id) DO UPDATE SET
			name = COALESCE((SELECT name FROM instances_view WHERE id = excluded.instance_id), transfer_history_instances.name),
			last_reading_at = excluded.last_reading_at,
			writes = excluded.writes,
			complete_reading_at = excluded.complete_reading_at
		WHERE transfer_history_instances.writes = excluded.writes - 1`,
		w.InstanceID, w.InstanceID, w.FirstRecordedAt, w.LastReadingAt, w.Number, w.CompleteReadingAt)
	if err != nil {
		return nil, fmt.Errorf("write transfer history instance: %w", err)
	}
	if stored, err := result.RowsAffected(); err != nil {
		return nil, fmt.Errorf("write transfer history instance: %w", err)
	} else if stored == 0 {
		return nil, ErrTransferHistoryWriteNumber
	}

	// A record that no write stored yet has no row to change.
	for _, change := range w.HashChanges {
		if _, err := tx.ExecContext(ctx, `
			UPDATE transfer_history_torrents SET hash = ? WHERE instance_id = ? AND hash = ?`,
			change.To, w.InstanceID, change.From); err != nil {
			return nil, fmt.Errorf("write transfer history hash change: %w", err)
		}
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

	if err := execTransferHistoryChunks(ctx, tx, w.Events, 10, `
		INSERT INTO transfer_history_events
			(instance_id, torrent_id, kind, second, uploaded, downloaded, ratio, seeding_seconds, cause_kind, cause_name)
		VALUES %s`,
		func(e TransferHistoryEvent) []any {
			d := e.Delete
			if d == nil {
				return []any{w.InstanceID, idOf(e.Record), e.Kind, e.Time.Unix(), nil, nil, nil, nil, nil, nil}
			}
			return []any{w.InstanceID, idOf(e.Record), e.Kind, e.Time.Unix(), d.Uploaded, d.Downloaded, d.Ratio, d.SeedingSeconds, d.CauseKind, d.CauseName}
		}); err != nil {
		return nil, fmt.Errorf("write transfer history events: %w", err)
	}

	if err := writeTransferHistoryLibrary(ctx, tx, w); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTransferHistoryCommitUnknown, err)
	}
	return ids, nil
}

func writeTransferHistoryLibrary(ctx context.Context, tx dbinterface.TxQuerier, w *TransferHistoryWrite) error {
	if err := execTransferHistoryChunks(ctx, tx, w.LibraryDays, 5, `
		INSERT INTO transfer_history_library_days (instance_id, day, torrents, total_size, unique_size)
		VALUES %s
		ON CONFLICT (instance_id, day) DO UPDATE SET
			torrents = excluded.torrents,
			total_size = excluded.total_size,
			unique_size = COALESCE(excluded.unique_size, transfer_history_library_days.unique_size)`,
		func(d TransferHistoryLibraryDay) []any {
			return []any{w.InstanceID, d.Day, d.Torrents, d.TotalSize, d.UniqueSize}
		}); err != nil {
		return fmt.Errorf("write transfer history library days: %w", err)
	}

	if err := execTransferHistoryChunks(ctx, tx, w.InstanceHours, 5, `
		INSERT INTO transfer_history_instance_hours (instance_id, hour, free_disk, max_upload_speed, max_download_speed)
		VALUES %s
		ON CONFLICT (instance_id, hour) DO UPDATE SET
			free_disk = excluded.free_disk,
			max_upload_speed = CASE WHEN excluded.max_upload_speed > transfer_history_instance_hours.max_upload_speed
				THEN excluded.max_upload_speed ELSE transfer_history_instance_hours.max_upload_speed END,
			max_download_speed = CASE WHEN excluded.max_download_speed > transfer_history_instance_hours.max_download_speed
				THEN excluded.max_download_speed ELSE transfer_history_instance_hours.max_download_speed END`,
		func(h TransferHistoryInstanceHour) []any {
			return []any{w.InstanceID, h.Hour, h.FreeDisk, h.MaxUploadSpeed, h.MaxDownloadSpeed}
		}); err != nil {
		return fmt.Errorf("write transfer history instance hours: %w", err)
	}

	// The state counts of an hour are those of its last reading, so they
	// replace the stored counts of the hour.
	type stateCount struct {
		hour     int64
		state    string
		torrents int64
	}
	var states []stateCount
	for _, h := range w.InstanceHours {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM transfer_history_state_hours WHERE instance_id = ? AND hour = ?`, w.InstanceID, h.Hour); err != nil {
			return fmt.Errorf("write transfer history state hours: %w", err)
		}
		for state, torrents := range h.States {
			states = append(states, stateCount{hour: h.Hour, state: state, torrents: torrents})
		}
	}
	if err := execTransferHistoryChunks(ctx, tx, states, 4, `
		INSERT INTO transfer_history_state_hours (instance_id, hour, state, torrents)
		VALUES %s`,
		func(c stateCount) []any {
			return []any{w.InstanceID, c.hour, c.state, c.torrents}
		}); err != nil {
		return fmt.Errorf("write transfer history state hours: %w", err)
	}
	return nil
}

// Writes returns the number of writes that stored an instance, or 0 when the
// instance has no history.
func (s *TransferHistoryStore) Writes(ctx context.Context, instanceID int) (int64, error) {
	var writes int64
	err := s.db.QueryRowContext(ctx, `
		SELECT writes FROM transfer_history_instances WHERE instance_id = ?`, instanceID).Scan(&writes)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read transfer history writes: %w", err)
	}
	return writes, nil
}

// upsertTransferHistoryRecords writes the records and returns the id of each
// hash. An existing record keeps its Baseline, with its tracker domain and category.
func upsertTransferHistoryRecords(ctx context.Context, tx dbinterface.TxQuerier, instanceID int, records []*TransferHistoryRecord) (map[string]int64, error) {
	ids := make(map[string]int64, len(records))
	for chunk := range transferHistoryChunks(records) {
		args := make([]any, 0, len(chunk)*20)
		for _, r := range chunk {
			args = append(args, instanceID, r.Hash, r.Name, r.TrackerDomain, r.Category, r.Private, r.Size,
				r.ReleaseGroup, r.Resolution, r.Source, r.ContentType, r.StatisticsCrossSeed,
				r.UploadFirst24h, r.BaselineUploaded, r.BaselineDownloaded, r.BaselineTrackerDomain, r.BaselineCategory, r.Deleted, r.Completed,
				r.BaselineStatisticsCrossSeed)
		}
		query := dbinterface.BuildQueryWithPlaceholders(`
			INSERT INTO transfer_history_torrents (instance_id, hash, name, tracker_domain, category, private,
				size, release_group, resolution, source, content_type, statistics_cross_seed,
				upload_first_24h, baseline_uploaded, baseline_downloaded, baseline_tracker_domain, baseline_category,
				deleted, completed, baseline_statistics_cross_seed)
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
				upload_first_24h = excluded.upload_first_24h,
				deleted = excluded.deleted,
				completed = excluded.completed,
				baseline_statistics_cross_seed = excluded.baseline_statistics_cross_seed
			RETURNING id, hash`, 20, len(chunk))
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

// TransferHistoryOffset is the offset of a time zone from UTC, in minutes,
// from the hour FromHour on.
type TransferHistoryOffset struct {
	FromHour int64
	Minutes  int64
}

// TransferHistoryBuckets groups the hours of a query: in buckets of Width
// hours, where Width 0 is one bucket, or by local day when Offsets is set. A
// local day is the number of days since 1970-01-01 in the time zone of
// Offsets, and an hour counts toward the local day of its start. Offsets is
// in hour order, and its first entry covers each hour before the second.
type TransferHistoryBuckets struct {
	Width   int64
	Offsets []TransferHistoryOffset
}

func (b TransferHistoryBuckets) sql() (bucket, groupBy string) {
	if len(b.Offsets) == 0 {
		return transferHistoryBucket("hour", b.Width)
	}
	// The newest offset comes first, because most rows are recent. The sum is in
	// minutes, because the hour column is a 32-bit integer on Postgres.
	day := func(minutes int64) string { return fmt.Sprintf("(hour * 60 + (%d)) / 1440", minutes) }
	bucket = day(b.Offsets[0].Minutes)
	if last := len(b.Offsets) - 1; last > 0 {
		var sb strings.Builder
		sb.WriteString("CASE")
		for i := last; i > 0; i-- {
			fmt.Fprintf(&sb, " WHEN hour >= %d THEN %s", b.Offsets[i].FromHour, day(b.Offsets[i].Minutes))
		}
		sb.WriteString(" ELSE " + bucket + " END")
		bucket = sb.String()
	}
	return bucket, bucket + ", "
}

// TrackerTotals returns the hourly totals of hours [fromHour, toHour), by
// tracker domain, in buckets.
func (s *TransferHistoryStore) TrackerTotals(ctx context.Context, instanceID int, fromHour, toHour int64, buckets TransferHistoryBuckets) ([]TransferHistoryTotal, error) {
	bucket, groupBy := buckets.sql()
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
// [fromHour, toHour), by bucket.
func (s *TransferHistoryStore) GapSeconds(ctx context.Context, instanceID int, fromHour, toHour int64, buckets TransferHistoryBuckets) (map[int64]int64, error) {
	bucket, groupBy := buckets.sql()
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
// tracker domain and category at the first sight of each record, by its
// private flag now, and by the Statistics cross-seed flag of its first life.
func (s *TransferHistoryStore) Baselines(ctx context.Context, instanceID int) ([]TransferHistoryBaseline, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT baseline_tracker_domain, private, baseline_category,
			COALESCE(baseline_statistics_cross_seed, statistics_cross_seed, FALSE),
			CAST(SUM(baseline_uploaded) AS BIGINT), CAST(SUM(baseline_downloaded) AS BIGINT)
		FROM transfer_history_torrents
		WHERE instance_id = ? AND (baseline_uploaded > 0 OR baseline_downloaded > 0)
		GROUP BY baseline_tracker_domain, private, baseline_category,
			COALESCE(baseline_statistics_cross_seed, statistics_cross_seed, FALSE)`,
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

// EventCounts returns the number of events of each kind on each UTC day of
// days [fromDay, toDay).
func (s *TransferHistoryStore) EventCounts(ctx context.Context, instanceID int, fromDay, toDay int64) ([]TransferHistoryEventCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT second / 86400, kind, COUNT(*)
		FROM transfer_history_events
		WHERE instance_id = ? AND kind IN (?, ?, ?) AND second >= ? AND second < ?
		GROUP BY second / 86400, kind`,
		instanceID, TransferHistoryEventAdd, TransferHistoryEventCompletion, TransferHistoryEventDelete,
		fromDay*86400, toDay*86400)
	if err != nil {
		return nil, fmt.Errorf("read transfer history event counts: %w", err)
	}
	defer rows.Close()
	var counts []TransferHistoryEventCount
	for rows.Next() {
		var c TransferHistoryEventCount
		if err := rows.Scan(&c.Day, &c.Kind, &c.Count); err != nil {
			return nil, fmt.Errorf("scan transfer history event count: %w", err)
		}
		counts = append(counts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history event counts: %w", err)
	}
	return counts, nil
}

// LibraryDays returns the stored library of each UTC day of days [fromDay,
// toDay), with the oldest day first.
func (s *TransferHistoryStore) LibraryDays(ctx context.Context, instanceID int, fromDay, toDay int64) ([]TransferHistoryLibraryDay, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT day, torrents, total_size, unique_size
		FROM transfer_history_library_days
		WHERE instance_id = ? AND day >= ? AND day < ?
		ORDER BY day`, instanceID, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("read transfer history library days: %w", err)
	}
	defer rows.Close()
	var days []TransferHistoryLibraryDay
	for rows.Next() {
		var d TransferHistoryLibraryDay
		var unique sql.NullInt64
		if err := rows.Scan(&d.Day, &d.Torrents, &d.TotalSize, &unique); err != nil {
			return nil, fmt.Errorf("scan transfer history library day: %w", err)
		}
		if unique.Valid {
			d.UniqueSize = new(unique.Int64)
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history library days: %w", err)
	}
	return days, nil
}

// transferHistoryDeletes is the WHERE clause of the delete events of seconds
// [fromSecond, toSecond). causeKinds limits them to those kinds of cause when
// it is not empty.
func transferHistoryDeletes(instanceID int, fromSecond, toSecond int64, causeKinds []string) (where string, args []any) {
	where = "e.instance_id = ? AND e.kind = ? AND e.second >= ? AND e.second < ?"
	args = []any{instanceID, TransferHistoryEventDelete, fromSecond, toSecond}
	if len(causeKinds) > 0 {
		where += " AND e.cause_kind IN (?" + strings.Repeat(", ?", len(causeKinds)-1) + ")"
		for _, kind := range causeKinds {
			args = append(args, kind)
		}
	}
	return where, args
}

// Deletes returns a page of the delete events of seconds
// [fromSecond, toSecond), with the newest first. causeKinds limits them to
// those kinds of cause when it is not empty.
func (s *TransferHistoryStore) Deletes(ctx context.Context, instanceID int, fromSecond, toSecond int64, causeKinds []string, limit, offset int) ([]TransferHistoryDeleteRow, error) {
	where, args := transferHistoryDeletes(instanceID, fromSecond, toSecond, causeKinds)
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.second, t.hash, t.name, t.tracker_domain, t.private, e.uploaded, e.downloaded,
			e.ratio, e.seeding_seconds, e.cause_kind, e.cause_name
		FROM transfer_history_events e
		JOIN transfer_history_torrents t ON t.id = e.torrent_id
		WHERE `+where+`
		ORDER BY e.second DESC, e.id DESC
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, fmt.Errorf("read transfer history deletes: %w", err)
	}
	defer rows.Close()
	var deletes []TransferHistoryDeleteRow
	for rows.Next() {
		var d TransferHistoryDeleteRow
		var second int64
		var private sql.NullBool
		var ratio sql.NullFloat64
		var seedingSeconds sql.NullInt64
		if err := rows.Scan(&second, &d.Hash, &d.Name, &d.TrackerDomain, &private, &d.Uploaded, &d.Downloaded,
			&ratio, &seedingSeconds, &d.CauseKind, &d.CauseName); err != nil {
			return nil, fmt.Errorf("scan transfer history delete: %w", err)
		}
		d.Time = time.Unix(second, 0).UTC()
		if private.Valid {
			d.Private = &private.Bool
		}
		if ratio.Valid {
			d.Ratio = &ratio.Float64
		}
		if seedingSeconds.Valid {
			d.SeedingSeconds = &seedingSeconds.Int64
		}
		deletes = append(deletes, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history deletes: %w", err)
	}
	return deletes, nil
}

// DeleteCauseCounts returns the number of delete events of seconds
// [fromSecond, toSecond), by kind of cause.
func (s *TransferHistoryStore) DeleteCauseCounts(ctx context.Context, instanceID int, fromSecond, toSecond int64) (map[string]int64, error) {
	where, args := transferHistoryDeletes(instanceID, fromSecond, toSecond, nil)
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.cause_kind, COUNT(*)
		FROM transfer_history_events e
		WHERE `+where+`
		GROUP BY e.cause_kind`, args...)
	if err != nil {
		return nil, fmt.Errorf("read transfer history delete counts: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int64)
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, fmt.Errorf("scan transfer history delete count: %w", err)
		}
		counts[kind] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history delete counts: %w", err)
	}
	return counts, nil
}

// TransferHistoryDeleteKey names a delete event by the hash of its torrent
// and its time in whole seconds since 1970-01-01 UTC.
type TransferHistoryDeleteKey struct {
	Hash   string
	Second int64
}

// DeleteEventsSince returns the delete events of an instance from fromSecond on.
func (s *TransferHistoryStore) DeleteEventsSince(ctx context.Context, instanceID int, fromSecond int64) (map[TransferHistoryDeleteKey]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.hash, e.second
		FROM transfer_history_events e
		JOIN transfer_history_torrents t ON t.id = e.torrent_id
		WHERE e.instance_id = ? AND e.kind = ? AND e.second >= ?`,
		instanceID, TransferHistoryEventDelete, fromSecond)
	if err != nil {
		return nil, fmt.Errorf("read transfer history delete events: %w", err)
	}
	defer rows.Close()
	events := make(map[TransferHistoryDeleteKey]struct{})
	for rows.Next() {
		var key TransferHistoryDeleteKey
		if err := rows.Scan(&key.Hash, &key.Second); err != nil {
			return nil, fmt.Errorf("scan transfer history delete event: %w", err)
		}
		events[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history delete events: %w", err)
	}
	return events, nil
}

// TransferHistorySettings are the Statistics settings. DetailRetentionDays is
// 0 when qui keeps the detail forever.
type TransferHistorySettings struct {
	Recording           bool
	DetailRetentionDays int
}

// DefaultTransferHistorySettings are the settings until the user saves others.
var DefaultTransferHistorySettings = TransferHistorySettings{Recording: true, DetailRetentionDays: 400}

const (
	// foldHourDays is the number of days of hourly totals that one delete of
	// a fold removes.
	foldHourDays = 7
	// foldTorrents is the number of torrents whose daily bytes one write of a
	// fold moves into yearly rows.
	foldTorrents = 50
)

// Settings returns the stored Statistics settings, or the defaults.
func (s *TransferHistoryStore) Settings(ctx context.Context) (TransferHistorySettings, error) {
	var settings TransferHistorySettings
	err := s.db.QueryRowContext(ctx, `
		SELECT recording, detail_retention_days FROM transfer_history_settings WHERE id = 1`).
		Scan(&settings.Recording, &settings.DetailRetentionDays)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultTransferHistorySettings, nil
	}
	if err != nil {
		return TransferHistorySettings{}, fmt.Errorf("read transfer history settings: %w", err)
	}
	return settings, nil
}

func (s *TransferHistoryStore) SaveSettings(ctx context.Context, settings TransferHistorySettings) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO transfer_history_settings (id, recording, detail_retention_days)
		VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			recording = excluded.recording,
			detail_retention_days = excluded.detail_retention_days`,
		settings.Recording, settings.DetailRetentionDays); err != nil {
		return fmt.Errorf("save transfer history settings: %w", err)
	}
	return nil
}

// Size returns the bytes that the tables of the Transfer history and their
// indexes take in the database.
func (s *TransferHistoryStore) Size(ctx context.Context) (int64, error) {
	if dbinterface.DialectOf(s.db) == "postgres" {
		var size int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT CAST(COALESCE(SUM(pg_total_relation_size(c.oid)), 0) AS BIGINT)
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind = 'r' AND n.nspname = current_schema() AND c.relname LIKE 'transfer_history_%'`).
			Scan(&size); err != nil {
			return 0, fmt.Errorf("read transfer history size: %w", err)
		}
		return size, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM sqlite_schema
		WHERE type IN ('table', 'index') AND tbl_name LIKE 'transfer_history_%'`)
	if err != nil {
		return 0, fmt.Errorf("read transfer history tables: %w", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan transfer history table: %w", err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read transfer history tables: %w", err)
	}

	// With a name, dbstat reads the pages of that table only.
	var size int64
	for _, name := range names {
		var pages int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(pgsize), 0) FROM dbstat WHERE name = ? AND aggregate = TRUE`, name).Scan(&pages); err != nil {
			return 0, fmt.Errorf("read the size of %s: %w", name, err)
		}
		size += pages
	}
	return size, nil
}

// HourlyDetailStart returns the first UTC day that has hourly totals of an
// instance. ok is false when the instance has none. A fold deletes whole days
// from the oldest on, so each day before it holds daily totals only.
func (s *TransferHistoryStore) HourlyDetailStart(ctx context.Context, instanceID int) (day int64, ok bool, err error) {
	var hour sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT MIN(hour) FROM transfer_history_tracker_hours WHERE instance_id = ?`, instanceID).Scan(&hour); err != nil {
		return 0, false, fmt.Errorf("read transfer history hourly detail: %w", err)
	}
	if !hour.Valid {
		return 0, false, nil
	}
	return hour.Int64 / 24, true, nil
}

// TrackerDayTotals returns the daily totals of days [fromDay, toDay), by
// tracker domain, for each day, or as one bucket when total is set.
func (s *TransferHistoryStore) TrackerDayTotals(ctx context.Context, instanceID int, fromDay, toDay int64, total bool) ([]TransferHistoryTotal, error) {
	width := int64(1)
	if total {
		width = 0
	}
	bucket, groupBy := transferHistoryBucket("day", width)
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+bucket+`, tracker_domain, public, statistics_cross_seed,
			CAST(SUM(uploaded) AS BIGINT), CAST(SUM(downloaded) AS BIGINT)
		FROM transfer_history_category_days
		WHERE instance_id = ? AND day >= ? AND day < ?
		GROUP BY `+groupBy+`tracker_domain, public, statistics_cross_seed`,
		instanceID, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("read transfer history tracker day totals: %w", err)
	}
	defer rows.Close()
	var totals []TransferHistoryTotal
	for rows.Next() {
		var t TransferHistoryTotal
		if err := rows.Scan(&t.Bucket, &t.TrackerDomain, &t.Public, &t.StatisticsCrossSeed, &t.Uploaded, &t.Downloaded); err != nil {
			return nil, fmt.Errorf("scan transfer history tracker day total: %w", err)
		}
		totals = append(totals, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history tracker day totals: %w", err)
	}
	return totals, nil
}

// TransferHistoryTorrentFacts is the record of one torrent as a query reads
// it. BaselineStatisticsCrossSeed is the Statistics cross-seed flag of its
// Baseline.
type TransferHistoryTorrentFacts struct {
	ID                          int64
	Hash                        string
	Name                        string
	TrackerDomain               string
	Private                     *bool
	Category                    string
	ReleaseGroup                string
	Resolution                  string
	Source                      string
	ContentType                 string
	StatisticsCrossSeed         bool
	Deleted                     bool
	UploadFirst24h              *int64
	BaselineUploaded            int64
	BaselineDownloaded          int64
	BaselineStatisticsCrossSeed bool
}

// TorrentFacts returns the records of the torrents of an instance, or the
// record of the torrent with hash when hash is set.
func (s *TransferHistoryStore) TorrentFacts(ctx context.Context, instanceID int, hash string) ([]TransferHistoryTorrentFacts, error) {
	where, args := "instance_id = ?", []any{instanceID}
	if hash != "" {
		where, args = where+" AND hash = ?", append(args, hash)
	}
	return s.torrentFacts(ctx, where, args)
}

// TorrentFactsOf returns the records of the torrents with ids.
func (s *TransferHistoryStore) TorrentFactsOf(ctx context.Context, ids []int64) ([]TransferHistoryTorrentFacts, error) {
	var facts []TransferHistoryTorrentFacts
	for chunk := range slices.Chunk(ids, torrentTransferChunk) {
		part, err := s.torrentFacts(ctx, "id IN ("+placeholders(len(chunk))+")", int64Args(chunk))
		if err != nil {
			return nil, err
		}
		facts = append(facts, part...)
	}
	return facts, nil
}

func (s *TransferHistoryStore) torrentFacts(ctx context.Context, where string, args []any) ([]TransferHistoryTorrentFacts, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, hash, name, tracker_domain, private, category, release_group, resolution, source,
			content_type, COALESCE(statistics_cross_seed, FALSE), deleted, upload_first_24h,
			baseline_uploaded, baseline_downloaded,
			COALESCE(baseline_statistics_cross_seed, statistics_cross_seed, FALSE)
		FROM transfer_history_torrents
		WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("read transfer history torrent facts: %w", err)
	}
	defer rows.Close()
	var facts []TransferHistoryTorrentFacts
	for rows.Next() {
		var f TransferHistoryTorrentFacts
		var private sql.NullBool
		var first24h sql.NullInt64
		if err := rows.Scan(&f.ID, &f.Hash, &f.Name, &f.TrackerDomain, &private, &f.Category, &f.ReleaseGroup,
			&f.Resolution, &f.Source, &f.ContentType, &f.StatisticsCrossSeed, &f.Deleted, &first24h,
			&f.BaselineUploaded, &f.BaselineDownloaded, &f.BaselineStatisticsCrossSeed); err != nil {
			return nil, fmt.Errorf("scan transfer history torrent facts: %w", err)
		}
		if private.Valid {
			f.Private = &private.Bool
		}
		if first24h.Valid {
			f.UploadFirst24h = &first24h.Int64
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history torrent facts: %w", err)
	}
	return facts, nil
}

// TransferHistoryTorrentBaseline is the Baseline of one torrent.
type TransferHistoryTorrentBaseline struct {
	TorrentID           int64
	Uploaded            int64
	Downloaded          int64
	StatisticsCrossSeed bool
}

// TorrentBaselines returns the Baseline of each torrent of an instance that
// has one.
func (s *TransferHistoryStore) TorrentBaselines(ctx context.Context, instanceID int) ([]TransferHistoryTorrentBaseline, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, baseline_uploaded, baseline_downloaded,
			COALESCE(baseline_statistics_cross_seed, statistics_cross_seed, FALSE)
		FROM transfer_history_torrents
		WHERE instance_id = ? AND (baseline_uploaded > 0 OR baseline_downloaded > 0)`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("read transfer history torrent baselines: %w", err)
	}
	defer rows.Close()
	var baselines []TransferHistoryTorrentBaseline
	for rows.Next() {
		var b TransferHistoryTorrentBaseline
		if err := rows.Scan(&b.TorrentID, &b.Uploaded, &b.Downloaded, &b.StatisticsCrossSeed); err != nil {
			return nil, fmt.Errorf("scan transfer history torrent baseline: %w", err)
		}
		baselines = append(baselines, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history torrent baselines: %w", err)
	}
	return baselines, nil
}

// TorrentDaysFoldedBefore returns the latest UTC day before which a fold moved
// the days of the torrents into years, or 0 before the first fold. Only the
// days before it can be in a yearly row.
func (s *TransferHistoryStore) TorrentDaysFoldedBefore(ctx context.Context) (int64, error) {
	var day int64
	err := s.db.QueryRowContext(ctx, `SELECT torrent_days_before FROM transfer_history_folds WHERE id = 1`).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read transfer history fold day: %w", err)
	}
	return day, nil
}

// TransferHistoryTorrentBytes is the transfer of one torrent in one bucket.
// Bucket is a day counted since 1970-01-01 UTC, or a year when Year is set,
// or 0 for the days of the whole period.
type TransferHistoryTorrentBytes struct {
	TorrentID  int64
	Year       bool
	Bucket     int64
	Uploaded   int64
	Downloaded int64
}

// torrentTransferChunk is the number of torrent ids in one statement of
// TorrentTransfer.
const torrentTransferChunk = 500

// TorrentTransfer returns the daily bytes of the torrents of an instance in
// UTC days [fromDay, toDay), and the yearly rows of a fold for each year that
// overlaps that period. It gives one row
// for each torrent and day, or one row for the days of each torrent when
// total is set, and always one row for each torrent and year. A non-nil
// torrentIDs limits the rows to those torrents. Each statement reads the days
// and the years of its torrents together, and a fold moves the days of one
// torrent in one transaction, so a fold at the same time moves no byte into
// the answer twice.
func (s *TransferHistoryStore) TorrentTransfer(ctx context.Context, instanceID int, torrentIDs []int64, fromDay, toDay int64, total bool) ([]TransferHistoryTorrentBytes, error) {
	if torrentIDs == nil {
		return s.torrentTransfer(ctx, instanceID, nil, fromDay, toDay, total)
	}
	var out []TransferHistoryTorrentBytes
	for chunk := range slices.Chunk(torrentIDs, torrentTransferChunk) {
		rows, err := s.torrentTransfer(ctx, instanceID, chunk, fromDay, toDay, total)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (s *TransferHistoryStore) torrentTransfer(ctx context.Context, instanceID int, torrentIDs []int64, fromDay, toDay int64, total bool) ([]TransferHistoryTorrentBytes, error) {
	day, groupBy := "0", ""
	if !total {
		day, groupBy = "r.day", ", r.day"
	}
	// The torrent ids come from an IN list, not a join, so that SQLite reads
	// the rows in the order of the primary key and groups them with no sort.
	torrents := "IN (SELECT id FROM transfer_history_torrents WHERE instance_id = ?)"
	ids := []any{instanceID}
	if torrentIDs != nil {
		torrents, ids = "IN ("+placeholders(len(torrentIDs))+")", int64Args(torrentIDs)
	}
	query := `
		SELECT 0, r.torrent_id, ` + day + `, CAST(SUM(r.uploaded) AS BIGINT), CAST(SUM(r.downloaded) AS BIGINT)
		FROM transfer_history_torrent_days r
		WHERE r.torrent_id ` + torrents + ` AND r.day >= ? AND r.day < ?
		GROUP BY r.torrent_id` + groupBy + `
		UNION ALL
		SELECT 1, r.torrent_id, r.year, r.uploaded, r.downloaded
		FROM transfer_history_torrent_years r
		WHERE r.torrent_id ` + torrents + ` AND r.year >= ? AND r.year <= ?`
	args := append(slices.Clone(ids), fromDay, toDay)
	args = append(args, ids...)
	args = append(args, time.Unix(fromDay*86400, 0).UTC().Year(), time.Unix((toDay-1)*86400, 0).UTC().Year())
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read transfer history torrent days: %w", err)
	}
	defer rows.Close()
	var out []TransferHistoryTorrentBytes
	for rows.Next() {
		var b TransferHistoryTorrentBytes
		var year int64
		if err := rows.Scan(&year, &b.TorrentID, &b.Bucket, &b.Uploaded, &b.Downloaded); err != nil {
			return nil, fmt.Errorf("scan transfer history torrent day: %w", err)
		}
		b.Year = year == 1
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history torrent days: %w", err)
	}
	return out, nil
}

// FoldHours deletes the hourly totals and the hourly state of an instance
// before beforeDay, a few whole days in each delete. The daily totals hold the
// same bytes. No daily row holds the state.
func (s *TransferHistoryStore) FoldHours(ctx context.Context, instanceID int, beforeDay int64) error {
	if err := s.deleteInstanceHours(ctx, instanceID, beforeDay); err != nil {
		return err
	}
	for {
		day, ok, err := s.HourlyDetailStart(ctx, instanceID)
		if err != nil {
			return err
		}
		if !ok || day >= beforeDay {
			return nil
		}
		if _, err := s.db.ExecContext(ctx, `
			DELETE FROM transfer_history_tracker_hours WHERE instance_id = ? AND hour < ?`,
			instanceID, min(day+foldHourDays, beforeDay)*24); err != nil {
			return fmt.Errorf("fold transfer history hours: %w", err)
		}
	}
}

// deleteInstanceHours deletes the state of an instance in the hours before
// beforeDay, a few whole days in each transaction.
func (s *TransferHistoryStore) deleteInstanceHours(ctx context.Context, instanceID int, beforeDay int64) error {
	for {
		var first sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `
			SELECT MIN(hour) FROM transfer_history_instance_hours WHERE instance_id = ?`, instanceID).Scan(&first); err != nil {
			return fmt.Errorf("read the first transfer history instance hour: %w", err)
		}
		if !first.Valid || first.Int64 >= beforeDay*24 {
			return nil
		}
		end := min(first.Int64/24+foldHourDays, beforeDay) * 24
		if err := s.deleteInstanceHoursBefore(ctx, instanceID, end); err != nil {
			return err
		}
	}
}

func (s *TransferHistoryStore) deleteInstanceHoursBefore(ctx context.Context, instanceID int, hour int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transfer history instance hour delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"transfer_history_instance_hours", "transfer_history_state_hours"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE instance_id = ? AND hour < ?`, instanceID, hour); err != nil {
			return fmt.Errorf("delete transfer history instance hours: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transfer history instance hour delete: %w", err)
	}
	return nil
}

// FoldTorrentDays moves the daily bytes of each torrent before beforeDay into
// one row for each torrent and UTC year. Each write moves the days of a few
// torrents, so a reader sees each byte once.
func (s *TransferHistoryStore) FoldTorrentDays(ctx context.Context, beforeDay int64) error {
	var after int64
	for {
		ids, err := s.torrentsWithDaysBefore(ctx, after, beforeDay)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := s.foldTorrentDays(ctx, ids, beforeDay); err != nil {
			return err
		}
		after = ids[len(ids)-1]
	}
}

// torrentsWithDaysBefore returns the ids after after, in order, of a few
// torrents with daily bytes before beforeDay.
func (s *TransferHistoryStore) torrentsWithDaysBefore(ctx context.Context, after, beforeDay int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id FROM transfer_history_torrents t
		WHERE t.id > ? AND EXISTS (
			SELECT 1 FROM transfer_history_torrent_days d WHERE d.torrent_id = t.id AND d.day < ?)
		ORDER BY t.id
		LIMIT ?`, after, beforeDay, foldTorrents)
	if err != nil {
		return nil, fmt.Errorf("read transfer history torrents to fold: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan transfer history torrent to fold: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transfer history torrents to fold: %w", err)
	}
	return ids, nil
}

type torrentYear struct {
	torrentID int64
	year      int
}

func (s *TransferHistoryStore) foldTorrentDays(ctx context.Context, ids []int64, beforeDay int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transfer history fold: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	in := "?" + strings.Repeat(", ?", len(ids)-1)
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, beforeDay)

	rows, err := tx.QueryContext(ctx, `
		SELECT torrent_id, day, uploaded, downloaded FROM transfer_history_torrent_days
		WHERE torrent_id IN (`+in+`) AND day < ?`, args...)
	if err != nil {
		return fmt.Errorf("read transfer history days to fold: %w", err)
	}
	years := make(map[torrentYear]*TransferHistoryTotal)
	for rows.Next() {
		var key torrentYear
		var day, uploaded, downloaded int64
		if err := rows.Scan(&key.torrentID, &day, &uploaded, &downloaded); err != nil {
			rows.Close()
			return fmt.Errorf("scan transfer history day to fold: %w", err)
		}
		key.year = time.Unix(day*86400, 0).UTC().Year()
		y := years[key]
		if y == nil {
			y = &TransferHistoryTotal{}
			years[key] = y
		}
		y.Uploaded += uploaded
		y.Downloaded += downloaded
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read transfer history days to fold: %w", err)
	}

	type yearRow struct {
		key   torrentYear
		bytes *TransferHistoryTotal
	}
	yearRows := make([]yearRow, 0, len(years))
	for key, bytes := range years {
		yearRows = append(yearRows, yearRow{key, bytes})
	}
	if err := execTransferHistoryChunks(ctx, tx, yearRows, 4, `
		INSERT INTO transfer_history_torrent_years (torrent_id, year, uploaded, downloaded)
		VALUES %s
		ON CONFLICT (torrent_id, year) DO UPDATE SET
			uploaded = transfer_history_torrent_years.uploaded + excluded.uploaded,
			downloaded = transfer_history_torrent_years.downloaded + excluded.downloaded`,
		func(r yearRow) []any {
			return []any{r.key.torrentID, r.key.year, r.bytes.Uploaded, r.bytes.Downloaded}
		}); err != nil {
		return fmt.Errorf("write transfer history torrent years: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM transfer_history_torrent_days WHERE torrent_id IN (`+in+`) AND day < ?`, args...); err != nil {
		return fmt.Errorf("delete folded transfer history days: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transfer_history_folds (id, torrent_days_before) VALUES (1, ?)
		ON CONFLICT (id) DO UPDATE SET torrent_days_before = CASE
			WHEN excluded.torrent_days_before > transfer_history_folds.torrent_days_before THEN excluded.torrent_days_before
			ELSE transfer_history_folds.torrent_days_before END`, beforeDay); err != nil {
		return fmt.Errorf("write transfer history fold day: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transfer history fold: %w", err)
	}
	return nil
}

const (
	// torrentDeleteBatch is the number of torrents whose rows one write of a
	// delete of history removes, and eventDeleteBatch the number of events.
	torrentDeleteBatch = 50
	eventDeleteBatch   = 1000
	// noEndDay is the day before which a delete of everything deletes. It
	// keeps hours inside the INTEGER columns of Postgres.
	noEndDay = math.MaxInt32 / 24
)

// DeleteInstanceHistory deletes the history of an instance in a series of
// short writes. The instance row goes last: after an error, LoadInstance reads
// what is left.
func (s *TransferHistoryStore) DeleteInstanceHistory(ctx context.Context, instanceID int) error {
	// Each batch starts after the last one, so that it does not read the
	// deleted rows of the batches before it, which Postgres keeps until a vacuum.
	for after := ""; ; {
		ids, last, err := s.torrentsAfter(ctx, instanceID, after)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		// The rows of a torrent go with it.
		if _, err := s.db.ExecContext(ctx, `
			DELETE FROM transfer_history_torrents WHERE id IN (`+placeholders(len(ids))+`)`, int64Args(ids)...); err != nil {
			return fmt.Errorf("delete transfer history torrents: %w", err)
		}
		after = last
	}
	if err := s.deleteTotalsBefore(ctx, instanceID, noEndDay); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM transfer_history_instances WHERE instance_id = ?`, instanceID); err != nil {
		return fmt.Errorf("delete transfer history instance: %w", err)
	}
	return nil
}

// DeleteAllHistory deletes the history of each instance. On Postgres, the
// tables give their space back at once.
func (s *TransferHistoryStore) DeleteAllHistory(ctx context.Context) error {
	if dbinterface.DialectOf(s.db) == "postgres" {
		return s.truncateHistory(ctx)
	}
	instances, err := s.Instances(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if err := s.DeleteInstanceHistory(ctx, inst.InstanceID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteHistoryBefore deletes the history of an instance on the UTC days
// before beforeDay, in a series of short writes. It also deletes the Baseline
// when beforeDay starts after the first recording, and the record of each
// torrent that was gone before beforeDay. The first recording moves to the
// start of beforeDay. A yearly row of a torrent goes when its year ends
// before beforeDay.
func (s *TransferHistoryStore) DeleteHistoryBefore(ctx context.Context, instanceID int, beforeDay int64) error {
	inst, err := s.instanceRow(ctx, instanceID)
	if err != nil || inst == nil {
		return err
	}
	cut := time.Unix(beforeDay*86400, 0).UTC()
	if err := s.deleteEventsBefore(ctx, instanceID, cut.Unix()); err != nil {
		return err
	}
	dropBaseline := cut.After(inst.FirstRecordedAt)
	for after := ""; ; {
		ids, last, err := s.torrentsAfter(ctx, instanceID, after)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		if err := s.deleteTorrentDaysBefore(ctx, ids, beforeDay, cut.Year(), dropBaseline); err != nil {
			return err
		}
		after = last
	}
	if err := s.deleteTotalsBefore(ctx, instanceID, beforeDay); err != nil {
		return err
	}
	if dropBaseline {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE transfer_history_instances SET first_recorded_at = ? WHERE instance_id = ?`, cut, instanceID); err != nil {
			return fmt.Errorf("move the first transfer history recording: %w", err)
		}
	}
	return nil
}

// truncateHistory empties each table of the history on Postgres, the
// settings apart. It finds the tables as Size does.
func (s *TransferHistoryStore) truncateHistory(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.relname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND n.nspname = current_schema()
			AND c.relname LIKE 'transfer_history_%' AND c.relname <> 'transfer_history_settings'`)
	if err != nil {
		return fmt.Errorf("read transfer history tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return fmt.Errorf("scan transfer history table: %w", err)
		}
		tables = append(tables, table)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read transfer history tables: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `TRUNCATE `+strings.Join(tables, ", ")); err != nil {
		return fmt.Errorf("truncate transfer history: %w", err)
	}
	return nil
}

func placeholders(n int) string { return "?" + strings.Repeat(", ?", n-1) }

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// RemovedInstances returns the ids in ids of instances that qui no longer has.
func (s *TransferHistoryStore) RemovedInstances(ctx context.Context, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM instances WHERE id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("read instances: %w", err)
	}
	defer rows.Close()
	present := make(map[int]struct{}, len(ids))
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		present[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read instances: %w", err)
	}
	var removed []int
	for _, id := range ids {
		if _, ok := present[id]; !ok {
			removed = append(removed, id)
		}
	}
	return removed, nil
}

func (s *TransferHistoryStore) instanceRow(ctx context.Context, instanceID int) (*TransferHistoryInstance, error) {
	inst := TransferHistoryInstance{InstanceID: instanceID}
	err := s.db.QueryRowContext(ctx, `
		SELECT first_recorded_at FROM transfer_history_instances WHERE instance_id = ?`, instanceID).Scan(&inst.FirstRecordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read transfer history instance: %w", err)
	}
	return &inst, nil
}

// deleteEventsBefore deletes the events of an instance before the second
// before, a few events of one kind in each write. Each batch starts at the
// last second of the batch before it.
func (s *TransferHistoryStore) deleteEventsBefore(ctx context.Context, instanceID int, before int64) error {
	for _, kind := range []TransferHistoryEventKind{TransferHistoryEventAdd, TransferHistoryEventCompletion, TransferHistoryEventDelete} {
		for from := int64(math.MinInt64); ; {
			ids, last, err := s.eventsFrom(ctx, instanceID, kind, from, before)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				break
			}
			if _, err := s.db.ExecContext(ctx, `
				DELETE FROM transfer_history_events WHERE id IN (`+placeholders(len(ids))+`)`, int64Args(ids)...); err != nil {
				return fmt.Errorf("delete transfer history events: %w", err)
			}
			from = last
		}
	}
	return nil
}

// eventsFrom returns the ids of a few events of one kind of an instance in
// the seconds [from, before), the oldest first, and the last second.
func (s *TransferHistoryStore) eventsFrom(ctx context.Context, instanceID int, kind TransferHistoryEventKind, from, before int64) (ids []int64, last int64, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, second FROM transfer_history_events
		WHERE instance_id = ? AND kind = ? AND second >= ? AND second < ?
		ORDER BY second
		LIMIT ?`, instanceID, kind, from, before, eventDeleteBatch)
	if err != nil {
		return nil, 0, fmt.Errorf("read transfer history events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id, &last); err != nil {
			return nil, 0, fmt.Errorf("scan transfer history event: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read transfer history events: %w", err)
	}
	return ids, last, nil
}

// torrentsAfter returns the ids of a few torrents of an instance whose hash
// sorts after after, and the last hash.
func (s *TransferHistoryStore) torrentsAfter(ctx context.Context, instanceID int, after string) (ids []int64, last string, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, hash FROM transfer_history_torrents
		WHERE instance_id = ? AND hash > ?
		ORDER BY hash
		LIMIT ?`, instanceID, after, torrentDeleteBatch)
	if err != nil {
		return nil, "", fmt.Errorf("read transfer history torrents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id, &last); err != nil {
			return nil, "", fmt.Errorf("scan transfer history torrent: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("read transfer history torrents: %w", err)
	}
	return ids, last, nil
}

// deleteTorrentDaysBefore deletes, in one write, the days of the torrents
// before beforeDay and their years before beforeYear. It deletes their
// Baseline when dropBaseline is set, and the record of each torrent that is
// gone and has no bytes and no events left.
func (s *TransferHistoryStore) deleteTorrentDaysBefore(ctx context.Context, ids []int64, beforeDay int64, beforeYear int, dropBaseline bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transfer history delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	in, idArgs := placeholders(len(ids)), int64Args(ids)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM transfer_history_torrent_days WHERE torrent_id IN (`+in+`) AND day < ?`, append(idArgs, beforeDay)...); err != nil {
		return fmt.Errorf("delete transfer history torrent days: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM transfer_history_torrent_years WHERE torrent_id IN (`+in+`) AND year < ?`, append(idArgs, beforeYear)...); err != nil {
		return fmt.Errorf("delete transfer history torrent years: %w", err)
	}
	if dropBaseline {
		if _, err := tx.ExecContext(ctx, `
			UPDATE transfer_history_torrents SET baseline_uploaded = 0, baseline_downloaded = 0
			WHERE id IN (`+in+`)`, idArgs...); err != nil {
			return fmt.Errorf("delete transfer history baselines: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM transfer_history_torrents
		WHERE id IN (`+in+`) AND deleted = TRUE
			AND baseline_uploaded = 0 AND baseline_downloaded = 0
			AND NOT EXISTS (SELECT 1 FROM transfer_history_torrent_days d WHERE d.torrent_id = transfer_history_torrents.id)
			AND NOT EXISTS (SELECT 1 FROM transfer_history_torrent_years y WHERE y.torrent_id = transfer_history_torrents.id)
			AND NOT EXISTS (SELECT 1 FROM transfer_history_events e WHERE e.torrent_id = transfer_history_torrents.id)`,
		idArgs...); err != nil {
		return fmt.Errorf("delete gone transfer history torrents: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transfer history delete: %w", err)
	}
	return nil
}

// deleteTotalsBefore deletes the hourly totals, the daily totals, and the
// Recording gaps of an instance before beforeDay, a few days in each write.
func (s *TransferHistoryStore) deleteTotalsBefore(ctx context.Context, instanceID int, beforeDay int64) error {
	for _, t := range []struct {
		table, column string
		perDay        int64
	}{
		{"transfer_history_tracker_hours", "hour", 24},
		{"transfer_history_category_days", "day", 1},
		{"transfer_history_gaps", "hour", 24},
		{"transfer_history_library_days", "day", 1},
		{"transfer_history_instance_hours", "hour", 24},
		{"transfer_history_state_hours", "hour", 24},
	} {
		before := beforeDay * t.perDay
		for {
			var first sql.NullInt64
			if err := s.db.QueryRowContext(ctx, `
				SELECT MIN(`+t.column+`) FROM `+t.table+` WHERE instance_id = ?`, instanceID).Scan(&first); err != nil {
				return fmt.Errorf("read %s: %w", t.table, err)
			}
			if !first.Valid || first.Int64 >= before {
				break
			}
			if _, err := s.db.ExecContext(ctx, `
				DELETE FROM `+t.table+` WHERE instance_id = ? AND `+t.column+` < ?`,
				instanceID, min(first.Int64+foldHourDays*t.perDay, before)); err != nil {
				return fmt.Errorf("delete %s: %w", t.table, err)
			}
		}
	}
	return nil
}
