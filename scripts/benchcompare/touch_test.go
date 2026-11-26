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
	sumCode   = "package sum\n\nconst Step = 1\n\nfunc Setup() int { return 8 }\n\nfunc Add(n int) int {\n\tn++\n\tif n < 0 {\n\t\tn = -n\n\t}\n\treturn n + Step\n}\n\nfunc Other(n int) int { return n }\n"
	sumTest   = "package sum\n\nimport \"testing\"\n\nfunc BenchmarkAdd(b *testing.B) {\n\tn := Setup()\n\tfor b.Loop() {\n\t\tAdd(n)\n\t}\n}\n\nfunc BenchmarkOther(b *testing.B) {\n\tfor b.Loop() {\n\t\tOther(1)\n\t}\n}\n"
	useTest   = "package use\n\nimport (\n\t\"testing\"\n\n\t\"example.test/app/sum\"\n)\n\nfunc BenchmarkUse(b *testing.B) {\n\tfor b.Loop() {\n\t\tsum.Add(1)\n\t}\n}\n"
	numCode   = "package num\n\nfunc One() int { return 1 }\n"
	numUser   = "package sum\n\nimport \"example.test/app/num\"\n\nfunc Pick() int { return num.One() }\n"
	kind      = "package sum\n\ntype Kind int\n\nfunc (Kind) String() string { return \"kind\" }\n\nfunc Gone() {}\n"
	embCode   = "package emb\n\nimport \"embed\"\n\n//go:embed data/*.txt\nvar data embed.FS\n\nfunc Count() int {\n\tfound, _ := data.ReadDir(\"data\")\n\treturn len(found)\n}\n"
	embTest   = "package emb\n\nimport \"testing\"\n\nfunc BenchmarkCount(b *testing.B) {\n\tfor b.Loop() {\n\t\tCount()\n\t}\n}\n"
	sizeCode  = "package size\n\nconst N = 8\n"
	sizeTest  = "package alloc\n\nimport (\n\t\"testing\"\n\n\t\"example.test/app/size\"\n)\n\nfunc BenchmarkAlloc(b *testing.B) {\n\tfor b.Loop() {\n\t\t_ = make([]byte, size.N)\n\t}\n}\n"
	countCode = "package count\n\nfunc Prep() int { return 1 }\n\nfunc Step(n int) int { return n + 1 }\n"
	countTest = "package count\n\nimport \"testing\"\n\nfunc BenchmarkCount(b *testing.B) {\n\tn := Prep()\n\tfor i := 0; i < b.N; i++ {\n\t\tn = Step(n)\n\t}\n}\n"
	farTest   = "package far\n\nimport \"testing\"\n\nfunc BenchmarkFar(b *testing.B) {\n\tfor b.Loop() {\n\t}\n}\n"
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
		{"a branch that no benchmark takes", replace("n = -n", "n = -n * 2"), nil, []string{"sum/sum.go: Add (10)"}},
		{"code that runs only before the loop", replace("return 8", "return 9"), nil, []string{"sum/sum.go: Setup (5)"}},
		{"a deleted line", replace("\tif n < 0 {\n\t\tn = -n\n\t}\n", ""), []string{"sum.BenchmarkAdd", "use.BenchmarkUse"}, nil},
		{"a deleted line beside a changed branch", func(r testRepo) {
			r.write("sum/sum.go", strings.NewReplacer("\tn++\n", "", "n = -n", "n = -n * 2").Replace(sumCode))
		}, []string{"sum.BenchmarkAdd", "use.BenchmarkUse"}, []string{"sum/sum.go: Add (9)"}},
		{"a deleted embedded file", func(r testRepo) { r.do("rm", "-q", "emb/data/b.txt") }, []string{"emb.BenchmarkCount"}, nil},
		{"a constant in a package without functions", func(r testRepo) { r.write("size/size.go", "package size\n\nconst N = 9\n") }, []string{"alloc.BenchmarkAlloc"}, nil},
		{"setup of a benchmark that counts to b.N", func(r testRepo) {
			r.write("count/count.go", strings.Replace(countCode, "return 1", "return 2", 1))
		}, nil, []string{"count/count.go: Prep (3)"}},
		{"a constant", replace("Step = 1", "Step = 2"), []string{"sum.BenchmarkAdd", "sum.BenchmarkOther", "use.BenchmarkUse"}, nil},
		{"a comment", replace("func Add", "// Add adds.\nfunc Add"), nil, nil},
		{"a comment on its own line in a function", replace("\treturn n + Step", "\n\t// Step up.\n\treturn n + Step"), nil, nil},
		{"a call over more lines", replace("func Other(n int) int { return n }", "func Other(\n\tn int,\n) int {\n\treturn n\n}"), nil, nil},
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
		}, []string{"alloc.BenchmarkAlloc", "count.BenchmarkCount", "emb.BenchmarkCount", "far.BenchmarkFar", "sum.BenchmarkAdd", "sum.BenchmarkOther", "use.BenchmarkUse"}, []string{"lone/lone.go: Lone (3)"}},
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
			r.write("size/size.go", sizeCode)
			r.write("alloc/alloc_test.go", sizeTest)
			r.write("count/count.go", countCode)
			r.write("count/count_test.go", countTest)
			r.write("emb/emb.go", embCode)
			r.write("emb/emb_test.go", embTest)
			r.write("emb/data/a.txt", "a\n")
			r.write("emb/data/b.txt", "b\n")
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
