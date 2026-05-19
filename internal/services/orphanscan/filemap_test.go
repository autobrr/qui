// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf8"
)

func TestNormalizePath_UnicodeCanonicalEquivalence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		composed string
		// decomposed must be canonically equivalent to composed but not byte-identical.
		decomposed string
	}{
		{
			name:       "a-ring",
			composed:   "Låpsley",
			decomposed: "La\u030apsley", // a + combining ring above
		},
		{
			name:       "u-umlaut",
			composed:   "München",
			decomposed: "Mu\u0308nchen", // u + combining diaeresis
		},
		{
			name:       "e-acute",
			composed:   "Café",
			decomposed: "Cafe\u0301", // e + combining acute accent
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.composed == tt.decomposed {
				t.Fatalf("expected composed and decomposed to differ (test bug): %q", tt.composed)
			}

			p1 := filepath.Join("downloads", tt.composed, "file.mkv")
			p2 := filepath.Join("downloads", tt.decomposed, "file.mkv")
			n1 := normalizePath(p1)
			n2 := normalizePath(p2)
			if n1 != n2 {
				t.Fatalf("expected normalized paths equal:\n  %q\n  %q\n  -> %q\n  -> %q", p1, p2, n1, n2)
			}

			m := NewTorrentFileMap()
			m.Add(p1)
			if !m.Has(n2) {
				t.Fatalf("expected torrent file map to match canonical-equivalent path: %q", p2)
			}
		})
	}
}

func TestNormalizePath_InvalidUTF8Preserved(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == goosWindows {
		t.Skip("windows path handling lower-cases before UTF-8 check")
	}

	// On Unix, filenames are arbitrary bytes; ensure we don't replace invalid
	// sequences with U+FFFD during normalization.
	bad := string([]byte{0xff, 0xfe})
	if utf8.ValidString(bad) {
		t.Fatalf("expected test string to be invalid UTF-8")
	}

	p := filepath.Join("downloads", bad, "file.mkv")
	want := filepath.Clean(p)
	got := normalizePath(p)
	if got != want {
		t.Fatalf("expected invalid UTF-8 path preserved:\n  %q\n  %q", got, want)
	}
}

func TestNormalizePath_WindowsCaseInsensitive(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosWindows {
		t.Skip("windows-only path normalization")
	}

	p1 := normalizePath(`L:\movies\Code.8.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv`)
	p2 := normalizePath(`l:\MOVIES\Code.8.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv`)
	if p1 != p2 {
		t.Fatalf("expected normalized paths equal on windows:\n  %q\n  %q", p1, p2)
	}

	m := NewTorrentFileMap()
	m.Add(p1)
	if !m.Has(p2) {
		t.Fatalf("expected torrent file map to match regardless of casing: %q", p2)
	}
}

func TestFindScanRoot_WindowsCaseInsensitive(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosWindows {
		t.Skip("windows-only path matching")
	}

	root := `l:\movies`
	path := `L:\movies\Code.8.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv`

	got := findScanRoot(path, []string{root})
	if got != root {
		t.Fatalf("expected scan root %q, got %q", root, got)
	}
}
