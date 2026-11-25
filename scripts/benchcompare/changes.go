// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/types"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type unit struct {
	pkg   string
	file  string
	name  string
	parts []span
	span
}

type changes struct {
	units   []unit
	otherGo bool
	module  string
	deps    map[string]map[string]bool
}

func (c changes) empty() bool { return len(c.units) == 0 && !c.otherGo }

func (c changes) reachedBy(dir string) []string {
	var pkgs []string
	for _, u := range c.units {
		if c.deps[dir][u.pkg] {
			pkgs = append(pkgs, u.pkg)
		}
	}
	slices.Sort(pkgs)
	return slices.Compact(pkgs)
}

func (c changes) folder(pkg string) string {
	if pkg == c.module {
		return "."
	}
	return strings.TrimPrefix(pkg, c.module+"/")
}

func changedCode(ctx context.Context, b *builder, baseCommit, headCommit string, now snapshot) (changes, error) {
	c := changes{module: now.workloads.module}
	out, err := b.repo.git(ctx, "", "diff", "--no-ext-diff", "--no-renames", "--name-only", "-z", baseCommit, headCommit)
	if err != nil {
		return c, err
	}
	files := split(out, "\x00")
	if c.units, err = b.repo.changedDecls(ctx, baseCommit, headCommit, c.module, files); err != nil {
		return c, err
	}
	head, err := b.list(ctx, headCommit, now.benches)
	if err != nil {
		return c, err
	}
	c.deps = head.testDeps(c.module)
	c.units = append(c.units, head.changedAssets(files)...)
	out, err = b.repo.git(ctx, "", "diff", "--no-ext-diff", "--no-renames", "--name-only", "-z", "--diff-filter=D", baseCommit, headCommit)
	if err != nil {
		return c, err
	}
	gone := slices.DeleteFunc(split(out, "\x00"), func(file string) bool { return strings.HasSuffix(file, ".go") })
	modules := slices.Contains(files, "go.mod") || slices.Contains(files, "go.sum")
	if len(gone) == 0 && !modules {
		return c, nil
	}
	was, err := b.list(ctx, baseCommit, now.benches)
	if err != nil {
		return c, err
	}
	c.units = append(c.units, was.changedAssets(gone)...)
	if modules {
		c.otherGo = was.goVersion != head.goVersion
		c.units = append(c.units, changedModules(was, head)...)
	}
	return c, nil
}

func (r repo) changedDecls(ctx context.Context, baseCommit, headCommit, module string, files []string) ([]unit, error) {
	var dirs []string
	for _, file := range files {
		if strings.HasSuffix(file, ".go") && isSource(file) {
			dirs = append(dirs, path.Dir(file))
		}
	}
	slices.Sort(dirs)
	var units []unit
	for _, dir := range slices.Compact(dirs) {
		before, err := treeDecls(ctx, commitTree{r, baseCommit}, dir)
		if err != nil {
			return nil, err
		}
		after, err := treeDecls(ctx, commitTree{r, headCommit}, dir)
		if err != nil {
			return nil, err
		}
		found := differing(path.Join(module, dir), before, after)
		for i, u := range found {
			if u.file == "" {
				continue
			}
			if found[i].parts, err = r.changedLines(ctx, baseCommit, headCommit, u); err != nil {
				return nil, err
			}
		}
		units = append(units, found...)
	}
	slices.SortStableFunc(units, func(a, b unit) int { return cmp.Compare(a.from, b.from) })
	return units, nil
}

func treeDecls(ctx context.Context, t commitTree, dir string) (decls, error) {
	files, err := t.goFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	return parseDecls(files, isSource)
}

func differing(pkg string, before, after decls) []unit {
	var units []unit
	var names []string
	for _, key := range sortedKeys(after) {
		for _, d := range after[key] {
			if slices.ContainsFunc(before[key], func(old decl) bool { return old.text == d.text }) {
				continue
			}
			if d.isFunc {
				units = append(units, unit{pkg: pkg, name: d.name, file: d.file, span: span{d.from, d.to}})
			} else {
				names = append(names, d.name)
			}
		}
	}
	for _, key := range sortedKeys(before) {
		for _, old := range before[key] {
			if old.matters() && !slices.ContainsFunc(after[key], func(d decl) bool { return d.name == old.name }) {
				names = append(names, old.name+" (removed)")
			}
		}
	}
	if len(names) > 0 {
		units = append(units, unit{pkg: pkg, name: strings.Join(names, ", ")})
	}
	return units
}

func (d decl) matters() bool {
	plain := d.isFunc && !strings.Contains(d.name, ".") && d.name != "init"
	return !plain || types.Universe.Lookup(d.name) != nil
}

