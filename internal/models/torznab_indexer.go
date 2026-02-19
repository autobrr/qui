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
	"sort"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var (
	ErrTorznabIndexerNotFound   = errors.New("torznab indexer not found")
	ErrTorznabIndexerIDRequired = errors.New("indexer_id is required for prowlarr backends")
)

// TorznabBackend represents the backend implementation used to access a Torznab indexer.
type TorznabBackend string

const (
	// TorznabBackendJackett routes requests through a Jackett instance.
	TorznabBackendJackett TorznabBackend = "jackett"
	// TorznabBackendProwlarr routes requests through a Prowlarr instance.
	TorznabBackendProwlarr TorznabBackend = "prowlarr"
	// TorznabBackendNative talks directly to a tracker-provided Torznab/Newznab endpoint.
	TorznabBackendNative TorznabBackend = "native"
)

// ParseTorznabBackend validates and normalizes a backend string.
func ParseTorznabBackend(value string) (TorznabBackend, error) {
	if value == "" {
		return TorznabBackendJackett, nil
	}

	switch TorznabBackend(value) {
	case TorznabBackendJackett, TorznabBackendProwlarr, TorznabBackendNative:
		return TorznabBackend(value), nil
	default:
		return "", fmt.Errorf("invalid torznab backend: %s", value)
	}
}

// MustTorznabBackend parses backend and panics on error (useful for defaults).
func MustTorznabBackend(value string) TorznabBackend {
	backend, err := ParseTorznabBackend(value)
	if err != nil {
		panic(err)
	}
	return backend
}

// TorznabIndexer represents a Torznab API indexer (Jackett, Prowlarr, etc.)
type TorznabIndexer struct {
	ID                     int                      `json:"id"`
	Name                   string                   `json:"name"`
	BaseURL                string                   `json:"base_url"`
	IndexerID              string                   `json:"indexer_id"` // Jackett/Prowlarr indexer ID (e.g., "aither")
	BasicUsername          *string                  `json:"basic_username,omitempty"`
	Backend                TorznabBackend           `json:"backend"`
	APIKeyEncrypted        string                   `json:"-"`
	BasicPasswordEncrypted *string                  `json:"-"`
	Enabled                bool                     `json:"enabled"`
	Priority               int                      `json:"priority"`
	TimeoutSeconds         int                      `json:"timeout_seconds"`
	LimitDefault           int                      `json:"limit_default"`
	LimitMax               int                      `json:"limit_max"`
	Capabilities           []string                 `json:"capabilities"`
	Categories             []TorznabIndexerCategory `json:"categories"`
	LastTestAt             *time.Time               `json:"last_test_at,omitempty"`
	LastTestStatus         string                   `json:"last_test_status"`
	LastTestError          *string                  `json:"last_test_error,omitempty"`
	CreatedAt              time.Time                `json:"created_at"`
	UpdatedAt              time.Time                `json:"updated_at"`
}

// TorznabIndexerUpdateParams captures optional fields for updating an indexer.
type TorznabIndexerUpdateParams struct {
	Name           string
	BaseURL        string
	IndexerID      *string
	Backend        *TorznabBackend
	APIKey         string
	BasicUsername  *string
	BasicPassword  *string
	Enabled        *bool
	Priority       *int
	TimeoutSeconds *int
}

// TorznabIndexerCapability represents a search capability
type TorznabIndexerCapability struct {
	IndexerID      int    `json:"indexer_id"`
	CapabilityType string `json:"capability_type"`
}

// TorznabIndexerCategory represents a category supported by an indexer
type TorznabIndexerCategory struct {
	IndexerID      int    `json:"indexer_id"`
	CategoryID     int    `json:"category_id"`
	CategoryName   string `json:"category_name"`
	ParentCategory *int   `json:"parent_category_id,omitempty"`
}

// TorznabIndexerCooldown captures a persisted rate-limit suspension window for an indexer.
type TorznabIndexerCooldown struct {
	IndexerID int           `json:"indexer_id"`
	ResumeAt  time.Time     `json:"resume_at"`
	Cooldown  time.Duration `json:"cooldown"`
	Reason    string        `json:"reason,omitempty"`
}

