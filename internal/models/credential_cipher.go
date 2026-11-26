// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

const credentialKeySize = 32

const credentialCipherPrefix = "qui2:"

type CredentialCipher struct {
	current cipher.AEAD
	legacy  cipher.AEAD
}

type CredentialCipherOption func(*CredentialCipher) error

func NewCredentialCipher(key []byte, opts ...CredentialCipherOption) (*CredentialCipher, error) {
	current, err := newCredentialAEAD(key)
	if err != nil {
		return nil, err
	}

	c := &CredentialCipher{current: current}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func WithLegacyEncryptionKey(key []byte) CredentialCipherOption {
	return func(c *CredentialCipher) error {
		legacy, err := newCredentialAEAD(key)
		if err != nil {
			return err
		}
		c.legacy = legacy
		return nil
	}
}

func newCredentialAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != credentialKeySize {
		return nil, errors.New("encryption key must be 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

func (c *CredentialCipher) Encrypt(plaintext string, aad []byte) (string, error) {
	nonce := make([]byte, c.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := c.current.Seal(nonce, nonce, []byte(plaintext), aad)
	return credentialCipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *CredentialCipher) Decrypt(ciphertext string, aad []byte) (string, error) {
	aead := c.current
	encoded := ciphertext
	if isLegacyCiphertext(ciphertext) {
		if c.legacy == nil {
			return "", errors.New("legacy ciphertext but no legacy key configured")
		}
		aead = c.legacy
	} else {
		encoded = strings.TrimPrefix(ciphertext, credentialCipherPrefix)
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	if len(data) < aead.NonceSize() {
		return "", errors.New("malformed ciphertext")
	}

	nonce, sealed := data[:aead.NonceSize()], data[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func isLegacyCiphertext(ciphertext string) bool {
	return !strings.HasPrefix(ciphertext, credentialCipherPrefix)
}

type legacyCredentialTable struct {
	table   string
	columns []string
}

type resealedRow struct {
	id      int64
	columns []string
	values  []any
}

func (c *CredentialCipher) rewriteLegacyRows(ctx context.Context, db dbinterface.Querier, spec legacyCredentialTable) (int, error) {
	pending, err := c.resealLegacyRows(ctx, db, spec)
	if err != nil {
		return 0, err
	}

	rewritten := 0
	for _, row := range pending {
		assignments := make([]string, 0, len(row.columns))
		for _, column := range row.columns {
			assignments = append(assignments, column+" = ?")
		}

		update := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", spec.table, strings.Join(assignments, ", "))
		if _, err := db.ExecContext(ctx, update, append(row.values, row.id)...); err != nil {
			return rewritten, fmt.Errorf("update %s credentials: %w", spec.table, err)
		}
		rewritten++
	}

	return rewritten, nil
}

func (c *CredentialCipher) resealLegacyRows(ctx context.Context, db dbinterface.Querier, spec legacyCredentialTable) ([]resealedRow, error) {
	query := fmt.Sprintf("SELECT id, %s FROM %s", strings.Join(spec.columns, ", "), spec.table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("select %s credentials: %w", spec.table, err)
	}
	defer rows.Close()

	var pending []resealedRow
	for rows.Next() {
		var id int64
		stored := make([]sql.NullString, len(spec.columns))
		dest := make([]any, 0, len(spec.columns)+1)
		dest = append(dest, &id)
		for i := range stored {
			dest = append(dest, &stored[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan %s credentials: %w", spec.table, err)
		}

		row := resealedRow{id: id}
		for i, value := range stored {
			if !value.Valid || strings.TrimSpace(value.String) == "" || !isLegacyCiphertext(value.String) {
				continue
			}

			plaintext, err := c.Decrypt(value.String, nil)
			if err != nil {
				log.Warn().
					Err(err).
					Str("table", spec.table).
					Int64("id", id).
					Str("column", spec.columns[i]).
					Msg("Leaving this row in the legacy format: a credential does not decrypt, most likely because sessionSecret changed")
				row.columns = nil
				break
			}

			resealed, err := c.Encrypt(plaintext, nil)
			if err != nil {
				return nil, fmt.Errorf("re-encrypt %s.%s: %w", spec.table, spec.columns[i], err)
			}

			row.columns = append(row.columns, spec.columns[i])
			row.values = append(row.values, resealed)
		}

		if len(row.columns) > 0 {
			pending = append(pending, row)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s credentials: %w", spec.table, err)
	}

	return pending, nil
}
