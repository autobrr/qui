// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sshtest generates the SSH key material and credential key that the
// models and database tests both need. The generators panic instead of taking
// a *testing.T so package-level vars can call them.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"

	"golang.org/x/crypto/ssh"
)

// PrivateKey returns a PEM-encoded ed25519 private key, passphrase-protected
// when passphrase is non-empty. Real key material: the store parses what it is
// handed, so a placeholder string would only prove the fixtures match.
func PrivateKey(passphrase string) string {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		panic(err)
	}

	return string(pem.EncodeToMemory(block))
}

// HostKey returns an ed25519 public key in SSH wire format, the form the pin
// column holds.
func HostKey() []byte {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		panic(err)
	}

	return sshPub.Marshal()
}

// EncryptionKey returns the deterministic 32-byte key the credential store
// tests seal with.
func EncryptionKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}
