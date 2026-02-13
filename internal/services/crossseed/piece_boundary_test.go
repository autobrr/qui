// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func mustLoadTorrent(t *testing.T, torrentData []byte) (metainfo.MetaInfo, metainfo.Info) {
	t.Helper()

	mi, err := metainfo.Load(bytes.NewReader(torrentData))
	require.NoError(t, err)

	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)

	require.Greater(t, info.PieceLength, int64(0))
	require.NotEmpty(t, info.Pieces)
	require.Equal(t, 0, len(info.Pieces)%20, "pieces must be a multiple of 20 bytes (sha1 per piece)")

	return *mi, info
}

func buildMultiFileTorrent(t *testing.T, rootName string, pieceLength int64, files map[string][]byte) []byte {
	t.Helper()

	base := t.TempDir()
	root := filepath.Join(base, rootName)
	require.NoError(t, os.MkdirAll(root, 0o755))

	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.WriteFile(path, content, 0o644))
	}

	info := metainfo.Info{
		Name:        rootName,
		PieceLength: pieceLength,
	}
	require.NoError(t, info.BuildFromFilePath(root))
	info.Name = rootName

	mi := metainfo.MetaInfo{
		InfoBytes: bencode.MustMarshal(info),
	}

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

func pieceHash(t *testing.T, pieces []byte, idx int) []byte {
	t.Helper()

	start := idx * 20
	end := start + 20
	require.GreaterOrEqual(t, start, 0)
	require.LessOrEqual(t, end, len(pieces))

	out := make([]byte, 20)
	copy(out, pieces[start:end])
	return out
}

func fileDisplayPath(info *metainfo.Info, file metainfo.FileInfo) string {
	if len(info.Files) == 0 {
		return info.Name
	}
	return file.DisplayPath(info)
}

func TestPieceBoundaryOverlapCanCauseMainPieceHashMismatch(t *testing.T) {
	const pieceLength = int64(16)

	// Main file length is intentionally not a multiple of pieceLength so that the piece
	// containing the end of the main file also contains the beginning of the extra file.
	main := bytes.Repeat([]byte("M"), int(pieceLength*3+5))

	// These extras differ, but the main file content is identical across torrents.
	extraA := bytes.Repeat([]byte("A"), 19)
	extraB := bytes.Repeat([]byte("B"), 19)

	// Prefix file names to ensure deterministic ordering (main first, extra second).
	torrentA := buildMultiFileTorrent(t, "test-root", pieceLength, map[string][]byte{
		"a-main.bin":  main,
		"b-extra.bin": extraA,
	})
	torrentB := buildMultiFileTorrent(t, "test-root", pieceLength, map[string][]byte{
		"a-main.bin":  main,
		"b-extra.bin": extraB,
	})

	_, infoA := mustLoadTorrent(t, torrentA)
	_, infoB := mustLoadTorrent(t, torrentB)

	require.Equal(t, infoA.PieceLength, infoB.PieceLength)

	require.Len(t, infoA.Files, 2)
	require.Len(t, infoB.Files, 2)

	require.Equal(t, "a-main.bin", fileDisplayPath(&infoA, infoA.Files[0]))
	require.Equal(t, "a-main.bin", fileDisplayPath(&infoB, infoB.Files[0]))
	require.Equal(t, int64(len(main)), infoA.Files[0].Length)
	require.Equal(t, int64(len(main)), infoB.Files[0].Length)

	mainEndOffset := infoA.Files[0].Length
	require.NotZero(t, mainEndOffset%pieceLength, "test requires main->extra boundary to be mid-piece")

	// The piece containing the last byte of the main file spans the boundary and therefore
	// depends on the extra file's bytes. Different extra bytes => different boundary piece hash.
	boundaryPiece := int((mainEndOffset - 1) / pieceLength)
	require.GreaterOrEqual(t, boundaryPiece, 0)

	// Sanity check: pieces strictly before boundaryPiece are fully within the main file (main is first file),
	// so they should match across torrents when the main content is identical.
	for i := range boundaryPiece {
		require.Equalf(t,
			pieceHash(t, infoA.Pieces, i),
			pieceHash(t, infoB.Pieces, i),
			"piece %d should match (fully within main file)",
			i,
		)
	}

	require.NotEqual(t,
		pieceHash(t, infoA.Pieces, boundaryPiece),
		pieceHash(t, infoB.Pieces, boundaryPiece),
		"boundary piece should differ because it spans main+extra and extras differ",
	)
}

