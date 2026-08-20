// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SSH credential ciphertexts are bound to the row they belong to via the
// AEAD's additional data, so a credential blob copied onto another instance —
// or a pin kept while the host column is rewritten underneath it — fails to
// decrypt instead of quietly authorising a connection nobody configured.
// The scope is a database-write attacker, which is a realistic position when
// Postgres runs on a different host than qui and its session secret.
const (
	aadFieldSSHKey     = "ssh_key"
	aadFieldSSHHostKey = "ssh_host_key"
)

var ErrSSHHostKeyNotPinned = errors.New("ssh host key is not pinned")

func sshKeyAAD(instanceID int) []byte {
	return []byte(strconv.Itoa(instanceID) + "|" + aadFieldSSHKey)
}

// hostKeyPinAAD binds the pin to the endpoint it was confirmed for. Redirecting
// an instance by editing ssh_host or ssh_port then reads as a decryption
// failure — unambiguous tampering — rather than as a host-key mismatch, which
// is also what a legitimate re-key looks like.
func hostKeyPinAAD(instanceID int, host string, port int) []byte {
	return []byte(strconv.Itoa(instanceID) + "|" + aadFieldSSHHostKey + "|" + host + "|" + strconv.Itoa(port))
}

// SetSSHCredentials stores the SSH endpoint and private key for an instance.
// Changing the host or port drops any existing pin: the pin was confirmed for
// one endpoint, and the new one has never been seen before.
func (s *InstanceStore) SetSSHCredentials(ctx context.Context, instanceID int, host string, port int, username, privateKey string) error {
	host = strings.TrimSpace(host)
	username = strings.TrimSpace(username)

	switch {
	case host == "":
		return errors.New("ssh host is required")
	case port < 1 || port > 65535:
		return fmt.Errorf("ssh port %d out of range", port)
	case username == "":
		return errors.New("ssh username is required")
	case privateKey == "":
		return errors.New("ssh private key is required")
	}

	encryptedKey, err := s.encryptWithAAD(privateKey, sshKeyAAD(instanceID))
	if err != nil {
		return fmt.Errorf("encrypt ssh key: %w", err)
	}

	query := `
		UPDATE instances
		SET ssh_host = ?, ssh_port = ?, ssh_username = ?, ssh_key_encrypted = ?,
		    ssh_host_key_encrypted = CASE WHEN ssh_host = ? AND ssh_port = ? THEN ssh_host_key_encrypted ELSE '' END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`
	return s.execInstanceUpdate(ctx, query, host, port, username, encryptedKey, host, port, instanceID)
}

// ClearSSHCredentials removes the credentials but keeps the pin: the pin
// belongs to the host, not to whoever last authenticated against it.
func (s *InstanceStore) ClearSSHCredentials(ctx context.Context, instanceID int) error {
	query := `
		UPDATE instances
		SET ssh_username = '', ssh_key_encrypted = '', updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`
	return s.execInstanceUpdate(ctx, query, instanceID)
}

// SetHostKeyPin pins the marshaled host public key for an instance. The
// endpoint comes from the stored row rather than the caller so the pin can
// never be bound to a host the instance is not actually configured for.
func (s *InstanceStore) SetHostKeyPin(ctx context.Context, instanceID int, marshaledKey []byte) error {
	if len(marshaledKey) == 0 {
		return errors.New("host key is empty")
	}

	instance, err := s.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	if instance.SSHHost == "" {
		return errors.New("instance has no ssh host configured")
	}

	encrypted, err := s.encryptWithAAD(string(marshaledKey), hostKeyPinAAD(instanceID, instance.SSHHost, instance.SSHPort))
	if err != nil {
		return fmt.Errorf("encrypt host key pin: %w", err)
	}

	query := `UPDATE instances SET ssh_host_key_encrypted = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	return s.execInstanceUpdate(ctx, query, encrypted, instanceID)
}

// GetDecryptedSSHKey returns the private key for an instance, or "" when no
// key is configured.
func (s *InstanceStore) GetDecryptedSSHKey(instance *Instance) (string, error) {
	if instance.SSHKeyEncrypted == "" {
		return "", nil
	}

	return s.decryptWithAAD(instance.SSHKeyEncrypted, sshKeyAAD(instance.ID))
}

// GetHostKeyPin returns the marshaled host public key confirmed for this
// instance. A decryption failure is returned as-is and never degrades to
// "unpinned": falling back to trust-on-first-use here would hand an attacker
// who can write the database exactly the re-pin the mismatch flow refuses.
func (s *InstanceStore) GetHostKeyPin(instance *Instance) ([]byte, error) {
	if instance.SSHHostKeyEncrypted == "" {
		return nil, ErrSSHHostKeyNotPinned
	}

	pin, err := s.decryptWithAAD(instance.SSHHostKeyEncrypted, hostKeyPinAAD(instance.ID, instance.SSHHost, instance.SSHPort))
	if err != nil {
		return nil, fmt.Errorf("decrypt host key pin: %w", err)
	}

	return []byte(pin), nil
}

func (s *InstanceStore) execInstanceUpdate(ctx context.Context, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
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

	return nil
}
