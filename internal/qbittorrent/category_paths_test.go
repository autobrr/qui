// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

// The expected folders are what qBittorrent's SessionImpl::categorySavePath
// gives: toValidPath on 5.2, toValidFileName on 5.3.
func TestCategorySavePath_FolderName(t *testing.T) {
	t.Parallel()

	v52 := CategoryPathRule{Nest: true}
	linux := CategoryPathRule{Nest: true, FileName: true, Platform: "linux"}
	windows := CategoryPathRule{Nest: true, FileName: true, Platform: "windows"}
	macos := CategoryPathRule{Nest: true, FileName: true, Platform: "macos"}

	tests := []struct {
		name     string
		category string
		rule     CategoryPathRule
		want     string
	}{
		{"5.2 replaces a run of reserved chars with a space", "movies:hd", v52, "/data/movies hd"},
		{"5.2 keeps outer spaces", "tv/ anime ", v52, "/data/tv/ anime "},
		{"5.2 subcategory ending in a slash is the parent", "tv/", v52, "/data/tv"},
		{"4.6 converts the full name", "tv/a:b", CategoryPathRule{}, "/data/tv/a b"},

		{"linux keeps the chars windows reserves", `movies:hd?"*<>|`, linux, `/data/movies:hd?"*<>|`},
		{"linux trims spaces", "tv/ anime ", linux, "/data/tv/anime"},
		{"linux pads a control char", "a\tb\x7fc", linux, "/data/a_b_c"},
		{"linux pads a dot-dot name", "tv/..", linux, "/data/tv/_"},
		{"subcategory ending in a slash is the parent", "tv/", linux, "/data/tv"},
		{"windows replaces a run with one pad", `movies:?hd`, windows, "/data/movies_hd"},
		{"windows adds no pad after an underscore", `a_:b`, windows, "/data/a_b"},
		{"windows replaces a backslash", `a\b`, windows, "/data/a_b"},
		{"windows strips trailing dots and spaces", "tv/anime. .", windows, "/data/tv/anime"},
		{"windows renames a device name", "tv/con", windows, "/data/tv/con_1"},
		{"windows renames a device name with an extension", "tv/LPT1.old", windows, "/data/tv/LPT1_1.old"},
		{"macos reserves only the colon", `movies:hd?`, macos, "/data/movies_hd?"},
		{"unknown platform follows linux", "movies:hd", CategoryPathRule{Nest: true, FileName: true, Platform: "unknown"}, "/data/movies:hd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			categories := map[string]qbt.Category{tt.category: {}}
			require.Equal(t, tt.want, CategorySavePath(fsops.SlashPaths, tt.category, categories, "/data", tt.rule))
		})
	}
}

// The parent walk has no depth cap: a deeply nested category that resolved to
// "" would lose its folder.
func TestCategorySavePath_DeepInheritanceIsNotDropped(t *testing.T) {
	t.Parallel()

	categories := map[string]qbt.Category{"top": {Name: "top", SavePath: "/archive"}}
	name := "top"
	want := "/archive"
	for i := range 40 {
		segment := fmt.Sprintf("s%d", i)
		name += "/" + segment
		want += "/" + segment
		categories[name] = qbt.Category{Name: name}
	}

	require.Equal(t, want, CategorySavePath(fsops.SlashPaths, name, categories, "/torrents", CategoryPathRule{Nest: true}))
}
