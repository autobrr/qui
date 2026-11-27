// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathcmp

import (
	"slices"
	"strings"
	"testing"
)

func FuzzNormalizePath(f *testing.F) {
	for _, seed := range []string{
		"",
		"/",
		"//",
		"///library//movie.mkv",
		"/library/folder/../movie.mkv",
		"/library/../../movie.mkv",
		"folder/../movie.mkv",
		"../movie.mkv",
		"./movie.mkv",
		`\\server\share\library\folder\..\movie.mkv`,
		"//server/share/../../movie.mkv",
		"//server/share/",
		"//server/share//library/movie.mkv",
		"//server",
		"//server/",
		"//./share/movie.mkv",
		"//../share/movie.mkv",
		"//server/../movie.mkv",
		`C:\library\folder\..\movie.mkv`,
		`C:\`,
		"C:",
		"C:/..",
		"C:movie.mkv",
		"C:..",
		"C:.",
		"C:folder/..",
		"./C:.",
		"folder/../C:/movie.mkv",
		"1:/movie.mkv",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, p string) {
		got := NormalizePath(p)
		if strings.Contains(got, `\`) {
			t.Fatalf("%q normalizes to %q, which holds a backslash", p, got)
		}
		if again := NormalizePath(got); again != got {
			t.Fatalf("%q normalizes to %q, and that normalizes to %q", p, got, again)
		}
		backslashed := strings.ReplaceAll(p, "/", `\`)
		if other := NormalizePath(backslashed); other != got {
			t.Fatalf("%q normalizes to %q, and with backslashes to %q", p, got, other)
		}
		absolute := IsAbsolute(p)
		if IsAbsolute(backslashed) != absolute {
			t.Fatalf("%q is absolute: %t, and with backslashes: %t", p, absolute, !absolute)
		}
		if IsAbsolute(got) != absolute {
			t.Fatalf("%q is absolute: %t, and it normalizes to %q, which is absolute: %t", p, absolute, got, !absolute)
		}
		if absolute && slices.Contains(strings.Split(got, "/"), "..") {
			t.Fatalf("the absolute path %q normalizes to %q, which climbs above its root", p, got)
		}
		if strings.HasSuffix(got, "/") && got != "/" && (len(got) != 3 || !IsWindowsDriveAbs(got)) {
			t.Fatalf("%q normalizes to %q, which ends in a separator", p, got)
		}
		if len(got) > 1 && strings.Contains(got[1:], "//") {
			t.Fatalf("%q normalizes to %q, which holds an empty segment", p, got)
		}
	})
}
