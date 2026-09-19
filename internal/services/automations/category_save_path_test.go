// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildCategorySavePaths(t *testing.T) {
	tests := []struct {
		name        string
		categories  map[string]qbt.Category
		defaultPath string
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
				"":       "/downloads/",
				"movies": "/media/movies",
				"tv":     "/downloads/shows",
				"music":  "/downloads/music",
			},
		},
		{
			name: "empty subcategory path nests under the parent's resolved path",
			categories: map[string]qbt.Category{
				"tv":        {Name: "tv", SavePath: "/media/tv"},
				"tv/anime":  {Name: "tv/anime"},
				"tv/docs":   {Name: "tv/docs", SavePath: "documentaries"},
				"a/b/c":     {Name: "a/b/c"},
				"books/new": {Name: "books/new"},
				"books":     {Name: "books", SavePath: "library"},
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
				"":       `C:\Downloads\`,
				"movies": `C:\Downloads\movies`,
				"tv":     `D:\media\tv`,
				"unc":    `\\nas\share`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, buildCategorySavePaths(tt.categories, tt.defaultPath))
		})
	}
}

func TestResolveMovePath_CategorySavePath(t *testing.T) {
	evalCtx := &EvalContext{CategorySavePaths: buildCategorySavePaths(map[string]qbt.Category{
		"tv":       {Name: "tv", SavePath: "/media/tv"},
		"tv/anime": {Name: "tv/anime"},
		"a/b":      {Name: "a/b"},
	}, "/downloads")}

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
			got, ok := resolveMovePath("{{.CategorySavePath}}/done", torrent, nil, tt.evalCtx)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveMovePath_DefaultSavePath(t *testing.T) {
	evalCtx := &EvalContext{CategorySavePaths: buildCategorySavePaths(map[string]qbt.Category{
		"tv": {Name: "tv", SavePath: "/media/tv"},
	}, "/downloads")}

	for _, category := range []string{"", "tv", "gone"} {
		torrent := qbt.Torrent{Hash: "abc", Name: "Show.S01", Category: category}
		got, ok := resolveMovePath("{{.DefaultSavePath}}/archive", torrent, nil, evalCtx)
		require.True(t, ok, category)
		require.Equal(t, "/downloads/archive", got, category)
	}

	_, ok := resolveMovePath("{{.DefaultSavePath}}/archive", qbt.Torrent{Hash: "abc"}, nil, &EvalContext{})
	require.False(t, ok, "map not loaded skips the move")
}

func TestRulesUseSavePathVariables(t *testing.T) {
	rule := func(enabled bool, conds *models.ActionConditions) *models.Automation {
		return &models.Automation{Enabled: enabled, Conditions: conds}
	}
	movePath := &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "{{.CategorySavePath}}/done"}}
	exportPath := &models.ActionConditions{ExportToInstance: &models.ExportToInstanceAction{Enabled: true, SavePath: "{{ .CategorySavePath }}"}}

	require.True(t, rulesUseSavePathVariables([]*models.Automation{rule(true, movePath)}))
	require.True(t, rulesUseSavePathVariables([]*models.Automation{rule(true, exportPath)}))
	require.False(t, rulesUseSavePathVariables([]*models.Automation{rule(false, movePath)}))
	require.False(t, rulesUseSavePathVariables([]*models.Automation{rule(true, &models.ActionConditions{Move: &models.MoveAction{Enabled: false, Path: "{{.CategorySavePath}}"}})}))
	require.False(t, rulesUseSavePathVariables([]*models.Automation{rule(true, &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/data/{{.Category}}"}})}))
	require.True(t, rulesUseSavePathVariables([]*models.Automation{rule(true, &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "{{.DefaultSavePath}}/archive"}})}))
}
