// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"strings"
	"testing"
)

func TestFindByExternalProgramIDQuery(t *testing.T) {
	sqliteQuery := findByExternalProgramIDQuery("sqlite")
	if !strings.Contains(sqliteQuery, "json_extract(conditions, '$.externalProgram.programId') = ?") {
		t.Fatalf("expected sqlite query to use json_extract, got:\n%s", sqliteQuery)
	}

	postgresQuery := findByExternalProgramIDQuery("postgres")
	if !strings.Contains(postgresQuery, "(conditions::jsonb -> 'externalProgram' ->> 'programId') = ?") {
		t.Fatalf("expected postgres query to use jsonb extraction, got:\n%s", postgresQuery)
	}
}

func TestClearExternalProgramActionQuery(t *testing.T) {
	sqliteQuery := clearExternalProgramActionQuery("sqlite")
	if !strings.Contains(sqliteQuery, "SET conditions = json_remove(conditions, '$.externalProgram')") {
		t.Fatalf("expected sqlite query to use json_remove, got:\n%s", sqliteQuery)
	}
	if !strings.Contains(sqliteQuery, "json_extract(conditions, '$.externalProgram.programId') = ?") {
		t.Fatalf("expected sqlite query to filter by json_extract, got:\n%s", sqliteQuery)
	}

	postgresQuery := clearExternalProgramActionQuery("postgres")
	if !strings.Contains(postgresQuery, "SET conditions = (conditions::jsonb - 'externalProgram')::text") {
		t.Fatalf("expected postgres query to remove externalProgram key via jsonb, got:\n%s", postgresQuery)
	}
	if !strings.Contains(postgresQuery, "(conditions::jsonb -> 'externalProgram' ->> 'programId') = ?") {
		t.Fatalf("expected postgres query to filter via jsonb extraction, got:\n%s", postgresQuery)
	}
}
