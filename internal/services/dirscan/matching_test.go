// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func buildTorrentBytes(t *testing.T, info *metainfo.Info) []byte {
	t.Helper()

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	mi := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "https://example.invalid/announce",
	}

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

// Discussion #2262: announce-list encoded as a bencode string must not fail
// the parse. Pins the leniency contract across go-torrent bumps.
func TestParseTorrentBytes_MalformedAnnounceList(t *testing.T) {
	info := "d6:lengthi1e4:name8:Test.Rel12:piece lengthi16384e6:pieces20:" + strings.Repeat("a", 20) + "e"
	data := []byte("d13:announce-list0:4:info" + info + "e")

	parsed, err := ParseTorrentBytes(data)
	require.NoError(t, err)
	require.Equal(t, "Test.Rel", parsed.Name)
}

func TestParseTorrentBytes_MultiFilePrefixesRootFolder(t *testing.T) {
	torrentBytes := buildTorrentBytes(t, &metainfo.Info{
		Name:        "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"Example.Show.S01E01.mkv"}, Length: 1},
			{Path: []string{"Example.Show.S01E02.mkv"}, Length: 1},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Equal(t, "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP", parsed.Name)
	require.Len(t, parsed.Files, 2)
	require.Equal(t, "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP/Example.Show.S01E01.mkv", parsed.Files[0].Path)
	require.Equal(t, "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP/Example.Show.S01E02.mkv", parsed.Files[1].Path)
}

func TestParseTorrentBytes_MultiFileDoesNotDoublePrefixRootFolder(t *testing.T) {
	torrentBytes := buildTorrentBytes(t, &metainfo.Info{
		Name:        "Example.Show.S02.1080p.WEB-DL.x264-GROUP",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"Example.Show.S02.1080p.WEB-DL.x264-GROUP", "Example.Show.S02E01.mkv"}, Length: 1},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Len(t, parsed.Files, 1)
	require.Equal(t, "Example.Show.S02.1080p.WEB-DL.x264-GROUP/Example.Show.S02E01.mkv", parsed.Files[0].Path)
}

func TestParseTorrentBytes_SanitizesInvalidUTF8(t *testing.T) {
	// "á" encoded as Latin-1 (0xe1) rather than UTF-8 is malformed. Torrent fields must
	// be UTF-8 per spec, so the bad byte is replaced with U+FFFD rather than passed downstream.
	torrentBytes := buildTorrentBytes(t, &metainfo.Info{
		Name:        "Movie.\xe1.2024.1080p-GROUP",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"Movie.\xe1.2024.1080p-GROUP.mkv"}, Length: 1},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Equal(t, "Movie.\uFFFD.2024.1080p-GROUP", parsed.Name)
	require.Len(t, parsed.Files, 1)
	require.Equal(t, "Movie.\uFFFD.2024.1080p-GROUP/Movie.\uFFFD.2024.1080p-GROUP.mkv", parsed.Files[0].Path)
}

func TestParseTorrentBytes_InvalidUTF8RootPrefixNotDoubled(t *testing.T) {
	// Root folder repeated in the file path AND invalid UTF-8 in the name: the root-dedup
	// comparison must see both sides sanitized, or the root gets prefixed twice.
	torrentBytes := buildTorrentBytes(t, &metainfo.Info{
		Name:        "Movie.\xe1.2024.1080p-GROUP",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"Movie.\xe1.2024.1080p-GROUP", "file.mkv"}, Length: 1},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Len(t, parsed.Files, 1)
	require.Equal(t, "Movie.\uFFFD.2024.1080p-GROUP/file.mkv", parsed.Files[0].Path)
}

func TestParseTorrentBytes_MultiFileNormalizesEmptyPathComponents(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		filePath []string
		wantPath string
	}{
		{name: "leading", root: "root", filePath: []string{"", "file"}, wantPath: "root/_/file"},
		{name: "middle", root: "root", filePath: []string{"dir", "", "file"}, wantPath: "root/dir/_/file"},
		{name: "trailing", root: "root", filePath: []string{"dir", ""}, wantPath: "root/dir/_"},
		{name: "consecutive", root: "root", filePath: []string{"dir", "", "", "file"}, wantPath: "root/dir/_/_/file"},
		{name: "underscore root", root: "_", filePath: []string{"", "file"}, wantPath: "_/_/file"},
		{name: "empty root", root: "", filePath: []string{"file"}, wantPath: "_/file"},
		{name: "empty root and component", root: "", filePath: []string{"", "file"}, wantPath: "_/_/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrentBytes := buildTorrentBytes(t, &metainfo.Info{
				Name:        tt.root,
				PieceLength: 262144,
				Files: []metainfo.FileInfo{
					{Path: tt.filePath, Length: 1},
				},
			})

			parsed, err := ParseTorrentBytes(torrentBytes)
			require.NoError(t, err)
			require.Equal(t, tt.wantPath, parsed.Files[0].Path)
		})
	}
}

