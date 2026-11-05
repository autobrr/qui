// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// unresolvedStatuses names the statuses of each expression that the scan
// cannot follow, keyed by "<function>: <expression>".
var unresolvedStatuses = map[string][]string{}

// TestEveryBuiltStatusHasAClass reads the source of the package, because a
// status that a build site sets with no table entry still compiles.
func TestEveryBuiltStatusHasAClass(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, p, nil, 0)
		require.NoError(t, err)
		files = append(files, file)
	}

	statuses, problems := scanResultStatuses(fset, files, unresolvedStatuses)
	require.Empty(t, problems)
	require.ElementsMatch(t, slices.Collect(maps.Keys(resultClasses)), slices.Collect(maps.Keys(statuses)),
		"the statuses that the source builds and the statuses in the class table differ")
}

func TestStatusScanFailsOnUnknownOrUnresolvedStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"status with no table entry": `func build() InstanceCrossSeedResult {
			return InstanceCrossSeedResult{Status: "synthetic_unknown_status"}
		}`,
		"status built from a name": `func build(mode string) {
			var result InstanceCrossSeedResult
			result.Status = "added_" + mode
		}`,
		"status copied from another type": `func build(input struct{ Status string }) InstanceCrossSeedResult {
			return InstanceCrossSeedResult{Status: input.Status}
		}`,
		"status copied from a field name that two types share": `type other struct{ Result struct{ Status string } }
		type held struct{ Result AddResult }
		func build(input other) InstanceCrossSeedResult {
			return InstanceCrossSeedResult{Status: input.Result.Status}
		}`,
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "synthetic.go", "package crossseed\n"+src, 0)
			require.NoError(t, err)
			_, problems := scanResultStatuses(fset, []*ast.File{file}, nil)
			require.Len(t, problems, 1)
		})
	}
}

// resultStatusTypes are the types whose Status field holds a cross-seed result status.
var resultStatusTypes = map[string]bool{"InstanceCrossSeedResult": true, "AddResult": true, "AddVerdict": true}

type statusScan struct {
	fset       *token.FileSet
	unresolved map[string][]string
	consts     map[string]string
	funcs      map[string][]*ast.FuncDecl
	imports    map[string]bool
	// fields holds each keyed value of a composite literal by field name, for
	// a fixed status in a value such as linkMode.
	fields map[string][]scopedExpr
	// fieldTypes holds the type of each struct field by field name.
	fieldTypes map[string]string
	statuses   map[string]bool
	problems   []string
	visiting   map[string]bool
}

type scopedExpr struct {
	decl *ast.FuncDecl
	expr ast.Expr
}

// scanResultStatuses returns each status that the files can give a result,
// and a problem for each status with no table entry and each expression that
// it cannot resolve.
func scanResultStatuses(fset *token.FileSet, files []*ast.File, unresolved map[string][]string) (map[string]bool, []string) {
	s := &statusScan{
		fset: fset, unresolved: unresolved,
		consts: map[string]string{}, funcs: map[string][]*ast.FuncDecl{}, imports: map[string]bool{},
		fields: map[string][]scopedExpr{}, fieldTypes: map[string]string{}, statuses: map[string]bool{}, visiting: map[string]bool{},
	}
	for _, file := range files {
		for _, spec := range file.Imports {
			name, _ := strconv.Unquote(spec.Path.Value)
			name = path.Base(name)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			s.imports[name] = true
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				s.funcs[decl.Name.Name] = append(s.funcs[decl.Name.Name], decl)
			case *ast.GenDecl:
				s.readConsts(decl)
				s.readFieldTypes(decl)
			}
		}
	}
	s.eachDecl(func(decl *ast.FuncDecl, node ast.Node) {
		if lit, ok := node.(*ast.CompositeLit); ok {
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						s.fields[key.Name] = append(s.fields[key.Name], scopedExpr{decl, kv.Value})
					}
				}
			}
		}
	})
	s.eachDecl(s.readSink)

	for status := range s.statuses {
		if _, ok := resultClasses[status]; !ok {
			s.problems = append(s.problems, fmt.Sprintf("status %q has no entry in resultClasses", status))
		}
	}
	return s.statuses, s.problems
}

