// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tableLimits() config {
	return config{Limits: limits{
		"sec/op": {Percent: 10, Min: 100e-6, Any: 50e-3},
		"B/op":   {Percent: 10, Min: 1 << 20, Any: 20 << 20},
		"kept-B": {Percent: 10, Min: 1 << 20, Any: 20 << 20},
	}}
}

func recorded(t *testing.T, file string) samples {
	t.Helper()
	out, err := os.ReadFile(filepath.Join("testdata", file))
	require.NoError(t, err)
	got, err := readSamples(out, 16)
	require.NoError(t, err)
	return got
}

func output(name, unit string, values ...float64) string {
	var b strings.Builder
	for _, v := range values {
		fmt.Fprintf(&b, "Benchmark%s-16\t1\t%f %s\n", name, v, unit)
	}
	return b.String()
}

func labelsOf(rows []row) map[string]label {
	got := map[string]label{}
	for _, r := range rows {
		got[r.name+" "+r.unit] = r.label
	}
	return got
}

func judged(t *testing.T, before, after string) map[string]label {
	t.Helper()
	b, err := readSamples([]byte(before), 16)
	require.NoError(t, err)
	a, err := readSamples([]byte(after), 16)
	require.NoError(t, err)
	return labelsOf(tableLimits().judge(&result{want: 10, rounds: 10, before: b, after: a}))
}

func TestExamplesOfTheLimitTable(t *testing.T) {
	raw, err := os.ReadFile("../../" + configPath)
	require.NoError(t, err)
	c, err := parseConfig(raw)
	require.NoError(t, err)
	r := &result{want: 10, rounds: 10, before: recorded(t, "examples_before.txt"), after: recorded(t, "examples_after.txt")}
	assert.Equal(t, map[string]label{
		"BenchmarkExamples/5us sec/op":   ok,
		"BenchmarkExamples/1.7ms sec/op": ok,
		"BenchmarkExamples/1ms sec/op":   look,
		"BenchmarkExamples/1s sec/op":    look,
		"BenchmarkExamples/5s sec/op":    look,
		"BenchmarkExamples/60s sec/op":   look,
	}, labelsOf(c.judge(r)))
}

func TestNoRealDifferenceIsOK(t *testing.T) {
	noisy := []float64{1e6, 1.9e6, 1.1e6, 1.8e6, 1.05e6, 1.7e6, 1.2e6, 1.6e6, 1.3e6, 1.5e6}
	slower := []float64{1.8e6, 1.1e6, 1.9e6, 1.2e6, 2.1e6, 1.25e6, 2e6, 1.4e6, 1.95e6, 1.7e6}
	assert.Equal(t, map[string]label{"BenchmarkNoisy sec/op": ok},
		judged(t, output("Noisy", "ns/op", noisy...), output("Noisy", "ns/op", slower...)))
}

func TestLongOperationsThatAreTooNoisy(t *testing.T) {
	spread := func(mid, step float64) []float64 {
		values := make([]float64, 0, 10)
		for i := range 10 {
			values = append(values, mid+step*(float64(i)-4.5))
		}
		return values
	}
	tests := []struct {
		name          string
		before, after []float64
		want          label
	}{
		{"noise hides 50 ms of 2 s", spread(2e9, 20e6), spread(2.01e9, 20e6), cantTell},
		{"steady runs show that 2 s lost nothing", spread(2e9, 1e6), spread(2e9, 1e6), ok},
		{"1% of 60 s is enough to see", spread(60e9, 50e6), spread(60e9, 50e6), ok},
		{"noise above 1% of 60 s", spread(60e9, 200e6), spread(60e9, 200e6), cantTell},
		{"a real small loss in noise that hides a larger one", spread(2e9, 14e6), spread(2.048e9, 4e6), cantTell},
		{"a real loss shows through noise", spread(2e9, 20e6), spread(2.5e9, 20e6), look},
		{"a real gain shows through noise", spread(2.5e9, 20e6), spread(2e9, 20e6), ok},
		{"a short operation with the same noise", spread(2e6, 20e3), spread(2.01e6, 20e3), ok},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, map[string]label{"BenchmarkLong sec/op": tt.want},
				judged(t, output("Long", "ns/op", tt.before...), output("Long", "ns/op", tt.after...)))
		})
	}
}

func TestMemoryLimits(t *testing.T) {
	const mib = 1 << 20
	tests := []struct {
		name          string
		unit          string
		before, after float64
		want          label
	}{
		{"12% of 1 MiB is under 1 MiB", "B/op", mib, 1.12 * mib, ok},
		{"9% of 100 MiB is under 10% and 20 MiB", "B/op", 100 * mib, 109 * mib, ok},
		{"20% of 10 MiB kept", "kept-B", 10 * mib, 12 * mib, look},
		{"5% of 500 MiB is above 20 MiB", "B/op", 500 * mib, 525 * mib, look},
		{"1 MiB where there was nothing", "B/op", 0, mib, look},
		{"a steady allocation that shrinks", "B/op", 12 * mib, 10 * mib, ok},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steady := func(v float64) []float64 { return slices.Repeat([]float64{v}, 10) }
			assert.Equal(t, map[string]label{"BenchmarkMemory " + tt.unit: tt.want},
				judged(t, output("Memory", tt.unit, steady(tt.before)...), output("Memory", tt.unit, steady(tt.after)...)))
		})
	}
}

func TestTooFewRunsAreNeverOK(t *testing.T) {
	assert.Equal(t, map[string]label{"BenchmarkCut sec/op": cantTell},
		judged(t, output("Cut", "ns/op", slices.Repeat([]float64{100}, 9)...), output("Cut", "ns/op", slices.Repeat([]float64{100}, 10)...)))
}
