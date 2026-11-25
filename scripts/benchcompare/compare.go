// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func compare(ctx context.Context, b *builder, baseRef string) (string, error) {
	setup := context.WithoutCancel(ctx)
	baseCommit, err := b.repo.gitLine(setup, "merge-base", baseRef, "HEAD")
	if err != nil {
		return "", err
	}
	headCommit, edited, err := b.repo.freeze(setup, b.scratch)
	if err != nil {
		return "", err
	}
	picked, err := selectBenchmarks(ctx, b, baseCommit, headCommit)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && !picked.stopped {
		return fmt.Sprintf("%s: the time limit of %s ended before the command knew which benchmarks the change touches.\n", cantTell, b.limit), nil
	}
	if err != nil {
		return "", err
	}
	if picked.noCode {
		return fmt.Sprintf("No Go code changed since %s, so there is nothing to compare.\n", short(baseCommit)), nil
	}
	m := meter{config: picked.now.config, want: b.rounds, progress: b.progress, run: func(ctx context.Context, s side, one bench, pattern string) (samples, error) {
		at := baseCommit
		if s == head {
			at = headCommit
		}
		return b.measure(ctx, at, one, pattern)
	}}
	results := m.measure(ctx, picked.touched)
	for _, one := range picked.now.benches {
		if why := picked.changed[one]; why != "" {
			results = append(results, &result{bench: one, problem: why})
		}
	}
	branch := short(headCommit)
	if edited {
		if branch, err = b.repo.gitLine(setup, "rev-parse", "--short=9", "HEAD"); err != nil {
			return "", err
		}
		branch += " with uncommitted changes"
	}
	return picked.report(branch, short(baseCommit), results, b.options), nil
}

func (s selection) report(branch, start string, results []*result, o options) string {
	var b strings.Builder
	rows := s.now.config.judgeAll(results)
	if len(rows) == 0 {
		b.WriteString("No benchmark runs the changed code.\n")
	} else {
		fmt.Fprintf(&b, "%s. The branch at %s against its merge base %s, at least %d runs of %s on each side, in turns on one machine (%s).\n\n", tally(rows), branch, start, o.rounds, o.benchtime, machine(results))
		b.WriteString(table(rows))
	}
	if s.otherGo {
		b.WriteString("\nThe version of Go changed, so every benchmark counts as touched.\n")
	}
	if s.stopped {
		fmt.Fprintf(&b, "\nThe time limit of %s ended before the command knew which changed code has no benchmark.\n", o.limit)
	}
	if len(s.uncovered) > 0 {
		b.WriteString("\nChanged code that no benchmark covers:\n\n" + bullets(s.uncovered))
	}
	return b.String()
}