func (s *statusScan) readConsts(decl *ast.GenDecl) {
	if decl.Tok != token.CONST {
		return
	}
	for _, spec := range decl.Specs {
		value := spec.(*ast.ValueSpec)
		for i, name := range value.Names {
			if i < len(value.Values) {
				if lit, ok := value.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s.consts[name.Name], _ = strconv.Unquote(lit.Value)
				}
			}
		}
	}
}

func (s *statusScan) readFieldTypes(decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			if structType, ok := typeSpec.Type.(*ast.StructType); ok {
				for _, field := range structType.Fields.List {
					for _, name := range field.Names {
						// A field name with two types has no type that the scan can trust.
						fieldType := typeName(field.Type)
						if prior, ok := s.fieldTypes[name.Name]; ok && prior != fieldType {
							fieldType = ""
						}
						s.fieldTypes[name.Name] = fieldType
					}
				}
			}
		}
	}
}

func (s *statusScan) eachDecl(visit func(*ast.FuncDecl, ast.Node)) {
	for _, decls := range s.funcs {
		for _, decl := range decls {
			ast.Inspect(decl, func(node ast.Node) bool {
				visit(decl, node)
				return true
			})
		}
	}
}

// readSink collects the status of a Status field in a composite literal and
// of an assignment to .Status.
func (s *statusScan) readSink(decl *ast.FuncDecl, node ast.Node) {
	switch node := node.(type) {
	case *ast.CompositeLit:
		if array, ok := node.Type.(*ast.ArrayType); ok && resultStatusTypes[typeName(array.Elt)] {
			for _, elt := range node.Elts {
				if lit, ok := elt.(*ast.CompositeLit); ok && lit.Type == nil {
					s.readStatusField(decl, lit)
				}
			}
		}
		if resultStatusTypes[typeName(node.Type)] {
			s.readStatusField(decl, node)
		}
	case *ast.AssignStmt:
		for i, lhs := range node.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Status" {
				continue
			}
			rhs, index := node.Rhs[0], i
			if len(node.Rhs) == len(node.Lhs) {
				rhs, index = node.Rhs[i], 0
			}
			// A typed constant of another package, such as a run status, cannot
			// go into the string Status field of a result.
			if s.isImported(rhs) {
				continue
			}
			receiver, ok := sel.X.(*ast.Ident)
			if !ok {
				s.problem(decl, sel.X, "cannot resolve the receiver type")
				continue
			}
			isResult, known := s.isResult(decl, receiver.Name)
			switch {
			case !known:
				s.problem(decl, sel.X, "cannot resolve the receiver type")
			case isResult:
				s.add(s.resolve(decl, rhs, index))
			}
		}
	}
}

func (s *statusScan) readStatusField(decl *ast.FuncDecl, lit *ast.CompositeLit) {
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Status" {
				s.add(s.resolve(decl, kv.Value, 0))
			}
		}
	}
}

func (s *statusScan) add(statuses []string) {
	for _, status := range statuses {
		if status != "" {
			s.statuses[status] = true
		}
	}
}

func (s *statusScan) problem(decl *ast.FuncDecl, expr ast.Expr, why string) {
	s.problems = append(s.problems, fmt.Sprintf("%s: %s in %s: %s", s.fset.Position(expr.Pos()), types.ExprString(expr), decl.Name.Name, why))
}

func (s *statusScan) isImported(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && s.imports[pkg.Name]
}

