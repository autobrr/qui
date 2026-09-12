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

var (
	ErrSSHHostKeyNotPinned     = errors.New("ssh host key is not pinned")
	ErrSSHEndpointChanged      = errors.New("ssh endpoint changed while pinning the host key")
	ErrSSHHostKeyAlreadyPinned = errors.New("ssh host key is already pinned")
)

func sshKeyAAD(instanceID int) []byte {
	return []byte(strconv.Itoa(instanceID) + "|" + aadFieldSSHKey)
}

// hostKeyPinAAD binds the pin to the endpoint it was confirmed for. Redirecting
// an instance by editing ssh_host or ssh_port then reads as a decryption
// failure — unambiguous tampering — rather than as a host-key mismatch, which
// is also what a legitimate re-key looks like. The decimal port goes last so
// the final "|" stays unambiguous whatever the host contains; add new parts
// after it.
func hostKeyPinAAD(instanceID int, host string, port int) []byte {
	return []byte(strconv.Itoa(instanceID) + "|" + aadFieldSSHHostKey + "|" + host + "|" + strconv.Itoa(port))
}

// SetSSHCredentials stores the SSH endpoint and private key for an instance.
// Changing the host or port drops any existing pin: the pin was confirmed for
// one endpoint, and the new one has never been seen before.
func (s *InstanceStore) SetSSHCredentials(ctx context.Context, instanceID int, host string, port int, username, privateKey string) error {
	host, err := normalizeSSHHost(host)
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)

	switch {
	case port < 1 || port > 65535:
		return fmt.Errorf("ssh port %d out of range", port)
	case username == "":
		return errors.New("ssh username is required")
	case privateKey == "":
		return errors.New("ssh private key is required")
	}

	// Reject at write time what the dial would only discover later: passphrase
	// protected keys are not supported, and an unparseable key is never going
	// to authenticate.
	if _, err := ssh.ParseRawPrivateKey([]byte(privateKey)); err != nil {
		if _, ok := errors.AsType[*ssh.PassphraseMissingError](err); ok {
			return errors.New("passphrase-protected ssh keys are not supported: provide a key without a passphrase")
		}
		return fmt.Errorf("parse ssh private key: %w", err)
	}

	encryptedKey, err := s.cipher.Encrypt(privateKey, sshKeyAAD(instanceID))
	if err != nil {
		return fmt.Errorf("encrypt ssh key: %w", err)
	}

	query := `
		UPDATE instances
		SET ssh_host = ?, ssh_port = ?, ssh_username = ?, ssh_key_encrypted = ?,
		    ssh_host_key_encrypted = CASE WHEN ssh_host = ? AND ssh_port = ? THEN ssh_host_key_encrypted ELSE '' END
		WHERE id = ?
	`
	return s.execInstanceUpdate(ctx, ErrInstanceNotFound, query, host, port, username, encryptedKey, host, port, instanceID)
}

// normalizeSSHHost returns the one stored form of a host so that a cosmetic
// re-save ("Example.com", "fd00:0:0:0:0:0:0:1") is not an endpoint change that
// drops a good pin. IP literals take netip's canonical text, which keeps an
// IPv6 zone's case: interface names match exactly. The literal is stored
// unbracketed and net.JoinHostPort brackets it at dial time.
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

// ClearSSHCredentials removes the credentials but keeps the pin: the pin
// belongs to the host, not to whoever last authenticated against it.
func (s *InstanceStore) ClearSSHCredentials(ctx context.Context, instanceID int) error {
	query := `
		UPDATE instances
		SET ssh_username = '', ssh_key_encrypted = ''
		WHERE id = ?
	`
	return s.execInstanceUpdate(ctx, ErrInstanceNotFound, query, instanceID)
}

// SetHostKeyPin pins the marshaled host public key the caller confirmed for
// host and port. The endpoint comes from the caller, not the stored row: a key
// confirmed for one host must not be pinned to whatever host the row carries by
// the time the confirmation lands, so a row whose endpoint no longer matches is
// refused with ErrSSHEndpointChanged.
//
// Pinning an already-pinned instance is refused: overwriting a live pin is
// exactly the silent re-pin the mismatch flow exists to prevent, so replacing
// one has to be asked for by name rather than fallen into.
func (s *InstanceStore) SetHostKeyPin(ctx context.Context, instanceID int, host string, port int, marshaledKey []byte) error {
	if err := validateMarshaledHostKey(marshaledKey); err != nil {
		return err
	}
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

	return s.setHostKeyPinFor(ctx, instanceID, host, port, marshaledKey)
}

// validateMarshaledHostKey enforces that the column only ever holds SSH wire
// format. Host key verification pins the algorithm by parsing it back out of
// this value, so a display-form key ("ssh-ed25519 AAAA...") stored here would
// be unusable at dial time with nothing pointing at why.
func validateMarshaledHostKey(marshaledKey []byte) error {
	if len(marshaledKey) == 0 {
		return errors.New("host key is empty")
	}
	if _, err := ssh.ParsePublicKey(marshaledKey); err != nil {
		return fmt.Errorf("host key is not in ssh wire format: %w", err)
	}
	return nil
}

// setHostKeyPinFor is a compare-and-set on the endpoint the AAD was built from:
// an interleaved credential update would otherwise leave a pin that can never
// decrypt again, with nothing pointing at why.
func (s *InstanceStore) setHostKeyPinFor(ctx context.Context, instanceID int, host string, port int, marshaledKey []byte) error {
	encrypted, err := s.cipher.Encrypt(string(marshaledKey), hostKeyPinAAD(instanceID, host, port))
	if err != nil {
		return fmt.Errorf("encrypt host key pin: %w", err)
	}

	query := `
		UPDATE instances
		SET ssh_host_key_encrypted = ?
		WHERE id = ? AND ssh_host = ? AND ssh_port = ? AND ssh_host_key_encrypted = ''
	`
	return s.execInstanceUpdate(ctx, ErrSSHEndpointChanged, query, encrypted, instanceID, host, port)
}

// GetDecryptedSSHKey returns the private key for an instance, or "" when no
// key is configured.
func (s *InstanceStore) GetDecryptedSSHKey(instance *Instance) (string, error) {
	if instance.SSHKeyEncrypted == "" {
		return "", nil
	}

	return s.cipher.Decrypt(instance.SSHKeyEncrypted, sshKeyAAD(instance.ID))
}

// GetHostKeyPin returns the marshaled host public key confirmed for this
// instance. A decryption failure is returned as-is and never degrades to
// "unpinned": falling back to trust-on-first-use here would hand an attacker
// who can write the database exactly the re-pin the mismatch flow refuses.
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

// execInstanceUpdate runs a single-row UPDATE and returns noRow when the WHERE
// clause matched nothing.
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
