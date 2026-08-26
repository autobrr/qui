// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import "testing"

// The Postgres half of TestInstanceSSHStatementsSQLite. The name carries the
// TestOpenPostgres prefix because that is what the Postgres CI job's -run
// filter selects on.
func TestOpenPostgresInstanceSSHStatements(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)
	runInstanceSSHLifecycle(ctx, t, db)
}
