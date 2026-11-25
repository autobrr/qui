// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type decl struct {
	name    string
	text    string
	file    string
	from    int
	to      int
	isFunc  bool
	node    ast.Node
	imports map[string]string
}

type decls map[string][]decl

var (
	majorVersion = regexp.MustCompile(`^v[0-9]+$`)
	directive    = regexp.MustCompile(`(?m)^//go:(?:build|embed|linkname|noinline|nosplit|norace|debug|uintptrescapes|noescape|wasmimport|wasmexport)\b.*$`)
)

func parseDecls(files map[string][]byte, keep func(file string) bool) (decls, error) {
	index := decls{}
	for _, name := range sortedKeys(files) {
		if !keep(name) {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, files[name], parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		imports := importNames(file)
		index.addFileWide(name, files[name], file, imports)
		for _, node := range file.Decls {
			text := tokensOf(files[name][fset.Position(node.Pos()).Offset:fset.Position(node.End()).Offset])
			d := decl{text: text + usedImports(node, imports), file: name, node: node, imports: imports, from: fset.Position(node.Pos()).Line, to: fset.Position(node.End()).Line}
			index.add(d)
		}
	}
	return index, nil
}

func (index decls) addFileWide(name string, source []byte, file *ast.File, imports map[string]string) {
	used := usedNames(file)
	lines := directive.FindAllString(string(source), -1)
	for _, spec := range file.Imports {
		target, err := strconv.Unquote(spec.Path.Value)
		known := slices.ContainsFunc(sortedKeys(imports), func(alias string) bool { return imports[alias] == target && used[alias] })
		if err == nil && !known {
			lines = append(lines, "import "+target)
		}
	}
	for _, line := range lines {
		index[line] = append(index[line], decl{name: line, text: line, file: name, node: file.Name})
	}
}

func (index decls) fileWide(file string) []string {
	var lines []string
	for key, found := range index {
		if slices.ContainsFunc(found, func(d decl) bool { _, wide := d.node.(*ast.Ident); return wide && d.file == file }) {
			lines = append(lines, key)
		}
	}
	return lines
}

func usedNames(node ast.Node) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(node, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if name, ok := selector.X.(*ast.Ident); ok {
				used[name.Name] = true
			}
		}
		return true
	})
	return used
}

func usedImports(node ast.Node, imports map[string]string) string {
	var b strings.Builder
	for _, name := range sortedKeys(usedNames(node)) {
		if target := imports[name]; target != "" {
			b.WriteString("\n" + name + " " + target)
		}
	}
	return b.String()
}

func tokensOf(source []byte) string {
	fset := token.NewFileSet()
	var lexer scanner.Scanner
	lexer.Init(fset.AddFile("", -1, len(source)), source, nil, 0)
	var b strings.Builder
	held := ""
	for {
		_, kind, text := lexer.Scan()
		if kind == token.EOF {
			return b.String()
		}
		closes := kind == token.RPAREN || kind == token.RBRACE || kind == token.RBRACK
		if !closes {
			b.WriteString(held)
		}
		held = ""
		switch {
		case kind == token.SEMICOLON || kind == token.COMMA:
			held = kind.String() + " "
		case kind.IsLiteral():
			b.WriteString(text + " ")
		default:
			b.WriteString(kind.String() + " ")
		}
	}
}

func (index decls) add(d decl) {
	switch node := d.node.(type) {
	case *ast.FuncDecl:
		d.isFunc, d.name = true, node.Name.Name
		key := d.name
		if receiver := receiverType(node); receiver != "" {
			d.name, key = receiver+"."+d.name, receiver
		}
		index[key] = append(index[key], d)
	case *ast.GenDecl:
		for _, spec := range node.Specs {
			for _, name := range specNames(spec) {
				d.name = name
				index[name] = append(index[name], d)
			}
		}
	}
}

func specNames(spec ast.Spec) []string {
	switch spec := spec.(type) {
	case *ast.TypeSpec:
		return []string{spec.Name.Name}
	case *ast.ValueSpec:
		var names []string
		for _, ident := range spec.Names {
			names = append(names, ident.Name)
		}
		return names
	}
	return nil
}

func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

func importNames(file *ast.File) map[string]string {
	names := map[string]string{}
	for _, spec := range file.Imports {
		target, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(target)
		if majorVersion.MatchString(name) {
			name = path.Base(path.Dir(target))
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = target
	}
	return names
}

func (index decls) benchmarks() []string {
	var names []string
	for name, found := range index {
		if found[0].isFunc && found[0].name == name && isBenchmark(name) && strings.HasSuffix(found[0].file, "_test.go") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func isBenchmark(name string) bool {
	rest, found := strings.CutPrefix(name, "Benchmark")
	return found && (rest == "" || rest[0] < 'a' || rest[0] > 'z')
}

func isTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }

func isSource(file string) bool { return !isTestFile(file) }
