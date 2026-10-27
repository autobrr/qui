// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/autobrr/qui/internal/fsops"
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
			n1 := fsops.NormalizePath(fsops.HostPaths, p1)
			n2 := fsops.NormalizePath(fsops.HostPaths, p2)
			if n1 != n2 {
				t.Fatalf("expected normalized paths equal:\n  %q\n  %q\n  -> %q\n  -> %q", p1, p2, n1, n2)
			}
		})
	}
}

func TestCleanPath_InvalidUTF8Preserved(t *testing.T) {
	t.Parallel()

	// On Unix, filenames are arbitrary bytes. fsops.CleanPath is the form that is
	// stored in settings and shown to the user, so it must keep them verbatim.
	bad := string([]byte{0xff, 0xfe})
	if utf8.ValidString(bad) {
		t.Fatalf("expected test string to be invalid UTF-8")
	}

	p := filepath.Join("downloads", bad, "file.mkv")
	want := filepath.Clean(p)
	got := fsops.CleanPath(fsops.HostPaths, p)
	if got != want {
		t.Fatalf("expected invalid UTF-8 path preserved:\n  %q\n  %q", got, want)
	}
}

// Case-insensitive filesystems (APFS, NTFS, exFAT, SMB, and Docker bind mounts
// of them) let qBittorrent report two spellings of one directory. Comparison
// must fold on every OS, not only on Windows.
func TestNormalizePath_CaseInsensitive(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	p1 := fsops.NormalizePath(fsops.HostPaths, filepath.Join(sep, "downloads", "cross-seed", "TrackerName", "Show.S01E01.mkv"))
	p2 := fsops.NormalizePath(fsops.HostPaths, filepath.Join(sep, "downloads", "cross-seed", "trackername", "show.s01e01.mkv"))
	if p1 != p2 {
		t.Fatalf("expected normalized paths equal:\n  %q\n  %q", p1, p2)
	}
}

func TestFindScanRoot_CaseInsensitive(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	root := filepath.Join(sep, "downloads", "cross-seed", "trackername")
	path := filepath.Join(sep, "downloads", "cross-seed", "TrackerName", "Show.S01E01.mkv")

	got := findScanRoot(fsops.HostPaths, path, []string{root})
	if got != root {
		t.Fatalf("expected scan root %q, got %q", root, got)
	}
}
