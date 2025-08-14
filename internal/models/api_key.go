// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
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
	db *sql.DB
}

func NewAPIKeyStore(db *sql.DB) *APIKeyStore {
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

func (s *APIKeyStore) Create(name string) (string, *APIKey, error) {
	// Generate new API key
	rawKey, err := GenerateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	// Hash the key for storage
	keyHash := HashAPIKey(rawKey)

	query := `
		INSERT INTO api_keys (key_hash, name) 
		VALUES (?, ?)
		RETURNING id, key_hash, name, created_at, last_used_at
	`

	apiKey := &APIKey{}
	err = s.db.QueryRow(query, keyHash, name).Scan(
		&apiKey.ID,
		&apiKey.KeyHash,
		&apiKey.Name,
		&apiKey.CreatedAt,
		&apiKey.LastUsedAt,
	)

	if err != nil {
		return "", nil, err
	}

	// Return both the raw key (to show user once) and the model
	return rawKey, apiKey, nil
}

func (s *APIKeyStore) GetByHash(keyHash string) (*APIKey, error) {
	query := `
		SELECT id, key_hash, name, created_at, last_used_at 
		FROM api_keys 
		WHERE key_hash = ?
	`

	apiKey := &APIKey{}
	err := s.db.QueryRow(query, keyHash).Scan(
		&apiKey.ID,
		&apiKey.KeyHash,
		&apiKey.Name,
		&apiKey.CreatedAt,
		&apiKey.LastUsedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}

	return apiKey, nil
}

func (s *APIKeyStore) List() ([]*APIKey, error) {
	query := `
		SELECT id, key_hash, name, created_at, last_used_at 
		FROM api_keys 
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]*APIKey, 0)
	for rows.Next() {
		apiKey := &APIKey{}
		err := rows.Scan(
			&apiKey.ID,
			&apiKey.KeyHash,
			&apiKey.Name,
			&apiKey.CreatedAt,
			&apiKey.LastUsedAt,
		)
		if err != nil {
			return nil, err
		}
		keys = append(keys, apiKey)
	}

	return keys, rows.Err()
}

func (s *APIKeyStore) UpdateLastUsed(id int) error {
	query := `
		UPDATE api_keys 
		SET last_used_at = CURRENT_TIMESTAMP 
		WHERE id = ?
	`

	result, err := s.db.Exec(query, id)
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

	return nil
}

func (s *APIKeyStore) Delete(id int) error {
	query := `DELETE FROM api_keys WHERE id = ?`

	result, err := s.db.Exec(query, id)
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

	return nil
}

// ValidateAPIKey validates a raw API key and returns the associated APIKey if valid
func (s *APIKeyStore) ValidateAPIKey(rawKey string) (*APIKey, error) {
	keyHash := HashAPIKey(rawKey)

	apiKey, err := s.GetByHash(keyHash)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}

	// Update last used timestamp asynchronously
	go func() {
		_ = s.UpdateLastUsed(apiKey.ID)
	}()

	return apiKey, nil
}
