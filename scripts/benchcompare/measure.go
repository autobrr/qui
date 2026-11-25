// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type side int

const (
	base side = iota
	head
)

func (s side) String() string {
	if s == head {
		return "the branch"
	}
	return "the starting point"
}

type runFunc func(ctx context.Context, s side, b bench, pattern string) (samples, error)

type meter struct {
	config   config
	want     int
	run      runFunc
	progress io.Writer
}

func (m meter) measure(ctx context.Context, order []bench) []*result {
	results := make([]*result, 0, len(order))
	for i, b := range order {
		r := &result{bench: b, want: m.want}
		results = append(results, r)
		for r.rounds < m.want && r.problem == "" && ctx.Err() == nil {
			fmt.Fprintf(m.progress, "[%d/%d] %s, run %d of %d\n", i+1, len(order), b, r.rounds+1, m.want)
			r.round(ctx, m.run, exactly(b.name), true)
		}
	}
	for ctx.Err() == nil {
		more := 0
		for _, r := range results {
			more += m.extraRounds(ctx, r)
		}
		if more == 0 {
			break
		}
	}
	return results
}

func (m meter) extraRounds(ctx context.Context, r *result) int {
	rerun := map[string]bool{}
	for _, row := range m.config.judge(r) {
		if row.state == noisy && !rerun[row.name] {
			rerun[row.name] = true
			fmt.Fprintf(m.progress, "%s.%s needs more runs: %s\n", r.bench.dir, row.name, row.reason)
			r.round(ctx, m.run, exactly(row.name), false)
		}
	}
	return len(rerun)
}

func (r *result) round(ctx context.Context, run runFunc, pattern string, whole bool) {
	order := [2]side{base, head}
	if (r.rounds+r.extra)%2 == 1 {
		order = [2]side{head, base}
	}
	var got [2]samples
	for _, s := range order {
		out, err := run(ctx, s, r.bench, pattern)
		if ctx.Err() != nil {
			return
		}
		if err != nil && whole {
			r.failed[s]++
		}
		if err != nil {
			failure := fmt.Sprintf("it fails on %s: %v", s, err)
			if _, ran := errors.AsType[runError](err); len(out.names) == 0 && !ran {
				r.problem = failure
				return
			}
			r.failure = cmp.Or(r.failure, failure)
		}
		got[s] = out
	}
	r.before.merge(got[base])
	r.after.merge(got[head])
	if whole {
		r.rounds++
	} else {
		r.extra++
	}
}

func exactly(name string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	return strings.Join(parts, "/")
}

func skipped(results []*result) []bench {
	var left []bench
	for _, r := range results {
		if r.rounds < r.want && r.problem == "" {
			left = append(left, r.bench)
		}
	}
	return left
}
