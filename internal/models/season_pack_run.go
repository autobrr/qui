// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// SeasonPackRun records one season-pack webhook processing attempt.
type SeasonPackRun struct {
	ID              int64     `json:"id"`
	TorrentName     string    `json:"torrentName"`
	Phase           string    `json:"phase"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason"`
	Message         string    `json:"message"`
	InstanceID      *int      `json:"instanceId,omitempty"`
	MatchedEpisodes int       `json:"matchedEpisodes"`
	TotalEpisodes   int       `json:"totalEpisodes"`
	Coverage        float64   `json:"coverage"`
	LinkMode        string    `json:"linkMode"`
	CreatedAt       time.Time `json:"createdAt"`
}

// SeasonPackRunStore persists season-pack run activity.
type SeasonPackRunStore struct {
	db dbinterface.Querier
}

// NewSeasonPackRunStore constructs a new season-pack run store.
func NewSeasonPackRunStore(db dbinterface.Querier) *SeasonPackRunStore {
	return &SeasonPackRunStore{db: db}
}

// Create inserts a new season-pack run and returns it with the generated ID and timestamp.
func (s *SeasonPackRunStore) Create(ctx context.Context, run *SeasonPackRun) (*SeasonPackRun, error) {
	if run == nil {
		return nil, errors.New("run cannot be nil")
	}

	var instanceID sql.NullInt64
	if run.InstanceID != nil {
		instanceID = sql.NullInt64{Int64: int64(*run.InstanceID), Valid: true}
	}

	if dbinterface.DialectOf(s.db) != "postgres" {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO season_pack_runs
				(torrent_name, phase, status, reason, message, instance_id, matched_episodes, total_episodes, coverage, link_mode)
			VALUES
				(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, run.TorrentName, run.Phase, run.Status, run.Reason, run.Message,
			instanceID, run.MatchedEpisodes, run.TotalEpisodes, run.Coverage, run.LinkMode)
		if err != nil {
			return nil, fmt.Errorf("insert season pack run: %w", err)
		}

		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("last insert id: %w", err)
		}

		return s.get(ctx, id)
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO season_pack_runs
			(torrent_name, phase, status, reason, message, instance_id, matched_episodes, total_episodes, coverage, link_mode)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, run.TorrentName, run.Phase, run.Status, run.Reason, run.Message,
		instanceID, run.MatchedEpisodes, run.TotalEpisodes, run.Coverage, run.LinkMode).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert season pack run: %w", err)
	}

	return s.get(ctx, id)
}

// List returns the most recent season-pack runs, ordered newest first.
func (s *SeasonPackRunStore) List(ctx context.Context, limit int) ([]*SeasonPackRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, torrent_name, phase, status, reason, message,
		       instance_id, matched_episodes, total_episodes, coverage, link_mode,
		       created_at
		FROM season_pack_runs
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list season pack runs: %w", err)
	}
	defer rows.Close()

	var runs []*SeasonPackRun
	for rows.Next() {
		r, err := scanSeasonPackRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan season pack run: %w", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate season pack runs: %w", err)
	}

	return runs, nil
}

func (s *SeasonPackRunStore) get(ctx context.Context, id int64) (*SeasonPackRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, torrent_name, phase, status, reason, message,
		       instance_id, matched_episodes, total_episodes, coverage, link_mode,
		       created_at
		FROM season_pack_runs
		WHERE id = ?
	`, id)

	var r SeasonPackRun
	var instanceID sql.NullInt64

	if err := row.Scan(
		&r.ID, &r.TorrentName, &r.Phase, &r.Status, &r.Reason, &r.Message,
		&instanceID, &r.MatchedEpisodes, &r.TotalEpisodes, &r.Coverage, &r.LinkMode,
		&r.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("get season pack run: %w", err)
	}

	if instanceID.Valid {
		instID := int(instanceID.Int64)
		r.InstanceID = &instID
	}

	return &r, nil
}

func scanSeasonPackRun(s sqlScanner) (*SeasonPackRun, error) {
	var r SeasonPackRun
	var instanceID sql.NullInt64

	if err := s.Scan(
		&r.ID, &r.TorrentName, &r.Phase, &r.Status, &r.Reason, &r.Message,
		&instanceID, &r.MatchedEpisodes, &r.TotalEpisodes, &r.Coverage, &r.LinkMode,
		&r.CreatedAt,
	); err != nil {
		return nil, err
	}

	if instanceID.Valid {
		id := int(instanceID.Int64)
		r.InstanceID = &id
	}

	return &r, nil
}