func TestPieceBoundariesSpanFiles_InRealTorrentFixtures(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		next := filepath.Dir(root)
		if next == root {
			t.Skip("could not locate repo root (go.mod)")
		}
		root = next
	}

	fixturesDir := filepath.Join(root, "torrentfiles")
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Skipf("no torrent fixtures directory at %q", fixturesDir)
	}

	var torrentPaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".torrent" {
			continue
		}
		torrentPaths = append(torrentPaths, filepath.Join(fixturesDir, entry.Name()))
	}
	slices.Sort(torrentPaths)
	if len(torrentPaths) < 2 {
		t.Skipf("need at least 2 .torrent fixtures in %q", fixturesDir)
	}

	type boundary struct {
		torrentPath   string
		pieceLength   int64
		boundaryOff   int64
		prevFile      string
		nextFile      string
		prevFileSize  int64
		nextFileSize  int64
		boundaryPiece int
	}

	var found *boundary
	for _, tp := range torrentPaths {
		data, err := os.ReadFile(tp)
		require.NoError(t, err)

		_, info := mustLoadTorrent(t, data)
		if len(info.Files) < 2 {
			continue
		}

		var offset int64
		for i := range len(info.Files) - 1 {
			offset += info.Files[i].Length
			if offset%info.PieceLength == 0 {
				continue
			}

			prev := info.Files[i]
			next := info.Files[i+1]
			found = &boundary{
				torrentPath:   tp,
				pieceLength:   info.PieceLength,
				boundaryOff:   offset,
				prevFile:      fileDisplayPath(&info, prev),
				nextFile:      fileDisplayPath(&info, next),
				prevFileSize:  prev.Length,
				nextFileSize:  next.Length,
				boundaryPiece: int((offset - 1) / info.PieceLength),
			}
			break
		}
		if found != nil {
			break
		}
	}

	if found == nil {
		t.Skip("fixtures did not contain a multi-file torrent with a mid-piece boundary in the first two files; add a fixture that demonstrates cross-file piece boundaries")
	}

	t.Logf(
		"found mid-piece boundary: torrent=%q boundaryOffset=%d pieceLength=%d boundaryPiece=%d prev=%q(%d) next=%q(%d)",
		found.torrentPath,
		found.boundaryOff,
		found.pieceLength,
		found.boundaryPiece,
		found.prevFile,
		found.prevFileSize,
		found.nextFile,
		found.nextFileSize,
	)

	require.NotZero(t, found.boundaryOff%found.pieceLength)
	require.NotEmpty(t, found.prevFile)
	require.NotEmpty(t, found.nextFile)
	require.Greater(t, found.prevFileSize, int64(0))
	require.Greater(t, found.nextFileSize, int64(0))
	require.GreaterOrEqual(t, found.boundaryPiece, 0)
	require.Equal(t, int((found.boundaryOff-1)/found.pieceLength), found.boundaryPiece)
}

