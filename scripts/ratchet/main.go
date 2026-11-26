// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"encoding/json/jsontext"
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

type counts map[string]map[string]int

func main() {
	base := flag.String("base", "origin/develop", "git ref whose allowlist this one may not exceed")
	lower := flag.Bool("lower", false, "lower each allowlist entry above the count of its file before the check, and delete an entry at 0")
	flag.Parse()

	got, err := lint()
	if err != nil {
		fail(err)
	}
	if got[mapAnyLinter], err = mapAnyCounts(); err != nil {
		fail(err)
	}
	if got[testSleepLinter], err = testSleepCounts(); err != nil {
		fail(err)
	}
	offLines, err := offLogLines()
	if err != nil {
		fail(err)
	}
	got[offlogLinter] = map[string]int{}
	for file, lines := range offLines {
		got[offlogLinter][file] = len(lines)
	}
	raw, err := os.ReadFile(allowlistPath)
	if err != nil {
		fail(err)
	}
	allow, err := parse(raw)
	if err != nil {
		fail(fmt.Errorf("%s: %w", allowlistPath, err))
	}
	if *lower {
		if err := lowerAllowlist(got, allow); err != nil {
			fail(err)
		}
	}
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
	problems = append(problems, offLogProblems(offLines, allow[offlogLinter])...)
	enabled, err := enabledLinters()
	if err != nil {
		fail(err)
	}
	hidden, err := nolintProblems(enabled)
	if err != nil {
		fail(err)
	}
	problems = append(problems, hidden...)
	grown, err := suppressionGrowth(*base)
	if err != nil {
		fail(err)
	}
	problems = append(problems, grown...)
	oversized, err := fileSizeProblems(*base)
	if err != nil {
		fail(err)
	}
	problems = append(problems, oversized...)
	comments, err := commentProblems()
	if err != nil {
		fail(err)
	}
	problems = append(problems, comments...)

	for _, linter := range slices.Sorted(maps.Keys(got)) {
		fmt.Printf("ratchet: %s has %d problems in %d files\n", linter, sum(got[linter]), len(got[linter]))
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
	return parseLint(raw)
}

func parseLint(raw []byte) (counts, error) {
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
		filename := strings.ReplaceAll(issue.Pos.Filename, `\`, "/")
		got[issue.FromLinter][filename]++
	}
	return got, nil
}

func parse(raw []byte) (counts, error) {
	var c counts
	return c, json.Unmarshal(raw, &c)
}

func compare(got, allow, base counts) []string {
	var problems []string
	for _, linter := range slices.Sorted(maps.Keys(merge(got, allow))) {
		for _, file := range slices.Sorted(maps.Keys(merge(got[linter], allow[linter]))) {
			n, want := got[linter][file], allow[linter][file]
			_, listed := allow[linter][file]
			switch {
			case n > want:
				problems = append(problems, fmt.Sprintf("%s: %s reports %d problems, the allowlist allows %d. Fix the new ones.%s", file, linter, n, want, linterFixes[linter]))
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
	return append(problems, growth(allow, base)...)
}

func lowerEntries(got, allow counts) []string {
	var lowered []string
	for _, linter := range slices.Sorted(maps.Keys(allow)) {
		for _, file := range slices.Sorted(maps.Keys(allow[linter])) {
			n, want := got[linter][file], allow[linter][file]
			switch {
			case n == 0:
				delete(allow[linter], file)
				lowered = append(lowered, fmt.Sprintf("%s: %s from %d to 0, entry deleted", file, linter, want))
			case n < want:
				allow[linter][file] = n
				lowered = append(lowered, fmt.Sprintf("%s: %s from %d to %d", file, linter, want, n))
			}
		}
	}
	return lowered
}

func lowerAllowlist(got, allow counts) error {
	if broken := got["typecheck"]; len(broken) > 0 {
		return fmt.Errorf("golangci-lint cannot type-check %s, so the other counts are not complete and the allowlist stays as it is. Make the code compile, then run the ratchet again", strings.Join(slices.Sorted(maps.Keys(broken)), ", "))
	}
	lowered := lowerEntries(got, allow)
	if len(lowered) == 0 {
		return nil
	}
	if err := writeAllowlist(allow); err != nil {
		return err
	}
	for _, l := range lowered {
		fmt.Println("ratchet: lowered", l)
	}
	return nil
}

func writeAllowlist(allow counts) error {
	out, err := json.Marshal(allow, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	//nolint:gosec // the allowlist is a tracked file that others read
	return os.WriteFile(allowlistPath, append(out, '\n'), 0o644)
}

func growth(allow, base counts) []string {
	var problems []string
	for _, linter := range slices.Sorted(maps.Keys(allow)) {
		if _, ok := base[linter]; !ok {
			continue
		}
		now, was := sum(allow[linter]), sum(base[linter])
		if now <= was {
			continue
		}
		var grown []string
		for _, file := range slices.Sorted(maps.Keys(allow[linter])) {
			if want, before := allow[linter][file], base[linter][file]; want > before {
				grown = append(grown, fmt.Sprintf("%s from %d to %d", file, before, want))
			}
		}
		problems = append(problems, fmt.Sprintf("%s: the total grew from %d to %d. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report. These entries grew: %s.", linter, was, now, strings.Join(grown, ", ")))
	}
	return problems
}

func sum(files map[string]int) int {
	total := 0
	for _, n := range files {
		total += n
	}
	return total
}

func merge[V any](a, b map[string]V) map[string]V {
	m := maps.Clone(a)
	if m == nil {
		m = map[string]V{}
	}
	maps.Copy(m, b)
	return m
}

func enabledLinters() (map[string]bool, error) {
	enabled := map[string]bool{}
	for _, config := range []string{".golangci.yml", configPath} {
		out, err := exec.CommandContext(context.Background(), "golangci-lint", "linters", "-c", config, "--json").Output()
		if err != nil {
			return nil, fmt.Errorf("golangci-lint linters -c %s: %w", config, err)
		}
		var list struct {
			Enabled []struct {
				Name string `json:"name"`
			}
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, fmt.Errorf("golangci-lint linters -c %s: %w", config, err)
		}
		for _, linter := range list.Enabled {
			enabled[linter.Name] = true
		}
	}
	return enabled, nil
}

func nolintProblems(enabled map[string]bool) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "grep", "-lz", "nolint", "--", "*.go").Output()
	if err != nil {
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
				if !strings.HasSuffix(path, "_test.go") && hidesGocognit(c.Text) {
					problems = append(problems, fmt.Sprintf("%s: %s: do not hide gocognit. Split the function.", fset.Position(c.Pos()), c.Text))
				}
				for _, name := range staleNolintNames(c.Text, enabled) {
					problems = append(problems, fmt.Sprintf("%s: %s: no golangci-lint config runs %s, so the nolint hides nothing. Delete the name.", fset.Position(c.Pos()), c.Text, name))
				}
			}
		}
	}
	return problems, nil
}

func staleNolintNames(comment string, enabled map[string]bool) []string {
	var stale []string
	for _, name := range nolintNames(comment) {
		if name != "" && name != "all" && !enabled[name] {
			stale = append(stale, name)
		}
	}
	return stale
}

func hidesGocognit(comment string) bool {
	names := nolintNames(comment)
	return slices.Contains(names, "gocognit") || slices.Contains(names, "all")
}

func nolintNames(comment string) []string {
	text := strings.TrimLeft(comment, "/ ")
	list, ok := strings.CutPrefix(text, "nolint:")
	switch {
	case !ok:
		if text == "nolint" || strings.HasPrefix(text, "nolint ") {
			return []string{"all"}
		}
		return nil
	case strings.HasPrefix(list, "all"):
		return []string{"all"}
	}
	list, _, _ = strings.Cut(list, "//")
	var names []string
	for item := range strings.SplitSeq(list, ",") {
		names = append(names, strings.ToLower(strings.TrimSpace(item)))
	}
	return names
}
