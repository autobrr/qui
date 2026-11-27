// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSitePickCountsAHalfBuiltPick(t *testing.T) {
	const models = `package models

type SitePick struct {
	IndexerIDs   []int
	GazelleSites []string
}

type InstanceCrossSeedCompletionSettings struct {
	Enabled bool
	SitePick
}
`
	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{
			name: "an entry point fills the pick of the scope facts by hand",
			path: "internal/services/crossseed/completion.go",
			body: `scope := s.collectSearchScope(ctx, instanceID, torrent, sites, searchScopeFacts{
		sites:       sites,
		gazelleOff:  gazelleClients.disabled(),
		gazelleKeys: gazelleClients.hosts(),
	})`,
			want: 1,
		},
		{
			name: "a pick built from the Torznab half only",
			path: "internal/services/crossseed/completion.go",
			body: `sites := s.resolveSitePick(ctx, models.SitePick{IndexerIDs: settings.IndexerIDs}, false, nil)`,
			want: 1,
		},
		{
			name: "a promoted half in a literal of a type that embeds the pick",
			path: "internal/api/handlers/crossseed.go",
			body: `settings := &models.InstanceCrossSeedCompletionSettings{Enabled: true, IndexerIDs: req.IndexerIDs}`,
			want: 1,
		},
		{
			name: "a resolved pick built outside the Search scope module",
			path: "internal/services/crossseed/library_scan.go",
			body: `sites := resolvedSitePick{indexerIDs: ids}
	_ = resolvedSitePick{}`,
			want: 2,
		},
		{
			name: "a pick in a slice or map literal that leaves its type out",
			path: "internal/services/crossseed/library_scan.go",
			body: `_ = []resolvedSitePick{{indexerIDs: ids}}
	_ = map[string]resolvedSitePick{"a": {}}
	_ = [][]models.SitePick{{{IndexerIDs: ids}}}`,
			want: 3,
		},
		{
			name: "the stored pick passed whole",
			path: "internal/services/crossseed/completion.go",
			body: `sites := s.resolveSitePick(ctx, settings.SitePick, false, nil)
	_ = models.InstanceCrossSeedCompletionSettings{SitePick: req.SitePick}`,
		},
		{
			name: "both halves, or neither for every site",
			path: "internal/api/handlers/crossseed.go",
			body: `_ = models.SitePick{IndexerIDs: r.IndexerIDs, GazelleSites: r.GazelleSites}
	_ = models.SitePick{}`,
		},
		{
			name: "the Search scope module fills its facts",
			path: "internal/services/crossseed/search_scope.go",
			body: `_ = searchScopeFacts{sites: sites}
	_ = resolvedSitePick{pick: pick}`,
		},
		{
			name: "the Search scope module still keys both halves",
			path: "internal/services/crossseed/search_scope.go",
			body: `_ = models.SitePick{GazelleSites: sites}`,
			want: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			modelsFile, err := parser.ParseFile(fset, "internal/models/site_pick.go", models, parser.SkipObjectResolution)
			require.NoError(t, err)
			f, err := parser.ParseFile(fset, tc.path, "package p\n\nfunc f() {\n\t"+tc.body+"\n}\n", parser.SkipObjectResolution)
			require.NoError(t, err)
			pickTypes := sitePickTypes(map[string]*ast.File{"internal/models/site_pick.go": modelsFile, tc.path: f})
			require.Equal(t, tc.want, countSitePick(tc.path, f, pickTypes))
		})
	}
}
