// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"strings"
	"testing"
)

func TestPruneRunHistoryForDirectoryQuery_PostgresUsesOffsetOnly(t *testing.T) {
	t.Parallel()

	query := pruneRunHistoryForDirectoryQuery("postgres")

	if !strings.Contains(query, "OFFSET ?") {
		t.Fatalf("expected postgres query to contain OFFSET clause, got:\n%s", query)
	}
	if strings.Contains(query, "LIMIT -1 OFFSET ?") {
		t.Fatalf("expected postgres query to avoid sqlite LIMIT -1 syntax, got:\n%s", query)
	}
}

func TestPruneRunHistoryForDirectoryQuery_SQLiteUsesLimitMinusOne(t *testing.T) {
	t.Parallel()

	query := pruneRunHistoryForDirectoryQuery("sqlite")

	if !strings.Contains(query, "LIMIT -1 OFFSET ?") {
		t.Fatalf("expected sqlite query to include LIMIT -1 OFFSET syntax, got:\n%s", query)
	}
}
