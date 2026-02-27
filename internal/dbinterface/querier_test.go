// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import "testing"

func TestBuildQueryWithPlaceholders(t *testing.T) {
	query := BuildQueryWithPlaceholders("INSERT INTO test(value, value2) VALUES %s", 2, 3)
	want := "INSERT INTO test(value, value2) VALUES (?, ?), (?, ?), (?, ?)"
	if query != want {
		t.Fatalf("unexpected query.\nwant: %s\ngot:  %s", want, query)
	}
}

func TestBuildQueryWithPlaceholdersZeroRows(t *testing.T) {
	query := BuildQueryWithPlaceholders("VALUES %s", 2, 0)
	want := "VALUES "
	if query != want {
		t.Fatalf("unexpected query.\nwant: %s\ngot:  %s", want, query)
	}
}