// TorznabIndexerError represents an error that occurred with an indexer
type TorznabIndexerError struct {
	ID           int        `json:"id"`
	IndexerID    int        `json:"indexer_id"`
	ErrorMessage string     `json:"error_message"`
	ErrorCode    string     `json:"error_code"`
	OccurredAt   time.Time  `json:"occurred_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	ErrorCount   int        `json:"error_count"`
}

// TorznabIndexerLatency represents a latency measurement
type TorznabIndexerLatency struct {
	ID            int       `json:"id"`
	IndexerID     int       `json:"indexer_id"`
	OperationType string    `json:"operation_type"`
	LatencyMs     int       `json:"latency_ms"`
	Success       bool      `json:"success"`
	MeasuredAt    time.Time `json:"measured_at"`
}

// TorznabIndexerLatencyStats represents aggregated latency statistics
type TorznabIndexerLatencyStats struct {
	IndexerID          int       `json:"indexer_id"`
	OperationType      string    `json:"operation_type"`
	TotalRequests      int       `json:"total_requests"`
	SuccessfulRequests int       `json:"successful_requests"`
	AvgLatencyMs       *float64  `json:"avg_latency_ms,omitempty"`
	MinLatencyMs       *int      `json:"min_latency_ms,omitempty"`
	MaxLatencyMs       *int      `json:"max_latency_ms,omitempty"`
	SuccessRatePct     float64   `json:"success_rate_pct"`
	LastMeasuredAt     time.Time `json:"last_measured_at"`
}

// TorznabIndexerHealth represents the health status of an indexer
type TorznabIndexerHealth struct {
	IndexerID        int        `json:"indexer_id"`
	IndexerName      string     `json:"indexer_name"`
	Enabled          bool       `json:"enabled"`
	LastTestStatus   string     `json:"last_test_status"`
	ErrorsLast24h    int        `json:"errors_last_24h"`
	UnresolvedErrors int        `json:"unresolved_errors"`
	AvgLatencyMs     *float64   `json:"avg_latency_ms,omitempty"`
	SuccessRatePct   *float64   `json:"success_rate_pct,omitempty"`
	RequestsLast7d   *int       `json:"requests_last_7d,omitempty"`
	LastMeasuredAt   *time.Time `json:"last_measured_at,omitempty"`
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
func (s *TorznabIndexerStore) Create(ctx context.Context, name, baseURL, apiKey string, basicUsername, basicPassword *string, enabled bool, priority, timeoutSeconds int) (*TorznabIndexer, error) {
	return s.CreateWithIndexerID(ctx, name, baseURL, "", apiKey, basicUsername, basicPassword, enabled, priority, timeoutSeconds, TorznabBackendJackett)
}

func (s *TorznabIndexerStore) CreateWithIndexerID(ctx context.Context, name, baseURL, indexerID, apiKey string, basicUsername, basicPassword *string, enabled bool, priority, timeoutSeconds int, backend TorznabBackend) (*TorznabIndexer, error) {
	if name == "" {
		return nil, errors.New("name cannot be empty")
	}
	if baseURL == "" {
		return nil, errors.New("base URL cannot be empty")
	}
	if apiKey == "" {
		return nil, errors.New("API key cannot be empty")
	}

	trimmedBasicUser := strings.TrimSpace(stringOrEmpty(basicUsername))
	trimmedBasicPass := strings.TrimSpace(stringOrEmpty(basicPassword))
	if trimmedBasicUser == "" {
		basicUsername = nil
		basicPassword = nil
	} else {
		if trimmedBasicPass == "" {
			return nil, ErrBasicAuthPasswordRequired
		}
		basicUsername = &trimmedBasicUser
		basicPassword = &trimmedBasicPass
	}

	indexerID = strings.TrimSpace(indexerID)
	if backend == "" {
		backend = TorznabBackendJackett
	}
	if backend != TorznabBackendJackett && backend != TorznabBackendProwlarr && backend != TorznabBackendNative {
		return nil, fmt.Errorf("unsupported torznab backend: %s", backend)
	}
	if backend == TorznabBackendProwlarr && strings.TrimSpace(indexerID) == "" {
		return nil, ErrTorznabIndexerIDRequired
	}

	// Encrypt API key
	encryptedAPIKey, err := s.encrypt(apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	// Encrypt basic auth password if provided
	var encryptedBasicPassword *string
	if basicPassword != nil && *basicPassword != "" {
		encrypted, err := s.encrypt(*basicPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt basic auth password: %w", err)
		}
		encryptedBasicPassword = &encrypted
	}

	// Set defaults
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	limitDefault := 100
	limitMax := 100

	// Begin transaction for string interning and insert
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern strings into string_pool (name, baseURL, optionally indexerID + basic username)
	toIntern := make([]*string, 0, 4)
	toIntern = append(toIntern, &name, &baseURL)
	if indexerID != "" {
		toIntern = append(toIntern, &indexerID)
	}
	toIntern = append(toIntern, basicUsername)

	ids, err := dbinterface.InternStringNullable(ctx, tx, toIntern...)
	if err != nil {
		return nil, fmt.Errorf("failed to intern strings: %w", err)
	}
	nameID := ids[0].Int64
	baseURLID := ids[1].Int64

	var indexerIDStringID sql.NullInt64
	if indexerID != "" {
		indexerIDStringID = sql.NullInt64{Int64: ids[2].Int64, Valid: ids[2].Valid}
	}
	basicUserID := ids[len(ids)-1]

	query := `
		INSERT INTO torznab_indexers (name_id, base_url_id, indexer_id_string_id, basic_username_id, basic_password_encrypted, backend, api_key_encrypted, enabled, priority, timeout_seconds, limit_default, limit_max)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := tx.ExecContext(ctx, query, nameID, baseURLID, indexerIDStringID, basicUserID, encryptedBasicPassword, backend, encryptedAPIKey, enabled, priority, timeoutSeconds, limitDefault, limitMax)
	if err != nil {
		return nil, fmt.Errorf("failed to create torznab indexer: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return s.Get(ctx, int(id))
}

// Get retrieves a Torznab indexer by ID using the view
func (s *TorznabIndexerStore) Get(ctx context.Context, id int) (*TorznabIndexer, error) {
	query := `
		SELECT id, name, base_url, indexer_id, basic_username, basic_password_encrypted, backend, api_key_encrypted, enabled, priority, timeout_seconds, limit_default, limit_max, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM torznab_indexers_view
		WHERE id = ?
	`

	var indexer TorznabIndexer
	var indexerID sql.NullString
	var basicUser sql.NullString
	var basicPass sql.NullString
	var backendStr string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&indexer.ID,
		&indexer.Name,
		&indexer.BaseURL,
		&indexerID,
		&basicUser,
		&basicPass,
		&backendStr,
		&indexer.APIKeyEncrypted,
		&indexer.Enabled,
		&indexer.Priority,
		&indexer.TimeoutSeconds,
		&indexer.LimitDefault,
		&indexer.LimitMax,
		&indexer.LastTestAt,
		&indexer.LastTestStatus,
		&indexer.LastTestError,
		&indexer.CreatedAt,
		&indexer.UpdatedAt,
	)
	if indexerID.Valid {
		indexer.IndexerID = indexerID.String
	}
	if basicUser.Valid {
		u := basicUser.String
		indexer.BasicUsername = &u
	}
	if basicPass.Valid {
		p := basicPass.String
		indexer.BasicPasswordEncrypted = &p
	}
	if backendStr == "" {
		indexer.Backend = TorznabBackendJackett
	} else {
		parsedBackend, err := ParseTorznabBackend(backendStr)
		if err != nil {
			return nil, err
		}
		indexer.Backend = parsedBackend
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTorznabIndexerNotFound
		}
		return nil, fmt.Errorf("failed to get torznab indexer: %w", err)
	}

	// Load capabilities
	caps, err := s.GetCapabilities(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get capabilities: %w", err)
	}
	indexer.Capabilities = caps

	// Load categories
	categories, err := s.GetCategories(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get categories: %w", err)
	}
	indexer.Categories = categories

	return &indexer, nil
}

