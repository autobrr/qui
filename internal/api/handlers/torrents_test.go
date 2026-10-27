// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

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

func TestValidateTorrentFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "empty path allowed",
			path:    "",
			wantErr: false,
		},
		{
			name:    "full torrent file path allowed",
			path:    "/downloads/output/movie.torrent",
			wantErr: false,
		},
		{
			name:    "unix directory path rejected",
			path:    "/downloads/output/",
			wantErr: true,
		},
		{
			name:    "windows directory path rejected",
			path:    `C:\Downloads\Output\`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		err := validateTorrentFilePath(tt.path)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error, got nil", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: expected nil error, got %v", tt.name, err)
		}
	}
}

// TestSortedPeersResponseOmitsFullUpdate pins the shadow field on the peers
// response: MergePeers never copies FullUpdate into the cached struct, so the
// endpoint has only ever sent false and must now send nothing.
func TestSortedPeersResponseOmitsFullUpdate(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(SortedPeersResponse{
		TorrentPeersResponse: &qbt.TorrentPeersResponse{Rid: 7, FullUpdate: true},
		SortedPeers:          []SortedPeer{{Key: "192.0.2.1:1"}},
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotContains(t, decoded, "full_update")
	require.EqualValues(t, 7, decoded["rid"])
	require.Contains(t, decoded, "sorted_peers")
}
