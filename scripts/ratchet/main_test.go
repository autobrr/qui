// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regression: #3726
func TestParseLintPaths(t *testing.T) {
	raw := []byte(`{"Issues": [
		{"FromLinter": "gocognit", "Pos": {"Filename": "internal\\api\\handlers\\auth.go"}},
		{"FromLinter": "gocognit", "Pos": {"Filename": "internal/api/handlers/auth.go"}},
		{"FromLinter": "contextcheck", "Pos": {"Filename": "internal\\api/handlers/auth.go"}}
	]}`)
	got, err := parseLint(raw)
	require.NoError(t, err)
	assert.Equal(t, counts{
		"gocognit":     {"internal/api/handlers/auth.go": 2},
		"contextcheck": {"internal/api/handlers/auth.go": 1},
	}, got)
}

func TestCompare(t *testing.T) {
	allow := counts{"nilerr": {"a.go": 2, "b.go": 1}}
	tests := []struct {
		name string
		got  counts
		base counts
		want []string
	}{
		{"matches", counts{"nilerr": {"a.go": 2, "b.go": 1}}, nil, nil},
		{"new problem in a listed file", counts{"nilerr": {"a.go": 3, "b.go": 1}}, nil,
			[]string{"a.go: nilerr reports 3 problems, the allowlist allows 2. Fix the new ones."}},
		{"problem in an unlisted file", counts{"nilerr": {"a.go": 2, "b.go": 1}, "gocognit": {"c.go": 1}}, nil,
			[]string{"c.go: gocognit reports 1 problems, the allowlist allows 0. Fix the new ones."}},
		{"fixed one, entry not lowered", counts{"nilerr": {"a.go": 1, "b.go": 1}}, nil,
			[]string{"a.go: nilerr reports 1 problems. Lower its entry in scripts/ratchet/allowlist.json from 2 to 1."}},
		{"fixed all, entry not deleted", counts{"nilerr": {"a.go": 2}}, nil,
			[]string{"b.go: nilerr reports no problems. Delete its entry from scripts/ratchet/allowlist.json."}},
		{"same as base", counts{"nilerr": {"a.go": 2, "b.go": 1}}, counts{"nilerr": {"a.go": 2, "b.go": 1}}, nil},
		{"grew against base", counts{"nilerr": {"a.go": 2, "b.go": 1}}, counts{"nilerr": {"a.go": 1}},
			[]string{"nilerr: the total grew from 1 to 3. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report. These entries grew: a.go from 1 to 2, b.go from 0 to 1."}},
		{"moved to another file", counts{"nilerr": {"a.go": 2, "b.go": 1}}, counts{"nilerr": {"a.go": 1, "c.go": 2}}, nil},
		{"moved and fixed one", counts{"nilerr": {"a.go": 2, "b.go": 1}}, counts{"nilerr": {"c.go": 4}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compare(tt.got, allow, tt.base))
		})
	}
}

func TestCompareNewLinter(t *testing.T) {
	got := counts{"nilerr": {"a.go": 1}, "forbidigo": {"c.go": 3}}
	assert.Empty(t, compare(got, got, counts{"nilerr": {"a.go": 1}}))
	assert.Equal(t, []string{"forbidigo: the total grew from 3 to 4. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report. These entries grew: c.go from 3 to 4."},
		compare(counts{"forbidigo": {"c.go": 4}}, counts{"forbidigo": {"c.go": 4}}, counts{"forbidigo": {"c.go": 3}}))
}

func TestCompareZeroEntry(t *testing.T) {
	allow := counts{"nilerr": {"a.go": 0}}
	assert.Equal(t, []string{"a.go: nilerr reports no problems. Delete its entry from scripts/ratchet/allowlist.json."},
		compare(counts{}, allow, nil))
}

func TestLowerAllowlist(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Dir(allowlistPath), 0o750))
	got := counts{"nilerr": {"a.go": 1, "c.go": 3, "d.go": 2}, "forbidigo": {"e.go": 1}}
	allow := counts{"nilerr": {"a.go": 2, "b.go": 1, "c.go": 3, "d.go": 1}}

	require.NoError(t, lowerAllowlist(got, allow))

	raw, err := os.ReadFile(allowlistPath)
	require.NoError(t, err)
	written, err := parse(raw)
	require.NoError(t, err)
	assert.Equal(t, counts{"nilerr": {"a.go": 1, "c.go": 3, "d.go": 1}}, written)
	assert.Equal(t, []string{
		"e.go: forbidigo reports 1 problems, the allowlist allows 0. Fix the new ones.",
		"d.go: nilerr reports 2 problems, the allowlist allows 1. Fix the new ones.",
	}, compare(got, written, nil))
}

