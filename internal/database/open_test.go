// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPostgresDSN(t *testing.T) {
	t.Parallel()

	dsn := buildPostgresDSN(OpenOptions{
		PostgresHost:     "localhost",
		PostgresPort:     5432,
		PostgresUser:     "qui",
		PostgresPassword: "secret",
		PostgresDatabase: "qui",
		PostgresSSLMode:  "disable",
		ConnectTimeout:   15 * time.Second,
	})

	if !strings.HasPrefix(dsn, "postgres://qui:secret@localhost:5432/qui?") {
		t.Fatalf("unexpected DSN prefix: %s", dsn)
	}
	if !strings.Contains(dsn, "sslmode=disable") {
		t.Fatalf("expected sslmode in DSN: %s", dsn)
	}
	if !strings.Contains(dsn, "connect_timeout=15") {
		t.Fatalf("expected connect_timeout in DSN: %s", dsn)
	}
}

func TestBuildPostgresDSNRequiresHostUserDB(t *testing.T) {
	t.Parallel()

	tests := []OpenOptions{
		{PostgresUser: "u", PostgresDatabase: "d"},
		{PostgresHost: "h", PostgresDatabase: "d"},
		{PostgresHost: "h", PostgresUser: "u"},
	}

	for i, tc := range tests {
		if dsn := buildPostgresDSN(tc); dsn != "" {
			t.Fatalf("case %d: expected empty DSN, got %q", i, dsn)
		}
	}
}
