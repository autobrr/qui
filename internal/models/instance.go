// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"
)

var ErrInstanceNotFound = errors.New("instance not found")

type Instance struct {
	ID                     int        `json:"id"`
	Name                   string     `json:"name"`
	Host                   string     `json:"host"`
	Port                   int        `json:"port"`
	Username               string     `json:"username"`
	PasswordEncrypted      string     `json:"-"`
	BasicUsername          *string    `json:"basic_username,omitempty"`
	BasicPasswordEncrypted *string    `json:"-"`
	IsActive               bool       `json:"is_active"`
	LastConnectedAt        *time.Time `json:"last_connected_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
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

func (s *InstanceStore) Create(name, host string, port int, username, password string, basicUsername, basicPassword *string) (*Instance, error) {
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
		INSERT INTO instances (name, host, port, username, password_encrypted, basic_username, basic_password_encrypted) 
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id, name, host, port, username, password_encrypted, basic_username, basic_password_encrypted, is_active, last_connected_at, created_at, updated_at
	`

	instance := &Instance{}
	err = s.db.QueryRow(query, name, host, port, username, encryptedPassword, basicUsername, encryptedBasicPassword).Scan(
		&instance.ID,
		&instance.Name,
		&instance.Host,
		&instance.Port,
		&instance.Username,
		&instance.PasswordEncrypted,
		&instance.BasicUsername,
		&instance.BasicPasswordEncrypted,
		&instance.IsActive,
		&instance.LastConnectedAt,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return instance, nil
}

func (s *InstanceStore) Get(id int) (*Instance, error) {
	query := `
		SELECT id, name, host, port, username, password_encrypted, basic_username, basic_password_encrypted, is_active, last_connected_at, created_at, updated_at 
		FROM instances 
		WHERE id = ?
	`

	instance := &Instance{}
	err := s.db.QueryRow(query, id).Scan(
		&instance.ID,
		&instance.Name,
		&instance.Host,
		&instance.Port,
		&instance.Username,
		&instance.PasswordEncrypted,
		&instance.BasicUsername,
		&instance.BasicPasswordEncrypted,
		&instance.IsActive,
		&instance.LastConnectedAt,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrInstanceNotFound
	}
	if err != nil {
		return nil, err
	}

	return instance, nil
}

func (s *InstanceStore) List(activeOnly bool) ([]*Instance, error) {
	query := `
		SELECT id, name, host, port, username, password_encrypted, basic_username, basic_password_encrypted, is_active, last_connected_at, created_at, updated_at 
		FROM instances
	`

	if activeOnly {
		query += " WHERE is_active = 1"
	}

	query += " ORDER BY name ASC"

	rows, err := s.db.Query(query)
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
			&instance.Port,
			&instance.Username,
			&instance.PasswordEncrypted,
			&instance.BasicUsername,
			&instance.BasicPasswordEncrypted,
			&instance.IsActive,
			&instance.LastConnectedAt,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

func (s *InstanceStore) Update(id int, name, host string, port int, username, password string, basicUsername, basicPassword *string) (*Instance, error) {
	// Start building the update query
	query := `UPDATE instances SET name = ?, host = ?, port = ?, username = ?, basic_username = ?`
	args := []interface{}{name, host, port, username, basicUsername}

	// Only update password if provided
	if password != "" {
		encryptedPassword, err := s.encrypt(password)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
		query += ", password_encrypted = ?"
		args = append(args, encryptedPassword)
	}

	// Only update basic password if provided
	if basicPassword != nil && *basicPassword != "" {
		encryptedBasicPassword, err := s.encrypt(*basicPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt basic auth password: %w", err)
		}
		query += ", basic_password_encrypted = ?"
		args = append(args, encryptedBasicPassword)
	} else if basicPassword != nil && *basicPassword == "" {
		// Clear basic password if empty string provided
		query += ", basic_password_encrypted = NULL"
	}

	query += " WHERE id = ?"
	args = append(args, id)

	result, err := s.db.Exec(query, args...)
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

	return s.Get(id)
}

func (s *InstanceStore) UpdateActive(id int, isActive bool) error {
	query := `UPDATE instances SET is_active = ? WHERE id = ?`

	result, err := s.db.Exec(query, isActive, id)
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

func (s *InstanceStore) UpdateLastConnected(id int) error {
	query := `UPDATE instances SET last_connected_at = CURRENT_TIMESTAMP WHERE id = ?`

	result, err := s.db.Exec(query, id)
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

func (s *InstanceStore) Delete(id int) error {
	query := `DELETE FROM instances WHERE id = ?`

	result, err := s.db.Exec(query, id)
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
