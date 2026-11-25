// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBenchmarkCarriesItsOwnLimit(t *testing.T) {
	c, err := parseConfig([]byte(`{
		"limits": {"sec/op": {"percent": 10, "min": 0.0001, "any": 0.05}},
		"benchmarks": {
			"pkg/a.BenchmarkList": {"sec/op": {"percent": 5, "min": 0.00005, "any": 0.05}},
			"pkg/a.BenchmarkList/tag": {"sec/op": {"percent": 2, "min": 0.00001, "any": 0.05}}
		}
	}`))
	require.NoError(t, err)
	a := bench{"pkg/a", "BenchmarkList"}
	tests := []struct {
		bench bench
		name  string
		want  float64
	}{
		{a, "BenchmarkList/tag/few", 2},
		{a, "BenchmarkList/tagged", 5},
		{a, "BenchmarkList", 5},
		{bench{"pkg/b", "BenchmarkList"}, "BenchmarkList", 10},
	}
	for _, tt := range tests {
		assert.InDelta(t, tt.want, c.limitFor(tt.bench, tt.name, "sec/op").Percent, 0, tt.bench.dir+"."+tt.name)
	}
}

func TestConfigRejects(t *testing.T) {
	const time = `"sec/op": {"percent": 10, "min": 0.0001, "any": 0.05}`
	for name, raw := range map[string]string{
		"an unknown key":                    `{"limits": {` + time + `}, "start": {}}`,
		"a limit without an upper bound":    `{"limits": {"sec/op": {"percent": 10, "min": 0.0001}}}`,
		"no limits":                         `{}`,
		"a benchmark limit without default": `{"limits": {` + time + `}, "benchmarks": {"a.BenchmarkB": {"B/op": {"percent": 1, "min": 1, "any": 1}}}}`,
		"a start that is not a full commit": `{"limits": {` + time + `}, "starts": {"a.BenchmarkB": "develop"}}`,
		"a start without a package folder":  `{"limits": {` + time + `}, "starts": {"BenchmarkB": "0123456789012345678901234567890123456789"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig([]byte(raw))
			assert.Error(t, err)
		})
	}
}
