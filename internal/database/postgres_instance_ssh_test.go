// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import "testing"

// The Postgres half of TestInstanceSSHStatementsSQLite. The PostgresIntegration
// suffix is what the Postgres CI job's -run filter selects on.
func TestInstanceSSHStatementsPostgresIntegration(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)
	runInstanceSSHLifecycle(ctx, t, db)
}
