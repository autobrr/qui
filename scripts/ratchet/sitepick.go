// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const (
	sitePickLinter    = "sitepick"
	sitePickScopeFile = "internal/services/crossseed/search_scope.go"
)

var (
	sitePickHalves      = []string{"IndexerIDs", "GazelleSites"}
	sitePickScopeFields = []string{"sites"}
)

func sitePickCounts() (map[string]int, error) {
	files := map[string]*ast.File{}
	err := eachTrackedGoFile(func(path string, src []byte) error {
		if strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, "scripts/ratchet/") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files[path] = f
		return nil
	})
	if err != nil {
		return nil, err
	}
	pickTypes := sitePickTypes(files)
	got := map[string]int{}
	for path, f := range files {
		if n := countSitePick(path, f, pickTypes); n > 0 {
			got[path] = n
		}
	}
	return got, nil
}

func sitePickTypes(files map[string]*ast.File) map[string]bool {
	types := map[string]bool{"SitePick": true}
	for _, f := range files {
		ast.Inspect(f, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if st, ok := spec.Type.(*ast.StructType); ok && embedsSitePick(st) {
				types[spec.Name.Name] = true
			}
			return false
		})
	}
	return types
}

func embedsSitePick(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 && typeName(field.Type) == "SitePick" {
			return true
		}
	}
	return false
}

func countSitePick(path string, f *ast.File, pickTypes map[string]bool) int {
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		name := typeName(lit.Type)
		keys := literalKeys(lit)
		switch {
		case pickTypes[name] && countKeys(keys, sitePickHalves) == 1:
			n++
		case path == sitePickScopeFile:
		case name == "resolvedSitePick" && len(keys) > 0:
			n++
		case name == "searchScopeFacts" && countKeys(keys, sitePickScopeFields) > 0:
			n++
		}
		return true
	})
	return n
}

func typeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return typeName(e.X)
	}
	return ""
}

func literalKeys(lit *ast.CompositeLit) map[string]bool {
	keys := map[string]bool{}
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				keys[id.Name] = true
			}
		}
	}
	return keys
}

func countKeys(keys map[string]bool, names []string) int {
	n := 0
	for _, name := range names {
		if keys[name] {
			n++
		}
	}
	return n
}
