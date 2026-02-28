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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/domain"
)

var ErrInstanceNotFound = errors.New("instance not found")

type Instance struct {
	ID                       int     `json:"id"`
	Name                     string  `json:"name"`
	Host                     string  `json:"host"`
	Username                 string  `json:"username"`
	PasswordEncrypted        string  `json:"-"`
	BasicUsername            *string `json:"basic_username,omitempty"`
	BasicPasswordEncrypted   *string `json:"-"`
	TLSSkipVerify            bool    `json:"tlsSkipVerify"`
	SortOrder                int     `json:"sortOrder"`
	IsActive                 bool    `json:"isActive"`
	HasLocalFilesystemAccess bool    `json:"hasLocalFilesystemAccess"`
	// Hardlink mode settings (per-instance)
	UseHardlinks      bool   `json:"useHardlinks"`
	HardlinkBaseDir   string `json:"hardlinkBaseDir"`
	HardlinkDirPreset string `json:"hardlinkDirPreset"` // "flat", "by-tracker", "by-instance"
	// Reflink mode (copy-on-write clones) - mutually exclusive with hardlink mode
	UseReflinks bool `json:"useReflinks"`
	// Fallback to regular mode when reflink/hardlink fails
	FallbackToRegularMode bool `json:"fallbackToRegularMode"`
}

func (i Instance) MarshalJSON() ([]byte, error) {
	// Create the JSON structure with redacted password fields
	return json.Marshal(&struct {
		ID                       int        `json:"id"`
		Name                     string     `json:"name"`
		Host                     string     `json:"host"`
		Username                 string     `json:"username"`
		Password                 string     `json:"password,omitempty"`
		BasicUsername            *string    `json:"basic_username,omitempty"`
		BasicPassword            string     `json:"basic_password,omitempty"`
		TLSSkipVerify            bool       `json:"tlsSkipVerify"`
		IsActive                 bool       `json:"isActive"`
		HasLocalFilesystemAccess bool       `json:"hasLocalFilesystemAccess"`
		UseHardlinks             bool       `json:"useHardlinks"`
		HardlinkBaseDir          string     `json:"hardlinkBaseDir"`
		HardlinkDirPreset        string     `json:"hardlinkDirPreset"`
		UseReflinks              bool       `json:"useReflinks"`
		FallbackToRegularMode    bool       `json:"fallbackToRegularMode"`
		LastConnectedAt          *time.Time `json:"last_connected_at,omitempty"`
		CreatedAt                time.Time  `json:"created_at"`
		UpdatedAt                time.Time  `json:"updated_at"`
		SortOrder                int        `json:"sortOrder"`
	}{
		ID:            i.ID,
		Name:          i.Name,
		Host:          i.Host,
		Username:      i.Username,
		Password:      domain.RedactString(i.PasswordEncrypted),
		BasicUsername: i.BasicUsername,
		BasicPassword: func() string {
			if i.BasicPasswordEncrypted != nil {
				return domain.RedactString(*i.BasicPasswordEncrypted)
			}
			return ""
		}(),
		TLSSkipVerify:            i.TLSSkipVerify,
		SortOrder:                i.SortOrder,
		IsActive:                 i.IsActive,
		HasLocalFilesystemAccess: i.HasLocalFilesystemAccess,
		UseHardlinks:             i.UseHardlinks,
		HardlinkBaseDir:          i.HardlinkBaseDir,
		HardlinkDirPreset:        i.HardlinkDirPreset,
		UseReflinks:              i.UseReflinks,
		FallbackToRegularMode:    i.FallbackToRegularMode,
	})
}

