// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRepo struct {
	t *testing.T
	repo
}

func newTestRepo(t *testing.T) testRepo {
	t.Helper()
	r := testRepo{t, repo{t.TempDir()}}
	r.do("init", "-q", "-b", "main")
	r.write("go.mod", "module example.test/app\n\ngo 1.26\n")
	return r
}

func (r testRepo) do(args ...string) string {
	r.t.Helper()
	args = append([]string{"-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="}, args...)
	cmd := exec.CommandContext(r.t.Context(), "git", args...)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (r testRepo) write(name, text string) {
	r.t.Helper()
	path := filepath.Join(r.root, filepath.FromSlash(name))
	require.NoError(r.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(r.t, os.WriteFile(path, []byte(text), 0o600))
}

func (r testRepo) commit(message string) string {
	r.t.Helper()
	r.do("add", "-A")
	r.do("commit", "-q", "--allow-empty", "-m", message)
	return r.do("rev-parse", "HEAD")
}

const (
	defaultLimits = `"limits": {"sec/op": {"percent": 10, "min": 0.0001, "any": 0.05}}`
	listBench     = "package list\n\nimport (\n\t\"testing\"\n\n\t\"example.test/app/testutil/lib\"\n)\n\nconst size = 100\n\nfunc rows() []int { return make([]int, size) }\n\nfunc BenchmarkList(b *testing.B) {\n\tfor b.Loop() {\n\t\tlib.Use(rows())\n\t}\n}\n"
	otherBench    = "package other\n\nimport \"testing\"\n\nfunc BenchmarkOther(b *testing.B) {\n\tfor b.Loop() {\n\t}\n}\n"
	library       = "package lib\n\nfunc Use(rows []int) { touch(rows) }\n\nfunc touch(rows []int) { clear(rows) }\n\nfunc Unused() {}\n"
)

func benchRepo(t *testing.T) (r testRepo, start string) {
	t.Helper()
	r = newTestRepo(t)
	r.write("list/list_test.go", listBench)
	r.write("other/other_test.go", otherBench)
	r.write("testutil/lib/lib.go", library)
	start = r.commit("add the benchmarks")
	r.write(configPath, benchConfig(start, ""))
	return r, start
}

func benchConfig(start, more string) string {
	return fmt.Sprintf(`{%s, "workload_dirs": ["testutil"], "starts": {"list.BenchmarkList": %q}%s}`, defaultLimits, start, more)
}

func TestCheckNeedsApproval(t *testing.T) {
	replace := func(text, before, after string) string { return strings.Replace(text, before, after, 1) }
	tests := []struct {
		name   string
		change func(r testRepo, start, before string)
		want   string
	}{
		{"a code change", func(r testRepo, _, _ string) { r.write("list/list.go", "package list\n") }, ""},
		{"a new benchmark", func(r testRepo, _, _ string) {
			r.write("list/new_test.go", strings.ReplaceAll(otherBench, "Other", "New"))
		}, ""},
		{"a comment in a benchmark", func(r testRepo, _, _ string) {
			r.write("list/list_test.go", replace(listBench, "const size", "// size\nconst size"))
		}, ""},
		{"a comment on its own line in a benchmark", func(r testRepo, _, _ string) {
			r.write("list/list_test.go", replace(listBench, "\tfor b.Loop", "\n\t// loop\n\tfor b.Loop"))
		}, ""},
		{"a helper that no benchmark calls", func(r testRepo, _, _ string) {
			r.write("testutil/lib/lib.go", replace(library, "Unused() {}", "Unused() { panic(1) }"))
		}, ""},
		{"a benchmark that moves to another file", func(r testRepo, _, _ string) {
			r.do("mv", "list/list_test.go", "list/bench_test.go")
		}, ""},
		{"a stricter limit of one benchmark", func(r testRepo, start, _ string) {
			r.write(configPath, benchConfig(start, `, "benchmarks": {"list.BenchmarkList": {"sec/op": {"percent": 5, "min": 0.0001, "any": 0.05}}}`))
		}, ""},
		{"a looser limit of one benchmark", func(r testRepo, start, _ string) {
			r.write(configPath, benchConfig(start, `, "benchmarks": {"list.BenchmarkList": {"sec/op": {"percent": 5, "min": 0.0002, "any": 0.05}}}`))
		}, "list.BenchmarkList: the limit of sec/op gets looser"},
		{"a looser default limit", func(r testRepo, start, _ string) {
			r.write(configPath, replace(benchConfig(start, ""), `"percent": 10`, `"percent": 11`))
		}, "limits: the limit of sec/op gets looser"},
		{"a starting commit that moves", func(r testRepo, start, before string) {
			r.write(configPath, benchConfig(before, ""))
		}, "list.BenchmarkList: the starting commit moves"},
		{"a starting commit for a benchmark without one", func(r testRepo, start, _ string) {
			r.write(configPath, replace(benchConfig(start, ""), `"starts": {`, fmt.Sprintf(`"starts": {"other.BenchmarkOther": %q, `, start)))
		}, "other.BenchmarkOther: the starting commit moves"},
		{"a workload change without a starting commit", func(r testRepo, _, _ string) {
			r.write("other/other_test.go", replace(otherBench, "b.Loop() {\n", "b.Loop() {\n\t\t_ = make([]int, 9)\n"))
		}, "other.BenchmarkOther: its workload changes"},
		{"a workload change in a helper", func(r testRepo, _, _ string) {
			r.write("list/list_test.go", replace(listBench, "size = 100", "size = 10"))
			r.write(configPath, replace(benchConfig(strings.Repeat("0", 40), ""), fmt.Sprintf(`"list.BenchmarkList": "%040d"`, 0), ""))
		}, "list.BenchmarkList: its workload changes"},
		{"a workload change in a shared folder", func(r testRepo, _, _ string) {
			r.write("testutil/lib/lib.go", replace(library, "clear(rows)", "clear(rows[:1])"))
			r.write(configPath, replace(benchConfig(strings.Repeat("0", 40), ""), fmt.Sprintf(`"list.BenchmarkList": "%040d"`, 0), ""))
		}, "list.BenchmarkList: its workload changes"},
		{"a workload change through an import", func(r testRepo, _, _ string) {
			r.write("testutil/other/lib/lib.go", replace(library, "clear(rows)", "clear(rows[:1])"))
			r.write("list/list_test.go", replace(listBench, "app/testutil/lib", "app/testutil/other/lib"))
			r.write(configPath, replace(benchConfig(strings.Repeat("0", 40), ""), fmt.Sprintf(`"list.BenchmarkList": "%040d"`, 0), ""))
		}, "list.BenchmarkList: its workload changes"},
		{"a build constraint on a benchmark file", func(r testRepo, _, _ string) {
			r.write("other/other_test.go", "//go:build windows\n\n"+otherBench)
		}, "other.BenchmarkOther: its workload changes"},
		{"an init function in a benchmark file", func(r testRepo, _, _ string) {
			r.write("other/other_test.go", otherBench+"\nfunc init() { _ = make([]int, 9) }\n")
		}, "other.BenchmarkOther: its workload changes"},
		{"a benchmark that goes away", func(r testRepo, _, _ string) { r.do("rm", "-q", "other/other_test.go") }, "other.BenchmarkOther: the benchmark is gone"},
		{"a shared folder that no longer counts", func(r testRepo, start, _ string) {
			r.write(configPath, replace(benchConfig(start, ""), `["testutil"]`, `[]`))
		}, "workload_dirs: testutil is gone"},
		{"a shared folder that starts to count", func(r testRepo, start, _ string) {
			r.write(configPath, replace(benchConfig(start, ""), `["testutil"]`, `["testutil", "list"]`))
		}, "workload_dirs: list is new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, start := benchRepo(t)
			before := r.commit("add the limits")
			r.do("switch", "-q", "-c", "branch")
			tt.change(r, start, before)
			after := r.commit("change")
			for _, approved := range []bool{false, true} {
				lines, passed, err := check(t.Context(), r.repo, before, after, approved)
				require.NoError(t, err)
				text := strings.Join(lines, "\n")
				assert.Equal(t, tt.want == "" || approved, passed, text)
				assert.Contains(t, text, tt.want)
			}
		})
	}
}

func TestCheckFailsEvenWithApproval(t *testing.T) {
	tests := []struct {
		name   string
		change func(r testRepo, start string)
		want   string
	}{
		{"a starting commit with another workload", func(r testRepo, _ string) {
			r.write("list/list_test.go", strings.Replace(listBench, "size = 100", "size = 10", 1))
		}, "Delete its entry"},
		{"a starting commit of an unknown benchmark", func(r testRepo, start string) {
			r.write(configPath, strings.Replace(benchConfig(start, ""), "BenchmarkList", "BenchmarkLost", 1))
		}, "list.BenchmarkLost: no such benchmark"},
		{"a limit of an unknown benchmark", func(r testRepo, start string) {
			r.write(configPath, benchConfig(start, `, "benchmarks": {"list.BenchmarkLost/1.7ms": {"sec/op": {"percent": 5, "min": 0.0001, "any": 0.05}}}`))
		}, "list.BenchmarkLost/1.7ms: no such benchmark"},
		{"a starting commit that is not on the base branch", func(r testRepo, _ string) {
			r.write(configPath, benchConfig(r.commit("side"), ""))
		}, "is not on the base branch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, start := benchRepo(t)
			before := r.commit("add the limits")
			tt.change(r, start)
			lines, passed, err := check(t.Context(), r.repo, before, r.commit("change"), true)
			require.NoError(t, err)
			assert.False(t, passed)
			assert.Contains(t, strings.Join(lines, "\n"), tt.want)
		})
	}
}

