// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gocomments

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
)

var directive = regexp.MustCompile(`^//(go:|nolint\b| nolint:|exhaustive:| ?#nosec\b| Deprecated: | Copyright \(c\) | SPDX-License-Identifier: | goes away (in #\d+|when the maintainer decides))`)

func Lines(path string, src []byte) ([]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(f) {
		return nil, nil
	}
	var lines []int
	for _, group := range f.Comments {
		for _, c := range group.List {
			if !directive.MatchString(c.Text) {
				lines = append(lines, fset.Position(c.Pos()).Line)
			}
		}
	}
	return lines, nil
}
