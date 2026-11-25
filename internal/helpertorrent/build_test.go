// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package helpertorrent_test

import (
	"bytes"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/helpertorrent"
)

const pieceLength = 1 << 18

func TestBuildGivesTheHelperItsOwnInfoHash(t *testing.T) {
	for _, tc := range []struct {
		name    string
		private bool
	}{
		{name: "public original", private: false},
		{name: "private original", private: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := map[string]any{
				"name":         "Some.Movie.2024.mkv",
				"length":       3 * pieceLength,
				"piece length": pieceLength,
				"pieces":       pieceHashes(3),
			}
			if tc.private {
				info["private"] = 1
			}
			original := encodeTorrent(t, map[string]any{"info": info})

			helper, err := helpertorrent.Build(original, []string{"Some.Movie.2024.mkv"}, "salt-1")
			require.NoError(t, err)

			assert.NotEqual(t, infoHashOf(t, original), helper.InfoHash)
			assert.Equal(t, infoHashOf(t, helper.TorrentFile), helper.InfoHash)
			assert.Equal(t, int64(1), decodeDict(t, helper.TorrentFile)["info"].(map[string]any)["private"])
		})
	}
}

func TestBuildKeepsTheInfoHashForTheSameSalt(t *testing.T) {
	original := singleFileTorrent(t)

	first, err := helpertorrent.Build(original, []string{"Some.Movie.2024.mkv"}, "salt-1")
	require.NoError(t, err)
	again, err := helpertorrent.Build(original, []string{"Some.Movie.2024.mkv"}, "salt-1")
	require.NoError(t, err)
	otherSalt, err := helpertorrent.Build(original, []string{"Some.Movie.2024.mkv"}, "salt-2")
	require.NoError(t, err)

	assert.Equal(t, first.InfoHash, again.InfoHash)
	assert.Equal(t, first.TorrentFile, again.TorrentFile)
	assert.NotEqual(t, first.InfoHash, otherSalt.InfoHash)
}

func TestBuildKeepsOnlyTheKeepList(t *testing.T) {
	original := encodeTorrent(t, map[string]any{
		"announce":      "https://tracker.example.invalid/announce/passkey",
		"announce-list": []any{[]any{"https://tracker.example.invalid/announce/passkey"}},
		"comment":       "https://tracker.example.invalid/torrents/123",
		"created by":    "mkbrr",
		"creation date": 1700000000,
		"encoding":      "UTF-8",
		"url-list":      []any{"https://seed.example.invalid/files/"},
		"httpseeds":     []any{"https://seed.example.invalid/seed"},
		"x-unknown":     "anything",
		"info": map[string]any{
			"name":         "Some.Show.S01",
			"name.utf-8":   "Some.Show.S01",
			"piece length": pieceLength,
			"pieces":       pieceHashes(2),
			"private":      1,
			"source":       "TRK",
			"x_cross_seed": "mkbrr-tag",
			"x-unknown":    "anything",
			"files": []any{
				map[string]any{
					"length":     pieceLength,
					"path":       []any{"Some.Show.S01E01.mkv"},
					"path.utf-8": []any{"Some.Show.S01E01.mkv"},
					"md5sum":     "0123456789abcdef0123456789abcdef",
					"sha1":       "0123456789abcdefghij",
					"mtime":      1700000000,
				},
				map[string]any{
					"length": pieceLength,
					"path":   []any{"Some.Show.S01E02.mkv"},
				},
			},
		},
	})

	helper, err := helpertorrent.Build(original, []string{
		"Some.Show.S01/Some.Show.S01E01.mkv",
		"Some.Show.S01/Some.Show.S01E02.mkv",
	}, "salt-1")
	require.NoError(t, err)

	root := decodeDict(t, helper.TorrentFile)
	assert.ElementsMatch(t, []string{"info"}, keysOf(root))
	info := root["info"].(map[string]any)
	assert.ElementsMatch(t, []string{"name", "piece length", "pieces", "private", "source", "files"}, keysOf(info))
	assert.NotEqual(t, "TRK", info["source"])
	files, ok := info["files"].([]any)
	require.True(t, ok, "helper has no files list")
	for _, file := range files {
		assert.ElementsMatch(t, []string{"length", "path"}, keysOf(file.(map[string]any)))
	}
}

