// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
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

// List returns the newest rows behind filter, at most limit, and the counts
// of every chip.
func (s *Store) List(ctx context.Context, filter Filter, limit int) (*List, error) {
	query := `SELECT t.id, t.kind, t.way, t.state, t.started_by, t.rule_id, t.source_instance_id, t.target_instance_id,
			m.hash, m.name, (SELECT COUNT(*) FROM torrent_transfer_members c WHERE c.transfer_id = t.id),
			t.options_json, t.log_json, t.retry_at, t.failure_class, t.failure_reason, t.created_at, t.finished_at
		FROM torrent_transfers t
		JOIN torrent_transfer_members m ON m.transfer_id = t.id AND m.position = 0`
	args := []any{}
	if states := filterStates[filter]; len(states) > 0 {
		query += ` WHERE t.state IN (?` + strings.Repeat(", ?", len(states)-1) + `)`
		for _, state := range states {
			args = append(args, string(state))
		}
	}
	query += ` ORDER BY t.id DESC LIMIT ?`
	args = append(args, limit)

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
		&r.Hash, &r.Name, &r.Torrents, &options, &logLines, &retryAt, &failureClass, &r.FailureReason, &r.CreatedAt, &finishedAt); err != nil {
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
// many Transfers it deleted.
func (s *Store) RemoveFinishedBefore(ctx context.Context, t time.Time, gone HelperGone) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, finished_at FROM torrent_transfers WHERE finished_at IS NOT NULL`)
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
// protects it.
func (s *Store) remove(ctx context.Context, transfers []finished, gone HelperGone) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove of transfers: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var dropped []torrentKey
	for _, f := range transfers {
		keys, err := dropGoneHelpers(ctx, tx, f, gone)
		if err != nil {
			return err
		}
		dropped = append(dropped, keys...)
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
	s.mu.Unlock()
	return nil
}

// dropGoneHelpers deletes the registry rows of the helper torrents of f that
// gone reports, and returns their keys.
func dropGoneHelpers(ctx context.Context, tx dbinterface.TxQuerier, f finished, gone HelperGone) ([]torrentKey, error) {
	if gone == nil {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT instance_id, hash FROM helper_torrents WHERE owner_kind = ? AND owner_id = ?`, string(OwnerTransfer), f.id)
	if err != nil {
		return nil, fmt.Errorf("read helper torrents of transfer %d: %w", f.id, err)
	}
	var keys []torrentKey
	for rows.Next() {
		var key torrentKey
		if err := rows.Scan(&key.instanceID, &key.hash); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read helper torrents of transfer %d: %w", f.id, err)
		}
		if gone(key.instanceID, key.hash, f.at) {
			keys = append(keys, key)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("read helper torrents of transfer %d: %w", f.id, err)
	}
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `DELETE FROM helper_torrents WHERE instance_id = ? AND hash = ?`, key.instanceID, key.hash); err != nil {
			return nil, fmt.Errorf("remove helper torrent of transfer %d: %w", f.id, err)
		}
	}
	return keys, nil
}

// keepFinished is how long the row of a finished Transfer stays.
const keepFinished = 30 * 24 * time.Hour

// purgeInterval is how often qui looks for rows older than keepFinished.
const purgeInterval = time.Hour

// purge removes the rows that finished more than keepFinished ago.
func (s *Service) purge() {
	s.purged = s.deps.Now()
	n, err := s.deps.Store.RemoveFinishedBefore(s.ctx, s.purged.Add(-keepFinished), s.helperGone())
	switch {
	case err != nil && s.ctx.Err() == nil:
		log.Error().Err(err).Msg("transfer: cannot remove old Transfers")
	case n > 0:
		log.Debug().Int("transfers", n).Msg("transfer: removed Transfers that ended more than 30 days ago")
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
