// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// dialTimeout is the TCP connection timeout for SSH.
	dialTimeout = 15 * time.Second
)

// tofuHostKeyCallback returns an ssh.HostKeyCallback that implements TOFU
// (Trust On First Use). On first connect (knownKey is empty), it captures the
// host key by calling onFirstSeen. On subsequent connects, it verifies the
// presented key matches the known key.
//
// This NEVER defaults to insecure — if knownKey is empty and onFirstSeen is nil,
// the callback rejects the connection.
func tofuHostKeyCallback(knownKey string, onFirstSeen func(key ssh.PublicKey) error) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		fingerprint := ssh.FingerprintSHA256(key)

		if knownKey == "" {
			// First connection — capture the key.
			if onFirstSeen == nil {
				return fmt.Errorf("host key not known for %s (fingerprint: %s) and no callback to capture it", hostname, fingerprint)
			}
			return onFirstSeen(key)
		}

		// Subsequent connection — verify the key matches.
		if fingerprint != knownKey {
			return fmt.Errorf("host key changed for %s: expected %s, got %s", hostname, knownKey, fingerprint)
		}
		return nil
	}
}

// buildSSHConfig creates an ssh.ClientConfig from instance SSH credentials.
// The key material is already decrypted by the caller. Credentials are NEVER logged.
func buildSSHConfig(username string, authType string, privateKey []byte, passphrase []byte, password string, hostKeyCallback ssh.HostKeyCallback) (*ssh.ClientConfig, error) {
	config := &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: hostKeyCallback,
		Timeout:         dialTimeout,
	}

	switch authType {
	case "key":
		var signer ssh.Signer
		var err error
		if len(passphrase) > 0 {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(privateKey, passphrase)
		} else {
			signer, err = ssh.ParsePrivateKey(privateKey)
		}
		if err != nil {
			return nil, fmt.Errorf("parse SSH private key: %w", err)
		}
		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}

	case "password":
		config.Auth = []ssh.AuthMethod{ssh.Password(password)}

	default:
		return nil, fmt.Errorf("unsupported SSH auth type: %q", authType)
	}

	return config, nil
}
