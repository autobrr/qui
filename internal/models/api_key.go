// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrAPIKeyNotFound = errors.New("api key not found")
var ErrInvalidAPIKey = errors.New("invalid api key")

type APIKey struct {
	ID         int        `json:"id"`
	KeyHash    string     `json:"-"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type APIKeyStore struct {
	db dbinterface.Querier
}

func NewAPIKeyStore(db dbinterface.Querier) *APIKeyStore {
	return &APIKeyStore{db: db}
}

// GenerateAPIKey generates a new API key
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// HashAPIKey creates a SHA256 hash of the API key
func HashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func (s *APIKeyStore) Create(ctx context.Context, name string) (string, *APIKey, error) {
	// Generate new API key
	rawKey, err := GenerateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	// Hash the key for storage
	keyHash := HashAPIKey(rawKey)

	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the name
	ids, err := dbinterface.InternStringNullable(ctx, tx, &name)
	if err != nil {
		return "", nil, fmt.Errorf("failed to intern name: %w", err)
	}

	// Insert the API key
	apiKey := &APIKey{}
	var createdAt, lastUsedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (key_hash, name_id) 
		VALUES (?, ?)
		RETURNING id, key_hash, created_at, last_used_at
	`, keyHash, ids[0]).Scan(
		&apiKey.ID,
		&apiKey.KeyHash,
		&createdAt,
		&lastUsedAt,
	)

	if err != nil {
		return "", nil, err
	}

	if err = tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	apiKey.Name = name
	apiKey.CreatedAt = createdAt.Time
	if lastUsedAt.Valid {
		apiKey.LastUsedAt = &lastUsedAt.Time
	}

	// Return both the raw key (to show user once) and the model
	return rawKey, apiKey, nil
}

func (s *APIKeyStore) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	query := `
		SELECT id, key_hash, name, created_at, last_used_at 
		FROM api_keys_view 
		WHERE key_hash = ?
	`

	var id int
	var keyHashResult, name string
	var createdAt sql.NullTime
	var lastUsedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, keyHash).Scan(
		&id,
		&keyHashResult,
		&name,
		&createdAt,
		&lastUsedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}

		return nil, err
	}

	apiKey := &APIKey{
		ID:        id,
		KeyHash:   keyHashResult,
		Name:      name,
		CreatedAt: createdAt.Time,
	}

	if lastUsedAt.Valid {
		apiKey.LastUsedAt = &lastUsedAt.Time
	}

	return apiKey, nil
}

func (s *APIKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	query := `
		SELECT id, key_hash, name, created_at, last_used_at 
		FROM api_keys_view 
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]*APIKey, 0)
	for rows.Next() {
		var id int
		var keyHash, name string
		var createdAt sql.NullTime
		var lastUsedAt sql.NullTime

		err := rows.Scan(
			&id,
			&keyHash,
			&name,
			&createdAt,
			&lastUsedAt,
		)
		if err != nil {
			return nil, err
		}

		apiKey := &APIKey{
			ID:        id,
			KeyHash:   keyHash,
			Name:      name,
			CreatedAt: createdAt.Time,
		}

		if lastUsedAt.Valid {
			apiKey.LastUsedAt = &lastUsedAt.Time
		}

		keys = append(keys, apiKey)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

func (s *APIKeyStore) UpdateLastUsed(ctx context.Context, id int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `
		UPDATE api_keys 
		SET last_used_at = CURRENT_TIMESTAMP 
		WHERE id = ?
	`

	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrAPIKeyNotFound
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *APIKeyStore) Delete(ctx context.Context, id int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `DELETE FROM api_keys WHERE id = ?`

	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrAPIKeyNotFound
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ValidateAPIKey validates a raw API key and returns the associated APIKey if valid
func (s *APIKeyStore) ValidateAPIKey(ctx context.Context, rawKey string) (*APIKey, error) {
	keyHash := HashAPIKey(rawKey)

	apiKey, err := s.GetByHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}

	// Update last used timestamp asynchronously
	go func() {
		_ = s.UpdateLastUsed(ctx, apiKey.ID)
	}()

	return apiKey, nil
}
