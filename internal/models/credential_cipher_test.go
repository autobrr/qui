// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// legacyEncrypt reproduces the pre-HKDF format, base64 of nonce and ciphertext
// with no prefix and no additional data.
func legacyEncrypt(t *testing.T, key []byte, plaintext string) string {
	t.Helper()

	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)

	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)

	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil))
}

func testKey(t *testing.T, fill byte) []byte {
	t.Helper()

	key := make([]byte, 32)
	for i := range key {
		key[i] = fill + byte(i)
	}
	return key
}

func TestNewCredentialCipherRejectsShortKey(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{name: "nil", key: nil},
		{name: "too_short", key: make([]byte, 16)},
		{name: "too_long", key: make([]byte, 33)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := models.NewCredentialCipher(tt.key)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "32 bytes")
		})
	}

	t.Run("legacy_option_rejects_short_key", func(t *testing.T) {
		_, err := models.NewCredentialCipher(testKey(t, 0), models.WithLegacyEncryptionKey(make([]byte, 8)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "32 bytes")
	})
}

func TestCredentialCipherRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
		aad       []byte
	}{
		{name: "password", plaintext: "correct horse battery staple"},
		{name: "api_key", plaintext: strings.Repeat("k", 64)},
		{name: "empty", plaintext: ""},
		{name: "with_aad", plaintext: "ssh private key", aad: []byte("instance:7:ssh_private_key")},
		{name: "unicode", plaintext: "pässwörd ✓"},
	}

	cipherUnderTest, err := models.NewCredentialCipher(testKey(t, 0))
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealed, err := cipherUnderTest.Encrypt(tt.plaintext, tt.aad)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(sealed, "qui2:"), "new writes carry the version prefix")

			opened, err := cipherUnderTest.Decrypt(sealed, tt.aad)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, opened)
		})
	}

	t.Run("nonce_is_fresh_per_call", func(t *testing.T) {
		first, err := cipherUnderTest.Encrypt("same", nil)
		require.NoError(t, err)
		second, err := cipherUnderTest.Encrypt("same", nil)
		require.NoError(t, err)

		assert.NotEqual(t, first, second)
	})
}

func TestCredentialCipherDecryptRejects(t *testing.T) {
	cipherUnderTest, err := models.NewCredentialCipher(testKey(t, 0))
	require.NoError(t, err)

	sealed, err := cipherUnderTest.Encrypt("secret", []byte("bound"))
	require.NoError(t, err)

	// Flip a byte of the sealed payload itself, not of its base64 text, so the
	// case reaches the GCM tag check rather than failing in the decoder.
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "qui2:"))
	require.NoError(t, err)
	payload[len(payload)-1] ^= 0x01
	tampered := "qui2:" + base64.StdEncoding.EncodeToString(payload)

	tests := []struct {
		name       string
		ciphertext string
		aad        []byte
	}{
		{name: "wrong_aad", ciphertext: sealed, aad: []byte("other")},
		{name: "missing_aad", ciphertext: sealed},
		{name: "tampered", ciphertext: tampered, aad: []byte("bound")},
		{name: "not_base64", ciphertext: "qui2:not base64 at all", aad: []byte("bound")},
		{name: "shorter_than_nonce", ciphertext: "qui2:" + base64.StdEncoding.EncodeToString([]byte("short")), aad: []byte("bound")},
		{name: "legacy_without_a_legacy_key", ciphertext: legacyEncrypt(t, testKey(t, 9), "secret")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cipherUnderTest.Decrypt(tt.ciphertext, tt.aad)
			require.Error(t, err)
		})
	}
}

func TestCredentialCipherReadsLegacyValues(t *testing.T) {
	currentKey := testKey(t, 0)
	legacyKey := testKey(t, 100)

	t.Run("legacy_key_configured", func(t *testing.T) {
		cipherUnderTest, err := models.NewCredentialCipher(currentKey, models.WithLegacyEncryptionKey(legacyKey))
		require.NoError(t, err)

		stored := legacyEncrypt(t, legacyKey, "old password")
		require.False(t, strings.HasPrefix(stored, "qui2:"), "the legacy format carries no prefix")

		opened, err := cipherUnderTest.Decrypt(stored, nil)
		require.NoError(t, err)
		assert.Equal(t, "old password", opened)

		resealed, err := cipherUnderTest.Encrypt(opened, nil)
		require.NoError(t, err)
		reopened, err := cipherUnderTest.Decrypt(resealed, nil)
		require.NoError(t, err)
		assert.Equal(t, "old password", reopened)
	})

	// Falling back to the current key here would report a forgotten legacy key
	// as an authentication failure, which is the changed-secret signature.
	t.Run("no_legacy_key_is_an_explicit_error", func(t *testing.T) {
		cipherUnderTest, err := models.NewCredentialCipher(currentKey)
		require.NoError(t, err)

		for name, stored := range map[string]string{
			"sealed_under_the_legacy_key":  legacyEncrypt(t, legacyKey, "old password"),
			"sealed_under_the_current_key": legacyEncrypt(t, currentKey, "same key, still unprefixed"),
		} {
			t.Run(name, func(t *testing.T) {
				_, err := cipherUnderTest.Decrypt(stored, nil)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no legacy key configured")
			})
		}
	})
}
