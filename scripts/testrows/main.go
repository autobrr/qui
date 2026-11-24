// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/json/v2"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

type source struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type count struct {
	Rows   int  `json:"rows"`
	Broken bool `json:"broken"`
}

func rows(src source) count {
	file, err := parser.ParseFile(token.NewFileSet(), src.Name, src.Text, parser.SkipObjectResolution)
	if err != nil {
		return count{Broken: true}
	}
	result := count{}
	ast.Inspect(file, func(node ast.Node) bool {
		if literal, ok := node.(*ast.CompositeLit); ok && isCaseTable(literal.Type) {
			result.Rows += len(literal.Elts)
		}
		return true
	})
	return result
}

func isCaseTable(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.ArrayType:
		return hasFields(t.Elt)
	case *ast.MapType:
		return hasFields(t.Value)
	}
	return false
}

func hasFields(expr ast.Expr) bool {
	s, ok := expr.(*ast.StructType)
	return ok && s.Fields.NumFields() > 0
}

func main() {
	var sources []source
	if err := json.UnmarshalRead(os.Stdin, &sources); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	counts := make([]count, len(sources))
	for i, src := range sources {
		counts[i] = rows(src)
	}
	if err := json.MarshalWrite(os.Stdout, counts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