func TestCheckComparesWithTheMergeBase(t *testing.T) {
	r, start := benchRepo(t)
	r.commit("add the limits")
	r.do("switch", "-q", "-c", "branch")
	r.write("list/list.go", "package list\n")
	after := r.commit("change")
	r.do("switch", "-q", "main")
	r.write(configPath, strings.Replace(benchConfig(start, ""), `"percent": 10`, `"percent": 9`, 1))
	_, passed, err := check(t.Context(), r.repo, r.commit("stricter on main"), after, false)
	require.NoError(t, err)
	assert.True(t, passed)
}

func TestCheckPassesForABranchThatStartsBeforeTheLimits(t *testing.T) {
	r := newTestRepo(t)
	r.write("list/list_test.go", otherBench)
	r.commit("start")
	r.do("switch", "-q", "-c", "branch")
	after := r.commit("change")
	r.do("switch", "-q", "main")
	r.write(configPath, driftLimits)
	_, passed, err := check(t.Context(), r.repo, r.commit("add the limits"), after, false)
	require.NoError(t, err)
	assert.True(t, passed)
}

func TestCheckFailsWithoutReadableLimits(t *testing.T) {
	for name, change := range map[string]func(r testRepo){
		"a file that goes away":      func(r testRepo) { r.do("rm", "-q", configPath) },
		"a file that does not parse": func(r testRepo) { r.write(configPath, "{") },
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := benchRepo(t)
			before := r.commit("add the limits")
			change(r)
			_, passed, err := check(t.Context(), r.repo, before, r.commit("change"), true)
			require.ErrorContains(t, err, configPath)
			assert.False(t, passed)
		})
	}
}
