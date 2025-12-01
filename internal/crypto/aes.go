// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package crypto provides shared encryption utilities for the application.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

var (
	// ErrInvalidKeySize is returned when the encryption key is not 32 bytes.
	ErrInvalidKeySize = errors.New("encryption key must be 32 bytes")
	// ErrMalformedCiphertext is returned when the ciphertext is too short.
	ErrMalformedCiphertext = errors.New("malformed ciphertext")
)

// GenerateSecureToken generates a cryptographically secure random token
// of the specified byte length, returned as a hex-encoded string.
// For example, length=32 produces a 64-character hex string.
func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// AESEncryptor provides AES-GCM encryption and decryption capabilities.
type AESEncryptor struct {
	key []byte
}

// NewAESEncryptor creates a new AESEncryptor with the given 32-byte key.
func NewAESEncryptor(key []byte) (*AESEncryptor, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeySize
	}
	return &AESEncryptor{key: key}, nil
}

// Encrypt encrypts a plaintext string using AES-GCM and returns base64-encoded ciphertext.
func (e *AESEncryptor) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
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

// Decrypt decrypts a base64-encoded ciphertext that was encrypted with Encrypt.
func (e *AESEncryptor) Decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		return "", ErrMalformedCiphertext
	}

	nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
