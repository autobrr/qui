// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartingCommits(t *testing.T) {
	r, pinned := benchRepo(t)
	r.write("early/early_test.go", strings.ReplaceAll(otherBench, "Other", "Early"))
	added := r.commit("add the limits and a benchmark")
	r.write("early/early_test.go", strings.ReplaceAll(otherBench, "Other", "Early")+"\nvar unrelated = 1\n")
	r.commit("leave its workload as it is")
	changed := strings.Replace(otherBench, "b.Loop() {\n", "b.Loop() {\n\t\t_ = make([]int, 9)\n", 1)
	r.write("other/other_test.go", changed)
	moved := r.commit("change the workload of other")
	r.write("other/other_test.go", changed+"\nfunc BenchmarkNew(b *testing.B) {\n}\n")
	r.write("list/list.go", "package list\n")
	last := r.commit("add one more benchmark")

	now, err := takeSnapshot(t.Context(), commitTree{r.repo, last}, nil)
	require.NoError(t, err)
	got := map[string]string{}
	for _, b := range now.benches {
		start, err := startOf(t.Context(), r.repo, last, now, b)
		require.NoError(t, err)
		got[b.String()] = start.commit
		assert.Empty(t, start.problem)
	}
	assert.Equal(t, map[string]string{
		"list.BenchmarkList":   pinned,
		"early.BenchmarkEarly": added,
		"other.BenchmarkOther": moved,
		"other.BenchmarkNew":   last,
	}, got)
}

func TestPinnedStartWithAnotherWorkloadIsNotCompared(t *testing.T) {
	r, _ := benchRepo(t)
	r.commit("add the limits")
	r.write("list/list_test.go", strings.Replace(listBench, "size = 100", "size = 10", 1))
	last := r.commit("change the workload past the check")
	now, err := takeSnapshot(t.Context(), commitTree{r.repo, last}, nil)
	require.NoError(t, err)
	start, err := startOf(t.Context(), r.repo, last, now, bench{"list", "BenchmarkList"})
	require.NoError(t, err)
	assert.Contains(t, start.problem, "workload")
}
