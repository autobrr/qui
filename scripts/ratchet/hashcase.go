// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const hashCaseLinter = "hashcase"

func hashCaseCounts() (map[string]int, error) {
	got := map[string]int{}
	err := eachTrackedGoFile(func(file string, src []byte) error {
		if strings.HasSuffix(file, "_test.go") || strings.HasPrefix(file, "pkg/infohash/") {
			return nil
		}
		n, err := countHashCase(file, src)
		if n > 0 {
			got[file] = n
		}
		return err
	})
	return got, err
}

func countHashCase(file string, src []byte) (int, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	stringsName := importName(f, "strings")
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && foldsCase(call.Fun, stringsName) && namesHash(call.Args) {
			n++
		}
		return true
	})
	return n, nil
}

func foldsCase(fun ast.Expr, stringsName string) bool {
	switch fun := fun.(type) {
	case *ast.Ident:
		return fun.Name == "normalizeLowerTrim"
	case *ast.SelectorExpr:
		switch fun.Sel.Name {
		case "ToLower", "ToUpper", "EqualFold":
			return stringsName != "" && isIdent(fun.X, stringsName)
		case "normalizeLowerTrim":
			return true
		case "Normalize":
			normalizer, ok := fun.X.(*ast.SelectorExpr)
			return ok && normalizer.Sel.Name == "DefaultNormalizer" && isIdent(normalizer.X, "stringutils")
		}
	}
	return false
}

func namesHash(args []ast.Expr) bool {
	found := false
	for _, arg := range args {
		ast.Inspect(arg, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && strings.Contains(strings.ToLower(id.Name), "hash") {
				found = true
			}
			return !found
		})
	}
	return found
}
