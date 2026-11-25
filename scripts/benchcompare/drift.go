// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

const listedChanges = 500

type commitRun func(ctx context.Context, commit string, one bench, pattern string) (samples, error)

type drifting struct {
	builder *builder
	head    string
	now     snapshot
	starts  map[bench]string
	results []*result
	deps    map[string]map[string]bool
}

type driftReport struct {
	summary string
	issue   string
	skipped []bench
	broken  bool
}

func drift(ctx context.Context, b *builder, skippedBefore []bench, run commitRun) (driftReport, error) {
	setup := context.WithoutCancel(ctx)
	d := drifting{builder: b, starts: map[bench]string{}}
	var err error
	if d.head, err = b.repo.gitLine(setup, "rev-parse", "HEAD"); err != nil {
		return driftReport{}, err
	}
	if d.now, err = takeSnapshot(setup, commitTree{b.repo, d.head}, nil); err != nil {
		return driftReport{}, err
	}
	order, err := d.findStarts(setup, skippedBefore)
	if err != nil {
		return driftReport{}, err
	}
	m := meter{config: d.now.config, want: b.rounds, progress: b.progress, run: func(ctx context.Context, s side, one bench, pattern string) (samples, error) {
		commit := d.starts[one]
		if s == head {
			commit = d.head
		}
		return run(ctx, commit, one, pattern)
	}}
	d.results = append(d.results, m.measure(ctx, order)...)
	return d.report(setup), nil
}

func (d *drifting) findStarts(ctx context.Context, skippedBefore []bench) ([]bench, error) {
	var order []bench
	for _, one := range skippedFirst(d.now.benches, skippedBefore) {
		found, err := startOf(ctx, d.builder.repo, d.head, d.now, one)
		if err != nil {
			return nil, err
		}
		d.starts[one] = found.commit
		switch {
		case found.problem != "":
			d.results = append(d.results, &result{bench: one, problem: found.problem})
		case found.commit != d.head:
			order = append(order, one)
		}
	}
	return order, nil
}

func skippedFirst(benches, skippedBefore []bench) []bench {
	var first, rest []bench
	for _, one := range skippedBefore {
		if slices.Contains(benches, one) {
			first = append(first, one)
		}
	}
	for _, one := range benches {
		if !slices.Contains(first, one) {
			rest = append(rest, one)
		}
	}
	return append(first, rest...)
}

func (d *drifting) report(ctx context.Context) driftReport {
	rows := d.now.config.judgeAll(d.results)
	report := driftReport{skipped: skipped(d.results), broken: slices.ContainsFunc(d.results, (*result).broken)}
	left := ""
	if len(report.skipped) > 0 {
		left = fmt.Sprintf("\nThe time budget of %s ended before these benchmarks. They go first in the next run:\n\n", d.builder.limit) + bullets(report.skipped)
	}
	report.summary = fmt.Sprintf("%s. Each benchmark at %s against its starting commit, at least %d runs of %s on each side, in turns on one machine (%s).\n\n%s%s", tally(rows), short(d.head), d.builder.rounds, d.builder.benchtime, machine(d.results), table(rows), left)
	lost, won := losses(rows), gains(rows)
	if len(lost)+len(won) == 0 {
		return report
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The weekly check compared each Go benchmark at %s with its starting commit in `%s`, with both versions on one machine (%s).\n", short(d.head), configPath, machine(d.results))
	if len(lost) > 0 {
		b.WriteString("\n### Slower or larger in total\n\n" + table(lost))
		for _, one := range benchesOf(lost) {
			fmt.Fprintf(&b, "\nPull requests since %s that touch the code of %s: %s\n", short(d.starts[one]), cell(one.String()), d.changesSince(ctx, one))
		}
	}
	if len(won) > 0 {
		b.WriteString("\n### Faster or smaller in total\n\n" + table(won))
		fmt.Fprintf(&b, "\nTo keep the gain, propose a newer starting commit. It also drops each loss in this table. Set these lines in `starts` in `%s`:\n\n```json\n", configPath)
		for _, one := range benchesOf(won) {
			fmt.Fprintf(&b, "%q: %q,\n", one, d.head)
		}
		b.WriteString("```\n")
	}
	report.issue = b.String() + left
	return report
}

func (r *result) broken() bool {
	unmeasured := r.rounds == r.want && (len(r.before.names) == 0 || len(r.after.names) == 0)
	return r.problem != "" || unmeasured || 2*r.failed[head] > r.rounds && 2*r.failed[base] <= r.rounds
}

func losses(rows []row) []row {
	return slices.DeleteFunc(slices.Clone(rows), func(r row) bool { return r.label != look })
}

func gains(rows []row) []row {
	fast, unfit := map[bench]bool{}, map[bench]bool{}
	for _, r := range rows {
		fast[r.bench] = fast[r.bench] || r.state == faster
		unfit[r.bench] = unfit[r.bench] || r.label != ok
	}
	return slices.DeleteFunc(slices.Clone(rows), func(r row) bool { return !fast[r.bench] || unfit[r.bench] })
}

func benchesOf(rows []row) []bench {
	var benches []bench
	for _, r := range rows {
		if !slices.Contains(benches, r.bench) {
			benches = append(benches, r.bench)
		}
	}
	return benches
}

var pullNumber = regexp.MustCompile(`\(#(\d+)\)$`)

func (d *drifting) changesSince(ctx context.Context, one bench) string {
	found, err := d.listChanges(ctx, one)
	switch more := len(found) - listedChanges; {
	case err != nil:
		return fmt.Sprintf("the list failed: %v", err)
	case len(found) == 0:
		return "none, so the cause is outside this repository or is noise."
	case more > 0:
		return fmt.Sprintf("%s, and %d older in `git log %s..%s`", strings.Join(found[:listedChanges], ", "), more, short(d.starts[one]), short(d.head))
	}
	return strings.Join(found, ", ")
}

func (d *drifting) listChanges(ctx context.Context, one bench) ([]string, error) {
	paths, err := d.pathsOf(ctx, one)
	if err != nil {
		return nil, err
	}
	args := append([]string{"log", "--first-parent", "--format=%h %s", d.starts[one] + ".." + d.head, "--"}, paths...)
	out, err := d.builder.repo.git(ctx, "", args...)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, line := range split(out, "\n") {
		hash, subject, _ := strings.Cut(line, " ")
		if pull := pullNumber.FindStringSubmatch(subject); pull != nil {
			hash = "#" + pull[1]
		}
		found = append(found, hash)
	}
	return found, nil
}

func (d *drifting) pathsOf(ctx context.Context, one bench) ([]string, error) {
	module := d.now.workloads.module
	if d.deps == nil {
		found, err := d.builder.list(ctx, d.head, d.now.benches)
		if err != nil {
			return nil, err
		}
		d.deps = found.testDeps(module)
	}
	paths := map[string]bool{"go.mod": true, "go.sum": true, ":(glob)" + path.Join(one.dir, "*"): true}
	for dep := range d.deps[one.dir] {
		if dir, inside := strings.CutPrefix(dep, module+"/"); inside {
			paths[dir] = true
		}
	}
	return slices.Sorted(maps.Keys(paths)), nil
}
