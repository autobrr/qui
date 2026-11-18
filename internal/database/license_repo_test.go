// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestServerSetupTimeIsTheFirstMigration(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, "server-setup-time")
	ctx := t.Context()

	_, err := db.ExecContext(ctx, `UPDATE migrations SET applied_at = '2024-03-01 10:00:00' WHERE filename = '001_initial_schema.sql'`)
	require.NoError(t, err)

	setupAt, err := database.NewLicenseRepo(db).ServerSetupTime(ctx)
	require.NoError(t, err)
	require.True(t, setupAt.Equal(time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)), "got %s", setupAt)
}
