// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestAPIKeyPermissionsMigrationGivesExistingKeysEveryPermissionSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkAPIKeyPermissionsMigration(t.Context(), t, conn, migrationsFS, "migrations/111_add_api_key_permissions.sql")
}

func TestAPIKeyPermissionsMigrationGivesExistingKeysEveryPermissionPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkAPIKeyPermissionsMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations/112_add_api_key_permissions.sql")
}

func checkAPIKeyPermissionsMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadFileFS, migration string) {
	t.Helper()

	const legacySchema = `
		CREATE TABLE string_pool (id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY,
			key_hash TEXT NOT NULL UNIQUE,
			name_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_used_at TIMESTAMP
		);
		CREATE VIEW api_keys_view AS
		SELECT ak.id, ak.key_hash, sp.value AS name, ak.created_at, ak.last_used_at
		FROM api_keys ak INNER JOIN string_pool sp ON ak.name_id = sp.id;
		INSERT INTO string_pool (id, value) VALUES (1, 'autobrr');
		INSERT INTO api_keys (id, key_hash, name_id) VALUES (1, 'hash', 1);
	`

	_, err := conn.ExecContext(ctx, legacySchema)
	require.NoError(t, err)
	body, err := fsys.ReadFile(migration)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, string(body))
	require.NoError(t, err)

	var permissionsJSON, instanceIDsJSON string
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT permissions, instance_ids FROM api_keys_view WHERE id = 1").Scan(&permissionsJSON, &instanceIDsJSON))
	var permissions []models.Permission
	require.NoError(t, json.Unmarshal([]byte(permissionsJSON), &permissions))
	require.Equal(t, models.AllPermissions(), permissions)
	require.JSONEq(t, "[]", instanceIDsJSON)
}
