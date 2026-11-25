// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
)

const publicPeerAddressMigration = "126_add_transfers_and_shares.sql"

func TestInstancePublicPeerAddressMigrationSQLite(t *testing.T) {
	t.Parallel()

	conn, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	_, err = conn.ExecContext(t.Context(), "PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	checkInstancePublicPeerAddressMigration(t.Context(), t, conn, migrationsFS, "migrations")
}

func TestInstancePublicPeerAddressMigrationPostgresIntegration(t *testing.T) {
	t.Parallel()

	ctx, dsn := openPostgresTestSchema(t)
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	checkInstancePublicPeerAddressMigration(ctx, t, conn, postgresMigrationsFS, "postgres_migrations")
}

func checkInstancePublicPeerAddressMigration(ctx context.Context, t *testing.T, conn *sql.DB, fsys fs.ReadDirFS, dir string) {
	t.Helper()

	exec := func(file string) {
		body, err := fs.ReadFile(fsys, path.Join(dir, file))
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, string(body))
		require.NoError(t, err, file)
	}
	entries, err := fsys.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".sql") && name < publicPeerAddressMigration {
			exec(name)
		}
	}

	var stringID int64
	require.NoError(t, conn.QueryRowContext(ctx, "INSERT INTO string_pool (value) VALUES ('seedbox') RETURNING id").Scan(&stringID))
	_, err = conn.ExecContext(ctx, "INSERT INTO instances (id, name_id, host_id, username_id, password_encrypted) VALUES (1, $1, $1, $1, 'x')", stringID)
	require.NoError(t, err)

	exec(publicPeerAddressMigration)

	var address string
	require.NoError(t, conn.QueryRowContext(ctx, "SELECT public_peer_address FROM instances_view WHERE id = 1").Scan(&address))
	require.Empty(t, address)
}

func TestInstancePublicPeerAddressStatementsSQLite(t *testing.T) {
	t.Parallel()

	runInstancePublicPeerAddressRoundTrip(t.Context(), t, openTestDatabase(t))
}

func TestInstancePublicPeerAddressStatementsPostgresIntegration(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)
	runInstancePublicPeerAddressRoundTrip(ctx, t, db)
}

func runInstancePublicPeerAddressRoundTrip(ctx context.Context, t *testing.T, db dbinterface.Querier) {
	t.Helper()

	store, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := store.Create(ctx, "seedbox", "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	update := func(params *models.InstanceUpdateParams) string {
		t.Helper()
		updated, err := store.Update(ctx, instance.ID, "seedbox", "http://localhost:8080", "user", "pass", nil, nil, params)
		require.NoError(t, err)
		return updated.PublicPeerAddress
	}

	require.Empty(t, update(&models.InstanceUpdateParams{TLSSkipVerify: new(true)}))
	require.Equal(t, "192.0.2.10:6881", update(&models.InstanceUpdateParams{PublicPeerAddress: new("192.0.2.10:6881")}))
	require.Equal(t, "192.0.2.10:6881", update(&models.InstanceUpdateParams{TLSSkipVerify: new(false)}))
	require.Empty(t, update(&models.InstanceUpdateParams{PublicPeerAddress: new("")}))
}
