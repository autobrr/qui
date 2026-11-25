// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"cmp"
	"fmt"
	"slices"

	"golang.org/x/perf/benchmath"
)

type label string

const (
	ok       label = "OK"
	look     label = "LOOK"
	cantTell label = "CAN'T TELL"
)

const confidence = 0.95

type row struct {
	bench         bench
	name, unit    string
	label         label
	before, after float64
	state         state
	stats         string
	reason        string
}

type state int

const (
	unmeasured state = iota
	measured
	faster
	noisy
)

type result struct {
	bench         bench
	want          int
	rounds        int
	extra         int
	before, after samples
	problem       string
	failure       string
	failed        [2]int
}

func (c config) judge(r *result) []row {
	if r.problem != "" {
		return []row{{bench: r.bench, name: r.bench.name, label: cantTell, reason: r.problem}}
	}
	names := slices.Clone(r.before.names)
	for _, name := range r.after.names {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		reason := "the benchmark printed no result"
		if r.rounds < r.want {
			reason = unfinished(0, r.want)
		}
		return []row{{bench: r.bench, name: r.bench.name, label: cantTell, reason: reason}}
	}
	var rows []row
	for _, name := range names {
		for _, unit := range sortedKeys(c.Limits) {
			before, after := r.before.values(name, unit), r.after.values(name, unit)
			if len(before)+len(after) == 0 {
				continue
			}
			judged := c.limitFor(r.bench, name, unit).judge(before, after, r.want)
			judged.bench, judged.name, judged.unit = r.bench, name, unit
			if len(before) < r.rounds || len(after) < r.rounds {
				judged.reason = cmp.Or(r.failure, judged.reason)
			}
			rows = append(rows, judged)
		}
	}
	if r.failure != "" && !slices.ContainsFunc(rows, func(one row) bool { return one.reason == r.failure }) {
		rows = append(rows, row{bench: r.bench, name: r.bench.name, label: cantTell, reason: r.failure})
	}
	return rows
}

func (l limit) judge(before, after []float64, want int) row {
	if n := min(len(before), len(after)); n < want {
		return row{label: cantTell, reason: unfinished(n, want)}
	}
	was, now := benchmath.NewSample(before, &benchmath.DefaultThresholds), benchmath.NewSample(after, &benchmath.DefaultThresholds)
	wasMid, nowMid := benchmath.AssumeNothing.Summary(was, confidence), benchmath.AssumeNothing.Summary(now, confidence)
	test := benchmath.AssumeNothing.Compare(was, now)
	delta, differs := nowMid.Center-wasMid.Center, test.P <= test.Alpha
	r := row{label: ok, state: measured, before: wasMid.Center, after: nowMid.Center, stats: test.String()}
	blur := (wasMid.Hi - wasMid.Lo + nowMid.Hi - nowMid.Lo) / 2
	sees := max(l.Any, wasMid.Center/100)
	switch {
	case differs && l.reached(wasMid.Center, delta):
		r.label = look
	case differs && delta < 0:
		if l.reached(nowMid.Center, -delta) {
			r.state = faster
		}
	case l.long(wasMid.Center) && blur > sees:
		r.label, r.state = cantTell, noisy
		r.reason = fmt.Sprintf("the runs differ by %.1f%%, so a loss of %.1f%% can hide", 100*blur/wasMid.Center, 100*sees/wasMid.Center)
	}
	if !differs {
		r.stats = "no real difference, " + r.stats
	}
	return r
}

func (l limit) reached(before, loss float64) bool {
	return loss >= l.Any || (loss >= l.Min && loss >= before*l.Percent/100)
}

func (l limit) long(before float64) bool {
	return before*l.Percent/100 >= l.Any
}

func unfinished(n, want int) string {
	return fmt.Sprintf("stopped after %d of %d runs", n, want)
}