func TestBuildPlacesEachFileAtItsNameOnDisk(t *testing.T) {
	multiFile := encodeTorrent(t, map[string]any{
		"info": map[string]any{
			"name":         "Some.Show.S01",
			"piece length": pieceLength,
			"pieces":       pieceHashes(2),
			"files": []any{
				map[string]any{"length": pieceLength, "path": []any{"Some.Show.S01E01.mkv"}},
				map[string]any{"length": pieceLength, "path": []any{"Some.Show.S01E02.mkv"}},
			},
		},
	})

	for _, tc := range []struct {
		name      string
		original  []byte
		fileNames []string
		wantName  string
	}{
		{
			name:      "renamed single file",
			original:  singleFileTorrent(t),
			fileNames: []string{"Some.Movie.2024.Renamed.mkv"},
			wantName:  "Some.Movie.2024.Renamed.mkv",
		},
		{
			name:      "single file in a subfolder",
			original:  singleFileTorrent(t),
			fileNames: []string{"Some.Movie.2024/Some.Movie.2024.mkv"},
			wantName:  "Some.Movie.2024",
		},
		{
			name:      "renamed root folder and a file moved into a subfolder",
			original:  multiFile,
			fileNames: []string{"Renamed.Show/Some.Show.S01E01.mkv", "Renamed.Show/Extras/E02.mkv"},
			wantName:  "Renamed.Show",
		},
		{
			name:      "files without a root folder",
			original:  multiFile,
			fileNames: []string{"Some.Show.S01E01.mkv", "Extras/E02.mkv"},
			wantName:  "Some.Show.S01",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			helper, err := helpertorrent.Build(tc.original, tc.fileNames, "salt-1")
			require.NoError(t, err)

			assert.Equal(t, tc.fileNames, placedFileNames(t, helper))
			assert.Equal(t, tc.wantName, decodeInfo(t, helper.TorrentFile).Name)
		})
	}
}

func TestBuildKeepsPaddingFilesAndPieces(t *testing.T) {
	files := []any{
		map[string]any{"length": pieceLength - 100, "path": []any{"01.flac"}},
		map[string]any{"length": 100, "path": []any{".pad", "100"}, "attr": "p"},
		map[string]any{"length": pieceLength - 50, "path": []any{"02.flac"}, "attr": "x"},
		map[string]any{"length": 50, "path": []any{"_____padding_file_0_"}},
		map[string]any{"length": pieceLength, "path": []any{"03.flac"}},
	}
	original := encodeTorrent(t, map[string]any{
		"info": map[string]any{
			"name":         "Some.Album.2024",
			"piece length": pieceLength,
			"pieces":       pieceHashes(3),
			"files":        files,
		},
	})
	fileNames := []string{"Some.Album.2024/01.flac", "Some.Album.2024/02.flac", "Some.Album.2024/03.flac"}

	helper, err := helpertorrent.Build(original, fileNames, "salt-1")
	require.NoError(t, err)

	assert.Equal(t, fileNames, placedFileNames(t, helper))
	info := decodeDict(t, helper.TorrentFile)["info"].(map[string]any)
	assert.Equal(t, int64(pieceLength), info["piece length"])
	assert.Equal(t, string(pieceHashes(3)), info["pieces"])
	helperFiles := info["files"].([]any)
	require.Len(t, helperFiles, len(files))
	for i, file := range helperFiles {
		file := file.(map[string]any)
		want := files[i].(map[string]any)
		assert.Equal(t, int64(want["length"].(int)), file["length"], "length of file %d", i)
		assert.Equal(t, want["attr"], file["attr"], "attr of file %d", i)
	}
	assert.Equal(t, []any{".pad", "100"}, helperFiles[1].(map[string]any)["path"])
	assert.Equal(t, []any{"_____padding_file_0_"}, helperFiles[3].(map[string]any)["path"])
}

func TestBuildRefusesV2AndHybridTorrents(t *testing.T) {
	fileTree := map[string]any{
		"Some.Movie.2024.mkv": map[string]any{
			"": map[string]any{"length": 3 * pieceLength, "pieces root": strings.Repeat("r", 32)},
		},
	}
	for _, tc := range []struct {
		name string
		info map[string]any
	}{
		{
			name: "v2",
			info: map[string]any{
				"name":         "Some.Movie.2024.mkv",
				"piece length": pieceLength,
				"meta version": 2,
				"file tree":    fileTree,
			},
		},
		{
			name: "hybrid",
			info: map[string]any{
				"name":         "Some.Movie.2024.mkv",
				"piece length": pieceLength,
				"meta version": 2,
				"file tree":    fileTree,
				"length":       3 * pieceLength,
				"pieces":       pieceHashes(3),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := encodeTorrent(t, map[string]any{"info": tc.info})

			_, err := helpertorrent.Build(original, []string{"Some.Movie.2024.mkv"}, "salt-1")

			require.ErrorIs(t, err, helpertorrent.ErrV2Unsupported)
			assert.EqualError(t, err, "v2 and hybrid torrents are unsupported")
		})
	}
}