// resolve returns the statuses that expr can hold. index picks one result of
// a call that returns more than one value.
func (s *statusScan) resolve(decl *ast.FuncDecl, expr ast.Expr, index int) []string {
	if statuses, ok := s.unresolved[decl.Name.Name+": "+types.ExprString(expr)]; ok {
		return statuses
	}
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if value, err := strconv.Unquote(expr.Value); err == nil {
			return []string{value}
		}
	case *ast.ParenExpr:
		return s.resolve(decl, expr.X, index)
	case *ast.Ident:
		if value, ok := s.consts[expr.Name]; ok {
			return []string{value}
		}
		return s.resolveIdent(decl, expr, index)
	case *ast.SelectorExpr:
		switch {
		case expr.Sel.Name == "Status":
			// A copy from a result type reads a status that its build site set.
			if s.isResultExpr(decl, expr.X) {
				return nil
			}
		case s.isImported(expr):
		case len(s.fields[expr.Sel.Name]) > 0:
			var statuses []string
			for _, field := range s.fields[expr.Sel.Name] {
				statuses = append(statuses, s.resolve(field.decl, field.expr, 0)...)
			}
			return statuses
		}
	case *ast.CallExpr:
		name := calleeName(expr)
		if lit := closure(decl, name); lit != nil {
			return s.resolveReturns(decl, lit.Body, index)
		}
		if callees := s.funcs[name]; len(callees) > 0 {
			var statuses []string
			for _, callee := range callees {
				statuses = append(statuses, s.resolveReturns(callee, callee.Body, index)...)
			}
			return statuses
		}
	}
	s.problem(decl, expr, "cannot resolve the status")
	return nil
}

func (s *statusScan) resolveReturns(decl *ast.FuncDecl, body *ast.BlockStmt, index int) []string {
	var statuses []string
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			switch {
			case len(node.Results) > index && (index == 0 || len(node.Results) > 1):
				statuses = append(statuses, s.resolve(decl, node.Results[index], 0)...)
			case len(node.Results) == 1:
				statuses = append(statuses, s.resolve(decl, node.Results[0], index)...)
			}
		}
		return true
	})
	return statuses
}

// resolveIdent follows a parameter to the arguments of each call, and a local
// variable to each value that it gets.
func (s *statusScan) resolveIdent(decl *ast.FuncDecl, ident *ast.Ident, index int) []string {
	key := fmt.Sprintf("%p %s %d", decl, ident.Name, index)
	if s.visiting[key] {
		return nil
	}
	s.visiting[key] = true

	var statuses []string
	found := false
	if i, paramType := paramIndex(decl.Type, ident.Name); i >= 0 {
		found = true
		if typeName(paramType) == "string" {
			statuses = append(statuses, s.resolveArgs(decl.Name.Name, nil, i)...)
		}
	}
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == ident.Name {
					found = true
					if len(node.Rhs) == len(node.Lhs) {
						if _, ok := node.Rhs[i].(*ast.FuncLit); ok {
							continue
						}
						statuses = append(statuses, s.resolve(decl, node.Rhs[i], 0)...)
					} else {
						statuses = append(statuses, s.resolve(decl, node.Rhs[0], i)...)
					}
				}
				// A parameter of a closure gets the arguments of each call of the closure.
				if id, ok := lhs.(*ast.Ident); ok && len(node.Rhs) == len(node.Lhs) {
					if lit, ok := node.Rhs[i].(*ast.FuncLit); ok {
						if p, paramType := paramIndex(lit.Type, ident.Name); p >= 0 {
							found = true
							if typeName(paramType) == "string" {
								statuses = append(statuses, s.resolveArgs(id.Name, decl, p)...)
							}
						}
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if name.Name == ident.Name {
					found = true
					if i < len(node.Values) {
						statuses = append(statuses, s.resolve(decl, node.Values[i], 0)...)
					}
				}
			}
		case *ast.RangeStmt:
			for _, v := range []ast.Expr{node.Key, node.Value} {
				if id, ok := v.(*ast.Ident); ok && id.Name == ident.Name {
					s.problem(decl, ident, "cannot resolve a range variable")
					found = true
				}
			}
		}
		return true
	})
	if !found {
		s.problem(decl, ident, "cannot resolve the status")
	}
	return statuses
}

