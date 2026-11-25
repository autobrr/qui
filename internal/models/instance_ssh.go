// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"
)

const (
	aadFieldSSHKey     = "ssh_key"
	aadFieldSSHHostKey = "ssh_host_key"
)

var (
	ErrSSHHostKeyNotPinned     = errors.New("ssh host key is not pinned")
	ErrSSHEndpointChanged      = errors.New("ssh endpoint changed while pinning the host key")
	ErrSSHHostKeyAlreadyPinned = errors.New("ssh host key is already pinned")
	ErrSSHKeyNotConfigured     = errors.New("ssh private key is not configured")
	ErrInvalidSSHCredentials   = errors.New("invalid ssh credentials")
)

func sshKeyAAD(instanceID int) []byte {
	return []byte(strconv.Itoa(instanceID) + "|" + aadFieldSSHKey)
}

func hostKeyPinAAD(instanceID int, host string, port int) []byte {
	return []byte(strconv.Itoa(instanceID) + "|" + aadFieldSSHHostKey + "|" + host + "|" + strconv.Itoa(port))
}

func (s *InstanceStore) SetSSHCredentials(ctx context.Context, instanceID int, host string, port int, username, privateKey string) error {
	host, username, err := validateSSHCredentials(host, port, username, privateKey)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSSHCredentials, err)
	}

	encryptedKey, err := s.cipher.Encrypt(privateKey, sshKeyAAD(instanceID))
	if err != nil {
		return fmt.Errorf("encrypt ssh key: %w", err)
	}

	query := `
		UPDATE instances
		SET ssh_host = ?, ssh_port = ?, ssh_username = ?, ssh_key_encrypted = ?,
		    ssh_host_key_encrypted = CASE WHEN ssh_host = ? AND ssh_port = ? THEN ssh_host_key_encrypted ELSE '' END,
		    ssh_tier = ?, ssh_hardlink = NULL
		WHERE id = ?
	`
	return s.execInstanceUpdate(ctx, ErrInstanceNotFound, query, host, port, username, encryptedKey, host, port, string(SSHTierSFTP), instanceID)
}

func validateSSHCredentials(host string, port int, username, privateKey string) (normalizedHost, trimmedUsername string, err error) {
	normalizedHost, err = normalizeSSHHost(host)
	if err != nil {
		return "", "", err
	}
	trimmedUsername = strings.TrimSpace(username)

	switch {
	case port < 1 || port > 65535:
		return "", "", fmt.Errorf("ssh port %d out of range", port)
	case trimmedUsername == "":
		return "", "", errors.New("ssh username is required")
	case privateKey == "":
		return "", "", errors.New("ssh private key is required")
	}

	if _, err := ssh.ParseRawPrivateKey([]byte(privateKey)); err != nil {
		if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); ok {
			return "", "", errors.New("passphrase-protected ssh keys are not supported: provide a key without a passphrase")
		}
		return "", "", fmt.Errorf("parse ssh private key: %w", err)
	}

	return normalizedHost, trimmedUsername, nil
}

func normalizeSSHHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	switch {
	case host == "":
		return "", errors.New("ssh host is required")
	case strings.ContainsFunc(host, func(r rune) bool { return !unicode.IsPrint(r) }):
		return "", fmt.Errorf("ssh host %q contains non-printable characters", host)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.String(), nil
	}
	host = strings.ToLower(host)
	if strings.ContainsAny(host, "/\\:@ ") {
		return "", fmt.Errorf("ssh host %q must be a bare hostname or IP, without scheme, port or credentials", host)
	}
	return host, nil
}

func SSHEndpointChanged(inst *Instance, host string, port int) bool {
	normalized, err := normalizeSSHHost(host)
	if err != nil {
		return true
	}
	return normalized != inst.SSHHost || port != inst.SSHPort
}

func (s *InstanceStore) ClearSSHCredentials(ctx context.Context, instanceID int) error {
	query := `
		UPDATE instances
		SET ssh_username = '', ssh_key_encrypted = '', ssh_tier = ?, ssh_hardlink = NULL
		WHERE id = ?
	`
	return s.execInstanceUpdate(ctx, ErrInstanceNotFound, query, string(SSHTierSFTP), instanceID)
}

