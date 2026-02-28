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