func (i *Instance) UnmarshalJSON(data []byte) error {
	// Temporary struct for unmarshaling
	var temp struct {
		ID                       int        `json:"id"`
		Name                     string     `json:"name"`
		Host                     string     `json:"host"`
		Username                 string     `json:"username"`
		Password                 string     `json:"password,omitempty"`
		BasicUsername            *string    `json:"basic_username,omitempty"`
		BasicPassword            string     `json:"basic_password,omitempty"`
		TLSSkipVerify            *bool      `json:"tlsSkipVerify,omitempty"`
		IsActive                 bool       `json:"isActive"`
		HasLocalFilesystemAccess *bool      `json:"hasLocalFilesystemAccess,omitempty"`
		UseHardlinks             *bool      `json:"useHardlinks,omitempty"`
		HardlinkBaseDir          *string    `json:"hardlinkBaseDir,omitempty"`
		HardlinkDirPreset        *string    `json:"hardlinkDirPreset,omitempty"`
		UseReflinks              *bool      `json:"useReflinks,omitempty"`
		FallbackToRegularMode    *bool      `json:"fallbackToRegularMode,omitempty"`
		LastConnectedAt          *time.Time `json:"last_connected_at,omitempty"`
		CreatedAt                time.Time  `json:"created_at"`
		UpdatedAt                time.Time  `json:"updated_at"`
		SortOrder                *int       `json:"sortOrder,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Copy non-secret fields
	i.ID = temp.ID
	i.Name = temp.Name
	i.Host = temp.Host
	i.Username = temp.Username
	i.BasicUsername = temp.BasicUsername

	if temp.TLSSkipVerify != nil {
		i.TLSSkipVerify = *temp.TLSSkipVerify
	}

	if temp.SortOrder != nil {
		i.SortOrder = *temp.SortOrder
	}

	i.IsActive = temp.IsActive

	// HasLocalFilesystemAccess defaults to false if not provided (opt-in feature)
	if temp.HasLocalFilesystemAccess != nil {
		i.HasLocalFilesystemAccess = *temp.HasLocalFilesystemAccess
	}

	// Hardlink settings (all default to false/empty if not provided)
	if temp.UseHardlinks != nil {
		i.UseHardlinks = *temp.UseHardlinks
	}
	if temp.HardlinkBaseDir != nil {
		i.HardlinkBaseDir = *temp.HardlinkBaseDir
	}
	if temp.HardlinkDirPreset != nil {
		i.HardlinkDirPreset = *temp.HardlinkDirPreset
	}
	// Reflink setting (defaults to false if not provided)
	if temp.UseReflinks != nil {
		i.UseReflinks = *temp.UseReflinks
	}
	// Fallback to regular mode setting (defaults to false if not provided)
	if temp.FallbackToRegularMode != nil {
		i.FallbackToRegularMode = *temp.FallbackToRegularMode
	}

	// Handle password - don't overwrite if redacted
	if temp.Password != "" && !domain.IsRedactedString(temp.Password) {
		i.PasswordEncrypted = temp.Password
	}

	// Handle basic password - don't overwrite if redacted
	if temp.BasicPassword != "" && !domain.IsRedactedString(temp.BasicPassword) {
		i.BasicPasswordEncrypted = &temp.BasicPassword
	}

	return nil
}

type InstanceStore struct {
	db            dbinterface.Querier
	encryptionKey []byte
}

func NewInstanceStore(db dbinterface.Querier, encryptionKey []byte) (*InstanceStore, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}

	return &InstanceStore{
		db:            db,
		encryptionKey: encryptionKey,
	}, nil
}

// encrypt encrypts a string using AES-GCM
func (s *InstanceStore) encrypt(plaintext string) (string, error) {
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
func (s *InstanceStore) decrypt(ciphertext string) (string, error) {
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

// validateAndNormalizeHost validates and normalizes a qBittorrent instance host URL
func validateAndNormalizeHost(rawHost string) (string, error) {
	// Trim whitespace
	rawHost = strings.TrimSpace(rawHost)

	// Check for empty host
	if rawHost == "" {
		return "", errors.New("host cannot be empty")
	}

	// Check if host already has a valid scheme
	if !strings.Contains(rawHost, "://") {
		// No scheme, add http://
		rawHost = "http://" + rawHost
	}

	// Parse the URL
	u, err := url.Parse(rawHost)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	// Validate scheme
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q: must be http or https", u.Scheme)
	}

	// Validate host
	if u.Host == "" {
		return "", errors.New("URL must include a host")
	}

	return u.String(), nil
}

func (s *InstanceStore) Create(ctx context.Context, name, rawHost, username, password string, basicUsername, basicPassword *string, tlsSkipVerify bool, hasLocalFilesystemAccess *bool) (*Instance, error) {
	// Validate and normalize the host
	normalizedHost, err := validateAndNormalizeHost(rawHost)
	if err != nil {
		return nil, err
	}

	// Localhost bypass auth uses an empty username, and the qBittorrent client should not attempt a login.
	if username == "" {
		password = ""
	}

	// Encrypt the password
	encryptedPassword, err := s.encrypt(password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt password: %w", err)
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

	// Start a transaction to ensure atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern all strings in a single call
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &name, &normalizedHost, &username, basicUsername)
	if err != nil {
		return nil, fmt.Errorf("failed to intern strings: %w", err)
	}

	// Ensure required fields (name, host, username) have valid IDs
	// If username is empty (localhost bypass), intern the empty string
	nameID := allIDs[0].Int64
	hostID := allIDs[1].Int64

	var usernameID int64
	if allIDs[2].Valid {
		usernameID = allIDs[2].Int64
	} else {
		// Username was empty - intern empty string for localhost bypass auth
		usernameID, err = dbinterface.InternEmptyString(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("failed to intern empty username: %w", err)
		}
	}

	// Insert instance with the interned IDs
	var instanceID int
	var passwordEncrypted sql.NullString
	var basicPasswordEncrypted sql.NullString
	var tlsSkipVerifyResult bool
	var hasLocalFilesystemAccessResult bool
	var sortOrder int
	var isActive bool

	// Default hasLocalFilesystemAccess to false if not provided (opt-in feature)
	localAccess := false
	if hasLocalFilesystemAccess != nil {
		localAccess = *hasLocalFilesystemAccess
	}

	err = tx.QueryRowContext(ctx, `
		WITH next_sort AS (
			SELECT COALESCE(MAX(sort_order), -1) + 1 AS next_order FROM instances
		)
		INSERT INTO instances (
			name_id,
			host_id,
			username_id,
			password_encrypted,
			basic_username_id,
			basic_password_encrypted,
			tls_skip_verify,
			has_local_filesystem_access,
			sort_order
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, next_order FROM next_sort
		RETURNING id, password_encrypted, basic_password_encrypted, tls_skip_verify, sort_order, is_active, has_local_filesystem_access
	`,
		nameID,
		hostID,
		usernameID,
		encryptedPassword,
		allIDs[3],
		encryptedBasicPassword,
		tlsSkipVerify,
		localAccess,
	).Scan(
		&instanceID,
		&passwordEncrypted,
		&basicPasswordEncrypted,
		&tlsSkipVerifyResult,
		&sortOrder,
		&isActive,
		&hasLocalFilesystemAccessResult,
	)
	if err != nil {
		return nil, err
	}

	instance := &Instance{
		ID:                       instanceID,
		Name:                     name,
		Host:                     normalizedHost,
		Username:                 username,
		PasswordEncrypted:        passwordEncrypted.String,
		TLSSkipVerify:            tlsSkipVerifyResult,
		HasLocalFilesystemAccess: hasLocalFilesystemAccessResult,
		SortOrder:                sortOrder,
		IsActive:                 isActive,
	}

	if basicUsername != nil {
		instance.BasicUsername = basicUsername
	}
	if basicPasswordEncrypted.Valid {
		instance.BasicPasswordEncrypted = &basicPasswordEncrypted.String
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return instance, nil
}

func (s *InstanceStore) Get(ctx context.Context, id int) (*Instance, error) {
	query := `
		SELECT id, name, host, username, password_encrypted, basic_username, basic_password_encrypted, tls_skip_verify, sort_order, is_active, has_local_filesystem_access, use_hardlinks, hardlink_base_dir, hardlink_dir_preset, use_reflinks, fallback_to_regular_mode
		FROM instances_view
		WHERE id = ?
	`

	var instanceID int
	var name, host, username, passwordEncrypted string
	var basicUsername, basicPasswordEncrypted sql.NullString
	var tlsSkipVerify bool
	var sortOrder int
	var isActive bool
	var hasLocalFilesystemAccess bool
	var useHardlinks bool
	var hardlinkBaseDir, hardlinkDirPreset string
	var useReflinks bool
	var fallbackToRegularMode bool

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&instanceID,
		&name,
		&host,
		&username,
		&passwordEncrypted,
		&basicUsername,
		&basicPasswordEncrypted,
		&tlsSkipVerify,
		&sortOrder,
		&isActive,
		&hasLocalFilesystemAccess,
		&useHardlinks,
		&hardlinkBaseDir,
		&hardlinkDirPreset,
		&useReflinks,
		&fallbackToRegularMode,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}

	instance := &Instance{
		ID:                       instanceID,
		Name:                     name,
		Host:                     host,
		Username:                 username,
		PasswordEncrypted:        passwordEncrypted,
		TLSSkipVerify:            tlsSkipVerify,
		SortOrder:                sortOrder,
		IsActive:                 isActive,
		HasLocalFilesystemAccess: hasLocalFilesystemAccess,
		UseHardlinks:             useHardlinks,
		HardlinkBaseDir:          hardlinkBaseDir,
		HardlinkDirPreset:        hardlinkDirPreset,
		UseReflinks:              useReflinks,
		FallbackToRegularMode:    fallbackToRegularMode,
	}

	if basicUsername.Valid {
		instance.BasicUsername = &basicUsername.String
	}
	if basicPasswordEncrypted.Valid {
		instance.BasicPasswordEncrypted = &basicPasswordEncrypted.String
	}

	return instance, nil
}

func (s *InstanceStore) List(ctx context.Context) ([]*Instance, error) {
	orderByName := "name COLLATE NOCASE"
	if dbinterface.DialectOf(s.db) == "postgres" {
		orderByName = "LOWER(name)"
	}

	query := fmt.Sprintf(`
		SELECT id, name, host, username, password_encrypted, basic_username, basic_password_encrypted, tls_skip_verify, sort_order, is_active, has_local_filesystem_access, use_hardlinks, hardlink_base_dir, hardlink_dir_preset, use_reflinks, fallback_to_regular_mode
		FROM instances_view
		ORDER BY sort_order ASC, %s ASC, id ASC
	`, orderByName)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*Instance
	for rows.Next() {
		var id int
		var name, host, username, passwordEncrypted string
		var basicUsername, basicPasswordEncrypted sql.NullString
		var tlsSkipVerify bool
		var sortOrder int
		var isActive bool
		var hasLocalFilesystemAccess bool
		var useHardlinks bool
		var hardlinkBaseDir, hardlinkDirPreset string
		var useReflinks bool
		var fallbackToRegularMode bool

		err := rows.Scan(
			&id,
			&name,
			&host,
			&username,
			&passwordEncrypted,
			&basicUsername,
			&basicPasswordEncrypted,
			&tlsSkipVerify,
			&sortOrder,
			&isActive,
			&hasLocalFilesystemAccess,
			&useHardlinks,
			&hardlinkBaseDir,
			&hardlinkDirPreset,
			&useReflinks,
			&fallbackToRegularMode,
		)
		if err != nil {
			return nil, err
		}

		instance := &Instance{
			ID:                       id,
			Name:                     name,
			Host:                     host,
			Username:                 username,
			PasswordEncrypted:        passwordEncrypted,
			TLSSkipVerify:            tlsSkipVerify,
			SortOrder:                sortOrder,
			IsActive:                 isActive,
			HasLocalFilesystemAccess: hasLocalFilesystemAccess,
			UseHardlinks:             useHardlinks,
			HardlinkBaseDir:          hardlinkBaseDir,
			HardlinkDirPreset:        hardlinkDirPreset,
			UseReflinks:              useReflinks,
			FallbackToRegularMode:    fallbackToRegularMode,
		}

		if basicUsername.Valid {
			instance.BasicUsername = &basicUsername.String
		}
		if basicPasswordEncrypted.Valid {
			instance.BasicPasswordEncrypted = &basicPasswordEncrypted.String
		}

		instances = append(instances, instance)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return instances, nil
}

// InstanceUpdateParams contains optional fields for updating an instance.
type InstanceUpdateParams struct {
	TLSSkipVerify            *bool
	HasLocalFilesystemAccess *bool
	UseHardlinks             *bool
	HardlinkBaseDir          *string
	HardlinkDirPreset        *string
	UseReflinks              *bool
	FallbackToRegularMode    *bool
}

func (s *InstanceStore) Update(ctx context.Context, id int, name, rawHost, username, password string, basicUsername, basicPassword *string, params *InstanceUpdateParams) (*Instance, error) {
	// Validate and normalize the host
	normalizedHost, err := validateAndNormalizeHost(rawHost)
	if err != nil {
		return nil, err
	}

	// Start a transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare strings to intern - always intern name, host, username
	// Also intern basic_username if it's provided and not empty
	var basicUsernameToIntern *string
	if basicUsername != nil && *basicUsername != "" {
		basicUsernameToIntern = basicUsername
	}

	// Intern all strings in a single call
	allIDs, err := dbinterface.InternStringNullable(ctx, tx, &name, &normalizedHost, &username, basicUsernameToIntern)
	if err != nil {
		return nil, fmt.Errorf("failed to intern strings: %w", err)
	}

	// Ensure required fields (name, host, username) have valid IDs
	// If username is empty (localhost bypass), intern the empty string
	nameID := allIDs[0].Int64
	hostID := allIDs[1].Int64

	var usernameID int64
	if allIDs[2].Valid {
		usernameID = allIDs[2].Int64
	} else {
		// Username was empty - intern empty string for localhost bypass auth
		usernameID, err = dbinterface.InternEmptyString(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("failed to intern empty username: %w", err)
		}
	}

	// Build UPDATE query
	query := "UPDATE instances SET name_id = ?, host_id = ?, username_id = ?"
	args := []any{nameID, hostID, usernameID}

	// Handle basic_username update
	if basicUsername != nil {
		if *basicUsername == "" {
			// Empty string explicitly provided - clear the basic username
			query += ", basic_username_id = NULL"
		} else {
			// Basic username provided - use the already interned ID
			query += ", basic_username_id = ?"
			args = append(args, allIDs[3])
		}
	}

	// Handle password update - encrypt if provided
	passwordToStore := password
	if username == "" {
		passwordToStore = ""
	}
	if passwordToStore != "" || username == "" {
		encryptedPassword, err := s.encrypt(passwordToStore)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
		query += ", password_encrypted = ?"
		args = append(args, encryptedPassword)
	}

	// Handle basic password update
	if basicPassword != nil {
		if *basicPassword == "" {
			// Empty string explicitly provided - clear the basic password
			query += ", basic_password_encrypted = NULL"
		} else {
			// Basic password provided - encrypt and update
			encryptedBasicPassword, err := s.encrypt(*basicPassword)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt basic auth password: %w", err)
			}
			query += ", basic_password_encrypted = ?"
			args = append(args, encryptedBasicPassword)
		}
	}

	if params != nil {
		if params.TLSSkipVerify != nil {
			query += ", tls_skip_verify = ?"
			args = append(args, *params.TLSSkipVerify)
		}

		if params.HasLocalFilesystemAccess != nil {
			query += ", has_local_filesystem_access = ?"
			args = append(args, *params.HasLocalFilesystemAccess)
		}

		if params.UseHardlinks != nil {
			query += ", use_hardlinks = ?"
			args = append(args, *params.UseHardlinks)
		}

		if params.HardlinkBaseDir != nil {
			query += ", hardlink_base_dir = ?"
			args = append(args, *params.HardlinkBaseDir)
		}

		if params.HardlinkDirPreset != nil {
			query += ", hardlink_dir_preset = ?"
			args = append(args, *params.HardlinkDirPreset)
		}

		if params.UseReflinks != nil {
			query += ", use_reflinks = ?"
			args = append(args, *params.UseReflinks)
		}

		if params.FallbackToRegularMode != nil {
			query += ", fallback_to_regular_mode = ?"
			args = append(args, *params.FallbackToRegularMode)
		}
	}

	query += " WHERE id = ?"
	args = append(args, id)

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	if rows == 0 {
		return nil, ErrInstanceNotFound
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return s.Get(ctx, id)
}

func (s *InstanceStore) SetActiveState(ctx context.Context, id int, active bool) (*Instance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE instances SET is_active = ? WHERE id = ?`, active, id)
	if err != nil {
		return nil, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	if rows == 0 {
		return nil, ErrInstanceNotFound
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return s.Get(ctx, id)
}

func (s *InstanceStore) UpdateOrder(ctx context.Context, instanceIDs []int) error {
	if len(instanceIDs) == 0 {
		return errors.New("instance ids cannot be empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var totalInstances int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM instances").Scan(&totalInstances); err != nil {
		return fmt.Errorf("failed to validate instance list: %w", err)
	}
	if len(instanceIDs) != totalInstances {
		return fmt.Errorf("partial reordering not allowed: expected %d instances, got %d", totalInstances, len(instanceIDs))
	}

	seen := make(map[int]struct{}, len(instanceIDs))
	updateQuery := `UPDATE instances SET sort_order = ? WHERE id = ?`
	for order, id := range instanceIDs {
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate instance id %d in reorder payload", id)
		}
		seen[id] = struct{}{}

		result, err := tx.ExecContext(ctx, updateQuery, order, id)
		if err != nil {
			return err
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if rows != 1 {
			return ErrInstanceNotFound
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *InstanceStore) Delete(ctx context.Context, id int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	query := `DELETE FROM instances WHERE id = ?`

	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrInstanceNotFound
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetDecryptedPassword returns the decrypted password for an instance
func (s *InstanceStore) GetDecryptedPassword(instance *Instance) (string, error) {
	return s.decrypt(instance.PasswordEncrypted)
}

// GetDecryptedBasicPassword returns the decrypted basic auth password for an instance
func (s *InstanceStore) GetDecryptedBasicPassword(instance *Instance) (*string, error) {
	if instance.BasicPasswordEncrypted == nil {
		return nil, nil
	}
	decrypted, err := s.decrypt(*instance.BasicPasswordEncrypted)
	if err != nil {
		return nil, err
	}
	return &decrypted, nil
}
