// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultArgon2Params(t *testing.T) {
	t.Parallel()

	params := DefaultArgon2Params()

	assert.Equal(t, uint32(64*1024), params.Memory, "memory should be 64MB")
	assert.Equal(t, uint32(3), params.Iterations)
	assert.Equal(t, uint8(2), params.Parallelism)
	assert.Equal(t, uint32(16), params.SaltLength)
	assert.Equal(t, uint32(32), params.KeyLength)
}

func TestHashPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "simple password",
			password: "password123",
		},
		{
			name:     "empty password",
			password: "",
		},
		{
			name:     "long password",
			password: strings.Repeat("a", 1000),
		},
		{
			name:     "unicode password",
			password: "пароль密码🔐",
		},
		{
			name:     "special characters",
			password: "!@#$%^&*()_+-=[]{}|;':\",./<>?`~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash, err := HashPassword(tt.password)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, hash)

			// Verify hash format: $argon2id$v=19$m=X,t=X,p=X$salt$hash
			assert.True(t, strings.HasPrefix(hash, "$argon2id$v="), "hash should start with $argon2id$v=")
			parts := strings.Split(hash, "$")
			assert.Len(t, parts, 6, "hash should have 6 parts when split by $")
		})
	}
}

func TestHashPassword_ProducesDifferentHashes(t *testing.T) {
	t.Parallel()

	// Same password should produce different hashes (due to random salt)
	password := "same-password"
	hashes := make(map[string]bool)

	for i := 0; i < 5; i++ {
		hash, err := HashPassword(password)
		require.NoError(t, err)
		assert.False(t, hashes[hash], "same hash produced twice (salt reuse)")
		hashes[hash] = true
	}
}

func TestVerifyPassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
	}{
		{
			name:     "simple password",
			password: "password123",
		},
		{
			name:     "empty password",
			password: "",
		},
		{
			name:     "unicode password",
			password: "пароль密码🔐",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash, err := HashPassword(tt.password)
			require.NoError(t, err)

			// Correct password should verify
			valid, err := VerifyPassword(tt.password, hash)
			require.NoError(t, err)
			assert.True(t, valid, "correct password should verify")

			// Wrong password should not verify
			valid, err = VerifyPassword(tt.password+"wrong", hash)
			require.NoError(t, err)
			assert.False(t, valid, "wrong password should not verify")
		})
	}
}

func TestVerifyPassword_InvalidHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hash    string
		wantErr string
	}{
		{
			name:    "empty hash",
			hash:    "",
			wantErr: "invalid hash format",
		},
		{
			name:    "wrong format - not enough parts",
			hash:    "$argon2id$v=19$salt$hash",
			wantErr: "invalid hash format",
		},
		{
			name:    "wrong algorithm",
			hash:    "$bcrypt$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
			wantErr: "incompatible hash algorithm",
		},
		{
			name:    "wrong version",
			hash:    "$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$aGFzaA",
			wantErr: "incompatible argon2 version",
		},
		{
			name:    "invalid parameters format",
			hash:    "$argon2id$v=19$invalid$c2FsdA$aGFzaA",
			wantErr: "failed to parse parameters",
		},
		{
			name:    "invalid base64 salt",
			hash:    "$argon2id$v=19$m=65536,t=3,p=2$!!!invalid!!$aGFzaA",
			wantErr: "failed to decode salt",
		},
		{
			name:    "invalid base64 hash",
			hash:    "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!!!invalid!!",
			wantErr: "failed to decode hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := VerifyPassword("password", tt.hash)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDecodeHash(t *testing.T) {
	t.Parallel()

	// First create a valid hash
	hash, err := HashPassword("test-password")
	require.NoError(t, err)

	params, salt, hashBytes, err := decodeHash(hash)
	require.NoError(t, err)

	// Verify extracted parameters match defaults
	defaultParams := DefaultArgon2Params()
	assert.Equal(t, defaultParams.Memory, params.Memory)
	assert.Equal(t, defaultParams.Iterations, params.Iterations)
	assert.Equal(t, defaultParams.Parallelism, params.Parallelism)

	// Verify salt and hash have expected lengths
	assert.Len(t, salt, int(defaultParams.SaltLength))
	assert.Len(t, hashBytes, int(defaultParams.KeyLength))
}

func TestDecodeHash_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hash    string
		wantErr string
	}{
		{
			name:    "too few parts",
			hash:    "$argon2id$v=19$m=65536",
			wantErr: "invalid hash format",
		},
		{
			name:    "wrong algorithm",
			hash:    "$scrypt$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
			wantErr: "incompatible hash algorithm",
		},
		{
			name:    "missing version prefix",
			hash:    "$argon2id$19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
			wantErr: "failed to parse version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := decodeHash(tt.hash)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
