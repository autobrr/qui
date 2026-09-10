// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// legacyRewriter is the startup pass every credential store exposes.
type legacyRewriter interface {
	RewriteLegacyCredentials(ctx context.Context) (int, error)
}

type legacyRewriteCase struct {
	name string
	// table is the table the pass updates, read back to check the stored format.
	table string
	// credentials maps each encrypted column to the plaintext seeded into it.
	credentials map[string]string
	// seed creates one row through the store's own writer, which keeps the
	// fixture engine-agnostic.
	seed     func(t *testing.T, db *database.DB, key []byte) int64
	newStore func(t *testing.T, db *database.DB, current, legacy []byte) legacyRewriter
}

func legacyRewriteCases() []legacyRewriteCase {
	return []legacyRewriteCase{
		{
			name:  "instances",
			table: "instances",
			credentials: map[string]string{
				"password_encrypted":       "instance-password",
				"api_key_encrypted":        "instance-api-key",
				"basic_password_encrypted": "instance-basic-password",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewInstanceStore(db, key)
				require.NoError(t, err)
				basicUsername, basicPassword := "basic-user", "basic-pass"
				instance, err := store.Create(t.Context(), "legacy-instance", "http://localhost:8080", "user", "pass", &basicUsername, &basicPassword, false, nil, "api-key")
				require.NoError(t, err)
				return int64(instance.ID)
			},
			newStore: func(t *testing.T, db *database.DB, current, legacy []byte) legacyRewriter {
				t.Helper()

				store, err := models.NewInstanceStore(db, current, models.WithLegacyEncryptionKey(legacy))
				require.NoError(t, err)
				return store
			},
		},
		{
			name:  "arr_instances",
			table: "arr_instances",
			credentials: map[string]string{
				"api_key_encrypted":        "arr-api-key",
				"basic_password_encrypted": "arr-basic-password",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewArrInstanceStore(db, key)
				require.NoError(t, err)
				basicUsername, basicPassword := "basic-user", "basic-pass"
				instance, err := store.Create(t.Context(), models.ArrInstanceTypeSonarr, "legacy-arr", "http://sonarr.invalid", "arr-key", &basicUsername, &basicPassword, true, 0, 15)
				require.NoError(t, err)
				return int64(instance.ID)
			},
			newStore: func(t *testing.T, db *database.DB, current, legacy []byte) legacyRewriter {
				t.Helper()

				store, err := models.NewArrInstanceStore(db, current, models.WithLegacyEncryptionKey(legacy))
				require.NoError(t, err)
				return store
			},
		},
		{
			name:  "torznab_indexers",
			table: "torznab_indexers",
			credentials: map[string]string{
				"api_key_encrypted":        "torznab-api-key",
				"basic_password_encrypted": "torznab-basic-password",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewTorznabIndexerStore(db, key)
				require.NoError(t, err)
				basicUsername, basicPassword := "basic-user", "basic-pass"
				indexer, err := store.Create(t.Context(), "legacy-indexer", "http://indexer.invalid", "indexer-key", &basicUsername, &basicPassword, true, 0, 30)
				require.NoError(t, err)
				return int64(indexer.ID)
			},
			newStore: func(t *testing.T, db *database.DB, current, legacy []byte) legacyRewriter {
				t.Helper()

				store, err := models.NewTorznabIndexerStore(db, current, models.WithLegacyEncryptionKey(legacy))
				require.NoError(t, err)
				return store
			},
		},
		{
			name:  "cross_seed_settings",
			table: "cross_seed_settings",
			credentials: map[string]string{
				"season_pack_tvdb_api_key_encrypted": "tvdb-api-key",
				"season_pack_tvdb_pin_encrypted":     "tvdb-pin",
				"redacted_api_key_encrypted":         "red-api-key",
				"orpheus_api_key_encrypted":          "ops-api-key",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewCrossSeedStore(db, key)
				require.NoError(t, err)
				_, err = store.UpsertSettings(t.Context(), &models.CrossSeedAutomationSettings{RunIntervalMinutes: 120})
				require.NoError(t, err)
				return 1
			},
			newStore: func(t *testing.T, db *database.DB, current, legacy []byte) legacyRewriter {
				t.Helper()

				store, err := models.NewCrossSeedStore(db, current, models.WithLegacyEncryptionKey(legacy))
				require.NoError(t, err)
				return store
			},
		},
	}
}

