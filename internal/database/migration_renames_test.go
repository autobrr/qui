// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeMigrationFilenames_RenamesLicenseProviderDodo(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	conn, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	_, err = conn.ExecContext(ctx, `
		CREATE TABLE migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO migrations (filename) VALUES ('055_add_license_provider_dodo.sql');
	`)
	require.NoError(t, err)

	db := &DB{writerConn: conn}
	require.NoError(t, db.normalizeMigrationFilenames(ctx))

	var count055 int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = '055_add_license_provider_dodo.sql'").Scan(&count055))
	require.Zero(t, count055)

	var count057 int
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = '057_add_license_provider_dodo.sql'").Scan(&count057))
	require.Equal(t, 1, count057)
}
