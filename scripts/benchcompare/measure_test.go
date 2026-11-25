// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRun struct {
	took  time.Duration
	value func(s side, n int) string
	err   error
	calls int
}

type fakeRunner map[bench]*fakeRun

func (f fakeRunner) run(ctx context.Context, s side, b bench, _ string) (samples, error) {
	fake := f[b]
	fake.calls++
	select {
	case <-ctx.Done():
		return samples{}, ctx.Err()
	case <-time.After(fake.took):
	}
	if fake.value == nil {
		return samples{}, fake.err
	}
	got, _ := readSamples([]byte(fake.value(s, fake.calls)), 1)
	return got, fake.err
}

func (f fakeRunner) meter() meter {
	return meter{config: tableLimits(), want: 10, run: f.run, progress: io.Discard}
}

func steady(name string, before, after time.Duration) func(side, int) string {
	return func(s side, _ int) string {
		took := before
		if s == head {
			took = after
		}
		return fmt.Sprintf("Benchmark%s 1 %d ns/op\n", name, took.Nanoseconds())
	}
}

func labelsByName(results []*result) map[string]label {
	got := map[string]label{}
	for _, r := range results {
		for _, row := range tableLimits().judge(r) {
			got[row.name] = row.label
		}
	}
	return got
}

func TestRunStopsAtItsTimeLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		quick, slow, late := bench{"a", "BenchmarkQuick"}, bench{"a", "BenchmarkSlow"}, bench{"a", "BenchmarkLate"}
		runner := fakeRunner{
			quick: {took: time.Second, value: steady("Quick", time.Millisecond, 2*time.Millisecond)},
			slow:  {took: time.Minute, value: steady("Slow", time.Minute, time.Minute)},
			late:  {took: time.Second, value: steady("Late", time.Millisecond, time.Millisecond)},
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		started := time.Now()
		results := runner.meter().measure(ctx, []bench{quick, slow, late})
		assert.Equal(t, 15*time.Minute, time.Since(started))
		assert.Equal(t, map[string]label{"BenchmarkQuick": look, "BenchmarkSlow": cantTell, "BenchmarkLate": cantTell}, labelsByName(results))
		assert.Equal(t, []bench{slow, late}, skipped(results))
	})
}

func TestLongOperationGetsMoreRunsUntilTheyAgree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		long := bench{"a", "BenchmarkLong"}
		runner := fakeRunner{long: {took: time.Second, value: func(_ side, n int) string {
			wobble := 60 * time.Millisecond
			if n > 20 {
				wobble = time.Millisecond
			}
			return fmt.Sprintf("BenchmarkLong 1 %d ns/op\n", (2*time.Second + time.Duration((n-1)/2%2)*wobble).Nanoseconds())
		}}}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		results := runner.meter().measure(ctx, []bench{long})
		require.NoError(t, ctx.Err())
		assert.Greater(t, runner[long].calls, 20)
		assert.Equal(t, map[string]label{"BenchmarkLong": ok}, labelsByName(results))
	})
}

func TestLongOperationThatNeverAgreesUsesTheWholeTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		long, quick := bench{"a", "BenchmarkLong"}, bench{"a", "BenchmarkQuick"}
		runner := fakeRunner{
			long: {took: time.Second, value: func(_ side, n int) string {
				return fmt.Sprintf("BenchmarkLong 1 %d ns/op\n", (2*time.Second + time.Duration((n-1)/2%2)*100*time.Millisecond).Nanoseconds())
			}},
			quick: {took: time.Second, value: steady("Quick", time.Millisecond, time.Millisecond)},
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		results := runner.meter().measure(ctx, []bench{long, quick})
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
		assert.Equal(t, map[string]label{"BenchmarkLong": cantTell, "BenchmarkQuick": ok}, labelsByName(results))
		assert.Empty(t, skipped(results))
	})
}

func TestBrokenBenchmarkCannotTell(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broken, fine := bench{"a", "BenchmarkBroken"}, bench{"a", "BenchmarkFine"}
		runner := fakeRunner{
			broken: {took: time.Second, err: errors.New("exit status 1")},
			fine:   {took: time.Second, value: steady("Fine", time.Millisecond, time.Millisecond)},
		}
		results := runner.meter().measure(t.Context(), []bench{broken, fine})
		assert.Equal(t, map[string]label{"BenchmarkBroken": cantTell, "BenchmarkFine": ok}, labelsByName(results))
		assert.Equal(t, 1, runner[broken].calls)
		assert.Empty(t, skipped(results))
	})
}

func TestRunThatFailsNowAndThenSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		flaky := bench{"a", "BenchmarkFlaky"}
		runner := fakeRunner{flaky: {took: time.Second, err: errors.New("exit status 1"), value: func(s side, n int) string {
			if s == head && n == 2 {
				return "BenchmarkFlaky/other 1 5 ns/op\n"
			}
			return "BenchmarkFlaky/other 1 5 ns/op\nBenchmarkFlaky/slow 1 5 ns/op\n"
		}}}
		results := runner.meter().measure(t.Context(), []bench{flaky})
		assert.Equal(t, map[string]label{"BenchmarkFlaky/other": ok, "BenchmarkFlaky/slow": cantTell}, labelsByName(results))
		for _, row := range tableLimits().judge(results[0]) {
			if row.name == "BenchmarkFlaky/slow" {
				assert.Contains(t, row.reason, "it fails on")
			}
		}
	})
}

func TestPartOfABenchmarkThatAlwaysFailsIsNotLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		partly := bench{"a", "BenchmarkPartly"}
		runner := fakeRunner{partly: {took: time.Second, err: errors.New("no rows"), value: steady("Partly/fine", time.Millisecond, time.Millisecond)}}
		results := runner.meter().measure(t.Context(), []bench{partly})
		assert.Equal(t, map[string]label{"BenchmarkPartly/fine": ok, "BenchmarkPartly": cantTell}, labelsByName(results))
	})
}
