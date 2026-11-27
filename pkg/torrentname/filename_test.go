// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentname

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzSanitizeExportFilename(f *testing.F) {
	for _, seed := range [][4]string{
		{"Movie.2024.1080p", "", "tracker.example.com", "ABCDEF0123456789"},
		{"", "abcdef0123456789", "", "abcdef0123456789"},
		{"", "", "", ""},
		{`a/b\c:d*e?f"g<h>i|j`, "", "", ""},
		{"line\nbreak\x00\x7f", "", "", ""},
		{" . .. ", "", "", ""},
		{"x.TORRENT", "", "", ""},
		{"a" + strings.Repeat("é", 200), "", "www.tracker.co.uk.", "zzzz"},
		{"ééé", "", strings.Repeat("a", 220) + ".com", "abcdef0123"},
		{"\xff\xfe", "", "\xff.com", "\xff"},
	} {
		f.Add(seed[0], seed[1], seed[2], seed[3])
	}
	f.Fuzz(func(t *testing.T, name, fallback, trackerDomain, hash string) {
		got := SanitizeExportFilename(name, fallback, trackerDomain, hash)
		call := fmt.Sprintf("SanitizeExportFilename(%q, %q, %q, %q)", name, fallback, trackerDomain, hash)
		if !utf8.ValidString(got) {
			t.Fatalf("%s = %q is not valid UTF-8", call, got)
		}
		if i := strings.IndexFunc(got, func(r rune) bool { return r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) }); i >= 0 {
			t.Fatalf("%s = %q holds %q, which a file name cannot hold", call, got, got[i])
		}
		if len(got) > 255 {
			t.Fatalf("%s has %d bytes, more than the 255 of a file name", call, len(got))
		}
		if !strings.HasSuffix(strings.ToLower(got), ".torrent") {
			t.Fatalf("%s = %q does not end in .torrent", call, got)
		}
		if strings.HasPrefix(got, ".") || strings.HasPrefix(got, " ") {
			t.Fatalf("%s = %q starts with a dot or a space", call, got)
		}
		short := strings.Map(func(r rune) rune {
			if r = unicode.ToLower(r); strings.ContainsRune("0123456789abcdef", r) {
				return r
			}
			return -1
		}, hash)
		short = short[:min(len(short), 5)]
		if short != "" && !strings.HasSuffix(got, " - "+short+".torrent") {
			t.Fatalf("%s = %q does not end in the short hash %q", call, got, short)
		}
	})
}

func FuzzTruncateUTF8(f *testing.F) {
	for _, seed := range []struct {
		input    string
		maxBytes int
	}{
		{"abc", 3},
		{"abc", 2},
		{"abc", 0},
		{"abc", -1},
		{"", 0},
		{"héllo", 2},
		{"\U0001f600\U0001f600", 5},
		{"\xff\xfe", 1},
	} {
		f.Add(seed.input, seed.maxBytes)
	}
	f.Fuzz(func(t *testing.T, input string, maxBytes int) {
		got := TruncateUTF8(input, maxBytes)
		call := fmt.Sprintf("TruncateUTF8(%q, %d)", input, maxBytes)
		if !strings.HasPrefix(input, got) {
			t.Fatalf("%s = %q is not the start of the input", call, got)
		}
		if len(got) > max(maxBytes, 0) {
			t.Fatalf("%s has %d bytes", call, len(got))
		}
		if utf8.ValidString(input) && !utf8.ValidString(got) {
			t.Fatalf("%s = %q cuts a character in two", call, got)
		}
		if got == input {
			return
		}
		if _, size := utf8.DecodeRuneInString(input[len(got):]); len(got)+size <= maxBytes {
			t.Fatalf("%s = %q leaves out a character that fits", call, got)
		}
	})
}