// regression: #4404
func TestLowerAllowlistKeepsTheFileWhenCodeDoesNotCompile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Dir(allowlistPath), 0o750))
	got := counts{"typecheck": {"internal/api/handlers/application.go": 1}}
	allow := counts{"gocognit": {"a.go": 2}}

	err := lowerAllowlist(got, allow)

	require.ErrorContains(t, err, "cannot type-check internal/api/handlers/application.go")
	assert.NoFileExists(t, allowlistPath)
	assert.Equal(t, counts{"gocognit": {"a.go": 2}}, allow)
}

// Each case matches what golangci-lint v2.14.0 does with the comment above a
// function that breaks gocognit.
func TestHidesGocognit(t *testing.T) {
	tests := []struct {
		comment string
		want    bool
	}{
		{"//nolint", true},
		{"//nolint // reason", true},
		{"//nolint:gocognit", true},
		{"//nolint:gocognit // reason", true},
		{"// nolint:gocognit", true},
		{"//nolint:GOCOGNIT", true},
		{"//nolint:gosec,all", true},
		{"//nolint:gosec, all", true},
		{"//nolint:gosec, gocognit", true},
		{"//nolint:gosec ,gocognit", true},
		{"//nolint:gosec,  gocognit // x", true},
		{"//nolint: gocognit", true},
		{"//nolint :gocognit", true},
		{"//nolint:allow", true},
		{"//nolint:gosec", false},
		{"//nolint:gosec // gocognit is fine", false},
		{"//nolint:gosec,allow", false},
		{"//nolint:gocognitx", false},
		{"//NOLINT:gocognit", false},
		{"//nolintx", false},
		{"// see nolint:gocognit", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, hidesGocognit(tt.comment), tt.comment)
	}
}

func TestStaleNolintNames(t *testing.T) {
	enabled := map[string]bool{"gosec": true, "gocognit": true}
	tests := []struct {
		comment string
		want    []string
	}{
		{"//nolint:gosec // reason", nil},
		{"//nolint:gosec,gocognit", nil},
		{"//nolint", nil},
		{"//nolint:all", nil},
		{"// see nolint:funlen", nil},
		{"//nolint:funlen // reason", []string{"funlen"}},
		{"//nolint:gosec, Funlen,dupl // gosec is fine", []string{"funlen", "dupl"}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, staleNolintNames(tt.comment, enabled), tt.comment)
	}
}

func TestSuppressionProblems(t *testing.T) {
	parse := func(raw string) counts {
		got, err := parseSuppressions([]byte(raw))
		require.NoError(t, err)
		return got
	}
	rules := []string{"qui/no-comments", "qui/no-direct-effect"}
	was := parse(`{"src/a.tsx": {"qui/no-comments": {"count": 2}, "qui/no-direct-effect": {"count": 1}}}`)
	assert.Empty(t, suppressionProblems(parse(`{"src/a.tsx": {"qui/no-comments": {"count": 1}, "qui/no-direct-effect": {"count": 1}}}`), was, rules))
	assert.Empty(t, suppressionProblems(parse(`{"src/b.tsx": {"qui/no-comments": {"count": 1}}, "src/c.tsx": {"qui/no-comments": {"count": 1}, "qui/no-direct-effect": {"count": 1}}}`), was, rules), "a move keeps the total level")
	assert.Empty(t, suppressionProblems(was, parse(`{"src/a.tsx": {"qui/no-comments": {"count": 2}}}`), rules), "a rule that the base does not hold starts with its counts")
	assert.Equal(t, []string{
		"web/eslint-suppressions.json: the total of qui/no-comments grew from 2 to 3. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report as its ESLint message says.",
		"web/eslint-suppressions.json: the total of qui/no-direct-effect grew from 1 to 2. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report as its ESLint message says.",
	}, suppressionProblems(parse(`{"src/a.tsx": {"qui/no-comments": {"count": 2}, "qui/no-direct-effect": {"count": 1}}, "src/b.tsx": {"qui/no-comments": {"count": 1}, "qui/no-direct-effect": {"count": 1}}}`), was, rules))
	assert.Equal(t, []string{
		"web/eslint-suppressions.json: holds 20 reports of no-undef. Only these rules may have entries there: qui/no-comments, qui/no-direct-effect. Fix each report as its ESLint message says, and delete the entries.",
	}, suppressionProblems(parse(`{"src/a.tsx": {"qui/no-comments": {"count": 2}, "qui/no-direct-effect": {"count": 1}, "no-undef": {"count": 20}}}`), was, rules))
	assert.Equal(t, []string{
		"web/eslint-suppressions.json: has no entry left for qui/no-direct-effect. Delete it from suppressedRules in scripts/ratchet/comments.go, so that its reports cannot come back without a change there.",
	}, suppressionProblems(parse(`{"src/a.tsx": {"qui/no-comments": {"count": 2}}}`), was, rules), "a rule whose last entry goes leaves suppressedRules")
}

