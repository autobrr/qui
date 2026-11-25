// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sumCode = "package sum\n\nconst Step = 1\n\nfunc Setup() int { return 8 }\n\nfunc Add(n int) int {\n\tif n < 0 {\n\t\tn = -n\n\t}\n\treturn n + Step\n}\n\nfunc Other(n int) int { return n }\n"
	sumTest = "package sum\n\nimport \"testing\"\n\nfunc BenchmarkAdd(b *testing.B) {\n\tn := Setup()\n\tfor b.Loop() {\n\t\tAdd(n)\n\t}\n}\n\nfunc BenchmarkOther(b *testing.B) {\n\tfor b.Loop() {\n\t\tOther(1)\n\t}\n}\n"
	useTest = "package use\n\nimport (\n\t\"testing\"\n\n\t\"example.test/app/sum\"\n)\n\nfunc BenchmarkUse(b *testing.B) {\n\tfor b.Loop() {\n\t\tsum.Add(1)\n\t}\n}\n"
	numCode = "package num\n\nfunc One() int { return 1 }\n"
	numUser = "package sum\n\nimport \"example.test/app/num\"\n\nfunc Pick() int { return num.One() }\n"
	kind    = "package sum\n\ntype Kind int\n\nfunc (Kind) String() string { return \"kind\" }\n\nfunc Gone() {}\n"
	farTest = "package far\n\nimport \"testing\"\n\nfunc BenchmarkFar(b *testing.B) {\n\tfor b.Loop() {\n\t}\n}\n"
)

func TestSelectionFollowsTheChangedCode(t *testing.T) {
	replace := func(before, after string) func(r testRepo) {
		return func(r testRepo) { r.write("sum/sum.go", strings.Replace(sumCode, before, after, 1)) }
	}
	tests := []struct {
		name      string
		change    func(r testRepo)
		touched   []string
		uncovered []string
	}{
		{"a function that two packages run", replace("return n + Step", "return n + Step + 1"), []string{"sum.BenchmarkAdd", "use.BenchmarkUse"}, nil},
		{"a branch that no benchmark takes", replace("n = -n", "n = -n * 2"), nil, []string{"sum/sum.go: Add (9)"}},
		{"code that runs only before the loop", replace("return 8", "return 9"), nil, []string{"sum/sum.go: Setup (5)"}},
		{"a deleted line", replace("\tif n < 0 {\n\t\tn = -n\n\t}\n", ""), []string{"sum.BenchmarkAdd", "use.BenchmarkUse"}, nil},
		{"a constant", replace("Step = 1", "Step = 2"), []string{"sum.BenchmarkAdd", "sum.BenchmarkOther", "use.BenchmarkUse"}, nil},
		{"a comment", replace("func Add", "// Add adds.\nfunc Add"), nil, nil},
		{"a new function", func(r testRepo) { r.write("sum/new.go", "package sum\n\nfunc New() int {\n\treturn 1\n}\n") }, nil, []string{"sum/new.go: New (4-5)"}},
		{"a package without a benchmark", func(r testRepo) { r.write("lone/lone.go", "package lone\n\nfunc Lone() int { return 1 }\n") }, nil, []string{"lone/lone.go: Lone (3)"}},
		{"a file that is not Go", func(r testRepo) { r.write("readme.md", "hi\n") }, nil, nil},
		{"an import under the same name", func(r testRepo) {
			r.write("sum/pick.go", strings.Replace(numUser, "app/num", "app/slow/num", 1))
		}, nil, []string{"sum/pick.go: Pick (5)"}},
		{"a deleted method", func(r testRepo) {
			r.write("sum/kind.go", "package sum\n\ntype Kind int\n\nfunc Gone() {}\n")
		}, []string{"sum.BenchmarkAdd", "sum.BenchmarkOther", "use.BenchmarkUse"}, nil},
		{"a deleted function that nothing calls", func(r testRepo) {
			r.write("sum/kind.go", strings.Replace(kind, "func Gone() {}\n", "", 1))
		}, nil, nil},
		{"a build constraint", func(r testRepo) {
			r.write("lone/old.go", "//go:build !plan9\n\npackage lone\n")
		}, nil, []string{"lone: //go:build !plan9"}},
		{"a new benchmark", func(r testRepo) {
			r.write("sum/new_test.go", strings.Replace(farTest, "package far", "package sum", 1))
		}, nil, nil},
		{"the version of Go", func(r testRepo) {
			r.write("go.mod", "module example.test/app\n\ngo 1.25\n")
			r.write("lone/lone.go", "package lone\n\nfunc Lone() int { return 1 }\n")
		}, []string{"far.BenchmarkFar", "sum.BenchmarkAdd", "sum.BenchmarkOther", "use.BenchmarkUse"}, []string{"lone/lone.go: Lone (3)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t)
			r.write("sum/sum.go", sumCode)
			r.write("sum/sum_test.go", sumTest)
			r.write("use/use_test.go", useTest)
			r.write("far/far_test.go", farTest)
			r.write("num/num.go", numCode)
			r.write("slow/num/num.go", numCode)
			r.write("sum/pick.go", numUser)
			r.write("sum/kind.go", kind)
			r.write("lone/old.go", "package lone\n")
			r.write(configPath, "{"+defaultLimits+"}")
			before := r.commit("start")
			tt.change(r)

			got := selected(t, r, before)
			touched := make([]string, 0, len(got.touched))
			for _, one := range got.touched {
				touched = append(touched, one.String())
			}
			assert.ElementsMatch(t, tt.touched, touched)
			assert.ElementsMatch(t, tt.uncovered, got.uncovered)
		})
	}
}

func selected(t *testing.T, r testRepo, before string) selection {
	t.Helper()
	var got selection
	o := options{cache: t.TempDir(), rounds: 6, limit: time.Hour, progress: io.Discard}
	require.NoError(t, withBuilder(t.Context(), r.repo, o, func(b *builder) error {
		after, _, err := r.freeze(t.Context(), b.scratch)
		require.NoError(t, err)
		got, err = selectBenchmarks(t.Context(), b, before, after)
		return err
	}))
	return got
}