// TestCheckPieceBoundarySafety tests the piece-boundary safety check logic.
func TestCheckPieceBoundarySafety(t *testing.T) {
	tests := []struct {
		name        string
		files       []TorrentFileForBoundaryCheck
		pieceLength int64
		wantSafe    bool
		wantReason  string
	}{
		{
			name:        "empty files",
			files:       []TorrentFileForBoundaryCheck{},
			pieceLength: 16,
			wantSafe:    true,
			wantReason:  "no files to check",
		},
		{
			name:        "invalid piece length",
			files:       []TorrentFileForBoundaryCheck{{Path: "a.mkv", Size: 100, IsContent: true}},
			pieceLength: 0,
			wantSafe:    false,
			wantReason:  "invalid piece length",
		},
		{
			name: "single content file",
			files: []TorrentFileForBoundaryCheck{
				{Path: "movie.mkv", Size: 1000, IsContent: true},
			},
			pieceLength: 16,
			wantSafe:    true,
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
		{
			name: "content then ignored - piece aligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "movie.mkv", Size: 64, IsContent: true},  // Ends at offset 64
				{Path: "movie.nfo", Size: 10, IsContent: false}, // Starts at offset 64
			},
			pieceLength: 16, // 64 % 16 == 0, so transition is piece-aligned
			wantSafe:    true,
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
		{
			name: "content then ignored - NOT piece aligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "movie.mkv", Size: 53, IsContent: true},  // Ends at offset 53
				{Path: "movie.nfo", Size: 10, IsContent: false}, // Starts at offset 53
			},
			pieceLength: 16, // 53 % 16 == 5, NOT piece-aligned
			wantSafe:    false,
			wantReason:  "found 1 piece boundary violation(s) between content and ignored files",
		},
		{
			name: "ignored then content - NOT piece aligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "a-sample.mkv", Size: 53, IsContent: false}, // Ignored file first
				{Path: "b-movie.mkv", Size: 1000, IsContent: true}, // Content file second
			},
			pieceLength: 16, // 53 % 16 == 5, NOT piece-aligned
			wantSafe:    false,
			wantReason:  "found 1 piece boundary violation(s) between content and ignored files",
		},
		{
			name: "content-content transition - no check needed",
			files: []TorrentFileForBoundaryCheck{
				{Path: "ep1.mkv", Size: 53, IsContent: true}, // Mid-piece end
				{Path: "ep2.mkv", Size: 47, IsContent: true}, // Both content, no transition
			},
			pieceLength: 16,
			wantSafe:    true, // No content/ignored transition, so always safe
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
		{
			name: "ignored-ignored transition - no check needed",
			files: []TorrentFileForBoundaryCheck{
				{Path: "sample1.mkv", Size: 53, IsContent: false},
				{Path: "sample2.mkv", Size: 47, IsContent: false},
			},
			pieceLength: 16,
			wantSafe:    true, // No content/ignored transition, so always safe
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
		{
			name: "complex: content-ignored-content all aligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "ep1.mkv", Size: 64, IsContent: true},
				{Path: "sample.mkv", Size: 32, IsContent: false},
				{Path: "ep2.mkv", Size: 64, IsContent: true},
			},
			pieceLength: 16, // 64 % 16 == 0, 96 % 16 == 0: all transitions aligned
			wantSafe:    true,
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
		{
			name: "complex: content-ignored-content first misaligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "ep1.mkv", Size: 65, IsContent: true},     // 65 % 16 == 1 (misaligned)
				{Path: "sample.mkv", Size: 31, IsContent: false}, // 96 % 16 == 0 (aligned)
				{Path: "ep2.mkv", Size: 64, IsContent: true},
			},
			pieceLength: 16,
			wantSafe:    false,
			wantReason:  "found 1 piece boundary violation(s) between content and ignored files",
		},
		{
			name: "complex: content-ignored-content second misaligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "ep1.mkv", Size: 64, IsContent: true},     // 64 % 16 == 0 (aligned)
				{Path: "sample.mkv", Size: 33, IsContent: false}, // 97 % 16 == 1 (misaligned)
				{Path: "ep2.mkv", Size: 63, IsContent: true},
			},
			pieceLength: 16,
			wantSafe:    false,
			wantReason:  "found 1 piece boundary violation(s) between content and ignored files",
		},
		{
			name: "complex: content-ignored-content both misaligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "ep1.mkv", Size: 65, IsContent: true},     // 65 % 16 == 1 (misaligned)
				{Path: "sample.mkv", Size: 33, IsContent: false}, // 98 % 16 == 2 (misaligned)
				{Path: "ep2.mkv", Size: 62, IsContent: true},
			},
			pieceLength: 16,
			wantSafe:    false,
			wantReason:  "found 2 piece boundary violation(s) between content and ignored files",
		},
		{
			name: "interleaved content and ignored - all misaligned",
			files: []TorrentFileForBoundaryCheck{
				{Path: "ep1.mkv", Size: 17, IsContent: true},
				{Path: "ep1.nfo", Size: 5, IsContent: false},
				{Path: "ep2.mkv", Size: 17, IsContent: true},
				{Path: "ep2.nfo", Size: 5, IsContent: false},
			},
			pieceLength: 16,
			wantSafe:    false, // Multiple violations
		},
		{
			name: "trailing ignored file at piece boundary",
			files: []TorrentFileForBoundaryCheck{
				{Path: "movie.mkv", Size: 1024, IsContent: true},
				{Path: "movie.nfo", Size: 500, IsContent: false},
			},
			pieceLength: 256, // 1024 % 256 == 0
			wantSafe:    true,
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
		{
			name: "leading ignored file at piece boundary",
			files: []TorrentFileForBoundaryCheck{
				{Path: "00-sample.mkv", Size: 256, IsContent: false},
				{Path: "01-movie.mkv", Size: 1024, IsContent: true},
			},
			pieceLength: 256, // 256 % 256 == 0
			wantSafe:    true,
			wantReason:  "all content/ignored transitions are piece-aligned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckPieceBoundarySafety(tt.files, tt.pieceLength)

			if result.Safe != tt.wantSafe {
				t.Errorf("CheckPieceBoundarySafety() Safe = %v, want %v", result.Safe, tt.wantSafe)
			}

			if tt.wantReason != "" && result.Reason != tt.wantReason {
				t.Errorf("CheckPieceBoundarySafety() Reason = %q, want %q", result.Reason, tt.wantReason)
			}

			// Verify violation details when unsafe
			if !result.Safe && len(result.UnsafeBoundaries) > 0 {
				for _, v := range result.UnsafeBoundaries {
					if v.ContentFile == "" {
						t.Error("violation missing ContentFile")
					}
					if v.IgnoredFile == "" {
						t.Error("violation missing IgnoredFile")
					}
					if v.Offset <= 0 {
						t.Error("violation has invalid Offset")
					}
					if v.PieceStart < 0 {
						t.Error("violation has invalid PieceStart")
					}
					if v.PieceEnd <= v.PieceStart {
						t.Error("violation has PieceEnd <= PieceStart")
					}
				}
			}
		})
	}
}

