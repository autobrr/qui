// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/autobrr/qui/pkg/torrentname"
)

func TestSanitizeTorrentExportFilename_UTF8Boundary(t *testing.T) {
	// 214 ASCII characters + one two-byte rune pushes the sanitized name past the byte limit.
	name := strings.Repeat("a", 214) + "ä"

	filename := torrentname.SanitizeExportFilename(name, "", "tracker.example.com", "0123456789abcdef")

	if !utf8.ValidString(filename) {
		t.Fatalf("expected filename to be valid UTF-8, got %q", filename)
	}

	if strings.Contains(filename, "ä") {
		t.Fatalf("expected filename to drop the trailing rune at the byte boundary, got %q", filename)
	}

	if !strings.HasPrefix(filename, "[example] ") {
		t.Fatalf("expected filename to include tracker tag from registrable domain, got %q", filename)
	}

	if !strings.HasSuffix(filename, " - 01234.torrent") {
		t.Fatalf("expected filename to include short hash suffix, got %q", filename)
	}
}

func TestSanitizeTorrentExportFilename_Fallbacks(t *testing.T) {
	filename := torrentname.SanitizeExportFilename("", "", "", "")

	if filename != "torrent.torrent" {
		t.Fatalf("expected fallback filename to be torrent.torrent, got %q", filename)
	}

	filename = torrentname.SanitizeExportFilename("	", "Alternative", "", "ABCDEF1234")
	if !strings.HasSuffix(filename, " - abcde.torrent") {
		t.Fatalf("expected filename to include lowercased short hash, got %q", filename)
	}
	if strings.HasPrefix(filename, "[") {
		t.Fatalf("did not expect tracker prefix when domain missing, got %q", filename)
	}
}

func TestSanitizeTorrentExportFilename_IgnoreSubdomain(t *testing.T) {
	filename := torrentname.SanitizeExportFilename("Movie", "", "please.domain.tld", "d1b08cafe")

	if !strings.HasPrefix(filename, "[domain] ") {
		t.Fatalf("expected tracker tag to ignore subdomain, got %q", filename)
	}

	filename = torrentname.SanitizeExportFilename("Show", "", "please.passthebeer.com", "abcdef1234")
	if !strings.HasPrefix(filename, "[passthebeer] ") {
		t.Fatalf("expected tracker tag to use registrable domain, got %q", filename)
	}
}
