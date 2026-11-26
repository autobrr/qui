// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import "testing"

func TestInstanceSSHStatementsPostgresIntegration(t *testing.T) {
	t.Parallel()

	db, ctx := openPostgresTestDB(t)
	runInstanceSSHLifecycle(ctx, t, db)
}
