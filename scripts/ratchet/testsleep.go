// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

const testSleepLinter = "testsleep"

func testSleepCounts() (map[string]int, error) {
	got := map[string]int{}
	err := eachTrackedGoFile(func(path string, src []byte) error {
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		n, err := countTestSleeps(path, src)
		if n > 0 {
			got[path] = n
		}
		return err
	})
	return got, err
}

func countTestSleeps(path string, src []byte) (int, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	timePkg, synctestPkg := importName(f, "time"), importName(f, "testing/synctest")
	if timePkg == "" {
		return 0, nil
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return !callsDirectly(node, synctestPkg, "Wait")
		case *ast.CallExpr:
			if !isSelector(node.Fun, synctestPkg, "Test") {
				return true
			}
			for _, arg := range node.Args {
				n += sleepsBeforeBubble(arg, timePkg)
			}
			return false
		case *ast.SelectorExpr:
			if isSelector(node, timePkg, "Sleep") {
				n++
			}
		}
		return true
	})
	return n, nil
}

func sleepsBeforeBubble(root ast.Node, timePkg string) int {
	n := 0
	ast.Inspect(root, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			lit, now := ast.Unparen(node.Fun).(*ast.FuncLit)
			if !now {
				return true
			}
			n += sleepsBeforeBubble(lit.Body, timePkg)
			for _, arg := range node.Args {
				n += sleepsBeforeBubble(arg, timePkg)
			}
			return false
		case *ast.FuncLit:
			return false
		case *ast.SelectorExpr:
			if isSelector(node, timePkg, "Sleep") {
				n++
			}
		}
		return true
	})
	return n
}

func callsDirectly(root ast.Node, pkg, name string) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested && node != root {
			return false
		}
		if call, ok := node.(*ast.CallExpr); ok && isSelector(call.Fun, pkg, name) {
			found = true
		}
		return !found
	})
	return found
}

func importName(f *ast.File, importPath string) string {
	for _, spec := range f.Imports {
		if p, err := strconv.Unquote(spec.Path.Value); err != nil || p != importPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return path.Base(importPath)
	}
	return ""
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name && isIdent(sel.X, pkg)
}