// writeLegacyCredentials replaces the row's columns with pre-HKDF ciphertext.
func writeLegacyCredentials(t *testing.T, db *database.DB, table string, id int64, key []byte, credentials map[string]string) {
	t.Helper()

	for column, plaintext := range credentials {
		query := fmt.Sprintf("UPDATE %s SET %s = ? WHERE id = ?", table, column)
		_, err := db.ExecContext(t.Context(), query, legacyEncrypt(t, key, plaintext), id)
		require.NoError(t, err)
	}
}

func readCredentialColumn(t *testing.T, db *database.DB, table, column string, id int64) string {
	t.Helper()

	query := fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", column, table)
	var stored string
	require.NoError(t, db.QueryRowContext(t.Context(), query, id).Scan(&stored))

	return stored
}

func TestRewriteLegacyCredentialsSQLite(t *testing.T) {
	runRewriteLegacyCredentialsTests(t, testdb.NewMigratedSQLite)
}

func TestRewriteLegacyCredentialsPostgres(t *testing.T) {
	runRewriteLegacyCredentialsTests(t, testdb.NewMigratedPostgres)
}

// newTestDB opens a migrated database for one engine. Each case takes its own,
// because the stores enforce unique names and base URLs across rows.
type newTestDB func(t testing.TB, name string) *database.DB

func runRewriteLegacyCredentialsTests(t *testing.T, newDB newTestDB) {
	t.Helper()

	currentKey := testKey(t, 0)
	legacyKey := testKey(t, 100)
	// Stands in for a session secret the operator changed, so neither
	// configured key opens the stored values.
	strangerKey := testKey(t, 200)

	for _, tc := range legacyRewriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("rewrites_legacy_rows", func(t *testing.T) {
				db := newDB(t, "rewrite-"+tc.name)
				id := tc.seed(t, db, legacyKey)
				writeLegacyCredentials(t, db, tc.table, id, legacyKey, tc.credentials)

				store := tc.newStore(t, db, currentKey, legacyKey)
				rewritten, err := store.RewriteLegacyCredentials(t.Context())
				require.NoError(t, err)
				assert.Equal(t, 1, rewritten)

				// Only the current key is configured on this reader, so a value
				// that opens proves the row was re-sealed, not left alone.
				reader, err := models.NewCredentialCipher(currentKey)
				require.NoError(t, err)

				for column, plaintext := range tc.credentials {
					stored := readCredentialColumn(t, db, tc.table, column, id)
					assert.True(t, strings.HasPrefix(stored, "qui2:"), "%s should carry the version prefix", column)

					opened, err := reader.Decrypt(stored, nil)
					require.NoError(t, err, "column %s", column)
					assert.Equal(t, plaintext, opened, "column %s", column)
				}

				rewritten, err = store.RewriteLegacyCredentials(t.Context())
				require.NoError(t, err)
				assert.Zero(t, rewritten, "a second pass has nothing left to do")
			})

			t.Run("skips_undecryptable_rows", func(t *testing.T) {
				db := newDB(t, "skip-"+tc.name)
				id := tc.seed(t, db, legacyKey)
				writeLegacyCredentials(t, db, tc.table, id, strangerKey, tc.credentials)

				before := make(map[string]string, len(tc.credentials))
				for column := range tc.credentials {
					before[column] = readCredentialColumn(t, db, tc.table, column, id)
				}

				store := tc.newStore(t, db, currentKey, legacyKey)
				rewritten, err := store.RewriteLegacyCredentials(t.Context())
				require.NoError(t, err)
				assert.Zero(t, rewritten)

				for column, stored := range before {
					assert.Equal(t, stored, readCredentialColumn(t, db, tc.table, column, id), "column %s must be left untouched", column)
				}
			})
		})
	}
}

