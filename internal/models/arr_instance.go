// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var (
	ErrArrInstanceNotFound = errors.New("arr instance not found")
)

// ArrInstanceType represents the type of ARR instance (sonarr or radarr)
type ArrInstanceType string

const (
	ArrInstanceTypeSonarr ArrInstanceType = "sonarr"
	ArrInstanceTypeRadarr ArrInstanceType = "radarr"
)

// ParseArrInstanceType validates and normalizes an ARR instance type string.
func ParseArrInstanceType(value string) (ArrInstanceType, error) {
	switch ArrInstanceType(strings.ToLower(value)) {
	case ArrInstanceTypeSonarr:
		return ArrInstanceTypeSonarr, nil
	case ArrInstanceTypeRadarr:
		return ArrInstanceTypeRadarr, nil
	default:
		return "", fmt.Errorf("invalid arr instance type: %s (must be 'sonarr' or 'radarr')", value)
	}
}

// ArrInstance represents a Sonarr or Radarr instance used for ID lookups
type ArrInstance struct {
	ID                     int             `json:"id"`
	Type                   ArrInstanceType `json:"type"`
	Name                   string          `json:"name"`
	BaseURL                string          `json:"base_url"`
	BasicUsername          *string         `json:"basic_username,omitempty"`
	APIKeyEncrypted        string          `json:"-"`
	BasicPasswordEncrypted *string         `json:"-"`
	Enabled                bool            `json:"enabled"`
	Priority               int             `json:"priority"`
	TimeoutSeconds         int             `json:"timeout_seconds"`
	LastTestAt             *time.Time      `json:"last_test_at,omitempty"`
	LastTestStatus         string          `json:"last_test_status"`
	LastTestError          *string         `json:"last_test_error,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// ArrInstanceUpdateParams captures optional fields for updating an ARR instance.
type ArrInstanceUpdateParams struct {
	Name           *string
	BaseURL        *string
	APIKey         *string
	BasicUsername  *string
	BasicPassword  *string
	Enabled        *bool
	Priority       *int
	TimeoutSeconds *int
}

type arrInstanceCreateParams struct {
	instanceType   ArrInstanceType
	name           string
	baseURL        string
	apiKey         string
	basicUsername  *string
	basicPassword  *string
	timeoutSeconds int
}

// ArrInstanceStore manages ARR instances in the database
type ArrInstanceStore struct {
	db            dbinterface.Querier
	encryptionKey []byte
}

// NewArrInstanceStore creates a new ArrInstanceStore
func NewArrInstanceStore(db dbinterface.Querier, encryptionKey []byte) (*ArrInstanceStore, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}

	return &ArrInstanceStore{
		db:            db,
		encryptionKey: encryptionKey,
	}, nil
}

// encrypt encrypts a string using AES-GCM
func (s *ArrInstanceStore) encrypt(plaintext string) (string, error) {
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
func (s *ArrInstanceStore) decrypt(ciphertext string) (string, error) {
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

// Create creates a new ARR instance
func (s *ArrInstanceStore) Create(ctx context.Context, instanceType ArrInstanceType, name, baseURL, apiKey string, basicUsername, basicPassword *string, enabled bool, priority, timeoutSeconds int) (*ArrInstance, error) {
	prepared, err := prepareCreateParams(instanceType, name, baseURL, apiKey, basicUsername, basicPassword, timeoutSeconds)
	if err != nil {
		return nil, err
	}

	encryptedAPIKey, err := s.encrypt(prepared.apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	// Encrypt basic auth password if provided
	var encryptedBasicPassword *string
	if prepared.basicPassword != nil && *prepared.basicPassword != "" {
		encrypted, err := s.encrypt(*prepared.basicPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt basic auth password: %w", err)
		}
		encryptedBasicPassword = &encrypted
	}

	// Begin transaction for string interning and insert
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern strings into string_pool
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &prepared.name, &prepared.baseURL, prepared.basicUsername)
	if err != nil {
		return nil, fmt.Errorf("failed to intern strings: %w", err)
	}
	nameID := allIDs[0].Int64
	baseURLID := allIDs[1].Int64

	query := `
		INSERT INTO arr_instances (type, name_id, base_url_id, basic_username_id, basic_password_encrypted, api_key_encrypted, enabled, priority, timeout_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var id int
	err = tx.QueryRowContext(ctx, query, prepared.instanceType, nameID, baseURLID, allIDs[2], encryptedBasicPassword, encryptedAPIKey, enabled, priority, prepared.timeoutSeconds).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create arr instance: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return s.Get(ctx, id)
}

func prepareCreateParams(instanceType ArrInstanceType, name, baseURL, apiKey string, basicUsername, basicPassword *string, timeoutSeconds int) (arrInstanceCreateParams, error) {
	name = strings.TrimSpace(name)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)

	if name == "" {
		return arrInstanceCreateParams{}, errors.New("name cannot be empty")
	}
	if baseURL == "" {
		return arrInstanceCreateParams{}, errors.New("base URL cannot be empty")
	}
	if apiKey == "" {
		return arrInstanceCreateParams{}, errors.New("API key cannot be empty")
	}
	normalizedType, err := ParseArrInstanceType(string(instanceType))
	if err != nil {
		return arrInstanceCreateParams{}, err
	}

	trimmedBasicUser := strings.TrimSpace(stringOrEmpty(basicUsername))
	trimmedBasicPass := strings.TrimSpace(stringOrEmpty(basicPassword))
	if trimmedBasicUser == "" {
		basicUsername = nil
		basicPassword = nil
	} else {
		if trimmedBasicPass == "" {
			return arrInstanceCreateParams{}, ErrBasicAuthPasswordRequired
		}
		basicUsername = &trimmedBasicUser
		basicPassword = &trimmedBasicPass
	}

	if timeoutSeconds <= 0 {
		timeoutSeconds = 15
	}

	return arrInstanceCreateParams{
		instanceType:   normalizedType,
		name:           name,
		baseURL:        baseURL,
		apiKey:         apiKey,
		basicUsername:  basicUsername,
		basicPassword:  basicPassword,
		timeoutSeconds: timeoutSeconds,
	}, nil
}

