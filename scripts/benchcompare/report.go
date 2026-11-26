// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"cmp"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/perf/benchunit"
)

var labelOrder = []label{look, cantTell, ok}

func (c config) judgeAll(results []*result) []row {
	rows := make([]row, 0, len(results))
	for _, r := range results {
		rows = append(rows, c.judge(r)...)
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		return cmp.Compare(slices.Index(labelOrder, a.label), slices.Index(labelOrder, b.label))
	})
	return rows
}

func tally(rows []row) string {
	counts := map[label]int{}
	for _, r := range rows {
		counts[r.label]++
	}
	parts := make([]string, 0, len(labelOrder))
	for _, l := range labelOrder {
		parts = append(parts, fmt.Sprintf("%d %s", counts[l], l))
	}
	return strings.Join(parts, ", ")
}

func table(rows []row) string {
	var b strings.Builder
	b.WriteString("| Label | Benchmark | Metric | Before | After | Difference | Note |\n|---|---|---|--:|--:|--:|---|\n")
	for _, r := range rows {
		before, after, difference := "", "", ""
		if r.state != unmeasured {
			before, after = amount(r.before, r.unit), amount(r.after, r.unit)
			difference = r.loss()
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", r.label, cell(r.bench.dir+"."+r.name), r.unit, before, after, difference, cell(joined(r.reason, r.stats)))
	}
	return b.String()
}

func (r row) loss() string {
	delta := r.after - r.before
	sign := "+"
	if delta < 0 {
		sign = "-"
	}
	text := sign + amount(math.Abs(delta), r.unit)
	if r.before == 0 {
		return text
	}
	return fmt.Sprintf("%s (%+.1f%%)", text, 100*delta/r.before)
}

func amount(value float64, unit string) string {
	if value == 0 {
		return "0"
	}
	text := benchunit.Scale(value, benchunit.ClassOf(unit))
	if strings.HasPrefix(unit, "sec") || strings.HasSuffix(unit, "-sec/op") {
		return text + "s"
	}
	return text + "B"
}

func cell(text string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ", "\r", "").Replace(text)
}

func joined(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "; ")
}

func machine(results []*result) string {
	cpu := ""
	for _, r := range results {
		cpu = cmp.Or(cpu, r.after.cpu, r.before.cpu)
	}
	return joined(cpu, runtime.GOOS+"/"+runtime.GOARCH, runtime.Version())
}

func bullets[T any](lines []T) string {
	var b strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&b, "- %v\n", line)
	}
	return b.String()
}