// TestRewriteLegacyCredentialsKeepsNullColumnsNull covers the nullable basic auth
// columns, which the pass must not turn into ciphertext of an empty string.
func TestRewriteLegacyCredentialsKeepsNullColumnsNull(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "rewrite-null-columns")
	currentKey := testKey(t, 0)
	legacyKey := testKey(t, 100)

	seedStore, err := models.NewInstanceStore(db, legacyKey)
	require.NoError(t, err)
	instance, err := seedStore.Create(t.Context(), "no-basic-auth", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	writeLegacyCredentials(t, db, "instances", int64(instance.ID), legacyKey, map[string]string{"password_encrypted": "pass"})

	var basicPassword *string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT basic_password_encrypted FROM instances WHERE id = ?", instance.ID).Scan(&basicPassword))
	require.Nil(t, basicPassword)

	store, err := models.NewInstanceStore(db, currentKey, models.WithLegacyEncryptionKey(legacyKey))
	require.NoError(t, err)
	rewritten, err := store.RewriteLegacyCredentials(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, rewritten)

	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT basic_password_encrypted FROM instances WHERE id = ?", instance.ID).Scan(&basicPassword))
	assert.Nil(t, basicPassword)
}

// TestRewriteLegacyCredentialsSkipsMixedRows pins the whole-row skip. One column
// that does not decrypt abandons the row. A sibling that would have decrypted is
// left legacy too, rather than leaving the row in two formats.
func TestRewriteLegacyCredentialsSkipsMixedRows(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "rewrite-mixed-row")
	currentKey := testKey(t, 0)
	legacyKey := testKey(t, 100)
	strangerKey := testKey(t, 200)

	seedStore, err := models.NewInstanceStore(db, legacyKey)
	require.NoError(t, err)
	basicUsername, basicPassword := "basic-user", "instance-basic-password"
	created, err := seedStore.Create(t.Context(), "mixed-row", "http://localhost:8080", "user", "instance-password", &basicUsername, &basicPassword, false, nil, "instance-api-key")
	require.NoError(t, err)
	id := int64(created.ID)

	// password_encrypted opens under the legacy key, basic_password_encrypted
	// does not. The pass must rewrite neither.
	writeLegacyCredentials(t, db, "instances", id, legacyKey, map[string]string{"password_encrypted": "instance-password"})
	writeLegacyCredentials(t, db, "instances", id, strangerKey, map[string]string{"basic_password_encrypted": "instance-basic-password"})

	before := map[string]string{
		"password_encrypted":       readCredentialColumn(t, db, "instances", "password_encrypted", id),
		"basic_password_encrypted": readCredentialColumn(t, db, "instances", "basic_password_encrypted", id),
	}
	require.False(t, strings.HasPrefix(before["password_encrypted"], "qui2:"), "the good column starts legacy")

	store, err := models.NewInstanceStore(db, currentKey, models.WithLegacyEncryptionKey(legacyKey))
	require.NoError(t, err)
	rewritten, err := store.RewriteLegacyCredentials(t.Context())
	require.NoError(t, err)
	assert.Zero(t, rewritten)

	for column, stored := range before {
		assert.Equal(t, stored, readCredentialColumn(t, db, "instances", column, id), "column %s must be byte-identical", column)
	}
}