// Get retrieves an ARR instance by ID using the view
func (s *ArrInstanceStore) Get(ctx context.Context, id int) (*ArrInstance, error) {
	query := `
		SELECT id, type, name, base_url, basic_username, basic_password_encrypted, api_key_encrypted, enabled, priority, timeout_seconds, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM arr_instances_view
		WHERE id = ?
	`

	var instance ArrInstance
	var typeStr string
	var basicUser sql.NullString
	var basicPass sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&instance.ID,
		&typeStr,
		&instance.Name,
		&instance.BaseURL,
		&basicUser,
		&basicPass,
		&instance.APIKeyEncrypted,
		&instance.Enabled,
		&instance.Priority,
		&instance.TimeoutSeconds,
		&instance.LastTestAt,
		&instance.LastTestStatus,
		&instance.LastTestError,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrArrInstanceNotFound
		}
		return nil, fmt.Errorf("failed to get arr instance: %w", err)
	}

	parsedType, err := ParseArrInstanceType(typeStr)
	if err != nil {
		return nil, err
	}
	instance.Type = parsedType

	if basicUser.Valid {
		u := basicUser.String
		instance.BasicUsername = &u
	}
	if basicPass.Valid {
		p := basicPass.String
		instance.BasicPasswordEncrypted = &p
	}

	return &instance, nil
}

