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

	accept, _ := shouldAcceptDirScanMatch(match, parsed, settings)
	if !accept {
		t.Fatalf("expected match to be accepted")
	}

	settings.MinPieceRatio = 80
	accept, _ = shouldAcceptDirScanMatch(match, parsed, settings)
	if accept {
		t.Fatalf("expected match to be rejected")
	}
}
