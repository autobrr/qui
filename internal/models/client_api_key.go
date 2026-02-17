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

var ErrClientAPIKeyNotFound = errors.New("client api key not found")

type ClientAPIKey struct {
	ID         int        `json:"id"`
	KeyHash    string     `json:"-"`
	ClientName string     `json:"clientName"`
	InstanceID int        `json:"instanceId"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type ClientAPIKeyStore struct {
	db dbinterface.Querier
}

func NewClientAPIKeyStore(db dbinterface.Querier) *ClientAPIKeyStore {
	return &ClientAPIKeyStore{db: db}
}

func (s *ClientAPIKeyStore) Create(ctx context.Context, clientName string, instanceID int) (string, *ClientAPIKey, error) {
	// Generate new API key
	rawKey, err := GenerateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	// Hash the key for storage
	keyHash := HashAPIKey(rawKey)

	// Use a transaction to atomically intern the string and insert the API key
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the client name
	ids, err := dbinterface.InternStringNullable(ctx, tx, &clientName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to intern client_name: %w", err)
	}

	// Insert the client API key
	clientAPIKey := &ClientAPIKey{}
	var createdAt, lastUsedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		INSERT INTO client_api_keys (key_hash, client_name_id, instance_id) 
		VALUES (?, ?, ?)
		RETURNING id, key_hash, instance_id, created_at, last_used_at
	`, keyHash, ids[0], instanceID).Scan(
		&clientAPIKey.ID,
		&clientAPIKey.KeyHash,
		&clientAPIKey.InstanceID,
		&createdAt,
		&lastUsedAt,
	)

	if err != nil {
		return "", nil, err
	}

	if err = tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	clientAPIKey.ClientName = clientName
	clientAPIKey.CreatedAt = createdAt.Time
	if lastUsedAt.Valid {
		clientAPIKey.LastUsedAt = &lastUsedAt.Time
	}

	// Return both the raw key (to show user once) and the model
	return rawKey, clientAPIKey, nil
}

func (s *ClientAPIKeyStore) GetAll(ctx context.Context) ([]*ClientAPIKey, error) {
	query := `
		SELECT id, key_hash, client_name, instance_id, created_at, last_used_at 
		FROM client_api_keys_view 
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*ClientAPIKey
	for rows.Next() {
		key := &ClientAPIKey{}
		err := rows.Scan(
			&key.ID,
			&key.KeyHash,
			&key.ClientName,
			&key.InstanceID,
			&key.CreatedAt,
			&key.LastUsedAt,
		)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

func (s *ClientAPIKeyStore) GetByKeyHash(ctx context.Context, keyHash string) (*ClientAPIKey, error) {
	query := `
		SELECT id, key_hash, client_name, instance_id, created_at, last_used_at 
		FROM client_api_keys_view 
		WHERE key_hash = ?
	`

	key := &ClientAPIKey{}
	err := s.db.QueryRowContext(ctx, query, keyHash).Scan(
		&key.ID,
		&key.KeyHash,
		&key.ClientName,
		&key.InstanceID,
		&key.CreatedAt,
		&key.LastUsedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrClientAPIKeyNotFound
	}

	if err != nil {
		return nil, err
	}

	return key, nil
}

func (s *ClientAPIKeyStore) ValidateKey(ctx context.Context, rawKey string) (*ClientAPIKey, error) {
	keyHash := HashAPIKey(rawKey)
	return s.GetByKeyHash(ctx, keyHash)
}

func (s *ClientAPIKeyStore) UpdateLastUsed(ctx context.Context, keyHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `UPDATE client_api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE key_hash = ?`
	result, err := tx.ExecContext(ctx, query, keyHash)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrClientAPIKeyNotFound
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *ClientAPIKeyStore) Delete(ctx context.Context, id int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `DELETE FROM client_api_keys WHERE id = ?`
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrClientAPIKeyNotFound
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *ClientAPIKeyStore) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `DELETE FROM client_api_keys WHERE instance_id = ?`
	_, err = tx.ExecContext(ctx, query, instanceID)
	if err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
