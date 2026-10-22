// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command ratchet holds the linters in golangci.yml to a shrink-only
// allowlist. allowlist.json maps each linter to the files that broke it when
// it landed, with the number of problems in each. The command fails when:
//
//  1. a file has more problems than its entry allows, or a file without an
//     entry has any;
//  2. a file has fewer problems than its entry, so the entry must go down
//     (delete it at 0);
//  3. the allowlist grew against the base ref: a new entry or a higher count;
//  4. a nolint comment hides gocognit. The fix for a complex function is to
//     split it.
//
// Run it from the repo root: go run ./scripts/ratchet [-base origin/develop]
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
)

const (
	configPath    = "scripts/ratchet/golangci.yml"
	allowlistPath = "scripts/ratchet/allowlist.json"
)

// counts maps a linter to a file to its number of problems.
type counts map[string]map[string]int

func main() {
	base := flag.String("base", "origin/develop", "git ref whose allowlist this one may not exceed")
	flag.Parse()

	got, err := lint()
	if err != nil {
		fail(err)
	}
	raw, err := os.ReadFile(allowlistPath)
	if err != nil {
		fail(err)
	}
	allow, err := parse(raw)
	if err != nil {
		fail(fmt.Errorf("%s: %w", allowlistPath, err))
	}
	// A wrong ref must fail, not skip the growth check.
	//nolint:gosec // the developer or CI picks -base
	if err := exec.CommandContext(context.Background(), "git", "rev-parse", "--verify", "--quiet", *base+"^{commit}").Run(); err != nil {
		fail(fmt.Errorf("-base %s is not a commit: %w", *base, err))
	}
	var baseAllow counts
	//nolint:gosec // the developer or CI picks -base
	if raw, err := exec.CommandContext(context.Background(), "git", "show", *base+":"+allowlistPath).Output(); err == nil {
		if baseAllow, err = parse(raw); err != nil {
			fail(fmt.Errorf("%s at %s: %w", allowlistPath, *base, err))
		}
	} else {
		fmt.Printf("ratchet: no %s at %s, skipping the growth check\n", allowlistPath, *base)
	}

	problems := compare(got, allow, baseAllow)
	hidden, err := hiddenGocognit()
	if err != nil {
		fail(err)
	}
	problems = append(problems, hidden...)

	for _, linter := range slices.Sorted(maps.Keys(got)) {
		total := 0
		for _, n := range got[linter] {
			total += n
		}
		fmt.Printf("ratchet: %s has %d problems in %d files\n", linter, total, len(got[linter]))
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Println("ratchet:", p)
		}
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ratchet:", err)
	os.Exit(2)
}

func lint() (counts, error) {
	out, err := os.CreateTemp("", "ratchet-*.json")
	if err != nil {
		return nil, err
	}
	out.Close()
	defer os.Remove(out.Name())

	//nolint:gosec // out is a temp file that this command created
	cmd := exec.CommandContext(context.Background(), "golangci-lint", "run", "-c", configPath, "--issues-exit-code=0", "--output.json.path="+out.Name(), "./...")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("golangci-lint: %w", err)
	}
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		return nil, err
	}
	var report struct {
		Issues []struct {
			FromLinter string
			Pos        struct{ Filename string }
		}
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("golangci-lint output: %w", err)
	}
	got := counts{}
	for _, issue := range report.Issues {
		if got[issue.FromLinter] == nil {
			got[issue.FromLinter] = map[string]int{}
		}
		got[issue.FromLinter][issue.Pos.Filename]++
	}
	return got, nil
}

func parse(raw []byte) (counts, error) {
	var c counts
	return c, json.Unmarshal(raw, &c)
}

// compare returns one message per rule 1-3 break. A nil base skips rule 3.
func compare(got, allow, base counts) []string {
	var problems []string
	for _, linter := range slices.Sorted(maps.Keys(merge(got, allow))) {
		for _, file := range slices.Sorted(maps.Keys(merge(got[linter], allow[linter]))) {
			n, want := got[linter][file], allow[linter][file]
			_, listed := allow[linter][file]
			switch {
			case n > want:
				problems = append(problems, fmt.Sprintf("%s: %s reports %d problems, the allowlist allows %d. Fix the new ones.", file, linter, n, want))
			case n == 0 && listed:
				problems = append(problems, fmt.Sprintf("%s: %s reports no problems. Delete its entry from %s.", file, linter, allowlistPath))
			case n < want:
				problems = append(problems, fmt.Sprintf("%s: %s reports %d problems. Lower its entry in %s from %d to %d.", file, linter, n, allowlistPath, want, n))
			}
		}
	}
	if base == nil {
		return problems
	}
	for _, linter := range slices.Sorted(maps.Keys(allow)) {
		for _, file := range slices.Sorted(maps.Keys(allow[linter])) {
			if want, was := allow[linter][file], base[linter][file]; want > was {
				problems = append(problems, fmt.Sprintf("%s: the %s entry grew from %d to %d. The allowlist only shrinks.", file, linter, was, want))
			}
		}
	}
	return problems
}

// merge returns the union of the keys of a and b, for iteration only.
func merge[V any](a, b map[string]V) map[string]V {
	m := maps.Clone(a)
	if m == nil {
		m = map[string]V{}
	}
	maps.Copy(m, b)
	return m
}

// hiddenGocognit reports rule 4: a nolint comment that covers gocognit.
func hiddenGocognit() ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "grep", "-lz", "nolint",
		"--", "*.go", ":!*_test.go").Output()
	if err != nil {
		// git grep exits 1 when nothing matches.
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("git grep: %w", err)
	}
	var problems []string
	fset := token.NewFileSet()
	for path := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, group := range f.Comments {
			for _, c := range group.List {
				if hidesGocognit(c.Text) {
					problems = append(problems, fmt.Sprintf("%s: %s: do not hide gocognit. Split the function.", fset.Position(c.Pos()), c.Text))
				}
			}
		}
	}
	return problems, nil
}

// hidesGocognit reports whether golangci-lint reads comment as a nolint
// directive that covers gocognit. It follows extractInlineRangeFromComment in
// golangci-lint v2.14.0 pkg/result/processors/nolint_filter.go, where a bare
// "nolint", "nolint " and any "nolint:all" prefix hide every linter.
func hidesGocognit(comment string) bool {
	text := strings.TrimLeft(comment, "/ ")
	list, ok := strings.CutPrefix(text, "nolint:")
	switch {
	case !ok:
		return text == "nolint" || strings.HasPrefix(text, "nolint ")
	case strings.HasPrefix(list, "all"):
		return true
	}
	list, _, _ = strings.Cut(list, "//")
	for item := range strings.SplitSeq(list, ",") {
		if name := strings.ToLower(strings.TrimSpace(item)); name == "gocognit" || name == "all" {
			return true
		}
	}
	return false
}
