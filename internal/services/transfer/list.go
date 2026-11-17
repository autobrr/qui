// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Filter is a filter chip of the Transfers page. FilterAll lists every row.
type Filter string

const (
	FilterAll      Filter = ""
	FilterActive   Filter = "active"
	FilterWaiting  Filter = "waiting"
	FilterFinished Filter = "finished"
	FilterFailed   Filter = "failed"
)

// Filters maps the name of a chip to its Filter.
var Filters = map[string]Filter{"": FilterAll, "all": FilterAll, "active": FilterActive, "waiting": FilterWaiting,
	"finished": FilterFinished, "failed": FilterFailed}

var filterStates = map[Filter][]State{
	FilterActive:   {StateCopying, StateChecking, StateFinishing},
	FilterWaiting:  {StateWaiting},
	FilterFinished: {StateDone, StateCancelled},
	FilterFailed:   {StateFailed},
}

// Row is one Transfer or Export run as the list of the Transfers page shows
// it: the torrent of the request names the row.
type Row struct {
	ID               int64
	Kind             Kind
	Way              Way
	State            State
	StartedBy        StartedBy
	RuleID           *int
	SourceInstanceID int
	TargetInstanceID int
	Hash             string
	Name             string
	// Torrents counts the torrents of the Transfer, the torrent of the
	// request with it.
	Torrents      int
	Size          int64
	Options       Options
	RetryAt       time.Time
	FailureClass  FailureClass
	FailureReason string
	CreatedAt     time.Time
	FinishedAt    time.Time
	// Hints is the advice in the log of the Transfer.
	Hints []Hint
}

// Counts is the number of rows behind each filter chip.
type Counts struct {
	Active   int
	Waiting  int
	Finished int
	Failed   int
}

type List struct {
	Rows   []Row
	Counts Counts
}

