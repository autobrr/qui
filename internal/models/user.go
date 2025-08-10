// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package models

import (
	"database/sql"
	"errors"
	"time"
)

var ErrUserNotFound = errors.New("user not found")
var ErrUserAlreadyExists = errors.New("user already exists")

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(username, passwordHash string) (*User, error) {
	query := `
		INSERT INTO user (id, username, password_hash) 
		VALUES (1, ?, ?)
		RETURNING id, username, password_hash, created_at, updated_at
	`

	user := &User{}
	err := s.db.QueryRow(query, username, passwordHash).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "UNIQUE constraint failed: user.username" {
			return nil, ErrUserAlreadyExists
		}
		if err.Error() == "CHECK constraint failed: id = 1" {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}

	return user, nil
}

func (s *UserStore) Get() (*User, error) {
	query := `
		SELECT id, username, password_hash, created_at, updated_at 
		FROM user 
		WHERE id = 1
	`

	user := &User{}
	err := s.db.QueryRow(query).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserStore) GetByUsername(username string) (*User, error) {
	query := `
		SELECT id, username, password_hash, created_at, updated_at 
		FROM user 
		WHERE username = ?
	`

	user := &User{}
	err := s.db.QueryRow(query, username).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserStore) UpdatePassword(passwordHash string) error {
	query := `
		UPDATE user 
		SET password_hash = ? 
		WHERE id = 1
	`

	result, err := s.db.Exec(query, passwordHash)
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

	return nil
}

func (s *UserStore) Exists() (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM user").Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
