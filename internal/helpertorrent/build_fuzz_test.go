// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package helpertorrent_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/helpertorrent"
)

func isPlainPart(part string) bool {
	return part != "" && part != "." && part != ".." && !strings.ContainsAny(part, `/\`)
}

func startsWithDriveLetter(name string) bool {
	return len(name) >= 2 && name[1] == ':' && strings.ContainsRune("abcdefghijklmnopqrstuvwxyz", rune(name[0]|0x20))
}

func FuzzBuildPlacesEachFileAtItsName(f *testing.F) {
	for _, seed := range [][2]string{
		{"Some.Show.S01/E01.mkv", "Some.Show.S01/E02.mkv"},
		{"Some.Show.S01/E01.mkv", "Some.Show.S01/Extras/E02.mkv"},
		{"E01.mkv", "Extras/E02.mkv"},
		{"Show/E01.mkv", "Show"},
		{"x", "x"},
		{"/a", "b"},
		{"C:/x", "b"},
		{"C:x", "b"},
		{"c:", "b"},
		{"1:x", "b"},
		{"../a", "b"},
		{"a/../b", "b"},
		{"a/..", "b"},
		{"a//b", "b"},
		{"a/", "b"},
		{"./a", "b"},
		{".", "."},
		{"", ""},
		{`a\b`, "c"},
		{"..a", "b.."},
		{"...", "..."},
		{"a", "_____padding_file_"},
		{"_____padding_file_", "_____padding_file_x"},
	} {
		f.Add(seed[0], seed[1])
	}
	roots := []struct {
		name string
		d    fsops.PathDialect
		root string
	}{
		{"slash", fsops.SlashPaths, "/downloads"},
		{"windows", fsops.WindowsPaths, `C:\downloads`},
	}
	f.Fuzz(func(t *testing.T, first, second string) {
		singleFile := singleFileTorrent(t)
		multiFile := encodeTorrent(t, map[string]any{
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
		plain := isPlainPart(first) && isPlainPart(second) && !startsWithDriveLetter(first)
		for _, tc := range []struct {
			original  []byte
			fileNames []string
			mustBuild bool
		}{
			{singleFile, []string{first}, false},
			{multiFile, []string{first, second}, false},
			{singleFile, []string{first}, plain},
			{multiFile, []string{first + "/" + second, first}, plain},
			{multiFile, []string{first + "/" + second, first + "/" + first}, plain},
		} {
			helper, err := helpertorrent.Build(tc.original, tc.fileNames, "salt-1")
			if err != nil {
				if tc.mustBuild {
					t.Fatalf("the file names %q are refused: %v", tc.fileNames, err)
				}
				continue
			}
			for _, name := range tc.fileNames {
				if strings.HasPrefix(name, "/") || startsWithDriveLetter(name) {
					t.Fatalf("%q is accepted, and it is absolute or on a drive", name)
				}
				for _, r := range roots {
					rel, err := r.d.Rel(r.root, r.d.Join(r.root, name))
					if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+r.d.Separator()) {
						t.Fatalf("%s: %q is accepted, and it leaves the folder %q", r.name, name, r.root)
					}
				}
				if slices.ContainsFunc(strings.Split(name, "/"), func(part string) bool { return !isPlainPart(part) }) {
					t.Fatalf("%q is accepted, and one of its parts is no file or folder name", name)
				}
			}
			info := decodeInfo(t, helper.TorrentFile)
			if !isPlainPart(info.Name) {
				t.Fatalf("the helper for %q has the name %q", tc.fileNames, info.Name)
			}
			for _, file := range info.Files {
				if len(file.Path) == 0 || slices.ContainsFunc(file.Path, func(part string) bool { return !isPlainPart(part) }) {
					t.Fatalf("the helper for %q holds the path %q", tc.fileNames, file.Path)
				}
			}
			if slices.ContainsFunc(tc.fileNames, func(name string) bool { return strings.Contains(name, "_____padding_file_") }) {
				continue
			}
			if placed := placedFileNames(t, helper); !slices.Equal(placed, tc.fileNames) {
				t.Fatalf("the helper for %q places its files at %q", tc.fileNames, placed)
			}
		}
	})
}
