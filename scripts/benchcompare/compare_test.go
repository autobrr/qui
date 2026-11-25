// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareMeasuresTheChangeAndLeavesNoWorktree(t *testing.T) {
	r := newTestRepo(t)
	slow := strings.Replace(sumTest, "Add(n)", "Add(n)\n\t\tb.ReportMetric(Cost, \"ns/op\")", 1)
	r.write("sum/sum.go", sumCode)
	r.write("sum/cost.go", "package sum\n\nconst Cost = 1e9\n")
	r.write("sum/sum_test.go", slow)
	r.write(configPath, driftLimits)
	r.commit("start")
	r.do("switch", "-q", "-c", "change")
	r.write("sum/cost.go", "package sum\n\nconst Cost = 1.2e9\n")
	r.write("sum/lone.go", "package sum\n\nfunc Lone() int { return 1 }\n")

	o := options{cache: t.TempDir(), rounds: 6, benchtime: time.Microsecond, limit: time.Hour, progress: io.Discard}
	report := compared(t.Context(), t, r, o)
	assert.Contains(t, report, "| LOOK | sum.BenchmarkAdd | sec/op | 1.000s | 1.200s | +200.0ms (+20.0%) |")
	assert.Contains(t, report, "with uncommitted changes against its merge base")
	assert.Contains(t, report, "Changed code that no benchmark covers:\n\n- sum/lone.go: Lone (3)\n")
	assert.Len(t, strings.Split(r.do("worktree", "list"), "\n"), 1)
}

func TestCompareWithoutTimeIsNotOK(t *testing.T) {
	r := driftRepo(t)
	r.commit("start")
	r.write("list/list.go", "package list\n\nfunc New() {}\n")
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	report := compared(ctx, t, r, options{cache: t.TempDir(), rounds: 10, progress: io.Discard})
	assert.Contains(t, report, "CAN'T TELL")
}

func TestCompareNamesTheFailureOfABenchmark(t *testing.T) {
	r := newTestRepo(t)
	r.write("sum/sum.go", sumCode)
	r.write("sum/sum_test.go", strings.Replace(sumTest, "Add(n)\n", "if Add(n) > 10 {\n\t\t\tb.Fatal(\"too many\")\n\t\t}\n", 1))
	r.write(configPath, driftLimits)
	r.commit("start")
	r.do("switch", "-q", "-c", "change")
	r.write("sum/sum.go", strings.Replace(sumCode, "n + Step", "n + Step + 1", 1))

	o := options{cache: t.TempDir(), rounds: 6, benchtime: time.Microsecond, limit: time.Hour, progress: io.Discard}
	report := compared(t.Context(), t, r, o)
	assert.Contains(t, report, "| CAN'T TELL | sum.BenchmarkAdd | sec/op |  |  |  | it fails on the branch: sum_test.go:9: too many |")
}

func compared(ctx context.Context, t *testing.T, r testRepo, o options) string {
	t.Helper()
	var report string
	require.NoError(t, withBuilder(ctx, r.repo, o, func(b *builder) (err error) {
		report, err = compare(ctx, b, "main")
		return err
	}))
	return report
}

func TestCompareGivesUpOnABenchmarkThatHangs(t *testing.T) {
	r := newTestRepo(t)
	r.write("sum/sum.go", sumCode)
	r.write("sum/sum_test.go", strings.Replace(strings.Replace(sumTest, "import \"testing\"", "import (\n\t\"testing\"\n\t\"time\"\n)", 1), "Add(n)\n", "for Add(n) > 9 {\n\t\t\ttime.Sleep(time.Hour)\n\t\t}\n", 1))
	r.write(configPath, driftLimits)
	r.commit("start")
	r.do("switch", "-q", "-c", "change")
	r.write("sum/sum.go", strings.Replace(sumCode, "n + Step", "n + Step + 1", 1))
	report := compared(t.Context(), t, r, options{cache: t.TempDir(), rounds: 10, benchtime: time.Microsecond, limit: 20 * time.Second, progress: io.Discard})
	assert.Contains(t, report, "| CAN'T TELL | sum.BenchmarkAdd |")
	assert.Contains(t, report, "one run takes more than 1s")
}
