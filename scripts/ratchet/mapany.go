// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const mapAnyLinter = "mapany"

var linterFixes = map[string]string{
	mapAnyLinter:    " Use a struct instead of a map[string]any or map[string]interface{}.",
	testSleepLinter: " A time.Sleep in a test outside synctest.Test waits in real time. Wait on a channel, or run the test in synctest.Test. A function that calls synctest.Wait runs in a bubble, so its sleeps do not count.",
}

func mapAnyCounts() (map[string]int, error) {
	got := map[string]int{}
	err := eachTrackedGoFile(func(path string, src []byte) error {
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		n, err := countMapAny(path, src)
		if n > 0 {
			got[path] = n
		}
		return err
	})
	return got, err
}

func countMapAny(path string, src []byte) (int, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		if m, ok := node.(*ast.MapType); ok && isIdent(m.Key, "string") && isEmptyInterface(m.Value) {
			n++
		}
		return true
	})
	return n, nil
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func isEmptyInterface(e ast.Expr) bool {
	if isIdent(e, "any") {
		return true
	}
	iface, ok := e.(*ast.InterfaceType)
	return ok && len(iface.Methods.List) == 0
}
