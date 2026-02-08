// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"

	"github.com/autobrr/qui/internal/dbinterface"
	"modernc.org/sqlite"
	lib "modernc.org/sqlite/lib"
)

var ErrUserDefinedViewExists = errors.New("user-defined view already exists")
var ErrUserDefinedViewNotCreated = errors.New("error creating user-defined view")
var ErrUserDefinedViewNotFound = errors.New("user-defined view not found")
var ErrUserDefinedViewNotUpdated = errors.New("error updating user-defined view")

// UserDefinedView is the data returned for a user-defined view.
type UserDefinedView struct {
	ID                int      `json:"id"`
	InstanceID        int      `json:"instanceId"`
	Name              string   `json:"name"`
	Status            []string `json:"status"`
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	Trackers          []string `json:"trackers"`
	ExcludeStatus     []string `json:"excludeStatus"`
	ExcludeCategories []string `json:"excludeCategories"`
	ExcludeTags       []string `json:"excludeTags"`
	ExcludeTrackers   []string `json:"excludeTrackers"`
}

// UserDefinedViewCreate is the data needed to create a new user-defined view
type UserDefinedViewCreate struct {
	InstanceID        int      `json:"instanceId"`
	Name              string   `json:"name"`
	Status            []string `json:"status"`
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	Trackers          []string `json:"trackers"`
	ExcludeStatus     []string `json:"excludeStatus"`
	ExcludeCategories []string `json:"excludeCategories"`
	ExcludeTags       []string `json:"excludeTags"`
	ExcludeTrackers   []string `json:"excludeTrackers"`
}

// UserDefinedViewUpdate is the data needed to update an existing user-defined view
type UserDefinedViewUpdate struct {
	Status            []string `json:"status"`
	Categories        []string `json:"categories"`
	Tags              []string `json:"tags"`
	Trackers          []string `json:"trackers"`
	ExcludeStatus     []string `json:"excludeStatus"`
	ExcludeCategories []string `json:"excludeCategories"`
	ExcludeTags       []string `json:"excludeTags"`
	ExcludeTrackers   []string `json:"excludeTrackers"`
}

type UserDefinedViewStore struct {
	db dbinterface.Querier
}

func NewUserDefinedViewStore(db dbinterface.Querier) *UserDefinedViewStore {
	if db == nil {
		panic("db cannot be nil")
	}
	return &UserDefinedViewStore{db: db}
}

func (s *UserDefinedViewStore) Create(ctx context.Context, create UserDefinedViewCreate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO user_defined_views(
			instance_id,
			name,
			status,
			categories,
			tags,
			trackers,
			exclude_status,
			exclude_categories,
			exclude_tags,
			exclude_trackers
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var status, categories, tags, trackers, excludeStatus, excludeCategories, excludeTags, excludeTrackers string
	if status, err = encodeStringSlice(create.Status); err != nil {
		return err
	}
	if categories, err = encodeStringSlice(create.Categories); err != nil {
		return err
	}
	if tags, err = encodeStringSlice(create.Tags); err != nil {
		return err
	}
	if trackers, err = encodeStringSlice(create.Trackers); err != nil {
		return err
	}
	if excludeStatus, err = encodeStringSlice(create.ExcludeStatus); err != nil {
		return err
	}
	if excludeCategories, err = encodeStringSlice(create.ExcludeCategories); err != nil {
		return err
	}
	if excludeTags, err = encodeStringSlice(create.ExcludeTags); err != nil {
		return err
	}
	if excludeTrackers, err = encodeStringSlice(create.ExcludeTrackers); err != nil {
		return err
	}

	res, err := tx.ExecContext(
		ctx,
		query,
		create.InstanceID,
		create.Name,
		status,
		categories,
		tags,
		trackers,
		excludeStatus,
		excludeCategories,
		excludeTags,
		excludeTrackers,
	)
	if err != nil {
		var sqlErr *sqlite.Error
		if errors.As(err, &sqlErr) {
			// UNIQUE constraint on id or CHECK constraint on instance_id, name
			if sqlErr.Code() == lib.SQLITE_CONSTRAINT_UNIQUE {
				return ErrUserDefinedViewExists
			}
		}
		return err
	}

	if affected, err := res.RowsAffected(); affected != 1 || err != nil {
		return ErrUserDefinedViewNotCreated
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (s *UserDefinedViewStore) List(ctx context.Context, instanceID int) ([]*UserDefinedView, error) {
	query := `
		SELECT id, instance_id, name, status, categories, tags, trackers, exclude_status, exclude_categories, exclude_tags, exclude_trackers
		FROM user_defined_views
		WHERE instance_id = ?
	`

	rows, err := s.db.QueryContext(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := []*UserDefinedView{}
	for rows.Next() {
		var status, categories, tags, trackers, excludeStatus, excludeCategories, excludeTags, excludeTrackers sql.NullString
		view := UserDefinedView{}
		err = rows.Scan(
			&view.ID,
			&view.InstanceID,
			&view.Name,
			&status,
			&categories,
			&tags,
			&trackers,
			&excludeStatus,
			&excludeCategories,
			&excludeTags,
			&excludeTrackers,
		)
		if err != nil {
			return nil, err
		}

		if err = decodeStringSlice(status, &view.Status); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(categories, &view.Categories); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(tags, &view.Tags); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(trackers, &view.Trackers); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(excludeStatus, &view.ExcludeStatus); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(excludeCategories, &view.ExcludeCategories); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(excludeTags, &view.ExcludeTags); err != nil {
			return nil, err
		}
		if err = decodeStringSlice(excludeTrackers, &view.ExcludeTrackers); err != nil {
			return nil, err
		}

		views = append(views, &view)
	}
	if rows.Err() != nil {
		return nil, err
	}

	return views, nil
}

func (s *UserDefinedViewStore) Update(ctx context.Context, id int, update UserDefinedViewUpdate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		UPDATE user_defined_views
		SET 
			status = ?,
			categories = ?,
			tags = ?,
			trackers = ?,
			exclude_status = ?,
			exclude_categories = ?,
			exclude_tags = ?,
			exclude_trackers = ?
		WHERE id = ?
	`

	var status, categories, tags, trackers, excludeStatus, excludeCategories, excludeTags, excludeTrackers string
	if status, err = encodeStringSlice(update.Status); err != nil {
		return err
	}
	if categories, err = encodeStringSlice(update.Categories); err != nil {
		return err
	}
	if tags, err = encodeStringSlice(update.Tags); err != nil {
		return err
	}
	if trackers, err = encodeStringSlice(update.Trackers); err != nil {
		return err
	}
	if excludeStatus, err = encodeStringSlice(update.ExcludeStatus); err != nil {
		return err
	}
	if excludeCategories, err = encodeStringSlice(update.ExcludeCategories); err != nil {
		return err
	}
	if excludeTags, err = encodeStringSlice(update.ExcludeTags); err != nil {
		return err
	}
	if excludeTrackers, err = encodeStringSlice(update.ExcludeTrackers); err != nil {
		return err
	}

	res, err := tx.ExecContext(
		ctx,
		query,
		status,
		categories,
		tags,
		trackers,
		excludeStatus,
		excludeCategories,
		excludeTags,
		excludeTrackers,
		id,
	)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); affected < 1 || err != nil {
		return ErrUserDefinedViewNotUpdated
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (s *UserDefinedViewStore) Delete(ctx context.Context, instanceID, id int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `DELETE FROM user_defined_views WHERE instance_id = ? AND id = ?`

	res, err := tx.ExecContext(ctx, query, instanceID, id)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); affected < 1 || err != nil {
		return ErrUserDefinedViewNotFound
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}
