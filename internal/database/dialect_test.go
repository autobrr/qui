// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import "testing"

func TestParseDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    Dialect
		wantErr bool
	}{
		{input: "", want: DialectSQLite},
		{input: "sqlite", want: DialectSQLite},
		{input: "postgres", want: DialectPostgres},
		{input: "postgresql", want: DialectPostgres},
		{input: "invalid", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseDialect(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("unexpected dialect for %q: want %q got %q", tc.input, tc.want, got)
			}
		})
	}
}

func TestRebindQuestionToDollar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "simple placeholders",
			query: "SELECT * FROM test WHERE a = ? AND b = ?",
			want:  "SELECT * FROM test WHERE a = $1 AND b = $2",
		},
		{
			name:  "single quoted literal",
			query: "SELECT '?' AS q, id FROM test WHERE a = ?",
			want:  "SELECT '?' AS q, id FROM test WHERE a = $1",
		},
		{
			name:  "double quoted identifier",
			query: "SELECT \"?\" FROM test WHERE a = ?",
			want:  "SELECT \"?\" FROM test WHERE a = $1",
		},
		{
			name:  "line comment",
			query: "SELECT * FROM test -- ?\nWHERE a = ?",
			want:  "SELECT * FROM test -- ?\nWHERE a = $1",
		},
		{
			name:  "block comment",
			query: "SELECT /* ? */ * FROM test WHERE a = ?",
			want:  "SELECT /* ? */ * FROM test WHERE a = $1",
		},
		{
			name:  "escaped single quotes",
			query: "SELECT 'it''s ?' FROM test WHERE a = ?",
			want:  "SELECT 'it''s ?' FROM test WHERE a = $1",
		},
		{
			name:  "dollar quoted string",
			query: "SELECT $tag$?$tag$ FROM test WHERE a = ? AND b = ?",
			want:  "SELECT $tag$?$tag$ FROM test WHERE a = $1 AND b = $2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rebindQuestionToDollar(tc.query)
			if got != tc.want {
				t.Fatalf("unexpected rebound query:\nwant: %s\n got: %s", tc.want, got)
			}
		})
	}
}
