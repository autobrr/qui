// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineContentTypeReturnsKnownTypes(t *testing.T) {
	t.Parallel()

	var inputs []*rls.Release
	inputs = append(inputs, nil)
	// One step past the last rls type covers the default branch too.
	for typ := rls.Unknown; typ <= rls.Series+1; typ++ {
		inputs = append(inputs, &rls.Release{Type: typ})
	}
	inputs = append(inputs,
		&rls.Release{Title: "Some Studio XXX Scene"},
		&rls.Release{Series: 1},
		&rls.Release{Year: 2020},
		&rls.Release{Title: "VICL-1234"},
		&rls.Release{Title: "VIBX-1234"},
		&rls.Release{Title: "VIRW-1234"},
		&rls.Release{Title: "VIWW-1234"},
		&rls.Release{Title: "VIPW-1234"},
	)

	seen := make(map[ContentType]bool)
	for _, input := range inputs {
		got := DetermineContentType(input).ContentType
		assert.Contains(t, ContentTypes, got, "input %+v", input)
		seen[got] = true
	}
	for _, contentType := range ContentTypes {
		assert.True(t, seen[contentType], "no input produced %q; drop it from ContentTypes or add a case here", contentType)
	}
}

// A new branch in DetermineContentType may not be covered by the inputs above,
// so also check that every assignment to ContentType in the source uses a
// value from ContentTypes.
func TestDetermineContentTypeAssignsOnlyKnownTypes(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "content_type.go", nil, 0)
	require.NoError(t, err)

	constValues := make(map[string]string)
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if i < len(spec.Values) {
				if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					constValues[name.Name] = value
				}
			}
		}
		return true
	})

	assignments := 0
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ContentType" || i >= len(assign.Rhs) {
				continue
			}
			assignments++
			pos := fset.Position(assign.Pos())
			switch rhs := assign.Rhs[i].(type) {
			case *ast.Ident:
				value, ok := constValues[rhs.Name]
				if assert.True(t, ok, "%s: ContentType assigned from %s, which is not a string constant", pos, rhs.Name) {
					assert.True(t, slices.Contains(ContentTypes, ContentType(value)), "%s: %q is missing from ContentTypes", pos, value)
				}
			default:
				t.Errorf("%s: assign ContentType from a ContentType* constant, not %T", pos, rhs)
			}
		}
		return true
	})
	assert.NotZero(t, assignments)
}
