// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const driftLimits = "{" + defaultLimits + "}"

func driftRepo(t *testing.T) testRepo {
	t.Helper()
	r := newTestRepo(t)
	r.write("list/list_test.go", strings.ReplaceAll(strings.Replace(otherBench, "package other", "package list", 1), "Other", "List"))
	r.write("other/other_test.go", otherBench)
	r.write(configPath, driftLimits)
	return r
}

func (r testRepo) edit(file string) string {
	r.t.Helper()
	dir, _, _ := strings.Cut(file, "/")
	r.write(file, "package "+dir+"\n")
	return r.commit("edit " + file)
}

func timesAt(at map[string]time.Duration) commitRun {
	return func(_ context.Context, commit string, one bench, _ string) (samples, error) {
		took, ok := at[commit+" "+one.dir]
		if !ok {
			took = time.Millisecond
		}
		return readSamples(fmt.Appendf(nil, "%s 1 %d ns/op\n", one.name, took.Nanoseconds()), 1)
	}
}

func driftFrom(ctx context.Context, t *testing.T, r testRepo, skippedBefore []bench, run commitRun) driftReport {
	t.Helper()
	var report driftReport
	o := options{cache: t.TempDir(), rounds: 10, progress: io.Discard}
	require.NoError(t, withBuilder(ctx, r.repo, o, func(b *builder) (err error) {
		report, err = drift(ctx, b, skippedBefore, run)
		return err
	}))
	return report
}

func driftOf(t *testing.T, r testRepo, run commitRun) driftReport {
	t.Helper()
	return driftFrom(t.Context(), t, r, nil, run)
}

func TestDriftReportsALossThatNoStepShows(t *testing.T) {
	r := driftRepo(t)
	start := r.commit("add the benchmarks (#1)")
	r.write("list/list.go", "package list\n")
	first := r.commit("make list slower (#2)")
	r.write("other/other.go", "package other\n")
	r.commit("change other (#3)")
	r.write("list/more.go", "package list\n")
	second := r.commit("make list slower again (#4)")
	times := map[string]time.Duration{
		start + " list":  100 * time.Millisecond,
		first + " list":  108 * time.Millisecond,
		second + " list": 116 * time.Millisecond,
	}
	for _, step := range [][2]string{{start, first}, {first, second}} {
		before := slices.Repeat([]float64{float64(times[step[0]+" list"])}, 10)
		after := slices.Repeat([]float64{float64(times[step[1]+" list"])}, 10)
		assert.Equal(t, map[string]label{"BenchmarkList sec/op": ok}, judged(t, output("List", "ns/op", before...), output("List", "ns/op", after...)))
	}

	r.do("checkout", "-q", first)
	assert.Empty(t, driftOf(t, r, timesAt(times)).issue)
	r.do("checkout", "-q", "main")
	report := driftOf(t, r, timesAt(times))
	assert.Contains(t, report.issue, "| LOOK | list.BenchmarkList | sec/op | 100.0ms | 116.0ms | +16.00ms (+16.0%) |")
	assert.Contains(t, report.issue, "that touch the code of list.BenchmarkList: #4, #2\n")
	assert.NotContains(t, report.issue, "BenchmarkOther")
	assert.Contains(t, report.summary, "| OK | other.BenchmarkOther |")
	assert.False(t, report.broken)
}

func TestDriftProposesANewerStartForAFasterBenchmark(t *testing.T) {
	r := driftRepo(t)
	start := r.commit("add the benchmarks")
	last := r.edit("list/list.go")
	report := driftOf(t, r, timesAt(map[string]time.Duration{start + " list": time.Second}))
	assert.Contains(t, report.issue, fmt.Sprintf("%q: %q,\n", "list.BenchmarkList", last))
	assert.NotContains(t, report.issue, "other.BenchmarkOther")
}

