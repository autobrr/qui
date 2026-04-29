// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestTofuHostKeyCallback_RejectsWhenNoKnownKeyAndNoCallback(t *testing.T) {
	cb := tofuHostKeyCallback("", nil)
	// Generate a dummy key for testing.
	_, privKey, err := generateTestKey()
	require.NoError(t, err)

	err = cb("seedbox", nil, privKey.PublicKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host key not known")
}

func TestTofuHostKeyCallback_CapturesOnFirstSeen(t *testing.T) {
	var captured ssh.PublicKey
	cb := tofuHostKeyCallback("", func(key ssh.PublicKey) error {
		captured = key
		return nil
	})

	_, privKey, err := generateTestKey()
	require.NoError(t, err)

	err = cb("seedbox", nil, privKey.PublicKey())
	require.NoError(t, err)
	assert.NotNil(t, captured)
}

func TestTofuHostKeyCallback_AcceptsMatchingKey(t *testing.T) {
	_, privKey, err := generateTestKey()
	require.NoError(t, err)

	fingerprint := ssh.FingerprintSHA256(privKey.PublicKey())
	cb := tofuHostKeyCallback(fingerprint, nil)

	err = cb("seedbox", nil, privKey.PublicKey())
	require.NoError(t, err)
}

func TestTofuHostKeyCallback_RejectsChangedKey(t *testing.T) {
	_, privKey1, err := generateTestKey()
	require.NoError(t, err)
	_, privKey2, err := generateTestKey()
	require.NoError(t, err)

	fingerprint1 := ssh.FingerprintSHA256(privKey1.PublicKey())
	cb := tofuHostKeyCallback(fingerprint1, nil)

	err = cb("seedbox", nil, privKey2.PublicKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host key changed")
}

func TestBuildSSHConfig_KeyAuth(t *testing.T) {
	pubKey, privKey, err := generateTestKey()
	require.NoError(t, err)
	_ = pubKey

	privBytes := ssh.MarshalAuthorizedKey(privKey.PublicKey())
	// We can't easily get PEM from ssh.Signer, so test with a generated key.
	// Just verify the function rejects invalid key material.
	_, err = buildSSHConfig("user", "key", []byte("invalid-key"), nil, "", ssh.InsecureIgnoreHostKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse SSH private key")

	_ = privBytes // unused in this test variant
}

func TestBuildSSHConfig_PasswordAuth(t *testing.T) {
	config, err := buildSSHConfig("user", "password", nil, nil, "secret", ssh.InsecureIgnoreHostKey())
	require.NoError(t, err)
	assert.Equal(t, "user", config.User)
	assert.Len(t, config.Auth, 1)
}

func TestBuildSSHConfig_UnsupportedAuthType(t *testing.T) {
	_, err := buildSSHConfig("user", "certificate", nil, nil, "", ssh.InsecureIgnoreHostKey())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported SSH auth type")
}

// generateTestKey creates a unique test Ed25519 SSH key pair.
func generateTestKey() (ssh.PublicKey, ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return signer.PublicKey(), signer, nil
}