func TestGrowthNamesOnlyTheLinterThatGrew(t *testing.T) {
	base := counts{commentsLinter: {"a.go": 3, "b.go": 1}, "gocognit": {"a.go": 1}}
	assert.Empty(t, growth(counts{commentsLinter: {"c.go": 4}, "gocognit": {"b.go": 1}}, base))
	assert.Equal(t, []string{
		"comments: the total grew from 4 to 6. The total only shrinks: code that moves to another file keeps it level, a new report does not. Fix the new report. These entries grew: a.go from 3 to 5.",
	}, growth(counts{commentsLinter: {"a.go": 5, "b.go": 1}, "gocognit": {"b.go": 1}}, base))
}

func TestOffLogProblems(t *testing.T) {
	lines := map[string][]int{"grown.go": {12, 40}, "level.go": {7}}
	allow := map[string]int{"grown.go": 1, "level.go": 1}
	assert.Equal(t, []string{
		"grown.go:12: a DEBUG or INFO line about a feature that is off. Delete the line, or write it at TRACE. See docs/standards/logging.md.",
		"grown.go:40: a DEBUG or INFO line about a feature that is off. Delete the line, or write it at TRACE. See docs/standards/logging.md.",
	}, offLogProblems(lines, allow))
}

func TestCountMapAny(t *testing.T) {
	tests := []struct {
		name string
		decl string
		want int
	}{
		{"any value", `var x = map[string]any{"a": 1}`, 1},
		{"empty interface value", `func f(m map[string]interface{}) {}`, 1},
		{"nested", `type T struct{ M map[string][]map[string]any; N map[string]any }`, 2},
		{"typed value", `var x map[string]string`, 0},
		{"other key", `var x map[int]any`, 0},
		{"interface with a method", `var x map[string]interface{ M() }`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := countMapAny("x.go", []byte("package x\n"+tt.decl+"\n"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, n)
		})
	}
}

func TestCountTestSleeps(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"real sleep", `import "time"; func TestX(t *testing.T) { time.Sleep(time.Second) }`, 1},
		{"sleep in a bubble", `import ("testing/synctest"; "time"); func TestX(t *testing.T) { synctest.Test(t, func(t *testing.T) { time.Sleep(time.Second) }) }`, 0},
		{"sleep in a bubble in parentheses", `import ("testing/synctest"; "time"); func TestX(t *testing.T) { synctest.Test(t, (func(t *testing.T) { time.Sleep(time.Second) })) }`, 0},
		{"sleep after a bubble", `import ("testing/synctest"; "time"); func TestX(t *testing.T) { synctest.Test(t, func(*testing.T) {}); time.Sleep(time.Second) }`, 1},
		{"sleep in a factory that runs before the bubble", `import ("testing/synctest"; "time"); func TestX(t *testing.T) { synctest.Test(t, func() func(*testing.T) { time.Sleep(time.Second); return func(*testing.T) {} }()) }`, 1},
		{"sleep in a callback that a factory returns", `import ("testing/synctest"; "time"); func TestX(t *testing.T) { synctest.Test(t, func() func(*testing.T) { return func(*testing.T) { time.Sleep(time.Second) } }()) }`, 0},
		{"sleep beside a bubble that waits", `import ("testing/synctest"; "time"); func TestX(t *testing.T) { time.Sleep(time.Second); synctest.Test(t, func(t *testing.T) { synctest.Wait() }) }`, 1},
		{"helper that waits on the bubble", `import ("testing/synctest"; "time"); func wait(d time.Duration) { time.Sleep(d); synctest.Wait() }`, 0},
		{"literal that waits on the bubble", `import ("testing/synctest"; "time"); var wait = func(d time.Duration) { time.Sleep(d); synctest.Wait() }`, 0},
		{"helper without a wait", `import "time"; func wait(d time.Duration) { time.Sleep(d) }`, 1},
		{"renamed import", `import clock "time"; func TestX(t *testing.T) { clock.Sleep(1) }`, 1},
		{"sleep as a value", `import "time"; var sleep = time.Sleep`, 1},
		{"no time import", `func TestX(t *testing.T) { time.Sleep(1) }`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := countTestSleeps("x_test.go", []byte("package x\n"+tt.src+"\n"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, n)
		})
	}
}
