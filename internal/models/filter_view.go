// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// ErrDuplicateFilterViewName is returned when a user already has a view with the same name.
var ErrDuplicateFilterViewName = errors.New("duplicate filter view name")

// FilterView is a named snapshot of the frontend's TorrentFilters object.
// Filters is opaque to the backend: it is stored and returned verbatim.
type FilterView struct {
	ID        int             `json:"id"`
	Name      string          `json:"name"`
	Filters   json.RawMessage `json:"filters"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type FilterViewStore struct {
	db dbinterface.Querier
}

func NewFilterViewStore(db dbinterface.Querier) *FilterViewStore {
	return &FilterViewStore{db: db}
}

func (s *FilterViewStore) List(ctx context.Context, userID int) ([]*FilterView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, filters, created_at, updated_at
		FROM filter_views
		WHERE user_id = ?
		ORDER BY name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := make([]*FilterView, 0)
	for rows.Next() {
		var v FilterView
		var filters string
		if err := rows.Scan(&v.ID, &v.Name, &filters, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Filters = json.RawMessage(filters)
		views = append(views, &v)
	}

	return views, rows.Err()
}

func (s *FilterViewStore) get(ctx context.Context, userID, id int) (*FilterView, error) {
	var v FilterView
	var filters string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, filters, created_at, updated_at
		FROM filter_views
		WHERE user_id = ? AND id = ?
	`, userID, id).Scan(&v.ID, &v.Name, &filters, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}

	v.Filters = json.RawMessage(filters)
	return &v, nil
}

func (s *FilterViewStore) Create(ctx context.Context, userID int, name string, filters json.RawMessage) (*FilterView, error) {
	var v FilterView
	var stored string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO filter_views (user_id, name, filters)
		VALUES (?, ?, ?)
		RETURNING id, name, filters, created_at, updated_at
	`, userID, name, string(filters)).Scan(&v.ID, &v.Name, &stored, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrDuplicateFilterViewName
		}
		return nil, err
	}

	v.Filters = json.RawMessage(stored)
	return &v, nil
}

func (s *FilterViewStore) Update(ctx context.Context, userID, id int, name string, filters json.RawMessage) (*FilterView, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE filter_views
		SET name = ?, filters = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND id = ?
	`, name, string(filters), userID, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrDuplicateFilterViewName
		}
		return nil, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}

	return s.get(ctx, userID, id)
}

func (s *FilterViewStore) Delete(ctx context.Context, userID, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM filter_views WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}
