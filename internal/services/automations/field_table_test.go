// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// Go cannot list the constants of a type at run time, so the test reads them from the source.
func TestConditionFieldsHasEveryField(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../../models/automation.go", nil, 0)
	require.NoError(t, err)

	var fields []ConditionField
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != "ConditionField" {
				continue
			}
			for _, v := range value.Values {
				name, err := strconv.Unquote(v.(*ast.BasicLit).Value)
				require.NoError(t, err)
				fields = append(fields, ConditionField(name))
			}
		}
	}

	require.NotEmpty(t, fields)
	for _, field := range fields {
		_, ok := ConditionFields[field]
		require.True(t, ok, "%s has no entry in ConditionFields", field)
	}
	require.Len(t, ConditionFields, len(fields), "ConditionFields has an entry for a field that is not a ConditionField constant")
}

// conditionFieldData gates a field on the same filesystem access that the table states.
func TestConditionFieldsRequirementsMatchFieldData(t *testing.T) {
	for field, spec := range ConditionFields {
		data := conditionFieldData[field]
		require.Equal(t, data.fileIdentity || data.localAccess, spec.Requires == RequiresLocalAccess, field)
	}
}
