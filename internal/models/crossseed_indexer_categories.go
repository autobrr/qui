// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/autobrr/qui/internal/dbinterface"
)

// ErrIndexerCategoryForeignKey is returned by CrossSeedIndexerCategoryStore.Set
// when the supplied instanceID or indexerID does not exist in the database.
var ErrIndexerCategoryForeignKey = errors.New("instance or indexer not found")

// CrossSeedIndexerCategory is a per-instance, per-indexer category mapping.
// When tracker category mode is active, the category here overrides the global
// default for cross-seeds matched via this indexer on this qBittorrent instance.
type CrossSeedIndexerCategory struct {
	InstanceID  int    `json:"instanceId"`
	IndexerID   int    `json:"indexerId"`
	IndexerName string `json:"indexerName"`
	Category    string `json:"category"`
}

// CrossSeedIndexerCategoryStore manages persistence for CrossSeedIndexerCategory mappings.
type CrossSeedIndexerCategoryStore struct {
	db dbinterface.Querier
}

// NewCrossSeedIndexerCategoryStore creates a new store. Panics if db is nil.
func NewCrossSeedIndexerCategoryStore(db dbinterface.Querier) *CrossSeedIndexerCategoryStore {
	if db == nil {
		panic("db cannot be nil")
	}
	return &CrossSeedIndexerCategoryStore{db: db}
}

// List returns all category mappings for a given instance, joined with indexer names.
func (s *CrossSeedIndexerCategoryStore) List(ctx context.Context, instanceID int) ([]CrossSeedIndexerCategory, error) {
	const query = `
		SELECT c.instance_id, c.indexer_id, sp.value AS indexer_name, c.category
		FROM cross_seed_indexer_categories c
		JOIN torznab_indexers ti ON ti.id = c.indexer_id
		JOIN string_pool sp ON ti.name_id = sp.id
		WHERE c.instance_id = ?
		ORDER BY sp.value`

	rows, err := s.db.QueryContext(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CrossSeedIndexerCategory
	for rows.Next() {
		var m CrossSeedIndexerCategory
		if err := rows.Scan(&m.InstanceID, &m.IndexerID, &m.IndexerName, &m.Category); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []CrossSeedIndexerCategory{}
	}
	return result, nil
}

// GetByIndexerName returns the category configured for a given (instanceID, indexerName) pair.
// Returns ("", nil) when no mapping exists (caller should use the global default).
func (s *CrossSeedIndexerCategoryStore) GetByIndexerName(ctx context.Context, instanceID int, indexerName string) (string, error) {
	trimmedIndexerName := strings.TrimSpace(indexerName)
	if trimmedIndexerName == "" {
		return "", nil
	}
	const query = `
		SELECT c.category
		FROM cross_seed_indexer_categories c
		JOIN torznab_indexers ti ON ti.id = c.indexer_id
		JOIN string_pool sp ON ti.name_id = sp.id
		WHERE c.instance_id = ? AND LOWER(sp.value) = LOWER(?)
		LIMIT 1`

	var category string
	err := s.db.QueryRowContext(ctx, query, instanceID, trimmedIndexerName).Scan(&category)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetByIndexerName: %w", err)
	}
	return category, nil
}

// Set upserts a category mapping for the given (instanceID, indexerID) pair.
// Returns an error if category is blank after trimming.
func (s *CrossSeedIndexerCategoryStore) Set(ctx context.Context, instanceID, indexerID int, category string) error {
	trimmedCategory := strings.TrimSpace(category)
	if trimmedCategory == "" {
		return errors.New("category cannot be blank")
	}

	const stmt = `
		INSERT INTO cross_seed_indexer_categories (instance_id, indexer_id, category)
		VALUES (?, ?, ?)
		ON CONFLICT(instance_id, indexer_id) DO UPDATE SET
			category = excluded.category,
			updated_at = CURRENT_TIMESTAMP`

	_, err := s.db.ExecContext(ctx, stmt, instanceID, indexerID, trimmedCategory)
	if isForeignKeyConstraintError(err) {
		return ErrIndexerCategoryForeignKey
	}
	return err
}

// Delete removes the category mapping for the given (instanceID, indexerID) pair.
// Returns nil if no such mapping exists.
func (s *CrossSeedIndexerCategoryStore) Delete(ctx context.Context, instanceID, indexerID int) error {
	const stmt = `DELETE FROM cross_seed_indexer_categories WHERE instance_id = ? AND indexer_id = ?`
	_, err := s.db.ExecContext(ctx, stmt, instanceID, indexerID)
	return err
}
