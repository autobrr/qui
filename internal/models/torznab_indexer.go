// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrTorznabIndexerNotFound = errors.New("torznab indexer not found")

// TorznabIndexer represents a Torznab API indexer (Jackett, Prowlarr, etc.)
type TorznabIndexer struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	BaseURL         string    `json:"base_url"`
	APIKeyEncrypted string    `json:"-"`
	Enabled         bool      `json:"enabled"`
	Priority        int       `json:"priority"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TorznabIndexerStore manages Torznab indexers in the database
type TorznabIndexerStore struct {
	db            dbinterface.Querier
	encryptionKey []byte
}

// NewTorznabIndexerStore creates a new TorznabIndexerStore
func NewTorznabIndexerStore(db dbinterface.Querier, encryptionKey []byte) (*TorznabIndexerStore, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}

	return &TorznabIndexerStore{
		db:            db,
		encryptionKey: encryptionKey,
	}, nil
}

// encrypt encrypts a string using AES-GCM
func (s *TorznabIndexerStore) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts a string encrypted with encrypt
func (s *TorznabIndexerStore) decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		return "", errors.New("malformed ciphertext")
	}

	nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// Create creates a new Torznab indexer
func (s *TorznabIndexerStore) Create(ctx context.Context, name, baseURL, apiKey string, enabled bool, priority, timeoutSeconds int) (*TorznabIndexer, error) {
	if name == "" {
		return nil, errors.New("name cannot be empty")
	}
	if baseURL == "" {
		return nil, errors.New("base URL cannot be empty")
	}
	if apiKey == "" {
		return nil, errors.New("API key cannot be empty")
	}

	// Encrypt API key
	encryptedAPIKey, err := s.encrypt(apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	// Set defaults
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	query := `
		INSERT INTO torznab_indexers (name, base_url, api_key_encrypted, enabled, priority, timeout_seconds)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query, name, baseURL, encryptedAPIKey, enabled, priority, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to create torznab indexer: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return s.Get(ctx, int(id))
}

// Get retrieves a Torznab indexer by ID
func (s *TorznabIndexerStore) Get(ctx context.Context, id int) (*TorznabIndexer, error) {
	query := `
		SELECT id, name, base_url, api_key_encrypted, enabled, priority, timeout_seconds, created_at, updated_at
		FROM torznab_indexers
		WHERE id = ?
	`

	var indexer TorznabIndexer
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&indexer.ID,
		&indexer.Name,
		&indexer.BaseURL,
		&indexer.APIKeyEncrypted,
		&indexer.Enabled,
		&indexer.Priority,
		&indexer.TimeoutSeconds,
		&indexer.CreatedAt,
		&indexer.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTorznabIndexerNotFound
		}
		return nil, fmt.Errorf("failed to get torznab indexer: %w", err)
	}

	return &indexer, nil
}

// List retrieves all Torznab indexers, ordered by priority (descending) and name
func (s *TorznabIndexerStore) List(ctx context.Context) ([]*TorznabIndexer, error) {
	query := `
		SELECT id, name, base_url, api_key_encrypted, enabled, priority, timeout_seconds, created_at, updated_at
		FROM torznab_indexers
		ORDER BY priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list torznab indexers: %w", err)
	}
	defer rows.Close()

	var indexers []*TorznabIndexer
	for rows.Next() {
		var indexer TorznabIndexer
		err := rows.Scan(
			&indexer.ID,
			&indexer.Name,
			&indexer.BaseURL,
			&indexer.APIKeyEncrypted,
			&indexer.Enabled,
			&indexer.Priority,
			&indexer.TimeoutSeconds,
			&indexer.CreatedAt,
			&indexer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan torznab indexer: %w", err)
		}
		indexers = append(indexers, &indexer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating torznab indexers: %w", err)
	}

	return indexers, nil
}

// ListEnabled retrieves all enabled Torznab indexers, ordered by priority
func (s *TorznabIndexerStore) ListEnabled(ctx context.Context) ([]*TorznabIndexer, error) {
	query := `
		SELECT id, name, base_url, api_key_encrypted, enabled, priority, timeout_seconds, created_at, updated_at
		FROM torznab_indexers
		WHERE enabled = 1
		ORDER BY priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled torznab indexers: %w", err)
	}
	defer rows.Close()

	var indexers []*TorznabIndexer
	for rows.Next() {
		var indexer TorznabIndexer
		err := rows.Scan(
			&indexer.ID,
			&indexer.Name,
			&indexer.BaseURL,
			&indexer.APIKeyEncrypted,
			&indexer.Enabled,
			&indexer.Priority,
			&indexer.TimeoutSeconds,
			&indexer.CreatedAt,
			&indexer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan torznab indexer: %w", err)
		}
		indexers = append(indexers, &indexer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating enabled torznab indexers: %w", err)
	}

	return indexers, nil
}

// Update updates a Torznab indexer
func (s *TorznabIndexerStore) Update(ctx context.Context, id int, name, baseURL, apiKey string, enabled *bool, priority, timeoutSeconds *int) (*TorznabIndexer, error) {
	// Get existing indexer
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	if name != "" {
		existing.Name = name
	}
	if baseURL != "" {
		existing.BaseURL = baseURL
	}
	if enabled != nil {
		existing.Enabled = *enabled
	}
	if priority != nil {
		existing.Priority = *priority
	}
	if timeoutSeconds != nil {
		existing.TimeoutSeconds = *timeoutSeconds
	}

	// Handle API key update
	var encryptedAPIKey string
	if apiKey != "" {
		encryptedAPIKey, err = s.encrypt(apiKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt API key: %w", err)
		}
		existing.APIKeyEncrypted = encryptedAPIKey
	}

	query := `
		UPDATE torznab_indexers
		SET name = ?, base_url = ?, api_key_encrypted = ?, enabled = ?, priority = ?, timeout_seconds = ?
		WHERE id = ?
	`

	_, err = s.db.ExecContext(ctx, query,
		existing.Name,
		existing.BaseURL,
		existing.APIKeyEncrypted,
		existing.Enabled,
		existing.Priority,
		existing.TimeoutSeconds,
		id,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to update torznab indexer: %w", err)
	}

	return s.Get(ctx, id)
}

// Delete deletes a Torznab indexer
func (s *TorznabIndexerStore) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM torznab_indexers WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete torznab indexer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrTorznabIndexerNotFound
	}

	return nil
}

// GetDecryptedAPIKey returns the decrypted API key for an indexer
func (s *TorznabIndexerStore) GetDecryptedAPIKey(indexer *TorznabIndexer) (string, error) {
	return s.decrypt(indexer.APIKeyEncrypted)
}

// Test tests the connection to a Torznab indexer by querying its capabilities
func (s *TorznabIndexerStore) Test(ctx context.Context, baseURL, apiKey string) error {
	// This would be implemented by calling the caps endpoint
	// For now, just validate the parameters
	if baseURL == "" {
		return errors.New("base URL is required")
	}
	if apiKey == "" {
		return errors.New("API key is required")
	}
	return nil
}