func (s *InstanceStore) SetHostKeyPin(ctx context.Context, instanceID int, host string, port int, marshaledKey []byte) error {
	host, err := normalizeSSHHost(host)
	if err != nil {
		return err
	}

	instance, err := s.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	if instance.SSHHost == "" {
		return errors.New("instance has no ssh host configured")
	}
	if instance.SSHHostKeyEncrypted != "" {
		return ErrSSHHostKeyAlreadyPinned
	}

	return s.setHostKeyPinFor(ctx, instanceID, host, port, marshaledKey, false)
}

func (s *InstanceStore) ReplaceHostKeyPin(ctx context.Context, instanceID int, host string, port int, marshaledKey []byte) error {
	host, err := normalizeSSHHost(host)
	if err != nil {
		return err
	}

	return s.setHostKeyPinFor(ctx, instanceID, host, port, marshaledKey, true)
}

func validateMarshaledHostKey(marshaledKey []byte) error {
	if len(marshaledKey) == 0 {
		return errors.New("host key is empty")
	}
	if _, err := ssh.ParsePublicKey(marshaledKey); err != nil {
		return fmt.Errorf("host key is not in ssh wire format: %w", err)
	}
	return nil
}

func (s *InstanceStore) setHostKeyPinFor(ctx context.Context, instanceID int, host string, port int, marshaledKey []byte, replace bool) error {
	if err := validateMarshaledHostKey(marshaledKey); err != nil {
		return err
	}

	encrypted, err := s.cipher.Encrypt(string(marshaledKey), hostKeyPinAAD(instanceID, host, port))
	if err != nil {
		return fmt.Errorf("encrypt host key pin: %w", err)
	}

	pinnedTerm := "= ''"
	if replace {
		pinnedTerm = "<> ''"
	}
	query := `
		UPDATE instances
		SET ssh_host_key_encrypted = ?, ssh_tier = ?, ssh_hardlink = NULL
		WHERE id = ? AND ssh_host = ? AND ssh_port = ? AND ssh_host_key_encrypted ` + pinnedTerm

	err = s.execInstanceUpdate(ctx, ErrSSHEndpointChanged, query, encrypted, string(SSHTierSFTP), instanceID, host, port)
	if !errors.Is(err, ErrSSHEndpointChanged) {
		return err
	}

	instance, getErr := s.Get(ctx, instanceID)
	switch {
	case getErr != nil:
		return getErr
	case replace && instance.SSHHostKeyEncrypted == "":
		return ErrSSHHostKeyNotPinned
	case !replace && instance.SSHHostKeyEncrypted != "":
		return ErrSSHHostKeyAlreadyPinned
	}
	return ErrSSHEndpointChanged
}

func (s *InstanceStore) GetDecryptedSSHKey(instance *Instance) (string, error) {
	if instance.SSHKeyEncrypted == "" {
		return "", ErrSSHKeyNotConfigured
	}

	return s.cipher.Decrypt(instance.SSHKeyEncrypted, sshKeyAAD(instance.ID))
}

func (s *InstanceStore) GetHostKeyPin(instance *Instance) ([]byte, error) {
	if instance.SSHHostKeyEncrypted == "" {
		return nil, ErrSSHHostKeyNotPinned
	}

	pin, err := s.cipher.Decrypt(instance.SSHHostKeyEncrypted, hostKeyPinAAD(instance.ID, instance.SSHHost, instance.SSHPort))
	if err != nil {
		return nil, fmt.Errorf("decrypt host key pin: %w", err)
	}

	return []byte(pin), nil
}

func (s *InstanceStore) execInstanceUpdate(ctx context.Context, noRow error, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return noRow
	}

	return nil
}

func (s *InstanceStore) SetSSHProbe(ctx context.Context, probed *Instance, tier SSHTier, hardlink bool) error {
	query := `
		UPDATE instances
		SET ssh_tier = COALESCE(NULLIF(?, ''), ssh_tier), ssh_hardlink = ?
		WHERE id = ? AND ssh_host = ? AND ssh_port = ? AND ssh_key_encrypted = ? AND ssh_host_key_encrypted = ?
	`
	err := s.execInstanceUpdate(ctx, ErrSSHEndpointChanged, query, string(tier), hardlink, probed.ID, probed.SSHHost, probed.SSHPort, probed.SSHKeyEncrypted, probed.SSHHostKeyEncrypted)
	if errors.Is(err, ErrSSHEndpointChanged) {
		return nil
	}
	return err
}
