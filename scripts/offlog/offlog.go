// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package offlog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	onName     = regexp.MustCompile(`(?i)(enabled|active|configured)$`)
	offName    = regexp.MustCompile(`(?i:disabled)$|^(?i:off)$|Off$`)
	offMessage = regexp.MustCompile(`(?i)\b(disabled|not enabled|not configured|turned off|is off)\b`)

	quietLevels = []string{"Debug", "Info"}
	writes      = []string{"Msg", "Msgf", "Send"}
)

func Count(path string, src []byte) (int, error) {
	lines, err := Lines(path, src)
	return len(lines), err
}

func Lines(path string, src []byte) ([]int, error) {
	if strings.HasSuffix(path, "_test.go") {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var lines []int
	var walk func(n ast.Node, off bool)
	walk = func(n ast.Node, off bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch v := m.(type) {
			case *ast.IfStmt:
				if v.Init != nil {
					walk(v.Init, off)
				}
				walk(v.Cond, off)
				walk(v.Body, off || testsOff(v.Cond))
				if v.Else != nil {
					walk(v.Else, off || onName.MatchString(switchName(v.Cond)))
				}
				return false
			case *ast.CallExpr:
				if writesAtQuietLevel(v) && (off || saysOff(v.Args)) {
					lines = append(lines, fset.Position(v.Pos()).Line)
				}
			}
			return true
		})
	}
	walk(f, false)
	return lines, nil
}

func writesAtQuietLevel(call *ast.CallExpr) bool {
	write, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !slices.Contains(writes, write.Sel.Name) {
		return false
	}
	for x := write.X; ; {
		link, ok := x.(*ast.CallExpr)
		if !ok {
			return false
		}
		method, ok := link.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if len(link.Args) == 0 && slices.Contains(quietLevels, method.Sel.Name) {
			return true
		}
		x = method.X
	}
}

func saysOff(args []ast.Expr) bool {
	return slices.ContainsFunc(args, func(arg ast.Expr) bool {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return false
		}
		text, err := strconv.Unquote(lit.Value)
		return err == nil && offMessage.MatchString(text)
	})
}

func testsOff(cond ast.Expr) bool {
	switch v := cond.(type) {
	case *ast.ParenExpr:
		return testsOff(v.X)
	case *ast.UnaryExpr:
		return v.Op == token.NOT && onName.MatchString(switchName(v.X))
	case *ast.BinaryExpr:
		return (v.Op == token.LAND || v.Op == token.LOR) && (testsOff(v.X) || testsOff(v.Y))
	}
	return offName.MatchString(switchName(cond))
}

func switchName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return switchName(v.X)
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.CallExpr:
		return switchName(v.Fun)
	}
	return ""
}
