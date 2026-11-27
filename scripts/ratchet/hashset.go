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
	hashSetLinter = "hashset"
	infohashPath  = "github.com/autobrr/qui/pkg/infohash"
)

func hashSetCounts() (map[string]int, error) {
	got := map[string]int{}
	err := eachTrackedGoFile(func(file string, src []byte) error {
		if strings.HasSuffix(file, "_test.go") || strings.HasPrefix(file, "pkg/infohash/") {
			return nil
		}
		n, err := countHashSet(file, src)
		if n > 0 {
			got[file] = n
		}
		return err
	})
	return got, err
}

func countHashSet(file string, src []byte) (int, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	infohashName := importName(f, infohashPath)
	if infohashName == "" {
		return 0, nil
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		if m, ok := node.(*ast.MapType); ok && isHashType(m.Key, infohashName) && isSetValue(m.Value) {
			n++
		}
		return true
	})
	return n, nil
}

func isHashType(e ast.Expr, infohashName string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Hash" && isIdent(sel.X, infohashName)
}

func isSetValue(e ast.Expr) bool {
	if isIdent(e, "bool") {
		return true
	}
	s, ok := e.(*ast.StructType)
	return ok && len(s.Fields.List) == 0
}
