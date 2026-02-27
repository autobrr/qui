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

type LogExclusions struct {
	ID        int       `json:"id"`
	Patterns  []string  `json:"patterns"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type LogExclusionsInput struct {
	Patterns []string `json:"patterns"`
}

type LogExclusionsStore struct {
	db dbinterface.Querier
}

func NewLogExclusionsStore(db dbinterface.Querier) *LogExclusionsStore {
	return &LogExclusionsStore{db: db}
}

// Get returns log exclusions, creating defaults if none exist
func (s *LogExclusionsStore) Get(ctx context.Context) (*LogExclusions, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, patterns, created_at, updated_at
		FROM log_exclusions
		LIMIT 1
	`)

	var le LogExclusions
	var patternsJSON string

	err := row.Scan(&le.ID, &patternsJSON, &le.CreatedAt, &le.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return s.createDefault(ctx)
	}
	if err != nil {
		return nil, err
	}

	// Parse JSON field
	if patternsJSON != "" && patternsJSON != "[]" {
		if err := json.Unmarshal([]byte(patternsJSON), &le.Patterns); err != nil {
			le.Patterns = []string{}
		}
	} else {
		le.Patterns = []string{}
	}

	return &le, nil
}

// Update replaces patterns
func (s *LogExclusionsStore) Update(ctx context.Context, input *LogExclusionsInput) (*LogExclusions, error) {
	if input == nil {
		return nil, errors.New("input is nil")
	}

	// Ensure we have a record (creates if none)
	existing, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Handle nil patterns as empty array
	patterns := input.Patterns
	if patterns == nil {
		patterns = []string{}
	}

	// Serialize JSON
	patternsJSON, err := json.Marshal(patterns)
	if err != nil {
		return nil, err
	}

	// Update in database
	_, err = s.db.ExecContext(ctx, `
		UPDATE log_exclusions
		SET patterns = ?
		WHERE id = ?
	`, string(patternsJSON), existing.ID)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx)
}

// createDefault creates empty log exclusions
func (s *LogExclusionsStore) createDefault(ctx context.Context) (*LogExclusions, error) {
	var id int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO log_exclusions (patterns)
		VALUES ('[]')
		RETURNING id
	`).Scan(&id)
	if err != nil {
		return nil, err
	}

	return &LogExclusions{
		ID:        id,
		Patterns:  []string{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}