// TestCheckPieceBoundarySafetyFromMetainfo tests the metainfo wrapper.
func TestCheckPieceBoundarySafetyFromMetainfo(t *testing.T) {
	const pieceLength = int64(16)

	// Build a torrent with content file ending mid-piece followed by ignored file
	main := bytes.Repeat([]byte("M"), int(pieceLength*3+5)) // 53 bytes, ends mid-piece
	extra := bytes.Repeat([]byte("E"), 11)                  // NFO-like extra file

	torrentData := buildMultiFileTorrent(t, "test-root", pieceLength, map[string][]byte{
		"a-main.mkv":  main,
		"b-extra.nfo": extra,
	})

	_, info := mustLoadTorrent(t, torrentData)

	// Test with extra file marked as ignored (should be unsafe)
	// Path format includes root folder: "test-root/a-main.mkv"
	result := CheckPieceBoundarySafetyFromMetainfo(&info, func(path string) bool {
		return path == "test-root/a-main.mkv" // Only main is content
	})

	require.False(t, result.Safe, "should detect unsafe boundary when ignored file starts mid-piece")
	require.Len(t, result.UnsafeBoundaries, 1)
	require.Equal(t, "test-root/a-main.mkv", result.UnsafeBoundaries[0].ContentFile)
	require.Equal(t, "test-root/b-extra.nfo", result.UnsafeBoundaries[0].IgnoredFile)

	// Test with both files marked as content (should be safe - no transitions)
	result2 := CheckPieceBoundarySafetyFromMetainfo(&info, func(path string) bool {
		return true // All content
	})

	require.True(t, result2.Safe, "should be safe when all files are content")
}

// TestHasUnsafeIgnoredExtras tests the main entry point function.
func TestHasUnsafeIgnoredExtras(t *testing.T) {
	const pieceLength = int64(16)

	// Build a torrent with content file ending mid-piece followed by ignored file
	main := bytes.Repeat([]byte("M"), int(pieceLength*3+5)) // 53 bytes, ends mid-piece
	extra := bytes.Repeat([]byte("E"), 11)

	torrentData := buildMultiFileTorrent(t, "test-root", pieceLength, map[string][]byte{
		"a-main.mkv":  main,
		"b-extra.nfo": extra,
	})

	_, info := mustLoadTorrent(t, torrentData)

	t.Run("unsafe when ignored file shares piece with content", func(t *testing.T) {
		unsafe, result := HasUnsafeIgnoredExtras(&info, func(path string) bool {
			return path == "test-root/b-extra.nfo" // NFO is ignored
		})

		require.True(t, unsafe)
		require.False(t, result.Safe)
	})

	t.Run("safe when no ignored files", func(t *testing.T) {
		unsafe, result := HasUnsafeIgnoredExtras(&info, func(path string) bool {
			return false // Nothing ignored
		})

		require.False(t, unsafe)
		require.True(t, result.Safe)
		require.Equal(t, "no ignored files", result.Reason)
	})

	t.Run("nil info returns safe", func(t *testing.T) {
		unsafe, result := HasUnsafeIgnoredExtras(nil, func(path string) bool {
			return true
		})

		require.False(t, unsafe)
		require.True(t, result.Safe)
	})
}

