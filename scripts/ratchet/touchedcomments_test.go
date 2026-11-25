// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/scripts/comments/gocomments"
)

func TestTouchedComments(t *testing.T) {
	const was = "package p\n\nfunc f() {\n\t// why\n\tg(1)\n\th() // how\n}\n"
	type files map[string]string
	tests := []struct {
		name     string
		was, now files
		want     []string
	}{
		{"a change below a comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\t// why\n\tg(2)\n\th() // how\n}\n"}, []string{"a.go:4"}},
		{"a change in front of a comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\t// why\n\tg(1)\n\th(1) // how\n}\n"}, []string{"a.go:6"}},
		{"code deleted below a comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\t// why\n\th() // how\n}\n"}, []string{"a.go:4"}},
		{"a new or reworded comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\t// because\n\tg(1)\n\th() // how\n}\n"}, []string{"a.go:4"}},
		{"a slash added to a comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\t// why/\n\tg(1)\n\th() // how\n}\n"}, []string{"a.go:4"}},
		{"a comment copied with its code", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\t// why\n\tg(1)\n\th() // how\n\th() // how\n}\n"}, []string{"a.go:7"}},
		{"a comment copied to a file that sorts first", files{"b.go": was}, files{"a.go": was, "b.go": was}, []string{"a.go:4", "a.go:6"}},
		{"a change below a directive under a comment", files{"a.go": "package p\n\n// why\n//nolint:unused // reason\nfunc f() {}\n"}, files{"a.go": "package p\n\n// why\n//nolint:unused // reason\nfunc f(int) {}\n"}, []string{"a.go:3"}},
		{"a comment moved and its code changed", files{"a.go": was}, files{"b.go": "package p\n\nfunc f() {\n\t// why\n\tg(2)\n\th() // how\n}\n"}, []string{"b.go:4"}},
		{"a comment moved with its code to two files", files{"a.go": was}, files{"b.go": was, "c.go": was}, []string{"c.go:4", "c.go:6"}},

		{"a change on a line without a comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f(int) {\n\t// why\n\tg(1)\n\th() // how\n\ti()\n}\n"}, nil},
		{"a change that deletes the comment", files{"a.go": was}, files{"a.go": "package p\n\nfunc f() {\n\tg(2)\n\th(1)\n}\n"}, nil},
		{"code that gofmt indents and aligns again", files{"a.go": "package p\n\nfunc f() {\n\t// why\n\tg(1)\n\tx = 1 // how\n}\n"}, files{"a.go": "package p\n\nfunc f() {\n\tif ok {\n\t\t// why\n\t\tg(1)\n\t\tx   = 1   // how\n\t}\n}\n"}, nil},
		{"code moved to another file", files{"a.go": was}, files{"b.go": was}, nil},
		{"code moved between files that both stay", files{"a.go": was, "b.go": "package p\n"}, files{"a.go": "package p\n", "b.go": was}, nil},
		{"one of two comments deleted", files{"a.go": "package p\n\nfunc f() {\n\t// why\n\tg(1) // how\n}\n"}, files{"a.go": "package p\n\nfunc f() {\n\t// why\n\tg(1)\n}\n"}, nil},
		{"a directive that gofmt sets apart from the comment above it", files{"a.go": "package p\n\n// why\nfunc f() {}\n"}, files{"a.go": "package p\n\n// why\n//\n//nolint:unused // reason\nfunc f() {}\n"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parse := func(sources files) map[string][]gocomments.Block {
				blocks := map[string][]gocomments.Block{}
				for file, src := range sources {
					var err error
					blocks[file], err = blocksOf(file, []byte(src))
					require.NoError(t, err)
				}
				return blocks
			}
			var got []string
			for _, b := range touchedComments(parse(tt.now), parse(tt.was)) {
				got = append(got, fmt.Sprintf("%s:%d", b.File, b.Line))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