// List retrieves all Torznab indexers using the view, ordered by priority (descending) and name
func (s *TorznabIndexerStore) List(ctx context.Context) ([]*TorznabIndexer, error) {
	query := `
		SELECT id, name, base_url, indexer_id, basic_username, basic_password_encrypted, backend, api_key_encrypted, enabled, priority, timeout_seconds, limit_default, limit_max, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM torznab_indexers_view
		ORDER BY priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list torznab indexers: %w", err)
	}
	defer rows.Close()

	indexers := make([]*TorznabIndexer, 0)
	for rows.Next() {
		var indexer TorznabIndexer
		var indexerID sql.NullString
		var basicUser sql.NullString
		var basicPass sql.NullString
		var backendStr string
		err := rows.Scan(
			&indexer.ID,
			&indexer.Name,
			&indexer.BaseURL,
			&indexerID,
			&basicUser,
			&basicPass,
			&backendStr,
			&indexer.APIKeyEncrypted,
			&indexer.Enabled,
			&indexer.Priority,
			&indexer.TimeoutSeconds,
			&indexer.LimitDefault,
			&indexer.LimitMax,
			&indexer.LastTestAt,
			&indexer.LastTestStatus,
			&indexer.LastTestError,
			&indexer.CreatedAt,
			&indexer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan torznab indexer: %w", err)
		}
		if indexerID.Valid {
			indexer.IndexerID = indexerID.String
		}
		if basicUser.Valid {
			u := basicUser.String
			indexer.BasicUsername = &u
		}
		if basicPass.Valid {
			p := basicPass.String
			indexer.BasicPasswordEncrypted = &p
		}
		if backendStr == "" {
			indexer.Backend = TorznabBackendJackett
		} else {
			parsedBackend, err := ParseTorznabBackend(backendStr)
			if err != nil {
				return nil, err
			}
			indexer.Backend = parsedBackend
		}
		indexers = append(indexers, &indexer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating torznab indexers: %w", err)
	}

	// Load capabilities and categories for all indexers
	for _, indexer := range indexers {
		caps, err := s.GetCapabilities(ctx, indexer.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get capabilities for indexer %d: %w", indexer.ID, err)
		}
		indexer.Capabilities = caps

		categories, err := s.GetCategories(ctx, indexer.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get categories for indexer %d: %w", indexer.ID, err)
		}
		indexer.Categories = categories
	}

	return indexers, nil
}

// ListEnabled retrieves all enabled Torznab indexers using the view, ordered by priority
func (s *TorznabIndexerStore) ListEnabled(ctx context.Context) ([]*TorznabIndexer, error) {
	query := `
		SELECT id, name, base_url, indexer_id, basic_username, basic_password_encrypted, backend, api_key_encrypted, enabled, priority, timeout_seconds, limit_default, limit_max, last_test_at, last_test_status, last_test_error, created_at, updated_at
		FROM torznab_indexers_view
		WHERE enabled = 1
		ORDER BY priority DESC, name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled torznab indexers: %w", err)
	}
	defer rows.Close()

	indexers := make([]*TorznabIndexer, 0)
	for rows.Next() {
		var indexer TorznabIndexer
		var indexerID sql.NullString
		var basicUser sql.NullString
		var basicPass sql.NullString
		var backendStr string
		err := rows.Scan(
			&indexer.ID,
			&indexer.Name,
			&indexer.BaseURL,
			&indexerID,
			&basicUser,
			&basicPass,
			&backendStr,
			&indexer.APIKeyEncrypted,
			&indexer.Enabled,
			&indexer.Priority,
			&indexer.TimeoutSeconds,
			&indexer.LimitDefault,
			&indexer.LimitMax,
			&indexer.LastTestAt,
			&indexer.LastTestStatus,
			&indexer.LastTestError,
			&indexer.CreatedAt,
			&indexer.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan torznab indexer: %w", err)
		}
		if indexerID.Valid {
			indexer.IndexerID = indexerID.String
		}
		if basicUser.Valid {
			u := basicUser.String
			indexer.BasicUsername = &u
		}
		if basicPass.Valid {
			p := basicPass.String
			indexer.BasicPasswordEncrypted = &p
		}
		if backendStr == "" {
			indexer.Backend = TorznabBackendJackett
		} else {
			parsedBackend, err := ParseTorznabBackend(backendStr)
			if err != nil {
				return nil, err
			}
			indexer.Backend = parsedBackend
		}
		indexers = append(indexers, &indexer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating enabled torznab indexers: %w", err)
	}

	// Load capabilities and categories for all indexers
	for _, indexer := range indexers {
		caps, err := s.GetCapabilities(ctx, indexer.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get capabilities for indexer %d: %w", indexer.ID, err)
		}
		indexer.Capabilities = caps

		categories, err := s.GetCategories(ctx, indexer.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get categories for indexer %d: %w", indexer.ID, err)
		}
		indexer.Categories = categories
	}

	return indexers, nil
}

