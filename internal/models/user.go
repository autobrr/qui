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

var ErrUserNotFound = errors.New("user not found")
var ErrUserAlreadyExists = errors.New("user already exists")

type User struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
}

type UserStore struct {
	db dbinterface.Querier
}

func NewUserStore(db dbinterface.Querier) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(ctx context.Context, username, passwordHash string) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO user (id, username, password_hash) 
		VALUES (1, ?, ?)
		RETURNING id, username, password_hash
	`

	user := &User{}
	err = tx.QueryRowContext(ctx, query, username, passwordHash).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
	)

	if err != nil {
		var sqlErr *sqlite.Error
		if errors.As(err, &sqlErr) {
			// UNIQUE constraint on username or CHECK constraint on id = 1
			if sqlErr.Code() == lib.SQLITE_CONSTRAINT_UNIQUE || sqlErr.Code() == lib.SQLITE_CONSTRAINT_CHECK {
				return nil, ErrUserAlreadyExists
			}
		}
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserStore) Get(ctx context.Context) (*User, error) {
	query := `
		SELECT id, username, password_hash 
		FROM user 
		WHERE id = 1
	`

	user := &User{}
	err := s.db.QueryRowContext(ctx, query).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	query := `
		SELECT id, username, password_hash 
		FROM user 
		WHERE username = ?
	`

	user := &User{}
	err := s.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserStore) UpdatePassword(ctx context.Context, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		UPDATE user 
		SET password_hash = ? 
		WHERE id = 1
	`

	result, err := tx.ExecContext(ctx, query, passwordHash)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrUserNotFound
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (s *UserStore) Exists(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user").Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
