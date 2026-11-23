// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/autobrr/qui/scripts/comments/gocomments"
	"github.com/autobrr/qui/scripts/offlog"
)

const (
	commentsLinter   = "comments"
	offlogLinter     = "offlog"
	suppressionsPath = "web/eslint-suppressions.json"
)

var suppressedRules = []string{"qui/disable-reason", "qui/entry-files", "qui/layer-order", "qui/no-comments", "qui/no-direct-effect", "react-hooks/exhaustive-deps", "react-refresh/only-export-components"}

func goComments() (map[string]int, error) {
	got := map[string]int{}
	err := eachTrackedGoFile(func(path string, src []byte) error {
		n, err := gocomments.Count(path, src)
		if n > 0 {
			got[path] = n
		}
		return err
	})
	return got, err
}

func offLogLines() (map[string][]int, error) {
	got := map[string][]int{}
	err := eachTrackedGoFile(func(path string, src []byte) error {
		lines, err := offlog.Lines(path, src)
		if len(lines) > 0 {
			got[path] = lines
		}
		return err
	})
	return got, err
}

func eachTrackedGoFile(visit func(path string, src []byte) error) error {
	paths, err := gocomments.Tracked()
	if err != nil {
		return err
	}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := visit(path, src); err != nil {
			return err
		}
	}
	return nil
}

func offLogProblems(lines map[string][]int, allow map[string]int) []string {
	var problems []string
	for _, file := range slices.Sorted(maps.Keys(lines)) {
		if len(lines[file]) <= allow[file] {
			continue
		}
		for _, line := range lines[file] {
			problems = append(problems, fmt.Sprintf("%s:%d: a DEBUG or INFO line about a feature that is off. Delete the line, or write it at TRACE. See docs/standards/logging.md.", file, line))
		}
	}
	return problems
}

func writeCommentCounts() error {
	raw, err := os.ReadFile(allowlistPath)
	if err != nil {
		return err
	}
	allow, err := parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", allowlistPath, err)
	}
	if allow[commentsLinter], err = goComments(); err != nil {
		return err
	}
	return writeAllowlist(allow)
}

func suppressionGrowth(base string) ([]string, error) {
	raw, err := os.ReadFile(suppressionsPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now, err := parseSuppressions(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", suppressionsPath, err)
	}
	//nolint:gosec // the developer or CI picks -base
	baseRaw, showErr := exec.CommandContext(context.Background(), "git", "show", base+":"+suppressionsPath).Output()
	if showErr != nil {
		fmt.Printf("ratchet: no %s at %s, skipping its growth check\n", suppressionsPath, base)
		baseRaw = raw
	}
	was, err := parseSuppressions(baseRaw)
	if err != nil {
		return nil, fmt.Errorf("%s at %s: %w", suppressionsPath, base, err)
	}
	return suppressionProblems(now, was, suppressedRules), nil
}

func suppressionProblems(now, was counts, rules []string) []string {
	var problems []string
	for _, rule := range rules {
		if _, ok := now[rule]; !ok {
			problems = append(problems, fmt.Sprintf("%s: has no entry left for %s. Delete it from suppressedRules in scripts/ratchet/comments.go, so that its reports cannot come back without a change there.", suppressionsPath, rule))
		}
	}
	for _, rule := range slices.Sorted(maps.Keys(now)) {
		if !slices.Contains(rules, rule) {
			problems = append(problems, fmt.Sprintf("%s: holds %d reports of %s. Only these rules may have entries there: %s. Fix each report as its ESLint message says, and delete the entries.", suppressionsPath, sum(now[rule]), rule, strings.Join(rules, ", ")))
			continue
		}
		if _, ok := was[rule]; !ok {
			continue
		}
		if total, before := sum(now[rule]), sum(was[rule]); total > before {
			problems = append(problems, fmt.Sprintf("%s: the total of %s grew from %d to %d. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report as its ESLint message says.", suppressionsPath, rule, before, total))
		}
	}
	return problems
}

func parseSuppressions(raw []byte) (counts, error) {
	var files map[string]map[string]struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	got := counts{}
	for file, rules := range files {
		for rule, entry := range rules {
			if got[rule] == nil {
				got[rule] = map[string]int{}
			}
			got[rule]["web/"+file] = entry.Count
		}
	}
	return got, nil
}
