// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

func TestResolveLocalTorrentFile(t *testing.T) {
	base := t.TempDir()

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "nested relative name", input: "Movie.2024/Movie.2024.mkv", want: filepath.Join(base, "Movie.2024", "Movie.2024.mkv"), ok: true},
		{name: "dot segments inside base", input: "sub/../Movie.mkv", want: filepath.Join(base, "Movie.mkv"), ok: true},
		{name: "backslash in a legal Linux file name", input: `AC\DC - Back In Black.mkv`},
		{name: "windows-style nested path", input: `Season 01\Episode 01.mkv`},
		{name: "backslash inside a nested legal Linux name", input: `dir/AC\DC.mkv`},
		{name: "windows traversal", input: `..\etc\passwd`},
		{name: "windows rooted path", input: `\evil\path`},
		{name: "windows UNC path", input: `\\server\share\file`},
		{name: "windows drive prefix with forward slashes", input: `C:/evil.mkv`},
		{name: "windows drive prefix with backslashes", input: `c:\evil.mkv`},
		{name: "posix traversal escaping the base", input: "a/../../etc/passwd"},
		{name: "posix parent traversal", input: "../evil.mkv"},
		{name: "posix absolute path", input: "/absolute.mkv"},
		{name: "current dir", input: "."},
		{name: "bare parent dir", input: ".."},
		{name: "empty name", input: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveLocalTorrentFile(fsops.HostPaths, base, tt.input)

			require.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
