// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizePathSegment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "MyTracker",
			expected: "MyTracker",
		},
		{
			name:     "name with spaces",
			input:    "My Tracker",
			expected: "My Tracker",
		},
		{
			name:     "strips illegal chars",
			input:    "Tracker<>:\"/\\|?*Name",
			expected: "TrackerName",
		},
		{
			name:     "removes trailing dots",
			input:    "Tracker...",
			expected: "Tracker",
		},
		{
			name:     "removes trailing spaces",
			input:    "Tracker   ",
			expected: "Tracker",
		},
		{
			name:     "Windows reserved name CON",
			input:    "CON",
			expected: "_CON",
		},
		{
			name:     "Windows reserved name PRN",
			input:    "PRN",
			expected: "_PRN",
		},
		{
			name:     "Windows reserved name AUX",
			input:    "AUX",
			expected: "_AUX",
		},
		{
			name:     "Windows reserved name NUL",
			input:    "NUL",
			expected: "_NUL",
		},
		{
			name:     "Windows reserved name COM1",
			input:    "COM1",
			expected: "_COM1",
		},
		{
			name:     "Windows reserved name LPT1",
			input:    "LPT1",
			expected: "_LPT1",
		},
		{
			name:     "case insensitive reserved name",
			input:    "con",
			expected: "_con",
		},
		{
			name:     "Windows console input device",
			input:    "CONIN$",
			expected: "_CONIN$",
		},
		{
			name:     "Windows console output device case insensitive",
			input:    "conout$",
			expected: "_conout$",
		},
		{
			name:     "Windows COM superscript device",
			input:    "COM¹",
			expected: "_COM¹",
		},
		{
			name:     "Windows LPT superscript device",
			input:    "LPT³",
			expected: "_LPT³",
		},
		{
			name:     "reserved name with space before extension",
			input:    "CON .txt",
			expected: "_CON .txt",
		},
		{
			name:     "reserved name with spaces before extension case insensitive",
			input:    "nul  .mkv",
			expected: "_nul  .mkv",
		},
		{
			name:     "non-device CON prefix",
			input:    "CONSOLE",
			expected: "CONSOLE",
		},
		{
			name:     "non-device COM number",
			input:    "COM10",
			expected: "COM10",
		},
		{
			name:     "non-device LPT prefix",
			input:    "LPT",
			expected: "LPT",
		},
		{
			name:     "COM zero is not reserved",
			input:    "COM0",
			expected: "COM0",
		},
		{
			name:     "LPT zero is not reserved",
			input:    "LPT0",
			expected: "LPT0",
		},
		{
			name:     "reserved name not at start",
			input:    "MyCON",
			expected: "MyCON",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "_",
		},
		{
			name:     "all illegal chars",
			input:    "<>:\"/\\|?*",
			expected: "_",
		},
		{
			name:     "mixed content",
			input:    "Tracker [Private]!@#$%^&()",
			expected: "Tracker [Private]!@#$%^&()",
		},
		{
			name:     "unicode characters preserved",
			input:    "トラッカー",
			expected: "トラッカー",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePathSegment(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizePathSegment(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsolationFolder(t *testing.T) {
	tests := []struct {
		name    string
		torrent string
		want    string
	}{
		{name: "normal name", torrent: "My.Movie.2024.1080p.BluRay", want: "My.Movie.2024.1080p.BluRay"},
		{name: "illegal characters", torrent: "Movie: The <Sequel>", want: "Movie The Sequel"},
		{name: "longer than 80 bytes is kept", torrent: strings.Repeat("a", 200), want: strings.Repeat("a", 200)},
		{name: "cut to 245 bytes", torrent: strings.Repeat("a", 300), want: strings.Repeat("a", 245)},
		{name: "cut never splits a character", torrent: strings.Repeat("a", 244) + "é" + "bbb", want: strings.Repeat("a", 244)},
		{name: "name over 255 bytes of 2-byte characters", torrent: strings.Repeat("é", 200), want: strings.Repeat("é", 122)},
		{name: "cut never splits a 4-byte character", torrent: strings.Repeat("a", 243) + "😀" + "bbb", want: strings.Repeat("a", 243)},
		{name: "trailing dots and spaces after the cut", torrent: strings.Repeat("a", 241) + ". .." + "tail", want: strings.Repeat("a", 241)},
		{name: "device name after the cut", torrent: "AUX" + strings.Repeat(" ", 300) + "x", want: "_AUX"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsolationFolder(tt.torrent); got != tt.want {
				t.Errorf("IsolationFolder(%q) = %q, want %q", tt.torrent, got, tt.want)
			}
		})
	}
}

func TestHashedIsolationFolder(t *testing.T) {
	if got := HashedIsolationFolder("ABCDEF1234567890", "Movie"); got != "Movie--abcdef12" {
		t.Errorf("HashedIsolationFolder = %q, want %q", got, "Movie--abcdef12")
	}

	got := HashedIsolationFolder("abcdef1234567890", strings.Repeat("é", 200))
	if want := strings.Repeat("é", 122) + "--abcdef12"; got != want {
		t.Errorf("HashedIsolationFolder = %q (%d bytes), want %q", got, len(got), want)
	}
}

func FuzzTorrentFilePathMatchesReference(f *testing.F) {
	for _, seed := range [][3]string{
		{"Album", "Disc 1", "01 - Track.flac"},
		{"Album\u200e", "Disc\u200f 1", "01 \u2014 Track\u202e.flac"},
		{"Album", "", "\u2019s.flac"},
		{"Movie.\xe1\xff.2024", "sub\xff\xfe\u200e", "file.mkv"},
		{"", "\u200e", "track.flac"},
		{"\u202a\u202b\u202c\u202d", "\xe2\x80", "\xe2"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	filter := func(name string) string {
		return strings.Map(func(r rune) rune {
			switch r {
			case '\u200e', '\u200f', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e':
				return -1
			default:
				return r
			}
		}, strings.ToValidUTF8(name, "\ufffd"))
	}
	f.Fuzz(func(t *testing.T, root, dir, file string) {
		if got, want := TorrentPathName(root), filter(root); got != want {
			t.Fatalf("TorrentPathName(%q) = %q, want %q", root, got, want)
		}
		parts := []string{filter(root)}
		refused := false
		for _, c := range []string{dir, file} {
			switch {
			case c == "":
				parts = append(parts, "_")
			case filter(c) == "":
				refused = true
			default:
				parts = append(parts, filter(c))
			}
		}
		want := strings.Join(parts, "/")
		if parts[0] == "" {
			want = strings.Join(parts[1:], "/")
		}
		got, err := TorrentFilePath(TorrentPathName(root), []string{dir, file})
		if refused {
			if err == nil {
				t.Fatalf("TorrentFilePath(%q, %q, %q) = %q, want an error", root, dir, file, got)
			}
			return
		}
		if err != nil || got != want {
			t.Fatalf("TorrentFilePath(%q, %q, %q) = %q, %v; want %q", root, dir, file, got, err, want)
		}
	})
}

func checkPathSegment(t *testing.T, call, got string) {
	t.Helper()
	if got == "" {
		t.Fatalf("%s is empty", call)
	}
	if i := strings.IndexFunc(got, func(r rune) bool { return r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) }); i >= 0 {
		t.Fatalf("%s = %q holds %q, which a Windows file name cannot hold", call, got, got[i])
	}
	if strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") {
		t.Fatalf("%s = %q ends in a dot or a space", call, got)
	}
}

func FuzzSanitizePathSegment(f *testing.F) {
	for _, seed := range []string{
		"",
		"Movie.2024.1080p",
		"a/b\\c:d*e?f\"g<h>i|j",
		"name. . ",
		"..",
		"\x00\x1f",
		"CON",
		"con.txt",
		"LPT1 ",
		"COM\u00b9.log",
		"\xff\xfe",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got := SanitizePathSegment(name)
		call := fmt.Sprintf("SanitizePathSegment(%q)", name)
		checkPathSegment(t, call, got)
		if IsWindowsDeviceName(got) {
			t.Fatalf("%s = %q is a Windows device name", call, got)
		}
	})
}

func FuzzIsolationFolder(f *testing.F) {
	for _, seed := range []string{
		"",
		"Movie",
		strings.Repeat("a", 300),
		strings.Repeat("\u00e9", 200),
		strings.Repeat("a", 244) + "\u00e9",
		strings.Repeat("a", 240) + ". . . .x",
		strings.Repeat(".", 300),
		strings.Repeat(".", 250) + "x",
		strings.Repeat("\xff", 300),
		"AUX" + strings.Repeat(" ", 300) + "x",
		"COM1" + strings.Repeat(" ", 241) + "x",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got := IsolationFolder(name)
		call := fmt.Sprintf("IsolationFolder(%q)", name)
		checkPathSegment(t, call, got)
		if hashed := HashedIsolationFolder("0123456789abcdef", name); len(hashed) > 255 {
			t.Fatalf("HashedIsolationFolder of %q has %d bytes, more than the 255 of a file name", name, len(hashed))
		}
		if utf8.ValidString(name) && !utf8.ValidString(got) {
			t.Fatalf("%s = %q cuts a character in two", call, got)
		}
		if IsWindowsDeviceName(got) {
			t.Fatalf("%s = %q is a Windows device name", call, got)
		}
		full := SanitizePathSegment(name)
		device, guarded := strings.CutPrefix(got, "_")
		guarded = guarded && IsWindowsDeviceName(device) && strings.HasPrefix(full, device)
		if got != "_" && !strings.HasPrefix(full, got) && !guarded {
			t.Fatalf("%s = %q is not the start of %q", call, got, full)
		}
	})
}
