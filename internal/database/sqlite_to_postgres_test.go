// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"strings"
	"testing"
)

func TestTopoSortSQLiteTables(t *testing.T) {
	tests := []struct {
		name           string
		metas          []sqliteTableMeta
		wantNames      []string
		wantErrorParts []string
		wantErr        bool
	}{
		{
			name: "happy path",
			metas: []sqliteTableMeta{
				{Name: "comments", Deps: map[string]struct{}{"posts": {}}},
				{Name: "posts", Deps: map[string]struct{}{"users": {}}},
				{Name: "users", Deps: map[string]struct{}{}},
			},
			wantNames: []string{"users", "posts", "comments"},
		},
		{
			name: "cycle",
			metas: []sqliteTableMeta{
				{Name: "a", Deps: map[string]struct{}{"b": {}}},
				{Name: "b", Deps: map[string]struct{}{"a": {}}},
			},
			wantErr:        true,
			wantErrorParts: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sorted, err := topoSortSQLiteTables(tt.metas)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected cycle error, got nil")
				}
				for _, part := range tt.wantErrorParts {
					if !strings.Contains(err.Error(), part) {
						t.Fatalf("expected unresolved table name %q in error, got %q", part, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("topoSortSQLiteTables failed: %v", err)
			}

			if len(sorted) != len(tt.wantNames) {
				t.Fatalf("expected %d tables, got %d", len(tt.wantNames), len(sorted))
			}
			for i, name := range tt.wantNames {
				if sorted[i].Name != name {
					t.Fatalf("unexpected sort order: %#v", sorted)
				}
			}
		})
	}
}