// List retrieves all ARR instances using the view, ordered by type, priority (descending), and name
func (s *ArrInstanceStore) List(ctx context.Context) ([]*ArrInstance, error) {
	query := `
		SELECT id, type, name, base_url, basic_username, basic_password_encrypted, api_key_encrypted, enabled, priority, timeout_seconds, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM arr_instances_view
		ORDER BY type ASC, priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list arr instances: %w", err)
	}
	defer rows.Close()

	instances := make([]*ArrInstance, 0)
	for rows.Next() {
		var instance ArrInstance
		var typeStr string
		var basicUser sql.NullString
		var basicPass sql.NullString
		err := rows.Scan(
			&instance.ID,
			&typeStr,
			&instance.Name,
			&instance.BaseURL,
			&basicUser,
			&basicPass,
			&instance.APIKeyEncrypted,
			&instance.Enabled,
			&instance.Priority,
			&instance.TimeoutSeconds,
			&instance.LastTestAt,
			&instance.LastTestStatus,
			&instance.LastTestError,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan arr instance: %w", err)
		}

		parsedType, err := ParseArrInstanceType(typeStr)
		if err != nil {
			return nil, err
		}
		instance.Type = parsedType

		if basicUser.Valid {
			u := basicUser.String
			instance.BasicUsername = &u
		}
		if basicPass.Valid {
			p := basicPass.String
			instance.BasicPasswordEncrypted = &p
		}

		instances = append(instances, &instance)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating arr instances: %w", err)
	}

	return instances, nil
}

// ListEnabled retrieves all enabled ARR instances, ordered by type, priority (descending), and name
func (s *ArrInstanceStore) ListEnabled(ctx context.Context) ([]*ArrInstance, error) {
	query := `
		SELECT id, type, name, base_url, basic_username, basic_password_encrypted, api_key_encrypted, enabled, priority, timeout_seconds, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM arr_instances_view
		WHERE enabled = 1
		ORDER BY type ASC, priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled arr instances: %w", err)
	}
	defer rows.Close()

	instances := make([]*ArrInstance, 0)
	for rows.Next() {
		var instance ArrInstance
		var typeStr string
		var basicUser sql.NullString
		var basicPass sql.NullString
		err := rows.Scan(
			&instance.ID,
			&typeStr,
			&instance.Name,
			&instance.BaseURL,
			&basicUser,
			&basicPass,
			&instance.APIKeyEncrypted,
			&instance.Enabled,
			&instance.Priority,
			&instance.TimeoutSeconds,
			&instance.LastTestAt,
			&instance.LastTestStatus,
			&instance.LastTestError,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan arr instance: %w", err)
		}

		parsedType, err := ParseArrInstanceType(typeStr)
		if err != nil {
			return nil, err
		}
		instance.Type = parsedType

		if basicUser.Valid {
			u := basicUser.String
			instance.BasicUsername = &u
		}
		if basicPass.Valid {
			p := basicPass.String
			instance.BasicPasswordEncrypted = &p
		}

		instances = append(instances, &instance)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating arr instances: %w", err)
	}

	return instances, nil
}

