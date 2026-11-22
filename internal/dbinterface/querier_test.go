// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import "testing"

func TestBuildQueryWithPlaceholders(t *testing.T) {
	tests := []struct {
		name               string
		template           string
		placeholdersPerRow int
		numRows            int
		want               string
	}{
		{
			name:               "normal",
			template:           "INSERT INTO test(value, value2) VALUES %s",
			placeholdersPerRow: 2,
			numRows:            3,
			want:               "INSERT INTO test(value, value2) VALUES (?, ?), (?, ?), (?, ?)",
		},
		{
			name:               "zero rows",
			template:           "VALUES %s",
			placeholdersPerRow: 2,
			numRows:            0,
			want:               "VALUES ",
		},
		{
			name:               "zero placeholders per row",
			template:           "VALUES %s",
			placeholdersPerRow: 0,
			numRows:            3,
			want:               "VALUES ",
		},
		{
			name:               "negative placeholders per row",
			template:           "VALUES %s",
			placeholdersPerRow: -1,
			numRows:            3,
			want:               "VALUES ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildQueryWithPlaceholders(tt.template, tt.placeholdersPerRow, tt.numRows)
			if got != tt.want {
				t.Fatalf("unexpected query.\nwant: %s\ngot:  %s", tt.want, got)
			}
		})
	}
}

type stubDialectProvider string

func (s stubDialectProvider) Dialect() string {
	return string(s)
}

type stubPointerDialectProvider struct{}

func (*stubPointerDialectProvider) Dialect() string {
	return "sqlite"
}

func TestDialectOf(t *testing.T) {
	t.Run("normalizes non-empty dialects", func(t *testing.T) {
		tests := []struct {
			name string
			in   DialectProvider
			want string
		}{
			{name: "sqlite", in: stubDialectProvider("sqlite"), want: "sqlite"},
			{name: "postgres", in: stubDialectProvider("postgres"), want: "postgres"},
			{name: "trimmed uppercase postgres", in: stubDialectProvider("  POSTGRES  "), want: "postgres"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := DialectOf(tt.in); got != tt.want {
					t.Fatalf("DialectOf(%q) = %q, want %q", tt.in, got, tt.want)
				}
			})
		}
	})

	t.Run("panics on nil querier instead of defaulting to sqlite", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected DialectOf(nil) to panic, but it returned")
			}
		}()
		_ = DialectOf(nil)
	})

	t.Run("panics on typed-nil provider instead of defaulting to sqlite", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected DialectOf(typedNil) to panic, but it returned")
			}
		}()
		var typedNil *stubPointerDialectProvider
		_ = DialectOf(typedNil)
	})

	t.Run("panics on empty dialect instead of defaulting to sqlite", func(t *testing.T) {
		for _, raw := range []string{"", "   "} {
			t.Run("dialect_"+raw, func(t *testing.T) {
				defer func() {
					if r := recover(); r == nil {
						t.Fatalf("expected DialectOf(%q) to panic, but it returned", raw)
					}
				}()
				_ = DialectOf(stubDialectProvider(raw))
			})
		}
	})
}
