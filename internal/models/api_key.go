// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrAPIKeyNotFound = errors.New("api key not found")
var ErrInvalidAPIKey = errors.New("invalid api key")

type APIKey struct {
	ID          int          `json:"id"`
	KeyHash     string       `json:"-"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"createdAt"`
	LastUsedAt  *time.Time   `json:"lastUsedAt,omitempty"`
	Permissions []Permission `json:"permissions"`
	InstanceIDs []int        `json:"instanceIds"`
}

type APIKeyStore struct {
	db dbinterface.Querier
}

func NewAPIKeyStore(db dbinterface.Querier) *APIKeyStore {
	return &APIKeyStore{db: db}
}

func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func HashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func (s *APIKeyStore) Create(ctx context.Context, name string, permissions []Permission, instanceIDs []int) (string, *APIKey, error) {
	rawKey, err := GenerateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	keyHash := HashAPIKey(rawKey)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ids, err := dbinterface.InternStringNullable(ctx, tx, &name)
	if err != nil {
		return "", nil, fmt.Errorf("failed to intern name: %w", err)
	}

	if instanceIDs == nil {
		instanceIDs = []int{}
	}
	permissionsJSON, instanceIDsJSON, err := encodeAPIKeyAccess(permissions, instanceIDs)
	if err != nil {
		return "", nil, err
	}

	apiKey := &APIKey{Permissions: permissions, InstanceIDs: instanceIDs}
	var createdAt, lastUsedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (key_hash, name_id, permissions, instance_ids) 
		VALUES (?, ?, ?, ?)
		RETURNING id, key_hash, created_at, last_used_at
	`, keyHash, ids[0], permissionsJSON, instanceIDsJSON).Scan(
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

	return rawKey, apiKey, nil
}

func (s *APIKeyStore) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	query := `
		SELECT id, key_hash, name, created_at, last_used_at, permissions, instance_ids 
		FROM api_keys_view 
		WHERE key_hash = ?
	`

	var id int
	var keyHashResult, name, permissionsJSON, instanceIDsJSON string
	var createdAt sql.NullTime
	var lastUsedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, keyHash).Scan(
		&id,
		&keyHashResult,
		&name,
		&createdAt,
		&lastUsedAt,
		&permissionsJSON,
		&instanceIDsJSON,
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
	if err := apiKey.decodeAccess(permissionsJSON, instanceIDsJSON); err != nil {
		return nil, err
	}

	return apiKey, nil
}

func (s *APIKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	query := `
		SELECT id, key_hash, name, created_at, last_used_at, permissions, instance_ids 
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
		var keyHash, name, permissionsJSON, instanceIDsJSON string
		var createdAt sql.NullTime
		var lastUsedAt sql.NullTime

		err := rows.Scan(
			&id,
			&keyHash,
			&name,
			&createdAt,
			&lastUsedAt,
			&permissionsJSON,
			&instanceIDsJSON,
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
		if err := apiKey.decodeAccess(permissionsJSON, instanceIDsJSON); err != nil {
			return nil, err
		}

		keys = append(keys, apiKey)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

func (s *APIKeyStore) Update(ctx context.Context, id int, name string, permissions []Permission, instanceIDs []int) (*APIKey, error) {
	if instanceIDs == nil {
		instanceIDs = []int{}
	}
	permissionsJSON, instanceIDsJSON, err := encodeAPIKeyAccess(permissions, instanceIDs)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ids, err := dbinterface.InternStringNullable(ctx, tx, &name)
	if err != nil {
		return nil, fmt.Errorf("failed to intern name: %w", err)
	}

	apiKey := &APIKey{ID: id, Name: name, Permissions: permissions, InstanceIDs: instanceIDs}
	var createdAt, lastUsedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		UPDATE api_keys SET name_id = ?, permissions = ?, instance_ids = ?
		WHERE id = ?
		RETURNING key_hash, created_at, last_used_at
	`, ids[0], permissionsJSON, instanceIDsJSON, id).Scan(&apiKey.KeyHash, &createdAt, &lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	apiKey.CreatedAt = createdAt.Time
	if lastUsedAt.Valid {
		apiKey.LastUsedAt = &lastUsedAt.Time
	}
	return apiKey, nil
}

func encodeAPIKeyAccess(permissions []Permission, instanceIDs []int) (string, string, error) {
	permissionsJSON, err := json.Marshal(permissions)
	if err != nil {
		return "", "", fmt.Errorf("failed to encode permissions: %w", err)
	}
	instanceIDsJSON, err := json.Marshal(instanceIDs)
	if err != nil {
		return "", "", fmt.Errorf("failed to encode instance limit: %w", err)
	}
	return string(permissionsJSON), string(instanceIDsJSON), nil
}

var decodedPermissions sync.Map

func (k *APIKey) decodeAccess(permissionsJSON, instanceIDsJSON string) error {
	if cached, ok := decodedPermissions.Load(permissionsJSON); ok {
		k.Permissions = cached.([]Permission)
	} else {
		if err := json.Unmarshal([]byte(permissionsJSON), &k.Permissions); err != nil {
			return fmt.Errorf("failed to decode permissions of API key %d: %w", k.ID, err)
		}
		decodedPermissions.Store(permissionsJSON, k.Permissions)
	}
	if instanceIDsJSON == "[]" {
		k.InstanceIDs = []int{}
		return nil
	}
	if err := json.Unmarshal([]byte(instanceIDsJSON), &k.InstanceIDs); err != nil {
		return fmt.Errorf("failed to decode instance limit of API key %d: %w", k.ID, err)
	}
	return nil
}

func (s *APIKeyStore) UpdateLastUsed(ctx context.Context, id int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `
		UPDATE api_keys 
		SET last_used_at = ? 
		WHERE id = ?
	`

	result, err := tx.ExecContext(ctx, query, time.Now().UTC(), id)
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
	defer func() { _ = tx.Rollback() }()

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

func (s *APIKeyStore) ValidateAPIKey(ctx context.Context, rawKey string) (*APIKey, error) {
	keyHash := HashAPIKey(rawKey)

	apiKey, err := s.GetByHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}

	age := time.Duration(-1)
	if apiKey.LastUsedAt != nil {
		age = time.Since(*apiKey.LastUsedAt)
	}
	if age < 0 || age >= time.Minute {
		go func() {
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := s.UpdateLastUsed(writeCtx, apiKey.ID); err != nil {
				log.Warn().Err(err).Int("keyId", apiKey.ID).Msg("failed to update API key last used timestamp")
			}
		}()
	}

	return apiKey, nil
}
