// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release *rls.Release
		want    ContentType
	}{
		{name: "nil release", release: nil, want: ContentTypeUnknown},
		{name: "movie", release: &rls.Release{Type: rls.Movie}, want: ContentTypeMovie},
		{name: "episode", release: &rls.Release{Type: rls.Episode}, want: ContentTypeTV},
		{name: "series", release: &rls.Release{Type: rls.Series}, want: ContentTypeTV},
		{name: "music", release: &rls.Release{Type: rls.Music}, want: ContentTypeMusic},
		{name: "audiobook", release: &rls.Release{Type: rls.Audiobook}, want: ContentTypeAudiobook},
		{name: "book", release: &rls.Release{Type: rls.Book}, want: ContentTypeBook},
		{name: "education counts as book", release: &rls.Release{Type: rls.Education}, want: ContentTypeBook},
		{name: "magazine counts as book", release: &rls.Release{Type: rls.Magazine}, want: ContentTypeBook},
		{name: "comic", release: &rls.Release{Type: rls.Comic}, want: ContentTypeComic},
		{name: "game", release: &rls.Release{Type: rls.Game}, want: ContentTypeGame},
		{name: "app", release: &rls.Release{Type: rls.App}, want: ContentTypeApp},
		{name: "untyped release with a season is tv", release: &rls.Release{Series: 2}, want: ContentTypeTV},
		{name: "untyped release with an episode is tv", release: &rls.Release{Episode: 5}, want: ContentTypeTV},
		{name: "untyped release with a year is a movie", release: &rls.Release{Year: 2020}, want: ContentTypeMovie},
		{name: "untyped release with nothing to go on", release: &rls.Release{}, want: ContentTypeUnknown},
		{name: "xxx token", release: &rls.Release{Title: "Some Studio XXX Scene"}, want: ContentTypeAdult},
		// The 3rd letter must not be a RIAJ medium, or the code reads as a music release.
		{name: "jav code alone", release: &rls.Release{Title: "ABQD-123"}, want: ContentTypeAdult},
		{name: "jav-shaped riaj code is music", release: &rls.Release{Title: "ABCD-123"}, want: ContentTypeMusic},
		{name: "xxx film franchise is not adult", release: &rls.Release{Type: rls.Movie, Title: "xXx", Year: 2002}, want: ContentTypeMovie},
		// RIAJ codes: the 3rd character of the manufacturer code names the medium.
		{name: "riaj cd", release: &rls.Release{Title: "VICL-1234"}, want: ContentTypeMusic},
		{name: "riaj dvd-video", release: &rls.Release{Title: "VIBX-1234"}, want: ContentTypeMovie},
		{name: "riaj cd-rom", release: &rls.Release{Title: "VIRW-1234"}, want: ContentTypeApp},
		{name: "riaj dvd-music", release: &rls.Release{Title: "VIWW-1234"}, want: ContentTypeMusic},
		{name: "riaj ps-game", release: &rls.Release{Title: "VIPW-1234"}, want: ContentTypeGame},
	}

	seen := make(map[ContentType]bool)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DetermineContentType(tt.release).ContentType)
		})
		seen[tt.want] = true
	}

	for _, contentType := range ContentTypes {
		assert.True(t, seen[contentType], "no case expects %q; drop it from ContentTypes or add a case here", contentType)
	}
}

// The cases above cannot cover a branch nobody wrote a case for, and a named
// string type still accepts an untyped literal, so check the source directly.
func TestDetermineContentTypeAssignsOnlyKnownTypes(t *testing.T) {
	t.Parallel()

	constNames := []string{
		"ContentTypeMovie", "ContentTypeTV", "ContentTypeMusic", "ContentTypeAudiobook",
		"ContentTypeBook", "ContentTypeComic", "ContentTypeGame", "ContentTypeApp",
		"ContentTypeAdult", "ContentTypeUnknown",
	}
	require.Len(t, constNames, len(ContentTypes))

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "content_type.go", nil, 0)
	require.NoError(t, err)

	assignments := 0
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ContentType" || i >= len(assign.Rhs) {
				continue
			}
			assignments++
			pos := fset.Position(assign.Pos())
			ident, ok := assign.Rhs[i].(*ast.Ident)
			if !ok {
				t.Errorf("%s: assign ContentType from a ContentType* constant, not %T", pos, assign.Rhs[i])
				continue
			}
			assert.True(t, slices.Contains(constNames, ident.Name), "%s: %s is not a listed content type constant", pos, ident.Name)
		}
		return true
	})
	assert.NotZero(t, assignments)
}
