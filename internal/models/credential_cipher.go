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

// credentialKeySize is the AES-256 key length every credential store expects.
const credentialKeySize = 32

// credentialCipherPrefix marks a value sealed under the HKDF-derived key. The
// stored bytes start with a random nonce, so a version byte inside the payload
// could not be told apart from legacy ciphertext and the marker has to sit
// outside the base64.
const credentialCipherPrefix = "qui2:"

// CredentialCipher seals the credentials the stores keep in the database. It
// writes under the current key and can still read values written under the
// pre-HKDF key when one is configured.
type CredentialCipher struct {
	current cipher.AEAD
	legacy  cipher.AEAD
}

// CredentialCipherOption configures a CredentialCipher at construction.
type CredentialCipherOption func(*CredentialCipher) error

// NewCredentialCipher builds a cipher around a 32-byte key.
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

// WithLegacyEncryptionKey lets the cipher read values written before the key
// was derived with HKDF.
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

// Encrypt seals plaintext under the current key. aad is bound into the tag and
// must be passed again to Decrypt.
func (c *CredentialCipher) Encrypt(plaintext string, aad []byte) (string, error) {
	nonce := make([]byte, c.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := c.current.Seal(nonce, nonce, []byte(plaintext), aad)
	return credentialCipherPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value written by Encrypt, or an unprefixed legacy value under
// the legacy key when one is configured.
func (c *CredentialCipher) Decrypt(ciphertext string, aad []byte) (string, error) {
	aead := c.current
	encoded := ciphertext
	if isLegacyCiphertext(ciphertext) {
		// Trying the current key here would turn a missing legacy key into an
		// authentication failure, which reads exactly like a changed session
		// secret and would make correct wiring opt-in.
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

// isLegacyCiphertext reports a stored value that predates the versioned format.
func isLegacyCiphertext(ciphertext string) bool {
	return !strings.HasPrefix(ciphertext, credentialCipherPrefix)
}

// legacyCredentialTable names the encrypted columns of one table for the
// startup rewrite pass.
type legacyCredentialTable struct {
	table   string
	columns []string
}

// resealedRow holds the columns of one row that the rewrite pass will update.
type resealedRow struct {
	id      int64
	columns []string
	values  []any
}

// rewriteLegacyRows re-seals every unprefixed value in the named columns under
// the current key and reports how many rows it updated.
func (c *CredentialCipher) rewriteLegacyRows(ctx context.Context, db dbinterface.Querier, spec legacyCredentialTable) (int, error) {
	pending, err := c.resealLegacyRows(ctx, db, spec)
	if err != nil {
		return 0, err
	}

	// Every row is its own idempotent UPDATE, so a failure part way through
	// leaves the rest legacy and readable. Report what did commit, because the
	// operator needs to know whether nothing or almost everything moved.
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

// resealLegacyRows reads the table and re-encrypts what it can, so the update
// statements below run after the result set is closed. A row that will not
// decrypt is left exactly as it is: an operator who changed sessionSecret has
// credentials to re-enter, and overwriting them here would destroy the evidence.
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
					Msg("Leaving a credential in the legacy format: it does not decrypt, most likely because sessionSecret changed")
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