var hunk = regexp.MustCompile(`(?m)^@@ -\S+ \+(\d+)(?:,(\d+))? @@`)

func (r repo) changedLines(ctx context.Context, baseCommit, headCommit string, u unit) ([]span, error) {
	diff, err := r.git(ctx, "", "diff", "--no-ext-diff", "--no-color", "--no-renames", "-U0", baseCommit, headCommit, "--", u.file)
	if err != nil {
		return nil, err
	}
	var parts []span
	for _, match := range hunk.FindAllStringSubmatch(diff, -1) {
		from, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, err
		}
		count := 1
		if match[2] != "" {
			if count, err = strconv.Atoi(match[2]); err != nil {
				return nil, err
			}
		}
		added := span{from, from + count - 1}
		if count == 0 {
			added = span{from, from + 1}
		}
		if added.overlaps(u.span) {
			parts = append(parts, added)
		}
	}
	return parts, nil
}

type listing struct {
	root      string
	goVersion string
	packages  []listed
}

type listed struct {
	ImportPath string
	Dir        string
	ForTest    string
	Deps       []string
	EmbedFiles []string
	SFiles     []string
	CFiles     []string
	HFiles     []string
	Module     *module
}

type module struct {
	Path      string
	Version   string
	Main      bool
	GoVersion string
	Replace   *module
}

func (m *module) version() string {
	switch {
	case m == nil || m.Main:
		return ""
	case m.Replace != nil:
		return m.Replace.Path + "@" + m.Replace.Version
	}
	return m.Path + "@" + m.Version
}

func (b *builder) list(ctx context.Context, commit string, benches []bench) (listing, error) {
	root, err := b.root(ctx, commit)
	if err != nil || len(benches) == 0 {
		return listing{root: root}, err
	}
	const fields = "-json=ImportPath,Dir,ForTest,Deps,EmbedFiles,SFiles,CFiles,HFiles,Module"
	args := []string{"list", "-e", "-test", "-deps", fields}
	for _, one := range benches {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(one.dir))); err == nil {
			args = append(args, "./"+one.dir)
		}
	}
	version, err := goTool(ctx, root, "env", "GOVERSION").Output()
	if err != nil {
		return listing{}, fmt.Errorf("go env: %w", err)
	}
	cmd := goTool(ctx, root, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return listing{}, fmt.Errorf("go list: %s", failure(stderr.Bytes(), err))
	}
	l := listing{root: root, goVersion: string(version)}
	decoder := jsontext.NewDecoder(bytes.NewReader(out))
	for {
		var one listed
		if err := json.UnmarshalDecode(decoder, &one); errors.Is(err, io.EOF) {
			return l, nil
		} else if err != nil {
			return listing{}, fmt.Errorf("go list: %w", err)
		}
		if one.Module != nil && one.Module.Main {
			l.goVersion = string(version) + one.Module.GoVersion
		}
		l.packages = append(l.packages, one)
	}
}

func (l listing) testDeps(moduleName string) map[string]map[string]bool {
	deps := map[string]map[string]bool{}
	for _, one := range l.packages {
		pkg, isTest := strings.CutSuffix(one.ImportPath, ".test")
		if !isTest {
			continue
		}
		dir := strings.TrimPrefix(strings.TrimPrefix(pkg, moduleName), "/")
		if dir == "" {
			dir = "."
		}
		deps[dir] = map[string]bool{}
		for _, dep := range one.Deps {
			plain, _, _ := strings.Cut(dep, " [")
			deps[dir][plain] = true
		}
	}
	return deps
}

func (l listing) changedAssets(files []string) []unit {
	var units []unit
	for _, one := range l.packages {
		if one.Module == nil || !one.Module.Main || one.ForTest != "" {
			continue
		}
		rel, err := filepath.Rel(l.root, one.Dir)
		if err != nil {
			continue
		}
		for _, asset := range slices.Concat(one.EmbedFiles, one.SFiles, one.CFiles, one.HFiles) {
			if slices.Contains(files, path.Join(filepath.ToSlash(rel), asset)) {
				units = append(units, unit{pkg: one.ImportPath, name: asset})
			}
		}
	}
	return units
}

func changedModules(was, now listing) []unit {
	versions := map[string]string{}
	for _, one := range was.packages {
		versions[one.ImportPath] = one.Module.version()
	}
	var units []unit
	for _, one := range now.packages {
		if version := one.Module.version(); version != versions[one.ImportPath] && version != "" {
			units = append(units, unit{pkg: one.ImportPath, name: version})
		}
	}
	return units
}