func TestBuildRefusesInputThatGivesAWrongHelper(t *testing.T) {
	original := encodeTorrent(t, map[string]any{
		"info": map[string]any{
			"name":         "Some.Show.S01",
			"piece length": pieceLength,
			"pieces":       pieceHashes(2),
			"files": []any{
				map[string]any{"length": pieceLength, "path": []any{"E01.mkv"}},
				map[string]any{"length": pieceLength, "path": []any{"E02.mkv"}},
			},
		},
	})
	_, err := helpertorrent.Build(original, []string{"Some.Show.S01/E01.mkv", "Some.Show.S01/E02.mkv"}, "salt-1")
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		fileNames []string
		salt      string
	}{
		{name: "fewer names than files", fileNames: []string{"Some.Show.S01/E01.mkv"}, salt: "salt-1"},
		{name: "parent folder", fileNames: []string{"Some.Show.S01/E01.mkv", "../E02.mkv"}, salt: "salt-1"},
		{name: "absolute POSIX path", fileNames: []string{"Some.Show.S01/E01.mkv", "/srv/E02.mkv"}, salt: "salt-1"},
		{name: "Windows separator", fileNames: []string{"Some.Show.S01/E01.mkv", `Some.Show.S01\E02.mkv`}, salt: "salt-1"},
		{name: "Windows drive", fileNames: []string{"Some.Show.S01/E01.mkv", "C:/E02.mkv"}, salt: "salt-1"},
		{name: "empty folder name", fileNames: []string{"Some.Show.S01/E01.mkv", "Some.Show.S01//E02.mkv"}, salt: "salt-1"},
		{name: "dot as a file name", fileNames: []string{"Some.Show.S01/E01.mkv", "."}, salt: "salt-1"},
		{name: "no salt", fileNames: []string{"Some.Show.S01/E01.mkv", "Some.Show.S01/E02.mkv"}, salt: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := helpertorrent.Build(original, tc.fileNames, tc.salt)
			assert.Error(t, err)
		})
	}
}

func singleFileTorrent(t *testing.T) []byte {
	t.Helper()
	return encodeTorrent(t, map[string]any{
		"announce": "https://tracker.example.invalid/announce/passkey",
		"info": map[string]any{
			"name":         "Some.Movie.2024.mkv",
			"length":       3 * pieceLength,
			"piece length": pieceLength,
			"pieces":       pieceHashes(3),
			"private":      1,
			"source":       "TRK",
		},
	})
}

func encodeTorrent(t *testing.T, torrent map[string]any) []byte {
	t.Helper()
	data, err := bencode.Marshal(torrent)
	require.NoError(t, err)
	return data
}

func placedFileNames(t *testing.T, helper helpertorrent.Helper) []string {
	t.Helper()
	info := decodeInfo(t, helper.TorrentFile)
	if !info.IsDir() {
		require.Equal(t, "Original", helper.ContentLayout)
		return []string{info.Name}
	}

	var names []string
	for _, file := range info.Files {
		if strings.Contains(file.Attr, "p") || strings.Contains(path.Join(file.Path...), "_____padding_file_") {
			continue
		}
		names = append(names, path.Join(info.Name, path.Join(file.Path...)))
	}
	switch helper.ContentLayout {
	case "Original":
		return names
	case "NoSubfolder":
		root, _, _ := strings.Cut(names[0], "/")
		for i, name := range names {
			rest, ok := strings.CutPrefix(name, root+"/")
			require.True(t, ok, "%q has no root folder %q", name, root)
			names[i] = rest
		}
		return names
	default:
		t.Fatalf("unexpected content layout %q", helper.ContentLayout)
		return nil
	}
}

func decodeInfo(t *testing.T, data []byte) metainfo.Info {
	t.Helper()
	mi, err := metainfo.Load(bytes.NewReader(data))
	require.NoError(t, err)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	return info
}

func decodeDict(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var dict map[string]any
	require.NoError(t, bencode.Unmarshal(data, &dict))
	return dict
}

func keysOf(dict map[string]any) []string {
	return slices.Collect(maps.Keys(dict))
}

func pieceHashes(n int) []byte {
	return bytes.Repeat([]byte("0123456789abcdefghij"), n)
}

func infoHashOf(t *testing.T, data []byte) string {
	t.Helper()
	mi, err := metainfo.Load(bytes.NewReader(data))
	require.NoError(t, err)
	return mi.HashInfoBytes().HexString()
}
