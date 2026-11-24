// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import "testing"

func TestRows(t *testing.T) {
	tests := []struct {
		name string
		text string
		want count
	}{
		{
			name: "rows of a slice and a map of an anonymous struct",
			text: `package a
func TestA(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a", "b"},
		{in: "c", want: "d"},
	}
	byName := map[string]struct{ ok bool }{"x": {ok: true}}
	for _, tc := range []struct{ n int }{{1}} {
		_ = tc
	}
	_, _ = tests, byName
}`,
			want: count{Rows: 4},
		},
		{
			name: "elements of a named type are fixtures, not rows",
			text: `package a
type torrent struct{ hash string }
var torrents = []torrent{{hash: "a"}, {hash: "b"}}
var names = []string{"a"}`,
			want: count{},
		},
		{
			name: "a set of an empty struct is not a table",
			text: `package a
var seen = map[string]struct{}{"x": {}, "y": {}}`,
			want: count{},
		},
		{
			name: "a file that does not parse",
			text: "package a\nfunc {",
			want: count{Broken: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rows(source{Name: "a_test.go", Text: tc.text}); got != tc.want {
				t.Fatalf("rows() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