// TestRewriteLegacyCredentialsKeepsEmptyColumnsEmpty covers the NOT NULL columns
// that default to the empty string, where an empty value means "not configured".
// Sealing an empty string would produce a real ciphertext, which HasAPIKey,
// apiKeyRedacted and getGazelleAPIKey all read as a key being set.
func TestRewriteLegacyCredentialsKeepsEmptyColumnsEmpty(t *testing.T) {
	currentKey := testKey(t, 0)
	legacyKey := testKey(t, 100)

	tests := []struct {
		name string
		// seeded is the one column carrying a legacy value, which makes the row
		// a rewrite candidate so the empty columns are actually visited.
		seeded string
		empty  []string
		seed   func(t *testing.T, db *database.DB, key []byte) int64
		store  func(t *testing.T, db *database.DB) legacyRewriter
	}{
		{
			name:   "instances",
			seeded: "password_encrypted",
			empty:  []string{"api_key_encrypted"},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewInstanceStore(db, key)
				require.NoError(t, err)
				instance, err := store.Create(t.Context(), "no-api-key", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
				require.NoError(t, err)
				return int64(instance.ID)
			},
			store: func(t *testing.T, db *database.DB) legacyRewriter {
				t.Helper()

				store, err := models.NewInstanceStore(db, currentKey, models.WithLegacyEncryptionKey(legacyKey))
				require.NoError(t, err)
				return store
			},
		},
		{
			name:   "cross_seed_settings",
			seeded: "redacted_api_key_encrypted",
			empty:  []string{"season_pack_tvdb_api_key_encrypted", "season_pack_tvdb_pin_encrypted", "orpheus_api_key_encrypted"},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewCrossSeedStore(db, key)
				require.NoError(t, err)
				_, err = store.UpsertSettings(t.Context(), &models.CrossSeedAutomationSettings{RunIntervalMinutes: 120})
				require.NoError(t, err)
				return 1
			},
			store: func(t *testing.T, db *database.DB) legacyRewriter {
				t.Helper()

				store, err := models.NewCrossSeedStore(db, currentKey, models.WithLegacyEncryptionKey(legacyKey))
				require.NoError(t, err)
				return store
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testdb.NewMigratedSQLite(t, "rewrite-empty-"+tt.name)
			id := tt.seed(t, db, legacyKey)
			writeLegacyCredentials(t, db, tt.name, id, legacyKey, map[string]string{tt.seeded: "configured"})

			for _, column := range tt.empty {
				require.Empty(t, readCredentialColumn(t, db, tt.name, column, id), "column %s should start empty", column)
			}

			rewritten, err := tt.store(t, db).RewriteLegacyCredentials(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, rewritten, "only the seeded column makes the row a candidate")

			assert.True(t, strings.HasPrefix(readCredentialColumn(t, db, tt.name, tt.seeded, id), "qui2:"))
			for _, column := range tt.empty {
				assert.Empty(t, readCredentialColumn(t, db, tt.name, column, id), "column %s must stay empty, not become ciphertext of an empty string", column)
			}
		})
	}
}

// TestRewriteLegacyCredentialsKeepsInstanceReadable checks the rewrite through
// the store's own accessors rather than the raw column.
func TestRewriteLegacyCredentialsKeepsInstanceReadable(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "rewrite-instance-api")
	currentKey := testKey(t, 0)
	legacyKey := testKey(t, 100)

	seedStore, err := models.NewInstanceStore(db, legacyKey)
	require.NoError(t, err)
	basicUsername, basicPassword := "basic-user", "instance-basic-password"
	created, err := seedStore.Create(t.Context(), "readable-instance", "http://localhost:8080", "user", "instance-password", &basicUsername, &basicPassword, false, nil, "instance-api-key")
	require.NoError(t, err)
	writeLegacyCredentials(t, db, "instances", int64(created.ID), legacyKey, map[string]string{
		"password_encrypted":       "instance-password",
		"api_key_encrypted":        "instance-api-key",
		"basic_password_encrypted": "instance-basic-password",
	})

	store, err := models.NewInstanceStore(db, currentKey, models.WithLegacyEncryptionKey(legacyKey))
	require.NoError(t, err)
	rewritten, err := store.RewriteLegacyCredentials(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, rewritten)

	instance, err := store.Get(t.Context(), created.ID)
	require.NoError(t, err)

	password, err := store.GetDecryptedPassword(instance)
	require.NoError(t, err)
	assert.Equal(t, "instance-password", password)

	apiKey, err := store.GetDecryptedAPIKey(instance)
	require.NoError(t, err)
	assert.Equal(t, "instance-api-key", apiKey)

	decryptedBasic, err := store.GetDecryptedBasicPassword(instance)
	require.NoError(t, err)
	require.NotNil(t, decryptedBasic)
	assert.Equal(t, "instance-basic-password", *decryptedBasic)
}

