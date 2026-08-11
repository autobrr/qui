// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func mustLoadTorrent(t *testing.T, torrentData []byte) (metainfo.MetaInfo, metainfo.Info) {
	t.Helper()

	mi, err := metainfo.Load(bytes.NewReader(torrentData))
	require.NoError(t, err)

	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)

	require.Greater(t, info.PieceLength, int64(0))

	return *mi, info
}

// buildMultiFileTorrent builds torrent bytes describing files with the given
// names and content lengths. Only file paths and lengths ever feed the
// piece-boundary logic under test, so no piece hashes are computed and no
// files are written to disk.
func buildMultiFileTorrent(t *testing.T, rootName string, pieceLength int64, files map[string][]byte) []byte {
	t.Helper()

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)

	info := metainfo.Info{
		Name:        rootName,
		PieceLength: pieceLength,
	}
	for _, name := range names {
		info.Files = append(info.Files, metainfo.FileInfo{
			Path:   strings.Split(name, "/"),
			Length: int64(len(files[name])),
		})
	}

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	mi := metainfo.MetaInfo{
		InfoBytes: infoBytes,
	}

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

func fileDisplayPath(info *metainfo.Info, file metainfo.FileInfo) string {
	return torrentDisplayPath(info, &file)
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

func TestBuildFilesForBoundaryCheck_MatchesQbtFileNames(t *testing.T) {
	// The boundary-check paths are looked up in maps keyed by qbt.TorrentFiles.Name,
	// so the two builders must format paths identically. Malformed UTF-8 in the
	// metadata used to break that: only BuildTorrentFilesFromInfo sanitized it.
	info := metainfo.Info{
		Name:        "Movie.\xe1.2024",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"sub\xe1", "file.mkv"}, Length: 1},
			{Path: []string{"", "extra.nfo"}, Length: 1},
		},
	}

	qbtFiles := BuildTorrentFilesFromInfo(stringutils.SanitizeUTF8(info.Name), info)
	boundaryFiles := BuildFilesForBoundaryCheck(&info, func(string) bool { return true })

	require.Len(t, boundaryFiles, len(qbtFiles))
	for i := range qbtFiles {
		require.Equal(t, qbtFiles[i].Name, boundaryFiles[i].Path)
	}
	require.Equal(t, "Movie.�.2024/sub�/file.mkv", boundaryFiles[0].Path)
	require.Equal(t, "Movie.�.2024/_/extra.nfo", boundaryFiles[1].Path)
}
