// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"bytes"
	"encoding/json"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

var nestRule = qbittorrent.CategoryPathRule{Nest: true}

func TestBuildCategorySavePaths(t *testing.T) {
	tests := []struct {
		name        string
		categories  map[string]qbt.Category
		defaultPath string
		rule        qbittorrent.CategoryPathRule
		want        map[string]string
	}{
		{
			name:        "uncategorized uses default save path",
			defaultPath: "/downloads",
			want:        map[string]string{"": "/downloads"},
		},
		{
			name: "absolute, relative and empty paths",
			categories: map[string]qbt.Category{
				"movies": {Name: "movies", SavePath: "/media/movies"},
				"tv":     {Name: "tv", SavePath: "shows"},
				"music":  {Name: "music"},
			},
			defaultPath: "/downloads/",
			want: map[string]string{
				"":       "/downloads",
				"movies": "/media/movies",
				"tv":     "/downloads/shows",
				"music":  "/downloads/music",
			},
		},
		{
			name: "empty subcategory path uses the full name when paths do not nest (qBittorrent 4.x, or 5.0/5.1 with subcategories off)",
			categories: map[string]qbt.Category{
				"tv":          {Name: "tv", SavePath: "/media/tv"},
				"tv/anime":    {Name: "tv/anime"},
				"tv/rel":      {Name: "tv/rel", SavePath: "relsub"},
				"movies:hd/x": {Name: "movies:hd/x"},
			},
			defaultPath: "/downloads",
			want: map[string]string{
				"":            "/downloads",
				"tv":          "/media/tv",
				"tv/anime":    "/downloads/tv/anime",
				"tv/rel":      "/downloads/relsub",
				"movies:hd/x": "/downloads/movies hd/x",
			},
		},
		{
			name: "empty subcategory path nests under the parent's resolved path",
			rule: nestRule,
			categories: map[string]qbt.Category{
				"tv":        {Name: "tv", SavePath: "/media/tv"},
				"tv/anime":  {Name: "tv/anime"},
				"tv/docs":   {Name: "tv/docs", SavePath: "documentaries"},
				"a/b/c":     {Name: "a/b/c"},
				"books/new": {Name: "books/new"},
				"books":     {Name: "books", SavePath: "library"},
				"tv/a:b":    {Name: "tv/a:b"},
			},
			defaultPath: "/downloads",
			want: map[string]string{
				"":          "/downloads",
				"tv":        "/media/tv",
				"tv/anime":  "/media/tv/anime",
				"tv/docs":   "/downloads/documentaries",
				"a/b/c":     "/downloads/a/b/c",
				"books":     "/downloads/library",
				"books/new": "/downloads/library/new",
				"tv/a:b":    "/media/tv/a b",
			},
		},
		{
			name: "trailing separators as qBittorrent reports them are dropped",
			rule: nestRule,
			categories: map[string]qbt.Category{
				"tvs":       {Name: "tvs", SavePath: "/media/tv/"},
				"tvs/anime": {Name: "tvs/anime"},
				"rel":       {Name: "rel", SavePath: "shows/"},
				"win":       {Name: "win", SavePath: `D:\media\`},
			},
			defaultPath: "/downloads/",
			want: map[string]string{
				"":          "/downloads",
				"tvs":       "/media/tv",
				"tvs/anime": "/media/tv/anime",
				"rel":       "/downloads/shows",
				"win":       `D:\media`,
			},
		},
		{
			name: "roots keep their separator",
			categories: map[string]qbt.Category{
				"music": {Name: "music"},
				"drive": {Name: "drive", SavePath: `D:\`},
			},
			defaultPath: "/",
			want: map[string]string{
				"":      "/",
				"music": "/music",
				"drive": `D:\`,
			},
		},
		{
			name: "qBittorrent 5.3+ on linux keeps characters a linux file name allows",
			rule: qbittorrent.CategoryPathRule{Nest: true, FileName: true, Platform: "linux"},
			categories: map[string]qbt.Category{
				"tv":     {Name: "tv", SavePath: "/media/tv"},
				"tv/a:b": {Name: "tv/a:b"},
				"c:x":    {Name: "c:x"},
			},
			defaultPath: "/downloads",
			want: map[string]string{
				"":       "/downloads",
				"tv":     "/media/tv",
				"tv/a:b": "/media/tv/a:b",
				"c:x":    "/downloads/c:x",
			},
		},
		{
			name: "qBittorrent 5.3+ on windows pads reserved characters and device names",
			rule: qbittorrent.CategoryPathRule{Nest: true, FileName: true, Platform: "windows"},
			categories: map[string]qbt.Category{
				"tv":     {Name: "tv", SavePath: `D:\media\tv`},
				"tv/a:b": {Name: "tv/a:b"},
				"tv/CON": {Name: "tv/CON"},
			},
			defaultPath: `C:\Downloads`,
			want: map[string]string{
				"":       `C:\Downloads`,
				"tv":     `D:\media\tv`,
				"tv/a:b": `D:\media\tv\a_b`,
				"tv/CON": `D:\media\tv\CON_1`,
			},
		},
		{
			name: "windows default save path",
			categories: map[string]qbt.Category{
				"movies": {Name: "movies"},
				"tv":     {Name: "tv", SavePath: `D:\media\tv`},
				"unc":    {Name: "unc", SavePath: `\\nas\share`},
			},
			defaultPath: `C:\Downloads\`,
			want: map[string]string{
				"":       `C:\Downloads`,
				"movies": `C:\Downloads\movies`,
				"tv":     `D:\media\tv`,
				"unc":    `\\nas\share`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, buildCategorySavePaths(tt.categories, tt.defaultPath, tt.rule))
		})
	}
}

func TestTrimTrailingSeparators(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"/":             "/",
		"//":            "/",
		"/downloads/":   "/downloads",
		"/downloads//":  "/downloads",
		"/downloads":    "/downloads",
		"shows/":        "shows",
		`\`:             `\`,
		`C:\`:           `C:\`,
		"C:/":           "C:/",
		"C:":            "C:",
		`C:\Downloads\`: `C:\Downloads`,
		`\\nas\share\`:  `\\nas\share`,
	}
	for in, want := range tests {
		require.Equal(t, want, trimTrailingSeparators(in), "input %q", in)
	}
}

func TestResolveMovePath_TrailingSeparatorSavePaths(t *testing.T) {
	evalCtx := &EvalContext{CategorySavePaths: buildCategorySavePaths(map[string]qbt.Category{
		"tvs": {Name: "tvs", SavePath: "/media/tv/"},
	}, "/downloads/", nestRule)}
	for path, want := range map[string]string{
		"{{.DefaultSavePath}}/x":  "/downloads/x",
		"{{.CategorySavePath}}/x": "/media/tv/x",
	} {
		got, ok := renderPathTemplate(path, qbt.Torrent{Hash: "abc", Name: "Show.S01", Category: "tvs"}, nil, evalCtx)
		require.True(t, ok, path)
		require.Equal(t, want, got, path)
	}
}

func TestResolveMovePath_CategorySavePath(t *testing.T) {
	evalCtx := &EvalContext{CategorySavePaths: buildCategorySavePaths(map[string]qbt.Category{
		"tv":       {Name: "tv", SavePath: "/media/tv"},
		"tv/anime": {Name: "tv/anime"},
		"a/b":      {Name: "a/b"},
	}, "/downloads", nestRule)}

	tests := []struct {
		name     string
		category string
		evalCtx  *EvalContext
		want     string
		wantOK   bool
	}{
		{name: "uncategorized", category: "", evalCtx: evalCtx, want: "/downloads/done", wantOK: true},
		{name: "nested empty path", category: "tv/anime", evalCtx: evalCtx, want: "/media/tv/anime/done", wantOK: true},
		{name: "unknown category skips the move", category: "gone", evalCtx: evalCtx},
		{name: "implicit parent is not a category", category: "a", evalCtx: evalCtx},
		{name: "map not loaded skips the move", category: "tv", evalCtx: &EvalContext{}},
		{name: "no eval context skips the move", category: "tv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrent := qbt.Torrent{Hash: "abc", Name: "Show.S01", Category: tt.category}
			got, ok := renderPathTemplate("{{.CategorySavePath}}/done", torrent, nil, tt.evalCtx)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveMovePath_DefaultSavePath(t *testing.T) {
	evalCtx := &EvalContext{CategorySavePaths: buildCategorySavePaths(map[string]qbt.Category{
		"tv": {Name: "tv", SavePath: "/media/tv"},
	}, "/downloads", nestRule)}

	for _, category := range []string{"", "tv", "gone"} {
		torrent := qbt.Torrent{Hash: "abc", Name: "Show.S01", Category: category}
		got, ok := renderPathTemplate("{{.DefaultSavePath}}/archive", torrent, nil, evalCtx)
		require.True(t, ok, category)
		require.Equal(t, "/downloads/archive", got, category)
	}

	_, ok := renderPathTemplate("{{.DefaultSavePath}}/archive", qbt.Torrent{Hash: "abc"}, nil, &EvalContext{})
	require.False(t, ok, "map not loaded skips the move")
}

func TestRenderPathTemplate_LogLevel(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Logger
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = previous })

	evalCtx := &EvalContext{CategorySavePaths: map[string]string{"": "/downloads"}}
	for path, want := range map[string]string{
		"{{.CategorySavePath}}/done": "debug",
		"/data/{{ .Name":             "error",
	} {
		logs.Reset()
		_, ok := renderPathTemplate(path, qbt.Torrent{Hash: "abc", Category: "gone"}, nil, evalCtx)
		require.False(t, ok, path)
		var entry struct{ Level string }
		require.NoError(t, json.Unmarshal(logs.Bytes(), &entry), path)
		require.Equal(t, want, entry.Level, path)
	}
}

func TestNeedsForSavePaths(t *testing.T) {
	rule := func(enabled bool, conds *models.ActionConditions) *models.Automation {
		return &models.Automation{Enabled: enabled, Conditions: conds}
	}
	move := func(enabled bool, path string) *models.ActionConditions {
		return &models.ActionConditions{Move: &models.MoveAction{Enabled: enabled, Path: path}}
	}
	exportPath := &models.ActionConditions{ExportToInstance: &models.ExportToInstanceAction{Enabled: true, SavePath: "{{ .CategorySavePath }}"}}

	tests := []struct {
		name string
		rule *models.Automation
		want bool
	}{
		{name: "move uses CategorySavePath", rule: rule(true, move(true, "{{.CategorySavePath}}/done")), want: true},
		{name: "move uses DefaultSavePath", rule: rule(true, move(true, "{{.DefaultSavePath}}/archive")), want: true},
		{name: "export uses CategorySavePath", rule: rule(true, exportPath), want: true},
		{name: "disabled rule", rule: rule(false, move(true, "{{.CategorySavePath}}/done"))},
		{name: "disabled move action", rule: rule(true, move(false, "{{.CategorySavePath}}/done"))},
		{name: "other variables only", rule: rule(true, move(true, "/data/{{.Category}}"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NeedsFor([]*models.Automation{tt.rule}).SavePaths)
		})
	}
}