// legacyReadCase reads a store's credentials back through its own accessors.
// The opts are threaded through so the same closure runs with and without the
// legacy key, which is what makes the missing-option case observable.
type legacyReadCase struct {
	name  string
	table string
	// credentials maps each encrypted column to the plaintext seeded into it.
	credentials map[string]string
	seed        func(t *testing.T, db *database.DB, key []byte) int64
	read        func(t *testing.T, db *database.DB, id int64, opts ...models.CredentialCipherOption) (map[string]string, error)
}

func legacyReadCases() []legacyReadCase {
	return []legacyReadCase{
		{
			name:  "instances",
			table: "instances",
			credentials: map[string]string{
				"password_encrypted":       "instance-password",
				"api_key_encrypted":        "instance-api-key",
				"basic_password_encrypted": "instance-basic-password",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewInstanceStore(db, key)
				require.NoError(t, err)
				basicUsername, basicPassword := "basic-user", "instance-basic-password"
				instance, err := store.Create(t.Context(), "legacy-read", "http://localhost:8080", "user", "instance-password", &basicUsername, &basicPassword, false, nil, "instance-api-key")
				require.NoError(t, err)
				return int64(instance.ID)
			},
			read: func(t *testing.T, db *database.DB, id int64, opts ...models.CredentialCipherOption) (map[string]string, error) {
				t.Helper()

				store, err := models.NewInstanceStore(db, testKey(t, 0), opts...)
				require.NoError(t, err)
				instance, err := store.Get(t.Context(), int(id))
				require.NoError(t, err)

				password, err := store.GetDecryptedPassword(instance)
				if err != nil {
					return nil, err
				}
				apiKey, err := store.GetDecryptedAPIKey(instance)
				if err != nil {
					return nil, err
				}
				basicPassword, err := store.GetDecryptedBasicPassword(instance)
				if err != nil {
					return nil, err
				}
				require.NotNil(t, basicPassword)

				return map[string]string{
					"password_encrypted":       password,
					"api_key_encrypted":        apiKey,
					"basic_password_encrypted": *basicPassword,
				}, nil
			},
		},
		{
			name:  "arr_instances",
			table: "arr_instances",
			credentials: map[string]string{
				"api_key_encrypted":        "arr-api-key",
				"basic_password_encrypted": "arr-basic-password",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewArrInstanceStore(db, key)
				require.NoError(t, err)
				basicUsername, basicPassword := "basic-user", "arr-basic-password"
				instance, err := store.Create(t.Context(), models.ArrInstanceTypeSonarr, "legacy-read-arr", "http://sonarr.invalid", "arr-api-key", &basicUsername, &basicPassword, true, 0, 15)
				require.NoError(t, err)
				return int64(instance.ID)
			},
			read: func(t *testing.T, db *database.DB, id int64, opts ...models.CredentialCipherOption) (map[string]string, error) {
				t.Helper()

				store, err := models.NewArrInstanceStore(db, testKey(t, 0), opts...)
				require.NoError(t, err)
				instance, err := store.Get(t.Context(), int(id))
				require.NoError(t, err)

				apiKey, err := store.GetDecryptedAPIKey(instance)
				if err != nil {
					return nil, err
				}
				basicPassword, err := store.GetDecryptedBasicPassword(instance)
				if err != nil {
					return nil, err
				}

				return map[string]string{
					"api_key_encrypted":        apiKey,
					"basic_password_encrypted": basicPassword,
				}, nil
			},
		},
		{
			name:  "torznab_indexers",
			table: "torznab_indexers",
			credentials: map[string]string{
				"api_key_encrypted":        "torznab-api-key",
				"basic_password_encrypted": "torznab-basic-password",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewTorznabIndexerStore(db, key)
				require.NoError(t, err)
				basicUsername, basicPassword := "basic-user", "torznab-basic-password"
				indexer, err := store.Create(t.Context(), "legacy-read-indexer", "http://indexer.invalid", "torznab-api-key", &basicUsername, &basicPassword, true, 0, 30)
				require.NoError(t, err)
				return int64(indexer.ID)
			},
			read: func(t *testing.T, db *database.DB, id int64, opts ...models.CredentialCipherOption) (map[string]string, error) {
				t.Helper()

				store, err := models.NewTorznabIndexerStore(db, testKey(t, 0), opts...)
				require.NoError(t, err)
				indexer, err := store.Get(t.Context(), int(id))
				require.NoError(t, err)

				apiKey, err := store.GetDecryptedAPIKey(indexer)
				if err != nil {
					return nil, err
				}
				basicPassword, err := store.GetDecryptedBasicPassword(indexer)
				if err != nil {
					return nil, err
				}

				return map[string]string{
					"api_key_encrypted":        apiKey,
					"basic_password_encrypted": basicPassword,
				}, nil
			},
		},
		{
			name:  "cross_seed_settings",
			table: "cross_seed_settings",
			credentials: map[string]string{
				"season_pack_tvdb_api_key_encrypted": "tvdb-api-key",
				"season_pack_tvdb_pin_encrypted":     "tvdb-pin",
			},
			seed: func(t *testing.T, db *database.DB, key []byte) int64 {
				t.Helper()

				store, err := models.NewCrossSeedStore(db, key)
				require.NoError(t, err)
				_, err = store.UpsertSettings(t.Context(), &models.CrossSeedAutomationSettings{
					RunIntervalMinutes:   120,
					SeasonPackTVDBAPIKey: "tvdb-api-key",
					SeasonPackTVDBPIN:    "tvdb-pin",
				})
				require.NoError(t, err)
				return 1
			},
			read: func(t *testing.T, db *database.DB, _ int64, opts ...models.CredentialCipherOption) (map[string]string, error) {
				t.Helper()

				store, err := models.NewCrossSeedStore(db, testKey(t, 0), opts...)
				require.NoError(t, err)

				apiKey, pin, err := store.GetDecryptedSeasonPackTVDBCredentials(t.Context())
				if err != nil {
					return nil, err
				}

				return map[string]string{
					"season_pack_tvdb_api_key_encrypted": apiKey,
					"season_pack_tvdb_pin_encrypted":     pin,
				}, nil
			},
		},
	}
}

// TestStoresReadLegacyCredentialsBeforeRewrite goes through each store's real
// constructor and its own accessors, with no rewrite pass in between. The
// without-option half is what would catch WithLegacyEncryptionKey being dropped
// from a single call site in main.go, which compiles and then fails at runtime.
func TestStoresReadLegacyCredentialsBeforeRewrite(t *testing.T) {
	legacyKey := testKey(t, 100)

	for _, tc := range legacyReadCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := testdb.NewMigratedSQLite(t, "legacy-read-"+tc.name)
			id := tc.seed(t, db, legacyKey)
			writeLegacyCredentials(t, db, tc.table, id, legacyKey, tc.credentials)

			t.Run("with_the_legacy_key", func(t *testing.T) {
				got, err := tc.read(t, db, id, models.WithLegacyEncryptionKey(legacyKey))
				require.NoError(t, err)
				for column, plaintext := range tc.credentials {
					assert.Equal(t, plaintext, got[column], "column %s", column)
				}
			})

			t.Run("without_the_legacy_key", func(t *testing.T) {
				_, err := tc.read(t, db, id)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no legacy key configured")
			})
		})
	}
}
