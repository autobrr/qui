// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"encoding/json"
	"path/filepath"
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

func TestCleanPath_InvalidUTF8Preserved(t *testing.T) {
	t.Parallel()

	// On Unix, filenames are arbitrary bytes. cleanPath is the form that is
	// stored in settings and shown to the user, so it must keep them verbatim.
	bad := string([]byte{0xff, 0xfe})
	if utf8.ValidString(bad) {
		t.Fatalf("expected test string to be invalid UTF-8")
	}

	p := filepath.Join("downloads", bad, "file.mkv")
	want := filepath.Clean(p)
	got := cleanPath(p)
	if got != want {
		t.Fatalf("expected invalid UTF-8 path preserved:\n  %q\n  %q", got, want)
	}
}

// Imported content (cp1252, FAT, old rips) can carry bytes that are not valid
// UTF-8. The walker reads those bytes from disk verbatim, but the same name
// arrives from the qBittorrent API with each bad byte replaced by U+FFFD.
// Comparison must coerce both sides the same way, or an owned file is reported
// as an orphan and deleted.
func TestNormalizePath_InvalidUTF8MatchesAPIForm(t *testing.T) {
	t.Parallel()

	onDisk := filepath.Join("downloads", "Keep", "mo"+string([]byte{0xff})+"vie.mkv")
	if utf8.ValidString(onDisk) {
		t.Fatalf("expected on-disk name to be invalid UTF-8")
	}

	encoded, err := json.Marshal(onDisk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var viaAPI string
	if err := json.Unmarshal(encoded, &viaAPI); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !utf8.ValidString(viaAPI) {
		t.Fatalf("expected API form to be valid UTF-8")
	}

	// Add normalizes internally; Has takes the already-normalized form, exactly as
	// the walker does for each path it reads off disk.
	m := NewTorrentFileMap()
	m.Add(viaAPI)
	if !m.Has(normalizePath(onDisk)) {
		t.Fatalf("owned file must match its API spelling:\n  disk %q -> %q\n  api  %q -> %q",
			onDisk, normalizePath(onDisk), viaAPI, normalizePath(viaAPI))
	}
}

// Case-insensitive filesystems (APFS, NTFS, exFAT, SMB, and Docker bind mounts
// of them) let qBittorrent report two spellings of one directory. Comparison
// must fold on every OS, not only on Windows.
func TestNormalizePath_CaseInsensitive(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	p1 := normalizePath(filepath.Join(sep, "downloads", "cross-seed", "TrackerName", "Show.S01E01.mkv"))
	p2 := normalizePath(filepath.Join(sep, "downloads", "cross-seed", "trackername", "show.s01e01.mkv"))
	if p1 != p2 {
		t.Fatalf("expected normalized paths equal:\n  %q\n  %q", p1, p2)
	}

	m := NewTorrentFileMap()
	m.Add(p1)
	if !m.Has(p2) {
		t.Fatalf("expected torrent file map to match regardless of casing: %q", p2)
	}
}

func TestFindScanRoot_CaseInsensitive(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	root := filepath.Join(sep, "downloads", "cross-seed", "trackername")
	path := filepath.Join(sep, "downloads", "cross-seed", "TrackerName", "Show.S01E01.mkv")

	got := findScanRoot(path, []string{root})
	if got != root {
		t.Fatalf("expected scan root %q, got %q", root, got)
	}
}