// ListEnabledByType retrieves all enabled ARR instances of a specific type, ordered by priority (descending)
func (s *ArrInstanceStore) ListEnabledByType(ctx context.Context, instanceType ArrInstanceType) ([]*ArrInstance, error) {
	query := `
		SELECT id, type, name, base_url, basic_username, basic_password_encrypted, api_key_encrypted, enabled, priority, timeout_seconds, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM arr_instances_view
		WHERE enabled = 1 AND type = ?
		ORDER BY priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query, instanceType)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled arr instances by type: %w", err)
	}
	defer rows.Close()

	instances := make([]*ArrInstance, 0)
	for rows.Next() {
		var instance ArrInstance
		var typeStr string
		var basicUser sql.NullString
		var basicPass sql.NullString
		err := rows.Scan(
			&instance.ID,
			&typeStr,
			&instance.Name,
			&instance.BaseURL,
			&basicUser,
			&basicPass,
			&instance.APIKeyEncrypted,
			&instance.Enabled,
			&instance.Priority,
			&instance.TimeoutSeconds,
			&instance.LastTestAt,
			&instance.LastTestStatus,
			&instance.LastTestError,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan arr instance: %w", err)
		}

		parsedType, err := ParseArrInstanceType(typeStr)
		if err != nil {
			return nil, err
		}
		instance.Type = parsedType

		if basicUser.Valid {
			u := basicUser.String
			instance.BasicUsername = &u
		}
		if basicPass.Valid {
			p := basicPass.String
			instance.BasicPasswordEncrypted = &p
		}

		instances = append(instances, &instance)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating arr instances: %w", err)
	}

	return instances, nil
}

// Update updates an existing ARR instance
func (s *ArrInstanceStore) Update(ctx context.Context, id int, params *ArrInstanceUpdateParams) (*ArrInstance, error) {
	if params == nil {
		return nil, errors.New("params cannot be nil")
	}

	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if params.Name != nil {
		normalizedName := strings.TrimSpace(*params.Name)
		if normalizedName == "" {
			return nil, errors.New("name cannot be empty")
		}
		existing.Name = normalizedName
	}
	if params.BaseURL != nil {
		normalizedBaseURL := strings.TrimRight(strings.TrimSpace(*params.BaseURL), "/")
		if normalizedBaseURL == "" {
			return nil, errors.New("base URL cannot be empty")
		}
		existing.BaseURL = normalizedBaseURL
	}
	if params.Enabled != nil {
		existing.Enabled = *params.Enabled
	}
	if params.Priority != nil {
		existing.Priority = *params.Priority
	}
	if params.TimeoutSeconds != nil {
		existing.TimeoutSeconds = *params.TimeoutSeconds
	}

	// Handle API key update
	if params.APIKey != nil {
		normalizedAPIKey := strings.TrimSpace(*params.APIKey)
		if normalizedAPIKey == "" {
			return nil, errors.New("API key cannot be empty")
		}
		encryptedAPIKey, err := s.encrypt(normalizedAPIKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt API key: %w", err)
		}
		existing.APIKeyEncrypted = encryptedAPIKey
	}

	// Handle basic auth update
	if params.BasicUsername != nil {
		trimmed := strings.TrimSpace(*params.BasicUsername)
		if trimmed == "" {
			existing.BasicUsername = nil
			existing.BasicPasswordEncrypted = nil
		} else {
			existing.BasicUsername = &trimmed
			// Only update password when explicitly provided.
			if params.BasicPassword != nil {
				trimmedPass := strings.TrimSpace(*params.BasicPassword)
				if trimmedPass == "" {
					existing.BasicPasswordEncrypted = nil
				} else {
					encrypted, err := s.encrypt(trimmedPass)
					if err != nil {
						return nil, fmt.Errorf("failed to encrypt basic auth password: %w", err)
					}
					existing.BasicPasswordEncrypted = &encrypted
				}
			}
			// If enabling basic auth and no password exists, require it.
			if existing.BasicPasswordEncrypted == nil || *existing.BasicPasswordEncrypted == "" {
				return nil, ErrBasicAuthPasswordRequired
			}
		}
	}

	// Begin transaction for string interning and update
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern strings into string_pool
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &existing.Name, &existing.BaseURL, existing.BasicUsername)
	if err != nil {
		return nil, fmt.Errorf("failed to intern strings: %w", err)
	}
	nameID := allIDs[0].Int64
	baseURLID := allIDs[1].Int64

	query := `
		UPDATE arr_instances
		SET name_id = ?, base_url_id = ?, basic_username_id = ?, basic_password_encrypted = ?, api_key_encrypted = ?, enabled = ?, priority = ?, timeout_seconds = ?
		WHERE id = ?
	`

	_, err = tx.ExecContext(ctx, query,
		nameID,
		baseURLID,
		allIDs[2],
		existing.BasicPasswordEncrypted,
		existing.APIKeyEncrypted,
		existing.Enabled,
		existing.Priority,
		existing.TimeoutSeconds,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update arr instance: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return s.Get(ctx, id)
}

// Delete deletes an ARR instance
// String pool cleanup is handled by the centralized CleanupUnusedStrings() function
func (s *ArrInstanceStore) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM arr_instances WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete arr instance: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrArrInstanceNotFound
	}

	return nil
}

// UpdateTestStatus updates the test status of an ARR instance
func (s *ArrInstanceStore) UpdateTestStatus(ctx context.Context, id int, status string, errorMsg *string) error {
	query := `
		UPDATE arr_instances
		SET last_test_at = CURRENT_TIMESTAMP, last_test_status = ?, last_test_error = ?
		WHERE id = ?
	`

	result, err := s.db.ExecContext(ctx, query, status, errorMsg, id)
	if err != nil {
		return fmt.Errorf("failed to update test status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrArrInstanceNotFound
	}

	return nil
}

// GetDecryptedAPIKey returns the decrypted API key for an ARR instance
func (s *ArrInstanceStore) GetDecryptedAPIKey(instance *ArrInstance) (string, error) {
	return s.decrypt(instance.APIKeyEncrypted)
}

// GetDecryptedBasicPassword returns the decrypted basic auth password for an ARR instance.
func (s *ArrInstanceStore) GetDecryptedBasicPassword(instance *ArrInstance) (string, error) {
	if instance.BasicPasswordEncrypted == nil || *instance.BasicPasswordEncrypted == "" {
		return "", nil
	}
	return s.decrypt(*instance.BasicPasswordEncrypted)
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
