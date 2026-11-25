// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSizeLimit(t *testing.T) {
	limit, err := parseSizeLimit([]byte(`{"max_bytes": 20000, "extensions": [".go"], "skip": ["a.go"]}`))
	require.NoError(t, err)
	assert.Equal(t, sizeLimit{MaxBytes: 20000, Extensions: []string{".go"}, Skip: []string{"a.go"}}, limit)

	_, err = parseSizeLimit([]byte(`{"max_bytes": 20000, "skips": ["a.go"]}`))
	require.Error(t, err, "a misspelled key must not pass as an empty skip list")
	_, err = parseSizeLimit([]byte(`{"extensions": [".go"]}`))
	require.Error(t, err, "a missing limit must not pass as no limit")
}

func TestSizeLimitCovers(t *testing.T) {
	limit := sizeLimit{Extensions: []string{".go", ".ts"}, Skip: []string{"web/src/routes.gen.ts", "vendor/"}}
	tests := []struct {
		file string
		want bool
	}{
		{"internal/api/auth.go", true},
		{"web/src/types.d.ts", true},
		{"web/src/routes.gen.ts", false},
		{"web/src/routes.gen.ts.go", true},
		{"vendor/pkg/a.go", false},
		{"vendors/a.go", true},
		{"web/src/App.tsx", false},
		{"docs/linting.md", false},
		{"Makefile", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, limit.covers(tt.file), tt.file)
	}
}

func TestSizeOfCountsEachLineEndAsOneByte(t *testing.T) {
	assert.Equal(t, 4, sizeOf([]byte("a\nb\n")))
	assert.Equal(t, 4, sizeOf([]byte("a\r\nb\r\n")))
	assert.Equal(t, 3, sizeOf([]byte("a\rb")))
}

func TestParseBlobSizes(t *testing.T) {
	out := "0a1b blob 6\na\r\nb\r\n\n" +
		"origin/develop:gone file.go missing\n" +
		"0a1c tree 3\nabc\n" +
		"0a1d blob 0\n\n"
	sizes, err := parseBlobSizes([]string{"crlf.ts", "gone file.go", "dir", "empty.go"}, []byte(out))
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"crlf.ts": 4, "empty.go": 0}, sizes, "a CRLF line end on the base branch counts as one byte, as it does in the working tree")

	_, err = parseBlobSizes([]string{"cut.go"}, []byte("0a1b blob 6\na\r\n"))
	require.Error(t, err)
}

func TestParseRenames(t *testing.T) {
	out := "R100\x00old.go\x00new.go\x00R087\x00a/b c.go\x00d/e.go\x00"
	assert.Equal(t, map[string]string{"new.go": "old.go", "d/e.go": "a/b c.go"}, parseRenames([]byte(out)))
	assert.Empty(t, parseRenames(nil))
}

func TestLooserLimit(t *testing.T) {
	was := sizeLimit{MaxBytes: 20000, Extensions: []string{".go", ".ts"}, Skip: []string{"gen.ts", "vendor/"}}
	assert.Empty(t, looserLimit(was, was))
	assert.Empty(t, looserLimit(sizeLimit{MaxBytes: 16000, Extensions: []string{".ts", ".py", ".go"}, Skip: []string{"vendor/"}}, was), "a tighter limit passes")
	assert.Equal(t, []string{
		".file-size.json: max_bytes grew from 20000 to 20001.",
		".file-size.json: skip has the new entry internal/big.go.",
		".file-size.json: extensions lost the entry .ts.",
		looserAdvice,
	}, looserLimit(sizeLimit{MaxBytes: 20001, Extensions: []string{".go"}, Skip: []string{"gen.ts", "internal/big.go", "vendor/"}}, was))
	assert.Equal(t, []string{".file-size.json: skip has the new entry web/.", looserAdvice},
		looserLimit(sizeLimit{MaxBytes: 20000, Extensions: was.Extensions, Skip: []string{"web/"}}, sizeLimit{MaxBytes: 20000, Extensions: was.Extensions, Skip: []string{"web/gen.ts"}}),
		"a wider skip entry is a new entry")
}

func TestSizeProblems(t *testing.T) {
	limit := sizeLimit{MaxBytes: 100}
	now := map[string]int{
		"at-limit.go":          100,
		"crossed.go":           101,
		"held.go":              500,
		"held-and-shrunk.go":   150,
		"new.go":               101,
		"new-small.go":         99,
		"renamed-held.go":      300,
		"renamed-and-grown.go": 150,
	}
	was := map[string]int{
		"at-limit.go":        100,
		"crossed.go":         100,
		"held.go":            101,
		"held-and-shrunk.go": 400,
		"old-held.go":        250,
		"old-small.go":       90,
		"renamed-held.go":    1,
	}
	renamed := map[string]string{"renamed-held.go": "old-held.go", "renamed-and-grown.go": "old-small.go"}
	assert.Equal(t, []string{
		"crossed.go: grew from 100 to 101 bytes. The limit in .file-size.json is 100 bytes, about 25 tokens.",
		"new.go: a new file of 101 bytes. The limit in .file-size.json is 100 bytes, about 25 tokens.",
		"renamed-and-grown.go: grew from 90 to 150 bytes. The limit in .file-size.json is 100 bytes, about 25 tokens.",
		sizeAdvice,
	}, sizeProblems(limit, now, was, renamed))
	assert.Empty(t, sizeProblems(limit, map[string]int{"held.go": 900, "small.go": 1}, was, nil))
}