// List returns one page of the rows behind filter, the newest first: at most
// limit rows after the first offset. It also returns the counts of every chip.
func (s *Store) List(ctx context.Context, filter Filter, limit, offset int) (*List, error) {
	query := `SELECT t.id, t.kind, t.way, t.state, t.started_by, t.rule_id, t.source_instance_id, t.target_instance_id,
			m.hash, m.name, (SELECT COUNT(*) FROM torrent_transfer_members c WHERE c.transfer_id = t.id),
			t.size, t.options_json, t.log_json, t.retry_at, t.failure_class, t.failure_reason, t.created_at, t.finished_at
		FROM torrent_transfers t
		JOIN torrent_transfer_members m ON m.transfer_id = t.id AND m.position = 0`
	clause, args := whereState(filter, "t.state")
	query += clause + ` ORDER BY t.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list transfers: %w", err)
	}
	defer rows.Close()
	list := &List{Rows: []Row{}}
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		list.Rows = append(list.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transfers: %w", err)
	}
	if list.Counts, err = s.counts(ctx); err != nil {
		return nil, err
	}
	return list, nil
}

// whereState is the WHERE clause that keeps the rows behind filter, by the
// state column, with its arguments. FilterAll keeps every row.
func whereState(filter Filter, column string) (string, []any) {
	states := filterStates[filter]
	if len(states) == 0 {
		return "", nil
	}
	args := make([]any, 0, len(states))
	for _, state := range states {
		args = append(args, string(state))
	}
	return ` WHERE ` + column + ` IN (?` + strings.Repeat(", ?", len(states)-1) + `)`, args
}

// IDs returns the id of each row behind filter, the newest first.
func (s *Store) IDs(ctx context.Context, filter Filter) ([]int64, error) {
	clause, args := whereState(filter, "state")
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM torrent_transfers`+clause+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list transfer ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list transfer ids: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanRow(rows *sql.Rows) (Row, error) {
	var (
		r                           Row
		kind, way, state, startedBy string
		ruleID                      sql.NullInt64
		options, logLines           string
		failureClass                string
		retryAt, finishedAt         sql.NullTime
	)
	if err := rows.Scan(&r.ID, &kind, &way, &state, &startedBy, &ruleID, &r.SourceInstanceID, &r.TargetInstanceID,
		&r.Hash, &r.Name, &r.Torrents, &r.Size, &options, &logLines, &retryAt, &failureClass, &r.FailureReason, &r.CreatedAt, &finishedAt); err != nil {
		return Row{}, fmt.Errorf("list transfers: %w", err)
	}
	r.Kind, r.Way, r.State, r.StartedBy, r.FailureClass = Kind(kind), Way(way), State(state), StartedBy(startedBy), FailureClass(failureClass)
	if ruleID.Valid {
		r.RuleID = new(int(ruleID.Int64))
	}
	r.RetryAt, r.FinishedAt = retryAt.Time, finishedAt.Time
	var lines []LogLine
	if err := json.Unmarshal([]byte(options), &r.Options); err != nil {
		return Row{}, fmt.Errorf("decode transfer %d: %w", r.ID, err)
	}
	if err := json.Unmarshal([]byte(logLines), &lines); err != nil {
		return Row{}, fmt.Errorf("decode transfer %d: %w", r.ID, err)
	}
	for _, line := range lines {
		if line.Hint != nil {
			r.Hints = append(r.Hints, *line.Hint)
		}
	}
	return r, nil
}

func (s *Store) counts(ctx context.Context) (Counts, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM torrent_transfers GROUP BY state`)
	if err != nil {
		return Counts{}, fmt.Errorf("count transfers: %w", err)
	}
	defer rows.Close()
	var c Counts
	chips := map[State]*int{}
	for filter, states := range filterStates {
		count := map[Filter]*int{FilterActive: &c.Active, FilterWaiting: &c.Waiting, FilterFinished: &c.Finished, FilterFailed: &c.Failed}[filter]
		for _, state := range states {
			chips[state] = count
		}
	}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return Counts{}, fmt.Errorf("count transfers: %w", err)
		}
		if count := chips[State(state)]; count != nil {
			*count += n
		}
	}
	return c, rows.Err()
}

// HelperGone reports whether qui can see that the instance no longer has the
// helper torrent hash, from a sync after since.
type HelperGone func(instanceID int, hash string, since time.Time) bool

// Remove deletes a finished Transfer, and the registry rows of its helper
// torrents that gone reports. It returns ErrNotFinished for a Transfer that
// holds its torrents.
func (s *Store) Remove(ctx context.Context, id int64, gone HelperGone) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if record.FinishedAt.IsZero() {
		return fmt.Errorf("%w: %d", ErrNotFinished, id)
	}
	return s.remove(ctx, []finished{{id, record.FinishedAt}}, gone)
}

// RemoveFinishedBefore deletes each Transfer that finished before t, and the
// registry rows of their helper torrents that gone reports. It returns how
// many Transfers it deleted. It keeps a permanent failure that is the last
// Transfer of one of its torrents: WaitsForUser reads that row to hold a rule
// off, and without it the rule would start the Transfer again.
func (s *Store) RemoveFinishedBefore(ctx context.Context, t time.Time, gone HelperGone) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.finished_at FROM torrent_transfers t
			WHERE t.finished_at IS NOT NULL
			AND NOT (t.kind = ? AND t.state = ? AND t.failure_class = ? AND EXISTS (
				SELECT 1 FROM torrent_transfer_members m WHERE m.transfer_id = t.id AND NOT EXISTS (
					SELECT 1 FROM torrent_transfers later
					JOIN torrent_transfer_members lm ON lm.transfer_id = later.id
					WHERE later.kind = t.kind AND later.source_instance_id = t.source_instance_id
					AND lm.hash = m.hash AND later.id > t.id)))`,
		string(KindTransfer), string(StateFailed), string(FailurePermanent))
	if err != nil {
		return 0, fmt.Errorf("read finished transfers: %w", err)
	}
	defer rows.Close()
	var old []finished
	for rows.Next() {
		var f finished
		if err := rows.Scan(&f.id, &f.at); err != nil {
			return 0, fmt.Errorf("read finished transfers: %w", err)
		}
		// Compared here, not in SQL: SQLite compares stored times as text,
		// and Finish stores them in the local zone.
		if f.at.Before(t) {
			old = append(old, f)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read finished transfers: %w", err)
	}
	rows.Close()
	if len(old) == 0 {
		return 0, nil
	}
	return len(old), s.remove(ctx, old, gone)
}

// finished is a Transfer that ended at at.
type finished struct {
	id int64
	at time.Time
}

// remove deletes the rows of the Transfers. It keeps the registry row of a
// helper torrent unless gone reports it: a cleanup that could not reach a
// client only logged that the helper stays there, and the registry is what
// protects it. ForgetGoneHelpers checks a kept helper again later.
func (s *Store) remove(ctx context.Context, transfers []finished, gone HelperGone) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove of transfers: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var dropped []torrentKey
	kept := make(map[torrentKey]time.Time)
	for _, f := range transfers {
		gones, stays, err := dropGoneHelpers(ctx, tx, f, gone)
		if err != nil {
			return err
		}
		dropped = append(dropped, gones...)
		for _, key := range stays {
			kept[key] = f.at
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_transfer_members WHERE transfer_id = ?`, f.id); err != nil {
			return fmt.Errorf("remove members of transfer %d: %w", f.id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_transfers WHERE id = ? AND finished_at IS NOT NULL`, f.id); err != nil {
			return fmt.Errorf("remove transfer %d: %w", f.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove of transfers: %w", err)
	}
	s.mu.Lock()
	for _, key := range dropped {
		delete(s.helpers, key)
	}
	maps.Copy(s.orphans, kept)
	s.mu.Unlock()
	return nil
}

// dropGoneHelpers deletes the registry rows of the helper torrents of f that
// gone reports. It returns their keys, and the keys of the rows it keeps.
func dropGoneHelpers(ctx context.Context, tx dbinterface.TxQuerier, f finished, gone HelperGone) (dropped, kept []torrentKey, err error) {
	if gone == nil {
		return nil, nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT instance_id, hash FROM helper_torrents WHERE owner_kind = ? AND owner_id = ?`, string(OwnerTransfer), f.id)
	if err != nil {
		return nil, nil, fmt.Errorf("read helper torrents of transfer %d: %w", f.id, err)
	}
	for rows.Next() {
		var key torrentKey
		if err := rows.Scan(&key.instanceID, &key.hash); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("read helper torrents of transfer %d: %w", f.id, err)
		}
		if gone(key.instanceID, key.hash, f.at) {
			dropped = append(dropped, key)
		} else {
			kept = append(kept, key)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("read helper torrents of transfer %d: %w", f.id, err)
	}
	for _, key := range dropped {
		if _, err := tx.ExecContext(ctx, `DELETE FROM helper_torrents WHERE instance_id = ? AND hash = ?`, key.instanceID, key.hash); err != nil {
			return nil, nil, fmt.Errorf("remove helper torrent of transfer %d: %w", f.id, err)
		}
	}
	return dropped, kept, nil
}

// orphan is the registry row of a helper torrent whose Transfer row is gone.
type orphan struct {
	key     torrentKey
	ownerID int64
}

// ForgetGoneHelpers deletes the registry row of each helper torrent whose
// Transfer row is gone, when gone reports the helper torrent gone. It checks
// a helper that a removal kept from the end of its Transfer, and one that it
// finds first from now. It returns how many rows it deleted.
func (s *Store) ForgetGoneHelpers(ctx context.Context, gone HelperGone) (int, error) {
	if gone == nil {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT instance_id, hash, owner_id FROM helper_torrents h
		WHERE owner_kind = ? AND NOT EXISTS (SELECT 1 FROM torrent_transfers t WHERE t.id = h.owner_id)`, string(OwnerTransfer))
	if err != nil {
		return 0, fmt.Errorf("read helper torrents without a transfer: %w", err)
	}
	defer rows.Close()
	var found []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.key.instanceID, &o.key.hash, &o.ownerID); err != nil {
			return 0, fmt.Errorf("read helper torrents without a transfer: %w", err)
		}
		found = append(found, o)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read helper torrents without a transfer: %w", err)
	}
	rows.Close()

	now := time.Now()
	since := make(map[torrentKey]time.Time, len(found))
	s.mu.RLock()
	for _, o := range found {
		at, seen := s.orphans[o.key]
		if !seen {
			at = now
		}
		since[o.key] = at
	}
	s.mu.RUnlock()

	// gone runs outside s.mu: it reads the state of the sync manager.
	var dropped []orphan
	for _, o := range found {
		if gone(o.key.instanceID, o.key.hash, since[o.key]) {
			dropped = append(dropped, o)
			delete(since, o.key)
		}
	}
	if len(dropped) > 0 {
		if err := s.deleteOrphans(ctx, dropped); err != nil {
			return 0, err
		}
	}

	s.mu.Lock()
	for _, o := range dropped {
		delete(s.helpers, o.key)
	}
	s.orphans = since
	s.mu.Unlock()
	return len(dropped), nil
}

