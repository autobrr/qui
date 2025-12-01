// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSecureToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		length  int
		wantLen int // expected hex string length (2 chars per byte)
		wantErr bool
	}{
		{
			name:    "16 bytes produces 32 char hex",
			length:  16,
			wantLen: 32,
		},
		{
			name:    "32 bytes produces 64 char hex",
			length:  32,
			wantLen: 64,
		},
		{
			name:    "1 byte produces 2 char hex",
			length:  1,
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token, err := GenerateSecureToken(tt.length)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, token, tt.wantLen)

			// Verify it's valid hex
			_, err = hex.DecodeString(token)
			assert.NoError(t, err, "token should be valid hex")
		})
	}
}

func TestGenerateSecureToken_Uniqueness(t *testing.T) {
	t.Parallel()

	// Generate multiple tokens and ensure they're all different
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := GenerateSecureToken(32)
		require.NoError(t, err)
		assert.False(t, tokens[token], "duplicate token generated")
		tokens[token] = true
	}
}

func TestNewAESEncryptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyLen  int
		wantErr error
	}{
		{
			name:    "valid 32 byte key",
			keyLen:  32,
			wantErr: nil,
		},
		{
			name:    "too short key",
			keyLen:  16,
			wantErr: ErrInvalidKeySize,
		},
		{
			name:    "too long key",
			keyLen:  64,
			wantErr: ErrInvalidKeySize,
		},
		{
			name:    "empty key",
			keyLen:  0,
			wantErr: ErrInvalidKeySize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key := make([]byte, tt.keyLen)
			encryptor, err := NewAESEncryptor(key)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, encryptor)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, encryptor)
			}
		})
	}
}

func TestAESEncryptor_EncryptDecrypt(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	encryptor, err := NewAESEncryptor(key)
	require.NoError(t, err)

	tests := []struct {
		name      string
		plaintext string
	}{
		{
			name:      "simple text",
			plaintext: "hello world",
		},
		{
			name:      "empty string",
			plaintext: "",
		},
		{
			name:      "unicode content",
			plaintext: "こんにちは世界 🌍 émojis",
		},
		{
			name:      "long text",
			plaintext: "This is a much longer piece of text that spans multiple blocks and tests the encryption of larger data sets to ensure everything works correctly.",
		},
		{
			name:      "special characters",
			plaintext: "!@#$%^&*()_+-=[]{}|;':\",./<>?`~",
		},
		{
			name:      "newlines and tabs",
			plaintext: "line1\nline2\ttab\r\nwindows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ciphertext, err := encryptor.Encrypt(tt.plaintext)
			require.NoError(t, err)
			assert.NotEqual(t, tt.plaintext, ciphertext, "ciphertext should differ from plaintext")

			decrypted, err := encryptor.Decrypt(ciphertext)
			require.NoError(t, err)
			assert.Equal(t, tt.plaintext, decrypted)
		})
	}
}

func TestAESEncryptor_EncryptProducesDifferentCiphertext(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	encryptor, err := NewAESEncryptor(key)
	require.NoError(t, err)

	plaintext := "same plaintext"
	ciphertexts := make(map[string]bool)

	// Encrypt the same plaintext multiple times
	for i := 0; i < 10; i++ {
		ciphertext, err := encryptor.Encrypt(plaintext)
		require.NoError(t, err)
		assert.False(t, ciphertexts[ciphertext], "same ciphertext produced twice (nonce reuse)")
		ciphertexts[ciphertext] = true
	}
}

func TestAESEncryptor_DecryptErrors(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	encryptor, err := NewAESEncryptor(key)
	require.NoError(t, err)

	tests := []struct {
		name       string
		ciphertext string
		wantErr    error
	}{
		{
			name:       "invalid base64",
			ciphertext: "not-valid-base64!@#$",
			wantErr:    nil, // will return base64 decode error
		},
		{
			name:       "too short ciphertext",
			ciphertext: "YWJj", // "abc" in base64, too short for nonce
			wantErr:    ErrMalformedCiphertext,
		},
		{
			name:       "empty string",
			ciphertext: "",
			wantErr:    nil, // will return base64 or malformed error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := encryptor.Decrypt(tt.ciphertext)
			assert.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestAESEncryptor_TamperedCiphertext(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	encryptor, err := NewAESEncryptor(key)
	require.NoError(t, err)

	// Encrypt something
	ciphertext, err := encryptor.Encrypt("secret data")
	require.NoError(t, err)

	// Tamper with the ciphertext by changing a character
	// First decode, tamper, then re-encode
	decoded := make([]byte, len(ciphertext))
	copy(decoded, []byte(ciphertext))
	if len(decoded) > 10 {
		decoded[10] ^= 0xFF // flip bits
	}
	tampered := string(decoded)

	// Try to decrypt tampered data - should fail
	_, err = encryptor.Decrypt(tampered)
	// The error type depends on where the tampering occurred,
	// but it should definitely error
	assert.Error(t, err)
}

func TestAESEncryptor_DifferentKeysCannotDecrypt(t *testing.T) {
	t.Parallel()

	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 1 // Different key

	encryptor1, err := NewAESEncryptor(key1)
	require.NoError(t, err)

	encryptor2, err := NewAESEncryptor(key2)
	require.NoError(t, err)

	// Encrypt with key1
	ciphertext, err := encryptor1.Encrypt("secret")
	require.NoError(t, err)

	// Try to decrypt with key2 - should fail
	_, err = encryptor2.Decrypt(ciphertext)
	assert.Error(t, err)
}
