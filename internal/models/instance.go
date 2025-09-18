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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/domain"
)

var ErrInstanceNotFound = errors.New("instance not found")

type Instance struct {
	ID                     int     `json:"id"`
	Name                   string  `json:"name"`
	Host                   string  `json:"host"`
	Username               string  `json:"username"`
	PasswordEncrypted      string  `json:"-"`
	BasicUsername          *string `json:"basic_username,omitempty"`
	BasicPasswordEncrypted *string `json:"-"`
}

func (i Instance) MarshalJSON() ([]byte, error) {
	// Create the JSON structure with redacted password fields
	return json.Marshal(&struct {
		ID              int        `json:"id"`
		Name            string     `json:"name"`
		Host            string     `json:"host"`
		Username        string     `json:"username"`
		Password        string     `json:"password,omitempty"`
		BasicUsername   *string    `json:"basic_username,omitempty"`
		BasicPassword   string     `json:"basic_password,omitempty"`
		IsActive        bool       `json:"is_active"`
		LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
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
	})
}

func (i *Instance) UnmarshalJSON(data []byte) error {
	// Temporary struct for unmarshaling
	var temp struct {
		ID              int        `json:"id"`
		Name            string     `json:"name"`
		Host            string     `json:"host"`
		Username        string     `json:"username"`
		Password        string     `json:"password,omitempty"`
		BasicUsername   *string    `json:"basic_username,omitempty"`
		BasicPassword   string     `json:"basic_password,omitempty"`
		IsActive        bool       `json:"is_active"`
		LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
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
	db            *sql.DB
	encryptionKey []byte
}

func NewInstanceStore(db *sql.DB, encryptionKey []byte) (*InstanceStore, error) {
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

func (s *InstanceStore) Create(ctx context.Context, name, rawHost, username, password string, basicUsername, basicPassword *string) (*Instance, error) {
	// Validate and normalize the host
	normalizedHost, err := validateAndNormalizeHost(rawHost)
	if err != nil {
		return nil, err
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

	query := `
		INSERT INTO instances (name, host, username, password_encrypted, basic_username, basic_password_encrypted) 
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, name, host, username, password_encrypted, basic_username, basic_password_encrypted
	`

	instance := &Instance{}
	err = s.db.QueryRowContext(ctx, query, name, normalizedHost, username, encryptedPassword, basicUsername, encryptedBasicPassword).Scan(
		&instance.ID,
		&instance.Name,
		&instance.Host,
		&instance.Username,
		&instance.PasswordEncrypted,
		&instance.BasicUsername,
		&instance.BasicPasswordEncrypted,
	)

	if err != nil {
		return nil, err
	}

	return instance, nil
}

func (s *InstanceStore) Get(ctx context.Context, id int) (*Instance, error) {
	query := `
		SELECT id, name, host, username, password_encrypted, basic_username, basic_password_encrypted 
		FROM instances 
		WHERE id = ?
	`

	instance := &Instance{}
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&instance.ID,
		&instance.Name,
		&instance.Host,
		&instance.Username,
		&instance.PasswordEncrypted,
		&instance.BasicUsername,
		&instance.BasicPasswordEncrypted,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInstanceNotFound
		}
		return nil, err
	}

	return instance, nil
}

func (s *InstanceStore) List(ctx context.Context) ([]*Instance, error) {
	query := `
		SELECT id, name, host, username, password_encrypted, basic_username, basic_password_encrypted 
		FROM instances
		ORDER BY name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []*Instance
	for rows.Next() {
		instance := &Instance{}
		err := rows.Scan(
			&instance.ID,
			&instance.Name,
			&instance.Host,
			&instance.Username,
			&instance.PasswordEncrypted,
			&instance.BasicUsername,
			&instance.BasicPasswordEncrypted,
		)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

func (s *InstanceStore) Update(ctx context.Context, id int, name, rawHost, username, password string, basicUsername, basicPassword *string) (*Instance, error) {
	// Validate and normalize the host
	normalizedHost, err := validateAndNormalizeHost(rawHost)
	if err != nil {
		return nil, err
	}

	// Start building the update query
	query := `UPDATE instances SET name = ?, host = ?, username = ?, basic_username = ?`
	args := []any{name, normalizedHost, username, basicUsername}

	// Handle password update - encrypt if provided
	if password != "" {
		encryptedPassword, err := s.encrypt(password)
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

	query += " WHERE id = ?"
	args = append(args, id)

	result, err := s.db.ExecContext(ctx, query, args...)
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

	return s.Get(ctx, id)
}

func (s *InstanceStore) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM instances WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
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
