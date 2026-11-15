// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathcmp

import "testing"

func TestNormalizePath(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`\\server\share\library\folder\..\movie.mkv`, "//server/share/library/movie.mkv"},
		{"//server/share/library/", "//server/share/library"},
		{"//server/share/../../movie.mkv", "//server/share/movie.mkv"},
		{`\\server\share\`, "//server/share"},
		{"/library/folder/../movie.mkv", "/library/movie.mkv"},
		{"///library//movie.mkv", "/library/movie.mkv"},
		{`C:\library\folder\..\movie.mkv`, "C:/library/movie.mkv"},
		{`C:\`, "C:/"},
		{"C:", "C:"},
		{"folder/../movie.mkv", "movie.mkv"},
		{"", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := NormalizePath(tc.input); got != tc.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
