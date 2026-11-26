// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
)

const approveLabel = "bench-approved"

type snapshot struct {
	config       config
	workloads    *workloads
	benches      []bench
	fingerprints map[bench]string
}

func takeSnapshot(ctx context.Context, t commitTree, now *config) (snapshot, error) {
	s := snapshot{fingerprints: map[bench]string{}}
	raw, err := t.file(ctx, configPath)
	if err != nil && (now == nil || !errors.Is(err, fs.ErrNotExist)) {
		return s, err
	}
	if err == nil {
		if s.config, err = parseConfig(raw); err != nil {
			return s, fmt.Errorf("%s: %w", configPath, err)
		}
	}
	dirs := s.config.WorkloadDirs
	if now != nil {
		dirs = now.WorkloadDirs
	}
	if s.workloads, err = newWorkloads(ctx, t, dirs); err != nil {
		return s, err
	}
	if s.benches, err = s.workloads.list(ctx); err != nil {
		return s, err
	}
	for _, b := range s.benches {
		if s.fingerprints[b], err = s.workloads.fingerprint(ctx, b); err != nil {
			return s, err
		}
	}
	return s, nil
}

func check(ctx context.Context, r repo, baseRef, headRef string, approved bool) ([]string, bool, error) {
	merge, err := r.gitLine(ctx, "merge-base", baseRef, headRef)
	if err != nil {
		return nil, false, err
	}
	now, err := takeSnapshot(ctx, commitTree{r, headRef}, nil)
	if errors.Is(err, fs.ErrNotExist) {
		if _, missing := (commitTree{r, merge}).file(ctx, configPath); errors.Is(missing, fs.ErrNotExist) {
			return []string{"This change starts before " + configPath + " exists. Merge the base branch to get the check."}, true, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	was, err := takeSnapshot(ctx, commitTree{r, merge}, &now.config)
	if err != nil {
		return nil, false, err
	}
	broken, err := startProblems(ctx, r, baseRef, now)
	if err != nil {
		return nil, false, err
	}
	if broken = append(broken, strayLimits(now)...); len(broken) > 0 {
		return broken, false, nil
	}
	asks := slices.Concat(benchChanges(was, now), startChanges(was, now), limitChanges(was.config, now.config))
	if len(asks) == 0 {
		return []string{"No starting commit moves and no limit gets looser. This change needs no approval."}, true, nil
	}
	if approved {
		return append(asks, fmt.Sprintf("A maintainer approved this with the label %q.", approveLabel)), true, nil
	}
	return append(asks, fmt.Sprintf("Take these changes back, or ask a maintainer to set the label %q on the pull request. Only a maintainer sets that label.", approveLabel)), false, nil
}

func startProblems(ctx context.Context, r repo, baseRef string, now snapshot) ([]string, error) {
	var problems []string
	for _, key := range sortedKeys(now.config.Starts) {
		b, _ := parseBench(key)
		start := now.config.Starts[key]
		if !slices.Contains(now.benches, b) {
			problems = append(problems, fmt.Sprintf("%s: no such benchmark. Delete its entry from starts in %s.", b, configPath))
			continue
		}
		if _, err := r.gitLine(ctx, "merge-base", "--is-ancestor", start, baseRef); err != nil {
			problems = append(problems, fmt.Sprintf("%s: the starting commit %s is not on the base branch.", b, start))
			continue
		}
		same, err := now.sameWorkload(ctx, r, start, b)
		if err != nil {
			return nil, err
		}
		if !same {
			problems = append(problems, fmt.Sprintf("%s: its workload is not the one at its starting commit %s. Delete its entry from starts in %s. It then starts at the commit that merges this change.", b, short(start), configPath))
		}
	}
	return problems, nil
}

func strayLimits(now snapshot) []string {
	var problems []string
	for _, key := range sortedKeys(now.config.Benchmarks) {
		b, _ := parseBench(key)
		b.name, _, _ = strings.Cut(b.name, "/")
		if !slices.Contains(now.benches, b) {
			problems = append(problems, fmt.Sprintf("%s: no such benchmark. Delete its entry from benchmarks in %s.", key, configPath))
		}
	}
	return problems
}

func (s snapshot) sameWorkload(ctx context.Context, r repo, commit string, b bench) (bool, error) {
	w, err := newWorkloads(ctx, commitTree{r, commit}, s.config.WorkloadDirs)
	if err != nil {
		return false, err
	}
	found, err := w.fingerprint(ctx, b)
	return found == s.fingerprints[b], err
}

func benchChanges(was, now snapshot) []string {
	var asks []string
	for _, b := range was.benches {
		switch now.fingerprints[b] {
		case was.fingerprints[b]:
		case "":
			asks = append(asks, fmt.Sprintf("%s: the benchmark is gone, so its drift check ends.", b))
		default:
			asks = append(asks, fmt.Sprintf("%s: its workload changes, so it gets a new starting commit.", b))
		}
	}
	return asks
}

func startChanges(was, now snapshot) []string {
	const added = "the commit that adds its workload"
	var asks []string
	for _, b := range now.benches {
		from, to := was.config.Starts[b.String()], now.config.Starts[b.String()]
		if from != to {
			asks = append(asks, fmt.Sprintf("%s: the starting commit moves from %s to %s.", b, cmp.Or(short(from), added), cmp.Or(short(to), added)))
		}
	}
	return asks
}

func short(commit string) string { return commit[:min(len(commit), 9)] }

func limitChanges(was, now config) []string {
	var asks []string
	for _, unit := range sortedKeys(was.Limits) {
		if now.Limits[unit].looserThan(was.Limits[unit]) {
			asks = append(asks, fmt.Sprintf("limits: the limit of %s gets looser.", unit))
		}
	}
	keys := maps.Clone(was.Benchmarks)
	if keys == nil {
		keys = map[string]limits{}
	}
	maps.Copy(keys, now.Benchmarks)
	for _, key := range sortedKeys(keys) {
		b, _ := parseBench(key)
		for _, unit := range sortedKeys(was.Limits) {
			before, after := was.limitFor(b, b.name, unit), now.limitFor(b, b.name, unit)
			if after.looserThan(before) {
				asks = append(asks, fmt.Sprintf("%s: the limit of %s gets looser.", key, unit))
			}
		}
	}
	return append(asks, dirChanges(was, now)...)
}

func dirChanges(was, now config) []string {
	if len(was.Limits) == 0 {
		return nil
	}
	var asks []string
	for _, dir := range was.WorkloadDirs {
		if !slices.Contains(now.WorkloadDirs, dir) {
			asks = append(asks, fmt.Sprintf("workload_dirs: %s is gone, so a change there no longer counts as a workload change.", dir))
		}
	}
	for _, dir := range now.WorkloadDirs {
		if !slices.Contains(was.WorkloadDirs, dir) {
			asks = append(asks, fmt.Sprintf("workload_dirs: %s is new, so a benchmark that uses it can get a newer starting commit.", dir))
		}
	}
	return asks
}

func (l limit) looserThan(was limit) bool {
	return l == (limit{}) || l.Percent > was.Percent || l.Min > was.Min || l.Any > was.Any
}
