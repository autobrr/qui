// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"io/fs"
	"strings"

	"golang.org/x/mod/modfile"
)

type workloads struct {
	tree   commitTree
	dirs   []string
	module string
	parsed map[string]decls
}

func newWorkloads(ctx context.Context, t commitTree, dirs []string) (*workloads, error) {
	mod, err := t.file(ctx, "go.mod")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return &workloads{tree: t, dirs: dirs, module: modfile.ModulePath(mod), parsed: map[string]decls{}}, nil
}

func (w *workloads) list(ctx context.Context) ([]bench, error) {
	dirs, err := w.tree.benchDirs(ctx)
	if err != nil {
		return nil, err
	}
	var all []bench
	for _, dir := range dirs {
		index, err := w.load(ctx, dir, isTestFile)
		if err != nil {
			return nil, err
		}
		for _, name := range index.benchmarks() {
			all = append(all, bench{dir, name})
		}
	}
	return all, nil
}

func (w *workloads) load(ctx context.Context, dir string, keep func(string) bool) (decls, error) {
	key := fmt.Sprintf("%s %t", dir, keep("_test.go"))
	if index, ok := w.parsed[key]; ok {
		return index, nil
	}
	files, err := w.tree.goFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	index, err := parseDecls(files, keep)
	if err != nil {
		return nil, err
	}
	w.parsed[key] = index
	return index, nil
}

type symbol struct{ dir, name string }

func (w *workloads) fingerprint(ctx context.Context, b bench) (string, error) {
	own, err := w.load(ctx, b.dir, isTestFile)
	if err != nil {
		return "", err
	}
	if len(own[b.name]) == 0 {
		return "", nil
	}
	todo := []symbol{{b.dir, b.name}, {b.dir, "init"}, {b.dir, "_"}}
	texts := map[string]bool{}
	files := map[string][]string{}
	seen := map[symbol]bool{}
	for len(todo) > 0 {
		next := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[next] {
			continue
		}
		seen[next] = true
		keep := isSource
		if next.dir == b.dir {
			keep = isTestFile
		}
		index, err := w.load(ctx, next.dir, keep)
		if err != nil {
			return "", err
		}
		for _, d := range index[next.name] {
			texts[next.dir+"\x00"+d.text] = true
			todo = append(todo, w.references(next.dir, b.dir, d)...)
			files[next.dir+"\x00"+d.file] = index.fileWide(d.file)
		}
	}
	for place, lines := range files {
		dir, _, _ := strings.Cut(place, "\x00")
		for _, line := range lines {
			texts[dir+"\x00"+line] = true
		}
	}
	sum := sha256.New()
	for _, text := range sortedKeys(texts) {
		fmt.Fprintf(sum, "%s\x00", text)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (w *workloads) references(dir, own string, d decl) []symbol {
	var found []symbol
	ast.Inspect(d.node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			found = append(found, symbol{dir, node.Name})
		case *ast.SelectorExpr:
			name, ok := node.X.(*ast.Ident)
			if !ok {
				break
			}
			if target := w.workloadDir(d.imports[name.Name]); target != "" && target != own {
				found = append(found, symbol{target, node.Sel.Name})
			}
		}
		return true
	})
	return found
}

func (w *workloads) workloadDir(importPath string) string {
	dir, inside := strings.CutPrefix(importPath, w.module+"/")
	if !inside {
		return ""
	}
	for _, root := range w.dirs {
		if dir == root || strings.HasPrefix(dir, root+"/") {
			return dir
		}
	}
	return ""
}