// deleteOrphans deletes the registry rows of orphans in one write.
func (s *Store) deleteOrphans(ctx context.Context, orphans []orphan) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove of helper torrents: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, o := range orphans {
		if _, err := tx.ExecContext(ctx, `DELETE FROM helper_torrents WHERE instance_id = ? AND hash = ? AND owner_kind = ? AND owner_id = ?`,
			o.key.instanceID, o.key.hash, string(OwnerTransfer), o.ownerID); err != nil {
			return fmt.Errorf("remove helper torrent of transfer %d: %w", o.ownerID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove of helper torrents: %w", err)
	}
	return nil
}

// keepFinished is how long the row of a finished Transfer stays.
const keepFinished = 30 * 24 * time.Hour

// purgeInterval is how often qui looks for rows older than keepFinished.
const purgeInterval = time.Hour

// purge removes the rows that finished more than keepFinished ago, and
// forgets the helper torrents of removed rows that qui now sees gone.
func (s *Service) purge() {
	s.purged = s.deps.Now()
	gone := s.helperGone()
	n, err := s.deps.Store.RemoveFinishedBefore(s.ctx, s.purged.Add(-keepFinished), gone)
	switch {
	case err != nil && s.ctx.Err() == nil:
		log.Error().Err(err).Msg("transfer: cannot remove old Transfers")
	case n > 0:
		log.Debug().Int("transfers", n).Msg("transfer: removed Transfers that ended more than 30 days ago")
	}
	n, err = s.deps.Store.ForgetGoneHelpers(s.ctx, gone)
	switch {
	case err != nil && s.ctx.Err() == nil:
		log.Error().Err(err).Msg("transfer: cannot forget helper torrents that are gone")
	case n > 0:
		log.Debug().Int("helpers", n).Msg("transfer: forgot helper torrents of removed Transfers that are gone")
	}
}

// Remove deletes the row of a finished Transfer or Export run. It returns
// ErrNotFound for an unknown id, and ErrNotFinished for a Transfer that has
// not ended.
func (s *Service) Remove(ctx context.Context, id int64) error {
	return s.deps.Store.Remove(ctx, id, s.helperGone())
}

// helperGone forgets a helper torrent only when the synced torrent list of a
// connected client shows that it is gone. It asks qBittorrent nothing.
func (s *Service) helperGone() HelperGone {
	if s.deps.Sync == nil {
		return nil
	}
	return s.deps.Sync.TorrentGoneSince
}
