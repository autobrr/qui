// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"slices"
	"testing"
)

func TestList(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   []comment
		broken bool
	}{
		{
			name: "raw string with comment-like text",
			text: "package a\n\nconst fixture = `\n//nolint:gosec // a fixture\n/* #nosec G304 */`\n",
			want: []comment{},
		},
		{
			name: "rune literal with a quote",
			text: "package a\n\nvar quote = '\"' //nolint:gosec // a rune\nvar name = \"//nolint:gosec\"\n",
			want: []comment{{Line: 3, Text: "//nolint:gosec // a rune"}},
		},
		{
			name: "block comment over several lines",
			text: "package a\n\n/*\nnolint:gosec\n*/\nvar x = 1\n",
			want: []comment{{Line: 3, Text: "/*\nnolint:gosec\n*/"}},
		},
		{
			name: "line comment after code",
			text: "package a\n\nvar data, _ = os.ReadFile(path) //nolint:gosec // the path comes from the config\n",
			want: []comment{{Line: 3, Text: "//nolint:gosec // the path comes from the config"}},
		},
		{
			name: "line directive does not move the next line",
			text: "package a\n\n//line other.go:100\nvar x = 1 // #nosec G115\n",
			want: []comment{{Line: 3, Text: "//line other.go:100"}, {Line: 4, Text: "// #nosec G115"}},
		},
		{
			name:   "unterminated raw string",
			text:   "package a\n\nconst fixture = `\n//nolint:gosec\n",
			want:   []comment{},
			broken: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := list(source{Name: "a.go", Text: tt.text})
			if !slices.Equal(got.Comments, tt.want) {
				t.Errorf("comments = %v, want %v", got.Comments, tt.want)
			}
			if got.Broken != tt.broken {
				t.Errorf("broken = %v, want %v", got.Broken, tt.broken)
			}
		})
	}
}
