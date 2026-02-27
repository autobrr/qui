// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"strings"
	"testing"
)

func TestTopoSortSQLiteTables(t *testing.T) {
	metas := []sqliteTableMeta{
		{Name: "comments", Deps: map[string]struct{}{"posts": {}}},
		{Name: "posts", Deps: map[string]struct{}{"users": {}}},
		{Name: "users", Deps: map[string]struct{}{}},
	}

	sorted, err := topoSortSQLiteTables(metas)
	if err != nil {
		t.Fatalf("topoSortSQLiteTables failed: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(sorted))
	}
	if sorted[0].Name != "users" || sorted[1].Name != "posts" || sorted[2].Name != "comments" {
		t.Fatalf("unexpected sort order: %#v", sorted)
	}
}

func TestTopoSortSQLiteTablesCycle(t *testing.T) {
	metas := []sqliteTableMeta{
		{Name: "a", Deps: map[string]struct{}{"b": {}}},
		{Name: "b", Deps: map[string]struct{}{"a": {}}},
	}

	_, err := topoSortSQLiteTables(metas)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "a, b") {
		t.Fatalf("expected unresolved table names in error, got %q", err.Error())
	}
}
