// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"

	"github.com/autobrr/qui/scripts/comments/gocomments"
)

const (
	commentsLinter   = "comments"
	suppressionsPath = "web/eslint-suppressions.json"
	suppressionsRule = "qui/no-comments"
)

func goComments() (map[string]int, error) {
	paths, err := gocomments.Tracked()
	if err != nil {
		return nil, err
	}
	got := map[string]int{}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		n, err := gocomments.Count(path, src)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			got[path] = n
		}
	}
	return got, nil
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
	out, err := json.Marshal(allow, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	//nolint:gosec // the allowlist is a tracked file that others read
	return os.WriteFile(allowlistPath, append(out, '\n'), 0o644)
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
	return growth(counts{suppressionsRule: now}, counts{suppressionsRule: was}), nil
}

func parseSuppressions(raw []byte) (map[string]int, error) {
	var files map[string]map[string]struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	got := map[string]int{}
	for file, rules := range files {
		if rule, ok := rules[suppressionsRule]; ok {
			got["web/"+file] = rule.Count
		}
	}
	return got, nil
}