// TestDifferentPieceLengthsAffectSafety proves that the safety logic depends on
// the incoming torrent's piece size. The same file sizes can be safe with one
// piece length and unsafe with another.
func TestDifferentPieceLengthsAffectSafety(t *testing.T) {
	// File layout: content file (1000 bytes) followed by ignored file (500 bytes)
	// Total: 1500 bytes
	files := []TorrentFileForBoundaryCheck{
		{Path: "content.mkv", Size: 1000, IsContent: true},
		{Path: "extra.nfo", Size: 500, IsContent: false},
	}

	// With piece length 1000: transition at offset 1000, 1000 % 1000 == 0 → SAFE
	result1000 := CheckPieceBoundarySafety(files, 1000)
	require.True(t, result1000.Safe, "should be safe with 1000-byte pieces (1000 %% 1000 == 0)")

	// With piece length 500: transition at offset 1000, 1000 % 500 == 0 → SAFE
	result500 := CheckPieceBoundarySafety(files, 500)
	require.True(t, result500.Safe, "should be safe with 500-byte pieces (1000 %% 500 == 0)")

	// With piece length 256: transition at offset 1000, 1000 % 256 == 232 → UNSAFE
	result256 := CheckPieceBoundarySafety(files, 256)
	require.False(t, result256.Safe, "should be unsafe with 256-byte pieces (1000 %% 256 == 232)")

	// With piece length 512: transition at offset 1000, 1000 % 512 == 488 → UNSAFE
	result512 := CheckPieceBoundarySafety(files, 512)
	require.False(t, result512.Safe, "should be unsafe with 512-byte pieces (1000 %% 512 == 488)")

	// Realistic piece sizes: 16 MiB (16777216) vs 8 MiB (8388608)
	// With file sizes that divide evenly by 8 MiB but not 16 MiB
	largeFiles := []TorrentFileForBoundaryCheck{
		{Path: "video.mkv", Size: 8388608 * 3, IsContent: true}, // 24 MiB = 3 * 8 MiB
		{Path: "subs.srt", Size: 50000, IsContent: false},
	}

	// 8 MiB pieces: 24 MiB % 8 MiB == 0 → SAFE
	result8MiB := CheckPieceBoundarySafety(largeFiles, 8388608)
	require.True(t, result8MiB.Safe, "should be safe with 8 MiB pieces (24 MiB %% 8 MiB == 0)")

	// 16 MiB pieces: 24 MiB % 16 MiB == 8 MiB → UNSAFE
	result16MiB := CheckPieceBoundarySafety(largeFiles, 16777216)
	require.False(t, result16MiB.Safe, "should be unsafe with 16 MiB pieces (24 MiB %% 16 MiB != 0)")
}

// TestPathFormatMatchesSourceFiles verifies that BuildFilesForBoundaryCheck
// produces paths that match the format of qbt.TorrentFiles (sourceFiles).
func TestPathFormatMatchesSourceFiles(t *testing.T) {
	const pieceLength = int64(16)

	// Build a multi-file torrent with a root folder
	main := bytes.Repeat([]byte("M"), int(pieceLength*3+5)) // 53 bytes
	extra := bytes.Repeat([]byte("E"), 11)

	torrentData := buildMultiFileTorrent(t, "test-root", pieceLength, map[string][]byte{
		"a-main.mkv":  main,
		"b-extra.nfo": extra,
	})

	_, info := mustLoadTorrent(t, torrentData)

	// Build files using boundary check function
	files := BuildFilesForBoundaryCheck(&info, func(path string) bool {
		return true // all content for this test
	})

	// Verify paths include the root folder (matching BuildTorrentFilesFromInfo behavior)
	require.Len(t, files, 2)
	require.Equal(t, "test-root/a-main.mkv", files[0].Path, "path should include root folder")
	require.Equal(t, "test-root/b-extra.nfo", files[1].Path, "path should include root folder")
}
