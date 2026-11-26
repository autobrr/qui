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

const (
	maxSeasonPackRunHistory = 200
	maxSeasonPackRunList    = 2 * maxSeasonPackRunHistory
)

type SeasonPackRun struct {
	ID              int64     `json:"id"`
	TorrentName     string    `json:"torrentName"`
	Phase           string    `json:"phase"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason"`
	Message         string    `json:"message"`
	InstanceID      *int      `json:"instanceId,omitempty"`
	AddedInstanceID *int      `json:"addedInstanceId,omitempty"`
	MatchedEpisodes int       `json:"matchedEpisodes"`
	TotalEpisodes   int       `json:"totalEpisodes"`
	Coverage        float64   `json:"coverage"`
	LinkMode        string    `json:"linkMode"`
	CreatedAt       time.Time `json:"createdAt"`
}

type SeasonPackRunStore struct {
	db dbinterface.Querier
}

func NewSeasonPackRunStore(db dbinterface.Querier) *SeasonPackRunStore {
	return &SeasonPackRunStore{db: db}
}

func (s *SeasonPackRunStore) Create(ctx context.Context, run *SeasonPackRun) (*SeasonPackRun, error) {
	if run == nil {
		return nil, errors.New("run cannot be nil")
	}

	instanceID, addedInstanceID := nullInt(run.InstanceID), nullInt(run.AddedInstanceID)

	var id int64
	if dbinterface.DialectOf(s.db) != "postgres" {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO season_pack_runs
				(torrent_name, phase, status, reason, message, instance_id, added_instance_id, matched_episodes, total_episodes, coverage, link_mode)
			VALUES
				(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, run.TorrentName, run.Phase, run.Status, run.Reason, run.Message,
			instanceID, addedInstanceID, run.MatchedEpisodes, run.TotalEpisodes, run.Coverage, run.LinkMode)
		if err != nil {
			return nil, fmt.Errorf("insert season pack run: %w", err)
		}

		id, err = res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("last insert id: %w", err)
		}
	} else {
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO season_pack_runs
				(torrent_name, phase, status, reason, message, instance_id, added_instance_id, matched_episodes, total_episodes, coverage, link_mode)
			VALUES
				(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id
		`, run.TorrentName, run.Phase, run.Status, run.Reason, run.Message,
			instanceID, addedInstanceID, run.MatchedEpisodes, run.TotalEpisodes, run.Coverage, run.LinkMode).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert season pack run: %w", err)
		}
	}

	if err := s.prune(ctx); err != nil {
		return nil, err
	}

	return s.get(ctx, id)
}

func (s *SeasonPackRunStore) List(ctx context.Context, limit int) ([]*SeasonPackRun, error) {
	if limit <= 0 || limit > maxSeasonPackRunList {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, torrent_name, phase, status, reason, message,
		       instance_id, added_instance_id, matched_episodes, total_episodes, coverage, link_mode,
		       created_at
		FROM season_pack_runs
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list season pack runs: %w", err)
	}
	defer rows.Close()

	runs := make([]*SeasonPackRun, 0)
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
		       instance_id, added_instance_id, matched_episodes, total_episodes, coverage, link_mode,
		       created_at
		FROM season_pack_runs
		WHERE id = ?
	`, id)

	r, err := scanSeasonPackRun(row)
	if err != nil {
		return nil, fmt.Errorf("get season pack run: %w", err)
	}
	return r, nil
}

func (s *SeasonPackRunStore) prune(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM season_pack_runs
		WHERE id NOT IN (
			SELECT id FROM season_pack_runs
			WHERE status = 'applied'
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		) AND id NOT IN (
			SELECT id FROM season_pack_runs
			WHERE status <> 'applied'
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		)
	`, maxSeasonPackRunHistory, maxSeasonPackRunHistory)
	if err != nil {
		return fmt.Errorf("prune old season pack runs: %w", err)
	}

	return nil
}

func scanSeasonPackRun(s sqlScanner) (*SeasonPackRun, error) {
	var r SeasonPackRun
	var instanceID, addedInstanceID sql.NullInt64

	if err := s.Scan(
		&r.ID, &r.TorrentName, &r.Phase, &r.Status, &r.Reason, &r.Message,
		&instanceID, &addedInstanceID, &r.MatchedEpisodes, &r.TotalEpisodes, &r.Coverage, &r.LinkMode,
		&r.CreatedAt,
	); err != nil {
		return nil, err
	}
	r.InstanceID, r.AddedInstanceID = intPtr(instanceID), intPtr(addedInstanceID)
	return &r, nil
}

func nullInt(id *int) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*id), Valid: true}
}

func intPtr(id sql.NullInt64) *int {
	if !id.Valid {
		return nil
	}
	return new(int(id.Int64))
}
