// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

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
		ORDER BY id ASC
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

	le.Patterns = parseLogExclusionPatterns(patternsJSON)

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
	switch dbinterface.DialectOf(s.db) {
	case "postgres":
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO log_exclusions (id, patterns)
			VALUES (1, '[]')
			ON CONFLICT (id) DO NOTHING
		`)
		if err != nil {
			return nil, err
		}
	default:
		_, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO log_exclusions (id, patterns)
			VALUES (1, '[]')
		`)
		if err != nil {
			return nil, err
		}
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, patterns, created_at, updated_at
		FROM log_exclusions
		WHERE id = 1
	`)

	var le LogExclusions
	var patternsJSON string
	if err := row.Scan(&le.ID, &patternsJSON, &le.CreatedAt, &le.UpdatedAt); err != nil {
		return nil, err
	}
	le.Patterns = parseLogExclusionPatterns(patternsJSON)
	return &le, nil
}

func parseLogExclusionPatterns(patternsJSON string) []string {
	if patternsJSON == "" || patternsJSON == "[]" {
		return []string{}
	}

	var patterns []string
	if err := json.Unmarshal([]byte(patternsJSON), &patterns); err != nil {
		sum := sha256.Sum256([]byte(patternsJSON))
		patternsHash := hex.EncodeToString(sum[:])
		if len(patternsHash) > 12 {
			patternsHash = patternsHash[:12]
		}

		log.Warn().
			Err(err).
			Int("patterns_json_len", len(patternsJSON)).
			Str("patterns_json_sha256", patternsHash).
			Msg("Failed to parse log exclusion patterns")
		return []string{}
	}
	if patterns == nil {
		return []string{}
	}
	return patterns
}
