// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
)

const (
	testHookLinter = "testhook"
	testSupportDir = "internal/testutil/"
)

type parsedFile struct {
	path string
	ast  *ast.File
}

func testHookCounts() (map[string]int, error) {
	files := map[string][]byte{}
	err := eachTrackedGoFile(func(path string, src []byte) error {
		files[path] = src
		return nil
	})
	if err != nil {
		return nil, err
	}
	return countTestHooks(files)
}

func countTestHooks(files map[string][]byte) (map[string]int, error) {
	packages := map[string][]parsedFile{}
	for file, src := range files {
		if strings.HasSuffix(file, "_test.go") || strings.HasPrefix(file, testSupportDir) {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		dir := path.Dir(file)
		packages[dir] = append(packages[dir], parsedFile{path: file, ast: f})
	}
	exportedSets := map[string]bool{}
	for _, pkg := range packages {
		for name := range fieldSets(pkg) {
			if ast.IsExported(name) {
				exportedSets[name] = true
			}
		}
	}
	got := map[string]int{}
	for _, pkg := range packages {
		hooks := testOnlyFuncFields(pkg, exportedSets)
		if len(hooks) == 0 {
			continue
		}
		for _, file := range pkg {
			if n := countHookNilChecks(file.ast, hooks); n > 0 {
				got[file.path] = n
			}
		}
	}
	return got, nil
}

func testOnlyFuncFields(pkg []parsedFile, exportedSets map[string]bool) map[string]bool {
	funcTypes := map[string]bool{}
	for _, file := range pkg {
		ast.Inspect(file.ast, func(node ast.Node) bool {
			if spec, ok := node.(*ast.TypeSpec); ok {
				if _, isFunc := spec.Type.(*ast.FuncType); isFunc {
					funcTypes[spec.Name.Name] = true
				}
			}
			return true
		})
	}
	sets := fieldSets(pkg)
	fields := map[string]bool{}
	for _, file := range pkg {
		ast.Inspect(file.ast, func(node ast.Node) bool {
			st, ok := node.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if !isFuncType(field.Type, funcTypes) {
					continue
				}
				for _, name := range field.Names {
					if !sets[name.Name] && !exportedSets[name.Name] {
						fields[name.Name] = true
					}
				}
			}
			return true
		})
	}
	return fields
}

func fieldSets(pkg []parsedFile) map[string]bool {
	sets := map[string]bool{}
	for _, file := range pkg {
		ast.Inspect(file.ast, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					if sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok {
						sets[sel.Sel.Name] = true
					}
				}
			case *ast.CompositeLit:
				for _, elt := range node.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok {
							sets[key.Name] = true
						}
					}
				}
			}
			return true
		})
	}
	return sets
}

func isFuncType(e ast.Expr, funcTypes map[string]bool) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.FuncType:
		return true
	case *ast.Ident:
		return funcTypes[e.Name]
	}
	return false
}

func countHookNilChecks(f *ast.File, hooks map[string]bool) int {
	n := 0
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		locals := hookCopies(fn.Body, hooks)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			cmp, ok := node.(*ast.BinaryExpr)
			if !ok || (cmp.Op != token.EQL && cmp.Op != token.NEQ) {
				return true
			}
			for _, pair := range [][2]ast.Expr{{cmp.X, cmp.Y}, {cmp.Y, cmp.X}} {
				if isIdent(pair[1], "nil") && (isHookSelector(pair[0], hooks) || isHookCopy(pair[0], locals)) {
					n++
				}
			}
			return true
		})
	}
	return n
}

func hookCopies(body *ast.BlockStmt, hooks map[string]bool) map[string]bool {
	locals := map[string]bool{}
	copyTo := func(names []ast.Expr, values []ast.Expr) {
		if len(names) != len(values) {
			return
		}
		for i, value := range values {
			if id, ok := names[i].(*ast.Ident); ok && isHookSelector(value, hooks) {
				locals[id.Name] = true
			}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			copyTo(node.Lhs, node.Rhs)
		case *ast.ValueSpec:
			names := make([]ast.Expr, len(node.Names))
			for i, name := range node.Names {
				names[i] = name
			}
			copyTo(names, node.Values)
		}
		return true
	})
	return locals
}

func isHookSelector(e ast.Expr, hooks map[string]bool) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	return ok && hooks[sel.Sel.Name]
}

func isHookCopy(e ast.Expr, locals map[string]bool) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && locals[id.Name]
}