func TestDriftListsSkippedBenchmarksAndRunsThemFirstNextTime(t *testing.T) {
	r := driftRepo(t)
	r.commit("add the benchmarks")
	r.edit("list/list.go")
	list, other := bench{"list", "BenchmarkList"}, bench{"other", "BenchmarkOther"}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	steady, calls := timesAt(nil), 0
	report := driftFrom(ctx, t, r, nil, func(ctx context.Context, commit string, one bench, pattern string) (samples, error) {
		if calls++; calls == 25 {
			cancel()
		}
		return steady(ctx, commit, one, pattern)
	})
	assert.Equal(t, []bench{other}, report.skipped)
	assert.Contains(t, report.summary, "They go first in the next run:\n\n- other.BenchmarkOther\n")
	assert.Contains(t, report.summary, "| CAN'T TELL | other.BenchmarkOther |")

	dir := t.TempDir()
	require.NoError(t, report.write(dir))
	before, err := readSkipped(filepath.Join(dir, "skipped.txt"))
	require.NoError(t, err)
	var order []bench
	report = driftFrom(t.Context(), t, r, before, func(ctx context.Context, commit string, one bench, pattern string) (samples, error) {
		order = append(order, one)
		return steady(ctx, commit, one, pattern)
	})
	assert.Empty(t, report.skipped)
	assert.Equal(t, other, order[0])
	assert.Contains(t, order, list)
}

func TestDriftProposesNoNewerStartThatWouldHideALoss(t *testing.T) {
	r := driftRepo(t)
	start := r.commit("add the benchmarks")
	last := r.edit("list/list.go")
	part := func(after, noise time.Duration) commitRun {
		n := 0
		return func(_ context.Context, commit string, one bench, _ string) (samples, error) {
			n++
			gain, took := time.Second, 2*time.Second+time.Duration(n/4%2)*noise
			if commit != start {
				gain, took = time.Millisecond, took+after
			}
			return readSamples(fmt.Appendf(nil, "%s/gain 1 %d ns/op\n%[1]s/other 1 %[3]d ns/op\n", one.name, gain.Nanoseconds(), took.Nanoseconds()), 1)
		}
	}
	proposal := fmt.Sprintf("%q: %q,\n", "list.BenchmarkList", last)
	small := driftOf(t, r, part(40*time.Millisecond, 0))
	assert.Contains(t, small.issue, proposal)
	assert.Contains(t, small.issue, "| OK | list.BenchmarkList/other | sec/op | 2.000s | 2.040s |")
	assert.NotContains(t, driftOf(t, r, part(time.Second, 0)).issue, proposal)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	noisy, calls := part(0, time.Second), 0
	assert.NotContains(t, driftFrom(ctx, t, r, nil, func(ctx context.Context, commit string, one bench, pattern string) (samples, error) {
		if calls++; calls == 100 {
			cancel()
		}
		return noisy(ctx, commit, one, pattern)
	}).issue, proposal)
}

func TestDriftFailsWhenABenchmarkFailsOnlyNow(t *testing.T) {
	r := driftRepo(t)
	start := r.commit("add the benchmarks")
	r.edit("list/list.go")
	once := 0
	for _, tt := range []struct {
		name   string
		broken bool
		fails  func(commit string) bool
		out    string
	}{
		{"no result at all", true, func(string) bool { return true }, ""},
		{"a part that fails on both sides", false, func(string) bool { return true }, "BenchmarkList/fine 1 5 ns/op\n"},
		{"a part that fails only now", true, func(commit string) bool { return commit != start }, "BenchmarkList/fine 1 5 ns/op\n"},
		{"one failure that does not come back", false, func(commit string) bool {
			if commit != start {
				once++
			}
			return commit != start && once == 3
		}, "BenchmarkList/fine 1 5 ns/op\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := driftOf(t, r, func(ctx context.Context, commit string, one bench, pattern string) (samples, error) {
				if one.dir != "list" || !tt.fails(commit) {
					return timesAt(nil)(ctx, commit, one, pattern)
				}
				got, err := readSamples([]byte(tt.out), 1)
				return got, errors.Join(err, errors.New("no such function"))
			})
			assert.Equal(t, tt.broken, report.broken)
			assert.Contains(t, report.summary, "no such function")
			assert.Equal(t, tt.broken, report.write(t.TempDir()) != nil)
		})
	}
}

func TestDriftWritesAnEmptyIssueWhenNothingIsLeftToReport(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, driftReport{issue: "a loss"}.write(dir))
	require.NoError(t, driftReport{}.write(dir))
	issue, err := os.ReadFile(filepath.Join(dir, "issue.md"))
	require.NoError(t, err)
	assert.Empty(t, issue)
}