// Update updates a Torznab indexer
func (s *TorznabIndexerStore) Update(ctx context.Context, id int, params TorznabIndexerUpdateParams) (*TorznabIndexer, error) {
	// Get existing indexer
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	if params.Name != "" {
		existing.Name = params.Name
	}
	if params.BaseURL != "" {
		existing.BaseURL = params.BaseURL
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
	if params.IndexerID != nil {
		existing.IndexerID = strings.TrimSpace(*params.IndexerID)
	}
	if params.Backend != nil {
		backend := *params.Backend
		if backend == "" {
			backend = TorznabBackendJackett
		}
		if backend != TorznabBackendJackett && backend != TorznabBackendProwlarr && backend != TorznabBackendNative {
			return nil, fmt.Errorf("unsupported torznab backend: %s", backend)
		}
		existing.Backend = backend
	}

	if existing.Backend == TorznabBackendProwlarr && strings.TrimSpace(existing.IndexerID) == "" {
		return nil, ErrTorznabIndexerIDRequired
	}

	// Handle API key update
	var encryptedAPIKey string
	if params.APIKey != "" {
		encryptedAPIKey, err = s.encrypt(params.APIKey)
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
	stringsToIntern := []string{existing.Name, existing.BaseURL}
	if existing.IndexerID != "" {
		stringsToIntern = append(stringsToIntern, existing.IndexerID)
	}
	// Optional basic username is interned via nullable API.
	basicUser := existing.BasicUsername

	// Intern required strings first, then optionally basic username.
	ids, err := dbinterface.InternStrings(ctx, tx, stringsToIntern...)
	if err != nil {
		return nil, fmt.Errorf("failed to intern strings: %w", err)
	}
	nameID, baseURLID := ids[0], ids[1]
	var indexerIDStringID sql.NullInt64
	if existing.IndexerID != "" {
		indexerIDStringID = sql.NullInt64{Int64: ids[2], Valid: true}
	}

	var basicUserID sql.NullInt64
	if basicUser != nil && strings.TrimSpace(*basicUser) != "" {
		bids, err := dbinterface.InternStrings(ctx, tx, strings.TrimSpace(*basicUser))
		if err != nil {
			return nil, fmt.Errorf("failed to intern basic username: %w", err)
		}
		basicUserID = sql.NullInt64{Int64: bids[0], Valid: true}
	}

	query := `
		UPDATE torznab_indexers
		SET name_id = ?, base_url_id = ?, indexer_id_string_id = ?, basic_username_id = ?, basic_password_encrypted = ?, backend = ?, api_key_encrypted = ?, enabled = ?, priority = ?, timeout_seconds = ?
		WHERE id = ?
	`

	_, err = tx.ExecContext(ctx, query,
		nameID,
		baseURLID,
		indexerIDStringID,
		basicUserID,
		existing.BasicPasswordEncrypted,
		existing.Backend,
		existing.APIKeyEncrypted,
		existing.Enabled,
		existing.Priority,
		existing.TimeoutSeconds,
		id,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to update torznab indexer: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return s.Get(ctx, id)
}

// Delete deletes a Torznab indexer
// String pool cleanup is handled by the centralized CleanupUnusedStrings() function
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

// UpdateTestStatus updates the test status of an indexer
func (s *TorznabIndexerStore) UpdateTestStatus(ctx context.Context, id int, status string, errorMsg *string) error {
	query := `
		UPDATE torznab_indexers
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
		return ErrTorznabIndexerNotFound
	}

	return nil
}

// GetDecryptedAPIKey returns the decrypted API key for an indexer
func (s *TorznabIndexerStore) GetDecryptedAPIKey(indexer *TorznabIndexer) (string, error) {
	return s.decrypt(indexer.APIKeyEncrypted)
}

// GetDecryptedBasicPassword returns the decrypted basic auth password for an indexer.
func (s *TorznabIndexerStore) GetDecryptedBasicPassword(indexer *TorznabIndexer) (string, error) {
	if indexer.BasicPasswordEncrypted == nil || *indexer.BasicPasswordEncrypted == "" {
		return "", nil
	}
	return s.decrypt(*indexer.BasicPasswordEncrypted)
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

// GetCapabilities retrieves all capabilities for an indexer
func (s *TorznabIndexerStore) GetCapabilities(ctx context.Context, indexerID int) ([]string, error) {
	query := `
		SELECT capability_type
		FROM torznab_indexer_capabilities_view
		WHERE indexer_id = ?
		ORDER BY capability_type
	`

	rows, err := s.db.QueryContext(ctx, query, indexerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query capabilities: %w", err)
	}
	defer rows.Close()

	capabilities := make([]string, 0)
	for rows.Next() {
		var cap string
		if err := rows.Scan(&cap); err != nil {
			return nil, fmt.Errorf("failed to scan capability: %w", err)
		}
		capabilities = append(capabilities, cap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating capabilities: %w", err)
	}

	return capabilities, nil
}

// SetCapabilities replaces all capabilities for an indexer
func (s *TorznabIndexerStore) SetCapabilities(ctx context.Context, indexerID int, capabilities []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing capabilities
	_, err = tx.ExecContext(ctx, "DELETE FROM torznab_indexer_capabilities WHERE indexer_id = ?", indexerID)
	if err != nil {
		return fmt.Errorf("failed to delete existing capabilities: %w", err)
	}

	// Insert new capabilities
	if len(capabilities) > 0 {
		// Intern capability strings
		capIDs, err := dbinterface.InternStrings(ctx, tx, capabilities...)
		if err != nil {
			return fmt.Errorf("failed to intern capability strings: %w", err)
		}

		// Build bulk insert query
		queryTemplate := "INSERT INTO torznab_indexer_capabilities (indexer_id, capability_type_id) VALUES %s"
		const capabilityBatchSize = 200 // Keep under SQLite's 999 variable limit (200 * 2 = 400 placeholders)
		fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 2, capabilityBatchSize)

		// Batch insert capabilities
		args := make([]interface{}, 0, capabilityBatchSize*2)
		for i := 0; i < len(capIDs); i += capabilityBatchSize {
			end := i + capabilityBatchSize
			if end > len(capIDs) {
				end = len(capIDs)
			}
			batch := capIDs[i:end]

			// Reset args for this batch
			args = args[:0]
			var query string
			if len(batch) == capabilityBatchSize {
				query = fullBatchQuery
			} else {
				// Build query for partial final batch
				query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 2, len(batch))
			}

			for _, capID := range batch {
				args = append(args, indexerID, capID)
			}

			_, err = tx.ExecContext(ctx, query, args...)
			if err != nil {
				return fmt.Errorf("failed to insert capabilities batch: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetCategories retrieves all categories for an indexer
func (s *TorznabIndexerStore) GetCategories(ctx context.Context, indexerID int) ([]TorznabIndexerCategory, error) {
	query := `
		SELECT indexer_id, category_id, category_name, parent_category_id
		FROM torznab_indexer_categories_view
		WHERE indexer_id = ?
		ORDER BY category_id
	`

	rows, err := s.db.QueryContext(ctx, query, indexerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query categories: %w", err)
	}
	defer rows.Close()

	categories := make([]TorznabIndexerCategory, 0)
	for rows.Next() {
		var cat TorznabIndexerCategory
		if err := rows.Scan(&cat.IndexerID, &cat.CategoryID, &cat.CategoryName, &cat.ParentCategory); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		categories = append(categories, cat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating categories: %w", err)
	}

	return categories, nil
}

// SetCategories replaces all categories for an indexer
func (s *TorznabIndexerStore) SetCategories(ctx context.Context, indexerID int, categories []TorznabIndexerCategory) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing categories
	_, err = tx.ExecContext(ctx, "DELETE FROM torznab_indexer_categories WHERE indexer_id = ?", indexerID)
	if err != nil {
		return fmt.Errorf("failed to delete existing categories: %w", err)
	}

	// Insert new categories
	if len(categories) > 0 {
		unique := make(map[int]TorznabIndexerCategory, len(categories))
		for _, cat := range categories {
			if _, exists := unique[cat.CategoryID]; !exists {
				unique[cat.CategoryID] = cat
			}
		}

		ordered := make([]TorznabIndexerCategory, 0, len(unique))
		for _, cat := range unique {
			ordered = append(ordered, cat)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].CategoryID < ordered[j].CategoryID })

		names := make([]string, len(ordered))
		for i, cat := range ordered {
			names[i] = cat.CategoryName
		}
		nameIDs, err := dbinterface.InternStrings(ctx, tx, names...)
		if err != nil {
			return fmt.Errorf("failed to intern category names: %w", err)
		}

		// Build bulk insert query
		queryTemplate := "INSERT INTO torznab_indexer_categories (indexer_id, category_id, category_name_id, parent_category_id) VALUES %s"
		const categoryBatchSize = 200 // Keep under SQLite's 999 variable limit (200 * 4 = 800 placeholders)
		fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 4, categoryBatchSize)

		// Batch insert categories
		args := make([]interface{}, 0, categoryBatchSize*4)
		for i := 0; i < len(ordered); i += categoryBatchSize {
			end := i + categoryBatchSize
			if end > len(ordered) {
				end = len(ordered)
			}
			batch := ordered[i:end]

			// Reset args for this batch
			args = args[:0]
			var query string
			if len(batch) == categoryBatchSize {
				query = fullBatchQuery
			} else {
				// Build query for partial final batch
				query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 4, len(batch))
			}

			for j, cat := range batch {
				nameID := nameIDs[i+j]
				args = append(args, indexerID, cat.CategoryID, nameID, cat.ParentCategory)
			}

			_, err = tx.ExecContext(ctx, query, args...)
			if err != nil {
				return fmt.Errorf("failed to insert categories batch: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SetLimits updates the limit_default and limit_max values for an indexer
func (s *TorznabIndexerStore) SetLimits(ctx context.Context, indexerID, limitDefault, limitMax int) error {
	query := `
		UPDATE torznab_indexers
		SET limit_default = ?, limit_max = ?
		WHERE id = ?
	`

	result, err := s.db.ExecContext(ctx, query, limitDefault, limitMax, indexerID)
	if err != nil {
		return fmt.Errorf("failed to update indexer limits: %w", err)
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

// RecordError records an error for an indexer
func (s *TorznabIndexerStore) RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern error message
	ids, err := dbinterface.InternStrings(ctx, tx, errorMessage)
	if err != nil {
		return fmt.Errorf("failed to intern error message: %w", err)
	}
	errorMessageID := ids[0]

	// Check if there's a recent unresolved error with the same message
	var existingID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM torznab_indexer_errors
		WHERE indexer_id = ? AND error_message_id = ? AND resolved_at IS NULL
		ORDER BY occurred_at DESC
		LIMIT 1
	`, indexerID, errorMessageID).Scan(&existingID)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to check for existing error: %w", err)
	}

	if existingID.Valid {
		// Increment error count for existing error
		_, err = tx.ExecContext(ctx, `
			UPDATE torznab_indexer_errors
			SET error_count = error_count + 1, occurred_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, existingID.Int64)
		if err != nil {
			return fmt.Errorf("failed to increment error count: %w", err)
		}
	} else {
		// Insert new error
		_, err = tx.ExecContext(ctx, `
			INSERT INTO torznab_indexer_errors (indexer_id, error_message_id, error_code)
			VALUES (?, ?, ?)
		`, indexerID, errorMessageID, errorCode)
		if err != nil {
			return fmt.Errorf("failed to insert error: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ResolveErrors marks all unresolved errors for an indexer as resolved
func (s *TorznabIndexerStore) ResolveErrors(ctx context.Context, indexerID int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE torznab_indexer_errors
		SET resolved_at = CURRENT_TIMESTAMP
		WHERE indexer_id = ? AND resolved_at IS NULL
	`, indexerID)
	if err != nil {
		return fmt.Errorf("failed to resolve errors: %w", err)
	}
	return nil
}

// GetRecentErrors retrieves recent errors for an indexer
func (s *TorznabIndexerStore) GetRecentErrors(ctx context.Context, indexerID int, limit int) ([]TorznabIndexerError, error) {
	query := `
		SELECT tie.id, tie.indexer_id, sp.value as error_message, tie.error_code, tie.occurred_at, tie.resolved_at, tie.error_count
		FROM torznab_indexer_errors tie
		INNER JOIN string_pool sp ON tie.error_message_id = sp.id
		WHERE tie.indexer_id = ?
		ORDER BY tie.occurred_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, indexerID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query errors: %w", err)
	}
	defer rows.Close()

	errors := make([]TorznabIndexerError, 0)
	for rows.Next() {
		var e TorznabIndexerError
		if err := rows.Scan(&e.ID, &e.IndexerID, &e.ErrorMessage, &e.ErrorCode, &e.OccurredAt, &e.ResolvedAt, &e.ErrorCount); err != nil {
			return nil, fmt.Errorf("failed to scan error: %w", err)
		}
		errors = append(errors, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating errors: %w", err)
	}

	return errors, nil
}

// RecordLatency records a latency measurement for an indexer
func (s *TorznabIndexerStore) RecordLatency(ctx context.Context, indexerID int, operationType string, latencyMs int, success bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO torznab_indexer_latency (indexer_id, operation_type, latency_ms, success)
		VALUES (?, ?, ?, ?)
	`, indexerID, operationType, latencyMs, success)
	if err != nil {
		return fmt.Errorf("failed to record latency: %w", err)
	}
	return nil
}

// GetLatencyStats retrieves aggregated latency statistics for an indexer
func (s *TorznabIndexerStore) GetLatencyStats(ctx context.Context, indexerID int) ([]TorznabIndexerLatencyStats, error) {
	query := `
		SELECT indexer_id, operation_type, total_requests, successful_requests, avg_latency_ms, min_latency_ms, max_latency_ms, success_rate_pct, last_measured_at
		FROM torznab_indexer_latency_stats
		WHERE indexer_id = ?
		ORDER BY operation_type
	`

	rows, err := s.db.QueryContext(ctx, query, indexerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query latency stats: %w", err)
	}
	defer rows.Close()

	stats := make([]TorznabIndexerLatencyStats, 0)
	for rows.Next() {
		var s TorznabIndexerLatencyStats
		if err := rows.Scan(&s.IndexerID, &s.OperationType, &s.TotalRequests, &s.SuccessfulRequests, &s.AvgLatencyMs, &s.MinLatencyMs, &s.MaxLatencyMs, &s.SuccessRatePct, &s.LastMeasuredAt); err != nil {
			return nil, fmt.Errorf("failed to scan latency stats: %w", err)
		}
		stats = append(stats, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating latency stats: %w", err)
	}

	return stats, nil
}

// GetHealth retrieves health information for an indexer
func (s *TorznabIndexerStore) GetHealth(ctx context.Context, indexerID int) (*TorznabIndexerHealth, error) {
	query := `
		SELECT indexer_id, indexer_name, enabled, last_test_status, errors_last_24h, unresolved_errors, avg_latency_ms, success_rate_pct, requests_last_7d, last_measured_at
		FROM torznab_indexer_health
		WHERE indexer_id = ?
	`

	var health TorznabIndexerHealth
	err := s.db.QueryRowContext(ctx, query, indexerID).Scan(
		&health.IndexerID,
		&health.IndexerName,
		&health.Enabled,
		&health.LastTestStatus,
		&health.ErrorsLast24h,
		&health.UnresolvedErrors,
		&health.AvgLatencyMs,
		&health.SuccessRatePct,
		&health.RequestsLast7d,
		&health.LastMeasuredAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTorznabIndexerNotFound
		}
		return nil, fmt.Errorf("failed to get health: %w", err)
	}

	return &health, nil
}

// GetAllHealth retrieves health information for all indexers
func (s *TorznabIndexerStore) GetAllHealth(ctx context.Context) ([]TorznabIndexerHealth, error) {
	query := `
		SELECT indexer_id, indexer_name, enabled, last_test_status, errors_last_24h, unresolved_errors, avg_latency_ms, success_rate_pct, requests_last_7d, last_measured_at
		FROM torznab_indexer_health
		ORDER BY indexer_name
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query health: %w", err)
	}
	defer rows.Close()

	healthList := make([]TorznabIndexerHealth, 0)
	for rows.Next() {
		var health TorznabIndexerHealth
		if err := rows.Scan(&health.IndexerID, &health.IndexerName, &health.Enabled, &health.LastTestStatus, &health.ErrorsLast24h, &health.UnresolvedErrors, &health.AvgLatencyMs, &health.SuccessRatePct, &health.RequestsLast7d, &health.LastMeasuredAt); err != nil {
			return nil, fmt.Errorf("failed to scan health: %w", err)
		}
		healthList = append(healthList, health)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating health: %w", err)
	}

	return healthList, nil
}

// CleanupOldLatency removes latency records older than the specified duration
func (s *TorznabIndexerStore) CleanupOldLatency(ctx context.Context, olderThan time.Duration) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM torznab_indexer_latency
		WHERE measured_at < datetime('now', ?)
	`, fmt.Sprintf("-%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old latency: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// ListRateLimitCooldowns returns any persisted cooldown windows for Torznab indexers.
func (s *TorznabIndexerStore) ListRateLimitCooldowns(ctx context.Context) ([]TorznabIndexerCooldown, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT indexer_id, resume_at, cooldown_seconds, COALESCE(reason, '')
		FROM torznab_indexer_cooldowns
	`)
	if err != nil {
		return nil, fmt.Errorf("list torznab cooldowns: %w", err)
	}
	defer rows.Close()

	cooldowns := make([]TorznabIndexerCooldown, 0)
	for rows.Next() {
		var (
			c       TorznabIndexerCooldown
			seconds int64
		)
		if err := rows.Scan(&c.IndexerID, &c.ResumeAt, &seconds, &c.Reason); err != nil {
			return nil, fmt.Errorf("scan torznab cooldown: %w", err)
		}
		c.Cooldown = time.Duration(seconds) * time.Second
		cooldowns = append(cooldowns, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate torznab cooldowns: %w", err)
	}

	return cooldowns, nil
}

// UpsertRateLimitCooldown stores or updates the cooldown window for an indexer.
func (s *TorznabIndexerStore) UpsertRateLimitCooldown(ctx context.Context, indexerID int, resumeAt time.Time, cooldown time.Duration, reason string) error {
	seconds := int64(cooldown.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO torznab_indexer_cooldowns (indexer_id, resume_at, cooldown_seconds, reason)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(indexer_id)
		DO UPDATE SET resume_at = excluded.resume_at,
			cooldown_seconds = excluded.cooldown_seconds,
			reason = excluded.reason,
			updated_at = CURRENT_TIMESTAMP
	`, indexerID, resumeAt.UTC(), seconds, reason)
	if err != nil {
		return fmt.Errorf("upsert torznab cooldown: %w", err)
	}
	return nil
}

// DeleteRateLimitCooldown removes any persisted cooldown for the provided indexer ID.
func (s *TorznabIndexerStore) DeleteRateLimitCooldown(ctx context.Context, indexerID int) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM torznab_indexer_cooldowns WHERE indexer_id = ?
	`, indexerID)
	if err != nil {
		return fmt.Errorf("delete torznab cooldown: %w", err)
	}
	return nil
}
