// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestShouldAcceptDirScanMatch_PiecePercentThreshold(t *testing.T) {
	parsed := &ParsedTorrent{
		Name:        "Example",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Example/a.bin", Size: 8, Offset: 0},
			{Path: "Example/b.bin", Size: 8, Offset: 8},
		},
		TotalSize: 16,
	}

	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{
			{TorrentFile: TorrentFile{Path: "Example/a.bin", Size: 8}},
			{TorrentFile: TorrentFile{Path: "Example/b.bin", Size: 4}},
		},
		UnmatchedTorrentFiles: []TorrentFile{{Path: "Example/c.bin", Size: 4}},
		IsPerfectMatch:        false,
		IsPartialMatch:        true,
		IsMatch:               true,
	}

	settings := &models.DirScanSettings{
		AllowPartial:                 true,
		MinPieceRatio:                70, // percent
		SkipPieceBoundarySafetyCheck: true,
	}

	decision := shouldAcceptDirScanMatch(match, parsed, settings)
	if !decision.Accept {
		t.Fatalf("expected match to be accepted")
	}

	settings.MinPieceRatio = 80
	decision = shouldAcceptDirScanMatch(match, parsed, settings)
	if decision.Accept {
		t.Fatalf("expected match to be rejected")
	}
}

func TestShouldAcceptDirScanMatch_NoFilesMatched(t *testing.T) {
	parsed := &ParsedTorrent{
		Name:        "Example",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Example/a.bin", Size: 8, Offset: 0},
		},
		TotalSize: 8,
	}

	match := &MatchResult{
		MatchedFiles:           nil,
		UnmatchedTorrentFiles:  []TorrentFile{{Path: "Example/a.bin", Size: 8}},
		UnmatchedSearcheeFiles: []*ScannedFile{{RelPath: "a.bin", Size: 8}},
		IsPerfectMatch:         false,
		IsPartialMatch:         false,
		IsMatch:                false,
	}

	settings := &models.DirScanSettings{
		AllowPartial: false,
	}

	decision := shouldAcceptDirScanMatch(match, parsed, settings)
	if decision.Accept {
		t.Fatalf("expected match to be rejected")
	}
	if decision.Reason != "no files matched" {
		t.Fatalf("expected reason %q, got %q", "no files matched", decision.Reason)
	}
}
