// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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
	db *sql.DB
}

func NewClientAPIKeyStore(db *sql.DB) *ClientAPIKeyStore {
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

	query := `
		INSERT INTO client_api_keys (key_hash, client_name, instance_id) 
		VALUES (?, ?, ?)
		RETURNING id, key_hash, client_name, instance_id, created_at, last_used_at
	`

	clientAPIKey := &ClientAPIKey{}
	err = s.db.QueryRowContext(ctx, query, keyHash, clientName, instanceID).Scan(
		&clientAPIKey.ID,
		&clientAPIKey.KeyHash,
		&clientAPIKey.ClientName,
		&clientAPIKey.InstanceID,
		&clientAPIKey.CreatedAt,
		&clientAPIKey.LastUsedAt,
	)

	if err != nil {
		return "", nil, err
	}

	// Return both the raw key (to show user once) and the model
	return rawKey, clientAPIKey, nil
}

func (s *ClientAPIKeyStore) GetAll(ctx context.Context) ([]*ClientAPIKey, error) {
	query := `
		SELECT id, key_hash, client_name, instance_id, created_at, last_used_at 
		FROM client_api_keys 
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

	return keys, rows.Err()
}

func (s *ClientAPIKeyStore) GetByKeyHash(ctx context.Context, keyHash string) (*ClientAPIKey, error) {
	query := `
		SELECT id, key_hash, client_name, instance_id, created_at, last_used_at 
		FROM client_api_keys 
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
	query := `UPDATE client_api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE key_hash = ?`
	_, err := s.db.ExecContext(ctx, query, keyHash)
	return err
}

func (s *ClientAPIKeyStore) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM client_api_keys WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, id)
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

	return nil
}

func (s *ClientAPIKeyStore) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	query := `DELETE FROM client_api_keys WHERE instance_id = ?`
	_, err := s.db.ExecContext(ctx, query, instanceID)
	return err
}
