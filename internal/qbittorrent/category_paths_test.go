// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
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

// The expected folders are what qBittorrent 4.6.7, 5.1.4, 5.2.4 and
// 5.4.0alpha1 reported as an automatically managed torrent's download_path
// for the same categories, with the download path /dl/temp. qBittorrent
// never produces a parent missing from the map or a relative download path
// itself, so those rows follow its source.
func TestCategoryDownloadPath(t *testing.T) {
	t.Parallel()

	v46 := CategoryPathRule{}
	flat := CategoryPathRule{InheritsDownloadPath: true}
	nests := CategoryPathRule{Nest: true, InheritsDownloadPath: true}
	linux := CategoryPathRule{Nest: true, FileName: true, Platform: "linux", InheritsDownloadPath: true}
	on := func(p string) *qbt.CategoryDownloadPath { return &qbt.CategoryDownloadPath{Enabled: true, Path: p} }
	off := &qbt.CategoryDownloadPath{}
	none := "no download path"

	tests := []struct {
		name         string
		rule         CategoryPathRule
		downloadPath string
		enabled      bool
		categories   map[string]*qbt.CategoryDownloadPath
		category     string
		want         string
	}{
		{"4.6 inherit is the download path itself", v46, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": nil}, "tv", "/dl/temp"},
		{"4.6 inherit with the download path off", v46, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": nil}, "tv", none},
		{"4.6 turned off", v46, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": off}, "tv", none},
		{"4.6 own relative path under the download path even when it is off", v46, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("inc")}, "tv", "/dl/temp/inc"},
		{"4.6 own empty path is named after the category", v46, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("")}, "tv", "/dl/temp/tv"},
		{"4.6 own absolute path", v46, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv")}, "tv", "/fast/tv"},
		{"4.6 subcategory ignores its parent", v46, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast"), "tv/hd": on("")}, "tv/hd", "/dl/temp/tv/hd"},

		{"5.1 subcategories off, inherit is named under the download path", flat, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": nil}, "tv", "/dl/temp/tv"},
		{"5.1 subcategories off, subcategory ignores its parent's path", flat, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv"), "tv/hd": nil}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.1 subcategories off, inherit with the download path off", flat, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv"), "tv/hd": nil}, "tv/hd", none},
		{"5.1 subcategories off, parent turned off does not apply", flat, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": off, "tv/hd": nil}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.1 subcategories off, empty path is the full name", flat, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv"), "tv/hd": on("")}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.1 subcategories off, relative path under the download path", flat, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("inc"), "tv/hd": on("x")}, "tv/hd", "/dl/temp/x"},
		{"5.1 subcategories on, inherit takes the parent's path", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv"), "tv/hd": nil}, "tv/hd", "/fast/tv"},
		{"5.1 subcategories on, relative path under the parent's", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("inc"), "tv/hd": on("x")}, "tv/hd", "/dl/temp/inc/x"},

		{"5.2 inherit with the download path off", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": nil}, "tv", none},
		{"5.2 turned off", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": off}, "tv", none},
		{"5.2 empty path under the download path even when it is off", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("")}, "tv", "/dl/temp/tv"},
		{"5.2 relative path under the download path even when it is off", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("inc")}, "tv", "/dl/temp/inc"},
		{"5.2 relative path is cleaned", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("../inc")}, "tv", "/dl/inc"},
		{"5.2 subcategory inherit nests under the parent", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": nil, "tv/hd": nil}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.2 inherit takes the parent's path with the download path off", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv"), "tv/hd": nil}, "tv/hd", "/fast/tv"},
		{"5.2 inherit takes the parent's relative path twice", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": on("inc"), "tv/hd": nil}, "tv/hd", "/dl/temp/inc/inc"},
		{"5.2 inherit takes the parent's off", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv": off, "tv/hd": nil}, "tv/hd", none},
		{"5.2 parent turned off still names a folder", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": off, "tv/hd": on("")}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.2 empty path under the parent's path", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv"), "tv/hd": on("")}, "tv/hd", "/fast/tv/hd"},
		{"5.2 parent that inherits still names a folder", nests, "/dl/temp", false, map[string]*qbt.CategoryDownloadPath{"tv": nil, "tv/hd": on("")}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.2 parent missing from the map", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"tv/hd": nil}, "tv/hd", "/dl/temp/tv/hd"},
		{"5.2 names the folder with toValidPath", nests, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"movies:hd": nil}, "movies:hd", "/dl/temp/movies hd"},
		{"5.3 names the folder with toValidFileName", linux, "/dl/temp", true, map[string]*qbt.CategoryDownloadPath{"movies:hd": nil}, "movies:hd", "/dl/temp/movies:hd"},

		{"a relative download path cannot be resolved", nests, "dl", true, map[string]*qbt.CategoryDownloadPath{"tv": nil}, "tv", ""},
		{"an absolute own path needs no download path", nests, "dl", true, map[string]*qbt.CategoryDownloadPath{"tv": on("/fast/tv")}, "tv", "/fast/tv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			categories := make(map[string]qbt.Category, len(tt.categories))
			for name, option := range tt.categories {
				categories[name] = qbt.Category{Name: name, DownloadPath: option}
			}
			got, ok := CategoryDownloadPath(fsops.SlashPaths, tt.category, categories, tt.downloadPath, tt.enabled, tt.rule)
			if tt.want == none {
				require.False(t, ok, "got %q", got)
				return
			}
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