func TestParseTorrentBytes_MultiFileKeepsDuplicatePadFiles(t *testing.T) {
	// libtorrent 2.0 names canonical pad files after their size, so equally sized payload
	// files produce two identical ".pad/<size>" entries. libtorrent allows that collision
	// (torrent_info.cpp: "pad files are allowed to collide with each-other, as long as they
	// have the same size"), so parsing must not reject the torrent either.
	torrentBytes := buildTorrentBytes(t, &metainfo.Info{
		Name:        "Pack",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"part.r00"}, Length: 100000},
			{Path: []string{".pad", "162144"}, Length: 162144},
			{Path: []string{"part.r01"}, Length: 100000},
			{Path: []string{".pad", "162144"}, Length: 162144},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Len(t, parsed.Files, 4)
	require.Equal(t, "Pack/.pad/162144", parsed.Files[1].Path)
	require.Equal(t, "Pack/.pad/162144", parsed.Files[3].Path)
}

func TestMatcher_Strict_NormalizesFilenames(t *testing.T) {
	matcher := NewMatcher(MatchModeStrict, 0)

	tests := []struct {
		name        string
		searcheeRel string
		torrentPath string
	}{
		{
			name:        "dots vs spaces",
			searcheeRel: "Movie.2023.2160p.REMUX.mkv",
			torrentPath: "Movie 2023 2160p REMUX.mkv",
		},
		{
			name:        "underscores and brackets",
			searcheeRel: "Movie_2023_[2160p]_(REMUX).mkv",
			torrentPath: "Movie 2023 2160p REMUX.mkv",
		},
		{
			name:        "TRaSH tags removed",
			searcheeRel: "Movie.2023.{tmdb-12345}.REMUX.mkv",
			torrentPath: "Movie 2023 REMUX.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searchee := &Searchee{
				Name: tt.searcheeRel,
				Files: []*ScannedFile{{
					RelPath: tt.searcheeRel,
					Size:    123,
				}},
			}
			torrentFiles := []TorrentFile{{
				Path: tt.torrentPath,
				Size: 123,
			}}

			res := matcher.Match(searchee, torrentFiles)
			require.True(t, res.IsPerfectMatch)
			require.True(t, res.IsMatch)
			require.Len(t, res.MatchedFiles, 1)
		})
	}
}

func TestMatcher_Flexible_RequiresExactFileSize(t *testing.T) {
	matcher := NewMatcher(MatchModeFlexible, 5)

	searchee := &Searchee{
		Name: "EPiC.Elvis.Presley.in.Concert.2025",
		Files: []*ScannedFile{{
			RelPath: "EPiC.Elvis.Presley.in.Concert.2025.NORDiC.1080p.AMZN.WEB-DL.H.264-NORViNE.mkv",
			Size:    1040,
		}},
	}
	torrentFiles := []TorrentFile{{
		Path: "EPiC.Elvis.Presley.in.Concert.2025.1080p.AMZN.WEB-DL.H.264-NTb.mkv",
		Size: 1000,
	}}

	res := matcher.Match(searchee, torrentFiles)
	require.False(t, res.IsMatch)
	require.False(t, res.IsPerfectMatch)
	require.Empty(t, res.MatchedFiles)
	require.Len(t, res.UnmatchedSearcheeFiles, 1)
	require.Len(t, res.UnmatchedTorrentFiles, 1)
}