// resolveArgs resolves argument index of each call of name. A nil within
// searches the whole package, and a closure is searched within its function.
func (s *statusScan) resolveArgs(name string, within *ast.FuncDecl, index int) []string {
	var statuses []string
	s.eachDecl(func(decl *ast.FuncDecl, node ast.Node) {
		call, ok := node.(*ast.CallExpr)
		if !ok || calleeName(call) != name || (within != nil && decl != within) || index >= len(call.Args) {
			return
		}
		statuses = append(statuses, s.resolve(decl, call.Args[index], 0)...)
	})
	return statuses
}

// isResult reports whether the local name in decl holds a result type, and
// whether the scan could read the type at all.
func (s *statusScan) isResult(decl *ast.FuncDecl, name string) (isResult, known bool) {
	var names []string
	if i, paramType := paramIndex(decl.Type, name); i >= 0 {
		names = append(names, typeName(paramType))
	}
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			if i, paramType := paramIndex(node.Type, name); i >= 0 {
				names = append(names, typeName(paramType))
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name && node.Tok == token.DEFINE {
					rhs, index := node.Rhs[0], i
					if len(node.Rhs) == len(node.Lhs) {
						rhs, index = node.Rhs[i], 0
					}
					names = append(names, s.exprType(rhs, index))
				}
			}
		case *ast.ValueSpec:
			for _, id := range node.Names {
				if id.Name == name {
					names = append(names, typeName(node.Type))
				}
			}
		}
		return true
	})
	isResult = slices.ContainsFunc(names, func(name string) bool { return resultStatusTypes[name] })
	return isResult, len(names) > 0 && !slices.Contains(names, "")
}

func (s *statusScan) isResultExpr(decl *ast.FuncDecl, expr ast.Expr) bool {
	if id, ok := expr.(*ast.Ident); ok {
		// The scan does not track scopes, so one binding of the name with a
		// result type is enough.
		isResult, _ := s.isResult(decl, id.Name)
		return isResult
	}
	return resultStatusTypes[s.exprType(expr, 0)]
}

func (s *statusScan) exprType(expr ast.Expr, index int) string {
	switch expr := expr.(type) {
	case *ast.UnaryExpr:
		return s.exprType(expr.X, index)
	case *ast.CompositeLit:
		return typeName(expr.Type)
	case *ast.SelectorExpr:
		if !s.isImported(expr) {
			return s.fieldTypes[expr.Sel.Name]
		}
	case *ast.CallExpr:
		for _, callee := range s.funcs[calleeName(expr)] {
			if results := fieldTypes(callee.Type.Results); index < len(results) {
				return typeName(results[index])
			}
		}
	}
	return ""
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// closure returns the function literal that decl assigns to name.
func closure(decl *ast.FuncDecl, name string) *ast.FuncLit {
	var found *ast.FuncLit
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		if assign, ok := node.(*ast.AssignStmt); ok && len(assign.Lhs) == len(assign.Rhs) {
			for i, lhs := range assign.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
					if lit, ok := assign.Rhs[i].(*ast.FuncLit); ok {
						found = lit
					}
				}
			}
		}
		return found == nil
	})
	return found
}

// paramIndex returns the position and type of the parameter name, or -1.
func paramIndex(fn *ast.FuncType, name string) (int, ast.Expr) {
	i := 0
	for _, field := range fn.Params.List {
		for _, id := range field.Names {
			if id.Name == name {
				return i, field.Type
			}
			i++
		}
		if len(field.Names) == 0 {
			i++
		}
	}
	return -1, nil
}

func fieldTypes(list *ast.FieldList) []ast.Expr {
	var out []ast.Expr
	if list == nil {
		return nil
	}
	for _, field := range list.List {
		for range max(len(field.Names), 1) {
			out = append(out, field.Type)
		}
	}
	return out
}

// typeName returns the name of a type expression, with a package prefix for
// an imported type and without a pointer.
func typeName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.StarExpr:
		return typeName(expr.X)
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return types.ExprString(expr)
	}
	return ""
}
