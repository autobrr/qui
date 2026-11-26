// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func FuzzPathKeys(f *testing.F) {
	for _, seed := range []string{
		"",
		"/downloads/Movie.2024/Movie.2024.mkv",
		"/downloads//Movie.2024/./sub/../Movie.2024.mkv/",
		`C:\Downloads\Movie.2024\..\Show`,
		`c:/downloads/show`,
		`\\Server\Share\Folder\..`,
		`//server/share`,
		"C:",
		"1:1 Scale/x.mkv",
		"./0:",
		"a/../c:/b",
		`\..\:`,
		`\a\..\??\c:\x`,
		"/data/Pok\u00e9mon",
		"/data/Poke\u0301mon",
		"/DATA/\u00c5NGSTR\u00d6M/\u0130stanbul",
		"/data/\u0124\u0331",
		"/data/\xff\xfe/..",
		"/data/caf\xe9.mkv",
		"/data/\ufffd",
		"i\u0307",
		"I\u0307",
		"\u0130",
	} {
		f.Add(seed)
	}
	dialects := []struct {
		name string
		d    PathDialect
	}{
		{"slash", SlashPaths},
		{"windows", WindowsPaths},
	}
	f.Fuzz(func(t *testing.T, p string) {
		for _, dialect := range dialects {
			d := dialect.d
			clean, key := CleanPath(d, p), NormalizePath(d, p)
			if !d.IsAbs(p) && d.IsAbs(clean) {
				t.Fatalf("%s: the relative path %q cleans to the absolute path %q", dialect.name, p, clean)
			}
			if d == WindowsPaths && strings.HasPrefix(clean, `\??`) && !strings.HasPrefix(d.FromSlash(p), `\??`) {
				t.Fatalf("%s: %q cleans to the device path %q", dialect.name, p, clean)
			}
			if got := NormalizePath(d, key); got != key {
				t.Fatalf("%s: the key %q of %q gets the key %q", dialect.name, key, p, got)
			}
			for _, out := range []string{clean, key} {
				if d.Clean(out) != out {
					t.Fatalf("%s: %q gives %q, which is not clean", dialect.name, p, out)
				}
				if utf8.ValidString(out) && !norm.NFC.IsNormalString(out) {
					t.Fatalf("%s: %q gives %q, which is not NFC", dialect.name, p, out)
				}
			}
			if strings.Contains(clean, "\ufffd") && !strings.Contains(p, "\ufffd") {
				t.Fatalf("%s: %q cleans to %q, which replaces bytes of the path", dialect.name, p, clean)
			}
			if isASCII(p) {
				if got := NormalizePath(d, strings.ToUpper(p)); got != key {
					t.Fatalf("%s: %q has the key %q, and in upper case it has %q", dialect.name, p, key, got)
				}
			}
			if !utf8.ValidString(p) {
				continue
			}
			if got := CleanPath(d, norm.NFD.String(p)); got != clean {
				t.Fatalf("%s: %q cleans to %q, and its NFD form cleans to %q", dialect.name, p, clean, got)
			}
			if got := NormalizePath(d, norm.NFD.String(p)); got != key {
				t.Fatalf("%s: %q has the key %q, and its NFD form has %q", dialect.name, p, key, got)
			}
		}
	})
}
